package sealedbox

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"
)

func decodeBase64ForTest(t *testing.T, s string) ([]byte, error) {
	t.Helper()
	return base64.StdEncoding.DecodeString(s)
}

func encodeBase64ForTest(b []byte) string {
	return base64.StdEncoding.EncodeToString(b)
}

func newTestKeypair(t *testing.T) *Keypair {
	t.Helper()
	kp, err := NewKeypair()
	if err != nil {
		t.Fatalf("NewKeypair(): %v", err)
	}
	return kp
}

func TestSealOpenRoundTrip(t *testing.T) {
	kp := newTestKeypair(t)
	plaintext := []byte(`{"email":"user@example.com","llm_api_key":"sk-live-secret"}`)

	env, err := Seal(kp, plaintext)
	if err != nil {
		t.Fatalf("Seal(): %v", err)
	}
	got, err := kp.Open(env)
	if err != nil {
		t.Fatalf("Open(): %v", err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Fatalf("Open() = %q, want %q", got, plaintext)
	}
}

// The client binds the key id and ephemeral point into the HKDF context, so
// two seals of the same plaintext must not produce the same ciphertext.
func TestSealsAreNotDeterministic(t *testing.T) {
	kp := newTestKeypair(t)
	plaintext := []byte("sk-live-secret")

	first, err := Seal(kp, plaintext)
	if err != nil {
		t.Fatalf("Seal(): %v", err)
	}
	second, err := Seal(kp, plaintext)
	if err != nil {
		t.Fatalf("Seal(): %v", err)
	}
	if first.Ciphertext == second.Ciphertext {
		t.Fatal("two seals produced identical ciphertext")
	}
	if first.Nonce == second.Nonce {
		t.Fatal("two seals produced identical nonces")
	}
}

func TestOpenRejectsWrongKeyID(t *testing.T) {
	intended := newTestKeypair(t)
	other := newTestKeypair(t)

	env, err := Seal(intended, []byte("secret"))
	if err != nil {
		t.Fatalf("Seal(): %v", err)
	}
	if _, err := other.Open(env); !errors.Is(err, ErrKeyID) {
		t.Fatalf("Open() error = %v, want ErrKeyID", err)
	}
}

func TestOpenRejectsUnknownVersion(t *testing.T) {
	kp := newTestKeypair(t)
	env, err := Seal(kp, []byte("secret"))
	if err != nil {
		t.Fatalf("Seal(): %v", err)
	}
	env.Version = version + 1

	if _, err := kp.Open(env); !errors.Is(err, ErrVersion) {
		t.Fatalf("Open() error = %v, want ErrVersion", err)
	}
}

// Every decode step runs on attacker-controlled input, so each kind of
// corruption must be refused rather than reach key agreement.
func TestOpenRejectsMalformedEnvelopes(t *testing.T) {
	kp := newTestKeypair(t)

	cases := map[string]func(*Envelope){
		"bad base64 ephemeral point": func(e *Envelope) { e.Ephemeral = "not-base64!!" },
		"truncated ephemeral point":  func(e *Envelope) { e.Ephemeral = "AABA" },
		"bad base64 nonce":           func(e *Envelope) { e.Nonce = "%%%" },
		"short nonce":                func(e *Envelope) { e.Nonce = "AAAAAAAA" },
		"empty ciphertext":           func(e *Envelope) { e.Ciphertext = "" },
		"missing key id":             func(e *Envelope) { e.KeyID = "" },
	}

	for name, corrupt := range cases {
		t.Run(name, func(t *testing.T) {
			env, err := Seal(kp, []byte("secret"))
			if err != nil {
				t.Fatalf("Seal(): %v", err)
			}
			corrupt(&env)
			if _, err := kp.Open(env); !errors.Is(err, ErrMalformed) && !errors.Is(err, ErrKeyID) {
				t.Fatalf("Open() error = %v, want ErrMalformed or ErrKeyID", err)
			}
		})
	}
}

// Flipping a bit in the ciphertext must be caught by the GCM tag rather than
// yielding partially decrypted data.
func TestOpenRejectsTamperedCiphertext(t *testing.T) {
	kp := newTestKeypair(t)
	env, err := Seal(kp, []byte("secret"))
	if err != nil {
		t.Fatalf("Seal(): %v", err)
	}

	raw, err := decodeBase64ForTest(t, env.Ciphertext)
	if err != nil {
		t.Fatal(err)
	}
	raw[0] ^= 0x01
	env.Ciphertext = encodeBase64ForTest(raw)

	if _, err := kp.Open(env); !errors.Is(err, ErrMalformed) {
		t.Fatalf("Open() error = %v, want ErrMalformed", err)
	}
}

func TestPublicKeyIsUncompressedPoint(t *testing.T) {
	kp := newTestKeypair(t)
	if len(kp.ID()) != 16 {
		t.Fatalf("ID() length = %d, want 16", len(kp.ID()))
	}

	raw, err := decodeBase64ForTest(t, kp.PublicKeyBase64())
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) != pubKeySize {
		t.Fatalf("public key length = %d, want %d", len(raw), pubKeySize)
	}
	if raw[0] != 0x04 {
		t.Fatalf("public key prefix = %#x, want 0x04 (uncompressed point)", raw[0])
	}
}

type innerPayload struct {
	Email    string `json:"email"`
	APIKey   string `json:"llm_api_key"`
	UseOwn   bool   `json:"use_own_keys"`
	ClearKey bool   `json:"clear_llm_key"`
}

func TestOpenIntoReadsSealedBody(t *testing.T) {
	kp := newTestKeypair(t)
	want := innerPayload{Email: "user@example.com", APIKey: "sk-live", UseOwn: true}

	plain, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("Marshal(): %v", err)
	}
	env, err := Seal(kp, plain)
	if err != nil {
		t.Fatalf("Seal(): %v", err)
	}
	body, err := json.Marshal(map[string]any{"sealed": env})
	if err != nil {
		t.Fatalf("Marshal(): %v", err)
	}

	var got innerPayload
	if err := kp.OpenInto(body, &got); err != nil {
		t.Fatalf("OpenInto(): %v", err)
	}
	if got != want {
		t.Fatalf("OpenInto() = %+v, want %+v", got, want)
	}
}

// A body that is not wrapped is read as-is, which is what a stale page or an
// insecure context sends.
func TestOpenIntoReadsPlaintextBody(t *testing.T) {
	kp := newTestKeypair(t)
	want := innerPayload{Email: "user@example.com", APIKey: "sk-live"}

	body, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("Marshal(): %v", err)
	}

	var got innerPayload
	if err := kp.OpenInto(body, &got); err != nil {
		t.Fatalf("OpenInto(): %v", err)
	}
	if got != want {
		t.Fatalf("OpenInto() = %+v, want %+v", got, want)
	}
}

// A null "sealed" value is not an envelope; it is read as ordinary JSON.
func TestOpenIntoTreatsNullSealedAsPlaintext(t *testing.T) {
	kp := newTestKeypair(t)
	want := innerPayload{Email: "user@example.com"}

	var got innerPayload
	if err := kp.OpenInto([]byte(`{"sealed":null,"email":"user@example.com"}`), &got); err != nil {
		t.Fatalf("OpenInto(): %v", err)
	}
	if got.Email != want.Email {
		t.Fatalf("OpenInto() = %+v, want %+v", got, want)
	}
}

func TestOpenIntoRejectsEnvelopeForAnotherKey(t *testing.T) {
	intended := newTestKeypair(t)
	other := newTestKeypair(t)

	env, err := Seal(intended, []byte(`{"email":"user@example.com"}`))
	if err != nil {
		t.Fatalf("Seal(): %v", err)
	}
	body, err := json.Marshal(map[string]any{"sealed": env})
	if err != nil {
		t.Fatalf("Marshal(): %v", err)
	}

	var got innerPayload
	if err := other.OpenInto(body, &got); !errors.Is(err, ErrKeyID) {
		t.Fatalf("OpenInto() error = %v, want ErrKeyID", err)
	}
}

func TestOpenIntoRejectsEmptyBody(t *testing.T) {
	kp := newTestKeypair(t)
	var got innerPayload
	if err := kp.OpenInto(nil, &got); err == nil {
		t.Fatal("OpenInto() error = nil, want an error for an empty body")
	}
}

// The request is still JSON after sealing, so a wrong payload type is reported
// as bad input rather than as a cryptographic failure.
func TestOpenIntoRejectsNonObjectBody(t *testing.T) {
	kp := newTestKeypair(t)
	var got innerPayload
	if err := kp.OpenInto([]byte(`[1,2,3]`), &got); err == nil {
		t.Fatal("OpenInto() error = nil, want an error for a JSON array")
	}
}
