package intelligence

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestExtractJSON(t *testing.T) {
	tests := []struct {
		name     string
		response string
		want     string
	}{
		{"plain json", `{"a":1}`, `{"a":1}`},
		{"fenced", "```json\n{\"a\":1}\n```", `{"a":1}`},
		{"empty fence", "```json\n\n```", ""},
		{"malformed fence returns raw", "```json\n```", "```json\n```"},
		{"fence without newline after marker", "```json {\"a\":1}```", "```json {\"a\":1}```"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractJSON(tt.response)
			if got != tt.want {
				t.Errorf("extractJSON() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseFinalizationJSON(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		summary, err := parseFinalizationJSON(`{"title":"t","summary":"s","mom_entries":[{"id":"CR_002","title":"budget","status":"approved"}],"key_points":["k"]}`)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if summary.Title != "t" || len(summary.MOMEntries) != 1 || summary.MOMEntries[0].ID != "CR_002" {
			t.Errorf("unexpected summary: %+v", summary)
		}
	})

	t.Run("empty response gets explicit error", func(t *testing.T) {
		_, err := parseFinalizationJSON("```json\n\n```")
		if err == nil || !strings.Contains(err.Error(), "empty JSON response") {
			t.Fatalf("want empty JSON error, got %v", err)
		}
	})
}

func TestParseRetryAfter(t *testing.T) {
	tests := []struct {
		name    string
		header  string
		message string
		want    time.Duration
	}{
		{"header wins", "7", "ignored", 7 * time.Second},
		{"groq message hint", "", "Rate limit reached ... Please try again in 39.915s.", 39915 * time.Millisecond},
		{"neither", "", "no hint", 0},
		{"garbage header falls back to message", "soon", "try again in 12s", 12 * time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseRetryAfter(tt.header, tt.message); got != tt.want {
				t.Errorf("parseRetryAfter() = %v, want %v", got, tt.want)
			}
		})
	}
}

// stubServer answers with the given status/headers/body and reports the
// received request body through the returned channel-reading closure.
func stubServer(t *testing.T, status int, headers map[string]string, body string) (*httptest.Server, func() []byte) {
	t.Helper()
	var received []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received, _ = io.ReadAll(r.Body)
		for k, v := range headers {
			w.Header().Set(k, v)
		}
		w.WriteHeader(status)
		io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv, func() []byte { return received }
}

func TestCallLLMRateLimit(t *testing.T) {
	groqBody := `{"error":{"message":"Rate limit reached for model ` + "`openai/gpt-oss-120b`" + ` in organization on tokens per minute (TPM): Limit 8000, Used 7714, Requested 5608. Please try again in 39.915s.","type":"tokens","code":"rate_limit_exceeded"}}`
	srv, _ := stubServer(t, http.StatusTooManyRequests, nil, groqBody)

	p, err := newOpenAICompatible("key", srv.URL, "m", "", Glossary{})
	if err != nil {
		t.Fatalf("constructor: %v", err)
	}

	_, err = p.callLLM(t.Context(), "hi", nil)
	var rl *RateLimitError
	if !errors.As(err, &rl) {
		t.Fatalf("want RateLimitError, got %v", err)
	}
	if rl.RetryAfter != 39915*time.Millisecond {
		t.Errorf("RetryAfter = %v, want 39.915s", rl.RetryAfter)
	}
	if !strings.Contains(rl.Message, "Rate limit reached") {
		t.Errorf("Message = %q, want provider detail preserved", rl.Message)
	}
}

func TestCallLLMRateLimitRetryAfterHeader(t *testing.T) {
	srv, _ := stubServer(t, http.StatusTooManyRequests,
		map[string]string{"Retry-After": "42"},
		`{"error":{"message":"slow down","type":"rate_limit_exceeded"}}`)

	p, _ := newOpenAICompatible("key", srv.URL, "m", "", Glossary{})

	_, err := p.callLLM(t.Context(), "hi", nil)
	var rl *RateLimitError
	if !errors.As(err, &rl) {
		t.Fatalf("want RateLimitError, got %v", err)
	}
	if rl.RetryAfter != 42*time.Second {
		t.Errorf("RetryAfter = %v, want 42s", rl.RetryAfter)
	}
}

func TestCallLLMSendsResponseFormat(t *testing.T) {
	okBody := `{"choices":[{"message":{"content":"{\"ok\":true}"}}]}`

	srv, received := stubServer(t, http.StatusOK, nil, okBody)
	p, _ := newOpenAICompatible("key", srv.URL, "m", "", Glossary{})

	if _, err := p.callLLM(t.Context(), "hi", momSummaryFormat()); err != nil {
		t.Fatalf("callLLM: %v", err)
	}
	var req struct {
		ResponseFormat map[string]interface{} `json:"response_format"`
	}
	if err := json.Unmarshal(received(), &req); err != nil {
		t.Fatalf("request not JSON: %v", err)
	}
	if req.ResponseFormat == nil {
		t.Fatal("response_format missing from request")
	}
	if name, _ := req.ResponseFormat["json_schema"].(map[string]interface{})["name"].(string); name != "mom_summary" {
		t.Errorf("schema name = %q, want mom_summary", name)
	}

	// Without a schema the field must be omitted entirely.
	if _, err := p.callLLM(t.Context(), "hi", nil); err != nil {
		t.Fatalf("callLLM: %v", err)
	}
	var req2 struct {
		ResponseFormat map[string]interface{} `json:"response_format"`
	}
	if err := json.Unmarshal(received(), &req2); err != nil {
		t.Fatalf("request not JSON: %v", err)
	}
	if req2.ResponseFormat != nil {
		t.Error("response_format should be omitted when no schema is given")
	}
}

func TestFinalizeMeetingParsesSchemaShapedResponse(t *testing.T) {
	content := `{"title":"Weekly sync","summary":"CR_002 budget approved.","mom_entries":[{"id":"CR_002","title":"budget","status":"approved by Linh","actions":"publish the decision log","assignee":"Linh","eta":"Friday"}],"key_points":["CR_002 budget approved"]}`
	body, _ := json.Marshal(map[string]interface{}{
		"choices": []map[string]interface{}{
			{"message": map[string]string{"content": content}},
		},
	})
	srv, _ := stubServer(t, http.StatusOK, nil, string(body))
	p, _ := newOpenAICompatible("key", srv.URL, "m", "", Glossary{})

	summary, err := p.FinalizeMeeting(t.Context(), FinalizationInput{MeetingID: "1", Title: "Weekly sync"})
	if err != nil {
		t.Fatalf("FinalizeMeeting: %v", err)
	}
	if summary.Title != "Weekly sync" || len(summary.MOMEntries) != 1 ||
		summary.MOMEntries[0].ID != "CR_002" || summary.MOMEntries[0].ETA != "Friday" {
		t.Errorf("unexpected summary: %+v", summary)
	}
}

func TestCallLLMSendsMaxCompletionTokens(t *testing.T) {
	srv, received := stubServer(t, http.StatusOK, nil,
		`{"choices":[{"message":{"content":"{\"ok\":true}"}}]}`)
	p, _ := newOpenAICompatible("key", srv.URL, "m", "", Glossary{})

	if _, err := p.callLLM(t.Context(), "hi", nil); err != nil {
		t.Fatalf("callLLM: %v", err)
	}
	var req struct {
		MaxCompletionTokens int `json:"max_completion_tokens"`
	}
	if err := json.Unmarshal(received(), &req); err != nil {
		t.Fatalf("request not JSON: %v", err)
	}
	if req.MaxCompletionTokens != maxCompletionTokens {
		t.Errorf("max_completion_tokens = %d, want %d", req.MaxCompletionTokens, maxCompletionTokens)
	}
}

func TestCallLLMSurfacesFailedGeneration(t *testing.T) {
	body := `{"error":{"message":"Failed to validate JSON. Please adjust your prompt. See 'failed_generation' for more details.","type":"invalid_request_error","param":"response_format","failed_generation":"{\"title\":\"t\",\"mom_ent"}}`
	srv, _ := stubServer(t, http.StatusBadRequest, nil, body)
	p, _ := newOpenAICompatible("key", srv.URL, "m", "", Glossary{})

	_, err := p.callLLM(t.Context(), "hi", nil)
	if err == nil {
		t.Fatal("want error, got nil")
	}
	if !strings.Contains(err.Error(), "invalid output") || !strings.Contains(err.Error(), "mom_ent") {
		t.Errorf("error should carry the invalid output snippet, got %v", err)
	}
}

func TestCallLLMRecordsUsageInBudget(t *testing.T) {
	srv, _ := stubServer(t, http.StatusOK, nil,
		`{"choices":[{"message":{"content":"{\"ok\":true}"}}],"usage":{"prompt_tokens":100,"completion_tokens":50}}`)
	p, _ := newOpenAICompatible("key", srv.URL, "m", "", Glossary{})

	if _, err := p.callLLM(t.Context(), "hi", nil); err != nil {
		t.Fatalf("callLLM: %v", err)
	}
	if got := p.budget.windowTotalLocked(); got != 150 {
		t.Errorf("budget window = %d, want usage total 150 (estimate must be replaced by actual usage)", got)
	}
	if p.budget.dayTokens != 150 {
		t.Errorf("budget dayTokens = %d, want 150", p.budget.dayTokens)
	}
}

func TestCallLLMRejectsRequestsOverDailyBudget(t *testing.T) {
	var hit int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit++
		io.WriteString(w, `{"choices":[{"message":{"content":"ok"}}]}`)
	}))
	t.Cleanup(srv.Close)

	p, _ := newOpenAICompatible("key", srv.URL, "m", "", Glossary{})
	// The httptest URL is not in providerTokenLimits, so give its budget an
	// explicit quota to exercise the daily gate.
	p.budget.dayLimit = 1000
	p.budget.day = p.budget.dayKey(time.Now())
	p.budget.dayTokens = p.budget.dayLimit

	_, err := p.callLLM(t.Context(), "hi", nil)
	if !errors.Is(err, ErrDailyTokenBudget) {
		t.Fatalf("callLLM() error = %v, want ErrDailyTokenBudget", err)
	}
	if hit != 0 {
		t.Errorf("provider was called %d times, want 0 (daily budget is checked before sending)", hit)
	}
}

func TestCallLLMReleasesBudgetOnRateLimit(t *testing.T) {
	srv, _ := stubServer(t, http.StatusTooManyRequests,
		nil, `{"error":{"message":"slow down","type":"rate_limit_exceeded"}}`)
	p, _ := newOpenAICompatible("key", srv.URL, "m", "", Glossary{})

	if _, err := p.callLLM(t.Context(), "hi", nil); err == nil {
		t.Fatal("want rate limit error, got nil")
	}
	if got := p.budget.windowTotalLocked(); got != 0 {
		t.Errorf("budget window = %d after rejected request, want 0 (rejected requests are not billed)", got)
	}
}

func TestParseRequestTooLargeError(t *testing.T) {
	msg := "Request too large for model `openai/gpt-oss-120b` in organization `org` service tier `on_demand` on tokens per minute (TPM): Limit 8000, Requested 9807, please reduce your message size and try again."
	e := parseRequestTooLargeError(msg)
	if e.Limit != 8000 || e.Requested != 9807 {
		t.Errorf("parsed limit/requested = %d/%d, want 8000/9807", e.Limit, e.Requested)
	}
	if e.Message != msg {
		t.Errorf("message not preserved")
	}
	// Unknown shape still yields a usable typed error.
	e2 := parseRequestTooLargeError("Request too large")
	if e2.Limit != 0 || e2.Requested != 0 {
		t.Errorf("unexpected parse of bare message: %+v", e2)
	}
}

func TestCallLLMSurfacesRequestTooLarge(t *testing.T) {
	groqBody := `{"error":{"message":"Request too large for model ` + "`openai/gpt-oss-120b`" + ` in organization ` + "`org`" + ` service tier ` + "`on_demand`" + ` on tokens per minute (TPM): Limit 8000, Requested 9807, please reduce your message size and try again.","type":"tokens"}}`
	srv, _ := stubServer(t, http.StatusBadRequest, nil, groqBody)
	p, _ := newOpenAICompatible("key", srv.URL, "m", "", Glossary{})

	_, err := p.callLLM(t.Context(), "big prompt", nil)
	var tooLarge *RequestTooLargeError
	if !errors.As(err, &tooLarge) {
		t.Fatalf("want RequestTooLargeError, got %v", err)
	}
	if tooLarge.Limit != 8000 || tooLarge.Requested != 9807 {
		t.Errorf("parsed %d/%d, want 8000/9807", tooLarge.Limit, tooLarge.Requested)
	}
	if got := p.budget.windowTotalLocked(); got != 0 {
		t.Errorf("budget window = %d after rejected request, want 0", got)
	}
}

func TestFinalizeMeetingSplitsWhenRequestTooLarge(t *testing.T) {
	summaryBody := `{"choices":[{"message":{"content":"{\"title\":\"Weekly sync\",\"summary\":\"All items covered after merge.\",\"mom_entries\":[{\"id\":\"CR_001\",\"title\":\"first\",\"status\":\"done\",\"actions\":\"\",\"assignee\":\"\",\"eta\":\"\"},{\"id\":\"CR_002\",\"title\":\"second\",\"status\":\"pending\",\"actions\":\"\",\"assignee\":\"\",\"eta\":\"\"}],\"key_points\":[\"two items\"]}"}}]}`
	chunkBody := `{"choices":[{"message":{"content":"{\"mom_entries\":[{\"id\":\"CR_001\",\"title\":\"first\",\"status\":\"done\",\"actions\":\"\",\"assignee\":\"\",\"eta\":\"\"}],\"key_points\":[\"first item\"]}"}}]}`

	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
			ResponseFormat struct {
				JSONSchema struct {
					Name string `json:"name"`
				} `json:"json_schema"`
			} `json:"response_format"`
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &req)
		calls = append(calls, req.ResponseFormat.JSONSchema.Name)

		switch req.ResponseFormat.JSONSchema.Name {
		case "mom_summary":
			if strings.Contains(req.Messages[1].Content, "Pre-extracted MOM entries") {
				io.WriteString(w, summaryBody)
				return
			}
			// The single-shot call: reject it as oversized.
			w.WriteHeader(http.StatusBadRequest)
			io.WriteString(w, `{"error":{"message":"Request too large for model `+"`m`"+` on tokens per minute (TPM): Limit 8000, Requested 9807, please reduce your message size and try again.","type":"tokens"}}`)
		case "mom_chunk_entries":
			io.WriteString(w, chunkBody)
		default:
			t.Errorf("unexpected schema %q", req.ResponseFormat.JSONSchema.Name)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	t.Cleanup(srv.Close)

	p, _ := newOpenAICompatible("key", srv.URL, "m", "", Glossary{})
	summary, err := p.FinalizeMeeting(t.Context(), FinalizationInput{
		MeetingID:      "1",
		Title:          "Weekly sync",
		FullTranscript: "CR_001 done. CR_002 pending.",
	})
	if err != nil {
		t.Fatalf("FinalizeMeeting: %v", err)
	}
	if summary.Title != "Weekly sync" || len(summary.MOMEntries) != 2 {
		t.Errorf("unexpected merged summary: %+v", summary)
	}
	if len(calls) < 3 || calls[0] != "mom_summary" || calls[1] != "mom_chunk_entries" {
		t.Errorf("call sequence = %v, want mom_summary then mom_chunk_entries...", calls)
	}
	last := calls[len(calls)-1]
	if last != "mom_summary" {
		t.Errorf("last call schema = %q, want mom_summary (merge)", last)
	}
}
