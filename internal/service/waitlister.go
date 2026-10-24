package service

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"slices"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/mail"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// The machinery of the waiting lists (ADR 0010), which every service that
// frees places or books persons runs in its own transactions.

// waitlister runs the waiting lists inside the callers' transactions. Every
// method needs the lesson lock of the lessons it touches.
type waitlister struct {
	ids   ids.Source
	mails Mails
}

// holdsPlace reports whether entry e reserves a place at now: it is invited
// and its invite has not expired. An invite whose expiry has passed holds
// nothing, even before the waitlist-expiry job ends it.
func holdsPlace(e domain.WaitlistEntry, now time.Time) bool {
	return e.Status == domain.WaitlistInvited && now.Before(e.ExpiresAt)
}

// heldPlaces counts, by lesson, the entries that hold a place at now.
func heldPlaces(entries []domain.WaitlistEntry, now time.Time) map[domain.LessonID]int {
	out := map[domain.LessonID]int{}

	for _, e := range entries {
		if holdsPlace(e, now) {
			out[e.LessonID]++
		}
	}

	return out
}

// places returns the active bookings of lesson id and the places its
// invites hold at now.
func places(ctx context.Context, tx store.Tx, now time.Time, id domain.LessonID) (active, held int, err error) {
	if active, err = tx.Bookings().CountActive(ctx, id); err != nil {
		return 0, 0, err
	}

	open, err := tx.Waitlist().ListOpenByLessons(ctx, []domain.LessonID{id})
	if err != nil {
		return 0, 0, err
	}

	return active, heldPlaces(open, now)[id], nil
}

// offerable reports whether lesson l takes invites at now: it takes place,
// members may see it, and booking has not closed.
func offerable(settings domain.Settings, now time.Time, l domain.Lesson) bool {
	return l.Status == domain.LessonScheduled && l.Visibility != domain.VisibilityHidden &&
		now.Before(bookingClosesAt(settings, l)) && now.Before(l.StartsAt)
}

// clearInvite takes e back to waiting without its invite window. The hash
// of the old link stays until a new invite replaces it, so the link can
// still tell that the person waits (ADR 0010).
func clearInvite(e *domain.WaitlistEntry) {
	e.Status, e.InvitedAt, e.ExpiresAt = domain.WaitlistWaiting, time.Time{}, time.Time{}
}

// endInvite ends the invite of e, an entry of lesson l, at now, because it
// ran out or was declined: the entry goes back to the end of the list if it
// stays on it and l still takes invites, and expires otherwise. It reports
// whether the entry stays.
func endInvite(e *domain.WaitlistEntry, settings domain.Settings, now time.Time, l domain.Lesson) bool {
	if !e.StayOnList || !offerable(settings, now, l) {
		e.Status = domain.WaitlistExpired

		return false
	}

	e.JoinedAt = now
	clearInvite(e)

	return true
}

// waiting reports whether open has a waiting entry outside skip.
func waiting(open []domain.WaitlistEntry, skip map[domain.WaitlistEntryID]bool) bool {
	return slices.ContainsFunc(open, func(e domain.WaitlistEntry) bool {
		return e.Status == domain.WaitlistWaiting && !skip[e.ID]
	})
}

// offer offers the free places of lesson l, which the caller has locked,
// to its waiting list (offerTo). It costs one query for a lesson without
// waiting entries.
func (w waitlister) offer(
	ctx context.Context, tx store.Tx, now time.Time, l domain.Lesson, skip map[domain.WaitlistEntryID]bool,
) error {
	open, err := tx.Waitlist().ListOpenByLessons(ctx, []domain.LessonID{l.ID})
	if err != nil || !waiting(open, skip) {
		return err
	}

	settings, loc, err := loadSchool(ctx, tx)
	if err != nil {
		return err
	}

	return w.offerTo(ctx, tx, settings, loc, now, l, open, skip)
}

// offerTo invites the oldest waiting entries among open, the open entries
// of lesson l, which the caller has locked, one per free place: the
// capacity minus the active bookings and the places held by invites.
// Entries in skip (an invite that just ended) are passed over, and so is a
// person whom no mail would reach (recipientsOf yields nobody): they stay
// waiting. Nothing is offered unless the lesson takes invites (offerable).
func (w waitlister) offerTo(
	ctx context.Context, tx store.Tx, settings domain.Settings, loc *time.Location, now time.Time, l domain.Lesson,
	open []domain.WaitlistEntry, skip map[domain.WaitlistEntryID]bool,
) error {
	if !waiting(open, skip) || !offerable(settings, now, l) {
		return nil
	}

	active, err := tx.Bookings().CountActive(ctx, l.ID)
	if err != nil {
		return err
	}

	free := l.Capacity - active - heldPlaces(open, now)[l.ID]
	if free <= 0 {
		return nil
	}

	var candidates []domain.PersonID

	for _, e := range open {
		if e.Status == domain.WaitlistWaiting && !skip[e.ID] {
			candidates = append(candidates, e.PersonID)
		}
	}

	recipients, err := recipientsOf(ctx, tx, candidates)
	if err != nil {
		return err
	}

	var at *lessonPlace // read once, for the first invite

	for _, e := range open {
		if free <= 0 {
			break
		}

		if e.Status != domain.WaitlistWaiting || skip[e.ID] || len(recipients[e.PersonID]) == 0 {
			continue // not waiting, or nobody would learn of the invite
		}

		if at == nil {
			p, err := loadLessonPlace(ctx, tx, l)
			if err != nil {
				return err
			}

			at = &p
		}

		if err := w.invite(ctx, tx, settings, loc, now, l, *at, e, recipients[e.PersonID]); err != nil {
			return err
		}

		free--
	}

	return nil
}

// invite invites the waiting entry e of lesson l at now: a new random token
// whose hash it stores, valid for the school's invite expiry but never
// beyond the booking cutoff, mailed to the recipients with the link that
// accepts it.
func (w waitlister) invite(
	ctx context.Context, tx store.Tx, settings domain.Settings, loc *time.Location, now time.Time, l domain.Lesson,
	at lessonPlace, e domain.WaitlistEntry, recipients []recipient,
) error {
	token, err := newToken()
	if err != nil {
		return err
	}

	e.Status, e.InviteTokenHash, e.InvitedAt = domain.WaitlistInvited, domain.HashToken(token), now
	e.ExpiresAt = minTime(now.Add(settings.WaitlistInviteExpiry), bookingClosesAt(settings, l))

	if err := w.update(ctx, tx, e); err != nil {
		return err
	}

	link := w.mails.AppURL + acceptRoute + "?token=" + url.QueryEscape(token)

	return w.send(ctx, tx, settings, now, recipients, mail.TemplateWaitlistInvite, func(r recipient) any {
		return mail.WaitlistInviteData{
			WaitlistData: at.waitlistData(settings, loc, l, r), Link: link, ExpiresAt: e.ExpiresAt.In(loc),
		}
	})
}

// settle brings the list of lesson l, which the caller has locked, up to
// date at now, given its open entries, and returns how many entries it
// ended. Once the lesson has started, every open entry ends (expired,
// without a mail). Before, an invite whose expiry has passed ends
// (endInvite) with a waitlist-expired mail; then the places this frees are
// offered to the others. Without anything due it costs no query.
func (w waitlister) settle(
	ctx context.Context, tx store.Tx, now time.Time, l domain.Lesson, open []domain.WaitlistEntry,
) (int, error) {
	started := !now.Before(l.StartsAt)
	due := func(e domain.WaitlistEntry) bool {
		return started || (e.Status == domain.WaitlistInvited && !now.Before(e.ExpiresAt))
	}

	if !slices.ContainsFunc(open, due) {
		return 0, nil
	}

	settings, loc, err := loadSchool(ctx, tx)
	if err != nil {
		return 0, err
	}

	ended := map[domain.WaitlistEntryID]bool{}
	stays := map[domain.PersonID]bool{}

	var (
		mailed []domain.PersonID
		after  []domain.WaitlistEntry // the open entries once settled
	)

	for _, e := range open {
		if !due(e) {
			after = append(after, e)

			continue
		}

		if started {
			e.Status = domain.WaitlistExpired
		} else {
			stays[e.PersonID] = endInvite(&e, settings, now, l)
			mailed = append(mailed, e.PersonID)
		}

		if err := w.update(ctx, tx, e); err != nil {
			return 0, err
		}

		ended[e.ID] = true

		if e.Open() {
			after = append(after, e)
		}
	}

	if err := w.mailExpired(ctx, tx, settings, loc, now, l, mailed, stays); err != nil {
		return 0, err
	}

	if started {
		return len(ended), nil
	}

	return len(ended), w.offerTo(ctx, tx, settings, loc, now, l, after, ended)
}

// settled settles the list of lesson l, which the caller has locked, at
// now (settle) and returns its open entries then. Booking and joining call
// it before they count the free places, so a place whose invite has run
// out goes to the next waiting entry rather than to whoever asks first.
func (w waitlister) settled(
	ctx context.Context, tx store.Tx, now time.Time, l domain.Lesson,
) ([]domain.WaitlistEntry, error) {
	open, err := tx.Waitlist().ListOpenByLessons(ctx, []domain.LessonID{l.ID})
	if err != nil {
		return nil, err
	}

	if n, err := w.settle(ctx, tx, now, l, open); err != nil || n == 0 {
		return open, err
	}

	return tx.Waitlist().ListOpenByLessons(ctx, []domain.LessonID{l.ID})
}

// mailExpired tells the recipients of the persons that the place reserved
// for them in lesson l was not taken, and whether each stays on the list.
func (w waitlister) mailExpired(
	ctx context.Context, tx store.Tx, settings domain.Settings, loc *time.Location, now time.Time, l domain.Lesson,
	persons []domain.PersonID, stays map[domain.PersonID]bool,
) error {
	if len(persons) == 0 {
		return nil
	}

	recipients, err := recipientsOf(ctx, tx, persons)
	if err != nil {
		return err
	}

	at, err := loadLessonPlace(ctx, tx, l)
	if err != nil {
		return err
	}

	for _, person := range persons {
		err := w.send(ctx, tx, settings, now, recipients[person], mail.TemplateWaitlistExpired, func(r recipient) any {
			return mail.WaitlistExpiredData{WaitlistData: at.waitlistData(settings, loc, l, r), StillWaiting: stays[person]}
		})
		if err != nil {
			return err
		}
	}

	return nil
}

// update validates and stores e.
func (w waitlister) update(ctx context.Context, tx store.Tx, e domain.WaitlistEntry) error {
	if err := e.Validate(); err != nil {
		return err
	}

	return tx.Waitlist().Update(ctx, e)
}

// end ends the open entry e of lesson l, which the caller has locked, with
// status (withdrawn or accepted) at now and, if its invite held a place
// that is free again (withdrawn), offers it to the next entries.
func (w waitlister) end(
	ctx context.Context, tx store.Tx, now time.Time, l domain.Lesson, e domain.WaitlistEntry,
	status domain.WaitlistStatus,
) error {
	held := holdsPlace(e, now)
	e.Status = status

	if err := w.update(ctx, tx, e); err != nil {
		return err
	}

	if !held || status == domain.WaitlistAccepted {
		return nil
	}

	return w.offer(ctx, tx, now, l, map[domain.WaitlistEntryID]bool{e.ID: true})
}

// booked ends the open entry of person among open, the open entries of
// lesson l, if there is one, because the person got booked otherwise (ADR
// 0010): an invite that holds a place is accepted, as the booking took that
// place, and anything else withdrawn. It reports whether an invite was
// accepted. The caller has locked the lesson and booked the person.
func (w waitlister) booked(
	ctx context.Context, tx store.Tx, now time.Time, l domain.Lesson, open []domain.WaitlistEntry,
	person domain.PersonID,
) (bool, error) {
	i := slices.IndexFunc(open, func(e domain.WaitlistEntry) bool { return e.PersonID == person && e.Open() })
	if i < 0 {
		return false, nil
	}

	if holdsPlace(open[i], now) {
		return true, w.end(ctx, tx, now, l, open[i], domain.WaitlistAccepted)
	}

	return false, w.end(ctx, tx, now, l, open[i], domain.WaitlistWithdrawn)
}

// void ends the invites of lesson l, which the school canceled: each goes
// back to waiting at its place in the list, without a mail, so a restore
// invites it first (ADR 0010). Waiting entries stay.
func (w waitlister) void(ctx context.Context, tx store.Tx, l domain.LessonID) error {
	open, err := tx.Waitlist().ListOpenByLessons(ctx, []domain.LessonID{l})
	if err != nil {
		return err
	}

	for _, e := range open {
		if e.Status != domain.WaitlistInvited {
			continue
		}

		clearInvite(&e)

		if err := w.update(ctx, tx, e); err != nil {
			return err
		}
	}

	return nil
}

// withdrawPerson withdraws every open entry of person, locking each lesson
// first (in the order of their IDs), and offers the places their invites
// held. Removing a person from the school does this before deleting it.
func (w waitlister) withdrawPerson(ctx context.Context, tx store.Tx, now time.Time, person domain.PersonID) error {
	entries, err := tx.Waitlist().ListByPersons(ctx, []domain.PersonID{person})
	if err != nil {
		return err
	}

	entries = slices.DeleteFunc(entries, func(e domain.WaitlistEntry) bool { return !e.Open() })
	slices.SortFunc(entries, func(a, b domain.WaitlistEntry) int { return bytes.Compare(a.LessonID[:], b.LessonID[:]) })

	for _, e := range entries {
		l, err := tx.Lessons().Lock(ctx, e.LessonID)
		if err != nil {
			return err
		}

		// Read again under the lock.
		if e, err = tx.Waitlist().Get(ctx, e.ID); err != nil {
			return err
		}

		if !e.Open() {
			continue
		}

		if err := w.end(ctx, tx, now, l, e, domain.WaitlistWithdrawn); err != nil {
			return err
		}
	}

	return nil
}

// send queues template to the recipients (sendMails).
func (w waitlister) send(
	ctx context.Context, tx store.Tx, settings domain.Settings, now time.Time, recipients []recipient, template string,
	data func(recipient) any,
) error {
	return sendMails(ctx, tx, w.ids, w.mails.Renderer, settings, now, recipients, template, data)
}

// lessonPlace is what the waiting-list mails name of a lesson besides its
// times.
type lessonPlace struct {
	courseType domain.CourseType
	room       domain.Room
	location   domain.Location
}

func loadLessonPlace(ctx context.Context, tx store.Tx, l domain.Lesson) (lessonPlace, error) {
	ct, err := tx.CourseTypes().Get(ctx, l.CourseTypeID)
	if err != nil {
		return lessonPlace{}, fmt.Errorf("course type %v: %w", l.CourseTypeID, refErr(err))
	}

	room, location, err := place(ctx, tx, l.RoomID)
	if err != nil {
		return lessonPlace{}, err
	}

	return lessonPlace{courseType: ct, room: room, location: location}, nil
}

// waitlistData is the mail data of lesson l for recipient r.
func (p lessonPlace) waitlistData(
	settings domain.Settings, loc *time.Location, l domain.Lesson, r recipient,
) mail.WaitlistData {
	return mail.WaitlistData{
		SchoolName: settings.SchoolName, GivenName: r.givenName, Participant: r.participants[0],
		CourseType: p.courseType.Name, StartsAt: l.StartsAt.In(loc), EndsAt: l.EndsAt.In(loc),
		Room: p.room.Name, Location: p.location.Name,
	}
}
