package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/user/realtime-meeting-ast/backend/internal/auth"
)

// wrapComboRequest runs one request through the session middleware the way the
// real chain does, with a cookie only when token is non-empty.
func wrapComboRequest(t *testing.T, a *auth.Authenticator, handler http.Handler, method, target, body, token string) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, target, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, target, nil)
	}
	if token != "" {
		req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})
	}
	rec := httptest.NewRecorder()
	withSessionEmail(a, handler).ServeHTTP(rec, req)
	return rec
}

// Like the self provider endpoint, the combo routes are reached without an
// auth gate on non-admin paths, so the handler itself must refuse anonymous
// identities — with no store touched at all.
func TestSelfProviderCombosRequireSessionIdentity(t *testing.T) {
	a, _ := newTestAuthenticator(t)
	kp := newSealBox(t)
	handler := handleSelfProviderCombos(nil, kp)

	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodDelete} {
		rec := wrapComboRequest(t, a, handler, method, "/api/user/provider/combos", "", "")
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s without session = %d, want %d", method, rec.Code, http.StatusUnauthorized)
		}
	}
}

// Validation happens before any store call, so these paths are observable with
// a nil store: a blank name is bad input (400), an unlisted method is 405.
func TestSelfProviderCombosValidateNameBeforeStore(t *testing.T) {
	a, tokens := newTestAuthenticator(t)
	kp := newSealBox(t)
	handler := handleSelfProviderCombos(nil, kp)
	token := tokens["user@example.com"]

	rec := wrapComboRequest(t, a, handler, http.MethodPost, "/api/user/provider/combos",
		`{"name":"   "}`, token)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("POST with blank name = %d, want %d", rec.Code, http.StatusBadRequest)
	}

	rec = wrapComboRequest(t, a, handler, http.MethodDelete, "/api/user/provider/combos", "", token)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("DELETE without name = %d, want %d", rec.Code, http.StatusBadRequest)
	}

	rec = wrapComboRequest(t, a, handler, http.MethodPut, "/api/user/provider/combos", "", token)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("PUT = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

// The apply route is POST-only, requires a session, and validates the name
// before the store (nil here) is ever called; a failure returns before the
// audit trail (nil settings store) is touched.
func TestSelfProviderComboApplyGatekeeping(t *testing.T) {
	a, tokens := newTestAuthenticator(t)
	kp := newSealBox(t)
	handler := handleSelfProviderComboApply(nil, nil, a, kp)
	token := tokens["user@example.com"]

	rec := wrapComboRequest(t, a, handler, http.MethodPut, "/api/user/provider/combos/apply", "", token)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("PUT = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}

	rec = wrapComboRequest(t, a, handler, http.MethodPost, "/api/user/provider/combos/apply", "", "")
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("POST without session = %d, want %d", rec.Code, http.StatusUnauthorized)
	}

	rec = wrapComboRequest(t, a, handler, http.MethodPost, "/api/user/provider/combos/apply",
		`{"name":""}`, token)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("POST with blank name = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}
