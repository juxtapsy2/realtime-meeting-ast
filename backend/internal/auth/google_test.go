package auth

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"testing"
)

// fakeGoogle routes requests to the Google endpoints, emulating a successful
// token exchange and userinfo lookup unless the RoundTripper opts out.
type fakeGoogle struct {
	tokenStatus   int
	userinfo      func() (int, map[string]any)
	lastTokenForm url.Values
}

func (f *fakeGoogle) RoundTrip(req *http.Request) (*http.Response, error) {
	switch req.URL.Host {
	case "oauth2.googleapis.com":
		_ = req.ParseForm()
		f.lastTokenForm = req.PostForm
		body := map[string]any{"access_token": "fake-access-token"}
		code := http.StatusOK
		if f.tokenStatus != 0 {
			code, body = f.tokenStatus, map[string]any{"error": "invalid_grant"}
		}
		return jsonResponse(code, body)
	case "www.googleapis.com":
		if f.userinfo != nil {
			code, body := f.userinfo()
			return jsonResponse(code, body)
		}
		return jsonResponse(http.StatusOK, map[string]any{
			"email":          "alice@example.com",
			"email_verified": true,
			"name":           "Alice",
		})
	default:
		return jsonResponse(http.StatusNotFound, map[string]any{"error": "unexpected host " + req.URL.Host})
	}
}

func jsonResponse(status int, body any) (*http.Response, error) {
	raw, _ := json.Marshal(body)
	return &http.Response{
		StatusCode: status,
		Body:       jsonBody(raw),
		Header:     http.Header{"Content-Type": {"application/json"}},
	}, nil
}

func jsonBody(raw []byte) *bodyReadCloser { return &bodyReadCloser{raw: raw} }

type bodyReadCloser struct {
	raw []byte
	off int
}

func (b *bodyReadCloser) Read(p []byte) (int, error) {
	if b.off >= len(b.raw) {
		return 0, io.EOF
	}
	n := copy(p, b.raw[b.off:])
	b.off += n
	return n, nil
}
func (b *bodyReadCloser) Close() error { return nil }

func newFakeClient(f *fakeGoogle) *GoogleClient {
	return &GoogleClient{
		ClientID:     "client-id",
		ClientSecret: "client-secret",
		RedirectURL:  "https://app.example/auth/callback",
		HTTPClient:   &http.Client{Transport: f},
	}
}

func TestStartBuildsAuthorizeURL(t *testing.T) {
	c := newFakeClient(&fakeGoogle{})
	raw, err := c.Start("state-123")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse authorize URL: %v", err)
	}
	if u.Host != "accounts.google.com" {
		t.Errorf("unexpected host %q", u.Host)
	}
	q := u.Query()
	for k, want := range map[string]string{
		"client_id":     "client-id",
		"redirect_uri":  c.RedirectURL,
		"response_type": "code",
		"scope":         "openid email profile",
		"state":         "state-123",
	} {
		if got := q.Get(k); got != want {
			t.Errorf("param %s = %q, want %q", k, got, want)
		}
	}
}

func TestExchangeReturnsVerifiedUser(t *testing.T) {
	f := &fakeGoogle{}
	c := newFakeClient(f)
	user, err := c.Exchange(context.Background(), "code-1")
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	if user.Email != "alice@example.com" || !user.EmailVerified {
		t.Errorf("unexpected user: %+v", user)
	}
	if f.lastTokenForm.Get("client_secret") != "client-secret" {
		t.Error("token exchange did not send client_secret")
	}
}

func TestExchangeRejectsTokenFailure(t *testing.T) {
	c := newFakeClient(&fakeGoogle{tokenStatus: http.StatusBadRequest})
	if _, err := c.Exchange(context.Background(), "code-1"); err == nil {
		t.Error("token exchange failure was not surfaced")
	}
}

func TestExchangeRejectsUnverifiedAndEmptyEmail(t *testing.T) {
	cases := []map[string]any{
		{"email": "bob@example.com", "email_verified": false},
		{"email": "", "email_verified": true},
	}
	for _, u := range cases {
		c := newFakeClient(&fakeGoogle{userinfo: func() (int, map[string]any) { return 200, u }})
		if _, err := c.Exchange(context.Background(), "code-1"); err == nil {
			t.Errorf("expected rejection for userinfo %v", u)
		}
	}
}

func TestExchangeRejectsUserinfoFailure(t *testing.T) {
	c := newFakeClient(&fakeGoogle{userinfo: func() (int, map[string]any) { return http.StatusInternalServerError, nil }})
	if _, err := c.Exchange(context.Background(), "code-1"); err == nil {
		t.Error("userinfo failure was not surfaced")
	}
}

func TestConfigured(t *testing.T) {
	if newFakeClient(&fakeGoogle{}).Configured() != true {
		t.Error("fully configured client should be Configured()=true")
	}
	if (&GoogleClient{}).Configured() {
		t.Error("empty client must be Configured()=false")
	}
}

func TestConfiguredRequiresRedirect(t *testing.T) {
	c := newFakeClient(&fakeGoogle{})
	c.RedirectURL = ""
	if c.Configured() {
		t.Error("client without redirect URL must be Configured()=false")
	}
}
