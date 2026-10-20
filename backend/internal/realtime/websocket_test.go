package realtime

import "testing"

// A reconnect starts the provider's segment counter over, so the same
// provider ID must qualify differently per session — otherwise post-reconnect
// segments collide with pre-disconnect ones (same meeting, same DB).
func TestQualifySegmentIDUniqueAcrossSessions(t *testing.T) {
	first := qualifySegmentID("aaaa1111", "seg_1")
	second := qualifySegmentID("bbbb2222", "seg_1")

	if first == second {
		t.Errorf("same provider ID from two sessions collided: %q", first)
	}
	if first != "aaaa1111_seg_1" {
		t.Errorf("got %q, want %q", first, "aaaa1111_seg_1")
	}
}

// Partial transcripts carry no ID; they must not gain one.
func TestQualifySegmentIDEmptyStaysEmpty(t *testing.T) {
	if got := qualifySegmentID("aaaa1111", ""); got != "" {
		t.Errorf("qualifySegmentID(empty) = %q, want empty", got)
	}
}
