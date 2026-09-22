package types

// MeetingInfo is a minimal meeting info used across packages
type MeetingInfo struct {
	ID     int    `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status"`
}
