package main

import (
	"database/sql"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/user/realtime-meeting-ast/backend/internal/auth"
	"github.com/user/realtime-meeting-ast/backend/internal/meetings"
	"github.com/user/realtime-meeting-ast/backend/internal/providerconfig"
	"github.com/user/realtime-meeting-ast/backend/internal/sealedbox"
	"github.com/user/realtime-meeting-ast/backend/internal/settings"
	"github.com/user/realtime-meeting-ast/backend/internal/transcription"
)

// adminUserProviderRequest is the write-only update payload for one user's
// provider configuration. API keys are only ever written: omitting a key
// leaves the stored key untouched, and clear_* removes it. Both the admin
// path (which names another user) and the self-service path (which ignores
// any email in the body) decode into this type.
type adminUserProviderRequest struct {
	Email       string `json:"email"`
	UseOwnKeys  *bool  `json:"use_own_keys"`
	STTProvider string `json:"stt_provider"`
	STTAPIKey   string `json:"stt_api_key"`
	ClearSTTKey bool   `json:"clear_stt_key"`
	LLMProvider string `json:"llm_provider"`
	LLMModel    string `json:"llm_model"`
	LLMAPIKey   string `json:"llm_api_key"`
	ClearLLMKey bool   `json:"clear_llm_key"`
}

// platformConfig is the platform defaults as safe to send to a browser: the
// provider and model that apply, only whether a key exists, and whether own
// keys can be stored at all.
//
// providerconfig.Selection deliberately carries the platform API keys in
// plaintext for internal resolution, and it has no JSON tags, so serializing
// it directly would hand those keys to the client. Every response therefore
// goes through here.
func platformConfig(store *providerconfig.Store) map[string]any {
	platform := store.Platform()
	return map[string]any{
		"stt_provider": platform.STTProvider,
		"stt_key_set":  platform.STTAPIKey != "",
		"llm_provider": platform.LLMProvider,
		"llm_model":    platform.LLMModel,
		"llm_key_set":  platform.LLMAPIKey != "",
		"google_stt":   platform.STTProvider == string(transcription.ProviderGoogle),
		"own_keys_ok":  store.OwnKeysAvailable(),
	}
}

// readProviderRequest decodes one update body, which may arrive sealed because
// it can carry API keys. Every failure is bad client input: an unexpected
// JSON document, or an envelope that does not decrypt for this server key.
func readProviderRequest(w http.ResponseWriter, r *http.Request, sealBox *sealedbox.Keypair, body *adminUserProviderRequest) bool {
	// The body is size limited before decoding because a sealed envelope still
	// arrives as one JSON object, and an oversized one is bad input whether it
	// is encrypted or not.
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<16))
	if err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return false
	}
	if err := sealBox.OpenInto(raw, body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return false
	}
	return true
}

// applyProviderReset returns one user to the platform provider defaults. The
// target must already be decided by the caller: the admin path takes it from
// the request, the self-service path takes it from the session.
func applyProviderReset(w http.ResponseWriter, r *http.Request, store *providerconfig.Store, settingsStore *settings.Store, a *auth.Authenticator, acting, target string) {
	ctx := r.Context()
	if err := store.Delete(ctx, target); err != nil {
		log.Printf("PROVIDERS: %s failed to reset configuration for %s: %v", acting, target, err)
		http.Error(w, "Failed to reset user configuration", http.StatusInternalServerError)
		return
	}
	log.Printf("PROVIDERS: %s (%s) reset %s to platform provider defaults", acting, a.Role(acting), target)
	// Removing stored keys is a configuration change, so it is audited like any
	// other. The audit stores the target email, never a key.
	if err := settingsStore.Audit(ctx, settings.ActionClear, acting, a.Role(acting), "USER_PROVIDERS:"+target); err != nil {
		log.Printf("PROVIDERS: failed to record audit for reset of %s: %v", target, err)
	}
	writeJSON(w, http.StatusOK, map[string]any{"reset": target})
}

// applyProviderUpdate writes one user's provider configuration and sends the
// response. It is the single write path for both the admin handler and the
// self-service handler, so validation, error mapping, and auditing cannot
// drift between them.
//
// acting is the session identity recorded in the audit trail; target is the
// user being changed. They are equal on the self-service path, which is the
// only way a non-superadmin ever reaches here.
func applyProviderUpdate(w http.ResponseWriter, r *http.Request, store *providerconfig.Store, settingsStore *settings.Store, a *auth.Authenticator, acting, target string, body adminUserProviderRequest) {
	ctx := r.Context()

	if !store.OwnKeysAvailable() && (body.STTAPIKey != "" || body.LLMAPIKey != "") {
		http.Error(w, "Cannot store user API keys: AUTH_HMAC_SECRET is not configured", http.StatusServiceUnavailable)
		return
	}

	// Preserve the current toggle when the caller does not send one.
	useOwn := false
	current, err := store.Get(ctx, target)
	switch {
	case body.UseOwnKeys != nil:
		useOwn = *body.UseOwnKeys
	case err == nil:
		useOwn = current.UseOwnKeys
	case err != sql.ErrNoRows:
		log.Printf("PROVIDERS: %s failed to load configuration for %s: %v", acting, target, err)
		http.Error(w, "Failed to load user configuration", http.StatusInternalServerError)
		return
	}

	if err := store.Save(ctx, providerconfig.SaveInput{
		Email:       target,
		UseOwnKeys:  useOwn,
		STTProvider: body.STTProvider,
		STTAPIKey:   body.STTAPIKey,
		ClearSTTKey: body.ClearSTTKey,
		LLMProvider: body.LLMProvider,
		LLMModel:    body.LLMModel,
		LLMAPIKey:   body.LLMAPIKey,
		ClearLLMKey: body.ClearLLMKey,
		UpdatedBy:   acting,
	}); err != nil {
		log.Printf("PROVIDERS: %s failed to save configuration for %s: %v", acting, target, err)
		switch {
		case errors.Is(err, providerconfig.ErrNoEncryptionKey):
			http.Error(w, "Cannot store user API keys: encryption is not configured", http.StatusServiceUnavailable)
			return
		case errors.Is(err, providerconfig.ErrOwnKeysNeedKey):
			http.Error(w, "Enabling own keys requires at least one STT or LLM API key", http.StatusBadRequest)
			return
		}
		// Other failures (constraint violations, connectivity) are server-side
		// problems; the cause stays in the log, not in the response.
		http.Error(w, "Failed to save user configuration", http.StatusInternalServerError)
		return
	}

	action := settings.ActionSet
	if !useOwn {
		action = settings.ActionClear
	}
	if acting == target {
		log.Printf("USER %s: updated own provider settings (own_keys=%v)", acting, useOwn)
	} else {
		log.Printf("ADMIN %s (%s): updated provider settings for %s (own_keys=%v)", acting, a.Role(acting), target, useOwn)
	}
	if err := settingsStore.Audit(ctx, action, acting, a.Role(acting), "USER_PROVIDERS:"+target); err != nil {
		log.Printf("PROVIDERS: failed to record audit for user providers %s: %v", target, err)
	}
	writeJSON(w, http.StatusOK, map[string]any{"email": target, "use_own_keys": useOwn})
}

// handleSelfProvider serves the signed-in user's own provider configuration.
//
// The target email always comes from the session, never from the body or the
// URL, so there is no identifier a caller can swap to edit somebody else's
// settings: an email in the body is decoded and then ignored. When the auth
// gate is open authMiddleware lets unauthenticated requests through on
// non-admin paths, so a missing identity is refused here.
func handleSelfProvider(store *providerconfig.Store, settingsStore *settings.Store, a *auth.Authenticator, sealBox *sealedbox.Keypair) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		acting := auth.NormalizeEmail(meetings.OwnerEmailFromContext(r.Context()))
		if !strings.Contains(acting, "@") {
			http.Error(w, "Sign in to manage your provider settings", http.StatusUnauthorized)
			return
		}

		switch r.Method {
		case http.MethodGet:
			summary, err := store.Summary(r.Context(), acting)
			if err != nil {
				log.Printf("USER %s: failed to read own provider settings: %v", acting, err)
				http.Error(w, "Failed to load your configuration", http.StatusInternalServerError)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"user":     summary,
				"platform": platformConfig(store),
			})

		case http.MethodPut, http.MethodPost:
			var body adminUserProviderRequest
			if !readProviderRequest(w, r, sealBox, &body) {
				return
			}
			applyProviderUpdate(w, r, store, settingsStore, a, acting, acting, body)

		case http.MethodDelete:
			applyProviderReset(w, r, store, settingsStore, a, acting, acting)

		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	}
}
