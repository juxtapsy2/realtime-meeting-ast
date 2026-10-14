package types

import "time"

// MeetingDocument is a meeting material file (agenda, notes, report, ...)
// uploaded after the fact. Its extracted text is persisted for summary
// generation but never serialized to clients.
type MeetingDocument struct {
	Name       string    `json:"name"`
	Mime       string    `json:"mime"`
	Chars      int       `json:"chars"`
	UploadedAt time.Time `json:"uploaded_at"`
}
