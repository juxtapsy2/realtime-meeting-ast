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

type OpenAIIntelligence struct {
	apiKey  string
	model   string
	baseURL string
	client  *http.Client
}

func NewOpenAIIntelligence(apiKey string) (*OpenAIIntelligence, error) {
	return newOpenAICompatible(apiKey, "https://api.openai.com/v1", "gpt-4o-mini")
}

// NewGroqIntelligence returns an IntelligenceProvider backed by the Groq API.
// Groq exposes an OpenAI-compatible chat completions endpoint, so it reuses the
// same client and parsing as the OpenAI provider.
func NewGroqIntelligence(apiKey string) (*OpenAIIntelligence, error) {
	return newOpenAICompatible(apiKey, "https://api.groq.com/openai/v1", "openai/gpt-oss-120b")
}

func newOpenAICompatible(apiKey, baseURL, defaultModel string) (*OpenAIIntelligence, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("API key is required")
	}

	model := os.Getenv("LLM_MODEL")
	if model == "" {
		model = defaultModel
	}

	return &OpenAIIntelligence{
		apiKey:  apiKey,
		model:   model,
		baseURL: baseURL,
		client:  &http.Client{Timeout: 60 * time.Second},
	}, nil
}

func (o *OpenAIIntelligence) AnalyzeChunk(ctx context.Context, input AnalysisInput) (MeetingStatePatch, error) {
	prompt := o.buildAnalysisPrompt(input)

	response, err := o.callLLM(ctx, prompt)
	if err != nil {
		return MeetingStatePatch{}, fmt.Errorf("failed to call LLM: %w", err)
	}

	patch, err := o.parseAnalysisResponse(response)
	if err != nil {
		return MeetingStatePatch{}, fmt.Errorf("failed to parse response: %w", err)
	}

	return patch, nil
}

func (o *OpenAIIntelligence) FinalizeMeeting(ctx context.Context, input FinalizationInput) (MeetingSummary, error) {
	prompt := o.buildFinalizationPrompt(input)

	response, err := o.callLLM(ctx, prompt)
	if err != nil {
		return MeetingSummary{}, fmt.Errorf("failed to call LLM: %w", err)
	}

	summary, err := o.parseFinalizationResponse(response)
	if err != nil {
		return MeetingSummary{}, fmt.Errorf("failed to parse response: %w", err)
	}

	return summary, nil
}

func (o *OpenAIIntelligence) buildAnalysisPrompt(input AnalysisInput) string {
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

func (o *OpenAIIntelligence) buildFinalizationPrompt(input FinalizationInput) string {
	stateJSON := "none"
	if input.FinalState != nil {
		if state, err := json.MarshalIndent(input.FinalState, "", "  "); err == nil {
			stateJSON = string(state)
		}
	}

	return fmt.Sprintf(`You are an AI meeting assistant. Generate a final meeting summary based on the complete transcript and final state.

Final Meeting State:
%s

Full Transcript:
%s

Generate a concise meeting summary:
1. Brief title for the meeting
2. Short executive summary (2-3 sentences)
3. Key points (at most 5)
4. Decisions made
5. Action items with assignees
6. Issues raised
7. Open questions

Be concise. Format your response as JSON:
{
  "title": "string",
  "summary": "string",
  "key_points": ["string"],
  "decisions": [{"id": "uuid", "title": "string", "description": "string", "status": "confirmed", "confidence": 1.0}],
  "action_items": [{"id": "uuid", "description": "string", "assignee": "string", "status": "pending"}],
  "issues": [{"id": "uuid", "title": "string", "description": "string", "status": "open"}],
  "questions": [{"id": "uuid", "question": "string", "status": "open"}]
}`, stateJSON, input.FullTranscript)
}

func (o *OpenAIIntelligence) callLLM(ctx context.Context, prompt string) (string, error) {
	requestBody := map[string]interface{}{
		"model": o.model,
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
		"max_tokens":  700,
	}

	jsonBody, err := json.Marshal(requestBody)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", o.baseURL+"/chat/completions", bytes.NewBuffer(jsonBody))
	if err != nil {
		return "", err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+o.apiKey)

	resp, err := o.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var response struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.Unmarshal(body, &response); err != nil {
		return "", err
	}

	if len(response.Choices) == 0 {
		var apiErr struct {
			Error struct {
				Message string `json:"message"`
				Type    string `json:"type"`
			} `json:"error"`
		}
		if json.Unmarshal(body, &apiErr) == nil && apiErr.Error.Message != "" {
			return "", fmt.Errorf("LLM API error (%s): %s", apiErr.Error.Type, apiErr.Error.Message)
		}
		return "", fmt.Errorf("no response from LLM (status %s)", resp.Status)
	}

	return response.Choices[0].Message.Content, nil
}

func (o *OpenAIIntelligence) parseAnalysisResponse(response string) (MeetingStatePatch, error) {
	var patch MeetingStatePatch

	// Try to extract JSON from the response
	jsonStr := extractJSON(response)

	if err := json.Unmarshal([]byte(jsonStr), &patch); err != nil {
		return MeetingStatePatch{}, fmt.Errorf("failed to parse JSON: %w", err)
	}

	// Generate IDs for new items
	for i := range patch.Topics {
		if patch.Topics[i].ID == "" {
			patch.Topics[i].ID = uuid.New().String()
		}
	}
	for i := range patch.Decisions {
		if patch.Decisions[i].ID == "" {
			patch.Decisions[i].ID = uuid.New().String()
		}
	}
	for i := range patch.ActionItems {
		if patch.ActionItems[i].ID == "" {
			patch.ActionItems[i].ID = uuid.New().String()
		}
	}
	for i := range patch.Issues {
		if patch.Issues[i].ID == "" {
			patch.Issues[i].ID = uuid.New().String()
		}
	}
	for i := range patch.Questions {
		if patch.Questions[i].ID == "" {
			patch.Questions[i].ID = uuid.New().String()
		}
	}

	return patch, nil
}

func (o *OpenAIIntelligence) parseFinalizationResponse(response string) (MeetingSummary, error) {
	var summary MeetingSummary

	// Try to extract JSON from the response
	jsonStr := extractJSON(response)

	if err := json.Unmarshal([]byte(jsonStr), &summary); err != nil {
		return MeetingSummary{}, fmt.Errorf("failed to parse JSON: %w", err)
	}

	return summary, nil
}

func extractJSON(response string) string {
	// Try to extract JSON from markdown code block
	startMarker := "```json\n"
	endMarker := "\n```"

	start := strings.Index(response, startMarker)
	end := strings.LastIndex(response, endMarker)

	if start >= 0 && end > start {
		return response[start+len(startMarker) : end]
	}

	return response
}
