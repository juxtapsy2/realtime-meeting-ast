// Package sealedbox encrypts sensitive admin request bodies between the
// browser and the backend so that provider API keys never cross the wire in
// the clear.
//
// The browser generates an ephemeral P-256 key pair, agrees on a shared secret
// with the public key published by the backend (ECDH), derives a content
// encryption key with HKDF-SHA256, and wraps the request body in AES-256-GCM.
// Only the backend holds the private half of the key pair, so only the backend
// can read the body.
//
// This is defence in depth, not a replacement for TLS: TLS already protects
// the transport, and an attacker able to run script in the admin page could
// read keys directly. What the seal box guarantees is that the backend's HTTP
// surface - body loggers, proxies, middleware, core dumps - never observes a
// secret in the plaintext form a plain JSON PUT would hand it.
//
// Plaintext bodies are still accepted (see OpenInto) so that a stale page or
// an insecure context, where Web Crypto is unavailable, keeps working. The
// current frontend seals whenever it can.
package sealedbox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"
)

const (
	// Algorithm names the construction; the client refuses to seal with a
	// public key that advertises anything else, so the two sides can evolve
	// independently.
	Algorithm = "ECDH-P256+HKDF-SHA256+AES-256-GCM"

	version    = 1
	infoPrefix = "realtime-meeting-ast:sealbox:v1:"
	keySize    = 32 // AES-256
	nonceSize  = 12 // AES-GCM standard nonce
	pubKeySize = 65 // uncompressed P-256 point
)

// Envelope is the sealed body a client sends in place of plain JSON.
type Envelope struct {
	Version    int    `json:"v"`
	KeyID      string `json:"kid"` // which server key this was sealed to
	Ephemeral  string `json:"epk"` // base64 caller-generated P-256 point
	Nonce      string `json:"n"`   // base64 nonce
	Ciphertext string `json:"ct"`  // base64 ciphertext including the tag
}

var (
	// ErrMalformed reports an envelope that cannot be read: bad base64, a
	// public point that is not a valid P-256 point, a wrong-sized nonce, or a
	// body that does not decrypt or unmarshal.
	ErrMalformed = errors.New("sealedbox: malformed envelope")
	// ErrVersion reports an envelope written by a newer protocol version.
	ErrVersion = errors.New("sealedbox: unsupported envelope version")
	// ErrKeyID reports an envelope addressed to a different server key.
	ErrKeyID = errors.New("sealedbox: envelope is for another key")
)

// Keypair is the backend's seal box key. It is generated per process because
// it only ever needs to live as long as the request it is protecting: the
// public half is published for clients to use and the private half never
// leaves the process.
type Keypair struct {
	priv     *ecdh.PrivateKey
	pub      *ecdh.PublicKey
	pubBytes []byte
	kid      string
}

// NewKeypair generates a fresh P-256 key pair.
func NewKeypair() (*Keypair, error) {
	priv, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("sealedbox: generating key: %w", err)
	}
	pub := priv.PublicKey()
	pubBytes := pub.Bytes()
	sum := sha256.Sum256(pubBytes)
	return &Keypair{
		priv:     priv,
		pub:      pub,
		pubBytes: pubBytes,
		kid:      hex.EncodeToString(sum[:8]),
	}, nil
}

// ID is a stable, non-secret fingerprint of the public key. It lets a client
// address an envelope to this key and lets the server reject an envelope
// meant for a key it no longer holds.
func (k *Keypair) ID() string { return k.kid }

// PublicKeyBase64 is the base64 encoded uncompressed P-256 point that clients
// import as their ECDH peer key.
func (k *Keypair) PublicKeyBase64() string { return base64.StdEncoding.EncodeToString(k.pubBytes) }

// Seal encrypts plaintext for this key pair. It is the reference implementation
// of the client side of the protocol, used by tests; the browser performs the
// same steps with Web Crypto.
func Seal(kp *Keypair, plaintext []byte) (Envelope, error) {
	ephemeral, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return Envelope{}, fmt.Errorf("sealedbox: generating ephemeral key: %w", err)
	}
	shared, err := ephemeral.ECDH(kp.pub)
	if err != nil {
		return Envelope{}, fmt.Errorf("sealedbox: key agreement: %w", err)
	}

	// The envelope carries the caller's ephemeral point; the server recovers
	// the same shared secret from it with its own private key.
	ephB64 := base64.StdEncoding.EncodeToString(ephemeral.PublicKey().Bytes())
	key, err := deriveKey(shared, kp.pubBytes, kp.kid, ephB64)
	if err != nil {
		return Envelope{}, err
	}

	nonce := make([]byte, nonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return Envelope{}, fmt.Errorf("sealedbox: reading nonce: %w", err)
	}
	gcm, err := newGCM(key)
	if err != nil {
		return Envelope{}, err
	}

	return Envelope{
		Version:    version,
		KeyID:      kp.kid,
		Ephemeral:  ephB64,
		Nonce:      base64.StdEncoding.EncodeToString(nonce),
		Ciphertext: base64.StdEncoding.EncodeToString(gcm.Seal(nil, nonce, plaintext, nil)),
	}, nil
}

// Open decrypts an envelope addressed to this key pair.
func (k *Keypair) Open(env Envelope) ([]byte, error) {
	if env.Version != version {
		return nil, fmt.Errorf("%w: got %d, want %d", ErrVersion, env.Version, version)
	}
	if env.KeyID != k.kid {
		return nil, ErrKeyID
	}

	// The ephemeral public point is attacker controlled, so every decode step
	// is validated before it reaches key agreement. Failure details are
	// collapsed into one error: a caller learns only that the body was wrong.
	pub, err := decodePoint(env.Ephemeral)
	if err != nil {
		return nil, err
	}
	nonce, err := base64.StdEncoding.DecodeString(env.Nonce)
	if err != nil || len(nonce) != nonceSize {
		return nil, ErrMalformed
	}
	ct, err := base64.StdEncoding.DecodeString(env.Ciphertext)
	if err != nil || len(ct) == 0 {
		return nil, ErrMalformed
	}

	shared, err := k.priv.ECDH(pub)
	if err != nil {
		return nil, ErrMalformed
	}
	// The key id and the published point bind the derived key to this server
	// key and this envelope, so an envelope cannot be replayed against a
	// different key or have its fields swapped.
	key, err := deriveKey(shared, k.pubBytes, k.kid, env.Ephemeral)
	if err != nil {
		return nil, err
	}
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	plaintext, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return nil, ErrMalformed
	}
	return plaintext, nil
}

// OpenInto decodes a request body that is either a sealed envelope - a JSON
// object of the form {"sealed":{...}} - or plain JSON.
//
// Plaintext stays supported so an older page, or a browser in an insecure
// context where Web Crypto is absent, can still save settings. Sealing is
// therefore opportunistic, and this function never fails because a client did
// not seal.
func (k *Keypair) OpenInto(body []byte, dst any) error {
	if len(body) == 0 {
		return errors.New("sealedbox: empty body")
	}

	var probe struct {
		Sealed json.RawMessage `json:"sealed"`
	}
	// A body that is not an object (an array, a scalar) is not a seal box
	// envelope and not an admin payload; json.Unmarshal reports that.
	if err := json.Unmarshal(body, &probe); err != nil {
		return err
	}
	if sealedBody(probe.Sealed) {
		var env Envelope
		if err := json.Unmarshal(probe.Sealed, &env); err != nil {
			return ErrMalformed
		}
		plaintext, err := k.Open(env)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(plaintext, dst); err != nil {
			return fmt.Errorf("sealedbox: sealed payload is not the expected JSON: %w", err)
		}
		return nil
	}

	noticePlaintext()
	return json.Unmarshal(body, dst)
}

// sealedBody reports whether the "sealed" key carries an envelope rather than
// being absent or null.
func sealedBody(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	trimmed := raw
	for len(trimmed) > 0 && (trimmed[0] == ' ' || trimmed[0] == '\n' || trimmed[0] == '\t' || trimmed[0] == '\r') {
		trimmed = trimmed[1:]
	}
	return len(trimmed) > 0 && string(trimmed) != "null"
}

// deriveKey runs HKDF-SHA256 over the ECDH shared secret. The server's public
// point is the salt and the key id plus the ephemeral point are the context,
// so the same shared secret yields a different key for a different envelope.
func deriveKey(shared, salt []byte, kid, epkB64 string) ([]byte, error) {
	key, err := hkdf.Key(sha256.New, shared, salt, infoPrefix+kid+":"+epkB64, keySize)
	if err != nil {
		return nil, fmt.Errorf("sealedbox: deriving key: %w", err)
	}
	return key, nil
}

// decodePoint parses a base64 encoded uncompressed P-256 point.
func decodePoint(encoded string) (*ecdh.PublicKey, error) {
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, ErrMalformed
	}
	if len(raw) != pubKeySize || raw[0] != 0x04 {
		return nil, ErrMalformed
	}
	pub, err := ecdh.P256().NewPublicKey(raw)
	if err != nil {
		return nil, ErrMalformed
	}
	return pub, nil
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("sealedbox: initialising cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("sealedbox: initialising GCM: %w", err)
	}
	return gcm, nil
}

// plaintextOnce records, once per process, that a client sent an unsealed
// body. It is not an error - older pages and insecure contexts still work -
// but it is the signal that they have all moved over and plaintext acceptance
// could be dropped.
var plaintextOnce sync.Once

func noticePlaintext() {
	plaintextOnce.Do(func() {
		log.Print("SEALBOX: accepted an unsealed admin body; a client is not sealing (stale page or insecure context)")
	})
}
