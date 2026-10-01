package settings

import "testing"

func TestFind(t *testing.T) {
	k, ok := Find("LLM_MODEL")
	if !ok || k.Name != "LLM_MODEL" {
		t.Fatalf("Find(LLM_MODEL) = %+v, %v", k, ok)
	}
	if _, ok := Find("NOPE"); ok {
		t.Fatal("Find(NOPE) reported known key")
	}
}

// API keys must not be settable through app_settings: the table stores values
// in plaintext, so platform keys stay in the environment and user keys live
// encrypted in user_provider_settings.
func TestAPIKeysAreNotSettable(t *testing.T) {
	for _, name := range []string{"STT_API_KEY", "LLM_API_KEY", "ADMIN_EMAILS", "SUPERADMIN_EMAIL"} {
		if _, ok := Find(name); ok {
			t.Errorf("Find(%q) must not resolve: secrets and env-only roles are not app settings", name)
		}
	}
	for _, k := range All {
		if k.Secret {
			t.Errorf("key %q is secret and must not be a runtime setting", k.Name)
		}
	}
}

func TestMaskedValue(t *testing.T) {
	if got := MaskedValue(KeyAllowedEmails, "a@b.com"); got != "a@b.com" {
		t.Fatalf("non-secret should pass through, got %q", got)
	}
	secret := Key{Name: "TEST_SECRET", Secret: true}
	if got := MaskedValue(secret, ""); got != "" {
		t.Fatalf("empty secret should be empty, got %q", got)
	}
	if got := MaskedValue(secret, "abcd"); got != "••••" {
		t.Fatalf("short secret masked as %q, want bullets", got)
	}
	if got := MaskedValue(secret, "gsk_abcdefg1234"); got != "••••1234" {
		t.Fatalf("long secret masked as %q, want tail only", got)
	}
}

func TestStoreEnvBaseline(t *testing.T) {
	t.Setenv("STT_PROVIDER", "google")
	t.Setenv("ALLOWED_EMAILS", "a@b.com")

	s := New(nil)
	if v, ok := s.Get(KeySTTProvider); !ok || v != "google" {
		t.Fatalf("Get(STT_PROVIDER) = %q, %v", v, ok)
	}
	if _, ok := s.Get(KeyLLMProvider); ok {
		t.Fatal("Get(LLM_PROVIDER) reported a value that is not in the environment")
	}
	if got := s.GetDefault(KeyLLMProvider, "openai"); got != "openai" {
		t.Fatalf("GetDefault fallback = %q", got)
	}

	st := s.Statuses()
	if len(st) != len(All) {
		t.Fatalf("Statuses returned %d entries, want %d", len(st), len(All))
	}
}
