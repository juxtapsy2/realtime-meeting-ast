package main

import (
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/user/realtime-meeting-ast/backend/internal/auth"
	"github.com/user/realtime-meeting-ast/backend/internal/meetings"
	"github.com/user/realtime-meeting-ast/backend/internal/providerconfig"
	"github.com/user/realtime-meeting-ast/backend/internal/sealedbox"
	"github.com/user/realtime-meeting-ast/backend/internal/settings"
)

// comboRequest is the body of a combo save/apply: just the name. It is read
// through readProviderRequest like every other provider write, so a sealed
// body and a plaintext one are both accepted.
type comboRequest struct {
	Name string `json:"name"`
}

// mapComboError turns a combo failure into the right status: bad names and
// unusable snapshots are the caller's input, a missing combo is a 404, a
// missing encryption box is a deployment problem, and anything else stays in
// the log rather than in the response.
func mapComboError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, providerconfig.ErrComboName):
		http.Error(w, "A combo name of up to 100 characters is required", http.StatusBadRequest)
	case errors.Is(err, providerconfig.ErrComboNoSource):
		http.Error(w, "Save a configuration with at least one API key first, then save it as a combo", http.StatusBadRequest)
	case errors.Is(err, providerconfig.ErrComboNotFound):
		http.Error(w, "Combo not found", http.StatusNotFound)
	case errors.Is(err, providerconfig.ErrOwnKeysNeedKey):
		http.Error(w, "Enabling own keys requires at least one STT or LLM API key", http.StatusBadRequest)
	case errors.Is(err, providerconfig.ErrNoEncryptionKey):
		http.Error(w, "Cannot apply combo: encryption is not configured", http.StatusServiceUnavailable)
	default:
		log.Printf("PROVIDERS: combo operation failed: %v", err)
		http.Error(w, "Combo operation failed", http.StatusInternalServerError)
	}
}

// handleSelfProviderCombos lists, snapshots, and deletes the signed-in user's
// saved STT/LLM combos. Like the self provider endpoint, the target identity
// always comes from the session, never from the body or URL, so there is no
// identifier a caller could swap to manage somebody else's combos.
func handleSelfProviderCombos(store *providerconfig.Store, sealBox *sealedbox.Keypair) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		acting := auth.NormalizeEmail(meetings.OwnerEmailFromContext(r.Context()))
		if !strings.Contains(acting, "@") {
			http.Error(w, "Sign in to manage your provider settings", http.StatusUnauthorized)
			return
		}

		switch r.Method {
		case http.MethodGet:
			list, err := store.ListCombos(r.Context(), acting)
			if err != nil {
				log.Printf("USER %s: failed to list provider combos: %v", acting, err)
				http.Error(w, "Failed to load your saved configurations", http.StatusInternalServerError)
				return
			}
			writeJSON(w, http.StatusOK, list)

		case http.MethodPost:
			var body comboRequest
			if !readProviderRequest(w, r, sealBox, &body) {
				return
			}
			if err := store.SaveCombo(r.Context(), acting, body.Name); err != nil {
				mapComboError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"saved": strings.TrimSpace(body.Name)})

		case http.MethodDelete:
			name := r.URL.Query().Get("name")
			if err := store.DeleteCombo(r.Context(), acting, name); err != nil {
				mapComboError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"deleted": strings.TrimSpace(name)})

		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	}
}

// handleSelfProviderComboApply switches the signed-in user's stored
// configuration to a saved combo. Applying rewrites the effective provider
// configuration exactly like a manual save, so it is audited the same way.
func handleSelfProviderComboApply(store *providerconfig.Store, settingsStore *settings.Store, a *auth.Authenticator, sealBox *sealedbox.Keypair) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		acting := auth.NormalizeEmail(meetings.OwnerEmailFromContext(r.Context()))
		if !strings.Contains(acting, "@") {
			http.Error(w, "Sign in to manage your provider settings", http.StatusUnauthorized)
			return
		}

		var body comboRequest
		if !readProviderRequest(w, r, sealBox, &body) {
			return
		}
		name := strings.TrimSpace(body.Name)
		if err := store.ApplyCombo(r.Context(), acting, name, acting); err != nil {
			mapComboError(w, err)
			return
		}
		if err := settingsStore.Audit(r.Context(), settings.ActionSet, acting, a.Role(acting), "USER_PROVIDERS:"+acting); err != nil {
			log.Printf("PROVIDERS: failed to record audit for combo apply by %s: %v", acting, err)
		}
		writeJSON(w, http.StatusOK, map[string]any{"applied": name})
	}
}
