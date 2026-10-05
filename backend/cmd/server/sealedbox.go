package main

import (
	"net/http"

	"github.com/user/realtime-meeting-ast/backend/internal/sealedbox"
)

// handleSealedBoxPublicKey publishes the public half of the seal box key so a
// browser can seal an admin payload before sending it. Only public material is
// returned: the private half never leaves the process and no configuration is
// exposed here.
//
// The endpoint is reached through the global auth middleware, so it is
// session-protected whenever access control is enabled - which is the only
// state in which an admin page could ask for it.
func handleSealedBoxPublicKey(kp *sealedbox.Keypair) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"alg": sealedbox.Algorithm,
			"kid": kp.ID(),
			"pub": kp.PublicKeyBase64(),
		})
	}
}
