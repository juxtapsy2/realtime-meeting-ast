package meetings

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/user/realtime-meeting-ast/backend/internal/intelligence"
	"github.com/user/realtime-meeting-ast/backend/internal/types"
	"github.com/user/realtime-meeting-ast/backend/internal/transcription"
)

// Broadcaster is the interface for broadcasting events to connected clients
type Broadcaster interface {
	BroadcastToMeeting(meetingID int, eventType string, data interface{})
}

type Service struct {
	repo         *Repository
	broadcaster  Broadcaster
	transcriber  transcription.Transcriber
	intelligence intelligence.IntelligenceProvider
}

func NewService(repo *Repository, broadcaster Broadcaster, transcriber transcription.Transcriber, intelligence intelligence.IntelligenceProvider) *Service {
	return &Service{
		repo:         repo,
		broadcaster:  broadcaster,
		transcriber:  transcriber,
		intelligence: intelligence,
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

type TranscriptResponse struct {
	Segments []TranscriptSegment `json:"segments"`
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

	s.broadcaster.BroadcastToMeeting(id, "meeting.started", meeting)

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

	s.broadcaster.BroadcastToMeeting(id, "meeting.ended", meeting)

	return nil
}

func (s *Service) SaveTranscriptSegment(meetingID int, segment *types.TranscriptEvent) error {
	query := `
		INSERT INTO transcript_segments (meeting_id, segment_id, speaker_id, text, start_time, end_time, confidence, is_final, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (meeting_id, segment_id) DO UPDATE
		SET text = $4, confidence = $7, is_final = $8`

	_, err := s.repo.DB().Exec(query,
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

func (s *Service) GetTranscriptSegments(meetingID int) ([]TranscriptSegment, error) {
	return s.repo.GetTranscriptSegments(meetingID)
}

func (s *Service) GetTranscript(w http.ResponseWriter, r *http.Request) {
	id, err := extractIDFromPath(r.URL.Path)
	if err != nil {
		http.Error(w, "Invalid meeting ID", http.StatusBadRequest)
		return
	}

	segments, err := s.repo.GetTranscriptSegments(id)
	if err != nil {
		http.Error(w, "Failed to get transcript", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(TranscriptResponse{Segments: segments})
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

func extractIDFromPath(path string) (int, error) {
	parts := strings.Split(path, "/")
	for _, part := range parts {
		if id, err := strconv.Atoi(part); err == nil {
			return id, nil
		}
	}
	return 0, strconv.ErrSyntax
}
