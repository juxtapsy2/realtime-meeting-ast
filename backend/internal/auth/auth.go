// Package auth implements a lightweight email-allowlist access gate.
//
// Authentication is intentionally simple: a visitor proves membership in the
// configured allowlist by providing their email, and the server issues a
// signed, expiring session token (HMAC-SHA256) as an HttpOnly cookie.
//
// This is a *cost and persona* gate: it stops anyone outside the allowlist
// from consuming the STT/LLM API keys behind this service. It is not
// multi-factor or strong identity proof. For stronger authentication
// (real email verification / SSO), pair this with a proxy identity provider
// such as Cloudflare Access.
package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

const CookieName = "auth_token"

// Role names, ordered from least to most privileged.
const (
	RoleUser       = "user"
	RoleAdmin      = "admin"
	RoleSuperAdmin = "superadmin"
)

// Authenticator issues and verifies signed session tokens against an email
// allowlist, and distinguishes the admin and superadmin roles. The user
// allowlist and admin list can be replaced at runtime (as the settings store
// changes); all reads are mutex-guarded.
//
// The superadmin is configured only through the SUPERADMIN_EMAIL environment
// variable: it is not stored in the database, so a compromised admin API
// caller cannot promote themselves above it.
type Authenticator struct {
	secret []byte
	ttl    time.Duration
	now    func() time.Time

	mu         sync.RWMutex
	allowed    map[string]struct{}
	admins     map[string]struct{}
	superAdmin string
}

// New returns an Authenticator. allowed emails are normalized (trimmed,
// lower-cased). secret authenticates tokens; ttl bounds session lifetime.
func New(secret []byte, allowed []string, ttl time.Duration) *Authenticator {
	set := make(map[string]struct{}, len(allowed))
	for _, email := range allowed {
		set[NormalizeEmail(email)] = struct{}{}
	}
	if len(secret) == 0 {
		secret = randomSecret()
	}
	return &Authenticator{
		secret:  secret,
		allowed: set,
		admins:  map[string]struct{}{},
		ttl:     ttl,
		now:     time.Now,
	}
}

// ParseAllowed splits a comma-separated allowlist value into normalized
// emails, ignoring blanks.
func ParseAllowed(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	seen := make(map[string]struct{}, len(parts))
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if email := NormalizeEmail(p); email != "" {
			if _, dup := seen[email]; dup {
				continue
			}
			seen[email] = struct{}{}
			out = append(out, email)
		}
	}
	return out
}

// NormalizeEmail trims whitespace and lower-cases an email address.
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// Enabled reports whether any emails are allowlisted. When false the caller
// should consider the gate open (deny-all would lock everyone out, and
// bypass keeps local development friction-free).
func (a *Authenticator) Enabled() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return len(a.allowed) > 0
}

// Allowed reports whether the email may hold a session. The superadmin is
// implicitly allowed: it is configured above the allowlist, so locking it out
// would remove the last account able to fix the allowlist.
func (a *Authenticator) Allowed(email string) bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	normalized := NormalizeEmail(email)
	if a.superAdmin != "" && a.superAdmin == normalized {
		return true
	}
	_, ok := a.allowed[normalized]
	return ok
}

// SetAllowed replaces the user allowlist at runtime. Existing session tokens
// are re-checked against the new list on every verification, so revocations
// take effect immediately.
func (a *Authenticator) SetAllowed(emails []string) {
	set := make(map[string]struct{}, len(emails))
	for _, e := range emails {
		if n := NormalizeEmail(e); n != "" {
			set[n] = struct{}{}
		}
	}
	a.mu.Lock()
	a.allowed = set
	a.mu.Unlock()
}

// SetAdmins replaces the admin allowlist at runtime.
func (a *Authenticator) SetAdmins(emails []string) {
	set := make(map[string]struct{}, len(emails))
	for _, e := range emails {
		if n := NormalizeEmail(e); n != "" {
			set[n] = struct{}{}
		}
	}
	a.mu.Lock()
	a.admins = set
	a.mu.Unlock()
}

// IsAdmin reports whether the email holds the admin role (or the superadmin
// role, which is strictly above admin). Admins may monitor the service through
// the admin API; a valid session (allowlisted user) is still required first.
func (a *Authenticator) IsAdmin(email string) bool {
	return a.Role(email) != RoleUser
}

// IsSuperAdmin reports whether the email is the single env-configured
// superadmin. Only the superadmin may change settings, user provider
// configuration, and the admin list.
func (a *Authenticator) IsSuperAdmin(email string) bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.superAdmin != "" && a.superAdmin == NormalizeEmail(email)
}

// Role returns the effective role for an email. The superadmin is not
// required to also appear in the database-backed admin list.
func (a *Authenticator) Role(email string) string {
	normalized := NormalizeEmail(email)
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.superAdmin != "" && a.superAdmin == normalized {
		return RoleSuperAdmin
	}
	if _, ok := a.admins[normalized]; ok {
		return RoleAdmin
	}
	return RoleUser
}

// Allowlist returns a snapshot of the user allowlist. Used for reporting and
// for unioning users into the admin view.
func (a *Authenticator) Allowlist() []string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	out := make([]string, 0, len(a.allowed))
	for email := range a.allowed {
		out = append(out, email)
	}
	return out
}

// SuperAdmin returns the configured superadmin email ("" when unset).
func (a *Authenticator) SuperAdmin() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.superAdmin
}

// SetSuperAdmin sets the single superadmin email. An empty value clears it.
func (a *Authenticator) SetSuperAdmin(email string) {
	normalized := NormalizeEmail(email)
	a.mu.Lock()
	a.superAdmin = normalized
	a.mu.Unlock()
}

// Issue signs a session token for the email (normalized). The token embeds
// an expiry and is self-contained (no server-side session storage).
func (a *Authenticator) Issue(email string) (string, error) {
	return a.issue(NormalizeEmail(email), tokenTypeSession, a.ttl)
}

// IssueState signs a short-lived one-shot token that is NOT bound to the
// allowlist. It is used to protect the OAuth callback from CSRF: the caller
// verifies it with VerifyState on the callback leg.
func (a *Authenticator) IssueState(nonce string, ttl time.Duration) (string, error) {
	return a.issue(nonce, tokenTypeState, ttl)
}

func (a *Authenticator) issue(sub, typ string, ttl time.Duration) (string, error) {
	payload, err := json.Marshal(tokenPayload{
		Sub: sub,
		Typ: typ,
		Exp: a.now().Add(ttl).Unix(),
	})
	if err != nil {
		return "", fmt.Errorf("auth: marshal token payload: %w", err)
	}
	body := base64.RawURLEncoding.EncodeToString(payload)
	sig := a.sign(body)
	return body + "." + sig, nil
}

// Verify checks token signature, expiry, type, and allowlist membership, and
// returns the token's email.
func (a *Authenticator) Verify(token string) (string, error) {
	return a.verify(token, tokenTypeSession)
}

// VerifyState checks a state token without consulting the allowlist.
func (a *Authenticator) VerifyState(token string) (string, error) {
	return a.verify(token, tokenTypeState)
}

func (a *Authenticator) verify(token, wantType string) (string, error) {
	dot := strings.IndexByte(token, '.')
	if dot < 0 {
		return "", errors.New("auth: malformed token")
	}
	body, sig := token[:dot], token[dot+1:]

	want := a.sign(body)
	if !hmac.Equal([]byte(sig), []byte(want)) {
		return "", errors.New("auth: invalid token signature")
	}

	raw, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil {
		return "", errors.New("auth: invalid token payload")
	}
	var payload tokenPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return "", errors.New("auth: invalid token payload")
	}
	if payload.Exp < a.now().Unix() {
		return "", errors.New("auth: token expired")
	}
	if payload.Typ != wantType {
		return "", errors.New("auth: token type mismatch")
	}
	if wantType == tokenTypeSession && !a.Allowed(payload.Sub) {
		return "", errors.New("auth: token email no longer allowed")
	}
	return payload.Sub, nil
}

const (
	tokenTypeSession = "session"
	tokenTypeState   = "state"

	// stateTokenTTL bounds the one-shot OAuth CSRF token lifetime.
	stateTokenTTL = 10 * time.Minute
)

type tokenPayload struct {
	Sub string `json:"sub"`
	Typ string `json:"typ"`
	Exp int64  `json:"exp"`
}

func (a *Authenticator) sign(body string) string {
	mac := hmac.New(sha256.New, a.secret)
	mac.Write([]byte(body))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func randomSecret() []byte {
	buf := make([]byte, 32)
	_, _ = rand.Read(buf)
	return buf
}
