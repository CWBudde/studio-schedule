package mcpserver

import (
	"context"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/httpapi"
	"github.com/cwbudde/studio-schedule/internal/service"
)

// lessonTools are the person tools of a lesson: its participants.
func lessonTools() []tool {
	return []tool{{
		name: "lesson",
		description: "The participants of one lesson as staff see them (listLessonParticipants; no command " +
			"mirrors it): the lesson's capacity, active bookings, places held by waiting-list invites, free " +
			"places, waiting entries and when the attendance window ends; every booking, canceled ones " +
			"included, with the participant's name, contact email and phone, status, source, trial, late " +
			"cancellation and payment; and the waiting list, ended entries included, in its order. The " +
			"office sees every lesson, an instructor or accompanist only the lessons they teach or " +
			"accompany; any other lesson is not found. An anonymized person is marked anonymized. The " +
			"lesson IDs come from the schedule tool.",
		access:       httpapi.AccessStaff,
		personalData: true,
		annotations:  readOnly("Lesson participants"),
		add:          handler(lesson),
	}}
}

// lessonInput are the arguments of the lesson tool.
type lessonInput struct {
	LessonID string `json:"lessonId" jsonschema:"The lesson (its ID, as the schedule lists it)."`
}

// lesson is the lesson tool: the body of listLessonParticipants for p,
// whose own lessons an instructor sees only.
func lesson(ctx context.Context, s *Server, p service.Principal, in lessonInput) (any, error) {
	id, err := requiredID[domain.LessonID]("lessonId", in.LessonID)
	if err != nil {
		return nil, err
	}

	lp, err := s.svc.Participants.Participants(ctx, p, id)
	if err != nil {
		return nil, err
	}

	return httpapi.NewLessonParticipants(lp), nil
}

// requiredID parses the ID argument field, which must be given. The error
// names the field, not the value.
func requiredID[I domain.AnyID](field, value string) (I, error) {
	if value == "" {
		return I{}, &domain.FieldError{Field: field, Msg: field + " is required"}
	}

	return parseID[I](field, value)
}
