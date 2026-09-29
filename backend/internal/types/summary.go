package types

// MeetingSummary is the structured AI summary produced when a meeting ends.
type MeetingSummary struct {
	Title      string `json:"title"`
	Summary    string `json:"summary"`
	MomEntries []struct {
		ID       string `json:"id"`
		Title    string `json:"title"`
		Status   string `json:"status"`
		Actions  string `json:"actions,omitempty"`
		Assignee string `json:"assignee,omitempty"`
		ETA      string `json:"eta,omitempty"`
	} `json:"mom_entries,omitempty"`
	KeyPoints []string `json:"key_points,omitempty"`
	Decisions []struct {
		ID               string   `json:"id"`
		Title            string   `json:"title"`
		Description      string   `json:"description,omitempty"`
		Status           string   `json:"status"`
		Confidence       float64  `json:"confidence"`
		SourceSegmentIDs []string `json:"source_segment_ids,omitempty"`
	} `json:"decisions,omitempty"`
	ActionItems []struct {
		ID               string   `json:"id"`
		Description      string   `json:"description"`
		Assignee         string   `json:"assignee,omitempty"`
		DueDate          string   `json:"due_date,omitempty"`
		Status           string   `json:"status"`
		SourceSegmentIDs []string `json:"source_segment_ids,omitempty"`
	} `json:"action_items,omitempty"`
	Issues []struct {
		ID               string   `json:"id"`
		Title            string   `json:"title"`
		Description      string   `json:"description,omitempty"`
		Status           string   `json:"status"`
		SourceSegmentIDs []string `json:"source_segment_ids,omitempty"`
	} `json:"issues,omitempty"`
	Questions []struct {
		ID               string   `json:"id"`
		Question         string   `json:"question"`
		Status           string   `json:"status"`
		SourceSegmentIDs []string `json:"source_segment_ids,omitempty"`
	} `json:"questions,omitempty"`
	Participants []string `json:"participants,omitempty"`
	Duration     float64  `json:"duration,omitempty"`
}
