package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"

	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/mail"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// The office's administration of events (ADR 0019): it creates, edits,
// publishes and cancels events, adds their sessions (lessons of kind
// workshop with Lesson.EventID), registers persons and guests and takes
// their fees at the desk (event_registrations.go, event_payment.go). Every
// change of an event, its registrations or its sessions' capacity runs
// under the event's lock, before any lock of its lessons (event → lessons
// → passes). The HTTP layer admits only office and admin accounts.

var (
	// ErrEventNoSessions refuses to publish an event without a scheduled
	// session: there is nothing to register for.
	ErrEventNoSessions = fmt.Errorf("%w: the event has no scheduled session", domain.ErrConflict)
	// ErrEventNotDraft refuses to publish an event that is published or
	// canceled already.
	ErrEventNotDraft = fmt.Errorf("%w: the event is not a draft", domain.ErrConflict)
	// ErrEventNotPublished refuses to take back an event that is a draft
	// or canceled.
	ErrEventNotPublished = fmt.Errorf("%w: the event is not published", domain.ErrConflict)
	// ErrEventClosed refuses a registration, or a new session, of an event
	// that takes none: a draft (registering is for published events; a
	// session may be added to it) or a canceled event. Canceling a
	// canceled event again is ErrEventClosed too.
	ErrEventClosed = fmt.Errorf("%w: the event takes no registrations", domain.ErrConflict)
	// errEventCourseType refuses to change the course type of an event
	// that has sessions, which carry it.
	errEventCourseType error = &domain.FieldError{
		Field: "courseTypeId", Msg: "the course type of an event with sessions cannot change",
	}
	// errNoSlug is a title that gives no slug, so the office must choose
	// one.
	errNoSlug error = &domain.FieldError{
		Field: "slug", Msg: "the title gives no slug: choose one of lowercase letters, digits and hyphens",
	}
)

// maxSlugTries bounds the derived slugs tried for a new event: the title's,
// then with "-2", "-3" and so on.
const maxSlugTries = 50

// EventService is the office's event administration (ADR 0019).
type EventService struct {
	store    store.Store
	clock    clock.Clock
	ids      ids.Source
	mails    Mails
	receipts *ReceiptService
	// lessons creates and cancels the sessions as the office's lesson
	// administration does.
	lessons *LessonService
}

// NewEventService returns an EventService that sends its mails with mails
// and issues the receipts of the fees with receipts.
func NewEventService(
	st store.Store, clk clock.Clock, src ids.Source, mails Mails, receipts *ReceiptService,
) *EventService {
	return &EventService{
		store: st, clock: clk, ids: src, mails: mails, receipts: receipts,
		lessons: NewLessonService(st, clk, src, mails),
	}
}

// EventData are the fields the office edits on an event. Texts are
// trimmed; a zero instant is not set.
type EventData struct {
	// Slug names the event in its URL. Empty, a new event's is derived
	// from the title and an edited event keeps its own.
	Slug         string
	Title        string
	Description  string
	CourseTypeID domain.CourseTypeID
	Capacity     int
	Price        domain.Money
	VATRate      int
	// SignupOpensAt and SignupClosesAt bound the sign-up window,
	// CancelUntil is the members' cancellation deadline (ADR 0019).
	SignupOpensAt  time.Time
	SignupClosesAt time.Time
	CancelUntil    time.Time
}

// apply sets the fields of d on e but its slug.
func (d EventData) apply(e *domain.Event) {
	e.Title, e.Description = strings.TrimSpace(d.Title), strings.TrimSpace(d.Description)
	e.CourseTypeID, e.Capacity, e.Price, e.VATRate = d.CourseTypeID, d.Capacity, d.Price, d.VATRate
	e.SignupOpensAt, e.SignupClosesAt, e.CancelUntil = d.SignupOpensAt, d.SignupClosesAt, d.CancelUntil
}

// EventAdmin is an event as the office sees it.
type EventAdmin struct {
	Event domain.Event
	// Sessions are the event's sessions, canceled ones included, ordered
	// by start.
	Sessions []ScheduledLesson
	// Counts counts the open registrations now.
	Counts store.EventRegistrationCounts
	// Image is the type and digest of the event's image, zero without one;
	// only Get reads it.
	Image domain.BackgroundInfo
}

// Create adds a draft event created by p (ADR 0019). Without a slug, the
// title's (slugify) is taken, with "-2", "-3" and so on appended while
// another event has it; a title that gives none is a validation error on
// slug. A slug that is given and taken is domain.ErrDuplicate. An unknown
// course type is a validation error on courseTypeId, other invalid fields
// on theirs.
func (s *EventService) Create(ctx context.Context, p Principal, d EventData) (EventAdmin, error) {
	var out EventAdmin

	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		now := s.clock.Now()
		e := domain.Event{
			ID: ids.New[domain.EventID](s.ids), Status: domain.EventDraft, CreatedAt: now, UpdatedAt: now,
			CreatedBy: actorOf(p),
		}
		d.apply(&e)

		slug, err := s.slugOf(ctx, tx, d.Slug, e.Title)
		if err != nil {
			return err
		}

		e.Slug = slug

		if err := s.write(ctx, tx, e, tx.Events().Create); err != nil {
			return err
		}

		out = EventAdmin{Event: e, Sessions: []ScheduledLesson{}}

		return nil
	})
	if err != nil {
		return EventAdmin{}, fmt.Errorf("create event: %w", err)
	}

	return out, nil
}

// slugOf returns the slug of a new event: given, trimmed, or else the
// first of the title's slug and its numbered variants that no event has.
func (s *EventService) slugOf(ctx context.Context, tx store.Tx, given, title string) (string, error) {
	if given = strings.TrimSpace(given); given != "" {
		return given, nil
	}

	base := slugify(title)
	if base == "" {
		return "", errNoSlug
	}

	for n := 1; n <= maxSlugTries; n++ {
		candidate := base
		if n > 1 {
			suffix := "-" + strconv.Itoa(n)
			candidate = cutSlug(base, domain.MaxEventSlugLength-len(suffix)) + suffix
		}

		switch _, err := tx.Events().BySlug(ctx, candidate); {
		case errors.Is(err, domain.ErrNotFound):
			return candidate, nil
		case err != nil:
			return "", err
		}
	}

	return "", fmt.Errorf("%w: the slugs of the title are taken; choose one", domain.ErrDuplicate)
}

// write validates e, checks its course type and stores it with put (the
// repository's Create or Update).
func (s *EventService) write(
	ctx context.Context, tx store.Tx, e domain.Event, put func(context.Context, domain.Event) error,
) error {
	if err := e.Validate(); err != nil {
		return err
	}

	switch _, err := tx.CourseTypes().Get(ctx, e.CourseTypeID); {
	case errors.Is(err, domain.ErrNotFound):
		return errUnknownCourseType
	case err != nil:
		return err
	}

	return put(ctx, e)
}

// slugify derives an event's slug from a title: lowercase, ä, ö, ü and ß
// spelled ae, oe, ue and ss, other accents dropped, every run of other
// characters a single hyphen, no hyphen at either end, and cut to
// domain.MaxEventSlugLength at a hyphen. It is empty for a title of
// symbols only.
func slugify(title string) string {
	s := strings.NewReplacer("ä", "ae", "ö", "oe", "ü", "ue", "ß", "ss").Replace(strings.ToLower(title))

	var b strings.Builder

	hyphen := false

	for _, r := range norm.NFD.String(s) {
		switch {
		case unicode.Is(unicode.Mn, r):
			// An accent of the letter before it.
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			if hyphen && b.Len() > 0 {
				b.WriteByte('-')
			}

			b.WriteRune(r)

			hyphen = false
		default:
			hyphen = true
		}
	}

	return cutSlug(b.String(), domain.MaxEventSlugLength)
}

// cutSlug cuts slug to at most n characters at a hyphen, or within its
// first group if that alone is longer.
func cutSlug(slug string, n int) string {
	if len(slug) <= n {
		return slug
	}

	if i := strings.LastIndexByte(slug[:n+1], '-'); i > 0 {
		return slug[:i]
	}

	return slug[:n]
}

// Update replaces the editable fields of an event (domain.ErrNotFound for
// an unknown one) under its lock; its status stays. Lowering the capacity
// below the places that registrations and open invites take is a
// validation error on capacity; a new capacity is written to every session that has not
// started, so a session always has its event's capacity. The course type
// of an event with sessions stays (a validation error on courseTypeId).
// A slug that is given and taken is domain.ErrDuplicate.
func (s *EventService) Update(ctx context.Context, id domain.EventID, d EventData) (EventAdmin, error) {
	var out EventAdmin

	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		old, err := tx.Events().Lock(ctx, id)
		if err != nil {
			return err
		}

		now := s.clock.Now()
		e := old
		d.apply(&e)
		e.Slug, e.UpdatedAt = cmpOrTrim(d.Slug, old.Slug), now

		sessions, err := tx.Lessons().ListByEvent(ctx, id)
		if err != nil {
			return err
		}

		if e.CourseTypeID != old.CourseTypeID && len(sessions) > 0 {
			return errEventCourseType
		}

		counts, err := tx.EventRegistrations().CountsByEvents(ctx, []domain.EventID{id}, now)
		if err != nil {
			return err
		}

		// Only a lower capacity can conflict: an event registered beyond
		// its capacity (the office's override) stays editable otherwise.
		if taken := counts[id].Registered + counts[id].Invited; e.Capacity < old.Capacity && e.Capacity < taken {
			return &domain.FieldError{
				Field: "capacity", Msg: fmt.Sprintf("%d places are taken, more than the capacity", taken),
			}
		}

		if err := s.write(ctx, tx, e, tx.Events().Update); err != nil {
			return err
		}

		c, err := chargesIn(ctx, tx)
		if err != nil {
			return err
		}

		if err := syncCapacity(ctx, tx, c, now, sessions, e.Capacity); err != nil {
			return err
		}

		out, err = s.admin(ctx, tx, e, true)

		return err
	})
	if err != nil {
		return EventAdmin{}, fmt.Errorf("update event %v: %w", id, err)
	}

	return out, nil
}

// cmpOrTrim returns s trimmed, or else def.
func cmpOrTrim(s, def string) string {
	if s = strings.TrimSpace(s); s != "" {
		return s
	}

	return def
}

// syncCapacity gives every session that has not started at now, canceled
// ones included (they may be restored), the capacity, each under its lock,
// through the ledger with the school's charges c. The caller holds the
// event's lock.
func syncCapacity(
	ctx context.Context, tx store.Tx, c charges, now time.Time, sessions []domain.Lesson, capacity int,
) error {
	for _, l := range sessions {
		if l.Capacity == capacity || !l.StartsAt.After(now) {
			continue
		}

		old, err := tx.Lessons().Lock(ctx, l.ID)
		if err != nil {
			return err
		}

		// The event's capacity is a lesson's too (1 or more).
		l = old
		l.Capacity = capacity

		if err := updateLesson(ctx, tx, c, now, old, l); err != nil {
			return err
		}
	}

	return nil
}

// Delete deletes an event with its sessions and image (domain.ErrNotFound
// for an unknown one). An event that has registrations, canceled ones
// included, or a session that has started is domain.ErrInUse: it is
// canceled instead.
func (s *EventService) Delete(ctx context.Context, id domain.EventID) error {
	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		if _, err := tx.Events().Lock(ctx, id); err != nil {
			return err
		}

		regs, err := tx.EventRegistrations().ListByEvent(ctx, id)
		if err != nil {
			return err
		}

		if len(regs) > 0 {
			return fmt.Errorf("%w: the event has registrations", domain.ErrInUse)
		}

		sessions, err := tx.Lessons().ListByEvent(ctx, id)
		if err != nil {
			return err
		}

		now := s.clock.Now()
		if slices.ContainsFunc(sessions, func(l domain.Lesson) bool { return !l.StartsAt.After(now) }) {
			return fmt.Errorf("%w: a session of the event has started", domain.ErrInUse)
		}

		for _, l := range sessions {
			if _, err := tx.Lessons().Lock(ctx, l.ID); err != nil {
				return err
			}

			if err := tx.Lessons().Delete(ctx, l.ID); err != nil {
				return err
			}
		}

		return tx.Events().Delete(ctx, id)
	})
	if err != nil {
		return fmt.Errorf("delete event %v: %w", id, err)
	}

	return nil
}

// Publish shows a draft event and its sessions to everyone (ADR 0019). An
// event that is not a draft is ErrEventNotDraft, one without a scheduled
// session ErrEventNoSessions.
func (s *EventService) Publish(ctx context.Context, id domain.EventID) (EventAdmin, error) {
	return s.setStatus(ctx, id, func(_ context.Context, _ store.Tx, e domain.Event, sessions []domain.Lesson) error {
		switch {
		case e.Status != domain.EventDraft:
			return ErrEventNotDraft
		case !slices.ContainsFunc(sessions, func(l domain.Lesson) bool { return l.Status == domain.LessonScheduled }):
			return ErrEventNoSessions
		}

		return nil
	}, domain.EventPublished)
}

// Unpublish makes a published event a draft again, seen by the office
// only. One that is not published is ErrEventNotPublished, one with open
// registrations domain.ErrInUse: they would lose sight of it.
func (s *EventService) Unpublish(ctx context.Context, id domain.EventID) (EventAdmin, error) {
	return s.setStatus(ctx, id, func(ctx context.Context, tx store.Tx, e domain.Event, _ []domain.Lesson) error {
		if e.Status != domain.EventPublished {
			return ErrEventNotPublished
		}

		regs, err := tx.EventRegistrations().ListByEvent(ctx, id)
		if err != nil {
			return err
		}

		if slices.ContainsFunc(regs, domain.EventRegistration.Open) {
			return fmt.Errorf("%w: the event has registrations", domain.ErrInUse)
		}

		return nil
	}, domain.EventDraft)
}

// setStatus gives the locked event status once check, given the event and
// its sessions, allows it.
func (s *EventService) setStatus(
	ctx context.Context, id domain.EventID,
	check func(context.Context, store.Tx, domain.Event, []domain.Lesson) error, status domain.EventStatus,
) (EventAdmin, error) {
	var out EventAdmin

	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		e, err := tx.Events().Lock(ctx, id)
		if err != nil {
			return err
		}

		sessions, err := tx.Lessons().ListByEvent(ctx, id)
		if err != nil {
			return err
		}

		if err := check(ctx, tx, e, sessions); err != nil {
			return err
		}

		e.Status, e.UpdatedAt = status, s.clock.Now()
		if err := tx.Events().Update(ctx, e); err != nil {
			return err
		}

		out, err = s.admin(ctx, tx, e, true)

		return err
	})
	if err != nil {
		return EventAdmin{}, fmt.Errorf("event %v: %w", id, err)
	}

	return out, nil
}

// List returns every event as the office sees it, ordered by its first
// session (EventRepo.List), without the images; a fixed number of queries
// for any number of events.
func (s *EventService) List(ctx context.Context) ([]EventAdmin, error) {
	var out []EventAdmin

	err := s.store.InTx(ctx, store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		events, err := tx.Events().List(ctx)
		if err != nil {
			return err
		}

		out, err = s.admins(ctx, tx, events)

		return err
	})
	if err != nil {
		return nil, fmt.Errorf("list events: %w", err)
	}

	return out, nil
}

// Get returns an event as the office sees it, with its image's type and
// digest (domain.ErrNotFound for an unknown one).
func (s *EventService) Get(ctx context.Context, id domain.EventID) (EventAdmin, error) {
	var out EventAdmin

	err := s.store.InTx(ctx, store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		e, err := tx.Events().Get(ctx, id)
		if err != nil {
			return err
		}

		out, err = s.admin(ctx, tx, e, true)

		return err
	})
	if err != nil {
		return EventAdmin{}, fmt.Errorf("event %v: %w", id, err)
	}

	return out, nil
}

// admin returns e as the office sees it, with its image's info if image.
func (s *EventService) admin(ctx context.Context, tx store.Tx, e domain.Event, image bool) (EventAdmin, error) {
	out, err := s.admins(ctx, tx, []domain.Event{e})
	if err != nil {
		return EventAdmin{}, err
	}

	if image {
		if out[0].Image, err = tx.Events().ImageInfo(ctx, e.ID); err != nil {
			return EventAdmin{}, err
		}
	}

	return out[0], nil
}

// admins returns the events as the office sees them, without the images.
func (s *EventService) admins(ctx context.Context, tx store.Tx, events []domain.Event) ([]EventAdmin, error) {
	out := make([]EventAdmin, len(events))
	if len(events) == 0 {
		return out, nil
	}

	ids := mapSlice(events, func(e domain.Event) domain.EventID { return e.ID })

	lessons, err := tx.Lessons().ListByEvents(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("sessions: %w", err)
	}

	counts, err := tx.EventRegistrations().CountsByEvents(ctx, ids, s.clock.Now())
	if err != nil {
		return nil, fmt.Errorf("registrations: %w", err)
	}

	sessions := map[domain.EventID][]ScheduledLesson{}

	if len(lessons) > 0 {
		refs, err := loadCatalogRefs(ctx, tx)
		if err != nil {
			return nil, err
		}

		for _, l := range lessons {
			sl, err := refs.resolve(l)
			if err != nil {
				return nil, err
			}

			sessions[l.EventID] = append(sessions[l.EventID], sl)
		}
	}

	for i, e := range events {
		out[i] = EventAdmin{Event: e, Sessions: sessions[e.ID], Counts: counts[e.ID]}
		if out[i].Sessions == nil {
			out[i].Sessions = []ScheduledLesson{}
		}
	}

	return out, nil
}

// Image returns the event's image, the zero Background without one
// (domain.ErrNotFound for an unknown event).
func (s *EventService) Image(ctx context.Context, id domain.EventID) (domain.Background, error) {
	var out domain.Background

	err := s.store.InTx(ctx, store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		var err error
		out, err = tx.Events().Image(ctx, id)

		return err
	})
	if err != nil {
		return domain.Background{}, fmt.Errorf("event %v image: %w", id, err)
	}

	return out, nil
}

// PutImage replaces the event's image (domain.ErrNotFound for an unknown
// event). One that is not a JPEG, PNG or WebP image of at most
// domain.MaxBackgroundBytes is a validation error on contentType or data,
// as for the school's background (ADR 0018).
func (s *EventService) PutImage(ctx context.Context, id domain.EventID, img domain.Background) error {
	if err := img.Validate(); err != nil {
		return err
	}

	return s.putImage(ctx, id, img)
}

// DeleteImage removes the event's image, if it has one
// (domain.ErrNotFound for an unknown event).
func (s *EventService) DeleteImage(ctx context.Context, id domain.EventID) error {
	return s.putImage(ctx, id, domain.Background{})
}

func (s *EventService) putImage(ctx context.Context, id domain.EventID, img domain.Background) error {
	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		return tx.Events().PutImage(ctx, id, img)
	})
	if err != nil {
		return fmt.Errorf("event %v image: %w", id, err)
	}

	return nil
}

// SessionData are the fields of a new session of an event: the date and
// start time are school-local; AccompanistID is zero for none.
type SessionData struct {
	Date          domain.Date
	StartTime     domain.TimeOfDay
	Duration      time.Duration
	RoomID        domain.RoomID
	InstructorID  domain.StaffID
	AccompanistID domain.StaffID
	// ConfirmRoomOverlap saves even if another lesson uses the room at
	// that time.
	ConfirmRoomOverlap bool
	// ConfirmPast saves even if the session starts in the past.
	ConfirmPast bool
}

// AddSession adds a session to an event (domain.ErrNotFound for an unknown
// one) under its lock: a public lesson of kind workshop without a class,
// with the event's course type and capacity (ADR 0019). A canceled event
// is ErrEventClosed. Otherwise the checks of CreateLesson apply: unknown
// rooms or staff are validation errors on their fields, a start that is
// not in the future ErrLessonPast unless confirmed, another lesson in the
// room a *RoomTakenError unless confirmed. Further changes of the session
// go through the lesson administration.
func (s *EventService) AddSession(ctx context.Context, id domain.EventID, d SessionData) (ScheduledLesson, error) {
	var out ScheduledLesson

	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		settings, loc, err := loadSchool(ctx, tx)
		if err != nil {
			return err
		}

		e, err := tx.Events().Lock(ctx, id)
		if err != nil {
			return err
		}

		if e.Status == domain.EventCanceled {
			return ErrEventClosed
		}

		data := LessonData{
			Date: d.Date, StartTime: d.StartTime, Duration: d.Duration, RoomID: d.RoomID, InstructorID: d.InstructorID,
			AccompanistID: d.AccompanistID, Capacity: e.Capacity, Visibility: domain.VisibilityPublic,
			Kind: domain.LessonWorkshop, ConfirmRoomOverlap: d.ConfirmRoomOverlap, ConfirmPast: d.ConfirmPast,
		}
		l := domain.Lesson{
			ID: ids.New[domain.LessonID](s.ids), CourseTypeID: e.CourseTypeID, Status: domain.LessonScheduled,
			EventID: e.ID,
		}

		if err := s.lessons.apply(&l, data, loc); err != nil {
			return err
		}

		if err := lessonReferences(ctx, tx, l); err != nil {
			return err
		}

		if err := s.lessons.confirmPast(l, data); err != nil {
			return err
		}

		if !d.ConfirmRoomOverlap {
			if err := roomFree(ctx, tx, l); err != nil {
				return err
			}
		}

		if err := tx.Lessons().Create(ctx, l); err != nil {
			return err
		}

		// A session has no bookings and no waiting list.
		out, err = adminLesson(ctx, tx, settings, l, 0, 0)

		return err
	})
	if err != nil {
		return ScheduledLesson{}, fmt.Errorf("add session to event %v: %w", id, err)
	}

	return out, nil
}

// Cancel cancels an event as the office account p, with an optional note
// (domain.ErrNotFound for an unknown event, ErrEventClosed for one canceled
// already; a note longer than domain.MaxCancelNoteLength is a validation
// error on note), in one transaction under its lock (ADR 0019): its sessions
// that have not started are canceled like lessons, without their own
// mails; every open registration is canceled, and a paid one refunded by
// a Storno of its receipt, even once the event has started; and everyone
// registered gets one event-canceled mail. The cancellation is audited.
func (s *EventService) Cancel(ctx context.Context, p Principal, id domain.EventID, note string) (EventAdmin, error) {
	var out EventAdmin

	// The note goes to the sessions it cancels (domain.Lesson) and the
	// mail, so it is checked even when no session is ahead.
	if utf8.RuneCountInString(strings.TrimSpace(note)) > domain.MaxCancelNoteLength {
		return EventAdmin{}, &domain.FieldError{Field: "note", Msg: "the note is too long"}
	}

	if err := s.awaitRefunds(ctx, id); err != nil {
		return EventAdmin{}, err
	}

	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		settings, loc, err := loadSchool(ctx, tx)
		if err != nil {
			return err
		}

		e, err := tx.Events().Lock(ctx, id)
		if err != nil {
			return err
		}

		if e.Status == domain.EventCanceled {
			return ErrEventClosed
		}

		now := s.clock.Now()

		sessions, err := s.cancelSessions(ctx, tx, settings, loc, p, now, id, note)
		if err != nil {
			return err
		}

		canceled, err := s.cancelRegistrations(ctx, tx, settings, loc, p, now, id)
		if err != nil {
			return err
		}

		e.Status, e.UpdatedAt = domain.EventCanceled, now
		if err := tx.Events().Update(ctx, e); err != nil {
			return err
		}

		if err := s.mailCanceled(ctx, tx, settings, loc, now, e, sessions, canceled, note); err != nil {
			return err
		}

		if err := s.audit(ctx, tx, p, now, domain.AuditEventCanceled, e.ID, nil, false); err != nil {
			return err
		}

		out, err = s.admin(ctx, tx, e, true)

		return err
	})
	if err != nil {
		return EventAdmin{}, fmt.Errorf("cancel event %v: %w", id, err)
	}

	return out, nil
}

// awaitRefunds waits for the receipt renderer before an event whose paid
// registrations are refunded is canceled, as before a sale.
func (s *EventService) awaitRefunds(ctx context.Context, id domain.EventID) error {
	paid := false

	err := s.store.InTx(ctx, store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		regs, err := tx.EventRegistrations().ListByEvent(ctx, id)
		paid = slices.ContainsFunc(regs, paidWithReceipt)

		return err
	})
	if err != nil {
		return fmt.Errorf("cancel event %v: %w", id, err)
	}

	if !paid {
		return nil
	}

	return s.receipts.awaitRenderer(ctx)
}

// paidWithReceipt reports whether r is open and its fee was paid with a receipt
// that a Storno refunds.
func paidWithReceipt(r domain.EventRegistration) bool {
	return r.Open() && r.Payment == domain.EventPaid && !r.ReceiptID.IsZero()
}

// cancelSessions cancels the event's sessions that have not started at
// now, like CancelLesson but without mails, and returns all its sessions
// as they are then.
func (s *EventService) cancelSessions(
	ctx context.Context, tx store.Tx, settings domain.Settings, loc *time.Location, p Principal, now time.Time,
	id domain.EventID, note string,
) ([]domain.Lesson, error) {
	sessions, err := tx.Lessons().ListByEvent(ctx, id)
	if err != nil {
		return nil, err
	}

	for i, l := range sessions {
		if l.Status != domain.LessonScheduled || !l.StartsAt.After(now) {
			continue
		}

		if l, err = tx.Lessons().Lock(ctx, l.ID); err != nil {
			return nil, err
		}

		if err := changeStatus(ctx, tx, settings, loc, now, &l, domain.LessonCanceled, s.lessons.cancellation(p, note)); err != nil {
			return nil, err
		}

		sessions[i] = l
	}

	return sessions, nil
}

// cancelRegistrations cancels the event's open registrations at now and
// refunds the paid ones, and returns them as they are then.
func (s *EventService) cancelRegistrations(
	ctx context.Context, tx store.Tx, settings domain.Settings, loc *time.Location, p Principal, now time.Time,
	id domain.EventID,
) ([]domain.EventRegistration, error) {
	regs, err := tx.EventRegistrations().ListByEvent(ctx, id)
	if err != nil {
		return nil, err
	}

	var out []domain.EventRegistration

	for _, r := range regs {
		if !r.Open() {
			continue
		}

		r, err := s.cancelRegistration(ctx, tx, settings, loc, p, now, r, true)
		if err != nil {
			return nil, err
		}

		out = append(out, r)
	}

	return out, nil
}

// mailCanceled queues the event-canceled mail about event e, with its
// sessions, for the canceled registrations: one mail per recipient, naming
// every participant it is for and whether one of their fees is refunded.
func (s *EventService) mailCanceled(
	ctx context.Context, tx store.Tx, settings domain.Settings, loc *time.Location, now time.Time, e domain.Event,
	sessions []domain.Lesson, regs []domain.EventRegistration, note string,
) error {
	persons := []domain.PersonID{}

	for _, r := range regs {
		if !r.PersonID.IsZero() {
			persons = append(persons, r.PersonID)
		}
	}

	chains, err := recipientsOf(ctx, tx, persons)
	if err != nil {
		return err
	}

	var set recipientSet

	// The addresses (lowercased) that mail about a refunded fee goes to.
	refunded := map[string]bool{}

	for _, r := range regs {
		set.addPerson(chains, r.PersonID, r.GuestName, r.GuestEmail)

		if r.Payment == domain.EventRefunded {
			for _, a := range addressesOf(chains, r) {
				refunded[strings.ToLower(a)] = true
			}
		}
	}

	data := mail.EventCanceledData{
		SchoolName: settings.SchoolName, Title: e.Title, Note: strings.TrimSpace(note),
	}
	if len(sessions) > 0 {
		data.FirstDay, data.LastDay = sessions[0].StartsAt.In(loc), sessions[len(sessions)-1].StartsAt.In(loc)
	}

	return sendMails(ctx, tx, s.ids, s.mails.Renderer, settings, now, set.list, mail.TemplateEventCanceled, mailOpts{},
		func(r recipient) any {
			d := data
			d.GivenName, d.Participants, d.Refunded = r.givenName, r.participants, refunded[strings.ToLower(r.email)]

			return d
		})
}

// addressesOf returns the addresses that mail about registration r goes
// to: its person's chain's (recipientsOf), or the guest's.
func addressesOf(chains map[domain.PersonID][]recipient, r domain.EventRegistration) []string {
	if r.PersonID.IsZero() {
		return []string{r.GuestEmail}
	}

	return mapSlice(chains[r.PersonID], func(rc recipient) string { return rc.email })
}

// audit records action of p at now on event, and on registration reg if
// not nil: its person is the target, a guest's has none.
func (s *EventService) audit(
	ctx context.Context, tx store.Tx, p Principal, now time.Time, action domain.AuditAction, event domain.EventID,
	reg *domain.EventRegistration, refund bool,
) error {
	e := domain.AuditEvent{
		ID: ids.New[domain.AuditEventID](s.ids), At: now, ActorID: actorOf(p), Action: action,
		Details: domain.AuditDetails{EventID: event, Refund: refund, Via: p.Via},
	}

	if reg != nil {
		e.Details.RegistrationID = reg.ID
		if !reg.PersonID.IsZero() {
			e.TargetKind, e.TargetPersonID = domain.AuditTargetPerson, reg.PersonID
		}
	}

	return tx.Audit().Create(ctx, e)
}
