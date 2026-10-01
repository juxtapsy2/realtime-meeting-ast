package types

// MeetingInfo is a minimal meeting info used across packages
type MeetingInfo struct {
	ID    int    `json:"id"`
	Title string `json:"title"`
	// OwnerEmail is the meeting owner, used to resolve which STT provider and
	// key apply to a live session. Not serialized: it is internal routing data,
	// not meeting content.
	OwnerEmail string `json:"-"`
	Status     string `json:"status"`
}
