package meetings

import (
	"encoding/json"
	"time"

	"github.com/user/realtime-meeting-ast/backend/internal/types"
)

type Meeting struct {
	ID    int    `json:"id"`
	Title string `json:"title"`
	// OwnerEmail is the authenticated user who created the meeting. It
	// determines which STT/LLM providers and keys apply to the meeting. It is
	// deliberately not serialized: meetings are visible to every allowlisted
	// user, so ownership is exposed only through the admin API.
	OwnerEmail string                  `json:"-"`
	ProjectID  *int                    `json:"project_id,omitempty"`
	Status     string                  `json:"status"`
	StartedAt  *time.Time              `json:"started_at,omitempty"`
	EndedAt    *time.Time              `json:"ended_at,omitempty"`
	Transcript []types.TranscriptEvent `json:"transcript,omitempty"`
	Summary    *types.MeetingSummary   `json:"summary,omitempty"`
	CreatedAt  time.Time               `json:"created_at"`
	UpdatedAt  time.Time               `json:"updated_at"`
}

type MeetingStatus string

const (
	MeetingStatusPending   MeetingStatus = "pending"
	MeetingStatusActive    MeetingStatus = "active"
	MeetingStatusPaused    MeetingStatus = "paused"
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
