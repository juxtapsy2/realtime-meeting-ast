package auth

import (
	"testing"
	"time"
)

func TestParseAllowed(t *testing.T) {
	got := ParseAllowed("  Alice@Example.com , bob@example.com, , ALICE@EXAMPLE.COM ")
	want := map[string]bool{"alice@example.com": true, "bob@example.com": true}
	if len(got) != 2 {
		t.Fatalf("got %v, want 2 distinct normalized emails", got)
	}
	for _, e := range got {
		if !want[e] {
			t.Errorf("unexpected email %q in parsed list", e)
		}
	}
	if got := ParseAllowed(""); got != nil {
		t.Errorf("empty raw should yield nil, got %v", got)
	}
}

func TestAllowedConfirmsCaseInsensitive(t *testing.T) {
	a := New([]byte("secret"), []string{"Owner@Company.io"}, time.Hour)
	cases := map[string]bool{
		"owner@company.io":    true,
		"OWNER@COMPANY.IO":    true,
		"  owner@company.io ": true,
		"other@company.io":    false,
	}
	for email, want := range cases {
		if got := a.Allowed(email); got != want {
			t.Errorf("Allowed(%q) = %v, want %v", email, got, want)
		}
	}
	if a.Enabled() != true {
		t.Error("Enabled() should be true when allowlist is non-empty")
	}
	if (&Authenticator{}).Enabled() {
		t.Error("Empty authenticator must report disabled")
	}
}

func TestIssueVerifyRoundTrip(t *testing.T) {
	a := New([]byte("secret"), []string{"alice@example.com"}, time.Hour)

	token, err := a.Issue("ALICE@EXAMPLE.COM")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	email, err := a.Verify(token)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if email != "alice@example.com" {
		t.Errorf("Verify returned %q, want normalized email", email)
	}
}

func TestVerifyRejectsTamperedToken(t *testing.T) {
	a := New([]byte("secret"), []string{"alice@example.com"}, time.Hour)

	token, _ := a.Issue("alice@example.com")

	// Flip one character in the payload (signed body).
	tampered := string(byte(token[0])^1) + token[1:]
	if _, err := a.Verify(tampered); err == nil {
		t.Error("tampered payload was accepted")
	}

	// Signature from a different secret must not validate.
	other := New([]byte("other-secret"), []string{"alice@example.com"}, time.Hour)
	if _, err := other.Verify(token); err == nil {
		t.Error("token signed with another secret was accepted")
	}
}

func TestVerifyRejectsExpiredToken(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	a := New([]byte("secret"), []string{"alice@example.com"}, time.Hour)
	a.now = func() time.Time { return now }

	token, _ := a.Issue("alice@example.com")

	// Simulate the clock advancing past the token TTL.
	a.now = func() time.Time { return now.Add(2 * time.Hour) }
	if _, err := a.Verify(token); err == nil {
		t.Error("expired token was accepted")
	}
}

func TestVerifyRejectsEmailRemovedFromAllowlist(t *testing.T) {
	a := New([]byte("secret"), []string{"alice@example.com"}, time.Hour)
	token, _ := a.Issue("alice@example.com")

	b := New([]byte("secret"), []string{"bob@example.com"}, time.Hour)
	if _, err := b.Verify(token); err == nil {
		t.Error("token for removed email was accepted")
	}
}

func TestStateTokenRoundTrip(t *testing.T) {
	a := New([]byte("secret"), []string{"alice@example.com"}, time.Hour)

	token, err := a.IssueState("nonce-abc", 2*time.Minute)
	if err != nil {
		t.Fatalf("IssueState: %v", err)
	}
	got, err := a.VerifyState(token)
	if err != nil {
		t.Fatalf("VerifyState: %v", err)
	}
	if got != "nonce-abc" {
		t.Errorf("VerifyState returned %q, want nonce", got)
	}
}

func TestTokenTypesAreSeparated(t *testing.T) {
	a := New([]byte("secret"), []string{"alice@example.com"}, time.Hour)

	session, _ := a.Issue("alice@example.com")
	state, _ := a.IssueState("nonce-abc", 2*time.Minute)

	if _, err := a.Verify(state); err == nil {
		t.Error("state token accepted as a session token")
	}
	if _, err := a.VerifyState(session); err == nil {
		t.Error("session token accepted as a state token")
	}
}
