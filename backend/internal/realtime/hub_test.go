package realtime

import (
	"encoding/json"
	"testing"
	"time"
)

func newTestClient(hub *Hub, meetingID int) *Client {
	return &Client{Hub: hub, MeetingID: meetingID, Send: make(chan []byte, 8)}
}

// nextEvent reads one event queued for a client, failing the test on timeout.
func nextEvent(t *testing.T, c *Client) Event {
	t.Helper()
	select {
	case raw := <-c.Send:
		var e Event
		if err := json.Unmarshal(raw, &e); err != nil {
			t.Fatalf("failed to decode event %q: %v", raw, err)
		}
		return e
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for an event")
		return Event{}
	}
}

// A claim grants the caller sole audio ownership, acknowledges with
// recording.unlocked, and is idempotent for the same connection.
func TestClaimAudioGrantsSoleOwnership(t *testing.T) {
	hub := NewHub()
	first := newTestClient(hub, 1)

	hub.ClaimAudio(1, first)
	if !hub.IsAudioOwner(1, first) {
		t.Fatal("claimant should hold the recording token")
	}
	if got := nextEvent(t, first); got.Type != "recording.unlocked" {
		t.Fatalf("claimant should be acknowledged, got %q", got.Type)
	}

	// Re-claiming by the same connection must not lock itself out.
	hub.ClaimAudio(1, first)
	if !hub.IsAudioOwner(1, first) {
		t.Fatal("re-claim by the same connection should keep ownership")
	}
	if got := nextEvent(t, first); got.Type != "recording.unlocked" {
		t.Fatalf("re-claim should be acknowledged, got %q", got.Type)
	}
	select {
	case raw := <-first.Send:
		t.Fatalf("unexpected extra event: %q", raw)
	default:
	}
}

// A second connection claiming the token takes over and the previous holder
// is told recording.locked — so exactly one tab can feed audio.
func TestSecondClaimTakesOverAndLocksPrevious(t *testing.T) {
	hub := NewHub()
	a := newTestClient(hub, 1)
	b := newTestClient(hub, 1)

	hub.ClaimAudio(1, a)
	if got := nextEvent(t, a); got.Type != "recording.unlocked" {
		t.Fatalf("first claimant should be acknowledged, got %q", got.Type)
	}

	hub.ClaimAudio(1, b)
	if got := nextEvent(t, a); got.Type != "recording.locked" {
		t.Fatalf("previous holder should be locked, got %q", got.Type)
	}
	if got := nextEvent(t, b); got.Type != "recording.unlocked" {
		t.Fatalf("new claimant should be acknowledged, got %q", got.Type)
	}
	if hub.IsAudioOwner(1, a) {
		t.Fatal("previous holder must not keep the recording token")
	}
	if !hub.IsAudioOwner(1, b) {
		t.Fatal("new claimant should hold the recording token")
	}
	// Ownership is per meeting: a claim elsewhere must not interfere.
	other := newTestClient(hub, 2)
	hub.ClaimAudio(2, other)
	if !hub.IsAudioOwner(1, b) {
		t.Fatal("claim on another meeting must not change this one")
	}
}

// Disconnecting releases the token: the next claimant starts clean instead of
// being blocked by a departed connection. Ownership fails closed while nil.
func TestDisconnectReleasesAudioOwnership(t *testing.T) {
	hub := NewHub()
	go hub.Run()

	c := newTestClient(hub, 7)
	hub.Register(c)

	// Register is processed asynchronously by Run; wait until it landed so
	// the later Unregister is not a no-op on an unknown client.
	waitRegistered(t, hub, c)

	hub.ClaimAudio(7, c)
	if !hub.IsAudioOwner(7, c) {
		t.Fatal("claimant should hold the recording token")
	}

	hub.Unregister(c)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !hub.IsAudioOwner(7, c) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("audio ownership was not released on disconnect")
}

func waitRegistered(t *testing.T, hub *Hub, c *Client) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		hub.mu.RLock()
		_, ok := hub.clients[c]
		hub.mu.RUnlock()
		if ok {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("client was never registered")
}

// The audio gate is the enforcement point: no claim means no audio, a
// non-owner is blocked (the duplicate-recording guard), and pause still
// overrides even the owner.
func TestAudioEnabledRequiresOwnership(t *testing.T) {
	hub := NewHub()
	a := newTestClient(hub, 1)
	b := newTestClient(hub, 1)

	// Fail closed: nobody may record without claiming first.
	if a.audioEnabled(1) {
		t.Fatal("audio must be dropped when no client holds the token")
	}

	hub.ClaimAudio(1, a)
	if !a.audioEnabled(1) {
		t.Fatal("the owner should be allowed to record")
	}
	if b.audioEnabled(1) {
		t.Fatal("a second connection must not record while another holds the token")
	}

	hub.SetMeetingPaused(1, true)
	if a.audioEnabled(1) {
		t.Fatal("even the owner must be blocked while the meeting is paused")
	}
}
