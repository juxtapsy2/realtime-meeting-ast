// Package secretbox encrypts short secrets (per-user provider API keys) at
// rest with AES-256-GCM.
//
// The key is derived from the application's HMAC secret so that stored keys
// are unreadable from the database alone: an attacker with a database dump
// still needs AUTH_HMAC_SECRET to recover them. Every ciphertext is bound to a
// field label as additional authenticated data, so a ciphertext cannot be
// moved between fields (e.g. an STT key replayed as an LLM key).
package secretbox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
)

// keyInfo domain-separates this key derivation from any other use of the
// HMAC secret.
const keyInfo = "meeting-ast/secretbox/v1"

// ErrNoKey is returned when no HMAC secret is configured. Without a stable
// secret, encrypted values would be unrecoverable after a restart, so callers
// must refuse to store secrets rather than write unreadable data.
var ErrNoKey = errors.New("secretbox: no encryption secret configured")

// ErrMalformed is returned for ciphertexts that are not valid base64/GCM.
var ErrMalformed = errors.New("secretbox: malformed ciphertext")

// Box seals and opens secrets with a key derived from hmacSecret.
type Box struct {
	aead cipher.AEAD
}

// New derives an AES-256-GCM key from hmacSecret. An empty secret is an error.
func New(hmacSecret string) (*Box, error) {
	if hmacSecret == "" {
		return nil, ErrNoKey
	}
	sum := sha256.Sum256([]byte(keyInfo + "\x00" + hmacSecret))
	block, err := aes.NewCipher(sum[:])
	if err != nil {
		return nil, fmt.Errorf("secretbox: init cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("secretbox: init gcm: %w", err)
	}
	return &Box{aead: aead}, nil
}

// Seal encrypts plaintext and returns a base64url-encoded nonce+ciphertext.
// The field label is authenticated but not stored, so Open needs the same one.
func (b *Box) Seal(field, plaintext string) (string, error) {
	if b == nil || b.aead == nil {
		return "", ErrNoKey
	}
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("secretbox: nonce: %w", err)
	}
	sealed := b.aead.Seal(nonce, nonce, []byte(plaintext), []byte(field))
	return base64.RawURLEncoding.EncodeToString(sealed), nil
}

// Open decrypts a value produced by Seal. It fails if the ciphertext was
// tampered with, was sealed under a different secret, or belongs to another
// field.
func (b *Box) Open(field, blob string) (string, error) {
	if b == nil || b.aead == nil {
		return "", ErrNoKey
	}
	raw, err := base64.RawURLEncoding.DecodeString(blob)
	if err != nil {
		return "", ErrMalformed
	}
	nonceSize := b.aead.NonceSize()
	if len(raw) < nonceSize+b.aead.Overhead() {
		return "", ErrMalformed
	}
	nonce, ciphertext := raw[:nonceSize], raw[nonceSize:]
	plaintext, err := b.aead.Open(nil, nonce, ciphertext, []byte(field))
	if err != nil {
		return "", ErrMalformed
	}
	return string(plaintext), nil
}
