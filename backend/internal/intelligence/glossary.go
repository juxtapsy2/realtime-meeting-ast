package intelligence

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

//go:embed glossary.json
var defaultGlossaryData []byte

// GlossaryTerm is a regulated business/technical term the LLM should use when
// writing summaries so it does not misread or mangle domain vocabulary.
type GlossaryTerm struct {
	Term        string   `json:"term"`
	Aliases     []string `json:"aliases,omitempty"`
	Expansion   string   `json:"expansion,omitempty"`
	Description string   `json:"description,omitempty"`
	// STTHints are exact spellings to bias speech recognition toward (e.g.
	// identifiers like "PADM2-158069", vendor names pronounced unusually).
	STTHints []string `json:"stt_hints,omitempty"`
	// STTNormalize are transcript forms to rewrite to Term after recognition
	// (e.g. mishearings like "positive" -> "POSM"). Curated and unambiguous.
	STTNormalize []string `json:"stt_normalize,omitempty"`
}

// Glossary is the full set of regulated terms loaded at startup.
type Glossary struct {
	Terms []GlossaryTerm `json:"terms"`
}

// LoadGlossary reads a glossary JSON file. When path is empty, the embedded
// default glossary is used.
func LoadGlossary(path string) (Glossary, error) {
	var data []byte
	if path != "" {
		b, err := os.ReadFile(path)
		if err != nil {
			return Glossary{}, fmt.Errorf("read glossary %q: %w", path, err)
		}
		data = b
	} else {
		data = defaultGlossaryData
	}

	var g Glossary
	if err := json.Unmarshal(data, &g); err != nil {
		return Glossary{}, fmt.Errorf("parse glossary: %w", err)
	}
	return g, nil
}

// PromptSection renders the glossary as an instruction block for LLM prompts.
func (g Glossary) PromptSection() string {
	if len(g.Terms) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("Business terminology glossary - use these canonical forms when writing. Never guess or invent different spellings:\n")
	for _, t := range g.Terms {
		b.WriteString("- " + t.Term)
		if t.Expansion != "" {
			b.WriteString(" (" + t.Expansion + ")")
		}
		if len(t.Aliases) > 0 {
			b.WriteString("; aliases: " + strings.Join(t.Aliases, ", "))
		}
		if t.Description != "" {
			b.WriteString(" - " + t.Description)
		}
		b.WriteString("\n")
	}
	return b.String()
}
