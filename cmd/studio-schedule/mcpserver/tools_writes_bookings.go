package mcpserver

import (
	"context"
	"fmt"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/httpapi"
	"github.com/cwbudde/studio-schedule/internal/service"
)

// bookingWriteTools book a person into a lesson and cancel that booking as
// the office does on a lesson's participants page (addLessonParticipant,
// removeLessonParticipant; ADR 0009).
func bookingWriteTools() []tool {
	return []tool{{
		name: "book_lesson",
		description: "Book a person into a lesson as the office does on the lesson's participants page " +
			"(addLessonParticipant; no command mirrors it), with the booking source staff. The release window " +
			"and the booking cutoff do not apply, and the lesson may have started; a lesson the school " +
			"canceled, a room rental, an event's session, a person booked already and a full lesson are " +
			"refused: this tool never books over the capacity. A person whose own waiting-list invite holds " +
			"a place takes it, and their other entry on the lesson's list ends. Before the lesson starts, the " +
			"person's mail recipients get the confirmation (queued in the outbox). The person IDs come from " +
			"search_persons, the lesson IDs from schedule. Returns the booking.",
		access:       httpapi.AccessOffice,
		personalData: true,
		write:        WriteBookings,
		annotations:  writeAnnotations("Book a person into a lesson", false, true),
		add:          handler(bookLesson),
	}, {
		name: "cancel_booking",
		description: "Cancel the active booking of a person on a lesson as the office does on the lesson's " +
			"participants page (removeLessonParticipant; no command mirrors it): never late, its place goes " +
			"to the waiting list, and the person's date is released, so a later change of their class " +
			"membership does not book them into it again. It works after the start too and on a lesson the " +
			"school canceled. Before the start of a lesson that takes place, the person's mail recipients get " +
			"the cancellation (queued in the outbox). A person without an active booking on the lesson is " +
			"not found. Returns the canceled booking.",
		access:       httpapi.AccessOffice,
		personalData: true,
		write:        WriteBookings,
		annotations:  writeAnnotations("Cancel a person's booking", true, true),
		add:          handler(cancelBooking),
	}}
}

// bookingInput are the arguments of book_lesson and cancel_booking.
type bookingInput struct {
	LessonID string `json:"lessonId" jsonschema:"The lesson (its ID, as the schedule lists it)."`
	PersonID string `json:"personId" jsonschema:"The person (their ID, as search_persons returns it)."`
}

// parse returns the lesson and the person of in, both required.
func (in bookingInput) parse() (domain.LessonID, domain.PersonID, error) {
	lesson, err := requiredID[domain.LessonID]("lessonId", in.LessonID)
	if err != nil {
		return domain.LessonID{}, domain.PersonID{}, err
	}

	person, err := requiredID[domain.PersonID]("personId", in.PersonID)
	if err != nil {
		return domain.LessonID{}, domain.PersonID{}, err
	}

	return lesson, person, nil
}

// bookLesson is the book_lesson tool: ParticipantService.Add for a person,
// never over the capacity, and the body of addLessonParticipant.
func bookLesson(ctx context.Context, s *Server, p service.Principal, in bookingInput) (any, error) {
	lesson, person, err := in.parse()
	if err != nil {
		return nil, err
	}

	b, err := s.svc.Participants.Add(ctx, p, lesson, service.NewParticipant{PersonID: person})
	if err != nil {
		return nil, err
	}

	return httpapi.NewStaffBooking(b), nil
}

// cancelBooking is the cancel_booking tool: ParticipantService.Remove of
// the person's active booking on the lesson, found among the lesson's
// participants, and the body of removeLessonParticipant.
func cancelBooking(ctx context.Context, s *Server, p service.Principal, in bookingInput) (any, error) {
	lesson, person, err := in.parse()
	if err != nil {
		return nil, err
	}

	lp, err := s.svc.Participants.Participants(ctx, p, lesson)
	if err != nil {
		return nil, err
	}

	// A person has at most one active booking of a lesson (a unique
	// index); Remove refuses it if it was canceled meanwhile.
	var booking domain.BookingID

	for _, part := range lp.Bookings {
		if part.Booking.PersonID == person && part.Booking.Status != domain.BookingCanceled {
			booking = part.Booking.ID
		}
	}

	if booking.IsZero() {
		return nil, fmt.Errorf("%w: person %v has no active booking of lesson %v", domain.ErrNotFound, person, lesson)
	}

	b, err := s.svc.Participants.Remove(ctx, p, booking)
	if err != nil {
		return nil, err
	}

	return httpapi.NewStaffBooking(b), nil
}
