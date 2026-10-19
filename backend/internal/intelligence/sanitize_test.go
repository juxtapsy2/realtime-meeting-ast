package intelligence

import (
	"strings"
	"testing"
)

func TestSanitizePromptTextEN(t *testing.T) {
	in := "Hello um, uh everyone. Um um we approved CR_002 and hmm the test plan."
	out := sanitizePromptText(in)
	for _, filler := range []string{"um", "uh", "hmm"} {
		if strings.Contains(strings.ToLower(out), filler) {
			t.Errorf("filler %q still present in %q", filler, out)
		}
	}
	if !strings.Contains(out, "approved CR_002") || !strings.Contains(out, "test plan") {
		t.Errorf("content lost: %q", out)
	}
}

func TestSanitizePromptTextVN(t *testing.T) {
	in := "Ừ, vâng ừ ờ bàn về PADM2-158069. Ừ ừ được duyệt."
	out := sanitizePromptText(in)
	for _, filler := range []string{"ừ", "ờ"} {
		if strings.Contains(out, filler) {
			t.Errorf("VN filler %q still present in %q", filler, out)
		}
	}
	if !strings.Contains(out, "PADM2-158069") {
		t.Errorf("identifier lost: %q", out)
	}
	if !strings.Contains(out, "được duyệt") {
		t.Errorf("meaning word lost: %q", out)
	}
}

func TestSanitizePromptTextKeepsContentWords(t *testing.T) {
	in := "The museum and umbraco ahoy demo was approved được."
	out := sanitizePromptText(in)
	if out != in {
		t.Errorf("got %q, want unchanged %q", out, in)
	}
}

func TestSanitizePromptTextDropsFillerOnlyLines(t *testing.T) {
	in := "um\nWe agreed.\nUh, mm, ừ ờ."
	out := sanitizePromptText(in)
	if strings.Contains(out, "um") || strings.Contains(out, "Uh") {
		t.Errorf("filler lines kept: %q", out)
	}
	if !strings.Contains(out, "We agreed.") {
		t.Errorf("content line lost: %q", out)
	}
}

func TestChunkText(t *testing.T) {
	if got := chunkText("short", 100); len(got) != 1 || got[0] != "short" {
		t.Errorf("short text chunking = %v", got)
	}

	var lines []string
	for i := 0; i < 50; i++ {
		lines = append(lines, strings.Repeat("x", 40))
	}
	text := strings.Join(lines, "\n")
	chunks := chunkText(text, 500)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}
	joined := strings.Join(chunks, "\n")
	if joined != text {
		t.Errorf("chunking lost text: got %d bytes, want %d", len(joined), len(text))
	}
	for i, c := range chunks {
		if len(c) > 500 {
			t.Errorf("chunk %d length %d exceeds max 500", i, len(c))
		}
	}
}

func TestChunkTextSplitsLongLines(t *testing.T) {
	text := strings.Repeat("word ", 400) // one line, ~2000 bytes
	chunks := chunkText(text, 300)
	if len(chunks) < 5 {
		t.Fatalf("expected long line to split into many chunks, got %d", len(chunks))
	}
	for i, c := range chunks {
		if len(c) > 300 {
			t.Errorf("chunk %d length %d exceeds max 300", i, len(c))
		}
	}
}

func TestFinalizationPromptSanitizesEvidence(t *testing.T) {
	prompt := finalizationPrompt(Glossary{}, FinalizationInput{
		Title:          "Sync",
		FullTranscript: "Um, we approved CR_002. Hmm, ừ ờ done.",
		DocumentText:   "Doc: um CR_003 pending",
	})
	for _, leftover := range []string{"Um", "Hmm", "ừ ờ done", "um CR_003"} {
		if strings.Contains(prompt, leftover) {
			t.Errorf("filler %q leaked into prompt", leftover)
		}
	}
	if !strings.Contains(prompt, "approved CR_002") {
		t.Errorf("content missing from prompt")
	}
	if !strings.Contains(prompt, "Ignore filler words") {
		t.Errorf("missing hesitation instruction in prompt")
	}
}
