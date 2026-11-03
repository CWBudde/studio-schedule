package service

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// The public and member tests of events run on the event fixture at
// bookingNow. The published events' sessions are on Friday 2026-10-23 and
// Saturday 2026-10-24, so their sign-up and cancellation close at Friday's
// start by default. Sven is a member of another family.

// stranger adds Sven, a member with an account of another family.
func (f *eventFixture) stranger(t *testing.T) (domain.Person, Principal) {
	t.Helper()

	sven := f.person(t, "Sven", "", "sven@example.org", "")

	return sven, f.principal(t, sven)
}

// pastEvent publishes an event whose one session was on Monday 2026-10-19.
func (f *eventFixture) pastEvent(t *testing.T) domain.Event {
	t.Helper()

	svc := f.events(t)

	e, err := svc.Create(t.Context(), f.office, f.eventData())
	if err != nil {
		t.Fatal(err)
	}

	s := f.sessionAt(t, "2026-10-19", 18)
	s.ConfirmPast = true

	if _, err := svc.AddSession(t.Context(), e.Event.ID, s); err != nil {
		t.Fatal(err)
	}

	got, err := svc.Publish(t.Context(), e.Event.ID)
	if err != nil {
		t.Fatal(err)
	}

	return got.Event
}

// draftEvent creates a draft with a session on Monday 2026-10-26.
func (f *eventFixture) draftEvent(t *testing.T) domain.Event {
	t.Helper()

	svc := f.events(t)

	e, err := svc.Create(t.Context(), f.office, f.eventData())
	if err != nil {
		t.Fatal(err)
	}

	if _, err := svc.AddSession(t.Context(), e.Event.ID, f.sessionAt(t, "2026-10-26", 18)); err != nil {
		t.Fatal(err)
	}

	return e.Event
}

// canceledEvent publishes an event with sessions at 8:00 and cancels it.
func (f *eventFixture) canceledEvent(t *testing.T) domain.Event {
	t.Helper()

	e, _ := f.publishedAt(t, f.eventData(), 8)

	got, err := f.events(t).Cancel(t.Context(), f.office, e.ID, "")
	if err != nil {
		t.Fatal(err)
	}

	return got.Event
}

func TestPublishedEvents(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newEventFixture(t)
	e, sessions := f.published(t, f.eventData())
	f.pastEvent(t)
	f.draftEvent(t)
	f.canceledEvent(t)
	f.register(t, e, NewRegistration{PersonID: f.mia.ID})

	img := domain.Background{ContentType: domain.BackgroundPNG, Data: []byte("\x89PNG\r\n\x1a\nimage")}
	if err := f.events(t).PutImage(ctx, e.ID, img); err != nil {
		t.Fatal(err)
	}

	got, err := f.events(t).Published(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != 1 {
		t.Fatalf("published %+v, want the one upcoming event", got)
	}

	pe := got[0]
	if pe.Event.ID != e.ID || pe.FreePlaces != 1 || !pe.SignupOpen || !pe.HasImage ||
		pe.CourseType.ID != f.courseType || pe.CourseType.Name == "" ||
		!pe.SignupClosesAt.Equal(sessions[0].StartsAt) || !pe.CancelUntil.Equal(sessions[0].StartsAt) {
		t.Errorf("published %+v", pe)
	}

	if len(pe.Sessions) != 2 || pe.Sessions[0].Lesson.ID != sessions[0].ID || pe.Sessions[0].EventSlug != e.Slug ||
		pe.Sessions[1].EventSlug != e.Slug || pe.Sessions[0].Room.ID != f.room {
		t.Errorf("sessions %+v", pe.Sessions)
	}

	// No events at all.
	empty, err := newEventFixture(t).events(t).Published(ctx)
	if err != nil || len(empty) != 0 {
		t.Errorf("no events: %+v, %v", empty, err)
	}
}

func TestEventBySlug(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newEventFixture(t)
	svc := f.events(t)
	e, _ := f.published(t, f.eventData())
	past := f.pastEvent(t)
	draft := f.draftEvent(t)
	canceled := f.canceledEvent(t)

	for _, slug := range []string{draft.Slug, "no-such-event"} {
		if _, err := svc.BySlug(ctx, slug); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("BySlug(%q): %v, want ErrNotFound", slug, err)
		}

		if _, err := svc.PublicImage(ctx, slug); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("PublicImage(%q): %v, want ErrNotFound", slug, err)
		}

		if _, err := svc.PublicEventID(ctx, slug); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("PublicEventID(%q): %v, want ErrNotFound", slug, err)
		}
	}

	got, err := svc.BySlug(ctx, e.Slug)
	if err != nil || got.Event.ID != e.ID || !got.SignupOpen || got.FreePlaces != 2 || got.HasImage {
		t.Errorf("published %+v, %v", got, err)
	}

	if img, err := svc.PublicImage(ctx, e.Slug); err != nil || !img.IsZero() {
		t.Errorf("no image: %+v, %v", img, err)
	}

	want := domain.Background{ContentType: domain.BackgroundPNG, Data: []byte("\x89PNG\r\n\x1a\nimage")}
	if err := svc.PutImage(ctx, canceled.ID, want); err != nil {
		t.Fatal(err)
	}

	if img, err := svc.PublicImage(ctx, canceled.Slug); err != nil || string(img.Data) != string(want.Data) {
		t.Errorf("image: %+v, %v", img, err)
	}

	// A canceled event keeps its page, with its canceled sessions, and
	// takes no registration; so does a past one.
	got, err = svc.BySlug(ctx, canceled.Slug)
	if err != nil || got.Event.Status != domain.EventCanceled || got.SignupOpen || !got.HasImage ||
		len(got.Sessions) != 2 || got.Sessions[0].Lesson.Status != domain.LessonCanceled ||
		!got.SignupClosesAt.IsZero() || !got.CancelUntil.IsZero() {
		t.Errorf("canceled %+v, %v", got, err)
	}

	if got, err = svc.BySlug(ctx, past.Slug); err != nil || got.SignupOpen || got.Event.ID != past.ID {
		t.Errorf("past %+v, %v", got, err)
	}

	for _, ev := range []domain.Event{e, canceled, past} {
		if id, err := svc.PublicEventID(ctx, ev.Slug); err != nil || id != ev.ID {
			t.Errorf("PublicEventID(%q) = %v, %v", ev.Slug, id, err)
		}
	}
}

func TestSignupWindow(t *testing.T) {
	t.Parallel()

	at := func(s string) time.Time { return mustInstant(t, s) }
	session := func(start string, status domain.LessonStatus) ScheduledLesson {
		return ScheduledLesson{Lesson: domain.Lesson{StartsAt: at(start), Status: status}}
	}
	first := session("2026-10-23T16:00:00Z", domain.LessonScheduled)
	sessions := []ScheduledLesson{session("2026-10-22T16:00:00Z", domain.LessonCanceled), first}
	now := at(bookingNow)

	for name, c := range map[string]struct {
		e                   domain.Event
		sessions            []ScheduledLesson
		now                 time.Time
		closes, cancelUntil time.Time
		open                bool
	}{
		"defaults": {
			e: domain.Event{Status: domain.EventPublished}, sessions: sessions, now: now,
			closes: first.Lesson.StartsAt, cancelUntil: first.Lesson.StartsAt, open: true,
		},
		"at the close": {
			e: domain.Event{Status: domain.EventPublished}, sessions: sessions, now: first.Lesson.StartsAt,
			closes: first.Lesson.StartsAt, cancelUntil: first.Lesson.StartsAt,
		},
		"not open yet": {
			e:        domain.Event{Status: domain.EventPublished, SignupOpensAt: at("2026-10-21T10:00:01Z")},
			sessions: sessions, now: now, closes: first.Lesson.StartsAt, cancelUntil: first.Lesson.StartsAt,
		},
		"opens now": {
			e:        domain.Event{Status: domain.EventPublished, SignupOpensAt: now},
			sessions: sessions, now: now, closes: first.Lesson.StartsAt, cancelUntil: first.Lesson.StartsAt, open: true,
		},
		"own close": {
			e:        domain.Event{Status: domain.EventPublished, SignupClosesAt: at("2026-10-22T00:00:00Z")},
			sessions: sessions, now: now, closes: at("2026-10-22T00:00:00Z"), cancelUntil: at("2026-10-22T00:00:00Z"),
			open: true,
		},
		"own deadline": {
			e: domain.Event{
				Status: domain.EventPublished, SignupClosesAt: at("2026-10-22T00:00:00Z"),
				CancelUntil: at("2026-10-21T12:00:00Z"),
			},
			sessions: sessions, now: now, closes: at("2026-10-22T00:00:00Z"), cancelUntil: at("2026-10-21T12:00:00Z"),
			open: true,
		},
		"no scheduled session": {
			e: domain.Event{Status: domain.EventPublished}, sessions: sessions[:1], now: now,
		},
		"draft": {
			e: domain.Event{Status: domain.EventDraft}, sessions: sessions, now: now,
			closes: first.Lesson.StartsAt, cancelUntil: first.Lesson.StartsAt,
		},
	} {
		closes, cancelUntil, open := signupWindow(c.e, c.sessions, c.now)
		if !closes.Equal(c.closes) || !cancelUntil.Equal(c.cancelUntil) || open != c.open {
			t.Errorf("%s: %v, %v, %v; want %v, %v, %v", name, closes, cancelUntil, open, c.closes, c.cancelUntil, c.open)
		}
	}
}

func TestRegisterForEvent(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newEventFixture(t)
	e, _ := f.published(t, f.eventData())
	anna := f.principal(t, f.anna)
	anna.Via = domain.AuditVia{Channel: domain.ChannelMCP}
	svc := f.events(t)
	before := len(f.mails(t))

	own, err := svc.Register(ctx, anna, e.ID, f.anna.ID)
	if err != nil {
		t.Fatal(err)
	}

	r := own.Registration.Registration
	if r.EventID != e.ID || r.PersonID != f.anna.ID || r.Status != domain.EventRegistered ||
		r.Payment != domain.EventUnpaid || r.Price != 4500 || r.VATRate != 1900 || r.RegisteredBy != anna.AccountID ||
		own.Registration.Name != "Anna Example" || f.registration(t, r.ID) != r {
		t.Errorf("registered %+v", own)
	}

	if own.Event.Event.ID != e.ID || own.Event.FreePlaces != 1 || !own.Cancelable || len(own.Event.Sessions) != 2 {
		t.Errorf("event %+v, cancelable %v", own.Event, own.Cancelable)
	}

	// Twice is a duplicate.
	if _, err := svc.Register(ctx, anna, e.ID, f.anna.ID); !errors.Is(err, domain.ErrDuplicate) {
		t.Errorf("twice: %v, want ErrDuplicate", err)
	}

	// A dependent takes the last place.
	mia, err := svc.Register(ctx, anna, e.ID, f.mia.ID)
	if err != nil || mia.Registration.Registration.PersonID != f.mia.ID || mia.Event.FreePlaces != 0 {
		t.Fatalf("dependent %+v, %v", mia, err)
	}

	if _, err := svc.Register(ctx, anna, e.ID, f.tom.ID); !errors.Is(err, ErrEventFull) {
		t.Errorf("full: %v, want ErrEventFull", err)
	}

	// Another family's person, and an unknown one, are not found, even on
	// a full event.
	for _, p := range []domain.PersonID{f.sam.ID, f.olga.ID, ids.New[domain.PersonID](f.seq), {}} {
		if _, err := svc.Register(ctx, anna, e.ID, p); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("person %v: %v, want ErrNotFound", p, err)
		}
	}

	if _, err := svc.Register(ctx, anna, ids.New[domain.EventID](f.seq), f.anna.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown event: %v", err)
	}

	// The audit records the channel.
	audits := f.audits(t)
	if a := audits[len(audits)-1]; a.Action != domain.AuditEventRegistrationAdded || a.ActorID != anna.AccountID ||
		a.TargetPersonID != f.mia.ID || a.Details.EventID != e.ID ||
		a.Details.RegistrationID != mia.Registration.Registration.ID || a.Details.Via != anna.Via {
		t.Errorf("audit %+v", a)
	}

	// One confirmation per registration to Anna, in German, with the fee
	// to pay and the deadline.
	mails := f.mails(t)[before:]
	if len(mails) != 2 {
		t.Fatalf("mails %+v", mails)
	}

	for _, m := range mails {
		if m.Template != "event-registered" || m.To != "anna@example.org" || !strings.Contains(m.Text, "45,00") ||
			!strings.Contains(m.Text, e.Title) || !strings.Contains(m.Text, "bis Freitag, 23. Oktober 2026,\n18:00 Uhr") {
			t.Errorf("mail to %s (%s):\n%s", m.To, m.Template, m.Text)
		}
	}

	if !strings.Contains(mails[1].Text, "Mia Example") {
		t.Errorf("the mail names Mia:\n%s", mails[1].Text)
	}
}

func TestRegisterForEventFreeAndManaged(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newEventFixture(t)
	d := f.eventData()
	d.Price, d.VATRate = 0, 0
	e, _ := f.published(t, d)
	before := len(f.mails(t))

	// Tom, a managed dependent, registers himself; a free event is paid.
	tom := f.principal(t, f.tom)

	got, err := f.events(t).Register(ctx, tom, e.ID, f.tom.ID)
	if err != nil {
		t.Fatal(err)
	}

	if r := got.Registration.Registration; r.Payment != domain.EventPaid || !r.ReceiptID.IsZero() ||
		r.RegisteredBy != tom.AccountID {
		t.Errorf("free %+v", r)
	}

	mails := f.mails(t)[before:]
	if len(mails) != 1 || mails[0].To != "tom@example.org" || strings.Contains(mails[0].Text, "€") {
		t.Errorf("mails %+v", mails)
	}

	// Tom cannot register his mother or sister.
	for _, p := range []domain.PersonID{f.anna.ID, f.mia.ID} {
		if _, err := f.events(t).Register(ctx, tom, e.ID, p); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("%v: %v, want ErrNotFound", p, err)
		}
	}

	// An anonymized dependent is not found.
	f.write(t, func(ctx context.Context, tx store.Tx) error {
		p, err := tx.Persons().Get(ctx, f.mia.ID)
		if err != nil {
			return err
		}

		return tx.Persons().Update(ctx, p.Anonymize(mustInstant(t, bookingNow)))
	})

	if _, err := f.events(t).Register(ctx, f.principal(t, f.anna), e.ID, f.mia.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("anonymized: %v, want ErrNotFound", err)
	}
}

func TestRegisterForEventClosed(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newEventFixture(t)
	anna := f.principal(t, f.anna)
	draft := f.draftEvent(t)
	canceled := f.canceledEvent(t)
	past := f.pastEvent(t)

	later := f.eventData()
	later.SignupOpensAt = mustInstant(t, "2026-10-22T08:00:00Z")
	opensLater, _ := f.publishedAt(t, later, 10)
	open, _ := f.publishedAt(t, f.eventData(), 12)

	for name, id := range map[string]domain.EventID{
		"draft": draft.ID, "canceled": canceled.ID, "past": past.ID, "not open yet": opensLater.ID,
	} {
		if _, err := f.events(t).Register(ctx, anna, id, f.mia.ID); !errors.Is(err, ErrEventClosed) {
			t.Errorf("%s: %v, want ErrEventClosed", name, err)
		}

		// Another family's person is not found, closed or not.
		if _, err := f.events(t).Register(ctx, anna, id, f.sam.ID); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("%s, another family: %v, want ErrNotFound", name, err)
		}
	}

	// At the first session's start the sign-up has closed.
	if _, err := f.eventsAt(t, "2026-10-23T10:00:00Z").Register(ctx, anna, open.ID, f.mia.ID); !errors.Is(err, ErrEventClosed) {
		t.Errorf("after the close: %v, want ErrEventClosed", err)
	}

	if _, err := f.eventsAt(t, "2026-10-22T08:00:00Z").Register(ctx, anna, opensLater.ID, f.mia.ID); err != nil {
		t.Errorf("when it opens: %v", err)
	}
}

func TestCancelOwnRegistration(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newEventFixture(t)
	e, _ := f.published(t, f.eventData())
	anna := f.principal(t, f.anna)
	svc := f.events(t)

	mia, err := svc.Register(ctx, anna, e.ID, f.mia.ID)
	if err != nil {
		t.Fatal(err)
	}

	tom, err := svc.Register(ctx, anna, e.ID, f.tom.ID)
	if err != nil {
		t.Fatal(err)
	}

	paid := f.paid(t, tom.Registration.Registration)
	before := len(f.mails(t))

	got, err := svc.CancelOwn(ctx, anna, mia.Registration.Registration.ID)
	if err != nil {
		t.Fatal(err)
	}

	if r := got.Registration.Registration; r.Status != domain.EventRegistrationCanceled ||
		r.Payment != domain.EventUnpaid || got.Cancelable || got.Event.FreePlaces != 1 ||
		got.Registration.Name != "Mia Example" {
		t.Errorf("canceled %+v", got)
	}

	if _, err := svc.CancelOwn(ctx, anna, mia.Registration.Registration.ID); !errors.Is(err, ErrRegistrationCanceled) {
		t.Errorf("twice: %v, want ErrRegistrationCanceled", err)
	}

	// A paid fee is refunded by a Storno.
	got, err = svc.CancelOwn(ctx, anna, paid.ID)
	if err != nil {
		t.Fatal(err)
	}

	if r := got.Registration.Registration; r.Payment != domain.EventRefunded || r.CancellationReceiptID.IsZero() ||
		f.registration(t, r.ID) != r {
		t.Errorf("refunded %+v", r)
	}

	type sent struct{ template, to string }

	var mails []sent

	for _, m := range f.mails(t)[before:] {
		mails = append(mails, sent{m.Template, m.To})

		if m.Template == "event-registration-canceled" &&
			strings.Contains(m.Text, "erstattet") != strings.Contains(m.Text, "Tom Example") {
			t.Errorf("refund in the mail:\n%s", m.Text)
		}
	}

	// Tom paid himself, so the Storno goes to him.
	want := []sent{
		{"event-registration-canceled", "anna@example.org"},
		{"receipt", "tom@example.org"},
		{"event-registration-canceled", "anna@example.org"},
	}
	if !slices.Equal(mails, want) {
		t.Errorf("mails %v, want %v", mails, want)
	}

	audits := f.audits(t)
	if a := audits[len(audits)-1]; a.Action != domain.AuditEventRegistrationCanceled || !a.Details.Refund ||
		a.ActorID != anna.AccountID || a.TargetPersonID != f.tom.ID {
		t.Errorf("audit %+v", a)
	}
}

func TestCancelOwnRegistrationRefused(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newEventFixture(t)
	e, _ := f.published(t, f.eventData())
	anna := f.principal(t, f.anna)
	_, sven := f.stranger(t)
	svc := f.events(t)

	mia, err := svc.Register(ctx, anna, e.ID, f.mia.ID)
	if err != nil {
		t.Fatal(err)
	}

	id := mia.Registration.Registration.ID
	guest := f.register(t, e, NewRegistration{GuestName: "Gina Guest", GuestEmail: "gina@example.org"})

	for name, c := range map[string]struct {
		p  Principal
		id domain.EventRegistrationID
	}{
		"another family": {sven, id},
		"a guest":        {anna, guest.ID},
		"unknown":        {anna, ids.New[domain.EventRegistrationID](f.seq)},
	} {
		if _, err := svc.CancelOwn(ctx, c.p, c.id); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("%s: %v, want ErrNotFound", name, err)
		}
	}

	// At the deadline it is too late.
	if _, err := f.eventsAt(t, "2026-10-23T16:00:00Z").CancelOwn(ctx, anna, id); !errors.Is(err, ErrTooLate) {
		t.Errorf("at the deadline: %v, want ErrTooLate", err)
	}

	// A registration on the waiting list is not canceled here.
	waiting := mia.Registration.Registration
	waiting.ID, waiting.PersonID, waiting.Status = ids.New[domain.EventRegistrationID](f.seq), f.tom.ID, domain.EventWaiting
	f.write(t, func(ctx context.Context, tx store.Tx) error { return tx.EventRegistrations().Create(ctx, waiting) })

	if _, err := svc.CancelOwn(ctx, anna, waiting.ID); !errors.Is(err, ErrNotRegistered) {
		t.Errorf("waiting: %v, want ErrNotRegistered", err)
	}

	// The school's cancellation canceled it already.
	if _, err := svc.Cancel(ctx, f.office, e.ID, ""); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.CancelOwn(ctx, anna, id); !errors.Is(err, ErrRegistrationCanceled) {
		t.Errorf("after the event's cancellation: %v, want ErrRegistrationCanceled", err)
	}
}

func TestCancelOwnWithoutDeadline(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newEventFixture(t)
	e, sessions := f.published(t, f.eventData())
	anna := f.principal(t, f.anna)

	mia, err := f.events(t).Register(ctx, anna, e.ID, f.mia.ID)
	if err != nil {
		t.Fatal(err)
	}

	// With every session canceled, there is no deadline: too late.
	for _, l := range sessions {
		if _, err := f.events(t).lessons.CancelLesson(ctx, f.office, l.ID, ""); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := f.events(t).CancelOwn(ctx, anna, mia.Registration.Registration.ID); !errors.Is(err, ErrTooLate) {
		t.Errorf("no deadline: %v, want ErrTooLate", err)
	}
}

func TestMyEvents(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newEventFixture(t)
	anna := f.principal(t, f.anna)
	sven, svenP := f.stranger(t)
	svc := f.events(t)

	// Three events on Friday and Saturday at 10:00, 12:00 and 14:00; Tom
	// registers for the second one first.
	early, _ := f.publishedAt(t, f.eventData(), 10)
	later, _ := f.publishedAt(t, f.eventData(), 12)
	draft, _ := f.publishedAt(t, f.eventData(), 14)

	register := func(p Principal, e domain.Event, person domain.PersonID) domain.EventRegistration {
		t.Helper()

		got, err := svc.Register(ctx, p, e.ID, person)
		if err != nil {
			t.Fatal(err)
		}

		return got.Registration.Registration
	}

	tom := register(anna, later, f.tom.ID)
	own := register(anna, early, f.anna.ID)
	mia := register(anna, later, f.mia.ID)
	register(svenP, early, sven.ID)

	gone := register(anna, draft, f.mia.ID)
	if _, err := svc.CancelOwn(ctx, anna, gone.ID); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.Unpublish(ctx, draft.ID); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.CancelOwn(ctx, anna, own.ID); err != nil {
		t.Fatal(err)
	}

	got, err := svc.Mine(ctx, anna)
	if err != nil {
		t.Fatal(err)
	}

	var order []domain.EventRegistrationID
	for _, m := range got {
		order = append(order, m.Registration.Registration.ID)
	}

	// Open first by the first session, Tom before Mia by registration;
	// then the canceled one; the draft's left out.
	if want := []domain.EventRegistrationID{tom.ID, mia.ID, own.ID}; !slices.Equal(order, want) {
		t.Fatalf("my events %v, want %v", order, want)
	}

	if !got[0].Cancelable || !got[1].Cancelable || got[2].Cancelable || got[0].Registration.Name != "Tom Example" ||
		got[0].Event.Event.ID != later.ID || got[2].Event.Event.ID != early.ID || got[2].Event.FreePlaces != 1 {
		t.Errorf("my events %+v", got)
	}

	// After the deadline nothing is cancelable.
	late, err := f.eventsAt(t, "2026-10-23T10:00:00Z").Mine(ctx, anna)
	if err != nil || len(late) != 3 || late[0].Cancelable || late[1].Cancelable {
		t.Errorf("late %+v, %v", late, err)
	}

	// Sven sees his own, Tom only his.
	if mine, err := svc.Mine(ctx, svenP); err != nil || len(mine) != 1 ||
		mine[0].Registration.Registration.PersonID != sven.ID {
		t.Errorf("Sven's %+v, %v", mine, err)
	}

	if mine, err := svc.Mine(ctx, f.principal(t, f.tom)); err != nil || len(mine) != 1 ||
		mine[0].Registration.Registration.ID != tom.ID {
		t.Errorf("Tom's %+v, %v", mine, err)
	}

	if mine, err := svc.Mine(ctx, Principal{AccountID: anna.AccountID}); err != nil || mine == nil || len(mine) != 0 {
		t.Errorf("without a person %+v, %v", mine, err)
	}
}

// TestMyEventsWithoutSessions: an event whose sessions are deleted sorts
// after those with sessions.
func TestMyEventsWithoutSessions(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newEventFixture(t)
	anna := f.principal(t, f.anna)
	svc := f.events(t)
	bare, sessions := f.publishedAt(t, f.eventData(), 10)
	e, _ := f.publishedAt(t, f.eventData(), 12)

	first, err := svc.Register(ctx, anna, bare.ID, f.anna.ID)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := svc.CancelOwn(ctx, anna, first.Registration.Registration.ID); err != nil {
		t.Fatal(err)
	}

	for _, l := range sessions {
		if err := svc.lessons.DeleteLesson(ctx, l.ID); err != nil {
			t.Fatal(err)
		}
	}

	second, err := svc.Register(ctx, anna, e.ID, f.anna.ID)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := svc.CancelOwn(ctx, anna, second.Registration.Registration.ID); err != nil {
		t.Fatal(err)
	}

	got, err := svc.Mine(ctx, anna)
	if err != nil || len(got) != 2 || got[0].Event.Event.ID != e.ID || got[1].Event.Event.ID != bare.ID ||
		len(got[1].Event.Sessions) != 0 {
		t.Errorf("my events %+v, %v", got, err)
	}
}

// TestMyEventReceipts: a guardian downloads the receipt and the Storno of
// a dependent's registration whoever paid it (Decision 2); another
// family's and a guest's are not found.
func TestMyEventReceipts(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newEventFixture(t)
	e, _ := f.published(t, f.eventData())
	anna := f.principal(t, f.anna)
	sven, svenP := f.stranger(t)
	receipts := f.receiptService(t, bookingNow)

	// Mia pays for herself.
	mia := f.paid(t, f.register(t, e, NewRegistration{PersonID: f.mia.ID}))
	guest := f.paid(t, f.register(t, e, NewRegistration{GuestName: "Gina Guest", GuestEmail: "gina@example.org"}))
	svens := f.paid(t, f.register(t, e, NewRegistration{PersonID: sven.ID, ConfirmOverCapacity: true}))

	if _, err := f.events(t).CancelOwn(ctx, anna, mia.ID); err != nil {
		t.Fatal(err)
	}

	storno := f.registration(t, mia.ID).CancellationReceiptID

	for _, id := range []domain.ReceiptID{mia.ReceiptID, storno} {
		if pdf, err := receipts.MyPDF(ctx, anna, id); err != nil || len(pdf.PDF) == 0 {
			t.Errorf("Anna's family's receipt %v: %v", id, err)
		}

		if _, err := receipts.MyPDF(ctx, svenP, id); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("Sven, receipt %v: %v, want ErrNotFound", id, err)
		}
	}

	for _, id := range []domain.ReceiptID{guest.ReceiptID, svens.ReceiptID} {
		if _, err := receipts.MyPDF(ctx, anna, id); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("Anna, receipt %v: %v, want ErrNotFound", id, err)
		}
	}

	// Sven paid his own.
	if _, err := receipts.MyPDF(ctx, svenP, svens.ReceiptID); err != nil {
		t.Errorf("Sven's own: %v", err)
	}

	// A pass's receipt is still the payer's only.
	pass := f.sell(t, f.mia, f.single, "")
	if _, err := receipts.MyPDF(ctx, anna, pass.ReceiptID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("Mia's pass: %v, want ErrNotFound", err)
	}
}
