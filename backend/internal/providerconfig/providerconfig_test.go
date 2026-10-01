package providerconfig

import (
	"errors"
	"testing"
)

func platformDefaults() Selection {
	return Selection{
		Source:      SourcePlatform,
		STTProvider: "google",
		STTAPIKey:   "platform-stt",
		LLMProvider: "groq",
		LLMAPIKey:   "platform-llm",
		LLMModel:    "openai/gpt-oss-120b",
	}
}

func TestResolveWithoutOwnKeys(t *testing.T) {
	got := resolve(platformDefaults(), false, "deepgram", "own", "openai", "gpt-4o", "own-key")
	want := platformDefaults()
	if got != want {
		t.Fatalf("resolve(own=false) = %+v, want %+v", got, want)
	}
}

func TestResolveOwnKeysWinPerField(t *testing.T) {
	got := resolve(platformDefaults(), true, "deepgram", "own-stt", "", "", "own-llm")
	if got.Source != SourceUser {
		t.Errorf("Source = %q, want %q", got.Source, SourceUser)
	}
	if got.STTProvider != "deepgram" || got.STTAPIKey != "own-stt" {
		t.Errorf("stt not overridden: %+v", got)
	}
	if got.LLMProvider != "groq" {
		t.Errorf("LLMProvider = %q, want platform fallback groq", got.LLMProvider)
	}
	if got.LLMModel != "openai/gpt-oss-120b" {
		t.Errorf("LLMModel = %q, want platform fallback", got.LLMModel)
	}
	if got.LLMAPIKey != "own-llm" {
		t.Errorf("LLMAPIKey = %q, want own-llm", got.LLMAPIKey)
	}
}

func TestResolveOwnKeysWithoutValuesFallBackToPlatform(t *testing.T) {
	got := resolve(platformDefaults(), true, "", "", "", "", "")
	want := platformDefaults()
	want.Source = SourceUser
	if got != want {
		t.Fatalf("resolve(empty own) = %+v, want %+v", got, want)
	}
}

func TestSelectionNeverLeaksAcrossFields(t *testing.T) {
	// A selection is a plain value struct: the platform key is replaced (not
	// appended) when the user supplies one, so two keys cannot be combined into
	// an invalid credential.
	got := resolve(platformDefaults(), true, "deepgram", "own", "groq", "own-model", "own-key")
	if got.STTAPIKey != "own" || got.LLMAPIKey != "own-key" {
		t.Fatalf("own keys not applied: %+v", got)
	}
	if got.STTProvider == "google" || got.LLMProvider != "groq" {
		t.Fatalf("providers not applied: %+v", got)
	}
}

func TestOwnKeysNeedKeyIsValidationError(t *testing.T) {
	// The sentinel exists so the HTTP layer can return 400 instead of 500, and
	// so callers can distinguish it from a storage failure.
	if !errors.Is(ErrOwnKeysNeedKey, ErrOwnKeysNeedKey) {
		t.Fatal("sentinel error is not usable with errors.Is")
	}
	if errors.Is(ErrOwnKeysNeedKey, ErrNoEncryptionKey) {
		t.Fatal("validation error must not be confused with a missing encryption key")
	}
}
