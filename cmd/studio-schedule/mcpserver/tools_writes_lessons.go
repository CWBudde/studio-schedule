package mcpserver

import (
	"context"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/httpapi"
	"github.com/cwbudde/studio-schedule/internal/service"
)

// lessonWriteTools cancel a lesson as the office's lesson page does (ADR
// 0007).
func lessonWriteTools() []tool {
	return []tool{{
		name: "cancel_lesson",
		description: "Cancel a lesson that has not started, e.g. when its instructor is ill, as the office's " +
			"lesson page does (cancelLesson; `lessons cancel --yes`): everyone with an active booking is " +
			"mailed, with the note if given (the mails wait in the outbox for the server's deliver-mail job). " +
			"The bookings stay but cost nothing: their passes get the visits back. The waiting list's invites " +
			"go back to waiting. A lesson canceled already stays as it is, and nobody is mailed again; a " +
			"lesson that has started is refused. There is no way to cancel without the mails. Returns the " +
			"lesson as the schedule shows it to the office.",
		access:      httpapi.AccessOffice,
		write:       WriteLessons,
		annotations: writeAnnotations("Cancel a lesson", true, true),
		add:         handler(cancelLesson),
	}}
}

// cancelLessonInput are the arguments of cancel_lesson. The note's limit
// is domain.MaxCancelNoteLength (a test checks the description).
type cancelLessonInput struct {
	LessonID string `json:"lessonId"       jsonschema:"The lesson (its ID, as the schedule lists it)."`
	Note     string `json:"note,omitempty" jsonschema:"A note for those booked, e.g. the reason (at most 500 characters)."`
}

// cancelLesson is the cancel_lesson tool: LessonService.CancelLesson, and
// the body of cancelLesson, as `lessons cancel` prints it.
func cancelLesson(ctx context.Context, s *Server, p service.Principal, in cancelLessonInput) (any, error) {
	id, err := requiredID[domain.LessonID]("lessonId", in.LessonID)
	if err != nil {
		return nil, err
	}

	sl, err := s.svc.Lessons.CancelLesson(ctx, p, id, in.Note)
	if err != nil {
		return nil, err
	}

	return httpapi.NewLessonDetail(sl), nil
}
