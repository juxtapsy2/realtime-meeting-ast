package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/user/realtime-meeting-ast/backend/internal/auth"
	"github.com/user/realtime-meeting-ast/backend/internal/meetings"
)

const testSecret = "test-auth-secret"

// newTestAuthenticator builds an authenticator with one allowlisted user, one
// database-managed admin, and one env-configured superadmin. Only the
// superadmin is in the allowlist implicitly, not by name.
func newTestAuthenticator(t *testing.T) (*auth.Authenticator, map[string]string) {
	t.Helper()
	a := auth.New([]byte(testSecret), auth.ParseAllowed("user@example.com,admin@example.com"), time.Hour)
	a.SetAdmins([]string{"admin@example.com"})
	a.SetSuperAdmin("root@example.com")

	tokens := map[string]string{}
	for _, email := range []string{"user@example.com", "admin@example.com", "root@example.com"} {
		token, err := a.Issue(email)
		if err != nil {
			t.Fatalf("Issue(%q): %v", email, err)
		}
		tokens[email] = token
	}
	return a, tokens
}

func request(t *testing.T, a *auth.Authenticator, method, path, token string) *httptest.ResponseRecorder {
	t.Helper()
	handler := authMiddleware(a, withSessionEmail(a, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})))
	req := httptest.NewRequest(method, path, nil)
	if token != "" {
		req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

// The role boundary is the security-critical part of the admin plane: a
// plain user may never reach admin endpoints, an admin may monitor but not
// mutate, and the superadmin may do both.
func TestAdminRoleMatrix(t *testing.T) {
	a, tokens := newTestAuthenticator(t)

	monitor := adminAuth(a, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	settings := adminAuth(a, superAdminOnly(a, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})))

	tests := []struct {
		name    string
		handler http.HandlerFunc
		token   string
		want    int
	}{
		{"monitor without session", monitor, "", http.StatusUnauthorized},
		{"monitor as user", monitor, tokens["user@example.com"], http.StatusForbidden},
		{"monitor as admin", monitor, tokens["admin@example.com"], http.StatusOK},
		{"monitor as superadmin", monitor, tokens["root@example.com"], http.StatusOK},
		{"settings as admin", settings, tokens["admin@example.com"], http.StatusForbidden},
		{"settings as superadmin", settings, tokens["root@example.com"], http.StatusOK},
		{"settings as user", settings, tokens["user@example.com"], http.StatusForbidden},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/admin/x", nil)
			req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: tc.token})
			rec := httptest.NewRecorder()
			tc.handler(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d (body %q)", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

// A revoked user must lose access immediately even while holding a valid
// token, and a forged token must never authenticate.
func TestRevokedAndForgedSessionsAreRejected(t *testing.T) {
	a, tokens := newTestAuthenticator(t)

	rec := request(t, a, http.MethodGet, "/api/meetings", tokens["user@example.com"])
	if rec.Code != http.StatusOK {
		t.Fatalf("allowlisted user status = %d, want 200", rec.Code)
	}

	a.SetAllowed(auth.ParseAllowed("admin@example.com"))
	if rec := request(t, a, http.MethodGet, "/api/meetings", tokens["user@example.com"]); rec.Code != http.StatusUnauthorized {
		t.Fatalf("revoked user status = %d, want 401", rec.Code)
	}
	if rec := request(t, a, http.MethodGet, "/api/meetings", tokens["admin@example.com"]); rec.Code != http.StatusOK {
		t.Fatalf("remaining allowlisted user status = %d, want 200", rec.Code)
	}
	if rec := request(t, a, http.MethodGet, "/api/meetings", "not.a.token"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("forged token status = %d, want 401", rec.Code)
	}
}

// With an empty allowlist the general gate is open, but /api/admin/* must still
// require a signed session so a non-admin cannot reach the admin surface.
func TestOpenGateStillProtectsAdminSurface(t *testing.T) {
	a := auth.New([]byte(testSecret), nil, time.Hour)
	a.SetSuperAdmin("root@example.com")
	rootToken, err := a.Issue("root@example.com")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	userToken, err := a.Issue("someone@example.com")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	if rec := request(t, a, http.MethodGet, "/api/meetings", ""); rec.Code != http.StatusOK {
		t.Fatalf("open gate status = %d, want 200", rec.Code)
	}
	if rec := request(t, a, http.MethodGet, "/api/admin/monitor", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous admin status = %d, want 401", rec.Code)
	}
	// A non-allowlisted email cannot hold a session at all when the gate is
	// open, so it cannot become an admin by signing in.
	if rec := request(t, a, http.MethodGet, "/api/admin/monitor", userToken); rec.Code != http.StatusUnauthorized {
		t.Fatalf("non-allowlisted admin status = %d, want 401", rec.Code)
	}
	adminMonitor := adminAuth(a, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/api/admin/monitor", nil)
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: rootToken})
	rec := httptest.NewRecorder()
	adminMonitor(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("superadmin monitor status = %d, want 200", rec.Code)
	}
}

// The superadmin is not in the allowlist, so /api/auth/me must still report its
// role from the session cookie; otherwise the admin button never appears.
func TestAuthMeReportsSuperAdminWhenGateIsOpen(t *testing.T) {
	a := auth.New([]byte(testSecret), nil, time.Hour)
	a.SetSuperAdmin("root@example.com")
	token, err := a.Issue("root@example.com")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	get := func(cookie string) map[string]any {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
		if cookie != "" {
			req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: cookie})
		}
		rec := httptest.NewRecorder()
		handleAuthMe(a)(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
		}
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return body
	}

	anonymous := get("")
	if anonymous["role"] != auth.RoleUser || anonymous["is_admin"] != false {
		t.Fatalf("anonymous role = %v (is_admin %v), want user", anonymous["role"], anonymous["is_admin"])
	}

	signedIn := get(token)
	if signedIn["email"] != "root@example.com" {
		t.Fatalf("email = %v, want root@example.com", signedIn["email"])
	}
	if signedIn["is_superadmin"] != true || signedIn["is_admin"] != true {
		t.Fatalf("role = %v (is_superadmin %v), want superadmin", signedIn["role"], signedIn["is_superadmin"])
	}
	if signedIn["role"] != auth.RoleSuperAdmin {
		t.Fatalf("role = %v, want %q", signedIn["role"], auth.RoleSuperAdmin)
	}
}

// When the gate is on, /api/auth/me must reject a missing or invalid session
// instead of reporting an anonymous user as authenticated.
func TestAuthMeRequiresSessionWhenGateIsEnabled(t *testing.T) {
	a := auth.New([]byte(testSecret), auth.ParseAllowed("user@example.com"), time.Hour)

	for _, cookie := range []string{"", "garbage"} {
		req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
		if cookie != "" {
			req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: cookie})
		}
		rec := httptest.NewRecorder()
		handleAuthMe(a)(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("cookie %q status = %d, want 401", cookie, rec.Code)
		}
	}
}

// The superadmin is env-only and therefore absent from the allowlist; it must
// still appear in the user list so the admin UI can configure it.
func TestKnownUsersIncludesSuperAdmin(t *testing.T) {
	a, _ := newTestAuthenticator(t)

	users := knownUsers(a)
	found := map[string]bool{}
	for _, u := range users {
		found[u] = true
	}
	for _, want := range []string{"user@example.com", "admin@example.com", "root@example.com"} {
		if !found[want] {
			t.Fatalf("knownUsers() = %v, missing %s", users, want)
		}
	}
	if len(users) != len(found) {
		t.Fatalf("knownUsers() = %v, contains duplicates", users)
	}
}

// Meetings must be stamped with the signed-in owner so their provider
// selection can be resolved later.
func TestSessionEmailReachesRequestContext(t *testing.T) {
	a, tokens := newTestAuthenticator(t)

	var got string
	handler := withSessionEmail(a, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = meetings.OwnerEmailFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/api/meetings", nil)
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: tokens["user@example.com"]})
	handler.ServeHTTP(httptest.NewRecorder(), req)

	if got != "user@example.com" {
		t.Fatalf("owner = %q, want user@example.com", got)
	}
	if got := meetings.OwnerEmailFromContext(context.Background()); got != "" {
		t.Fatalf("owner for an anonymous request = %q, want empty", got)
	}
}

// The stored allowlist is what the admin UI renders as chips, so it must be the
// same list that is actually enforced: trimmed, lower-cased, no duplicates.
// A duplicate would otherwise show as two identical chips that both work.
func TestCanonicalAllowlist(t *testing.T) {
	cases := map[string]string{
		"  Alice@Example.com , bob@example.com ,, ALICE@EXAMPLE.COM , carol@example.com ": "alice@example.com,bob@example.com,carol@example.com",
		"a@example.com": "a@example.com",
		// An empty value is not an allowlist: it clears the override, which is a
		// different action and must not be rewritten into an empty string here.
		"":    "",
		"   ": "   ",
	}
	for in, want := range cases {
		if got := canonicalAllowlist(in); got != want {
			t.Errorf("canonicalAllowlist(%q) = %q, want %q", in, got, want)
		}
	}
}
