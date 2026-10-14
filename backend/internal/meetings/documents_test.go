package meetings

import (
	"strings"
	"testing"
)

func TestDocumentsText(t *testing.T) {
	stored := []storedDocument{
		{Name: "agenda.txt", Text: "Discuss CR_002"},
		{Name: "notes.md", Text: "Decision: ship Friday"},
	}

	got := documentsText(stored)
	want := "[file: agenda.txt]\nDiscuss CR_002\n\n[file: notes.md]\nDecision: ship Friday"
	if got != want {
		t.Errorf("documentsText() = %q, want %q", got, want)
	}
}

func TestDocumentsTextEmpty(t *testing.T) {
	if got := documentsText(nil); got != "" {
		t.Errorf("documentsText(nil) = %q, want empty", got)
	}
}

func TestDocumentsTextRespectsLLMBudget(t *testing.T) {
	stored := []storedDocument{
		{Name: "a.txt", Text: strings.Repeat("a", maxLLMChars)},
		{Name: "b.txt", Text: "second file must be dropped"},
	}

	got := documentsText(stored)
	if strings.Contains(got, "second file must be dropped") {
		t.Errorf("second file exceeded budget and should be dropped: len=%d", len(got))
	}
	if len([]rune(got)) > maxLLMChars+len("\n[truncated]") {
		t.Errorf("text length %d exceeds budget %d", len([]rune(got)), maxLLMChars)
	}
	if !strings.HasSuffix(got, "[truncated]") {
		t.Errorf("expected truncation marker, tail: %q", got[len(got)-20:])
	}
}
