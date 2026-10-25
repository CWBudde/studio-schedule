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

// registerParticipants registers the staff actions on the participants of
// a lesson (ADR 0007 and ADR 0009, amendments): the office on every lesson,
// an instructor or accompanist on their own (a 404 on any other), list,
// add, mark trials, remove and take attendance, and find persons to add;
// and the history of a booking for the office.
func registerParticipants(api huma.API, h *handlers) {
	write := func(op huma.Operation) huma.Operation {
		op.Tags = []string{"participants"}
		if op.Method == "" {
			op.Method = http.MethodPost
		}

		return withInvalid(api, op)
	}

	register(api, AccessStaff, huma.Operation{
		OperationID: "listLessonParticipants",
		Method:      http.MethodGet,
		Path:        BasePath + "/lessons/{lessonId}/participants",
		Summary:     "List a lesson's participants",
		Description: "Returns every booking of the lesson, canceled ones included, with the participant's name " +
			"and contact data, and its waiting list, ended entries included, with the counts. The office " +
			"sees every lesson, an instructor or accompanist the lessons they teach or accompany (their staff " +
			"record linked to their person); any other lesson is a 404.",
		Tags: []string{"participants"},
	}, h.listLessonParticipants)

	register(api, AccessStaff, write(huma.Operation{
		OperationID: "addLessonParticipant",
		Path:        BasePath + "/lessons/{lessonId}/participants",
		Summary:     "Add a participant",
		Description: "Books a person (`personId`) or a guest (`guest`, a name and an optional email) into the " +
			"lesson as staff (source `staff`), optionally as a `trial` with a `trialNote`. Unlike " +
			"`bookLesson`, the release window and the booking cutoff do not apply, and the lesson may have " +
			"started: instructors and accompanists add until the end of the lesson's school day (afterwards " +
			"a 422 on `path.lessonId` with reason `day-over`), the office at any time. A lesson the school canceled is a 409 " +
			"`closed` with why `canceled`. A person booked already is a 409 `already-booked`; a lesson whose " +
			"active bookings and the places held by others' waiting-list invites fill its capacity a 409 " +
			"`full`, unless `confirmOverCapacity` is set (the booking event records the override). A person " +
			"whose own invite holds a place takes it without the confirmation (an override if the lesson has " +
			"been overbooked since); any other entry of the person on the lesson's waiting list ends. Before " +
			"the lesson starts, the person's mail recipients, or a guest with an email (in the school's " +
			"language, asked to contact the school to cancel), get the confirmation. Lessons of others are " +
			"a 404, an unknown person a 422 on `body.personId`.",
		DefaultStatus: http.StatusCreated,
	}), h.addLessonParticipant)

	register(api, AccessStaff, write(huma.Operation{
		OperationID: "updateLessonParticipant",
		Method:      http.MethodPut,
		Path:        BasePath + "/bookings/{bookingId}/participant",
		Summary:     "Mark a participant's trial",
		Description: "Sets whether an active booking is a trial, and its note for the instructor (a note " +
			"without `trial` is a 422 on `body.trialNote`). Same access and time window as " +
			"`addLessonParticipant` (a 422 on `path.bookingId` with reason `day-over` once the day is over for instructors); a " +
			"canceled booking is a 409 `already-canceled`.",
	}), h.updateLessonParticipant)

	register(api, AccessStaff, write(huma.Operation{
		OperationID: "removeLessonParticipant",
		Path:        BasePath + "/bookings/{bookingId}/remove",
		Summary:     "Remove a participant",
		Description: "Cancels a booking of the lesson as staff: never late, and a person's date is released, so " +
			"a later change of their class membership does not book them into it again. Its place goes to the " +
			"waiting list. After the start too (a walk-in added by mistake), until the end of the lesson's " +
			"school day for instructors and accompanists (afterwards a 422 on `path.bookingId` with reason `day-over`), at any time " +
			"for the office, and on a lesson the school canceled. Before the start of a lesson that takes " +
			"place, the participant's mail recipients get the cancellation; for a canceled lesson nobody is " +
			"mailed or invited. A canceled booking is a 409 `already-canceled`; a booking of a lesson of " +
			"others a 404.",
	}), h.removeLessonParticipant)

	register(api, AccessStaff, write(huma.Operation{
		OperationID: "setAttendance",
		Path:        BasePath + "/bookings/{bookingId}/attendance",
		Summary:     "Take attendance",
		Description: "Sets an active booking to `attended`, `no_show` or back to `booked` (unknown). From the " +
			"lesson's start on (before it a 422 on `path.bookingId` with reason `not-started`), until the end of its school day for " +
			"instructors and accompanists (afterwards a 422 on `path.bookingId` with reason `day-over`), at any time after the start " +
			"for the office. Each change adds a booking event. A canceled booking is a 409 " +
			"`already-canceled`, a lesson the school canceled a 409 `closed` with why `canceled`.",
	}), h.setAttendance)

	register(api, AccessStaff, write(huma.Operation{
		OperationID: "markAllAttended",
		Path:        BasePath + "/lessons/{lessonId}/attendance/all",
		Summary:     "Mark everyone attended",
		Description: "Sets every booking of the lesson whose attendance is unknown (`booked`) to `attended`, " +
			"under the rules of `setAttendance` (the 422s on `path.lessonId` with reason `not-started` or `day-over`), and returns the bookings it " +
			"changed.",
	}), h.markAllAttended)

	register(api, AccessStaff, huma.Operation{
		OperationID: "searchPersonsForStaff",
		Method:      http.MethodGet,
		Path:        BasePath + "/staff/persons",
		Summary:     "Find persons to add",
		Description: "Returns the persons whose given or family name contains every word of `q` (at least two " +
			"characters), ignoring case, ordered by family and given name; at most 20. Only the name and the " +
			"birth year, no contact data: for instructors and accompanists adding a person to their lesson " +
			"(`addLessonParticipant`). The office's picker is `listPersons`.",
		Tags: []string{"participants"},
	}, h.searchPersonsForStaff)

	register(api, AccessOffice, huma.Operation{
		OperationID: "listBookingEvents",
		Method:      http.MethodGet,
		Path:        BasePath + "/admin/bookings/{bookingId}/events",
		Summary:     "List a booking's history",
		Description: "Returns every change of the booking, oldest first, with the account that made it (the " +
			"admin when an admin was signed in as someone else; none for the system).",
		Tags: []string{"participants"},
	}, h.listBookingEvents)
}

// LessonParticipant is a booking of a lesson as staff see it.
type LessonParticipant struct {
	BookingID    domain.BookingID `doc:"The booking."                       json:"bookingId"`
	PersonID     domain.PersonID  `doc:"The booked person; absent for a guest." json:"personId,omitzero"`
	Name         string           `doc:"Given and family name of the person, or the guest's name." json:"name"`
	Anonymized   bool             `doc:"Whether the office anonymized the person (ADR 0016); its name is then the fixed one (Deleted person), which clients show translated." json:"anonymized,omitempty"`
	Guest        bool             `doc:"Whether this is a guest without a person record."           json:"guest"`
	ContactEmail string           `doc:"Where mails about the participant go first: the person's account, their own address or a guardian's account; a guest's address. Absent if none." json:"contactEmail,omitempty"`
	ContactPhone string           `doc:"The person's phone, else the first of their guardians' with one; absent if none." json:"contactPhone,omitempty"`
	Status       string           `doc:"booked: attendance unknown; attended; no_show; canceled." enum:"booked,canceled,attended,no_show" json:"status"`
	Source       string           `doc:"How the booking came about (see Booking)." enum:"self,member,staff" json:"source"`
	Trial        bool             `doc:"Whether this is a trial lesson."      json:"trial"`
	TrialNote    string           `doc:"Note on the trial for the instructor." json:"trialNote,omitempty"`
	Late         bool             `doc:"Whether the cancellation came after the cancellation cutoff." json:"late"`
	BookedAt     time.Time        `doc:"When the booking was made."           json:"bookedAt"`
	CanceledAt   time.Time        `doc:"When it was canceled, if so."         json:"canceledAt,omitzero"`
	Payment      BookingPayment   `doc:"How the booking is paid." json:"payment"`
}

// LessonWaitlistEntry is an entry of a lesson's waiting list as staff see
// it.
type LessonWaitlistEntry struct {
	EntryID    domain.WaitlistEntryID `doc:"The entry."                          json:"entryId"`
	PersonID   domain.PersonID        `doc:"The person on the list."             json:"personId"`
	Name       string                 `doc:"Given and family name of the person." json:"name"`
	Anonymized bool                   `doc:"Whether the office anonymized the person (ADR 0016); its name is then the fixed one (Deleted person), which clients show translated." json:"anonymized,omitempty"`
	Status     string                 `doc:"As in WaitlistEntry, except that an invite whose expiry has passed is expired." enum:"waiting,invited,accepted,expired,withdrawn" json:"status"`
	Position   int                    `doc:"Place among the waiting entries, 1 for the next to be invited; absent unless waiting." json:"position,omitzero" minimum:"1"`
	StayOnList bool                   `doc:"Whether an invite that expires or is declined puts the entry back at the end of the list." json:"stayOnList"`
	JoinedAt   time.Time              `doc:"When the entry joined the list, or went back to its end." json:"joinedAt"`
	ExpiresAt  time.Time              `doc:"Until when the invite reserves the place; only while invited." json:"expiresAt,omitzero"`
}

// LessonParticipants is the response body of listLessonParticipants.
type LessonParticipants struct {
	LessonID   domain.LessonID       `doc:"The lesson."                                   json:"lessonId"`
	Capacity   int                   `doc:"Places of the lesson."                         json:"capacity"`
	Active     int                   `doc:"Active (not canceled) bookings; more than the capacity after an override." json:"active"`
	Held       int                   `doc:"Places held by open waiting-list invites."    json:"held"`
	FreePlaces int                   `doc:"Capacity minus active bookings and held places, at least 0." json:"freePlaces"`
	Waiting    int                   `doc:"Entries waiting for an invite."                json:"waiting"`
	DayOverAt  time.Time             `doc:"When the attendance window of instructors and accompanists ends: the end of the lesson's school day, or its end if later. Attendance opens at the start." json:"dayOverAt"`
	Bookings   []LessonParticipant   `doc:"All bookings, canceled ones included, ordered by when they were made." json:"bookings" nullable:"false"`
	Waitlist   []LessonWaitlistEntry `doc:"The waiting list, ended entries included, in its order." json:"waitlist" nullable:"false"`
}

// LessonParticipantsOutput is the response of listLessonParticipants.
type LessonParticipantsOutput struct {
	Body LessonParticipants
}

// GuestData names a guest without a person record.
type GuestData struct {
	Name  string `doc:"The guest's name."                                 json:"name"            maxLength:"200" minLength:"1"`
	Email string `doc:"The guest's email, for the confirmation and the lesson mails." json:"email,omitempty" maxLength:"254" required:"false"`
}

// AddParticipantData is the request body of addLessonParticipant: exactly
// one of personId and guest.
type AddParticipantData struct {
	PersonID            domain.PersonID `doc:"The person to book."                    json:"personId,omitzero" required:"false"`
	Guest               *GuestData      `doc:"A guest to book instead of a person."   json:"guest,omitempty"   required:"false"`
	Trial               bool            `doc:"Book as a trial lesson."                json:"trial,omitempty"   required:"false"`
	TrialNote           string          `doc:"Note on the trial for the instructor; only with trial." json:"trialNote,omitempty" maxLength:"500" required:"false"`
	ConfirmOverCapacity bool            `doc:"Book even if the lesson is full (skips the 409 full); the booking event records the override." json:"confirmOverCapacity,omitempty" required:"false"`
}

// AddParticipantInput is the request of addLessonParticipant.
type AddParticipantInput struct {
	LessonID domain.LessonID `doc:"The lesson." path:"lessonId"`
	Body     AddParticipantData
}

// UpdateParticipantData is the request body of updateLessonParticipant.
type UpdateParticipantData struct {
	Trial     bool   `doc:"Whether the booking is a trial lesson."              json:"trial"`
	TrialNote string `doc:"Note on the trial for the instructor; only with trial." json:"trialNote,omitempty" maxLength:"500" required:"false"`
}

// UpdateParticipantInput is the request of updateLessonParticipant.
type UpdateParticipantInput struct {
	BookingID domain.BookingID `doc:"The booking." path:"bookingId"`
	Body      UpdateParticipantData
}

// AttendanceData is the request body of setAttendance.
type AttendanceData struct {
	Status string `doc:"attended, no_show, or booked to set it back to unknown." enum:"attended,no_show,booked" json:"status"`
}

// AttendanceInput is the request of setAttendance.
type AttendanceInput struct {
	BookingID domain.BookingID `doc:"The booking." path:"bookingId"`
	Body      AttendanceData
}

// StaffBooking is a booking as the staff actions return it: a person's or
// a guest's, with its trial.
type StaffBooking struct {
	ID         domain.BookingID `doc:"Booking ID."                          json:"id"`
	LessonID   domain.LessonID  `doc:"The lesson."                          json:"lessonId"`
	PersonID   domain.PersonID  `doc:"The booked person; absent for a guest." json:"personId,omitzero"`
	GuestName  string           `doc:"The guest's name; only for a guest."  json:"guestName,omitempty"`
	GuestEmail string           `doc:"The guest's email, if they gave one." json:"guestEmail,omitempty"`
	Status     string           `doc:"State of the booking."                enum:"booked,canceled,attended,no_show" json:"status"`
	Source     string           `doc:"How the booking came about (see Booking)." enum:"self,member,staff" json:"source"`
	Trial      bool             `doc:"Whether this is a trial lesson."       json:"trial"`
	TrialNote  string           `doc:"Note on the trial for the instructor." json:"trialNote,omitempty"`
	BookedAt   time.Time        `doc:"When the booking was made."           json:"bookedAt"`
	CanceledAt time.Time        `doc:"When it was canceled, if so."         json:"canceledAt,omitzero"`
	Late       bool             `doc:"Whether the cancellation came after the cancellation cutoff." json:"late"`
}

// StaffBookingOutput is the response of the staff actions on one booking.
type StaffBookingOutput struct {
	Body StaffBooking
}

// StaffBookingsOutput is the response of markAllAttended.
type StaffBookingsOutput struct {
	Body struct {
		Items []StaffBooking `doc:"The bookings marked attended." json:"items" nullable:"false"`
	}
}

// StaffPersonSearchInput is the request of searchPersonsForStaff.
type StaffPersonSearchInput struct {
	Query string `doc:"Words to look for in the names; at least two characters." maxLength:"100" minLength:"2" query:"q" required:"true"`
}

// StaffPersonMatch is a person found by searchPersonsForStaff: no contact
// data.
type StaffPersonMatch struct {
	ID        domain.PersonID `doc:"Person ID."                       json:"id"`
	Name      string          `doc:"Given and family name."           json:"name"`
	BirthYear int             `doc:"Year of birth; absent if unknown." json:"birthYear,omitzero"`
}

// StaffPersonMatchesOutput is the response of searchPersonsForStaff.
type StaffPersonMatchesOutput struct {
	Body struct {
		Items []StaffPersonMatch `doc:"Matching persons ordered by family and given name." json:"items" nullable:"false"`
	}
}

// BookingEvent is an entry in the history of a booking.
type BookingEvent struct {
	ID       domain.BookingEventID `doc:"Event ID."                                   json:"id"`
	At       time.Time             `doc:"When it happened."                           json:"at"`
	Status   string                `doc:"The booking's status from then on."         enum:"booked,canceled,attended,no_show" json:"status"`
	Late     bool                  `doc:"A cancellation after the cancellation cutoff." json:"late"`
	Override bool                  `doc:"A booking beyond the lesson's capacity."     json:"override"`
	Note     string                `doc:"waitlist: the place came from the waiting list." json:"note,omitempty"`
	Actor    *BookingEventActor    `doc:"Who did it; absent for the system (e.g. automatic class member bookings, or an invite accepted by its link)." json:"actor,omitempty"`
}

// BookingEventActor is the account behind a booking event.
type BookingEventActor struct {
	AccountID domain.AccountID `doc:"The account."                         json:"accountId"`
	Name      string           `doc:"Given and family name of its person." json:"name"`
	Email     string           `doc:"Its email."                           json:"email"`
}

// BookingEventsOutput is the response of listBookingEvents.
type BookingEventsOutput struct {
	Body struct {
		Items []BookingEvent `doc:"The history, oldest first." json:"items" nullable:"false"`
	}
}

func (h *handlers) listLessonParticipants(
	ctx context.Context, in *LessonRefInput,
) (*LessonParticipantsOutput, error) {
	p, _ := principalFrom(ctx) // AccessStaff

	lp, err := h.deps.Participants.Participants(ctx, p, in.LessonID)
	if err != nil {
		return nil, h.participantError(ctx, err, "path.lessonId")
	}

	body := LessonParticipants{
		LessonID: lp.Lesson.ID, Capacity: lp.Lesson.Capacity, Active: lp.Active, Held: lp.Held,
		FreePlaces: lp.FreePlaces(), Waiting: lp.Waiting, DayOverAt: lp.DayOverAt.UTC(),
		Bookings: make([]LessonParticipant, 0, len(lp.Bookings)), Waitlist: make([]LessonWaitlistEntry, 0, len(lp.Waitlist)),
	}

	for _, pt := range lp.Bookings {
		b := pt.Booking
		body.Bookings = append(body.Bookings, LessonParticipant{
			BookingID: b.ID, PersonID: b.PersonID, Name: pt.Name, Anonymized: pt.Anonymized, Guest: b.PersonID.IsZero(),
			ContactEmail: pt.ContactEmail, ContactPhone: pt.ContactPhone, Status: string(b.Status),
			Source: string(b.Source), Trial: b.Trial, TrialNote: b.TrialNote, Late: b.Late,
			BookedAt: b.BookedAt.UTC(), CanceledAt: b.CanceledAt.UTC(), Payment: paymentBody(pt.Payment),
		})
	}

	for _, w := range lp.Waitlist {
		e := w.Entry
		entry := LessonWaitlistEntry{
			EntryID: e.ID, PersonID: e.PersonID, Name: w.Name, Anonymized: w.Anonymized, Status: string(w.Status),
			Position:   w.Position,
			StayOnList: e.StayOnList, JoinedAt: e.JoinedAt.UTC(),
		}

		if w.Status == domain.WaitlistInvited {
			entry.ExpiresAt = e.ExpiresAt.UTC()
		}

		body.Waitlist = append(body.Waitlist, entry)
	}

	return &LessonParticipantsOutput{Body: body}, nil
}

func (h *handlers) addLessonParticipant(ctx context.Context, in *AddParticipantInput) (*StaffBookingOutput, error) {
	p, _ := principalFrom(ctx) // AccessStaff

	np := service.NewParticipant{
		PersonID: in.Body.PersonID, Trial: in.Body.Trial, TrialNote: in.Body.TrialNote,
		ConfirmOverCapacity: in.Body.ConfirmOverCapacity,
	}
	if in.Body.Guest != nil {
		np.GuestName, np.GuestEmail = in.Body.Guest.Name, in.Body.Guest.Email
	}

	if np.PersonID.IsZero() == (in.Body.Guest == nil) {
		return nil, huma.Error422UnprocessableEntity("validation failed", &huma.ErrorDetail{
			Message: "give either a person or a guest", Location: "body",
		})
	}

	return h.staffBookingOutput(ctx, "path.lessonId")(h.deps.Participants.Add(ctx, p, in.LessonID, np))
}

func (h *handlers) updateLessonParticipant(
	ctx context.Context, in *UpdateParticipantInput,
) (*StaffBookingOutput, error) {
	p, _ := principalFrom(ctx) // AccessStaff

	return h.staffBookingOutput(ctx, "path.bookingId")(h.deps.Participants.UpdateTrial(ctx, p, in.BookingID,
		service.ParticipantUpdate{Trial: in.Body.Trial, TrialNote: in.Body.TrialNote}))
}

func (h *handlers) removeLessonParticipant(ctx context.Context, in *BookingRefInput) (*StaffBookingOutput, error) {
	p, _ := principalFrom(ctx) // AccessStaff

	return h.staffBookingOutput(ctx, "path.bookingId")(h.deps.Participants.Remove(ctx, p, in.BookingID))
}

func (h *handlers) setAttendance(ctx context.Context, in *AttendanceInput) (*StaffBookingOutput, error) {
	p, _ := principalFrom(ctx) // AccessStaff

	return h.staffBookingOutput(ctx, "path.bookingId")(h.deps.Participants.SetAttendance(ctx, p, in.BookingID,
		domain.BookingStatus(in.Body.Status)))
}

func (h *handlers) markAllAttended(ctx context.Context, in *LessonRefInput) (*StaffBookingsOutput, error) {
	p, _ := principalFrom(ctx) // AccessStaff

	changed, err := h.deps.Participants.MarkAllAttended(ctx, p, in.LessonID)
	if err != nil {
		return nil, h.participantError(ctx, err, "path.lessonId")
	}

	out := &StaffBookingsOutput{}
	out.Body.Items = make([]StaffBooking, 0, len(changed))

	for _, b := range changed {
		out.Body.Items = append(out.Body.Items, staffBookingBody(b))
	}

	return out, nil
}

func (h *handlers) searchPersonsForStaff(
	ctx context.Context, in *StaffPersonSearchInput,
) (*StaffPersonMatchesOutput, error) {
	hits, err := h.deps.Participants.FindPersons(ctx, in.Query)
	if err != nil {
		return nil, h.toHTTPError(ctx, err)
	}

	out := &StaffPersonMatchesOutput{}
	out.Body.Items = make([]StaffPersonMatch, 0, len(hits))

	for _, p := range hits {
		out.Body.Items = append(out.Body.Items, StaffPersonMatch{ID: p.ID, Name: p.Name, BirthYear: p.BirthYear})
	}

	return out, nil
}

func (h *handlers) listBookingEvents(ctx context.Context, in *BookingRefInput) (*BookingEventsOutput, error) {
	p, _ := principalFrom(ctx) // AccessOffice

	events, err := h.deps.Participants.Events(ctx, p, in.BookingID)
	if err != nil {
		return nil, h.toHTTPError(ctx, err)
	}

	out := &BookingEventsOutput{}
	out.Body.Items = make([]BookingEvent, 0, len(events))

	for _, v := range events {
		e := v.Event
		item := BookingEvent{
			ID: e.ID, At: e.At.UTC(), Status: string(e.Status), Late: e.Late, Override: e.Override, Note: e.Note,
		}

		if !e.ActorAccountID.IsZero() {
			item.Actor = &BookingEventActor{AccountID: e.ActorAccountID, Name: v.ActorName, Email: v.ActorEmail}
		}

		out.Body.Items = append(out.Body.Items, item)
	}

	return out, nil
}

func (h *handlers) staffBookingOutput(
	ctx context.Context, at string,
) func(domain.Booking, error) (*StaffBookingOutput, error) {
	return func(b domain.Booking, err error) (*StaffBookingOutput, error) {
		if err != nil {
			return nil, h.participantError(ctx, err, at)
		}

		return &StaffBookingOutput{Body: staffBookingBody(b)}, nil
	}
}

func staffBookingBody(b domain.Booking) StaffBooking {
	return StaffBooking{
		ID: b.ID, LessonID: b.LessonID, PersonID: b.PersonID, GuestName: b.GuestName, GuestEmail: b.GuestEmail,
		Status: string(b.Status), Source: string(b.Source), Trial: b.Trial, TrialNote: b.TrialNote,
		BookedAt: b.BookedAt.UTC(), CanceledAt: b.CanceledAt.UTC(), Late: b.Late,
	}
}

// participantError maps the errors of the staff actions: the end of the
// instructors' window and attendance before the start to a 422 Invalid at
// the path parameter at with reason day-over or not-started, the rest like
// bookingError.
func (h *handlers) participantError(ctx context.Context, err error, at string) error {
	switch {
	case errors.Is(err, service.ErrDayOver):
		return invalid(reasonDayOver, "the lesson's day is over; the office changes it now", at)
	case errors.Is(err, service.ErrLessonNotStarted):
		return invalid(reasonNotStarted, "the lesson has not started", at)
	}

	return h.bookingError(ctx, err)
}
