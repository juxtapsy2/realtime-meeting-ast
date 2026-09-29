package transcription

import (
	"strings"
	"testing"
)

func TestNewVocabularyHintBoostTiers(t *testing.T) {
	vocab := NewVocabulary([]VocabularyTerm{
		{Term: "PADM2-158069"},
		{Term: "POSM", Expansion: "Point of Sale Material"},
		{Term: "Infosys"},
	})

	hints := map[string]float32{}
	for _, h := range vocab.Hints {
		hints[h.Phrase] = h.Boost
	}

	if got := hints["PADM2-158069"]; got != boostIdentifier {
		t.Fatalf("identifier boost = %v, want %v", got, boostIdentifier)
	}
	if got := hints["POSM"]; got != boostAcronym {
		t.Fatalf("acronym boost = %v, want %v", got, boostAcronym)
	}
	if got := hints["Infosys"]; got != boostTerm {
		t.Fatalf("proper noun boost = %v, want %v", got, boostTerm)
	}
	if got := hints["Point of Sale Material"]; got != boostExpansion {
		t.Fatalf("expansion boost = %v, want %v", got, boostExpansion)
	}
}

func TestNewVocabularyExpansionEqualsTerm(t *testing.T) {
	// "Solar" has expansion "Solar": must not create a duplicate hint.
	vocab := NewVocabulary([]VocabularyTerm{{Term: "Solar", Expansion: "Solar"}})
	if len(vocab.Hints) != 1 {
		t.Fatalf("expected 1 hint, got %d: %+v", len(vocab.Hints), vocab.Hints)
	}
}

func TestNewVocabularyHintBoostOverrideFromSTTHints(t *testing.T) {
	vocab := NewVocabulary([]VocabularyTerm{
		{Term: "Vendor", STTHints: []string{"Vxceed", "PADM2-158069"}},
	})
	byPhrase := map[string]float32{}
	for _, h := range vocab.Hints {
		byPhrase[h.Phrase] = h.Boost
	}
	if got := byPhrase["Vxceed"]; got != boostTerm {
		t.Fatalf("stt hint proper noun boost = %v, want %v", got, boostTerm)
	}
	if got := byPhrase["PADM2-158069"]; got != boostIdentifier {
		t.Fatalf("stt hint identifier boost = %v, want %v", got, boostIdentifier)
	}
}

func TestNewVocabularyNormalizationsOnlyCurated(t *testing.T) {
	// Only STTNormalize produces rewrites. A term with no STTNormalize list must
	// not generate any normalization, even if it has an expansion phrase.
	vocab := NewVocabulary([]VocabularyTerm{
		{
			Term:         "POSM",
			Expansion:    "Point of Sale Material",
			STTNormalize: []string{"positive", "padsam", "POSM"},
		},
		{
			Term:      "CR",
			Expansion: "Change Request",
		},
	})

	var searches []string
	for _, r := range vocab.Normalizations {
		searches = append(searches, r.Search)
	}
	joined := strings.Join(searches, "|")
	for _, want := range []string{"positive", "padsam"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing normalization %q, got %v", want, searches)
		}
	}
	if strings.Contains(joined, "Change Request") {
		t.Fatalf("expansion auto-normalized: %v", searches)
	}
	// Identity (POSM -> POSM) must be skipped, and no replacement points to POSM
	// from a non-curated source.
	for _, r := range vocab.Normalizations {
		if r.Replace != "POSM" {
			t.Fatalf("unexpected replace target %q -> %q", r.Search, r.Replace)
		}
		if strings.EqualFold(r.Search, r.Replace) {
			t.Fatalf("identity normalization not skipped: %q", r.Search)
		}
	}
}

func TestNewVocabularyDedupKeepsStrongestBoost(t *testing.T) {
	// "Coke" (term, boost 15) precedes "Coca-Cola"; the latter's expansion of
	// "Coca-Cola" (boost 10) must not mask this term's hint at boost 15.
	vocab := NewVocabulary([]VocabularyTerm{
		{Term: "Coca-Cola", Expansion: "Coca-Cola", STTHints: []string{"coca-cola"}},
		{Term: "Coke"},
	})
	byPhrase := map[string]float32{}
	for _, h := range vocab.Hints {
		byPhrase[strings.ToLower(h.Phrase)] = h.Boost
	}
	if got := byPhrase["coca-cola"]; got != boostTerm {
		t.Fatalf("coca-cola boost = %v, want %v", got, boostTerm)
	}
	if got := byPhrase["coke"]; got != boostTerm {
		t.Fatalf("coke boost = %v, want %v", got, boostTerm)
	}
}

func TestNewVocabularyNormalizationCap(t *testing.T) {
	terms := make([]VocabularyTerm, 0, 60)
	for i := 0; i < 60; i++ {
		terms = append(terms, VocabularyTerm{
			Term:         "Term" + string(rune('A'+i)),
			STTNormalize: []string{"misheard-a", "misheard-b"},
		})
	}
	vocab := NewVocabulary(terms)
	if len(vocab.Normalizations) > maxNormalizationEntries {
		t.Fatalf("normalizations %d exceed cap %d", len(vocab.Normalizations), maxNormalizationEntries)
	}
}

func TestNewVocabularyEmpty(t *testing.T) {
	vocab := NewVocabulary(nil)
	if len(vocab.Hints) != 0 || len(vocab.Normalizations) != 0 {
		t.Fatalf("expected empty vocabulary, got %+v", vocab)
	}
}
