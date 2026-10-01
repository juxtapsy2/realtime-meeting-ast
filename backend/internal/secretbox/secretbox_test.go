package secretbox

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

const sttField = "stt_api_key"

func TestNewRequiresSecret(t *testing.T) {
	if _, err := New(""); !errors.Is(err, ErrNoKey) {
		t.Fatalf("New(\"\") = %v, want ErrNoKey", err)
	}
}

func TestSealOpenRoundTrip(t *testing.T) {
	b, err := New("hmac-secret")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	sealed, err := b.Seal(sttField, "gsk_live_abc123")
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if strings.Contains(sealed, "gsk_live_abc123") {
		t.Fatal("ciphertext contains the plaintext")
	}

	opened, err := b.Open(sttField, sealed)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if opened != "gsk_live_abc123" {
		t.Fatalf("Open returned %q", opened)
	}
}

func TestSealIsRandomized(t *testing.T) {
	b, _ := New("hmac-secret")
	a, _ := b.Seal(sttField, "same")
	c, _ := b.Seal(sttField, "same")
	if a == c {
		t.Fatal("sealing the same value twice produced identical ciphertext")
	}
}

func TestOpenRejectsWrongField(t *testing.T) {
	b, _ := New("hmac-secret")
	sealed, _ := b.Seal(sttField, "key")
	if _, err := b.Open("llm_api_key", sealed); !errors.Is(err, ErrMalformed) {
		t.Fatalf("cross-field open = %v, want ErrMalformed", err)
	}
}

func TestOpenRejectsWrongSecret(t *testing.T) {
	sealed, _ := mustBox(t, "secret-a").Seal(sttField, "key")
	if _, err := mustBox(t, "secret-b").Open(sttField, sealed); !errors.Is(err, ErrMalformed) {
		t.Fatalf("open with another secret = %v, want ErrMalformed", err)
	}
}

func TestOpenRejectsTamperedCiphertext(t *testing.T) {
	sealed, _ := mustBox(t, "s").Seal(sttField, "key")
	raw, err := base64.RawURLEncoding.DecodeString(sealed)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	raw[len(raw)-1] ^= 0xff // flip a ciphertext byte
	tampered := base64.RawURLEncoding.EncodeToString(raw)
	if _, err := mustBox(t, "s").Open(sttField, tampered); !errors.Is(err, ErrMalformed) {
		t.Fatalf("open tampered = %v, want ErrMalformed", err)
	}
}

func TestOpenRejectsGarbage(t *testing.T) {
	b := mustBox(t, "s")
	if _, err := b.Open(sttField, "!!!not-base64!!!"); !errors.Is(err, ErrMalformed) {
		t.Fatalf("open garbage = %v, want ErrMalformed", err)
	}
	if _, err := b.Open(sttField, "c2hvcnQ"); !errors.Is(err, ErrMalformed) {
		t.Fatalf("open short blob = %v, want ErrMalformed", err)
	}
}

func mustBox(t *testing.T, secret string) *Box {
	t.Helper()
	b, err := New(secret)
	if err != nil {
		t.Fatalf("New(%q): %v", secret, err)
	}
	return b
}
