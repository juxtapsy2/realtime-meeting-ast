package meetings

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/user/realtime-meeting-ast/backend/internal/intelligence"
	"github.com/user/realtime-meeting-ast/backend/internal/types"
)

// Broadcaster is the interface for broadcasting events to connected clients
type Broadcaster interface {
	BroadcastToMeeting(meetingID int, eventType string, data interface{})
}

type Service struct {
	repo         *Repository
	broadcaster  Broadcaster
	intelligence intelligence.IntelligenceProvider
	glossary     intelligence.Glossary
}

func NewService(repo *Repository, broadcaster Broadcaster, intelligence intelligence.IntelligenceProvider, glossary intelligence.Glossary) *Service {
	return &Service{
		repo:         repo,
		broadcaster:  broadcaster,
		intelligence: intelligence,
		glossary:     glossary,
	}
}

type CreateMeetingRequest struct {
	Title     string `json:"title"`
	ProjectID *int   `json:"project_id,omitempty"`
}

type MeetingResponse struct {
	Meeting *Meeting `json:"meeting"`
}

type MeetingsResponse struct {
	Meetings []*Meeting `json:"meetings"`
	Total    int        `json:"total"`
	Page     int        `json:"page"`
	Limit    int        `json:"limit"`
}

func (s *Service) CreateMeeting(w http.ResponseWriter, r *http.Request) {
	var req CreateMeetingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Title == "" {
		http.Error(w, "Title is required", http.StatusBadRequest)
		return
	}

	meeting := &Meeting{
		Title:     req.Title,
		ProjectID: req.ProjectID,
	}

	if err := s.repo.Create(meeting); err != nil {
		http.Error(w, "Failed to create meeting", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(MeetingResponse{Meeting: meeting})
}

func (s *Service) GetMeeting(w http.ResponseWriter, r *http.Request) {
	id, err := extractIDFromPath(r.URL.Path)
	if err != nil {
		http.Error(w, "Invalid meeting ID", http.StatusBadRequest)
		return
	}

	meeting, err := s.repo.GetByID(id)
	if err != nil {
		http.Error(w, "Meeting not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(MeetingResponse{Meeting: meeting})
}

func (s *Service) ListMeetings(w http.ResponseWriter, r *http.Request) {
	page, limit := parsePagination(r)
	meetings, total, err := s.repo.List(limit, (page-1)*limit)
	if err != nil {
		http.Error(w, "Failed to list meetings", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(MeetingsResponse{Meetings: meetings, Total: total, Page: page, Limit: limit})
}

// parsePagination reads page/limit from query params with sensible defaults and
// caps. page is 1-based, limit is clamped to [1, 100].
func parsePagination(r *http.Request) (int, int) {
	page := intParam(r, "page", 1)
	if page < 1 {
		page = 1
	}
	limit := intParam(r, "limit", 20)
	if limit < 1 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	return page, limit
}

func intParam(r *http.Request, key string, def int) int {
	v := r.URL.Query().Get(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func (s *Service) UpdateMeeting(w http.ResponseWriter, r *http.Request) {
	id, err := extractIDFromPath(r.URL.Path)
	if err != nil {
		http.Error(w, "Invalid meeting ID", http.StatusBadRequest)
		return
	}

	meeting, err := s.repo.GetByID(id)
	if err != nil {
		http.Error(w, "Meeting not found", http.StatusNotFound)
		return
	}

	var req CreateMeetingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Title != "" {
		meeting.Title = req.Title
	}
	if req.ProjectID != nil {
		meeting.ProjectID = req.ProjectID
	}

	if err := s.repo.Update(meeting); err != nil {
		http.Error(w, "Failed to update meeting", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(MeetingResponse{Meeting: meeting})
}

func (s *Service) DeleteMeeting(w http.ResponseWriter, r *http.Request) {
	id, err := extractIDFromPath(r.URL.Path)
	if err != nil {
		http.Error(w, "Invalid meeting ID", http.StatusBadRequest)
		return
	}

	if err := s.repo.Delete(id); err != nil {
		http.Error(w, "Failed to delete meeting", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) StartMeeting(id int) error {
	if err := s.repo.StartMeeting(id); err != nil {
		return err
	}

	meeting, err := s.repo.GetByID(id)
	if err != nil {
		return err
	}

	s.broadcaster.BroadcastToMeeting(id, "meeting.started", meeting)

	return nil
}

func (s *Service) EndMeeting(id int) error {
	if err := s.repo.EndMeeting(id); err != nil {
		return err
	}

	meeting, err := s.repo.GetByID(id)
	if err != nil {
		return err
	}

	s.broadcaster.BroadcastToMeeting(id, "meeting.ended", meeting)

	if s.intelligence != nil {
		s.finalizeMeetingAsync(id)
	}

	return nil
}

func (s *Service) PauseMeeting(id int) error {
	if err := s.repo.PauseMeeting(id); err != nil {
		return err
	}

	meeting, err := s.repo.GetByID(id)
	if err != nil {
		return err
	}

	s.broadcaster.BroadcastToMeeting(id, "meeting.paused", meeting)

	return nil
}

func (s *Service) ResumeMeeting(id int) error {
	if err := s.repo.ResumeMeeting(id); err != nil {
		return err
	}

	meeting, err := s.repo.GetByID(id)
	if err != nil {
		return err
	}

	s.broadcaster.BroadcastToMeeting(id, "meeting.resumed", meeting)

	return nil
}

// generateSummaryWithRetry calls the intelligence provider with bounded retry
// and backoff. LLM/network failures are typically transient (rate limits,
// timeouts, empty responses), so a couple of retries usually recover the
// summary without blocking transcription.
func (s *Service) generateSummaryWithRetry(ctx context.Context, meetingID int, input intelligence.FinalizationInput) (intelligence.MeetingSummary, error) {
	const maxAttempts = 3

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		summary, err := s.intelligence.FinalizeMeeting(ctx, input)
		if err == nil {
			return summary, nil
		}
		lastErr = err
		if attempt == maxAttempts {
			break
		}
		log.Printf("Meeting %d: summary attempt %d/%d failed: %v; retrying", meetingID, attempt, maxAttempts, err)
		select {
		case <-time.After(time.Duration(attempt) * time.Second):
		case <-ctx.Done():
			return intelligence.MeetingSummary{}, ctx.Err()
		}
	}
	return intelligence.MeetingSummary{}, lastErr
}

// finalizeMeetingAsync generates and persists the meeting summary in the
// background so transcription/client flow is not blocked.
func (s *Service) finalizeMeetingAsync(meetingID int) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("Meeting %d: panic in finalizeMeetingAsync: %v", meetingID, r)
			}
		}()

		ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
		defer cancel()

		segments, err := s.GetTranscriptSegments(meetingID)
		if err != nil {
			log.Printf("Meeting %d: failed to load transcript for summary: %v", meetingID, err)
			return
		}

		parts := make([]string, 0, len(segments))
		for _, seg := range segments {
			if strings.TrimSpace(seg.Text) != "" {
				parts = append(parts, strings.TrimSpace(seg.Text))
			}
		}
		if len(parts) == 0 {
			return
		}

		meeting, err := s.repo.GetByID(meetingID)
		if err != nil {
			log.Printf("Meeting %d: failed to load meeting for summary: %v", meetingID, err)
		}

		input := intelligence.FinalizationInput{
			MeetingID:      strconv.Itoa(meetingID),
			FullTranscript: strings.Join(parts, " "),
			FinalState:     &intelligence.MeetingState{},
			Glossary:       s.glossary,
		}
		if meeting != nil {
			input.Title = meeting.Title
		}

		summary, err := s.generateSummaryWithRetry(ctx, meetingID, input)
		if err != nil {
			log.Printf("Meeting %d: failed to finalize summary: %v", meetingID, err)
			return
		}

		data, err := json.Marshal(summary)
		if err != nil {
			log.Printf("Meeting %d: failed to marshal summary: %v", meetingID, err)
			return
		}

		if err := s.repo.SaveSummary(meetingID, data); err != nil {
			log.Printf("Meeting %d: failed to persist summary: %v", meetingID, err)
			return
		}

		s.broadcaster.BroadcastToMeeting(meetingID, "meeting.summary", summary)
		log.Printf("Meeting %d: summary generated and persisted", meetingID)
	}()
}

func (s *Service) SaveTranscriptSegment(meetingID int, segment *types.TranscriptEvent) error {
	data, err := json.Marshal(segment)
	if err != nil {
		return fmt.Errorf("failed to marshal transcript segment: %w", err)
	}

	return s.repo.AppendTranscriptSegment(meetingID, data)
}

func (s *Service) GetTranscriptSegments(meetingID int) ([]types.TranscriptEvent, error) {
	data, err := s.repo.GetTranscript(meetingID)
	if err != nil {
		return nil, err
	}

	var segments []types.TranscriptEvent
	if len(data) > 0 {
		if err := json.Unmarshal(data, &segments); err != nil {
			return nil, fmt.Errorf("failed to parse transcript: %w", err)
		}
	}
	return segments, nil
}

func (s *Service) GetTranscript(w http.ResponseWriter, r *http.Request) {
	id, err := extractIDFromPath(r.URL.Path)
	if err != nil {
		http.Error(w, "Invalid meeting ID", http.StatusBadRequest)
		return
	}

	segments, err := s.repo.GetTranscript(id)
	if err != nil {
		http.Error(w, "Failed to get transcript", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(segments)
}

func (s *Service) GetMeetingByID(id int) (*types.MeetingInfo, error) {
	meeting, err := s.repo.GetByID(id)
	if err != nil {
		return nil, err
	}
	return &types.MeetingInfo{
		ID:     meeting.ID,
		Title:  meeting.Title,
		Status: meeting.Status,
	}, nil
}

// RegenerateSummary triggers background summary generation for a completed
// meeting that has no summary yet (e.g. a previous generation attempt failed).
// It is intentionally a no-go when a summary already exists.
func (s *Service) RegenerateSummary(w http.ResponseWriter, r *http.Request) {
	id, err := extractIDFromPath(r.URL.Path)
	if err != nil {
		http.Error(w, "Invalid meeting ID", http.StatusBadRequest)
		return
	}

	meeting, err := s.repo.GetByID(id)
	if err != nil {
		http.Error(w, "Meeting not found", http.StatusNotFound)
		return
	}

	if meeting.Summary != nil {
		http.Error(w, "Meeting already has a summary", http.StatusConflict)
		return
	}

	segments, err := s.repo.GetTranscript(id)
	if err != nil {
		http.Error(w, "Failed to load transcript", http.StatusInternalServerError)
		return
	}
	if len(segments) == 0 {
		http.Error(w, "Meeting has no transcript to summarize", http.StatusBadRequest)
		return
	}

	if s.intelligence == nil {
		http.Error(w, "AI summarization is not configured", http.StatusServiceUnavailable)
		return
	}

	s.finalizeMeetingAsync(id)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{"status": "generating"})
}

func extractIDFromPath(path string) (int, error) {
	parts := strings.Split(path, "/")
	for _, part := range parts {
		if id, err := strconv.Atoi(part); err == nil {
			return id, nil
		}
	}
	return 0, strconv.ErrSyntax
}
