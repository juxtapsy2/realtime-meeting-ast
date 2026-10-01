package providerconfig

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/user/realtime-meeting-ast/backend/internal/secretbox"
)

type fakeLoader struct {
	settings map[string]UserSettings
	err      error
}

func (f fakeLoader) load(_ context.Context, email string) (UserSettings, error) {
	if f.err != nil {
		return UserSettings{}, f.err
	}
	us, ok := f.settings[email]
	if !ok {
		return UserSettings{}, sql.ErrNoRows
	}
	return us, nil
}

func newTestStore(t *testing.T, loader settingsLoader) *Store {
	t.Helper()
	box, err := secretbox.New("test-hmac-secret")
	if err != nil {
		t.Fatalf("secretbox.New: %v", err)
	}
	s := &Store{box: box, loader: loader}
	s.SetPlatform(platformDefaults())
	return s
}

// A user with no stored row must resolve to the platform defaults with NO
// error. Returning an error here would break transcription and summarization
// for every user who has not configured their own keys.
func TestForUnknownUserUsesPlatformWithoutError(t *testing.T) {
	s := newTestStore(t, fakeLoader{settings: map[string]UserSettings{}})

	for _, email := range []string{"nobody@example.com", "  Nobody@Example.com  "} {
		sel, err := s.For(context.Background(), email)
		if err != nil {
			t.Fatalf("For(%q) returned error: %v", email, err)
		}
		if sel != platformDefaults() {
			t.Fatalf("For(%q) = %+v, want platform defaults", email, sel)
		}
	}
}

func TestForEmptyEmailUsesPlatform(t *testing.T) {
	s := newTestStore(t, fakeLoader{err: errors.New("loader must not be called")})
	sel, err := s.For(context.Background(), "   ")
	if err != nil {
		t.Fatalf("For(\"\") = %v", err)
	}
	if sel != platformDefaults() {
		t.Fatalf("For(\"\") = %+v", sel)
	}
}

func TestForDisabledOwnKeysUsesPlatform(t *testing.T) {
	s := newTestStore(t, fakeLoader{settings: map[string]UserSettings{
		"a@example.com": {Email: "a@example.com", UseOwnKeys: false, LLMProvider: "openai"},
	}})
	sel, err := s.For(context.Background(), "a@example.com")
	if err != nil {
		t.Fatalf("For: %v", err)
	}
	if sel.Source != SourcePlatform || sel.LLMProvider != "groq" {
		t.Fatalf("own keys disabled but selection = %+v", sel)
	}
}

func TestForOwnKeysDecryptsStoredKeys(t *testing.T) {
	box, err := secretbox.New("test-hmac-secret")
	if err != nil {
		t.Fatalf("secretbox.New: %v", err)
	}
	stt, err := box.Seal(fieldSTTAPIKey, "dg_own_key")
	if err != nil {
		t.Fatalf("Seal stt: %v", err)
	}
	llm, err := box.Seal(fieldLLMAPIKey, "sk_own_key")
	if err != nil {
		t.Fatalf("Seal llm: %v", err)
	}

	s := newTestStore(t, fakeLoader{settings: map[string]UserSettings{
		"a@example.com": {
			Email: "a@example.com", UseOwnKeys: true,
			STTProvider: "deepgram", STTAPIKeyEnc: stt,
			LLMProvider: "openai", LLMModel: "gpt-4o-mini", LLMAPIKeyEnc: llm,
		},
	}})

	sel, err := s.For(context.Background(), "a@example.com")
	if err != nil {
		t.Fatalf("For: %v", err)
	}
	if sel.Source != SourceUser {
		t.Errorf("Source = %q, want %q", sel.Source, SourceUser)
	}
	if sel.STTProvider != "deepgram" || sel.STTAPIKey != "dg_own_key" {
		t.Errorf("stt = %+v", sel)
	}
	if sel.LLMProvider != "openai" || sel.LLMModel != "gpt-4o-mini" || sel.LLMAPIKey != "sk_own_key" {
		t.Errorf("llm = %+v", sel)
	}
}

// A key sealed under a different secret cannot be decrypted: resolution must
// fail rather than fall back to the platform account, which would bill the
// wrong key and hide the misconfiguration.
func TestForUndecryptableKeyFailsInsteadOfFallingBack(t *testing.T) {
	other, err := secretbox.New("a-different-secret")
	if err != nil {
		t.Fatalf("secretbox.New: %v", err)
	}
	blob, err := other.Seal(fieldLLMAPIKey, "sk_someone_elses_key")
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}

	s := newTestStore(t, fakeLoader{settings: map[string]UserSettings{
		"a@example.com": {Email: "a@example.com", UseOwnKeys: true, LLMAPIKeyEnc: blob},
	}})

	sel, err := s.For(context.Background(), "a@example.com")
	if err == nil {
		t.Fatalf("For must fail for an undecryptable key, got %+v", sel)
	}
	if sel.LLMAPIKey != "" {
		t.Fatal("failed resolution must not return any key material")
	}
}

func TestForSurfacesStorageFailure(t *testing.T) {
	boom := errors.New("db down")
	s := newTestStore(t, fakeLoader{err: boom})
	if _, err := s.For(context.Background(), "a@example.com"); !errors.Is(err, boom) {
		t.Fatalf("For error = %v, want %v", err, boom)
	}
}

// When the superadmin changes a platform provider or model, resolution must
// use the new value on the next call without restarting the process, and the
// value must be copied rather than aliased.
func TestSetPlatformAppliesToNextResolution(t *testing.T) {
	s := newTestStore(t, fakeLoader{err: sql.ErrNoRows})

	updated := platformDefaults()
	updated.STTProvider = "deepgram"
	updated.LLMModel = "llama-3.3-70b"
	s.SetPlatform(updated)
	updated.LLMModel = "mutated-after-set"

	sel, err := s.For(context.Background(), "nobody@example.com")
	if err != nil {
		t.Fatalf("For: %v", err)
	}
	if sel.STTProvider != "deepgram" {
		t.Errorf("STTProvider = %q, want deepgram", sel.STTProvider)
	}
	if sel.LLMModel != "llama-3.3-70b" {
		t.Errorf("LLMModel = %q, want llama-3.3-70b", sel.LLMModel)
	}
	if sel.Source != SourcePlatform {
		t.Errorf("Source = %q, want %q", sel.Source, SourcePlatform)
	}
}
