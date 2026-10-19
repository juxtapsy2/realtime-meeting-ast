package meetings

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/user/realtime-meeting-ast/backend/internal/intelligence"
)

func TestSummaryRetryDelay(t *testing.T) {
	plain := errors.New("LLM returned an empty JSON response")
	rateLimited := func(wait time.Duration) error {
		return &intelligence.RateLimitError{RetryAfter: wait, Message: "rate limited"}
	}

	tests := []struct {
		name    string
		attempt int
		err     error
		want    time.Duration
	}{
		{"first failure backoff", 1, plain, 5 * time.Second},
		{"second failure backoff", 2, plain, 10 * time.Second},
		{"third failure backoff", 3, plain, 20 * time.Second},
		{"rate limit overrides base wait", 1, rateLimited(40 * time.Second), 40 * time.Second},
		{"short rate limit hint keeps base wait", 2, rateLimited(3 * time.Second), 10 * time.Second},
		{"rate limit hint capped at 60s", 1, rateLimited(120 * time.Second), 60 * time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := summaryRetryDelay(tt.attempt, tt.err); got != tt.want {
				t.Errorf("summaryRetryDelay(%d, err) = %v, want %v", tt.attempt, tt.err, got)
			}
		})
	}
}

// stubProvider fails FinalizeMeeting with a fixed error and counts calls.
type stubProvider struct {
	err   error
	calls int
}

func (p *stubProvider) AnalyzeChunk(context.Context, intelligence.AnalysisInput) (intelligence.MeetingStatePatch, error) {
	return intelligence.MeetingStatePatch{}, nil
}

func (p *stubProvider) FinalizeMeeting(context.Context, intelligence.FinalizationInput) (intelligence.MeetingSummary, error) {
	p.calls++
	return intelligence.MeetingSummary{}, p.err
}

func TestGenerateSummaryWithRetryStopsOnDailyBudget(t *testing.T) {
	p := &stubProvider{err: intelligence.ErrDailyTokenBudget}
	s := &Service{}

	_, err := s.generateSummaryWithRetry(context.Background(), p, 1, intelligence.FinalizationInput{})
	if !errors.Is(err, intelligence.ErrDailyTokenBudget) {
		t.Fatalf("generateSummaryWithRetry() error = %v, want ErrDailyTokenBudget", err)
	}
	if p.calls != 1 {
		t.Errorf("provider called %d times, want 1 (terminal error must not be retried)", p.calls)
	}
}

func TestGenerateSummaryWithRetryStopsOnRequestTooLarge(t *testing.T) {
	p := &stubProvider{err: &intelligence.RequestTooLargeError{Limit: 8000, Requested: 9807}}
	s := &Service{}

	_, err := s.generateSummaryWithRetry(context.Background(), p, 1, intelligence.FinalizationInput{})
	var tooLarge *intelligence.RequestTooLargeError
	if !errors.As(err, &tooLarge) {
		t.Fatalf("generateSummaryWithRetry() error = %v, want RequestTooLargeError", err)
	}
	if p.calls != 1 {
		t.Errorf("provider called %d times, want 1 (oversized input cannot shrink on retry)", p.calls)
	}
}

func TestSummaryFailureMessage(t *testing.T) {
	rateLimited := &intelligence.RateLimitError{RetryAfter: 46 * time.Second, Message: "rate limited"}

	tests := []struct {
		name string
		err  error
		want string
	}{
		{"daily budget", intelligence.ErrDailyTokenBudget, "daily limit"},
		{"rate limit with hint", rateLimited, "about 46 seconds"},
		{"rate limit without hint", &intelligence.RateLimitError{Message: "rate limited"}, "about a minute"},
		{"timeout", context.DeadlineExceeded, "timed out"},
		{"request too large", &intelligence.RequestTooLargeError{Limit: 8000, Requested: 9807}, "too large"},
		{"unknown failure", errors.New("boom"), "Summary generation failed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := summaryFailureMessage(tt.err); !strings.Contains(got, tt.want) {
				t.Errorf("summaryFailureMessage(%v) = %q, want substring %q", tt.err, got, tt.want)
			}
		})
	}
}
