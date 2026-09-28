package meetings

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
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
		INSERT INTO meetings (title, project_id, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id`

	now := time.Now()
	meeting.CreatedAt = now
	meeting.UpdatedAt = now
	if meeting.Status == "" {
		meeting.Status = string(MeetingStatusPending)
	}

	return r.db.QueryRow(query,
		meeting.Title,
		meeting.ProjectID,
		meeting.Status,
		meeting.CreatedAt,
		meeting.UpdatedAt,
	).Scan(&meeting.ID)
}

func (r *Repository) GetByID(id int) (*Meeting, error) {
	query := `
		SELECT id, title, project_id, status, started_at, ended_at, transcript, summary, created_at, updated_at
		FROM meetings
		WHERE id = $1`

	meeting := &Meeting{}
	var transcript []byte
	var summary []byte
	err := r.db.QueryRow(query, id).Scan(
		&meeting.ID,
		&meeting.Title,
		&meeting.ProjectID,
		&meeting.Status,
		&meeting.StartedAt,
		&meeting.EndedAt,
		&transcript,
		&summary,
		&meeting.CreatedAt,
		&meeting.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("meeting not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get meeting: %w", err)
	}

	if err := scanMeetingJSON(meeting, transcript, summary); err != nil {
		return nil, err
	}

	return meeting, nil
}

func (r *Repository) List() ([]*Meeting, error) {
	query := `
		SELECT id, title, project_id, status, started_at, ended_at, transcript, summary, created_at, updated_at
		FROM meetings
		ORDER BY created_at DESC`

	rows, err := r.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to list meetings: %w", err)
	}
	defer rows.Close()

	var meetings []*Meeting
	for rows.Next() {
		meeting := &Meeting{}
		var transcript []byte
		var summary []byte
		err := rows.Scan(
			&meeting.ID,
			&meeting.Title,
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
			return nil, fmt.Errorf("failed to scan meeting: %w", err)
		}
		if err := scanMeetingJSON(meeting, transcript, summary); err != nil {
			return nil, fmt.Errorf("failed to parse meeting %d: %w", meeting.ID, err)
		}
		meetings = append(meetings, meeting)
	}

	return meetings, nil
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
