package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/mail"
	"github.com/cwbudde/studio-schedule/internal/service"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// Lessons and persons of the demo data the booking tests write.
const (
	// bkJazz is today's Jazz Dance (2026-09-22, 17:00 school time), after
	// testNow, with three bookings: Kira Gildenmoor, Ella Amberly
	// (personsTestElla) and Emil Elderwick.
	bkJazz = "00000000-0000-7000-8000-0000000000cd"
	// bkEvening is a lesson later today with three bookings.
	bkEvening = "00000000-0000-7000-8000-0000000000ce"
	// bkRental is a room rental on 2026-09-26.
	bkRental = "00000000-0000-7000-8000-00000000032e"
	// bkClara is Clara Amberly (demoMember), booked on neither lesson and
	// not waiting for the full workshop.
	bkClara = "00000000-0000-7000-8000-000000000001"
)

// bkOptions are the options of a session with the booking tools.
var bkOptions = Options{PersonalData: true, AllowWrites: []WriteGroup{WriteBookings}}

// bkLessonID parses the lesson ID s.
func bkLessonID(t *testing.T, s string) domain.LessonID {
	t.Helper()

	id, err := domain.ParseID[domain.LessonID](s)
	if err != nil {
		t.Fatal(err)
	}

	return id
}

// bkBookings returns the bookings of lesson, canceled ones included.
func bkBookings(t *testing.T, f *fixture, lesson string) []domain.Booking {
	t.Helper()

	var bookings []domain.Booking

	err := f.st.InTx(t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		var err error
		bookings, err = tx.Bookings().ListByLesson(ctx, bkLessonID(t, lesson))

		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	return bookings
}

// bkActive counts the bookings that are not canceled.
func bkActive(bookings []domain.Booking) int {
	n := 0

	for _, b := range bookings {
		if b.Status != domain.BookingCanceled {
			n++
		}
	}

	return n
}

// bkBookingOf returns the booking of person among bookings, failing the
// test if there is none.
func bkBookingOf(t *testing.T, bookings []domain.Booking, person string) domain.Booking {
	t.Helper()

	i := slices.IndexFunc(bookings, func(b domain.Booking) bool { return b.PersonID.String() == person })
	if i < 0 {
		t.Fatalf("no booking of %s among %+v", person, bookings)
	}

	return bookings[i]
}

// bkEvents returns the events of booking, oldest first.
func bkEvents(t *testing.T, f *fixture, booking domain.BookingID) []domain.BookingEvent {
	t.Helper()

	var events []domain.BookingEvent

	err := f.st.InTx(t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		var err error
		events, err = tx.Bookings().ListEvents(ctx, booking)

		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	return events
}

// bkMails returns the recipients of the outbox's mails of template,
// newest first.
func bkMails(t *testing.T, f *fixture, template string) []string {
	t.Helper()

	var to []string

	for _, m := range mailOutbox(t, f) {
		if m.Template == template {
			to = append(to, m.To)
		}
	}

	return to
}

// bkWantCalls fails unless the mcp-tool-called events of f are one per
// outcome, in order, each of tool by actor over the MCP channel, without
// a target.
func bkWantCalls(t *testing.T, f *fixture, actor domain.AccountID, tool string, outcomes ...string) {
	t.Helper()

	calls := toolCalls(t, f.st)
	if len(calls) != len(outcomes) {
		t.Fatalf("%d tool calls audited, want %d: %+v", len(calls), len(outcomes), calls)
	}

	for i, e := range calls {
		want := domain.AuditDetails{Tool: tool, Outcome: outcomes[i], Via: via}
		if e.ActorID != actor || e.Details != want || e.TargetKind != "" || !e.TargetID.IsZero() ||
			!e.TargetPersonID.IsZero() {
			t.Errorf("call %d audited as %+v, want %+v by %v without a target", i, e, want, actor)
		}
	}
}

// bkFaultOnce fails the next read-write transaction with the error set by
// failNext, and passes every other on: the tool's write fails, the audit
// of the call that follows is written.
type bkFaultOnce struct {
	store.Store

	mu  sync.Mutex
	err error
}

func (s *bkFaultOnce) failNext(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.err = err
}

func (s *bkFaultOnce) InTx(ctx context.Context, mode store.TxMode, fn func(ctx context.Context, tx store.Tx) error) error {
	s.mu.Lock()

	var err error
	if mode == store.ReadWrite {
		err, s.err = s.err, nil
	}

	s.mu.Unlock()

	if err != nil {
		return err
	}

	return s.Store.InTx(ctx, mode, fn)
}

// TestBookLesson: the office books a person who is not booked into a
// future lesson. The result is the staff booking (addLessonParticipant's
// body) with the source staff; the store has it, its event names the
// office's account, the person is mailed the confirmation, and the call
// is audited.
func TestBookLesson(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	office := f.principal(t, demoOffice)
	cs := f.session(t, demoOffice, bkOptions, registry)
	confirmed := len(bkMails(t, f, mail.TemplateBookingConfirmed))

	text, isError := callTool(t, cs, "book_lesson", map[string]any{"lessonId": bkJazz, "personId": bkClara})
	if isError {
		t.Fatalf("book_lesson: error result %q", text)
	}

	golden(t, "book_lesson.json", text)
	lessonsTestNoBirthDate(t, "book_lesson", text)

	var doc map[string]any
	if err := json.Unmarshal([]byte(text), &doc); err != nil {
		t.Fatal(err)
	}

	if doc["lessonId"] != bkJazz || doc["personId"] != bkClara || doc["status"] != "booked" ||
		doc["source"] != "staff" || doc["late"] != false {
		t.Errorf("booking %v", doc)
	}

	bookings := bkBookings(t, f, bkJazz)
	if bkActive(bookings) != 4 {
		t.Errorf("%d active bookings, want 4", bkActive(bookings))
	}

	b := bkBookingOf(t, bookings, bkClara)
	if b.ID.String() != doc["id"] || b.Status != domain.BookingBooked || b.Source != domain.BookingSourceStaff ||
		!b.BookedAt.Equal(testNow) {
		t.Errorf("stored booking %+v, result %v", b, doc)
	}

	events := bkEvents(t, f, b.ID)
	if len(events) != 1 || events[0].ActorAccountID != office.AccountID || events[0].Status != domain.BookingBooked ||
		events[0].Override {
		t.Errorf("booking events %+v, want one by the office %v", events, office.AccountID)
	}

	if to := bkMails(t, f, mail.TemplateBookingConfirmed); len(to) != confirmed+1 || to[0] != demoMember {
		t.Errorf("confirmations %v, want one more, to %s", to, demoMember)
	}

	bkWantCalls(t, f, office.AccountID, "book_lesson", "ok")
}

// TestBookLessonFull: a full lesson is refused with the service's error,
// never booked over its capacity, and nobody is mailed (plan, "Booking
// rules").
func TestBookLessonFull(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	office := f.principal(t, demoOffice)
	cs := f.session(t, demoOffice, bkOptions, registry)
	before := bkBookings(t, f, lessonsTestWorkshop)
	confirmed := len(bkMails(t, f, mail.TemplateBookingConfirmed))

	if len(before) != 4 || bkActive(before) != 4 {
		t.Fatalf("the workshop has %d bookings, %d active; want it full with 4", len(before), bkActive(before))
	}

	wantError(t, cs, "book_lesson", map[string]any{"lessonId": lessonsTestWorkshop, "personId": bkClara},
		"add participant to lesson "+lessonsTestWorkshop+": "+service.ErrLessonFull.Error())

	if after := bkBookings(t, f, lessonsTestWorkshop); !slices.Equal(after, before) {
		t.Errorf("the bookings changed: %+v, want %+v", after, before)
	}

	if n := len(bkMails(t, f, mail.TemplateBookingConfirmed)); n != confirmed {
		t.Errorf("%d confirmations, want %d", n, confirmed)
	}

	bkWantCalls(t, f, office.AccountID, "book_lesson", "conflict")
}

// TestBookLessonRefused: a person booked already, a lesson the school
// canceled, a room rental, an unknown person and an unknown lesson are
// error results with the service's errors; nothing is booked.
func TestBookLessonRefused(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	office := f.principal(t, demoOffice)

	if _, err := f.svc.Lessons.CancelLesson(t.Context(), office, bkLessonID(t, bkEvening), ""); err != nil {
		t.Fatal(err)
	}

	cs := f.session(t, demoOffice, bkOptions, registry)
	jazz, evening, rental := bkBookings(t, f, bkJazz), bkBookings(t, f, bkEvening), bkBookings(t, f, bkRental)

	for _, tc := range []struct {
		lesson, person, text string
	}{
		{bkJazz, personsTestElla, "add participant to lesson " + bkJazz + ": " + service.ErrAlreadyBooked.Error()},
		{bkEvening, bkClara, "add participant to lesson " + bkEvening + ": the lesson cannot be booked: canceled"},
		{bkRental, bkClara, "add participant to lesson " + bkRental + ": the lesson cannot be booked: rental"},
		{bkJazz, lessonsTestUnknown, "add participant to lesson " + bkJazz + ": validation failed: person does not exist"},
		{lessonsTestUnknown, bkClara, "add participant to lesson " + lessonsTestUnknown + ": lesson: not found"},
	} {
		wantError(t, cs, "book_lesson", map[string]any{"lessonId": tc.lesson, "personId": tc.person}, tc.text)
	}

	if !slices.Equal(bkBookings(t, f, bkJazz), jazz) || !slices.Equal(bkBookings(t, f, bkEvening), evening) ||
		!slices.Equal(bkBookings(t, f, bkRental), rental) {
		t.Error("a refused booking changed the bookings")
	}

	bkWantCalls(t, f, office.AccountID, "book_lesson", "conflict", "conflict", "conflict", "invalid", "not-found")
}

// TestCancelBooking: the office cancels a person's active booking. The
// result is the canceled staff booking (removeLessonParticipant's body),
// never late; the store's booking is canceled and released, its event
// names the office's account, the person's recipients are mailed, and the
// call is audited. A second identical call finds no active booking and
// changes nothing.
func TestCancelBooking(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	office := f.principal(t, demoOffice)
	cs := f.session(t, demoOffice, bkOptions, registry)
	canceled := len(bkMails(t, f, mail.TemplateBookingCanceled))
	args := map[string]any{"lessonId": bkJazz, "personId": personsTestElla}

	text, isError := callTool(t, cs, "cancel_booking", args)
	if isError {
		t.Fatalf("cancel_booking: error result %q", text)
	}

	golden(t, "cancel_booking.json", text)
	lessonsTestNoBirthDate(t, "cancel_booking", text)

	var doc map[string]any
	if err := json.Unmarshal([]byte(text), &doc); err != nil {
		t.Fatal(err)
	}

	if doc["lessonId"] != bkJazz || doc["personId"] != personsTestElla || doc["status"] != "canceled" ||
		doc["late"] != false || doc["canceledAt"] != "2026-09-22T08:00:00Z" {
		t.Errorf("booking %v", doc)
	}

	bookings := bkBookings(t, f, bkJazz)
	b := bkBookingOf(t, bookings, personsTestElla)

	if b.ID.String() != doc["id"] || b.Status != domain.BookingCanceled || b.Late || !b.Released ||
		!b.CanceledAt.Equal(testNow) || bkActive(bookings) != 2 {
		t.Errorf("stored booking %+v, %d active", b, bkActive(bookings))
	}

	events := bkEvents(t, f, b.ID)
	if last := events[len(events)-1]; last.ActorAccountID != office.AccountID ||
		last.Status != domain.BookingCanceled || last.Late {
		t.Errorf("booking events %+v, want the last a cancellation by the office %v", events, office.AccountID)
	}

	// Ella is a child: her guardians, Clara and Tom Amberly, get one each.
	mails := bkMails(t, f, mail.TemplateBookingCanceled)
	if len(mails) != canceled+2 ||
		!slices.Equal(slices.Sorted(slices.Values(mails[:2])), []string{demoMember, "tom.amberly@example.org"}) {
		t.Errorf("cancellations %v, want two more, to Ella's guardians", mails)
	}

	// Again: the person has no active booking of the lesson any more.
	wantError(t, cs, "cancel_booking", args,
		"not found: person "+personsTestElla+" has no active booking of lesson "+bkJazz)

	if after := bkBookings(t, f, bkJazz); !slices.Equal(after, bookings) {
		t.Errorf("the second call changed the bookings: %+v", after)
	}

	if n := len(bkMails(t, f, mail.TemplateBookingCanceled)); n != len(mails) {
		t.Errorf("the second call mailed: %d cancellations, want %d", n, len(mails))
	}

	bkWantCalls(t, f, office.AccountID, "cancel_booking", "ok", "not-found")
}

// TestCancelBookingNotFound: a person without a booking of the lesson and
// an unknown lesson are not found; nothing changes.
func TestCancelBookingNotFound(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	office := f.principal(t, demoOffice)
	cs := f.session(t, demoOffice, bkOptions, registry)
	jazz := bkBookings(t, f, bkJazz)

	wantError(t, cs, "cancel_booking", map[string]any{"lessonId": bkJazz, "personId": bkClara},
		"not found: person "+bkClara+" has no active booking of lesson "+bkJazz)
	wantError(t, cs, "cancel_booking", map[string]any{"lessonId": lessonsTestUnknown, "personId": personsTestElla},
		"participants of lesson "+lessonsTestUnknown+": lesson: not found")

	if !slices.Equal(bkBookings(t, f, bkJazz), jazz) {
		t.Error("the bookings changed")
	}

	bkWantCalls(t, f, office.AccountID, "cancel_booking", "not-found", "not-found")
}

// TestCancelBookingStoreFails: the booking found, a store that fails while
// it is canceled is an unexpected error: "internal error" for the client,
// the cause in the log, the call audited as an error, the booking active.
func TestCancelBookingStoreFails(t *testing.T) {
	t.Parallel()

	st := &bkFaultOnce{Store: newFixture(t).st}
	f := fixtureOn(st)
	office := f.principal(t, demoOffice)
	cs := f.sessionOf(t, office, func(context.Context) (service.Principal, error) { return office, nil },
		bkOptions, registry)

	st.failNext(errors.New("connection reset"))

	wantError(t, cs, "cancel_booking", map[string]any{"lessonId": bkJazz, "personId": personsTestElla},
		"internal error")

	if log := f.log.String(); !strings.Contains(log, "connection reset") || !strings.Contains(log, "tool=cancel_booking") {
		t.Errorf("log %q", log)
	}

	if b := bkBookingOf(t, bkBookings(t, f, bkJazz), personsTestElla); b.Status != domain.BookingBooked {
		t.Errorf("booking %+v", b)
	}

	bkWantCalls(t, f, office.AccountID, "cancel_booking", "error")
}

// TestBookingToolsInput: both IDs are required and must be IDs; the
// errors name the field, never its value, and the value is neither in
// the log nor in the audit events. Nothing is booked or canceled.
func TestBookingToolsInput(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	office := f.principal(t, demoOffice)
	cs := f.session(t, demoOffice, bkOptions, registry)
	jazz := bkBookings(t, f, bkJazz)

	var outcomes []string

	for _, name := range []string{"book_lesson", "cancel_booking"} {
		for _, tc := range []struct {
			args any
			text string
		}{
			{nil, "validation failed: lessonId is required"},
			{map[string]any{"personId": bkClara}, "validation failed: lessonId is required"},
			{map[string]any{"lessonId": bkJazz}, "validation failed: personId is required"},
			{map[string]any{"lessonId": "Zebulon", "personId": bkClara}, "validation failed: lessonId is not an ID"},
			{map[string]any{"lessonId": bkJazz, "personId": "Zebulon"}, "validation failed: personId is not an ID"},
		} {
			wantError(t, cs, name, tc.args, tc.text)

			outcomes = append(outcomes, "invalid")
		}
	}

	if !slices.Equal(bkBookings(t, f, bkJazz), jazz) {
		t.Error("an invalid call changed the bookings")
	}

	if strings.Contains(f.log.String(), "Zebulon") {
		t.Errorf("the log has the argument: %s", f.log.String())
	}

	calls := toolCalls(t, f.st)
	if s := fmt.Sprintf("%+v", calls); strings.Contains(s, "Zebulon") {
		t.Errorf("the audit events have the argument: %s", s)
	}

	for i, e := range calls {
		if want := []string{"book_lesson", "cancel_booking"}[i/5]; e.Details.Tool != want ||
			e.Details.Outcome != outcomes[i] || e.ActorID != office.AccountID {
			t.Errorf("call %d audited as %+v, want %s %s", i, e, want, outcomes[i])
		}
	}

	if len(calls) != len(outcomes) {
		t.Errorf("%d calls audited, want %d", len(calls), len(outcomes))
	}
}

// TestBookingToolsInstructor: an instructor reaches neither booking tool
// (office), not even with personal data and the bookings group.
func TestBookingToolsInstructor(t *testing.T) {
	t.Parallel()

	f := newFixture(t)

	names := toolNames(t, f.session(t, demoInstructor, bkOptions, registry))
	if slices.Contains(names, "book_lesson") || slices.Contains(names, "cancel_booking") {
		t.Errorf("an instructor lists %v", names)
	}

	office := toolNames(t, f.session(t, demoOffice, bkOptions, registry))
	if !slices.Contains(office, "book_lesson") || !slices.Contains(office, "cancel_booking") {
		t.Errorf("the office lists %v", office)
	}
}
