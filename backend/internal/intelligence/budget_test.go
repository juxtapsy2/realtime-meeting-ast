package intelligence

import (
	"context"
	"errors"
	"testing"
	"time"
)

// newTestBudget returns a budget with small limits so tests do not wait out
// the real 60s window.
func newTestBudget(minLimit, dayLimit int, windowLen time.Duration) *TokenBudget {
	b := newTokenBudget()
	b.minLimit = minLimit
	b.dayLimit = dayLimit
	b.windowLen = windowLen
	b.day = b.dayKey(time.Now())
	return b
}

func TestReserveAdmitsUnderLimit(t *testing.T) {
	b := newTestBudget(100, 1000, time.Minute)

	res, err := b.Reserve(context.Background(), 60)
	if err != nil {
		t.Fatalf("Reserve() error = %v", err)
	}
	if got := b.windowTotalLocked(); got != 60 {
		t.Errorf("window total = %d, want 60", got)
	}
	if b.dayTokens != 60 {
		t.Errorf("dayTokens = %d, want 60", b.dayTokens)
	}
	if res.tokens != 60 {
		t.Errorf("reservation tokens = %d, want 60", res.tokens)
	}
}

func TestReserveWaitsForWindow(t *testing.T) {
	b := newTestBudget(100, 1000, 200*time.Millisecond)

	if _, err := b.Reserve(context.Background(), 80); err != nil {
		t.Fatalf("first Reserve() error = %v", err)
	}

	start := time.Now()
	// 80 + 30 > 100: must wait for the first reservation to leave the window.
	res, err := b.Reserve(context.Background(), 30)
	if err != nil {
		t.Fatalf("second Reserve() error = %v", err)
	}
	if elapsed := time.Since(start); elapsed < 100*time.Millisecond {
		t.Errorf("second Reserve() returned after %v, expected it to wait for the window", elapsed)
	}
	if res.tokens != 30 {
		t.Errorf("reservation tokens = %d, want 30", res.tokens)
	}
}

func TestReserveLargeRequestNeedsEmptyWindow(t *testing.T) {
	b := newTestBudget(100, 1000, 200*time.Millisecond)

	if _, err := b.Reserve(context.Background(), 10); err != nil {
		t.Fatalf("first Reserve() error = %v", err)
	}

	// An estimate above the whole minute limit only fits an empty window.
	start := time.Now()
	if _, err := b.Reserve(context.Background(), 150); err != nil {
		t.Fatalf("large Reserve() error = %v", err)
	}
	if elapsed := time.Since(start); elapsed < 100*time.Millisecond {
		t.Errorf("large Reserve() returned after %v, expected it to wait for an empty window", elapsed)
	}
}

func TestReserveDailyBudgetExhausted(t *testing.T) {
	b := newTestBudget(1000, 100, time.Minute)
	b.dayTokens = 90

	if _, err := b.Reserve(context.Background(), 20); !errors.Is(err, ErrDailyTokenBudget) {
		t.Errorf("Reserve() error = %v, want ErrDailyTokenBudget", err)
	}

	// Requests that still fit the day are admitted.
	if _, err := b.Reserve(context.Background(), 10); err != nil {
		t.Errorf("Reserve() within daily limit error = %v", err)
	}
}

func TestRecordReplacesEstimate(t *testing.T) {
	b := newTestBudget(100, 1000, time.Minute)

	res, err := b.Reserve(context.Background(), 90)
	if err != nil {
		t.Fatalf("Reserve() error = %v", err)
	}
	b.Record(res, 40)

	if b.dayTokens != 40 {
		t.Errorf("dayTokens after Record = %d, want 40", b.dayTokens)
	}
	if got := b.windowTotalLocked(); got != 40 {
		t.Errorf("window total after Record = %d, want 40", got)
	}
	// Actual usage below the estimate frees capacity for the next request.
	if _, err := b.Reserve(context.Background(), 60); err != nil {
		t.Errorf("Reserve() after Record error = %v", err)
	}
}

func TestReleaseFreesWindow(t *testing.T) {
	b := newTestBudget(100, 1000, time.Minute)

	res, err := b.Reserve(context.Background(), 90)
	if err != nil {
		t.Fatalf("Reserve() error = %v", err)
	}
	b.Release(res)

	if b.dayTokens != 0 {
		t.Errorf("dayTokens after Release = %d, want 0", b.dayTokens)
	}
	if got := b.windowTotalLocked(); got != 0 {
		t.Errorf("window total after Release = %d, want 0", got)
	}
	if _, err := b.Reserve(context.Background(), 90); err != nil {
		t.Errorf("Reserve() after Release error = %v", err)
	}
}

func TestReserveHonorsContextCancellation(t *testing.T) {
	b := newTestBudget(100, 1000, time.Minute)

	if _, err := b.Reserve(context.Background(), 100); err != nil {
		t.Fatalf("first Reserve() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := b.Reserve(ctx, 10); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Reserve() error = %v, want context.DeadlineExceeded", err)
	}
}

func TestBudgetForSharesPerCredential(t *testing.T) {
	first := budgetFor("https://api.groq.com/openai/v1", "key-a")
	second := budgetFor("https://api.groq.com/openai/v1", "key-a")
	if first != second {
		t.Error("same baseURL+apiKey must share one budget")
	}

	other := budgetFor("https://api.groq.com/openai/v1", "key-b")
	if first == other {
		t.Error("different API keys must not share a budget")
	}
}

func TestEstimateTokens(t *testing.T) {
	if got := estimateTokens(make([]byte, 4000)); got != 4000*estimateNumerator/estimateDenominator+completionReserve {
		t.Errorf("estimateTokens(4000 bytes) = %d, want %d", got, 4000*estimateNumerator/estimateDenominator+completionReserve)
	}
}
