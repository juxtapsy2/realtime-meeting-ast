package transcription

import "strings"

// PhraseHint biases the recognizer toward a specific word or phrase. Boost is
// between 0 (exclusive) and 20; higher values increase recognition probability
// at the cost of more false positives.
type PhraseHint struct {
	Phrase string
	Boost  float32
}

// NormalizationRule replaces a literal transcript substring with the canonical
// form. This is a post-recognition rewrite applied by the recognizer (stable
// partials and finals only), used to clean up known mishearings such as
// "positive" -> "POSM". Keep search strings unambiguous to avoid rewriting
// ordinary words.
type NormalizationRule struct {
	Search  string
	Replace string
}

// Vocabulary carries domain terminology injected into speech recognition via
// phrase biasing (SpeechAdaptation) and exact rewrites (TranscriptNormalization).
type Vocabulary struct {
	Hints          []PhraseHint
	Normalizations []NormalizationRule
}

// VocabularyTerm is the normalization-neutral description of one regulated
// term. The mapping from glossary to VocabularyTerm happens at the composition
// root so the transcription package does not depend on the intelligence package.
type VocabularyTerm struct {
	Term      string
	Expansion string
	// STTHints are extra spellings to bias toward (e.g. exact identifiers like
	// "PADM2-158069"). They do not get normalized.
	STTHints []string
	// STTNormalize lists transcript forms to rewrite to Term (e.g. mishearings
	// of "POSM"). Opt-in and curated: only add unambiguous forms.
	STTNormalize []string
}

const (
	// boostIdentifier is for exact business identifiers (digits, hyphens) that
	// the model should almost always transcribe verbatim.
	boostIdentifier float32 = 20
	// boostAcronym is for all-caps code-like terms such as POSM, SIT, CCVN.
	boostAcronym float32 = 18
	// boostTerm is for proper nouns, vendor names and verbose terms.
	boostTerm float32 = 15
	// boostExpansion is a mild bias for the spelled-out expansion phrases.
	boostExpansion float32 = 10
	// maxNormalizationEntries is the recognizer limit for replacement entries.
	maxNormalizationEntries = 100
)

// NewVocabulary builds the STT vocabulary from regulated terms.
//
// Hints: the term itself, any explicit STT hints, and the spelled-out expansion
// (when it differs from the term). Boost tiers prefer exact identifiers and
// short all-caps acronyms, which are the highest-value, lowest-false-positive
// hints.
//
// Normalizations: only the explicit, curated STTNormalize list is used. Aliases
// are deliberately NOT auto-converted because normalization performs literal
// substring replacement, so short or common words (e.g. "CA", "minutes") would
// corrupt unrelated transcript text.
func NewVocabulary(terms []VocabularyTerm) Vocabulary {
	var v Vocabulary

	for _, t := range terms {
		term := strings.TrimSpace(t.Term)
		if term == "" {
			continue
		}

		boost := boostTerm
		switch {
		case containsDigit(term):
			boost = boostIdentifier
		case isAcronym(term):
			boost = boostAcronym
		}

		v.Hints = append(v.Hints, PhraseHint{Phrase: term, Boost: boost})

		for _, h := range t.STTHints {
			h = strings.TrimSpace(h)
			if h == "" {
				continue
			}
			b := boostTerm
			switch {
			case containsDigit(h):
				b = boostIdentifier
			case isAcronym(h):
				b = boostAcronym
			}
			v.Hints = append(v.Hints, PhraseHint{Phrase: h, Boost: b})
		}

		expansion := strings.TrimSpace(t.Expansion)
		if expansion != "" && !strings.EqualFold(expansion, term) {
			v.Hints = append(v.Hints, PhraseHint{Phrase: expansion, Boost: boostExpansion})
		}

		for _, n := range t.STTNormalize {
			n = strings.TrimSpace(n)
			if n == "" || strings.EqualFold(n, term) {
				continue
			}
			v.Normalizations = append(v.Normalizations, NormalizationRule{Search: n, Replace: term})
		}
	}

	v.Hints = dedupHints(v.Hints)
	v.Normalizations = dedupRules(v.Normalizations)
	if len(v.Normalizations) > maxNormalizationEntries {
		v.Normalizations = v.Normalizations[:maxNormalizationEntries]
	}
	return v
}

func isAcronym(s string) bool {
	if len(s) == 0 || len(s) > 8 {
		return false
	}
	for _, r := range s {
		if r < 'A' || r > 'Z' {
			return false
		}
	}
	return true
}

func containsDigit(s string) bool {
	return strings.ContainsAny(s, "0123456789")
}

func dedupHints(in []PhraseHint) []PhraseHint {
	idx := make(map[string]int, len(in))
	out := make([]PhraseHint, 0, len(in))
	for _, h := range in {
		key := strings.ToLower(strings.TrimSpace(h.Phrase))
		if key == "" {
			continue
		}
		if i, ok := idx[key]; ok {
			// Keep the strongest boost: an expansion hint (low) must not
			// mask a term hint (high) that spells the same phrase.
			if h.Boost > out[i].Boost {
				out[i].Boost = h.Boost
			}
			continue
		}
		idx[key] = len(out)
		out = append(out, h)
	}
	return out
}

func dedupRules(in []NormalizationRule) []NormalizationRule {
	seen := make(map[string]bool, len(in))
	out := make([]NormalizationRule, 0, len(in))
	for _, r := range in {
		key := strings.ToLower(strings.TrimSpace(r.Search))
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, r)
	}
	return out
}
