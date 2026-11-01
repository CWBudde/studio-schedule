package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// The office registers persons and guests for events at the desk and
// cancels registrations, with or without a refund (ADR 0019). Every change
// runs under the event's lock and is audited.

var (
	// ErrEventFull refuses a registration when the registrations with a
	// place and the open invites fill the event's capacity, unless the
	// office confirms going over it.
	ErrEventFull = fmt.Errorf("%w: the event is full", domain.ErrConflict)
	// ErrRegistrationCanceled refuses to cancel, or to take the fee of, a
	// registration that is canceled already.
	ErrRegistrationCanceled = fmt.Errorf("%w: the registration is canceled already", domain.ErrConflict)
)

// NewRegistration is whom the office registers for an event: a person, or
// a guest with a name and an email.
type NewRegistration struct {
	PersonID   domain.PersonID
	GuestName  string
	GuestEmail string
	// ConfirmOverCapacity registers even when the event is full (ADR 0019,
	// as for lessons).
	ConfirmOverCapacity bool
}

// RegistrationView is a registration with the name it is shown with.
type RegistrationView struct {
	Registration domain.EventRegistration
	// Name is the person's given and family name, or the guest's name.
	Name string
	// Anonymized tells that the person is anonymized (ADR 0016), so Name
	// is the fixed one.
	Anonymized bool
}

// AddRegistration registers a person or a guest for a published event as
// the office account p (domain.ErrNotFound for an unknown event; ADR
// 0019). An unknown, merged or anonymized person is a validation error on
// personId; a guest needs a name (guest.name) and an email (guest.email).
// A draft or canceled event is ErrEventClosed, a full one ErrEventFull
// unless confirmed, and a person with an open registration
// domain.ErrDuplicate. The registration snapshots the event's price and VAT
// rate; a free one is paid, a priced one unpaid until the office takes the
// fee (MarkPaid). It is audited.
func (s *EventService) AddRegistration(
	ctx context.Context, p Principal, event domain.EventID, in NewRegistration,
) (RegistrationView, error) {
	var out RegistrationView

	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		e, err := tx.Events().Lock(ctx, event)
		if err != nil {
			return err
		}

		// Lock order: the event, then the person (its registration).
		name := strings.TrimSpace(in.GuestName)

		var person domain.Person
		if !in.PersonID.IsZero() {
			if person, err = lockLivePerson(ctx, tx, in.PersonID); errors.Is(err, domain.ErrNotFound) {
				return errUnknownPerson
			} else if err != nil {
				return err
			}

			name = fullName(person)
		}

		now := s.clock.Now()
		r := domain.EventRegistration{
			ID: ids.New[domain.EventRegistrationID](s.ids), EventID: e.ID, PersonID: in.PersonID,
			GuestName: strings.TrimSpace(in.GuestName), GuestEmail: strings.TrimSpace(in.GuestEmail),
			Status: domain.EventRegistered, Payment: domain.EventUnpaid, Price: e.Price, VATRate: e.VATRate,
			RegisteredBy: actorOf(p), CreatedAt: now, UpdatedAt: now,
		}

		if r.Price == 0 {
			r.Payment = domain.EventPaid
		}

		if r.PersonID.IsZero() && r.GuestName == "" {
			return errNoGuestName
		}

		if err := r.Validate(); err != nil {
			return err
		}

		if e.Status != domain.EventPublished {
			return ErrEventClosed
		}

		counts, err := tx.EventRegistrations().CountsByEvents(ctx, []domain.EventID{e.ID}, now)
		if err != nil {
			return err
		}

		if c := counts[e.ID]; c.Registered+c.Invited >= e.Capacity && !in.ConfirmOverCapacity {
			return ErrEventFull
		}

		if err := tx.EventRegistrations().Create(ctx, r); err != nil {
			return err
		}

		if err := s.audit(ctx, tx, p, now, domain.AuditEventRegistrationAdded, e.ID, &r, false); err != nil {
			return err
		}

		out = RegistrationView{Registration: r, Name: name}

		return nil
	})
	if err != nil {
		return RegistrationView{}, fmt.Errorf("register for event %v: %w", event, err)
	}

	return out, nil
}

// ListRegistrations returns the registrations of an event, canceled ones
// included, in the order of its waiting list (domain.ErrNotFound for an
// unknown event), with the persons' names.
func (s *EventService) ListRegistrations(ctx context.Context, event domain.EventID) ([]RegistrationView, error) {
	var out []RegistrationView

	err := s.store.InTx(ctx, store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		if _, err := tx.Events().Get(ctx, event); err != nil {
			return err
		}

		regs, err := tx.EventRegistrations().ListByEvent(ctx, event)
		if err != nil {
			return err
		}

		out, err = registrationViews(ctx, tx, regs)

		return err
	})
	if err != nil {
		return nil, fmt.Errorf("registrations of event %v: %w", event, err)
	}

	return out, nil
}

// registrationViews names the registrations' persons with one query.
func registrationViews(
	ctx context.Context, tx store.Tx, regs []domain.EventRegistration,
) ([]RegistrationView, error) {
	var persons []domain.PersonID

	for _, r := range regs {
		if !r.PersonID.IsZero() {
			persons = append(persons, r.PersonID)
		}
	}

	found, err := tx.Persons().ListByIDs(ctx, persons)
	if err != nil {
		return nil, fmt.Errorf("persons: %w", err)
	}

	byPerson := byID(found)
	out := make([]RegistrationView, len(regs))

	for i, r := range regs {
		out[i] = RegistrationView{Registration: r, Name: r.GuestName}

		// A registration keeps its person (the store refuses to delete it).
		if person, ok := byPerson[r.PersonID]; ok {
			out[i].Name, out[i].Anonymized = fullName(person), person.Anonymized()
		}
	}

	return out, nil
}

// CancelRegistration cancels an open registration as the office account p
// (domain.ErrNotFound for an unknown one, ErrRegistrationCanceled for one
// canceled already), even after the members' deadline (ADR 0019). With
// refund, a fee paid with a receipt is refunded by a Storno of the receipt,
// even once the event has started; without, the payment stays. It is
// audited, with whether it refunded.
func (s *EventService) CancelRegistration(
	ctx context.Context, p Principal, id domain.EventRegistrationID, refund bool,
) (RegistrationView, error) {
	var out RegistrationView

	if refund {
		if err := s.receipts.awaitRenderer(ctx); err != nil {
			return RegistrationView{}, err
		}
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

		if !r.Open() {
			return ErrRegistrationCanceled
		}

		now := s.clock.Now()

		var refunded bool
		if r, refunded, err = s.cancelRegistration(ctx, tx, settings, loc, p, now, r, refund); err != nil {
			return err
		}

		if err := s.audit(ctx, tx, p, now, domain.AuditEventRegistrationCanceled, r.EventID, &r, refunded); err != nil {
			return err
		}

		views, err := registrationViews(ctx, tx, []domain.EventRegistration{r})
		if err != nil {
			return err
		}

		out = views[0]

		return nil
	})
	if err != nil {
		return RegistrationView{}, fmt.Errorf("cancel registration %v: %w", id, err)
	}

	return out, nil
}

// lockRegistration returns registration id read under its event's lock.
func lockRegistration(ctx context.Context, tx store.Tx, id domain.EventRegistrationID) (domain.EventRegistration, error) {
	r, err := tx.EventRegistrations().Get(ctx, id)
	if err != nil {
		return domain.EventRegistration{}, err
	}

	if _, err := tx.Events().Lock(ctx, r.EventID); err != nil {
		return domain.EventRegistration{}, err
	}

	return tx.EventRegistrations().Get(ctx, id)
}

// cancelRegistration cancels the open registration r at now as p and, with
// refund, refunds a fee paid with a receipt by a Storno of the receipt
// (ReceiptService.storno, the event's start notwithstanding). It returns
// the registration as stored and whether this call refunded it. The caller
// holds the event's lock.
func (s *EventService) cancelRegistration(
	ctx context.Context, tx store.Tx, settings domain.Settings, loc *time.Location, p Principal, now time.Time,
	r domain.EventRegistration, refund bool,
) (domain.EventRegistration, bool, error) {
	r.Status, r.CanceledAt, r.UpdatedAt = domain.EventRegistrationCanceled, now, now
	r.InviteTokenHash, r.InvitedAt, r.ExpiresAt = [32]byte{}, time.Time{}, time.Time{}

	if err := tx.EventRegistrations().Update(ctx, r); err != nil {
		return domain.EventRegistration{}, false, err
	}

	if !refund || r.Payment != domain.EventPaid || r.ReceiptID.IsZero() {
		return r, false, nil
	}

	orig, err := tx.Receipts().Get(ctx, r.ReceiptID)
	if err != nil {
		return domain.EventRegistration{}, false, fmt.Errorf("receipt %v: %w", r.ReceiptID, refErr(err))
	}

	if _, err := s.receipts.storno(ctx, tx, settings, loc, p, orig, "", true); err != nil {
		return domain.EventRegistration{}, false, err
	}

	// The Storno refunded it.
	r, err = tx.EventRegistrations().Get(ctx, r.ID)

	return r, true, err
}
