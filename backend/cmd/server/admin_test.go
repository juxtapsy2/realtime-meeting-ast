package main

import (
	"context"
	"crypto/ecdh"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/user/realtime-meeting-ast/backend/internal/auth"
	"github.com/user/realtime-meeting-ast/backend/internal/meetings"
	"github.com/user/realtime-meeting-ast/backend/internal/sealedbox"
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

// The user-provider update is the one admin body that carries API keys, so it
// is the one the browser seals. These tests pin the decoding contract that the
// handler relies on: a sealed body must decode to the same update a plaintext
// body would, and an envelope for a different key must be refused.
func newSealBox(t *testing.T) *sealedbox.Keypair {
	t.Helper()
	kp, err := sealedbox.NewKeypair()
	if err != nil {
		t.Fatalf("sealedbox.NewKeypair(): %v", err)
	}
	return kp
}

func sealProviderUpdate(t *testing.T, kp *sealedbox.Keypair, want adminUserProviderRequest) []byte {
	t.Helper()
	plaintext, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("json.Marshal(): %v", err)
	}
	env, err := sealedbox.Seal(kp, plaintext)
	if err != nil {
		t.Fatalf("sealedbox.Seal(): %v", err)
	}
	body, err := json.Marshal(map[string]any{"sealed": env})
	if err != nil {
		t.Fatalf("json.Marshal(): %v", err)
	}
	return body
}

func TestSealedProviderUpdateDecodes(t *testing.T) {
	kp := newSealBox(t)
	useOwn := true
	want := adminUserProviderRequest{
		Email:       "User@Example.com",
		UseOwnKeys:  &useOwn,
		STTProvider: "google",
		STTAPIKey:   "stt-secret",
		LLMProvider: "groq",
		LLMModel:    "openai/gpt-oss-120b",
		LLMAPIKey:   "llm-secret",
		ClearLLMKey: true,
	}

	var got adminUserProviderRequest
	if err := kp.OpenInto(sealProviderUpdate(t, kp, want), &got); err != nil {
		t.Fatalf("OpenInto(): %v", err)
	}
	if got.Email != want.Email || got.STTAPIKey != want.STTAPIKey || got.LLMAPIKey != want.LLMAPIKey {
		t.Fatalf("decoded update = %+v, want %+v", got, want)
	}
	if got.UseOwnKeys == nil || !*got.UseOwnKeys {
		t.Fatal("UseOwnKeys was lost during decoding")
	}
	if !got.ClearLLMKey || got.LLMModel != want.LLMModel || got.STTProvider != want.STTProvider {
		t.Fatalf("decoded update = %+v, want %+v", got, want)
	}
}

// A stale page, or a browser without Web Crypto, still sends plain JSON.
func TestPlaintextProviderUpdateStillDecodes(t *testing.T) {
	kp := newSealBox(t)
	want := adminUserProviderRequest{Email: "User@Example.com", LLMAPIKey: "llm-secret"}

	body, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("json.Marshal(): %v", err)
	}

	var got adminUserProviderRequest
	if err := kp.OpenInto(body, &got); err != nil {
		t.Fatalf("OpenInto(): %v", err)
	}
	if got.Email != want.Email || got.LLMAPIKey != want.LLMAPIKey {
		t.Fatalf("decoded update = %+v, want %+v", got, want)
	}
}

func TestProviderUpdateFromAnotherSealBoxIsRejected(t *testing.T) {
	intended := newSealBox(t)
	other := newSealBox(t)
	want := adminUserProviderRequest{Email: "user@example.com", LLMAPIKey: "llm-secret"}

	var got adminUserProviderRequest
	if err := other.OpenInto(sealProviderUpdate(t, intended, want), &got); !errors.Is(err, sealedbox.ErrKeyID) {
		t.Fatalf("OpenInto() error = %v, want ErrKeyID", err)
	}
	if got.LLMAPIKey != "" {
		t.Fatal("a rejected envelope still populated the update")
	}
}

// The browser has to be able to import the published key as an uncompressed
// P-256 point, and it has to be able to tell whether it speaks the same
// construction as the server.
func TestSealedBoxPublicKeyPayload(t *testing.T) {
	kp := newSealBox(t)
	handler := handleSealedBoxPublicKey(kp)

	req := httptest.NewRequest(http.MethodGet, "/api/sealedbox/public-key", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var payload struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
		Pub string `json:"pub"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal(): %v", err)
	}
	if payload.Alg != sealedbox.Algorithm {
		t.Fatalf("alg = %q, want %q", payload.Alg, sealedbox.Algorithm)
	}
	if payload.Kid != kp.ID() {
		t.Fatalf("kid = %q, want %q", payload.Kid, kp.ID())
	}
	if payload.Pub != kp.PublicKeyBase64() {
		t.Fatalf("pub = %q, want %q", payload.Pub, kp.PublicKeyBase64())
	}

	// The browser imports this value as a raw P-256 point, so it has to be a
	// well-formed uncompressed point rather than, say, a PEM blob.
	raw, err := base64.StdEncoding.DecodeString(payload.Pub)
	if err != nil {
		t.Fatalf("published key is not base64: %v", err)
	}
	if len(raw) != 65 || raw[0] != 0x04 {
		t.Fatalf("published key length = %d, prefix = %#x; want 65 bytes starting 0x04", len(raw), raw[0])
	}
	if _, err := ecdh.P256().NewPublicKey(raw); err != nil {
		t.Fatalf("published key is not an importable P-256 point: %v", err)
	}
}
