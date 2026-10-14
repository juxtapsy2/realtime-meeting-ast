package meetings

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/user/realtime-meeting-ast/backend/internal/types"
)

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) DB() *sql.DB {
	return r.db
}

func (r *Repository) Create(meeting *Meeting) error {
	query := `
		INSERT INTO meetings (title, owner_email, project_id, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id`

	now := time.Now()
	meeting.CreatedAt = now
	meeting.UpdatedAt = now
	if meeting.Status == "" {
		meeting.Status = string(MeetingStatusPending)
	}

	// An empty owner is stored as SQL NULL so "no owner" stays distinguishable
	// from an empty string, and such meetings fall back to platform defaults.
	var owner *string
	if meeting.OwnerEmail != "" {
		owner = &meeting.OwnerEmail
	}

	return r.db.QueryRow(query,
		meeting.Title,
		owner,
		meeting.ProjectID,
		meeting.Status,
		meeting.CreatedAt,
		meeting.UpdatedAt,
	).Scan(&meeting.ID)
}

func (r *Repository) GetByID(id int) (*Meeting, error) {
	query := `
		SELECT id, title, owner_email, project_id, status, started_at, ended_at, transcript, summary, documents, created_at, updated_at
		FROM meetings
		WHERE id = $1`

	meeting := &Meeting{}
	var owner sql.NullString
	var transcript []byte
	var summary []byte
	var documents []byte
	err := r.db.QueryRow(query, id).Scan(
		&meeting.ID,
		&meeting.Title,
		&owner,
		&meeting.ProjectID,
		&meeting.Status,
		&meeting.StartedAt,
		&meeting.EndedAt,
		&transcript,
		&summary,
		&documents,
		&meeting.CreatedAt,
		&meeting.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("meeting not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get meeting: %w", err)
	}
	meeting.OwnerEmail = owner.String

	if err := scanMeetingJSON(meeting, transcript, summary); err != nil {
		return nil, err
	}
	if err := scanDocuments(meeting, documents); err != nil {
		return nil, err
	}

	return meeting, nil
}

// List returns a page of meetings ordered newest-first, and the total number of
// meetings. limit is the max page size; offset is the number of meetings to skip
// (0 for the first page).
func (r *Repository) List(limit, offset int) ([]*Meeting, int, error) {
	var total int
	if err := r.db.QueryRow(`SELECT COUNT(*) FROM meetings`).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count meetings: %w", err)
	}

	query := `
		SELECT id, title, owner_email, project_id, status, started_at, ended_at, transcript, summary, created_at, updated_at
		FROM meetings
		ORDER BY created_at DESC, id DESC
		LIMIT $1 OFFSET $2`

	rows, err := r.db.Query(query, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list meetings: %w", err)
	}
	defer rows.Close()

	var meetings []*Meeting
	for rows.Next() {
		meeting := &Meeting{}
		var owner sql.NullString
		var transcript []byte
		var summary []byte
		err := rows.Scan(
			&meeting.ID,
			&meeting.Title,
			&owner,
			&meeting.ProjectID,
			&meeting.Status,
			&meeting.StartedAt,
			&meeting.EndedAt,
			&transcript,
			&summary,
			&meeting.CreatedAt,
			&meeting.UpdatedAt,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to scan meeting: %w", err)
		}
		meeting.OwnerEmail = owner.String
		if err := scanMeetingJSON(meeting, transcript, summary); err != nil {
			return nil, 0, fmt.Errorf("failed to parse meeting %d: %w", meeting.ID, err)
		}
		meetings = append(meetings, meeting)
	}

	return meetings, total, nil
}

func (r *Repository) Update(meeting *Meeting) error {
	query := `
		UPDATE meetings
		SET title = $2, project_id = $3, status = $4, started_at = $5, ended_at = $6, updated_at = $7
		WHERE id = $1`

	meeting.UpdatedAt = time.Now()

	_, err := r.db.Exec(query,
		meeting.ID,
		meeting.Title,
		meeting.ProjectID,
		meeting.Status,
		meeting.StartedAt,
		meeting.EndedAt,
		meeting.UpdatedAt,
	)

	if err != nil {
		return fmt.Errorf("failed to update meeting: %w", err)
	}

	return nil
}

func (r *Repository) Delete(id int) error {
	query := `DELETE FROM meetings WHERE id = $1`

	result, err := r.db.Exec(query, id)
	if err != nil {
		return fmt.Errorf("failed to delete meeting: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("meeting not found")
	}

	return nil
}

func (r *Repository) StartMeeting(id int) error {
	query := `
		UPDATE meetings
		SET status = $2, started_at = $3, updated_at = $4
		WHERE id = $1`

	now := time.Now()
	_, err := r.db.Exec(query, id, MeetingStatusActive, now, now)
	if err != nil {
		return fmt.Errorf("failed to start meeting: %w", err)
	}

	return nil
}

func (r *Repository) EndMeeting(id int) error {
	query := `
		UPDATE meetings
		SET status = $2, ended_at = $3, updated_at = $4
		WHERE id = $1`

	now := time.Now()
	_, err := r.db.Exec(query, id, MeetingStatusCompleted, now, now)
	if err != nil {
		return fmt.Errorf("failed to end meeting: %w", err)
	}

	return nil
}

func (r *Repository) PauseMeeting(id int) error {
	query := `
		UPDATE meetings
		SET status = $2, updated_at = $3
		WHERE id = $1`

	_, err := r.db.Exec(query, id, MeetingStatusPaused, time.Now())
	if err != nil {
		return fmt.Errorf("failed to pause meeting: %w", err)
	}

	return nil
}

func (r *Repository) ResumeMeeting(id int) error {
	query := `
		UPDATE meetings
		SET status = $2, updated_at = $3
		WHERE id = $1`

	_, err := r.db.Exec(query, id, MeetingStatusActive, time.Now())
	if err != nil {
		return fmt.Errorf("failed to resume meeting: %w", err)
	}

	return nil
}

func scanMeetingJSON(meeting *Meeting, transcript []byte, summary []byte) error {
	if len(transcript) > 0 {
		if err := json.Unmarshal(transcript, &meeting.Transcript); err != nil {
			return fmt.Errorf("failed to parse transcript: %w", err)
		}
	}
	if len(summary) > 0 && string(summary) != "null" {
		if err := json.Unmarshal(summary, &meeting.Summary); err != nil {
			return fmt.Errorf("failed to parse summary: %w", err)
		}
	}
	return nil
}

func (r *Repository) AppendTranscriptSegment(meetingID int, segment []byte) error {
	query := `
		UPDATE meetings
		SET transcript = transcript || $2::jsonb, updated_at = NOW()
		WHERE id = $1`

	_, err := r.db.Exec(query, meetingID, string(segment))
	if err != nil {
		return fmt.Errorf("failed to append transcript segment: %w", err)
	}
	return nil
}

func (r *Repository) GetTranscript(meetingID int) ([]byte, error) {
	query := `SELECT transcript FROM meetings WHERE id = $1`

	var transcript []byte
	err := r.db.QueryRow(query, meetingID).Scan(&transcript)
	if err != nil {
		return nil, fmt.Errorf("failed to get transcript: %w", err)
	}
	return transcript, nil
}

func (r *Repository) SaveSummary(meetingID int, summary []byte) error {
	query := `
		UPDATE meetings
		SET summary = $2, updated_at = NOW()
		WHERE id = $1`

	_, err := r.db.Exec(query, meetingID, string(summary))
	if err != nil {
		return fmt.Errorf("failed to save summary: %w", err)
	}
	return nil
}

// storedDocument mirrors types.MeetingDocument but keeps the extracted text,
// which is persisted for summary generation and never leaves the server.
type storedDocument struct {
	Name       string    `json:"name"`
	Mime       string    `json:"mime"`
	Chars      int       `json:"chars"`
	UploadedAt time.Time `json:"uploaded_at"`
	Text       string    `json:"text"`
}

func (r *Repository) GetDocuments(meetingID int) ([]types.MeetingDocument, error) {
	query := `SELECT documents FROM meetings WHERE id = $1`

	var raw []byte
	if err := r.db.QueryRow(query, meetingID).Scan(&raw); err != nil {
		return nil, fmt.Errorf("failed to get documents: %w", err)
	}
	return decodeDocuments(raw)
}

func (r *Repository) GetStoredDocuments(meetingID int) ([]storedDocument, error) {
	query := `SELECT documents FROM meetings WHERE id = $1`

	var raw []byte
	if err := r.db.QueryRow(query, meetingID).Scan(&raw); err != nil {
		return nil, fmt.Errorf("failed to get documents: %w", err)
	}
	var stored []storedDocument
	if len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &stored); err != nil {
			return nil, fmt.Errorf("failed to parse documents: %w", err)
		}
	}
	return stored, nil
}

func (r *Repository) SaveDocuments(meetingID int, docs []storedDocument) error {
	data, err := json.Marshal(docs)
	if err != nil {
		return fmt.Errorf("failed to marshal documents: %w", err)
	}

	query := `
		UPDATE meetings
		SET documents = $2, updated_at = NOW()
		WHERE id = $1`

	_, err = r.db.Exec(query, meetingID, string(data))
	if err != nil {
		return fmt.Errorf("failed to save documents: %w", err)
	}
	return nil
}

func scanDocuments(meeting *Meeting, raw []byte) error {
	docs, err := decodeDocuments(raw)
	if err != nil {
		return err
	}
	if len(docs) > 0 {
		meeting.Documents = docs
	}
	return nil
}

func decodeDocuments(raw []byte) ([]types.MeetingDocument, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}

	var stored []storedDocument
	if err := json.Unmarshal(raw, &stored); err != nil {
		return nil, fmt.Errorf("failed to parse documents: %w", err)
	}

	docs := make([]types.MeetingDocument, 0, len(stored))
	for _, d := range stored {
		docs = append(docs, types.MeetingDocument{
			Name:       d.Name,
			Mime:       d.Mime,
			Chars:      d.Chars,
			UploadedAt: d.UploadedAt,
		})
	}
	return docs, nil
}
