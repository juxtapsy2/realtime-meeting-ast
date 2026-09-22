package types

// TranscriptEvent represents a normalized transcript segment
type TranscriptEvent struct {
	MeetingID  string   `json:"meeting_id"`
	SegmentID  string   `json:"segment_id"`
	Text       string   `json:"text"`
	SpeakerID  *string  `json:"speaker_id,omitempty"`
	StartTime  float64  `json:"start_time"`
	EndTime    float64  `json:"end_time"`
	Confidence *float64 `json:"confidence,omitempty"`
	Final      bool     `json:"final"`
}
