package service

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/mail"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// The staff action tests run on the booking fixture (Europe/Berlin) from
// bookingNow, Wednesday 2026-10-21, 12:00 in Berlin. Their lesson is a
// one-off on Thursday 2026-10-22 from 18:00 to 19:00 (16:00–17:00 UTC),
// taught by Ada Example (the fixture's instructor) with Ben Example (the
// fixture's accompanist); its school day ends at midnight in Berlin,
// 22:00 UTC.
const (
	staffLesson  = "2026-10-22T16:00:00Z"
	staffStarted = "2026-10-22T16:30:00Z"
	staffDayOver = "2026-10-22T22:00:00Z"
	staffLate    = "2026-10-22T21:59:59Z"
)

// staffFixture adds the staff accounts to the booking fixture: Ivy, whose
// person is linked to the instructor Ada Example; Paul, linked to the
// accompanist Ben Example; Otto, an instructor of other lessons; and Nils,
// an instructor account without a staff link.
type staffFixture struct {
	*bookingFixture

	ivy, paul, otto, nils domain.Person
}

func newStaffFixture(t *testing.T) *staffFixture {
	t.Helper()

	f := &staffFixture{bookingFixture: newBookingFixture(t)}
	f.ivy = f.staffAccount(t, "Ivy", f.instructor)
	f.paul = f.staffAccount(t, "Paul", f.pianist)

	other := ids.New[domain.StaffID](f.seq)
	f.write(t, func(ctx context.Context, tx store.Tx) error {
		return tx.Staff().Create(ctx, domain.Staff{
			ID: other, DisplayName: "Otto Example", Kind: domain.StaffInstructor, Active: true,
		})
	})

	f.otto = f.staffAccount(t, "Otto", other)
	f.nils = f.staffAccount(t, "Nils", domain.StaffID{})

	// Anna has a phone, for the contact data of her daughter.
	f.write(t, func(ctx context.Context, tx store.Tx) error {
		f.anna.Phone = "+49 000 0000001"

		return tx.Persons().Update(ctx, f.anna)
	})

	return f
}

// staffAccount stores a person with an instructor account and links the
// staff member to it (none for a zero ID).
func (f *staffFixture) staffAccount(t *testing.T, given string, staff domain.StaffID) domain.Person {
	t.Helper()

	p := f.person(t, given, "", strings.ToLower(given)+"@example.net", "")
	f.write(t, func(ctx context.Context, tx store.Tx) error {
		acc, err := tx.Accounts().GetByPerson(ctx, p.ID)
		if err != nil {
			return err
		}

		acc.Role = domain.RoleInstructor
		if err := tx.Accounts().Update(ctx, acc); err != nil {
			return err
		}

		if staff.IsZero() {
			return nil
		}

		s, err := tx.Staff().Get(ctx, staff)
		if err != nil {
			return err
		}

		s.PersonID = p.ID

		return tx.Staff().Update(ctx, s)
	})

	return p
}

func (f *staffFixture) participants(t *testing.T, now string) *ParticipantService {
	t.Helper()

	return NewParticipantService(f.st, clock.NewFixed(mustInstant(t, now)), f.seq, testMails(newTestRenderer(t)))
}

// lessonWithPianist stores the staff lesson with capacity places and the
// fixture's accompanist.
func (f *staffFixture) lessonWithPianist(t *testing.T, capacity int) domain.Lesson {
	t.Helper()

	l := f.oneOff(t, staffLesson, capacity, domain.VisibilityPublic, domain.LessonRegular)
	l.AccompanistID = f.pianist
	f.write(t, func(ctx context.Context, tx store.Tx) error { return tx.Lessons().Update(ctx, l) })

	return l
}

func TestParticipantOwnership(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newStaffFixture(t)
	svc := f.participants(t, bookingNow)
	l := f.lessonWithPianist(t, 5)

	for name, p := range map[string]Principal{
		"office":      f.principal(t, f.olga),
		"instructor":  f.principal(t, f.ivy),
		"accompanist": f.principal(t, f.paul),
	} {
		if _, err := svc.Participants(ctx, p, l.ID); err != nil {
			t.Errorf("%s: %v", name, err)
		}

		if _, err := svc.Add(ctx, p, l.ID, NewParticipant{GuestName: "Guest " + name}); err != nil {
			t.Errorf("%s adds: %v", name, err)
		}
	}

	// Another instructor, an instructor without a staff link and a member
	// see no such lesson.
	for name, p := range map[string]Principal{
		"other instructor": f.principal(t, f.otto),
		"no staff link":    f.principal(t, f.nils),
		"member":           f.principal(t, f.anna),
	} {
		if _, err := svc.Participants(ctx, p, l.ID); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("%s lists: want ErrNotFound, got %v", name, err)
		}

		if _, err := svc.Add(ctx, p, l.ID, NewParticipant{GuestName: "Guest"}); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("%s adds: want ErrNotFound, got %v", name, err)
		}

		if _, err := svc.MarkAllAttended(ctx, p, l.ID); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("%s marks: want ErrNotFound, got %v", name, err)
		}
	}

	// Their booking operations are 404s too.
	b := f.bookingsOf(t, l)[0]
	otto := f.principal(t, f.otto)

	if _, err := svc.Remove(ctx, otto, b.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("other instructor removes: want ErrNotFound, got %v", err)
	}

	if _, err := svc.UpdateTrial(ctx, otto, b.ID, ParticipantUpdate{Trial: true}); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("other instructor updates: want ErrNotFound, got %v", err)
	}

	if _, err := svc.SetAttendance(ctx, otto, b.ID, domain.BookingAttended); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("other instructor takes attendance: want ErrNotFound, got %v", err)
	}

	// A staff member who is no longer active loses the lessons.
	f.write(t, func(ctx context.Context, tx store.Tx) error {
		s, err := tx.Staff().Get(ctx, f.pianist)
		if err != nil {
			return err
		}

		s.Active = false

		return tx.Staff().Update(ctx, s)
	})

	if _, err := svc.Participants(ctx, f.principal(t, f.paul), l.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("inactive accompanist: want ErrNotFound, got %v", err)
	}

	if _, err := svc.Participants(ctx, f.principal(t, f.ivy), domain.LessonID{1}); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("missing lesson: want ErrNotFound, got %v", err)
	}
}

func TestAddParticipant(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newStaffFixture(t)
	svc := f.participants(t, bookingNow)
	ivy := f.principal(t, f.ivy)
	l := f.lessonWithPianist(t, 5)

	b, err := svc.Add(ctx, ivy, l.ID, NewParticipant{PersonID: f.mia.ID, Trial: true, TrialNote: "First ballet class"})
	if err != nil {
		t.Fatal(err)
	}

	stored, events := f.booking(t, b.ID)
	if stored != b || b.Source != domain.BookingSourceStaff || b.Status != domain.BookingBooked || !b.Trial ||
		b.TrialNote != "First ballet class" || b.PersonID != f.mia.ID {
		t.Errorf("booking %+v, stored %+v", b, stored)
	}

	if len(events) != 1 || events[0].ActorAccountID != ivy.AccountID || events[0].Override ||
		events[0].Status != domain.BookingBooked {
		t.Errorf("events %+v", events)
	}

	// Mia's mails go to her guardian Anna, in German.
	confirmed := f.mailsOf(t, mail.TemplateBookingConfirmed)
	if len(confirmed) != 1 || confirmed[0].To != "anna@example.org" || !strings.Contains(confirmed[0].Text, "Mia Example") {
		t.Fatalf("confirmation mails %+v", confirmed)
	}

	if _, err := svc.Add(ctx, ivy, l.ID, NewParticipant{PersonID: f.mia.ID}); !errors.Is(err, ErrAlreadyBooked) {
		t.Errorf("again: want ErrAlreadyBooked, got %v", err)
	}

	// A guest with an email gets the confirmation in the school's
	// language; one without an email gets none.
	guest, err := svc.Add(ctx, ivy, l.ID, NewParticipant{GuestName: "Gina Guest", GuestEmail: "gina@example.org"})
	if err != nil {
		t.Fatal(err)
	}

	if guest.PersonID != (domain.PersonID{}) || guest.GuestName != "Gina Guest" || guest.Source != domain.BookingSourceStaff {
		t.Errorf("guest booking %+v", guest)
	}

	confirmed = f.mailsOf(t, mail.TemplateBookingConfirmed)
	if len(confirmed) != 2 || confirmed[1].To != "gina@example.org" || !strings.Contains(confirmed[1].Text, "Gina Guest") ||
		!strings.Contains(confirmed[1].Subject, "Studio Example") {
		t.Fatalf("guest confirmation %+v", confirmed)
	}

	if walkIn, err := svc.Add(ctx, ivy, l.ID, NewParticipant{GuestName: " Walter Walk-in "}); err != nil {
		t.Fatal(err)
	} else if walkIn.GuestName != "Walter Walk-in" || walkIn.GuestEmail != "" {
		t.Errorf("walk-in %+v, want the name trimmed", walkIn)
	}

	if n := len(f.mailsOf(t, mail.TemplateBookingConfirmed)); n != 2 {
		t.Errorf("a guest without an email got a mail: %d confirmations", n)
	}

	// A guest may come twice: guests have no person to be unique.
	if _, err := svc.Add(ctx, ivy, l.ID, NewParticipant{GuestName: "Walter Walk-in"}); err != nil {
		t.Errorf("second guest of the same name: %v", err)
	}

	for name, c := range map[string]struct {
		in    NewParticipant
		field string
	}{
		"unknown person":     {NewParticipant{PersonID: domain.PersonID{1}}, "personId"},
		"blank guest":        {NewParticipant{GuestName: " "}, "guest.name"},
		"bad guest email":    {NewParticipant{GuestName: "Gina", GuestEmail: "gina@"}, "guest.email"},
		"note without trial": {NewParticipant{PersonID: f.tom.ID, TrialNote: "note"}, "trialNote"},
	} {
		_, err := svc.Add(ctx, ivy, l.ID, c.in)
		wantFieldError(t, err, c.field)

		if t.Failed() {
			t.Fatalf("%s", name)
		}
	}

	if _, err := svc.Add(ctx, ivy, l.ID, NewParticipant{}); !errors.Is(err, domain.ErrValidation) {
		t.Errorf("nobody: want ErrValidation, got %v", err)
	}
}

func TestAddParticipantCapacity(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newStaffFixture(t)
	svc := f.participants(t, bookingNow)
	ivy := f.principal(t, f.ivy)
	l := f.lessonWithPianist(t, 1)
	f.bookAs(t, f.sam, l)

	if _, err := svc.Add(ctx, ivy, l.ID, NewParticipant{PersonID: f.mia.ID}); !errors.Is(err, ErrLessonFull) {
		t.Fatalf("full: want ErrLessonFull, got %v", err)
	}

	b, err := svc.Add(ctx, ivy, l.ID, NewParticipant{PersonID: f.mia.ID, ConfirmOverCapacity: true})
	if err != nil {
		t.Fatal(err)
	}

	if _, events := f.booking(t, b.ID); len(events) != 1 || !events[0].Override {
		t.Errorf("events %+v, want an override", events)
	}

	// Confirming on a lesson with a free place records no override.
	free := f.oneOff(t, "2026-10-22T18:00:00Z", 3, domain.VisibilityHidden, domain.LessonPrivate)

	b, err = svc.Add(ctx, ivy, free.ID, NewParticipant{PersonID: f.mia.ID, ConfirmOverCapacity: true})
	if err != nil {
		t.Fatal(err)
	}

	if _, events := f.booking(t, b.ID); len(events) != 1 || events[0].Override {
		t.Errorf("events %+v, want no override", events)
	}
}

func TestAddParticipantWithdrawsWaitlistEntry(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newStaffFixture(t)
	l := f.fullLesson(t, 1) // Ada Example's lesson, Stella has its place

	joined, err := f.waitlistService(t, bookingNow).Join(ctx, f.principal(t, f.anna), l.ID, f.mia.ID, false)
	if err != nil {
		t.Fatal(err)
	}

	b, err := f.participants(t, bookingNow).Add(ctx, f.principal(t, f.olga), l.ID,
		NewParticipant{PersonID: f.mia.ID, ConfirmOverCapacity: true})
	if err != nil {
		t.Fatal(err)
	}

	if e := f.entry(t, joined.Entry.ID); e.Status != domain.WaitlistWithdrawn {
		t.Errorf("entry %+v, want withdrawn", e)
	}

	if _, events := f.booking(t, b.ID); len(events) != 1 || !events[0].Override ||
		events[0].ActorAccountID != f.principal(t, f.olga).AccountID {
		t.Errorf("events %+v", events)
	}
}

func TestAddParticipantWindow(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newStaffFixture(t)
	ivy, olga := f.principal(t, f.ivy), f.principal(t, f.olga)
	l := f.lessonWithPianist(t, 5)

	// The booking cutoff and the start do not matter: a walk-in during the
	// lesson gets no mail.
	if _, err := f.participants(t, staffStarted).Add(ctx, ivy, l.ID, NewParticipant{PersonID: f.mia.ID}); err != nil {
		t.Fatal(err)
	}

	if _, err := f.participants(t, staffLate).Add(ctx, ivy, l.ID, NewParticipant{PersonID: f.tom.ID}); err != nil {
		t.Fatal(err)
	}

	if mails := f.mailsOf(t, mail.TemplateBookingConfirmed); len(mails) != 0 {
		t.Errorf("walk-ins got mails: %+v", mails)
	}

	// Once the day is over, only the office adds.
	late := f.participants(t, staffDayOver)
	if _, err := late.Add(ctx, ivy, l.ID, NewParticipant{GuestName: "Late Guest"}); !errors.Is(err, ErrDayOver) {
		t.Errorf("instructor after the day: want ErrDayOver, got %v", err)
	}

	if _, err := late.Add(ctx, olga, l.ID, NewParticipant{GuestName: "Late Guest"}); err != nil {
		t.Errorf("office after the day: %v", err)
	}

	// Never into a lesson the school canceled.
	l.Status, l.CanceledAt = domain.LessonCanceled, mustInstant(t, bookingNow)
	f.write(t, func(ctx context.Context, tx store.Tx) error { return tx.Lessons().Update(ctx, l) })

	_, err := f.participants(t, bookingNow).Add(ctx, olga, l.ID, NewParticipant{GuestName: "Guest"})
	wantClosed(t, err, ClosedCanceled)
}

func TestUpdateParticipantTrial(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newStaffFixture(t)
	svc := f.participants(t, bookingNow)
	ivy := f.principal(t, f.ivy)
	l := f.lessonWithPianist(t, 5)
	f.bookAs(t, f.mia, l)
	b := f.bookingIn(t, l, f.mia)

	got, err := svc.UpdateTrial(ctx, ivy, b.ID, ParticipantUpdate{Trial: true, TrialNote: "Knee injury last year"})
	if err != nil {
		t.Fatal(err)
	}

	if stored, events := f.booking(t, b.ID); stored != got || !got.Trial || got.TrialNote != "Knee injury last year" ||
		len(events) != 0 {
		t.Errorf("booking %+v, stored %+v, events %+v", got, stored, events)
	}

	_, err = svc.UpdateTrial(ctx, ivy, b.ID, ParticipantUpdate{TrialNote: "note"})
	wantFieldError(t, err, "trialNote")

	if got, err = svc.UpdateTrial(ctx, ivy, b.ID, ParticipantUpdate{}); err != nil || got.Trial || got.TrialNote != "" {
		t.Errorf("clear: %+v, %v", got, err)
	}

	if _, err := f.participants(t, staffDayOver).UpdateTrial(ctx, ivy, b.ID, ParticipantUpdate{Trial: true}); !errors.Is(err, ErrDayOver) {
		t.Errorf("after the day: want ErrDayOver, got %v", err)
	}

	if _, err := svc.Remove(ctx, ivy, b.ID); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.UpdateTrial(ctx, ivy, b.ID, ParticipantUpdate{Trial: true}); !errors.Is(err, ErrBookingCanceled) {
		t.Errorf("canceled: want ErrBookingCanceled, got %v", err)
	}
}

func TestRemoveParticipant(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newStaffFixture(t)
	ivy := f.principal(t, f.ivy)
	l := f.fullLesson(t, 1) // Stella has the only place

	if _, err := f.waitlistService(t, bookingNow).Join(ctx, f.principal(t, f.anna), l.ID, f.mia.ID, false); err != nil {
		t.Fatal(err)
	}

	stella := f.bookingIn(t, l, f.stella(t))
	svc := f.participants(t, bookingNow)

	// Removing after the cancellation cutoff is never late, and does not
	// release the date.
	removed, err := svc.Remove(ctx, ivy, stella.ID)
	if err != nil {
		t.Fatal(err)
	}

	stored, events := f.booking(t, stella.ID)
	if stored != removed || removed.Status != domain.BookingCanceled || removed.Late || removed.Released ||
		removed.CanceledAt != mustInstant(t, bookingNow) {
		t.Errorf("removed %+v, stored %+v", removed, stored)
	}

	if last := events[len(events)-1]; last.Status != domain.BookingCanceled || last.ActorAccountID != ivy.AccountID || last.Late {
		t.Errorf("events %+v", events)
	}

	// Stella learns of it; the freed place goes to Mia, via Anna.
	if canceled := f.mailsOf(t, mail.TemplateBookingCanceled); len(canceled) != 1 || canceled[0].To != "stella@example.org" {
		t.Errorf("cancellation mails %+v", canceled)
	}

	if invites := f.mailsOf(t, mail.TemplateWaitlistInvite); len(invites) != 1 || invites[0].To != "anna@example.org" {
		t.Errorf("invites %+v", invites)
	}

	if _, err := svc.Remove(ctx, ivy, stella.ID); !errors.Is(err, ErrBookingCanceled) {
		t.Errorf("again: want ErrBookingCanceled, got %v", err)
	}
}

func TestRemoveParticipantAfterStart(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newStaffFixture(t)
	ivy, olga := f.principal(t, f.ivy), f.principal(t, f.olga)
	l := f.lessonWithPianist(t, 5)
	f.bookAs(t, f.mia, l)
	f.bookAs(t, f.tom, l)

	// A walk-in booked by mistake goes without a mail.
	if _, err := f.participants(t, staffStarted).Remove(ctx, ivy, f.bookingIn(t, l, f.mia).ID); err != nil {
		t.Fatal(err)
	}

	if mails := f.mailsOf(t, mail.TemplateBookingCanceled); len(mails) != 0 {
		t.Errorf("mails after the start: %+v", mails)
	}

	tom := f.bookingIn(t, l, f.tom)
	if _, err := f.participants(t, staffDayOver).Remove(ctx, ivy, tom.ID); !errors.Is(err, ErrDayOver) {
		t.Errorf("instructor after the day: want ErrDayOver, got %v", err)
	}

	if _, err := f.participants(t, "2026-11-30T10:00:00Z").Remove(ctx, olga, tom.ID); err != nil {
		t.Errorf("office weeks later: %v", err)
	}
}

func TestSetAttendance(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newStaffFixture(t)
	paul, olga := f.principal(t, f.paul), f.principal(t, f.olga)
	l := f.lessonWithPianist(t, 5)
	f.bookAs(t, f.mia, l)
	b := f.bookingIn(t, l, f.mia)

	before := f.participants(t, "2026-10-22T15:59:59Z")
	if _, err := before.SetAttendance(ctx, olga, b.ID, domain.BookingAttended); !errors.Is(err, ErrLessonNotStarted) {
		t.Errorf("before the start: want ErrLessonNotStarted, got %v", err)
	}

	// From the start on, the accompanist takes attendance.
	at := f.participants(t, staffLesson)

	got, err := at.SetAttendance(ctx, paul, b.ID, domain.BookingAttended)
	if err != nil {
		t.Fatal(err)
	}

	stored, events := f.booking(t, b.ID)
	if got.Status != domain.BookingAttended || stored != got || len(events) != 1 ||
		events[0].Status != domain.BookingAttended || events[0].ActorAccountID != paul.AccountID ||
		events[0].At != mustInstant(t, staffLesson) {
		t.Errorf("attended %+v, events %+v", got, events)
	}

	// The same status again changes nothing.
	if _, err := at.SetAttendance(ctx, paul, b.ID, domain.BookingAttended); err != nil {
		t.Fatal(err)
	}

	if _, events := f.booking(t, b.ID); len(events) != 1 {
		t.Errorf("events %+v, want no new one", events)
	}

	last := f.participants(t, staffLate)
	for _, status := range []domain.BookingStatus{domain.BookingNoShow, domain.BookingBooked} {
		if got, err := last.SetAttendance(ctx, paul, b.ID, status); err != nil || got.Status != status {
			t.Errorf("%s: %+v, %v", status, got, err)
		}
	}

	if _, events := f.booking(t, b.ID); len(events) != 3 {
		t.Errorf("events %+v, want three", events)
	}

	_, err = last.SetAttendance(ctx, paul, b.ID, domain.BookingCanceled)
	wantFieldError(t, err, "status")

	// After the day, only the office.
	over := f.participants(t, staffDayOver)
	if _, err := over.SetAttendance(ctx, paul, b.ID, domain.BookingAttended); !errors.Is(err, ErrDayOver) {
		t.Errorf("accompanist after the day: want ErrDayOver, got %v", err)
	}

	if got, err := over.SetAttendance(ctx, olga, b.ID, domain.BookingAttended); err != nil || got.Status != domain.BookingAttended {
		t.Errorf("office after the day: %+v, %v", got, err)
	}

	// A canceled booking has no attendance.
	f.bookAs(t, f.tom, l)
	tom := f.bookingIn(t, l, f.tom)

	if _, err := f.participants(t, bookingNow).Remove(ctx, olga, tom.ID); err != nil {
		t.Fatal(err)
	}

	if _, err := at.SetAttendance(ctx, olga, tom.ID, domain.BookingAttended); !errors.Is(err, ErrBookingCanceled) {
		t.Errorf("canceled booking: want ErrBookingCanceled, got %v", err)
	}

	// Nor does a lesson the school canceled.
	l.Status, l.CanceledAt = domain.LessonCanceled, mustInstant(t, bookingNow)
	f.write(t, func(ctx context.Context, tx store.Tx) error { return tx.Lessons().Update(ctx, l) })

	_, err = at.SetAttendance(ctx, olga, b.ID, domain.BookingNoShow)
	wantClosed(t, err, ClosedCanceled)
}

func TestMarkAllAttended(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newStaffFixture(t)
	ivy := f.principal(t, f.ivy)
	l := f.lessonWithPianist(t, 5)
	f.bookAs(t, f.mia, l)
	f.bookAs(t, f.tom, l)
	f.bookAs(t, f.sam, l)

	svc := f.participants(t, staffStarted)
	if _, err := svc.SetAttendance(ctx, ivy, f.bookingIn(t, l, f.tom).ID, domain.BookingNoShow); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.Remove(ctx, ivy, f.bookingIn(t, l, f.sam).ID); err != nil {
		t.Fatal(err)
	}

	if _, err := f.participants(t, bookingNow).MarkAllAttended(ctx, ivy, l.ID); !errors.Is(err, ErrLessonNotStarted) {
		t.Errorf("before the start: want ErrLessonNotStarted, got %v", err)
	}

	changed, err := svc.MarkAllAttended(ctx, ivy, l.ID)
	if err != nil {
		t.Fatal(err)
	}

	if len(changed) != 1 || changed[0].PersonID != f.mia.ID || changed[0].Status != domain.BookingAttended {
		t.Errorf("changed %+v, want only Mia", changed)
	}

	statuses := map[domain.PersonID]domain.BookingStatus{}
	for _, b := range f.bookingsOf(t, l) {
		statuses[b.PersonID] = b.Status
	}

	if statuses[f.mia.ID] != domain.BookingAttended || statuses[f.tom.ID] != domain.BookingNoShow ||
		statuses[f.sam.ID] != domain.BookingCanceled {
		t.Errorf("statuses %v", statuses)
	}

	if again, err := svc.MarkAllAttended(ctx, ivy, l.ID); err != nil || len(again) != 0 {
		t.Errorf("again: %+v, %v", again, err)
	}

	if _, err := f.participants(t, staffDayOver).MarkAllAttended(ctx, ivy, l.ID); !errors.Is(err, ErrDayOver) {
		t.Errorf("after the day: want ErrDayOver, got %v", err)
	}
}

func TestLessonParticipants(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newStaffFixture(t)
	ivy := f.principal(t, f.ivy)
	l := f.fullLesson(t, 2) // Stella and a filler

	waitlist := f.waitlistService(t, bookingNow)
	if _, err := waitlist.Join(ctx, f.principal(t, f.anna), l.ID, f.mia.ID, false); err != nil {
		t.Fatal(err)
	}

	if _, err := waitlist.Join(ctx, f.principal(t, f.tom), l.ID, f.tom.ID, true); err != nil {
		t.Fatal(err)
	}

	svc := f.participants(t, bookingNow)

	// Stella leaves: Mia is invited and holds the place.
	if _, err := svc.Remove(ctx, ivy, f.bookingIn(t, l, f.stella(t)).ID); err != nil {
		t.Fatal(err)
	}

	guest, err := svc.Add(ctx, ivy, l.ID, NewParticipant{
		GuestName: "Gina Guest", GuestEmail: "gina@example.org", Trial: true, TrialNote: "Tried jazz before",
		ConfirmOverCapacity: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	got, err := svc.Participants(ctx, ivy, l.ID)
	if err != nil {
		t.Fatal(err)
	}

	if got.Active != 2 || got.Held != 1 || got.FreePlaces() != 0 || len(got.Waitlist) != 2 ||
		got.Waitlist[0].Status != domain.WaitlistInvited || got.Waitlist[0].Position != 0 ||
		got.Waitlist[1].Position != 1 {
		t.Errorf("with an invite: counts %d/%d, waitlist %+v", got.Active, got.Held, got.Waitlist)
	}

	// Adding Mia takes the place her invite holds.
	if _, err := svc.Add(ctx, ivy, l.ID, NewParticipant{PersonID: f.mia.ID}); err != nil {
		t.Fatal(err)
	}

	got, err = svc.Participants(ctx, ivy, l.ID)
	if err != nil {
		t.Fatal(err)
	}

	if got.Lesson.ID != l.ID || got.DayOverAt != mustInstant(t, "2026-10-23T22:00:00Z") {
		t.Errorf("lesson %v, day over at %v", got.Lesson.ID, got.DayOverAt)
	}

	names := make([]string, len(got.Bookings))
	for i, pt := range got.Bookings {
		names[i] = pt.Name + "/" + string(pt.Booking.Status)
	}

	want := []string{"Stella Example/canceled", "Filler1 Example/booked", "Gina Guest/booked", "Mia Example/booked"}
	if !slices.Equal(names, want) {
		t.Errorf("bookings %v, want %v", names, want)
	}

	if g := got.Bookings[2]; g.Booking.ID != guest.ID || g.ContactEmail != "gina@example.org" || g.ContactPhone != "" ||
		g.Booking.TrialNote != "Tried jazz before" {
		t.Errorf("guest %+v", g)
	}

	// Mia's contact is her guardian Anna.
	if m := got.Bookings[3]; m.ContactEmail != "anna@example.org" || m.ContactPhone != "+49 000 0000001" {
		t.Errorf("Mia %+v", m)
	}

	if s := got.Bookings[0]; s.ContactEmail != "stella@example.org" {
		t.Errorf("Stella %+v", s)
	}

	// Mia accepted her invite by being added; Tom waits first in line.
	if got.Active != 3 || got.Held != 0 || got.FreePlaces() != 0 || len(got.Waitlist) != 2 ||
		got.Waitlist[0].Name != "Mia Example" || got.Waitlist[0].Status != domain.WaitlistAccepted ||
		got.Waitlist[0].Position != 0 || got.Waitlist[1].Status != domain.WaitlistWaiting || got.Waitlist[1].Position != 1 ||
		!got.Waitlist[1].Entry.StayOnList {
		t.Errorf("counts %d/%d, waitlist %+v", got.Active, got.Held, got.Waitlist)
	}
}

// TestLessonParticipantsDayEnd: a lesson that runs past midnight closes
// its window at its end.
func TestDayOverAt(t *testing.T) {
	t.Parallel()

	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}

	l := domain.Lesson{StartsAt: mustInstant(t, "2026-10-22T21:30:00Z"), EndsAt: mustInstant(t, "2026-10-22T23:00:00Z")}
	if got := dayOverAt(loc, l); !got.Equal(l.EndsAt) {
		t.Errorf("past midnight: %v, want the end", got)
	}

	l.EndsAt = mustInstant(t, "2026-10-22T21:45:00Z")
	if got := dayOverAt(loc, l); !got.Equal(mustInstant(t, "2026-10-22T22:00:00Z")) {
		t.Errorf("before midnight: %v", got)
	}
}

func TestBookingEvents(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newStaffFixture(t)
	l := f.lessonWithPianist(t, 5)
	ivy, olga := f.principal(t, f.ivy), f.principal(t, f.olga)

	b, err := f.participants(t, bookingNow).Add(ctx, ivy, l.ID, NewParticipant{PersonID: f.mia.ID})
	if err != nil {
		t.Fatal(err)
	}

	// An admin signed in as Olga acts as the admin.
	admin := f.person(t, "Ada", "", "ada@example.net", "")
	adminAccount := f.principal(t, admin).AccountID
	impersonated := olga
	impersonated.ImpersonatorID = adminAccount

	if _, err := f.participants(t, staffStarted).SetAttendance(ctx, impersonated, b.ID, domain.BookingAttended); err != nil {
		t.Fatal(err)
	}

	svc := f.participants(t, staffStarted)

	events, err := svc.Events(ctx, olga, b.ID)
	if err != nil {
		t.Fatal(err)
	}

	if len(events) != 2 || events[0].ActorName != "Ivy Example" || events[0].ActorEmail != "ivy@example.net" ||
		events[0].Event.Status != domain.BookingBooked || events[1].ActorName != "Ada Example" ||
		events[1].Event.Status != domain.BookingAttended {
		t.Errorf("events %+v", events)
	}

	if _, err := svc.Events(ctx, ivy, b.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("instructor: want ErrNotFound, got %v", err)
	}

	if _, err := svc.Events(ctx, olga, domain.BookingID{1}); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("missing booking: want ErrNotFound, got %v", err)
	}
}

// TestLessonRecipientsGuests: a guest's address gets the lesson mails, a
// guest without one is left out.
func TestLessonRecipientsGuests(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newStaffFixture(t)
	l := f.lessonWithPianist(t, 5)
	svc := f.participants(t, bookingNow)
	olga := f.principal(t, f.olga)

	for _, in := range []NewParticipant{
		{GuestName: "Gina Guest", GuestEmail: "gina@example.org"},
		{GuestName: "Walter Walk-in"},
		{PersonID: f.mia.ID},
	} {
		if _, err := svc.Add(ctx, olga, l.ID, in); err != nil {
			t.Fatal(err)
		}
	}

	var recipients []recipient

	err := f.st.InTx(ctx, store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		var err error
		recipients, err = lessonRecipients(ctx, tx, l.ID)

		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	got := make([]string, len(recipients))
	for i, r := range recipients {
		got[i] = r.email + ":" + r.locale + ":" + strings.Join(r.participants, ",")
	}

	if want := []string{"gina@example.org::Gina Guest", "anna@example.org:de:Mia Example"}; !slices.Equal(got, want) {
		t.Errorf("recipients %v, want %v", got, want)
	}
}
