package meetings

import (
	"encoding/json"
	"time"
)

type Meeting struct {
	ID          int        `json:"id"`
	Title       string     `json:"title"`
	ProjectID   *int       `json:"project_id,omitempty"`
	Status      string     `json:"status"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	EndedAt     *time.Time `json:"ended_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

type MeetingStatus string

const (
	MeetingStatusPending   MeetingStatus = "pending"
	MeetingStatusActive    MeetingStatus = "active"
	MeetingStatusCompleted MeetingStatus = "completed"
)

func (m *Meeting) MarshalJSON() ([]byte, error) {
	type MeetingAlias Meeting
	return json.Marshal(&struct {
		*MeetingAlias
	}{
		MeetingAlias: (*MeetingAlias)(m),
	})
}
