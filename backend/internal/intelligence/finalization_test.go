package intelligence

import (
	"strings"
	"testing"
)

func TestSummaryLanguageName(t *testing.T) {
	cases := map[string]string{
		"":   "English",
		"en": "English",
		"EN": "English",
		"vi": "Vietnamese",
		"VI": "Vietnamese",
		"fr": "English", // unsupported falls back to the default
	}
	for in, want := range cases {
		if got := summaryLanguageName(in); got != want {
			t.Errorf("summaryLanguageName(%q) = %q, want %q", in, got, want)
		}
	}
}

// The output language must reach the full finalization prompt, and the old
// "preserve the speakers' original language" rule must be gone — it directly
// conflicts with writing the MoM in one chosen language.
func TestFinalizationPromptLanguage(t *testing.T) {
	input := FinalizationInput{Title: "Sync"}

	prompt := finalizationPrompt(Glossary{}, input)
	if !strings.Contains(prompt, "Write the MOM output in English") {
		t.Error("default prompt should request English output")
	}
	if strings.Contains(prompt, "Preserve the speakers' original language") {
		t.Error("default prompt must not preserve the source mix instead of the chosen language")
	}

	input.Language = "vi"
	prompt = finalizationPrompt(Glossary{}, input)
	if !strings.Contains(prompt, "Write the MOM output in Vietnamese") {
		t.Error("Vietnamese prompt should request Vietnamese output")
	}
	if !strings.Contains(prompt, `Write EVERY output field in Vietnamese`) {
		t.Error("Vietnamese prompt should constrain every output field to the language")
	}
}

// The chunked map-reduce path (chunk extraction + merge) must carry the same
// language as the single-shot prompt, or a large meeting would mix languages.
func TestChunkAndMergePromptLanguage(t *testing.T) {
	if got := chunkExtractionPrompt("T", Glossary{}, "vi", 1, 2, "evidence"); !strings.Contains(got, "in Vietnamese") {
		t.Error("chunk extraction prompt should request Vietnamese entries")
	}
	if got := chunkExtractionPrompt("T", Glossary{}, "", 1, 1, "evidence"); !strings.Contains(got, "in English") {
		t.Error("chunk extraction prompt should default to English")
	}
	if got := mergePrompt("T", Glossary{}, "vi", nil, nil); !strings.Contains(got, "Write the merged MOM in Vietnamese") {
		t.Error("merge prompt should request Vietnamese output")
	}
	if got := mergePrompt("T", Glossary{}, "en", nil, nil); !strings.Contains(got, "Write the merged MOM in English") {
		t.Error("merge prompt should request English output")
	}
}
