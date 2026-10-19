package intelligence

import (
	"context"
	"errors"
	"sync"
	"time"
)

// Published token limits for the Groq free tier (openai/gpt-oss-120b):
// 8K tokens per minute and 200K tokens per day. The budget gate admits a
// request only when the estimated cost fits both limits, so the client stays
// under them proactively instead of learning about an overshoot from a 429.
const (
	minuteTokenLimit = 8_000
	dayTokenLimit    = 200_000
	tokenWindow      = time.Minute

	// completionReserve holds back headroom for the response, which is only
	// known afterwards and also counts against the minute window.
	completionReserve = 1_500

	// estimateDivisor converts request bytes to a token estimate. The naive
	// ~4 bytes/token understates mixed Vietnamese/English text with dense
	// business identifiers (observed as low as ~2.65), which caused
	// "Request too large" rejections. Estimate at ~2.6 bytes/token (i.e.
	// len*5/13) so admission is conservative and rarely under-reserves.
	estimateNumerator   = 5
	estimateDenominator = 13
)

// ErrDailyTokenBudget is terminal: retrying cannot help until the provider's
// daily window resets. Callers must not retry on it.
var ErrDailyTokenBudget = errors.New("provider daily token budget exhausted")

// estimateTokens approximates the cost of one request: prompt tokens (from
// the marshaled body) plus headroom for the completion.
func estimateTokens(requestBytes []byte) int {
	return len(requestBytes)*estimateNumerator/estimateDenominator + completionReserve
}

type budgetEvent struct {
	id     int
	at     time.Time
	tokens int
}

// Reservation holds admitted tokens until the actual usage is known.
type Reservation struct {
	id     int
	tokens int
}

// TokenBudget is a proactive token-rate gate for one provider credential.
// Limits are per API key, so all clients holding the same key must count
// against a single shared budget (see budgetFor).
type TokenBudget struct {
	mu        sync.Mutex
	window    []budgetEvent
	dayTokens int
	day       string // UTC date of dayTokens
	nextID    int

	now       func() time.Time
	windowLen time.Duration
	minLimit  int
	dayLimit  int
}

func newTokenBudget() *TokenBudget {
	return &TokenBudget{
		now:       time.Now,
		windowLen: tokenWindow,
		minLimit:  minuteTokenLimit,
		dayLimit:  dayTokenLimit,
	}
}

var budgetRegistry = struct {
	sync.Mutex
	m map[string]*TokenBudget
}{m: make(map[string]*TokenBudget)}

// budgetFor returns the shared token budget for one provider credential.
// Providers are constructed per resolution, but the provider limits apply to
// the API key, so every instance with the same baseURL+apiKey counts against
// one budget.
func budgetFor(baseURL, apiKey string) *TokenBudget {
	budgetRegistry.Lock()
	defer budgetRegistry.Unlock()

	key := baseURL + "\x00" + apiKey
	if b, ok := budgetRegistry.m[key]; ok {
		return b
	}
	b := newTokenBudget()
	budgetRegistry.m[key] = b
	return b
}

// dayKey is the UTC calendar day a token count belongs to.
func (b *TokenBudget) dayKey(now time.Time) string {
	return now.UTC().Format("2006-01-02")
}

// pruneLocked drops window events older than the window length and rolls the
// daily counter over on a new UTC day. Callers must hold b.mu.
func (b *TokenBudget) pruneLocked(now time.Time) {
	if b.day != b.dayKey(now) {
		b.day = b.dayKey(now)
		b.dayTokens = 0
	}
	kept := b.window[:0]
	for _, ev := range b.window {
		if now.Sub(ev.at) < b.windowLen {
			kept = append(kept, ev)
		}
	}
	b.window = kept
}

// windowTotalLocked sums the events still inside the window.
func (b *TokenBudget) windowTotalLocked() int {
	total := 0
	for _, ev := range b.window {
		total += ev.tokens
	}
	return total
}

// Reserve blocks until est tokens fit inside both limits, then holds them
// until Record or Release. est includes completionReserve. A request larger
// than the minute limit (a big MoM) is admitted only when the window is
// completely empty, so it gets a quiet minute to itself.
func (b *TokenBudget) Reserve(ctx context.Context, est int) (*Reservation, error) {
	for {
		b.mu.Lock()
		now := b.now()
		b.pruneLocked(now)

		if b.dayTokens+est > b.dayLimit {
			b.mu.Unlock()
			return nil, ErrDailyTokenBudget
		}

		total := b.windowTotalLocked()
		if total+est <= b.minLimit || (est > b.minLimit && total == 0) {
			b.nextID++
			res := &Reservation{id: b.nextID, tokens: est}
			b.window = append(b.window, budgetEvent{id: res.id, at: now, tokens: est})
			b.dayTokens += est
			b.mu.Unlock()
			return res, nil
		}

		// Wait until the oldest event leaves the window, then re-check.
		wait := b.windowLen
		for _, ev := range b.window {
			if d := ev.at.Add(b.windowLen).Sub(now); d < wait {
				wait = d
			}
		}
		b.mu.Unlock()

		if wait < 25*time.Millisecond {
			wait = 25 * time.Millisecond
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

// Record replaces the reservation with the actual token usage reported by
// the provider (prompt + completion).
func (b *TokenBudget) Record(res *Reservation, actual int) {
	if res == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	b.dayTokens += actual - res.tokens
	res.tokens = actual
	for i := range b.window {
		if b.window[i].id == res.id {
			b.window[i].tokens = actual
			return
		}
	}
}

// Release drops a reservation without recording usage, for requests the
// provider rejected or never received (they are not billed).
func (b *TokenBudget) Release(res *Reservation) {
	if res == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	b.dayTokens -= res.tokens
	for i := range b.window {
		if b.window[i].id == res.id {
			b.window = append(b.window[:i], b.window[i+1:]...)
			return
		}
	}
}
