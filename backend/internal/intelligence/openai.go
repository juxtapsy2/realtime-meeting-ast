package intelligence

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

type OpenAIIntelligence struct {
	apiKey   string
	model    string
	baseURL  string
	client   *http.Client
	glossary Glossary
	// budget gates requests against the provider's token limits. It is
	// shared per API key (see budgetFor) because the limits belong to the
	// credential, not to this instance.
	budget *TokenBudget
}

// NewOpenAIIntelligence returns an IntelligenceProvider backed by the OpenAI
// API. model overrides the provider default when non-empty.
func NewOpenAIIntelligence(apiKey, model string, glossary Glossary) (*OpenAIIntelligence, error) {
	return newOpenAICompatible(apiKey, "https://api.openai.com/v1", "gpt-4o-mini", model, glossary)
}

// NewGroqIntelligence returns an IntelligenceProvider backed by the Groq API.
// Groq exposes an OpenAI-compatible chat completions endpoint, so it reuses the
// same client and parsing as the OpenAI provider. model overrides the provider
// default when non-empty.
func NewGroqIntelligence(apiKey, model string, glossary Glossary) (*OpenAIIntelligence, error) {
	return newOpenAICompatible(apiKey, "https://api.groq.com/openai/v1", "openai/gpt-oss-120b", model, glossary)
}

func newOpenAICompatible(apiKey, baseURL, defaultModel, model string, glossary Glossary) (*OpenAIIntelligence, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("API key is required")
	}

	if model == "" {
		model = defaultModel
	}

	return &OpenAIIntelligence{
		apiKey:   apiKey,
		model:    model,
		baseURL:  baseURL,
		client:   &http.Client{Timeout: 60 * time.Second},
		glossary: glossary,
		budget:   budgetFor(baseURL, apiKey),
	}, nil
}

func (o *OpenAIIntelligence) AnalyzeChunk(ctx context.Context, input AnalysisInput) (MeetingStatePatch, error) {
	prompt := o.buildAnalysisPrompt(input)

	response, err := o.callLLM(ctx, prompt, nil)
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
	prompt := finalizationPrompt(o.glossary, input)

	response, err := o.callLLM(ctx, prompt, momSummaryFormat())
	if err != nil {
		var tooLarge *RequestTooLargeError
		if errors.As(err, &tooLarge) {
			// The single request exceeded the provider's hard cap (e.g. a
			// dense written document). Split the evidence: extract entries
			// per chunk, then merge them into the final MOM.
			return o.finalizeChunked(ctx, input)
		}
		return MeetingSummary{}, fmt.Errorf("failed to call LLM: %w", err)
	}

	summary, err := parseFinalizationJSON(response)
	if err != nil {
		return MeetingSummary{}, fmt.Errorf("failed to parse response: %w", err)
	}

	return summary, nil
}

// finalizeChunked is the map-reduce fallback for evidence that exceeds the
// provider's per-request token cap: extract MoM entries from bounded chunks,
// then merge the entries into the final MOM with one small call.
func (o *OpenAIIntelligence) finalizeChunked(ctx context.Context, input FinalizationInput) (MeetingSummary, error) {
	evidence := strings.TrimSpace(strings.Join(
		[]string{sanitizePromptText(input.FullTranscript), sanitizePromptText(input.DocumentText)}, "\n\n"))
	if evidence == "" {
		return MeetingSummary{}, fmt.Errorf("no evidence to summarize")
	}
	chunks := chunkText(evidence, chunkEvidenceBytes)
	if len(chunks) == 0 {
		return MeetingSummary{}, fmt.Errorf("no evidence to summarize")
	}
	log.Printf("finalization: evidence too large for one request, splitting into %d chunks", len(chunks))

	var allEntries []MOMEntry
	var allKeyPoints []string
	for i, chunk := range chunks {
		prompt := chunkExtractionPrompt(input.Title, o.glossary, i+1, len(chunks), chunk)
		response, err := o.callLLM(ctx, prompt, chunkEntriesFormat())
		if err != nil {
			return MeetingSummary{}, fmt.Errorf("failed to call LLM for chunk %d/%d: %w", i+1, len(chunks), err)
		}
		entries, keyPoints, err := parseChunkEntries(response)
		if err != nil {
			return MeetingSummary{}, fmt.Errorf("failed to parse chunk %d/%d: %w", i+1, len(chunks), err)
		}
		allEntries = append(allEntries, entries...)
		allKeyPoints = append(allKeyPoints, keyPoints...)
	}

	response, err := o.callLLM(ctx, mergePrompt(input.Title, o.glossary, allEntries, allKeyPoints), momSummaryFormat())
	if err != nil {
		return MeetingSummary{}, fmt.Errorf("failed to call LLM for merge: %w", err)
	}
	summary, err := parseFinalizationJSON(response)
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

// maxCompletionTokens bounds every generation. Without an explicit value the
// provider's default can truncate a long MoM mid-JSON (schema validation then
// fails with failed_generation) or push the model to compress the list of
// items. 8192 leaves room for reasoning plus a full-length MoM.
const maxCompletionTokens = 8192

// callLLM performs one chat-completions request. responseFormat, when
// non-nil, is sent as the response_format field (e.g. a strict JSON schema).
func (o *OpenAIIntelligence) callLLM(ctx context.Context, prompt string, responseFormat map[string]interface{}) (string, error) {
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
		"temperature":           0.3,
		"max_completion_tokens": maxCompletionTokens,
	}
	if responseFormat != nil {
		requestBody["response_format"] = responseFormat
	}

	jsonBody, err := json.Marshal(requestBody)
	if err != nil {
		return "", err
	}

	// Gate on the provider's token budget before sending: wait for the
	// minute window instead of provoking a 429. ErrDailyTokenBudget is
	// terminal and must not be retried.
	res, err := o.budget.Reserve(ctx, estimateTokens(jsonBody))
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", o.baseURL+"/chat/completions", bytes.NewBuffer(jsonBody))
	if err != nil {
		o.budget.Release(res)
		return "", err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+o.apiKey)

	resp, err := o.client.Do(req)
	if err != nil {
		o.budget.Release(res)
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		o.budget.Release(res)
		return "", err
	}

	var apiErr struct {
		Error struct {
			Message          string `json:"message"`
			Type             string `json:"type"`
			FailedGeneration string `json:"failed_generation"`
		} `json:"error"`
	}
	_ = json.Unmarshal(body, &apiErr)

	// Surface throttling as a typed error so retry loops can honor the
	// provider-suggested wait instead of guessing (Groq TPM limits can
	// require ~40s between large requests).
	if resp.StatusCode == http.StatusTooManyRequests ||
		strings.Contains(apiErr.Error.Type, "rate_limit") ||
		strings.Contains(apiErr.Error.Message, "Rate limit") {
		o.budget.Release(res)
		msg := apiErr.Error.Message
		if msg == "" {
			msg = fmt.Sprintf("LLM API error (status %s)", resp.Status)
		} else if apiErr.Error.Type != "" {
			msg = fmt.Sprintf("LLM API error (%s): %s", apiErr.Error.Type, apiErr.Error.Message)
		}
		return "", &RateLimitError{
			RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After"), apiErr.Error.Message),
			Message:    msg,
		}
	}

	// Groq rejects a single request above the per-request token cap even in
	// an empty window ("Request too large for model ... TPM: Limit 8000,
	// Requested 9807"). Such requests are not billed and retrying them
	// verbatim is pointless; surface a typed error so callers can shrink or
	// split the prompt.
	if resp.StatusCode != http.StatusOK && strings.Contains(apiErr.Error.Message, "Request too large") {
		o.budget.Release(res)
		return "", parseRequestTooLargeError(apiErr.Error.Message)
	}

	// Rejected requests are not billed, so give their reservation back.
	if resp.StatusCode != http.StatusOK {
		o.budget.Release(res)
	}

	// Permanent rejections (bad key, depleted billing, forbidden) cannot
	// succeed on retry; surface them typed so retry loops fail fast with
	// a message that says configuration must change.
	switch resp.StatusCode {
	case http.StatusUnauthorized, http.StatusPaymentRequired, http.StatusForbidden:
		msg := apiErr.Error.Message
		if msg == "" {
			msg = fmt.Sprintf("LLM API error (status %s)", resp.Status)
		}
		return "", &ProviderHTTPError{StatusCode: resp.StatusCode, Message: msg}
	}

	var response struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}

	if err := json.Unmarshal(body, &response); err != nil {
		if resp.StatusCode == http.StatusOK {
			// The provider billed us but the body is unreadable; keep the
			// estimate so the window is not under-counted.
			o.budget.Record(res, estimateTokens(jsonBody))
		}
		return "", err
	}

	if resp.StatusCode == http.StatusOK {
		actual := response.Usage.PromptTokens + response.Usage.CompletionTokens
		if actual <= 0 {
			actual = estimateTokens(jsonBody)
		}
		o.budget.Record(res, actual)
	}

	if len(response.Choices) > 0 {
		content := strings.TrimSpace(response.Choices[0].Message.Content)
		if content == "" {
			return "", fmt.Errorf("LLM returned empty response (status %s)", resp.Status)
		}
		return content, nil
	}

	if apiErr.Error.Message != "" {
		msg := fmt.Sprintf("LLM API error (%s): %s", apiErr.Error.Type, apiErr.Error.Message)
		if gen := apiErr.Error.FailedGeneration; gen != "" {
			msg += fmt.Sprintf(" (invalid output: %.300s)", gen)
		}
		return "", errors.New(msg)
	}
	return "", fmt.Errorf("no response from LLM (status %s)", resp.Status)
}

// retryAfterMessage matches Groq's "Please try again in 39.915s." hint.
var retryAfterMessage = regexp.MustCompile(`try again in ([0-9]+(?:\.[0-9]+)?)s`)

// requestTooLargeMessage matches Groq's hard per-request rejection:
// "Request too large for model ... TPM: Limit 8000, Requested 9807, ...".
var requestTooLargeMessage = regexp.MustCompile(`Limit (\d+), Requested (\d+)`)

// parseRetryAfter extracts the suggested wait from the Retry-After header,
// falling back to the provider's error message. Zero when neither applies.
func parseRetryAfter(headerValue, message string) time.Duration {
	if secs, err := strconv.ParseFloat(strings.TrimSpace(headerValue), 64); err == nil && secs > 0 {
		return time.Duration(secs * float64(time.Second))
	}
	if m := retryAfterMessage.FindStringSubmatch(message); len(m) == 2 {
		if secs, err := strconv.ParseFloat(m[1], 64); err == nil && secs > 0 {
			return time.Duration(secs * float64(time.Second))
		}
	}
	return 0
}

// parseRequestTooLargeError turns Groq's "Request too large" message into a
// typed error carrying the reported limit/requested sizes when present.
func parseRequestTooLargeError(message string) *RequestTooLargeError {
	e := &RequestTooLargeError{Message: message}
	if m := requestTooLargeMessage.FindStringSubmatch(message); len(m) == 3 {
		e.Limit, _ = strconv.Atoi(m[1])
		e.Requested, _ = strconv.Atoi(m[2])
	}
	return e
}

func (o *OpenAIIntelligence) parseAnalysisResponse(response string) (MeetingStatePatch, error) {
	var patch MeetingStatePatch

	// Try to extract JSON from the response
	jsonStr := extractJSON(response)
	if strings.TrimSpace(jsonStr) == "" {
		return MeetingStatePatch{}, fmt.Errorf("LLM returned an empty JSON response")
	}

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

func extractJSON(response string) string {
	// Try to extract JSON from markdown code block
	startMarker := "```json\n"
	endMarker := "\n```"

	start := strings.Index(response, startMarker)
	end := strings.LastIndex(response, endMarker)

	// end must cover the whole start marker or the slice below would be
	// invalid; a malformed fence falls through to the raw response.
	if start >= 0 && end >= start+len(startMarker) {
		return response[start+len(startMarker) : end]
	}

	return response
}
