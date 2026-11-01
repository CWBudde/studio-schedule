package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/service"
)

// registerBookings registers the members' bookings (ADR 0009): book a
// lesson for yourself or a dependent, cancel a booking, and cancel the
// following weeks of a class.
func registerBookings(api huma.API, h *handlers) {
	write := func(op huma.Operation) huma.Operation {
		op.Tags = []string{"bookings"}
		op.Method = http.MethodPost

		return withConflict(api, op)
	}

	register(api, AccessSignedIn, write(huma.Operation{
		OperationID: "bookLesson",
		Path:        BasePath + "/lessons/{lessonId}/bookings",
		Summary:     "Book a lesson",
		Description: "Books the lesson for the signed-in person or one of their dependents (managed ones " +
			"included). A person outside the family, or a lesson the caller may not see, is a 404. Everyone " +
			"books under member rules: staff get a 409 `closed` with why `release` for a lesson members may " +
			"not book yet (not released, beyond their early access) or at all (hidden). A canceled lesson is " +
			"a 409 `closed` with why `canceled`, one that has started a 409 `closed` with why `started`, and " +
			"one whose booking cutoff has passed a 409 `closed` with why `cutoff`, checked in this order. A " +
			"person booked already is a 409 `already-booked`, a lesson whose active bookings fill its " +
			"capacity a 409 `full`. The signed-in account gets a confirmation mail.",
		DefaultStatus: http.StatusCreated,
	}), h.bookLesson)

	register(api, AccessSignedIn, write(huma.Operation{
		OperationID: "cancelBooking",
		Path:        BasePath + "/bookings/{bookingId}/cancel",
		Summary:     "Cancel a booking",
		Description: "Cancels a booking of the signed-in person or one of their dependents (404 for any other " +
			"booking) and frees its place. A booking canceled already is a 409 `already-canceled`; one whose " +
			"lesson has started a 422 on `path.bookingId`. From the cancellation cutoff before the start on, " +
			"the cancellation is allowed but `late`, unless the school canceled the lesson. The signed-in " +
			"account gets a mail.",
	}), h.cancelBooking)

	// A read, but it answers the 409 already-canceled of cancelFollowing.
	register(api, AccessSignedIn, withConflict(api, huma.Operation{
		OperationID: "previewCancelFollowing",
		Method:      http.MethodGet,
		Path:        BasePath + "/bookings/{bookingId}/cancel-following",
		Summary:     "Preview canceling the following weeks",
		Description: "Returns what `cancelFollowing` would cancel now with `until`, without changing anything, " +
			"and fails like it (an `until` before the lesson's date is a 422 on `query.until`). Without " +
			"`until`, the response names the default: the last day of the class's term.",
		Tags: []string{"bookings"},
	}), h.previewCancelFollowing)

	register(api, AccessSignedIn, write(huma.Operation{
		OperationID: "cancelFollowing",
		Path:        BasePath + "/bookings/{bookingId}/cancel-following",
		Summary:     "Cancel the following weeks",
		Description: "Cancels a booking of a class lesson together with the same person's other active " +
			"bookings of that class from this lesson on up to the slot date `until` (default: the last day " +
			"of the class's term) whose lessons have not started, in one transaction, each `late` like in " +
			"`cancelBooking`. A person who is a member of the class stays one but is paused until then: " +
			"they are booked automatically again only into the lessons after it (`pausedUntil`). A booking " +
			"of a one-off lesson, or one whose lesson has started, is a 422 on `path.bookingId`; an " +
			"`until` before the lesson's date a 422 on `body.until`; a canceled booking a 409 " +
			"`already-canceled`; any other person's a 404. The signed-in account gets one mail listing " +
			"every canceled booking.",
	}), h.cancelFollowing)
}

// Booking is a booking of a person for a lesson.
type Booking struct {
	ID         domain.BookingID `doc:"Booking ID."                    json:"id"`
	LessonID   domain.LessonID  `doc:"The lesson."                    json:"lessonId"`
	PersonID   domain.PersonID  `doc:"The booked person."             json:"personId"`
	Status     string           `doc:"State of the booking."          enum:"booked,canceled,attended,no_show" json:"status"`
	Source     string           `doc:"How the booking came about: self, by a member; member, a class member's automatic booking; staff, by the office or the lesson's instructor or accompanist." enum:"self,member,staff" json:"source"`
	BookedAt   time.Time        `doc:"When the booking was made."     json:"bookedAt"`
	CanceledAt time.Time        `doc:"When it was canceled, if so."   json:"canceledAt,omitzero"`
	Late       bool             `doc:"Whether the cancellation came after the cancellation cutoff." json:"late"`
}

// BookLessonData is the request body of bookLesson.
type BookLessonData struct {
	PersonID domain.PersonID `doc:"Whom to book: the signed-in person or one of their dependents." json:"personId"`
}

// BookLessonInput is the request of bookLesson.
type BookLessonInput struct {
	LessonID domain.LessonID `doc:"The lesson." path:"lessonId"`
	Body     BookLessonData
}

// BookingRefInput is the request of the operations on one booking.
type BookingRefInput struct {
	BookingID domain.BookingID `doc:"The booking." path:"bookingId"`
}

// BookingOutput is the response of bookLesson and cancelBooking.
type BookingOutput struct {
	Body Booking
}

// CanceledBooking is a booking that cancelFollowing cancels.
type CanceledBooking struct {
	BookingID domain.BookingID `doc:"The booking."              json:"bookingId"`
	LessonID  domain.LessonID  `doc:"Its lesson."               json:"lessonId"`
	StartsAt  time.Time        `doc:"Start of the lesson (UTC)." json:"startsAt"`
	EndsAt    time.Time        `doc:"End of the lesson (UTC)."   json:"endsAt"`
	Late      bool             `doc:"Whether the cancellation is late." json:"late"`
}

// FollowingCancellation is the response body of previewCancelFollowing and
// cancelFollowing.
type FollowingCancellation struct {
	Bookings    []CanceledBooking `doc:"The bookings, ordered by the start of their lessons; the requested one first." json:"bookings" nullable:"false"`
	Until       domain.Date       `doc:"The last slot date canceled: the one asked for, or the last day of the class's term." format:"date" json:"until"`
	PausedUntil domain.Date       `doc:"Only for a member of the class: they stay a member, paused until this date (until), and are booked again automatically from the day after." format:"date" json:"pausedUntil,omitzero"`
}

// PreviewCancelFollowingInput is the request of previewCancelFollowing.
type PreviewCancelFollowingInput struct {
	BookingID domain.BookingID `doc:"The booking." path:"bookingId"`
	Until     domain.Date      `doc:"Last slot date to cancel; default: the last day of the class's term." format:"date" query:"until"`
}

// CancelFollowingData is the request body of cancelFollowing.
type CancelFollowingData struct {
	Until domain.Date `doc:"Last slot date to cancel; default: the last day of the class's term." format:"date" json:"until,omitzero" required:"false"`
}

// CancelFollowingInput is the request of cancelFollowing.
type CancelFollowingInput struct {
	BookingID domain.BookingID `doc:"The booking." path:"bookingId"`
	Body      *CancelFollowingData
}

// FollowingCancellationOutput is the response of previewCancelFollowing and
// cancelFollowing.
type FollowingCancellationOutput struct {
	Body FollowingCancellation
}

func (h *handlers) bookLesson(ctx context.Context, in *BookLessonInput) (*BookingOutput, error) {
	p, _ := principalFrom(ctx)

	return h.bookingOutput(ctx)(h.deps.Bookings.Book(ctx, p, in.LessonID, in.Body.PersonID))
}

func (h *handlers) cancelBooking(ctx context.Context, in *BookingRefInput) (*BookingOutput, error) {
	p, _ := principalFrom(ctx)

	return h.bookingOutput(ctx)(h.deps.Bookings.Cancel(ctx, p, in.BookingID))
}

func (h *handlers) previewCancelFollowing(
	ctx context.Context, in *PreviewCancelFollowingInput,
) (*FollowingCancellationOutput, error) {
	p, _ := principalFrom(ctx)

	return h.followingOutput(ctx, "query.until")(h.deps.Bookings.PreviewCancelFollowing(ctx, p, in.BookingID, in.Until))
}

func (h *handlers) cancelFollowing(ctx context.Context, in *CancelFollowingInput) (*FollowingCancellationOutput, error) {
	p, _ := principalFrom(ctx)

	var until domain.Date
	if in.Body != nil {
		until = in.Body.Until
	}

	return h.followingOutput(ctx, "body.until")(h.deps.Bookings.CancelFollowing(ctx, p, in.BookingID, until))
}

func (h *handlers) bookingOutput(ctx context.Context) func(domain.Booking, error) (*BookingOutput, error) {
	return func(b domain.Booking, err error) (*BookingOutput, error) {
		if err != nil {
			return nil, h.bookingError(ctx, err)
		}

		return &BookingOutput{Body: bookingBody(b)}, nil
	}
}

// followingOutput answers previewCancelFollowing and cancelFollowing; a bad
// until is a 422 at untilAt, where the request carries it.
func (h *handlers) followingOutput(
	ctx context.Context, untilAt string,
) func(service.FollowingCancellation, error) (*FollowingCancellationOutput, error) {
	return func(fc service.FollowingCancellation, err error) (*FollowingCancellationOutput, error) {
		var field *domain.FieldError

		switch {
		case errors.As(err, &field) && field.Field == "until":
			return nil, huma.Error422UnprocessableEntity("validation failed", &huma.ErrorDetail{
				Message: field.Msg, Location: untilAt,
			})
		case err != nil:
			return nil, h.bookingError(ctx, err)
		}

		body := FollowingCancellation{
			Bookings: make([]CanceledBooking, 0, len(fc.Bookings)), Until: fc.Until, PausedUntil: fc.PausedUntil,
		}
		for _, fb := range fc.Bookings {
			body.Bookings = append(body.Bookings, CanceledBooking{
				BookingID: fb.Booking.ID, LessonID: fb.Lesson.ID, StartsAt: fb.Lesson.StartsAt.UTC(),
				EndsAt: fb.Lesson.EndsAt.UTC(), Late: fb.Late,
			})
		}

		return &FollowingCancellationOutput{Body: body}, nil
	}
}

func bookingBody(b domain.Booking) Booking {
	return Booking{
		ID: b.ID, LessonID: b.LessonID, PersonID: b.PersonID, Status: string(b.Status), Source: string(b.Source),
		BookedAt: b.BookedAt.UTC(), CanceledAt: b.CanceledAt.UTC(), Late: b.Late,
	}
}

// bookingError maps the conflicts of the booking operations to their
// reasons, a started lesson or a one-off lesson to a 422 on the booking
// the path names, and everything else like toHTTPError.
func (h *handlers) bookingError(ctx context.Context, err error) error {
	var closed *service.BookingClosedError

	switch {
	case errors.As(err, &closed):
		c := conflict(reasonClosed, "the lesson cannot be booked")
		c.Why = string(closed.Why)

		return c
	case errors.Is(err, service.ErrEventSession):
		return conflict(reasonEventSession, "the lesson is a session of an event; register for the event")
	case errors.Is(err, service.ErrAlreadyBooked):
		return conflict(reasonAlreadyBooked, "the person is booked for the lesson already")
	case errors.Is(err, service.ErrLessonFull):
		return conflict(reasonFull, "the lesson is full")
	case errors.Is(err, service.ErrBookingCanceled):
		return conflict(reasonAlreadyCanceled, "the booking is canceled already")
	case errors.Is(err, service.ErrLessonStarted):
		return huma.Error422UnprocessableEntity("validation failed", &huma.ErrorDetail{
			Message: "the lesson has started", Location: "path.bookingId",
		})
	case errors.Is(err, service.ErrNotClassBooking):
		return huma.Error422UnprocessableEntity("validation failed", &huma.ErrorDetail{
			Message: "the booking is not for a lesson of a class", Location: "path.bookingId",
		})
	}

	return h.toHTTPError(ctx, err)
}
