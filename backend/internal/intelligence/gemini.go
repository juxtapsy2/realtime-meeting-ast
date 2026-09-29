package intelligence

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
)

// GeminiIntelligence calls the Gemini developer API (generativelanguage) using
// its OpenAI-compatible chat completions endpoint. It is a provider of
// IntelligenceProvider so business logic stays provider-agnostic.
type GeminiIntelligence struct {
	apiKey   string
	model    string
	client   *http.Client
	glossary Glossary
}

func NewGeminiIntelligence(apiKey string, glossary Glossary) (*GeminiIntelligence, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("Gemini API key is required")
	}

	model := os.Getenv("LLM_MODEL")
	if model == "" {
		model = "gemini-flash-latest"
	}

	return &GeminiIntelligence{
		apiKey:   apiKey,
		model:    model,
		client:   &http.Client{Timeout: 90 * time.Second},
		glossary: glossary,
	}, nil
}

func (g *GeminiIntelligence) AnalyzeChunk(ctx context.Context, input AnalysisInput) (MeetingStatePatch, error) {
	prompt := g.buildAnalysisPrompt(input)

	response, err := g.callLLM(ctx, prompt)
	if err != nil {
		return MeetingStatePatch{}, fmt.Errorf("failed to call LLM: %w", err)
	}

	patch, err := g.parseAnalysisResponse(response)
	if err != nil {
		return MeetingStatePatch{}, fmt.Errorf("failed to parse response: %w", err)
	}

	return patch, nil
}

func (g *GeminiIntelligence) FinalizeMeeting(ctx context.Context, input FinalizationInput) (MeetingSummary, error) {
	prompt := g.buildFinalizationPrompt(input)

	response, err := g.callLLM(ctx, prompt)
	if err != nil {
		return MeetingSummary{}, fmt.Errorf("failed to call LLM: %w", err)
	}

	summary, err := g.parseFinalizationResponse(response)
	if err != nil {
		return MeetingSummary{}, fmt.Errorf("failed to parse response: %w", err)
	}

	return summary, nil
}

func (g *GeminiIntelligence) buildAnalysisPrompt(input AnalysisInput) string {
	stateJSON, _ := json.MarshalIndent(input.CurrentState, "", "  ")

	return fmt.Sprintf(`You are an AI meeting assistant. Analyze the following transcript chunk and update the meeting state.

Current Meeting State:
%s

Recent Context:
%s

New Transcript Chunk:
%s

Analyze the transcript and identify:
1. Current topic being discussed
2. Any decisions made (with confidence level 0-1)
3. Any action items mentioned (with assignee if specified)
4. Any issues raised
5. Any open questions

Return a JSON patch with only the NEW or UPDATED items. Do not repeat existing items unless they have changed.

Format your response as JSON:
{
  "current_topic": "string or null if unchanged",
  "topics": [{"id": "uuid", "title": "string", "keywords": ["string"]}],
  "decisions": [{"id": "uuid", "title": "string", "description": "string", "status": "proposed|confirmed", "confidence": 0.0-1.0, "source_segment_ids": ["string"]}],
  "action_items": [{"id": "uuid", "description": "string", "assignee": "string", "status": "pending", "source_segment_ids": ["string"]}],
  "issues": [{"id": "uuid", "title": "string", "description": "string", "status": "open", "source_segment_ids": ["string"]}],
  "questions": [{"id": "uuid", "question": "string", "status": "open", "source_segment_ids": ["string"]}]
}`, string(stateJSON), input.RecentContext, input.TranscriptChunk)
}

func (g *GeminiIntelligence) buildFinalizationPrompt(input FinalizationInput) string {
	stateJSON := "none"
	if input.FinalState != nil {
		if state, err := json.MarshalIndent(input.FinalState, "", "  "); err == nil {
			stateJSON = string(state)
		}
	}

	glossary := g.glossary.PromptSection()

	return fmt.Sprintf(`You are an AI meeting assistant. Generate a final meeting summary in Minutes of Meeting (MOM) style based on the complete transcript.

%s
Final Meeting State:
%s

Full Transcript:
%s

Write the summary in this exact MOM format. One entry per discussed item:

28/09/2026 %s MOM:
[ITEM_ID] Item title: current status of the item.

  Actions: next action needed, ETA: expected completion period

Rules:
- The opening line must be "DD/MM/YYYY <meeting title> MOM:" using today's date.
- Each entry begins with the exact business identifier in square brackets when one exists (e.g. [CR_002]). Never invent or change identifiers.
- For every entry:
  * state the CURRENT STATUS clearly (what has been confirmed/done/decided so far).
  * then list the NEXT ACTIONS as a single "Actions:" line.
  * include who is responsible when mentioned, and the ETA when mentioned.
- Use the regulated business glossary terms EXACTLY as written. Never guess or substitute similar-sounding words for glossary terms.
- Preserve the speakers' original language. If the meeting mixed Vietnamese and English, keep that mix in each entry.
- Keep entries terse; no commentary outside the MOM format.

Return JSON only:
{
  "title": "meeting title",
  "summary": "one- or two-sentence recap in MOM style",
  "mom_entries": [
    {
      "id": "CR_002",
      "title": "item title",
      "status": "current status of the item",
      "actions": "next actions, who, and ETA if mentioned",
      "assignee": "responsible person/team if mentioned",
      "eta": "expected completion period if mentioned"
    }
  ],
  "key_points": ["short bullet"],
  "participants": ["names if known"]
}

Do not include decisions, action_items, issues, questions, or duration arrays - use mom_entries only.`, glossary, stateJSON, input.FullTranscript, input.Title)
}

func (g *GeminiIntelligence) callLLM(ctx context.Context, prompt string) (string, error) {
	requestBody := map[string]interface{}{
		"model": g.model,
		"messages": []map[string]string{
			{
				"role":    "system",
				"content": "You are an AI meeting assistant that analyzes meeting transcripts and extracts structured information. Always respond with valid JSON only, no additional text.",
			},
			{
				"role":    "user",
				"content": prompt,
			},
		},
		"temperature": 0.3,
	}

	jsonBody, err := json.Marshal(requestBody)
	if err != nil {
		return "", err
	}

	const maxAttempts = 4
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if attempt > 1 {
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(3 * time.Duration(attempt) * time.Second):
			}
		}

		content, err := g.callGemini(ctx, jsonBody)
		if err == nil {
			return content, nil
		}
		lastErr = err
		if isRetryableGeminiError(err) {
			continue
		}
		break
	}
	return "", lastErr
}

func isRetryableGeminiError(err error) bool {
	return strings.Contains(err.Error(), "status 503") ||
		strings.Contains(err.Error(), "status 429") ||
		strings.Contains(err.Error(), "status 408")
}

func (g *GeminiIntelligence) callGemini(ctx context.Context, jsonBody []byte) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", "https://generativelanguage.googleapis.com/v1beta/openai/chat/completions", bytes.NewBuffer(jsonBody))
	if err != nil {
		return "", err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+g.apiKey)

	resp, err := g.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode == http.StatusServiceUnavailable || resp.StatusCode == http.StatusTooManyRequests {
		return "", fmt.Errorf("Gemini API error (status %d): %s", resp.StatusCode, geminiErrorMessage(body))
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Gemini API error (status %d): %s", resp.StatusCode, geminiErrorMessage(body))
	}

	var response struct {
		Choices []struct {
			Message struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.Unmarshal(body, &response); err != nil {
		return "", err
	}

	if len(response.Choices) == 0 || len(response.Choices[0].Message.Content) == 0 {
		return "", fmt.Errorf("no response from LLM")
	}

	return decodeGeminiContent(response.Choices[0].Message.Content)
}

// geminiErrorMessage extracts the human-readable message from a Gemini error
// body, which may be either an object {"error":{...}} or an array [{"error":{...}}].
func geminiErrorMessage(body []byte) string {
	if msg := parseGeminiError(body, func(data []byte) (msg string) {
		var e struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(data, &e) == nil {
			msg = e.Error.Message
		}
		return
	}); msg != "" {
		return msg
	}
	return string(body)
}

func parseGeminiError(body []byte, extract func([]byte) string) string {
	var asArray []json.RawMessage
	if json.Unmarshal(body, &asArray) != nil {
		return extract(body)
	}
	for _, item := range asArray {
		if msg := extract(item); msg != "" {
			return msg
		}
	}
	return ""
}

// decodeGeminiContent handles Gemini's OpenAI-compatible endpoint which may
// return content either as a string ("Four") or as an array of {text} blocks.
func decodeGeminiContent(raw json.RawMessage) (string, error) {
	var plain string
	if err := json.Unmarshal(raw, &plain); err == nil {
		return plain, nil
	}

	var blocks []struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return "", fmt.Errorf("unexpected content shape: %s", string(raw))
	}

	parts := make([]string, 0, len(blocks))
	for _, b := range blocks {
		if b.Text != "" {
			parts = append(parts, b.Text)
		}
	}
	if len(parts) == 0 {
		return "", fmt.Errorf("empty content blocks")
	}
	return strings.Join(parts, "\n"), nil
}

func (g *GeminiIntelligence) parseAnalysisResponse(response string) (MeetingStatePatch, error) {
	var patch MeetingStatePatch

	jsonStr := extractJSON(response)
	if err := json.Unmarshal([]byte(jsonStr), &patch); err != nil {
		return MeetingStatePatch{}, fmt.Errorf("failed to parse JSON: %w", err)
	}

	if err := patch.ensureIDs(); err != nil {
		return MeetingStatePatch{}, err
	}

	return patch, nil
}

func (g *GeminiIntelligence) parseFinalizationResponse(response string) (MeetingSummary, error) {
	var summary MeetingSummary

	jsonStr := extractJSON(response)
	if err := json.Unmarshal([]byte(jsonStr), &summary); err != nil {
		return MeetingSummary{}, fmt.Errorf("failed to parse JSON: %w", err)
	}

	return summary, nil
}

func (p *MeetingStatePatch) ensureIDs() error {
	for i := range p.Topics {
		if p.Topics[i].ID == "" {
			p.Topics[i].ID = uuid.New().String()
		}
	}
	for i := range p.Decisions {
		if p.Decisions[i].ID == "" {
			p.Decisions[i].ID = uuid.New().String()
		}
	}
	for i := range p.ActionItems {
		if p.ActionItems[i].ID == "" {
			p.ActionItems[i].ID = uuid.New().String()
		}
	}
	for i := range p.Issues {
		if p.Issues[i].ID == "" {
			p.Issues[i].ID = uuid.New().String()
		}
	}
	for i := range p.Questions {
		if p.Questions[i].ID == "" {
			p.Questions[i].ID = uuid.New().String()
		}
	}
	return nil
}
