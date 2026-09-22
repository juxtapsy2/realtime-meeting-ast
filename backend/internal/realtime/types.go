package realtime

import "github.com/user/realtime-meeting-ast/backend/internal/types"

// MeetingService is the interface for meeting operations needed by the realtime package
type MeetingService interface {
	GetMeetingByID(id int) (*types.MeetingInfo, error)
	StartMeeting(id int) error
	EndMeeting(id int) error
	SaveTranscriptSegment(meetingID int, segment *types.TranscriptEvent) error
}
