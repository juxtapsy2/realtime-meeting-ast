package providerconfig

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestValidateComboName(t *testing.T) {
	cases := map[string]bool{
		"":                       false,
		"   ":                    false,
		strings.Repeat("x", 101): false,
		strings.Repeat("x", 100): true,
		"  Gemini setup  ":       true,
		"Gemini setup":           true,
	}
	for name, wantOK := range cases {
		err := ValidateComboName(name)
		if wantOK && err != nil {
			t.Errorf("ValidateComboName(%q) = %v, want nil", name, err)
		}
		if !wantOK && !errors.Is(err, ErrComboName) {
			t.Errorf("ValidateComboName(%q) = %v, want ErrComboName", name, err)
		}
	}
}

// The active state is derived, not stored: system while own keys are off,
// the alphabetically first identical combo when one matches, custom otherwise.
func TestDeriveActiveCombo(t *testing.T) {
	settings := UserSettings{
		UseOwnKeys:  true,
		STTProvider: "deepgram", STTAPIKeyEnc: "stt_blob",
		LLMProvider: "gemini", LLMModel: "gemini-2.5-pro", LLMAPIKeyEnc: "llm_blob",
	}
	same := Combo{
		Name: "A", STTProvider: "deepgram", STTAPIKeyEnc: "stt_blob",
		LLMProvider: "gemini", LLMModel: "gemini-2.5-pro", LLMAPIKeyEnc: "llm_blob",
	}
	other := Combo{Name: "B", STTProvider: "groq", STTAPIKeyEnc: "other_stt", LLMProvider: "groq"}

	cases := []struct {
		name     string
		settings UserSettings
		combos   []Combo
		wantKind string
		wantName string
	}{
		{"system when own keys are off", UserSettings{UseOwnKeys: false}, []Combo{same}, ActiveKindSystem, ""},
		{"system with no row and no combos", UserSettings{}, nil, ActiveKindSystem, ""},
		{"custom when own keys are on with no combos", settings, nil, ActiveKindCustom, ""},
		{"custom when nothing matches", settings, []Combo{other}, ActiveKindCustom, ""},
		{"combo when fields match", settings, []Combo{other, same}, ActiveKindCombo, "A"},
		{
			"alphabetically first of several identical combos",
			settings,
			[]Combo{{Name: "Z", STTProvider: "deepgram", STTAPIKeyEnc: "stt_blob", LLMProvider: "gemini", LLMModel: "gemini-2.5-pro", LLMAPIKeyEnc: "llm_blob"}, same},
			ActiveKindCombo, "A",
		},
	}
	for _, tc := range cases {
		kind, name := deriveActiveCombo(tc.settings, tc.combos)
		if kind != tc.wantKind || name != tc.wantName {
			t.Errorf("%s: deriveActiveCombo = (%q, %q), want (%q, %q)",
				tc.name, kind, name, tc.wantKind, tc.wantName)
		}
	}
}

// Name validation must run before any store access, so a bad name is a 400
// even when the database is unreachable (and in tests, absent entirely).
func TestComboWritesValidateNameBeforeTouchingStore(t *testing.T) {
	ctx := context.Background()
	loaderMustNotRun := &Store{loader: fakeLoader{err: errors.New("loader must not be called")}}
	noDB := &Store{}

	if err := loaderMustNotRun.SaveCombo(ctx, "a@example.com", "   "); !errors.Is(err, ErrComboName) {
		t.Errorf("SaveCombo with blank name = %v, want ErrComboName", err)
	}
	if err := noDB.ApplyCombo(ctx, "a@example.com", "", "u@example.com"); !errors.Is(err, ErrComboName) {
		t.Errorf("ApplyCombo with blank name = %v, want ErrComboName", err)
	}
	if err := noDB.DeleteCombo(ctx, "a@example.com", strings.Repeat("n", 101)); !errors.Is(err, ErrComboName) {
		t.Errorf("DeleteCombo with 101-char name = %v, want ErrComboName", err)
	}
	if _, err := noDB.ListCombos(ctx, "  "); err == nil {
		t.Error("ListCombos with empty email must fail before touching the database")
	}
}

// A combo always holds at least one key: without one it could never be
// applied as own-keys configuration, so snapshotting must refuse instead of
// saving an unusable combo.
func TestSaveComboRequiresStoredSourceWithKey(t *testing.T) {
	ctx := context.Background()

	s := &Store{loader: fakeLoader{settings: map[string]UserSettings{}}}
	if err := s.SaveCombo(ctx, "a@example.com", "setup"); !errors.Is(err, ErrComboNoSource) {
		t.Errorf("SaveCombo with no stored row = %v, want ErrComboNoSource", err)
	}

	s = &Store{loader: fakeLoader{settings: map[string]UserSettings{
		"a@example.com": {Email: "a@example.com", UseOwnKeys: true, LLMProvider: "groq"},
	}}}
	if err := s.SaveCombo(ctx, "a@example.com", "setup"); !errors.Is(err, ErrComboNoSource) {
		t.Errorf("SaveCombo with no stored key = %v, want ErrComboNoSource", err)
	}

	boom := errors.New("db down")
	s = &Store{loader: fakeLoader{err: boom}}
	if err := s.SaveCombo(ctx, "a@example.com", "setup"); !errors.Is(err, boom) {
		t.Errorf("SaveCombo storage failure = %v, want %v", err, boom)
	}
}
