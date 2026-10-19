package intelligence

import (
	"regexp"
	"strings"
)

// Filler removal happens only when building LLM prompts; persisted
// transcripts stay byte-identical. Rules are deliberately conservative:
// standalone hesitation tokens only, so content words (museum, umbraco,
// approved, được) are never touched.

var (
	// EN hesitations: the trailing \b keeps the following separator intact,
	// so adjacent fillers ("um, uh, mm") are all removed in one pass.
	enFillerRe = regexp.MustCompile(`(?i)(^|[\s,.;:!?])(um+|uh+|er+|ah+|hmm+|hm+|mm+|uh-?huh|mhm)\b`)

	// VN filler particles. \b is ASCII-only in Go, so the trailing side must
	// be matched as an explicit separator class.
	vnFillerRe = regexp.MustCompile(`(?i)(^|[\s,.;:!?])(ừ+|ờ+|ơ+)([\s,.;:!?]|$)`)

	// Separators left dangling after a removal ("ừ ừ" -> " ").
	strayPunctRe    = regexp.MustCompile(`,\s*([,.;:!?])`)
	lineLeadCommaRe = regexp.MustCompile(`(?m)^,\s*`)

	// A line that after token removal contains only punctuation/space was a
	// filler-only utterance ("Um, uh, mm.").
	fillerOnlyLineRe = regexp.MustCompile(`(?m)^[[:punct:][:space:]]*$`)

	multiSpaceRe = regexp.MustCompile(`[ \t]{2,}`)
	multiNLRe    = regexp.MustCompile(`\n{3,}`)
)

// sanitizePromptText strips filler words and collapses the whitespace they
// leave behind. Typical transcripts lose 5-15% of their tokens; written
// documents lose almost nothing.
func sanitizePromptText(text string) string {
	if text == "" {
		return ""
	}
	prev := text
	for i := 0; i < 10; i++ { // repeat so "ừ ừ ừ" and mixed chains fully collapse
		next := enFillerRe.ReplaceAllString(prev, "$1")
		next = vnFillerRe.ReplaceAllString(next, "$1")
		if next == prev {
			break
		}
		prev = next
	}
	prev = strayPunctRe.ReplaceAllString(prev, "$1")
	prev = lineLeadCommaRe.ReplaceAllString(prev, "")

	lines := strings.Split(prev, "\n")
	kept := lines[:0]
	for _, line := range lines {
		if fillerOnlyLineRe.MatchString(line) && strings.TrimSpace(line) != "" {
			continue
		}
		kept = append(kept, line)
	}
	out := multiSpaceRe.ReplaceAllString(strings.Join(kept, "\n"), " ")
	return strings.TrimSpace(multiNLRe.ReplaceAllString(out, "\n\n"))
}

// chunkEvidenceBytes caps one map-step chunk. At the worst observed token
// density (~2.65 bytes/token for ID-heavy text) this keeps a chunk near
// 4000 tokens, comfortably under Groq's 8000-token single-request cap once
// prompt overhead is added.
const chunkEvidenceBytes = 10 * 1024

// chunkText splits evidence into chunks of at most maxBytes, breaking on
// line boundaries where possible and on spaces for long lines.
func chunkText(text string, maxBytes int) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if len(text) <= maxBytes {
		return []string{text}
	}

	var chunks []string
	var current strings.Builder
	flush := func() {
		if s := strings.TrimSpace(current.String()); s != "" {
			chunks = append(chunks, s)
		}
		current.Reset()
	}

	for _, line := range strings.Split(text, "\n") {
		for len(line) > maxBytes {
			// A single line longer than the budget: cut at the last space.
			cut := strings.LastIndex(line[:maxBytes], " ")
			if cut <= 0 {
				cut = maxBytes
			}
			flush()
			chunks = append(chunks, strings.TrimSpace(line[:cut]))
			line = strings.TrimSpace(line[cut:])
		}
		if current.Len()+len(line)+1 > maxBytes {
			flush()
		}
		if line != "" {
			current.WriteString(line)
			current.WriteByte('\n')
		}
	}
	flush()
	return chunks
}
