package meetings

import (
	"database/sql"
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
		SELECT id, title, project_id, status, started_at, ended_at, created_at, updated_at
		FROM meetings
		WHERE id = $1`

	meeting := &Meeting{}
	err := r.db.QueryRow(query, id).Scan(
		&meeting.ID,
		&meeting.Title,
		&meeting.ProjectID,
		&meeting.Status,
		&meeting.StartedAt,
		&meeting.EndedAt,
		&meeting.CreatedAt,
		&meeting.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("meeting not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get meeting: %w", err)
	}

	return meeting, nil
}

func (r *Repository) List() ([]*Meeting, error) {
	query := `
		SELECT id, title, project_id, status, started_at, ended_at, created_at, updated_at
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
		err := rows.Scan(
			&meeting.ID,
			&meeting.Title,
			&meeting.ProjectID,
			&meeting.Status,
			&meeting.StartedAt,
			&meeting.EndedAt,
			&meeting.CreatedAt,
			&meeting.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan meeting: %w", err)
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

func (r *Repository) GetTranscriptSegments(meetingID int) ([]TranscriptSegment, error) {
	query := `
		SELECT segment_id, speaker_id, text, start_time, end_time, confidence, is_final
		FROM transcript_segments
		WHERE meeting_id = $1
		ORDER BY start_time ASC`

	rows, err := r.db.Query(query, meetingID)
	if err != nil {
		return nil, fmt.Errorf("failed to get transcript segments: %w", err)
	}
	defer rows.Close()

	var segments []TranscriptSegment
	for rows.Next() {
		var seg TranscriptSegment
		err := rows.Scan(
			&seg.SegmentID,
			&seg.SpeakerID,
			&seg.Text,
			&seg.StartTime,
			&seg.EndTime,
			&seg.Confidence,
			&seg.IsFinal,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan transcript segment: %w", err)
		}
		segments = append(segments, seg)
	}

	return segments, nil
}

type TranscriptSegment struct {
	SegmentID  string
	SpeakerID  *string
	Text       string
	StartTime  float64
	EndTime    float64
	Confidence *float64
	IsFinal    bool
}
