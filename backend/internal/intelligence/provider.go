package intelligence

import (
	"context"
)

// IntelligenceProvider is the interface for AI analysis providers
type IntelligenceProvider interface {
	// AnalyzeChunk analyzes a chunk of transcript and returns meeting state updates
	AnalyzeChunk(ctx context.Context, input AnalysisInput) (MeetingStatePatch, error)

	// FinalizeMeeting generates a final meeting summary
	FinalizeMeeting(ctx context.Context, input FinalizationInput) (MeetingSummary, error)
}

// AnalysisInput contains the data for intelligence analysis
type AnalysisInput struct {
	MeetingID       string                 `json:"meeting_id"`
	TranscriptChunk string                 `json:"transcript_chunk"`
	RecentContext   string                 `json:"recent_context"`
	CurrentState    *MeetingState          `json:"current_state"`
	ProjectContext  map[string]interface{} `json:"project_context,omitempty"`
}

// MeetingStatePatch represents updates to the meeting state
type MeetingStatePatch struct {
	CurrentTopic *string        `json:"current_topic,omitempty"`
	Topics       []Topic        `json:"topics,omitempty"`
	Decisions    []Decision     `json:"decisions,omitempty"`
	ActionItems  []ActionItem   `json:"action_items,omitempty"`
	Issues       []Issue        `json:"issues,omitempty"`
	Questions    []OpenQuestion `json:"questions,omitempty"`
}

// MeetingState represents the current state of a meeting
type MeetingState struct {
	CurrentTopic string         `json:"current_topic"`
	Topics       []Topic        `json:"topics"`
	Decisions    []Decision     `json:"decisions"`
	ActionItems  []ActionItem   `json:"action_items"`
	Issues       []Issue        `json:"issues"`
	Questions    []OpenQuestion `json:"questions"`
}

// Topic represents a discussion topic
type Topic struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	Keywords []string `json:"keywords,omitempty"`
}

// Decision represents a decision made in the meeting
type Decision struct {
	ID               string   `json:"id"`
	Title            string   `json:"title"`
	Description      string   `json:"description,omitempty"`
	Status           string   `json:"status"` // proposed, confirmed, superseded
	Confidence       float64  `json:"confidence"`
	SourceSegmentIDs []string `json:"source_segment_ids,omitempty"`
}

// ActionItem represents an action item from the meeting
type ActionItem struct {
	ID               string   `json:"id"`
	Description      string   `json:"description"`
	Assignee         string   `json:"assignee,omitempty"`
	DueDate          string   `json:"due_date,omitempty"`
	Status           string   `json:"status"` // pending, completed
	SourceSegmentIDs []string `json:"source_segment_ids,omitempty"`
}

// Issue represents an issue discussed in the meeting
type Issue struct {
	ID               string   `json:"id"`
	Title            string   `json:"title"`
	Description      string   `json:"description,omitempty"`
	Status           string   `json:"status"` // open, resolved
	SourceSegmentIDs []string `json:"source_segment_ids,omitempty"`
}

// OpenQuestion represents an open question from the meeting
type OpenQuestion struct {
	ID               string   `json:"id"`
	Question         string   `json:"question"`
	Status           string   `json:"status"` // open, resolved
	SourceSegmentIDs []string `json:"source_segment_ids,omitempty"`
}

// FinalizationInput contains data for meeting finalization
type FinalizationInput struct {
	MeetingID      string        `json:"meeting_id"`
	FullTranscript string        `json:"full_transcript"`
	FinalState     *MeetingState `json:"final_state"`
	Title          string        `json:"title,omitempty"`
	Glossary       Glossary      `json:"-"`
}

// MOMEntry is one item in the "current status → next actions" minutes-of-meeting
// style summary entry.
type MOMEntry struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Status   string `json:"status"`
	Actions  string `json:"actions,omitempty"`
	Assignee string `json:"assignee,omitempty"`
	ETA      string `json:"eta,omitempty"`
}

// MeetingSummary is the final meeting summary
type MeetingSummary struct {
	Title        string         `json:"title"`
	Summary      string         `json:"summary"`
	MOMEntries   []MOMEntry     `json:"mom_entries"`
	KeyPoints    []string       `json:"key_points"`
	Decisions    []Decision     `json:"decisions"`
	ActionItems  []ActionItem   `json:"action_items"`
	Issues       []Issue        `json:"issues"`
	Questions    []OpenQuestion `json:"questions"`
	Participants []string       `json:"participants,omitempty"`
	Duration     float64        `json:"duration,omitempty"`
}

// Provider represents the type of intelligence provider
type Provider string

const (
	ProviderOpenAI Provider = "openai"
	ProviderGemini Provider = "gemini"
	ProviderGroq   Provider = "groq"
)

// NewProvider creates a new intelligence provider
// NewProvider builds the intelligence provider for a single selection. The
// model is passed explicitly so each user's own configuration is honoured
// without mutating process-wide state; an empty model uses the provider default.
func NewProvider(providerType Provider, apiKey, model string, glossary Glossary) (IntelligenceProvider, error) {
	switch providerType {
	case ProviderGemini:
		return NewGeminiIntelligence(apiKey, model, glossary)
	case ProviderGroq:
		return NewGroqIntelligence(apiKey, model, glossary)
	case ProviderOpenAI:
		return NewOpenAIIntelligence(apiKey, model, glossary)
	default:
		return NewOpenAIIntelligence(apiKey, model, glossary)
	}
}
