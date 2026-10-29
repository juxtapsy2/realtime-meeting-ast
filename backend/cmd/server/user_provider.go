package main

import (
	"database/sql"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/user/realtime-meeting-ast/backend/internal/auth"
	"github.com/user/realtime-meeting-ast/backend/internal/intelligence"
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
// it can carry API keys. dst is the destination for the decoded document (a
// pointer to the caller's request type). Every failure is bad client input: an
// unexpected JSON document, or an envelope that does not decrypt for this
// server key.
func readProviderRequest(w http.ResponseWriter, r *http.Request, sealBox *sealedbox.Keypair, dst any) bool {
	// The body is size limited before decoding because a sealed envelope still
	// arrives as one JSON object, and an oversized one is bad input whether it
	// is encrypted or not.
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<16))
	if err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return false
	}
	if err := sealBox.OpenInto(raw, dst); err != nil {
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

// handleProviderModels returns the LLM model IDs available for a provider so
// the settings UI can offer a picker instead of free-typing model names.
//
// The body is sealed like every provider write because it may carry a freshly
// typed API key. With no key in the body the target's stored (or platform)
// key is used, so a user with a saved key just picks a model. selfOnly
// selects the identity rule: the self-service route always acts on the
// session identity, the admin route on the body's email (superadmin-gated by
// its middleware).
func handleProviderModels(store *providerconfig.Store, sealBox *sealedbox.Keypair, selfOnly bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		target := ""
		if selfOnly {
			target = auth.NormalizeEmail(meetings.OwnerEmailFromContext(r.Context()))
			if !strings.Contains(target, "@") {
				http.Error(w, "Sign in to manage your provider settings", http.StatusUnauthorized)
				return
			}
		} else {
			acting, _ := r.Context().Value(adminEmailCtxKey{}).(string)
			target = auth.NormalizeEmail(acting)
			if !strings.Contains(target, "@") {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
		}

		var body adminUserProviderRequest
		if !readProviderRequest(w, r, sealBox, &body) {
			return
		}
		if !selfOnly {
			target = auth.NormalizeEmail(body.Email)
			if !strings.Contains(target, "@") {
				http.Error(w, "A valid email is required", http.StatusBadRequest)
				return
			}
		}

		// A typed key wins (preview before saving); otherwise fall back to
		// the target's resolved configuration, which already merges own-key
		// and platform fallback per field. Provider names are validated
		// lower-case, so a typed "Gemini" is accepted as "gemini".
		providerName := strings.ToLower(strings.TrimSpace(body.LLMProvider))
		apiKey := strings.TrimSpace(body.LLMAPIKey)
		if providerName == "" || apiKey == "" {
			sel, err := store.For(r.Context(), target)
			if err != nil {
				log.Printf("PROVIDERS: model list failed to resolve configuration for %s: %v", target, err)
				http.Error(w, "Failed to resolve provider configuration", http.StatusInternalServerError)
				return
			}
			if providerName == "" {
				providerName = sel.LLMProvider
			}
			if apiKey == "" {
				apiKey = sel.LLMAPIKey
			}
		}

		switch intelligence.Provider(providerName) {
		case intelligence.ProviderOpenAI, intelligence.ProviderGroq, intelligence.ProviderGemini:
		default:
			// Surface this as client input, not an upstream failure: the
			// picker only makes sense for providers we can query.
			http.Error(w, `Unsupported LLM provider "`+providerName+`" — supported: openai, groq, gemini`, http.StatusBadRequest)
			return
		}
		if apiKey == "" {
			http.Error(w, "No API key available for this provider — type a key first", http.StatusBadRequest)
			return
		}

		models, err := intelligence.ListModels(r.Context(), intelligence.Provider(providerName), apiKey)
		if err != nil {
			// The upstream status/code is safe to surface; details stay in
			// the log and the key never appears in either.
			log.Printf("PROVIDERS: %s: model list for provider %q failed: %v", target, providerName, err)
			http.Error(w, "Failed to list models from the provider: "+err.Error(), http.StatusBadGateway)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"models": models})
	}
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
