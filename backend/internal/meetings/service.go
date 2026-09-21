package meetings

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/user/realtime-meeting-ast/backend/internal/realtime"
	"github.com/user/realtime-meeting-ast/backend/internal/transcription"
)

type Service struct {
	repo      *Repository
	hub       *realtime.Hub
	transcriber transcription.Transcriber
}

func NewService(repo *Repository, hub *realtime.Hub, transcriber transcription.Transcriber) *Service {
	return &Service{
		repo:      repo,
		hub:       hub,
		transcriber: transcriber,
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
	meetings, err := s.repo.List()
	if err != nil {
		http.Error(w, "Failed to list meetings", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(MeetingsResponse{Meetings: meetings})
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
	meeting, err := s.repo.GetByID(id)
	if err != nil {
		return err
	}

	if err := s.repo.StartMeeting(id); err != nil {
		return err
	}

	// Notify connected clients
	s.hub.BroadcastToMeeting(id, realtime.Event{
		Type: "meeting.started",
		Data: meeting,
	})

	return nil
}

func (s *Service) EndMeeting(id int) error {
	meeting, err := s.repo.GetByID(id)
	if err != nil {
		return err
	}

	if err := s.repo.EndMeeting(id); err != nil {
		return err
	}

	// Notify connected clients
	s.hub.BroadcastToMeeting(id, realtime.Event{
		Type: "meeting.ended",
		Data: meeting,
	})

	return nil
}

func (s *Service) SaveTranscriptSegment(meetingID int, segment *transcription.TranscriptEvent) error {
	query := `
		INSERT INTO transcript_segments (meeting_id, segment_id, speaker_id, text, start_time, end_time, confidence, is_final, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (meeting_id, segment_id) DO UPDATE
		SET text = $4, confidence = $7, is_final = $8`

	_, err := s.repo.db.Exec(query,
		meetingID,
		segment.SegmentID,
		segment.SpeakerID,
		segment.Text,
		segment.StartTime,
		segment.EndTime,
		segment.Confidence,
		segment.Final,
		time.Now(),
	)

	return err
}

func (s *Service) GetMeetingByID(id int) (*Meeting, error) {
	return s.repo.GetByID(id)
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
