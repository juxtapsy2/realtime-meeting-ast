package main

import (
	"context"
	"crypto/ecdh"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/user/realtime-meeting-ast/backend/internal/auth"
	"github.com/user/realtime-meeting-ast/backend/internal/meetings"
	"github.com/user/realtime-meeting-ast/backend/internal/providerconfig"
	"github.com/user/realtime-meeting-ast/backend/internal/sealedbox"
	"github.com/user/realtime-meeting-ast/backend/internal/settings"
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
// plain user never reaches an admin endpoint, an admin reaches the routes it
// is meant to operate (monitoring and the access list), and the superadmin
// alone reaches the routes that mutate shared state.
//
// These cases cover which route a role is admitted to. What an admitted admin
// may then write is decided per setting by canWriteSetting, which
// TestSettingWriteRoles covers.
func TestAdminRoleMatrix(t *testing.T) {
	a, tokens := newTestAuthenticator(t)

	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	// /api/admin/monitor and /api/admin/settings admit admins;
	// /api/admin/admins and /api/admin/users stay superadmin only.
	adminRoute := adminAuth(a, ok)
	superadminRoute := adminAuth(a, superAdminOnly(a, ok))

	tests := []struct {
		name    string
		handler http.HandlerFunc
		token   string
		want    int
	}{
		{"monitor without session", adminRoute, "", http.StatusUnauthorized},
		{"monitor as user", adminRoute, tokens["user@example.com"], http.StatusForbidden},
		{"monitor as admin", adminRoute, tokens["admin@example.com"], http.StatusOK},
		{"monitor as superadmin", adminRoute, tokens["root@example.com"], http.StatusOK},
		{"settings as admin", adminRoute, tokens["admin@example.com"], http.StatusOK},
		{"settings as superadmin", adminRoute, tokens["root@example.com"], http.StatusOK},
		{"admins as admin", superadminRoute, tokens["admin@example.com"], http.StatusForbidden},
		{"admins as superadmin", superadminRoute, tokens["root@example.com"], http.StatusOK},
		{"admins as user", superadminRoute, tokens["user@example.com"], http.StatusForbidden},
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

// The settings surface is authorised per setting rather than per route: the
// access list is an administrator's own responsibility, while the platform
// provider and model decide which account every default user is billed
// against, so only the superadmin may change those. Unknown names are never
// writable by anybody.
func TestSettingWriteRoles(t *testing.T) {
	cases := []struct {
		role string
		name string
		want bool
	}{
		{auth.RoleSuperAdmin, settings.KeyAllowedEmails.Name, true},
		{auth.RoleSuperAdmin, settings.KeySTTProvider.Name, true},
		{auth.RoleSuperAdmin, settings.KeyLLMProvider.Name, true},
		{auth.RoleSuperAdmin, settings.KeyLLMModel.Name, true},
		{auth.RoleAdmin, settings.KeyAllowedEmails.Name, true},
		{auth.RoleAdmin, settings.KeySTTProvider.Name, false},
		{auth.RoleAdmin, settings.KeyLLMProvider.Name, false},
		{auth.RoleAdmin, settings.KeyLLMModel.Name, false},
		{auth.RoleUser, settings.KeyAllowedEmails.Name, false},
		{auth.RoleUser, settings.KeyLLMProvider.Name, false},
		{auth.RoleAdmin, "LLM_API_KEY", false},
		{auth.RoleSuperAdmin, "AUTH_HMAC_SECRET", false},
		{auth.RoleSuperAdmin, "", false},
	}
	for _, tc := range cases {
		if got := canWriteSetting(tc.role, tc.name); got != tc.want {
			t.Errorf("canWriteSetting(%q, %q) = %v, want %v", tc.role, tc.name, got, tc.want)
		}
	}
}

// The platform Selection carries the API keys that resolve default users'
// providers, and it has no JSON tags, so a handler that marshals it directly
// would send those keys to the browser. Everything user-facing goes through
// platformConfig, which must keep the provider detail and drop the secrets.
func TestPlatformConfigRedactsPlatformKeys(t *testing.T) {
	store := providerconfig.NewStore(nil, nil, providerconfig.Selection{
		STTProvider: "google",
		STTAPIKey:   "platform-stt-secret",
		LLMProvider: "groq",
		LLMAPIKey:   "platform-llm-secret",
		LLMModel:    "openai/gpt-oss-120b",
	})
	raw, err := json.Marshal(platformConfig(store))
	if err != nil {
		t.Fatalf("json.Marshal(): %v", err)
	}
	for _, secret := range []string{"platform-stt-secret", "platform-llm-secret"} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("platformConfig leaked %q: %s", secret, raw)
		}
	}
	var cfg map[string]any
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("json.Unmarshal(): %v", err)
	}
	if cfg["stt_key_set"] != true || cfg["llm_key_set"] != true {
		t.Fatalf("key_set flags = %v / %v, want both true", cfg["stt_key_set"], cfg["llm_key_set"])
	}
	if cfg["stt_provider"] != "google" || cfg["llm_provider"] != "groq" {
		t.Fatalf("providers lost: %v / %v", cfg["stt_provider"], cfg["llm_provider"])
	}
	if cfg["own_keys_ok"] != false {
		t.Fatalf("own_keys_ok = %v, want false with no encryption box", cfg["own_keys_ok"])
	}
}

// The self-service endpoint must never act on an anonymous identity: when the
// auth gate is open authMiddleware lets such requests through on non-admin
// paths, so the handler itself is the last line of defence.
func TestSelfProviderRequiresSessionIdentity(t *testing.T) {
	kp, err := sealedbox.NewKeypair()
	if err != nil {
		t.Fatalf("sealedbox.NewKeypair(): %v", err)
	}
	handler := handleSelfProvider(nil, nil, nil, kp)
	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		req := httptest.NewRequest(method, "/api/user/provider", nil)
		rec := httptest.NewRecorder()
		handler(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s without session = %d, want %d", method, rec.Code, http.StatusUnauthorized)
		}
	}
}
