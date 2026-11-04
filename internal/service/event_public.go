package service

import (
	"bytes"
	"cmp"
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/mail"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// The public and the members' side of events (ADR 0019): anyone reads the
// published events and, by slug, a canceled or past one; a signed-in member
// registers themself and their dependents (ADR 0008) within the sign-up
// window, cancels such a registration before the cancellation deadline, and
// lists their family's registrations (My events). A draft is the office's
// only: it is domain.ErrNotFound here. Registering and canceling run under
// the event's lock, which comes before the person's (event → persons →
// term → class → lesson → passes).

// ErrTooLate refuses a member's own cancellation of a registration once the
// event's cancellation deadline has passed (ADR 0019); the office can still
// cancel it.
var ErrTooLate = fmt.Errorf("%w: the cancellation deadline has passed", domain.ErrConflict)

// PublicEvent is an event as anyone sees it: published, or canceled or past
// by slug, with its sessions and its free places now. It names no
// registrant.
type PublicEvent struct {
	Event domain.Event
	// CourseType is the event's course type.
	CourseType domain.CourseType
	// Sessions are all the event's sessions, canceled ones included (their
	// status tells), ordered by start, each with the event's slug.
	Sessions []ScheduledLesson
	// FreePlaces is the capacity minus the registrations with a place and
	// the open invites, never below 0.
	FreePlaces int
	// SignupOpen tells that the event is published and the sign-up window
	// is open now, whether places are left or not.
	SignupOpen bool
	// SignupClosesAt is when the sign-up closes: the event's own close,
	// else the start of its first scheduled session; zero without either.
	SignupClosesAt time.Time
	// CancelUntil is the deadline of a member's own cancellation: the
	// event's own, else SignupClosesAt.
	CancelUntil time.Time
	// HasImage tells that the event has an image (PublicImage).
	HasImage bool
}

// MyEvent is a registration of the caller's family with its event.
type MyEvent struct {
	Registration RegistrationView
	Event        PublicEvent
	// Cancelable tells that the member may cancel the registration now
	// (CancelOwn): it has a place, the event is published, and the
	// cancellation deadline has not passed.
	Cancelable bool
}

// signupWindow returns when the sign-up of event e with its sessions
// (ordered by start) closes and the members' cancellation deadline, the
// defaults filled in (ADR 0019), and whether e takes registrations at now:
// it is published, its window has opened (at publishing without its own
// opening) and not closed. Without its own close and without a scheduled
// session both instants are zero and the window is shut.
func signupWindow(e domain.Event, sessions []ScheduledLesson, now time.Time) (closes, cancelUntil time.Time, open bool) {
	closes = e.SignupClosesAt
	if closes.IsZero() {
		if i := slices.IndexFunc(sessions, func(sl ScheduledLesson) bool {
			return sl.Lesson.Status == domain.LessonScheduled
		}); i >= 0 {
			closes = sessions[i].Lesson.StartsAt
		}
	}

	cancelUntil = cmp.Or(e.CancelUntil, closes)
	open = e.Status == domain.EventPublished && !now.Before(e.SignupOpensAt) && !closes.IsZero() && now.Before(closes)

	return closes, cancelUntil, open
}

// publicEvents returns the events as anyone sees them at now: a constant
// number of queries plus one per event for its image's info.
func publicEvents(
	ctx context.Context, tx store.Tx, events []domain.Event, now time.Time,
) ([]PublicEvent, error) {
	out := make([]PublicEvent, len(events))
	if len(events) == 0 {
		return out, nil
	}

	parts, err := readEventParts(ctx, tx, events, now, true)
	if err != nil {
		return nil, err
	}

	for i, e := range events {
		img, err := tx.Events().ImageInfo(ctx, e.ID)
		if err != nil {
			return nil, fmt.Errorf("event %v image: %w", e.ID, err)
		}

		c := parts.counts[e.ID]
		pe := PublicEvent{
			// An event keeps its course type (CourseTypeRepo.Delete refuses
			// it), so the catalog has it.
			Event: e, CourseType: parts.refs.courseTypes[e.CourseTypeID], Sessions: parts.sessionsOf(e.ID),
			FreePlaces: max(0, e.Capacity-c.Registered-c.Invited), HasImage: !img.IsZero(),
		}
		pe.SignupClosesAt, pe.CancelUntil, pe.SignupOpen = signupWindow(e, pe.Sessions, now)
		out[i] = pe
	}

	return out, nil
}

// publicEvent is publicEvents of one event.
func publicEvent(ctx context.Context, tx store.Tx, e domain.Event, now time.Time) (PublicEvent, error) {
	out, err := publicEvents(ctx, tx, []domain.Event{e}, now)
	if err != nil {
		return PublicEvent{}, err
	}

	return out[0], nil
}

// upcoming reports whether one of the event's scheduled sessions has not
// ended at now.
func upcoming(pe PublicEvent, now time.Time) bool {
	return slices.ContainsFunc(pe.Sessions, func(sl ScheduledLesson) bool {
		return sl.Lesson.Status == domain.LessonScheduled && sl.Lesson.EndsAt.After(now)
	})
}

// Published returns the published events with a scheduled session that has
// not ended, in the order of EventRepo.List (by their first session). It
// reads a constant number of queries plus one per event (its image's info).
func (s *EventService) Published(ctx context.Context) ([]PublicEvent, error) {
	var out []PublicEvent

	err := s.store.InTx(ctx, store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		events, err := tx.Events().List(ctx)
		if err != nil {
			return err
		}

		events = slices.DeleteFunc(events, func(e domain.Event) bool { return e.Status != domain.EventPublished })
		now := s.clock.Now()

		all, err := publicEvents(ctx, tx, events, now)
		if err != nil {
			return err
		}

		out = slices.DeleteFunc(all, func(pe PublicEvent) bool { return !upcoming(pe, now) })

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("published events: %w", err)
	}

	return out, nil
}

// bySlug returns the published or canceled event with the slug; a draft or
// an unknown slug is domain.ErrNotFound.
func bySlug(ctx context.Context, tx store.Tx, slug string) (domain.Event, error) {
	e, err := tx.Events().BySlug(ctx, slug)
	if err != nil {
		return domain.Event{}, err
	}

	if e.Status == domain.EventDraft {
		return domain.Event{}, fmt.Errorf("%w: the event is a draft", domain.ErrNotFound)
	}

	return e, nil
}

// BySlug returns the published or canceled event with the slug, past ones
// too; a draft or an unknown slug is domain.ErrNotFound.
func (s *EventService) BySlug(ctx context.Context, slug string) (PublicEvent, error) {
	var out PublicEvent

	err := s.store.InTx(ctx, store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		e, err := bySlug(ctx, tx, slug)
		if err != nil {
			return err
		}

		out, err = publicEvent(ctx, tx, e, s.clock.Now())

		return err
	})
	if err != nil {
		return PublicEvent{}, fmt.Errorf("event %q: %w", slug, err)
	}

	return out, nil
}

// PublicImage returns the image of the published or canceled event with the
// slug, the zero Background without one; a draft or an unknown slug is
// domain.ErrNotFound.
func (s *EventService) PublicImage(ctx context.Context, slug string) (domain.Background, error) {
	var out domain.Background

	err := s.store.InTx(ctx, store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		e, err := bySlug(ctx, tx, slug)
		if err != nil {
			return err
		}

		out, err = tx.Events().Image(ctx, e.ID)

		return err
	})
	if err != nil {
		return domain.Background{}, fmt.Errorf("event %q image: %w", slug, err)
	}

	return out, nil
}

// PublicEventID returns the ID of the published or canceled event with the
// slug, for the writes addressed by slug (Register); a draft or an unknown
// slug is domain.ErrNotFound.
func (s *EventService) PublicEventID(ctx context.Context, slug string) (domain.EventID, error) {
	var out domain.EventID

	err := s.store.InTx(ctx, store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		e, err := bySlug(ctx, tx, slug)
		out = e.ID

		return err
	})
	if err != nil {
		return domain.EventID{}, fmt.Errorf("event %q: %w", slug, err)
	}

	return out, nil
}

// Register registers person, the member p's own or a dependent's (ADR
// 0008), for the event as p, and returns it with the event as it is then.
// An unknown event, and a person who is not of p's family, merged or
// anonymized, are domain.ErrNotFound; the person's refusal comes before
// every other, so nobody learns about another family's persons. A draft or
// canceled event, and one outside its sign-up window, is ErrEventClosed; a
// full one ErrEventFull; a person with an open registration
// domain.ErrDuplicate. The registration snapshots the event's price and VAT
// rate: a free one is paid, a priced one unpaid until the office takes the
// fee at the desk. It is audited, and p's account gets the
// event-registered mail.
func (s *EventService) Register(
	ctx context.Context, p Principal, event domain.EventID, person domain.PersonID,
) (MyEvent, error) {
	var out MyEvent

	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		settings, loc, err := loadSchool(ctx, tx)
		if err != nil {
			return err
		}

		e, err := tx.Events().Lock(ctx, event)
		if err != nil {
			return err
		}

		if _, err := familyMember(ctx, tx, p, person); err != nil {
			return err
		}

		// Lock order: the event, then the person.
		who, err := lockLivePerson(ctx, tx, person)
		if err != nil {
			return err
		}

		now := s.clock.Now()

		pe, err := publicEvent(ctx, tx, e, now)
		if err != nil {
			return err
		}

		switch {
		case !pe.SignupOpen:
			return ErrEventClosed
		case pe.FreePlaces == 0:
			return ErrEventFull
		}

		r := s.newRegistration(p, e, now)
		r.PersonID = person

		if err := tx.EventRegistrations().Create(ctx, r); err != nil {
			return err
		}

		if err := s.audit(ctx, tx, p, now, domain.AuditEventRegistrationAdded, e.ID, &r, false); err != nil {
			return err
		}

		// The place was free, so it is taken now.
		pe.FreePlaces--
		out = MyEvent{
			Registration: RegistrationView{Registration: r, Name: fullName(who)}, Event: pe,
			Cancelable: cancelable(r, pe, now),
		}

		return s.mailRegistration(ctx, tx, p, settings, loc, now, mail.TemplateEventRegistered, out, false)
	})
	if err != nil {
		return MyEvent{}, fmt.Errorf("register for event %v: %w", event, err)
	}

	return out, nil
}

// newRegistration returns a registration of event e by p at now, without
// whom it is for: registered, at the event's price and VAT rate, and paid
// if free, unpaid otherwise.
func (s *EventService) newRegistration(p Principal, e domain.Event, now time.Time) domain.EventRegistration {
	r := domain.EventRegistration{
		ID: ids.New[domain.EventRegistrationID](s.ids), EventID: e.ID, Status: domain.EventRegistered,
		Payment: domain.EventUnpaid, Price: e.Price, VATRate: e.VATRate, RegisteredBy: actorOf(p),
		CreatedAt: now, UpdatedAt: now,
	}

	if r.Price == 0 {
		r.Payment = domain.EventPaid
	}

	return r
}

// cancelable reports whether the member may cancel r of the event pe at
// now: r has a place, the event is published, and its deadline is ahead.
func cancelable(r domain.EventRegistration, pe PublicEvent, now time.Time) bool {
	return r.Status == domain.EventRegistered && pe.Event.Status == domain.EventPublished &&
		now.Before(pe.CancelUntil)
}

// mailRegistration queues template about the registration m to p's
// account, in its language (else the school's): the event-registered mail
// with the fee to pay at the desk (unless paid) and the deadline, or the
// event-registration-canceled mail with whether its fee was refunded.
func (s *EventService) mailRegistration(
	ctx context.Context, tx store.Tx, p Principal, settings domain.Settings, loc *time.Location, now time.Time,
	template string, m MyEvent, refunded bool,
) error {
	rc, err := actingRecipient(ctx, tx, p)
	if err != nil {
		return err
	}

	r := m.Registration.Registration
	data := mail.EventRegistrationData{
		SchoolName: settings.SchoolName, Participant: m.Registration.Name, Title: m.Event.Event.Title,
		Sessions: []mail.BookedLesson{}, Refunded: refunded,
	}

	for _, sl := range m.Event.Sessions {
		if sl.Lesson.Status == domain.LessonScheduled {
			data.Sessions = append(data.Sessions, mail.BookedLesson{
				CourseType: sl.CourseType.Name, StartsAt: sl.Lesson.StartsAt.In(loc), EndsAt: sl.Lesson.EndsAt.In(loc),
				Room: sl.Room.Name, Location: sl.Location.Name,
			})
		}
	}

	if template == mail.TemplateEventRegistered && !m.Event.CancelUntil.IsZero() {
		data.CancelUntil = m.Event.CancelUntil.In(loc)
	}

	return sendMails(ctx, tx, s.ids, s.mails.Renderer, settings, now, []recipient{rc}, template, mailOpts{},
		func(rc recipient) any {
			d := data
			d.GivenName = rc.givenName

			if template == mail.TemplateEventRegistered && r.Payment == domain.EventUnpaid {
				_, words := receiptLanguage(cmp.Or(rc.locale, settings.DefaultLocale))
				d.Fee = words.formatMoney(r.Price, settings.Currency)
			}

			return d
		})
}

// CancelOwn cancels a registration of the member p's family (ADR 0019): of
// p's own person or a dependent's. A paid fee is refunded by a Storno of
// its receipt, which makes the registration refunded. It returns the
// registration with its event as they are then. An unknown registration,
// and one of anyone else or of a guest, is domain.ErrNotFound; a canceled
// one ErrRegistrationCanceled; one on the waiting list ErrNotRegistered;
// one whose event's cancellation deadline has passed, or that has none (no
// scheduled session left), ErrTooLate. A refund with incomplete receipt
// settings is ErrReceiptSettingsIncomplete. It is audited, with whether it
// refunded, and p's account gets the event-registration-canceled mail.
func (s *EventService) CancelOwn(ctx context.Context, p Principal, id domain.EventRegistrationID) (MyEvent, error) {
	var out MyEvent

	if err := s.awaitRefund(ctx, p, id); err != nil {
		return MyEvent{}, fmt.Errorf("cancel registration %v: %w", id, err)
	}

	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		settings, loc, err := loadSchool(ctx, tx)
		if err != nil {
			return err
		}

		r, err := lockRegistration(ctx, tx, id)
		if err != nil {
			return err
		}

		who, err := familyMember(ctx, tx, p, r.PersonID)
		if err != nil {
			return err
		}

		switch r.Status {
		case domain.EventRegistrationCanceled:
			return ErrRegistrationCanceled
		case domain.EventWaiting, domain.EventInvited:
			return ErrNotRegistered
		}

		e, err := tx.Events().Get(ctx, r.EventID)
		if err != nil {
			return err
		}

		now := s.clock.Now()

		before, err := publicEvent(ctx, tx, e, now)
		if err != nil {
			return err
		}

		if !now.Before(before.CancelUntil) {
			return ErrTooLate
		}

		r, refunded, err := s.cancelRegistration(ctx, tx, settings, loc, p, now, r, true)
		if err != nil {
			return err
		}

		if err := s.audit(ctx, tx, p, now, domain.AuditEventRegistrationCanceled, e.ID, &r, refunded); err != nil {
			return err
		}

		after, err := publicEvent(ctx, tx, e, now)
		if err != nil {
			return err
		}

		out = MyEvent{
			Registration: RegistrationView{Registration: r, Name: fullName(who), Anonymized: who.Anonymized()},
			Event:        after,
		}

		return s.mailRegistration(ctx, tx, p, settings, loc, now, mail.TemplateEventRegistrationCanceled, out, refunded)
	})
	if err != nil {
		return MyEvent{}, fmt.Errorf("cancel registration %v: %w", id, err)
	}

	return out, nil
}

// awaitRefund waits for the receipt renderer before registration id is
// canceled if its fee was paid with a receipt, which the cancellation
// refunds by a Storno. A registration of anyone outside p's family is
// domain.ErrNotFound before anything is waited for, so its payment shows
// nowhere.
func (s *EventService) awaitRefund(ctx context.Context, p Principal, id domain.EventRegistrationID) error {
	paid := false

	err := s.store.InTx(ctx, store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		r, err := tx.EventRegistrations().Get(ctx, id)
		if err != nil {
			return err
		}

		if _, err := familyMember(ctx, tx, p, r.PersonID); err != nil {
			return err
		}

		paid = paidWithReceipt(r)

		return nil
	})
	if err != nil {
		return err
	}

	if !paid {
		return nil
	}

	return s.receipts.awaitRenderer(ctx)
}

// Mine returns the registrations of the member p's family, p's own person
// and their dependents, canceled ones included, each with its event: the
// open ones first, then by the event's first session (events without
// sessions last), then by when they were made. Registrations for a draft
// are left out; without a person, p has none. It reads a constant number
// of queries plus one per event (its image's info).
func (s *EventService) Mine(ctx context.Context, p Principal) ([]MyEvent, error) {
	out := []MyEvent{}

	err := s.store.InTx(ctx, store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		if p.PersonID.IsZero() {
			return nil
		}

		dependents, err := tx.Persons().ListDependents(ctx, p.PersonID)
		if err != nil {
			return err
		}

		regs, err := tx.EventRegistrations().ListByPersons(ctx, append([]domain.PersonID{p.PersonID}, personIDs(dependents)...))
		if err != nil {
			return err
		}

		events, err := tx.Events().ListByIDs(ctx, mapSlice(regs, func(r domain.EventRegistration) domain.EventID {
			return r.EventID
		}))
		if err != nil {
			return err
		}

		events = slices.DeleteFunc(events, func(e domain.Event) bool { return e.Status == domain.EventDraft })
		now := s.clock.Now()

		views, err := publicEvents(ctx, tx, events, now)
		if err != nil {
			return err
		}

		byEvent := make(map[domain.EventID]PublicEvent, len(views))
		for _, pe := range views {
			byEvent[pe.Event.ID] = pe
		}

		regs = slices.DeleteFunc(regs, func(r domain.EventRegistration) bool {
			_, ok := byEvent[r.EventID]

			return !ok
		})

		named, err := registrationViews(ctx, tx, regs)
		if err != nil {
			return err
		}

		for _, rv := range named {
			pe := byEvent[rv.Registration.EventID]
			out = append(out, MyEvent{Registration: rv, Event: pe, Cancelable: cancelable(rv.Registration, pe, now)})
		}

		slices.SortStableFunc(out, byOpenAndStart)

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("my events: %w", err)
	}

	return out, nil
}

// byOpenAndStart orders My events: open registrations first, then by the
// event's first session (events without sessions last), then by when the
// registration was made and its ID.
func byOpenAndStart(a, b MyEvent) int {
	ra, rb := a.Registration.Registration, b.Registration.Registration
	closed := func(r domain.EventRegistration) int {
		if r.Open() {
			return 0
		}

		return 1
	}

	return cmp.Or(
		cmp.Compare(closed(ra), closed(rb)),
		byFirstSession(a.Event.Sessions, b.Event.Sessions),
		ra.CreatedAt.Compare(rb.CreatedAt),
		bytes.Compare(ra.ID[:], rb.ID[:]),
	)
}

// byFirstSession orders by the start of the first session, without
// sessions last.
func byFirstSession(a, b []ScheduledLesson) int {
	none := func(s []ScheduledLesson) int {
		if len(s) == 0 {
			return 1
		}

		return 0
	}

	if c := cmp.Compare(none(a), none(b)); c != 0 || len(a) == 0 {
		return c
	}

	return a[0].Lesson.StartsAt.Compare(b[0].Lesson.StartsAt)
}
