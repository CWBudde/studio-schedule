package service

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/mail"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// Event waiting lists are ordered by registration creation time and ID.
// The event lock serializes invites, acceptance and expiry with places taken
// through the ordinary registration flow.

const eventInviteRoute = "events/invite"

// ErrEventNotFull directs a member to register when a place is available.
var ErrEventNotFull = fmt.Errorf("%w: the event still has a free place", domain.ErrConflict)

// JoinWaitlist adds a member of p's family to the waiting list of a full
// event during its sign-up window.
func (s *EventService) JoinWaitlist(
	ctx context.Context, p Principal, id domain.EventID, person domain.PersonID,
) (MyEvent, error) {
	var out MyEvent
	var refusal error

	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		e, err := tx.Events().Lock(ctx, id)
		if err != nil {
			return err
		}

		if _, err := familyMember(ctx, tx, p, person); err != nil {
			return err
		}

		who, err := lockLivePerson(ctx, tx, person)
		if err != nil {
			return err
		}

		now := s.clock.Now()
		refusal = nil
		if _, err := s.settleEventInvites(ctx, tx, e, now); err != nil {
			return err
		}

		pe, err := publicEvent(ctx, tx, e, now)
		if err != nil {
			return err
		}

		switch {
		case !pe.SignupOpen:
			refusal = ErrEventClosed
		case pe.FreePlaces > 0:
			refusal = ErrEventNotFull
		}

		if refusal != nil {
			return nil
		}

		r := s.newRegistration(p, e, now)
		r.PersonID, r.Status = person, domain.EventWaiting
		if err := tx.EventRegistrations().Create(ctx, r); err != nil {
			return err
		}

		if err := s.audit(ctx, tx, p, now, domain.AuditEventRegistrationAdded, e.ID, &r, false); err != nil {
			return err
		}

		out = MyEvent{Registration: RegistrationView{Registration: r, Name: fullName(who)}, Event: pe}

		return nil
	})

	return out, errors.Join(err, refusal)
}

// EventInvite describes the active offer shown from the mailed link.
type EventInvite struct {
	GivenName string
	Event     PublicEvent
	ExpiresAt time.Time
}

// Invite is a read-only check of the token. A used or expired link is a 404.
func (s *EventService) Invite(ctx context.Context, token string) (EventInvite, error) {
	var out EventInvite

	err := s.store.InTx(ctx, store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		r, err := tx.EventRegistrations().GetByInviteTokenHash(ctx, domain.HashToken(token))
		if err != nil {
			return err
		}

		now := s.clock.Now()
		if !r.ExpiresAt.After(now) {
			return domain.ErrNotFound
		}

		e, err := tx.Events().Get(ctx, r.EventID)
		if err != nil {
			return err
		}

		pe, err := publicEvent(ctx, tx, e, now)
		if err != nil {
			return err
		}
		if !pe.SignupOpen {
			return domain.ErrNotFound
		}

		person, err := tx.Persons().Get(ctx, r.PersonID)
		if err != nil {
			return err
		}

		out = EventInvite{GivenName: person.GivenName, Event: pe, ExpiresAt: r.ExpiresAt}

		return nil
	})

	return out, err
}

type eventInviteAction uint8

const (
	eventInviteAccept eventInviteAction = iota
	eventInviteDecline
	eventInviteLeave
)

// AcceptInvite takes the place held by an active offer in p's family.
func (s *EventService) AcceptInvite(ctx context.Context, p Principal, id domain.EventRegistrationID) (MyEvent, error) {
	return s.changeEventInvite(ctx, p, id, "", eventInviteAccept)
}

// DeclineInvite releases the place held by an active offer in p's family.
func (s *EventService) DeclineInvite(ctx context.Context, p Principal, id domain.EventRegistrationID) (MyEvent, error) {
	return s.changeEventInvite(ctx, p, id, "", eventInviteDecline)
}

// LeaveWaitlist removes a waiting or invited member of p's family from the list.
func (s *EventService) LeaveWaitlist(ctx context.Context, p Principal, id domain.EventRegistrationID) (MyEvent, error) {
	return s.changeEventInvite(ctx, p, id, "", eventInviteLeave)
}

// AcceptInviteByToken takes the place held by the mailed token. No session
// is needed, and the token is consumed by the same transaction.
func (s *EventService) AcceptInviteByToken(ctx context.Context, token string) (MyEvent, error) {
	return s.changeEventInvite(ctx, Principal{}, domain.EventRegistrationID{}, token, eventInviteAccept)
}

// DeclineInviteByToken releases a place held by the mailed token.
func (s *EventService) DeclineInviteByToken(ctx context.Context, token string) (MyEvent, error) {
	return s.changeEventInvite(ctx, Principal{}, domain.EventRegistrationID{}, token, eventInviteDecline)
}

func (s *EventService) changeEventInvite(
	ctx context.Context, p Principal, id domain.EventRegistrationID, token string, action eventInviteAction,
) (MyEvent, error) {
	var out MyEvent
	var refusal error

	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		refusal = nil
		byToken := id.IsZero()
		hash := domain.HashToken(token)
		if byToken {
			r, err := tx.EventRegistrations().GetByInviteTokenHash(ctx, hash)
			if err != nil {
				return err
			}

			return s.changeLockedEventInvite(ctx, tx, p, r.ID, &hash, action, &out, &refusal)
		}

		return s.changeLockedEventInvite(ctx, tx, p, id, nil, action, &out, &refusal)
	})

	return out, errors.Join(err, refusal)
}

func (s *EventService) changeLockedEventInvite(
	ctx context.Context, tx store.Tx, p Principal, id domain.EventRegistrationID, hash *[32]byte,
	action eventInviteAction, out *MyEvent, refusal *error,
) error {
	byToken := hash != nil
	r, err := lockRegistration(ctx, tx, id)
	if err != nil {
		return err
	}

	if byToken {
		if r.Status != domain.EventInvited || r.InviteTokenHash != *hash {
			return domain.ErrNotFound
		}
	} else if _, err := familyMember(ctx, tx, p, r.PersonID); err != nil {
		return err
	}

	e, err := tx.Events().Get(ctx, r.EventID)
	if err != nil {
		return err
	}

	now := s.clock.Now()
	if r.Status == domain.EventInvited && !r.ExpiresAt.After(now) {
		if _, err := s.settleEventInvites(ctx, tx, e, now); err != nil {
			return err
		}

		*refusal = ErrInviteExpired
		if byToken {
			*refusal = domain.ErrNotFound
		}

		return nil
	}

	switch action {
	case eventInviteLeave:
		if r.Status != domain.EventWaiting && r.Status != domain.EventInvited {
			return ErrNotWaiting
		}
	default:
		switch r.Status {
		case domain.EventWaiting:
			return ErrNotInvited
		case domain.EventRegistered:
			return ErrInviteUsed
		case domain.EventRegistrationCanceled:
			return ErrInviteExpired
		}
	}

	if action == eventInviteAccept {
		pe, err := publicEvent(ctx, tx, e, now)
		if err != nil {
			return err
		}
		if !pe.SignupOpen {
			if byToken {
				return domain.ErrNotFound
			}

			return ErrEventClosed
		}
		if err := s.acceptEventPlace(ctx, tx, p, &r, now); err != nil {
			return err
		}
	} else if err := s.endEventWaitlistPlace(ctx, tx, p, e, &r, now); err != nil {
		return err
	}

	pe, err := publicEvent(ctx, tx, e, now)
	if err != nil {
		return err
	}

	views, err := registrationViews(ctx, tx, []domain.EventRegistration{r})
	if err != nil {
		return err
	}
	*out = MyEvent{Registration: views[0], Event: pe, Cancelable: cancelable(r, pe, now)}
	if action != eventInviteAccept {
		return nil
	}

	settings, loc, err := loadSchool(ctx, tx)
	if err != nil {
		return err
	}
	if !byToken {
		return s.mailRegistration(ctx, tx, p, settings, loc, now, mail.TemplateEventRegistered, *out, false)
	}

	rs, err := personRecipients(ctx, tx, r.PersonID)
	if err != nil {
		return err
	}

	return s.mailEventRegistration(ctx, tx, settings, loc, now, rs, mail.TemplateEventRegistered, *out, false)
}

func (s *EventService) acceptEventPlace(
	ctx context.Context, tx store.Tx, p Principal, r *domain.EventRegistration, now time.Time,
) error {
	if _, err := lockLivePerson(ctx, tx, r.PersonID); err != nil {
		return err
	}

	r.Status, r.UpdatedAt = domain.EventRegistered, now
	clearEventInvite(r)
	if err := tx.EventRegistrations().Update(ctx, *r); err != nil {
		return err
	}

	if p.AccountID.IsZero() {
		return nil
	}

	return s.audit(ctx, tx, p, now, domain.AuditEventRegistrationAdded, r.EventID, r, false)
}

func (s *EventService) endEventWaitlistPlace(
	ctx context.Context, tx store.Tx, p Principal, e domain.Event, r *domain.EventRegistration, now time.Time,
) error {
	held := r.Status == domain.EventInvited
	if err := cancelEventRegistration(ctx, tx, r, now); err != nil {
		return err
	}
	if !p.AccountID.IsZero() {
		if err := s.audit(ctx, tx, p, now, domain.AuditEventRegistrationCanceled, r.EventID, r, false); err != nil {
			return err
		}
	}
	if !held {
		return nil
	}

	return s.offerEventPlaces(ctx, tx, e, now)
}

func clearEventInvite(r *domain.EventRegistration) {
	r.InviteTokenHash, r.InvitedAt, r.ExpiresAt = [32]byte{}, time.Time{}, time.Time{}
}

func cancelEventRegistration(ctx context.Context, tx store.Tx, r *domain.EventRegistration, now time.Time) error {
	r.Status, r.CanceledAt, r.UpdatedAt = domain.EventRegistrationCanceled, now, now
	clearEventInvite(r)

	return tx.EventRegistrations().Update(ctx, *r)
}

// offerEventPlaces reserves every free place it can for the oldest
// reachable waiting members. The caller holds the event lock.
func (s *EventService) offerEventPlaces(ctx context.Context, tx store.Tx, e domain.Event, now time.Time) error {
	if e.Status != domain.EventPublished {
		return nil
	}

	counts, err := tx.EventRegistrations().CountsByEvents(ctx, []domain.EventID{e.ID}, now)
	if err != nil {
		return err
	}

	c := counts[e.ID]
	free := e.Capacity - c.Registered - c.Invited
	if free <= 0 || c.Waiting == 0 {
		return nil
	}

	sessions, err := tx.Lessons().ListByEvent(ctx, e.ID)
	if err != nil {
		return err
	}

	var first time.Time
	if i := slices.IndexFunc(sessions, func(l domain.Lesson) bool { return l.Status == domain.LessonScheduled }); i >= 0 {
		first = sessions[i].StartsAt
	}
	closes, _, open := eventSignupWindow(e, first, now)
	if !open {
		return nil
	}

	regs, err := tx.EventRegistrations().ListByEvent(ctx, e.ID)
	if err != nil {
		return err
	}

	settings, loc, err := loadSchool(ctx, tx)
	if err != nil {
		return err
	}

	var persons []domain.PersonID
	for _, r := range regs {
		if r.Status == domain.EventWaiting && !r.PersonID.IsZero() {
			persons = append(persons, r.PersonID)
		}
	}

	rs, err := recipientsOf(ctx, tx, persons)
	if err != nil {
		return err
	}

	expires := minTime(now.Add(settings.WaitlistInviteExpiry), closes)
	for _, r := range regs {
		if free == 0 {
			break
		}
		if r.Status != domain.EventWaiting || len(rs[r.PersonID]) == 0 {
			continue
		}

		if err := s.offerEventPlace(ctx, tx, settings, loc, e, r, rs[r.PersonID], now, expires); err != nil {
			return err
		}
		free--
	}

	return nil
}

func (s *EventService) offerEventPlace(
	ctx context.Context, tx store.Tx, settings domain.Settings, loc *time.Location, e domain.Event,
	r domain.EventRegistration, rs []recipient, now, expires time.Time,
) error {
	token := newToken()
	link := s.mails.AppURL + eventInviteRoute + "?token=" + url.QueryEscape(token)
	r.Status, r.UpdatedAt = domain.EventInvited, now
	r.InvitedAt, r.ExpiresAt, r.InviteTokenHash = now, expires, domain.HashToken(token)
	if err := tx.EventRegistrations().Update(ctx, r); err != nil {
		return err
	}

	data := eventInviteMail(settings, e, expires.In(loc), link)
	if err := sendMails(ctx, tx, s.ids, s.mails.Renderer, settings, now, rs, mail.TemplateEventInvite, mailOpts{}, data); err != nil {
		return err
	}

	note := pushNote{template: mail.TemplateEventInvite, url: link, tag: "event-invite-" + e.ID.String(), expires: expires}

	return pushTo(ctx, tx, s.ids, s.mails, settings, now, rs, note, data)
}

func eventInviteMail(settings domain.Settings, e domain.Event, expires time.Time, link string) func(recipient) any {
	return func(rc recipient) any {
		return mail.EventInviteData{
			SchoolName: settings.SchoolName, GivenName: rc.givenName, Title: e.Title,
			Participant: rc.participants[0], ExpiresAt: expires, Link: link,
		}
	}
}

// ExpireDueInvites ends due offers under their event locks. An error in one
// transaction is reported alongside the number settled in the others.
func (s *EventService) ExpireDueInvites(ctx context.Context) (int, error) {
	now := s.clock.Now()
	var ids []domain.EventID

	err := s.store.InTx(ctx, store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		var err error
		ids, err = tx.EventRegistrations().ListDueEvents(ctx, now)

		return err
	})
	if err != nil {
		return 0, err
	}

	var total int
	var failures []error
	for _, id := range ids {
		var ended int
		err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
			e, err := tx.Events().Lock(ctx, id)
			if err != nil {
				return err
			}
			ended, err = s.settleEventInvites(ctx, tx, e, s.clock.Now())

			return err
		})
		if err != nil {
			failures = append(failures, fmt.Errorf("expire event %v invites: %w", id, err))
			continue
		}
		total += ended
	}

	return total, errors.Join(failures...)
}

func (s *EventService) settleEventInvites(
	ctx context.Context, tx store.Tx, e domain.Event, now time.Time,
) (int, error) {
	regs, err := tx.EventRegistrations().ListByEvent(ctx, e.ID)
	if err != nil {
		return 0, err
	}

	due := slices.DeleteFunc(regs, func(r domain.EventRegistration) bool {
		return r.Status != domain.EventInvited || r.ExpiresAt.After(now)
	})
	if len(due) == 0 {
		return 0, nil
	}

	settings, loc, err := loadSchool(ctx, tx)
	if err != nil {
		return 0, err
	}

	for _, r := range due {
		if err := cancelEventRegistration(ctx, tx, &r, now); err != nil {
			return 0, err
		}
		if r.PersonID.IsZero() {
			continue
		}

		rs, err := personRecipients(ctx, tx, r.PersonID)
		if err != nil {
			return 0, err
		}

		data := eventInviteMail(settings, e, now.In(loc), "")
		if err := sendMails(ctx, tx, s.ids, s.mails.Renderer, settings, now, rs, mail.TemplateEventInviteExpired, mailOpts{}, data); err != nil {
			return 0, err
		}
	}

	return len(due), s.offerEventPlaces(ctx, tx, e, now)
}
