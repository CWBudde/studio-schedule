package service

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// The office takes an event's fee at the desk: marking a registration paid
// issues its receipt in the same transaction (ADR 0019; ADR 0012,
// amendment "event fees").

// ErrAlreadyPaid refuses to take the fee of a registration that is paid
// (a free one from the start) or refunded.
var ErrAlreadyPaid = fmt.Errorf("%w: the registration is paid already", domain.ErrConflict)

// EventPayment is how a registration's fee was paid: the method (cash if
// zero), the paying person (the registered person if zero; a guest pays
// for themself) and whom the receipt names and mails.
type EventPayment struct {
	Method    domain.PaymentMethod
	PayerID   domain.PersonID
	Recipient ReceiptRecipient
}

// MarkPaid records that the fee of an open, unpaid registration was paid,
// as the office account p (domain.ErrNotFound for an unknown registration),
// and issues its receipt in the same transaction: one line for the event
// and its dates at the registration's price, mailed to the payer, or to a
// guest's email in the school's language. A paid or refunded registration
// is ErrAlreadyPaid, a canceled one ErrRegistrationCanceled, an unknown or
// merged payer a validation error on payerId, incomplete receipt settings
// ErrReceiptSettingsIncomplete. It is audited.
func (s *EventService) MarkPaid(
	ctx context.Context, p Principal, id domain.EventRegistrationID, d EventPayment,
) (RegistrationView, error) {
	var out RegistrationView

	// The receipt is rendered in the transaction: wait for the renderer
	// first, as Sell does.
	if err := s.receipts.awaitRenderer(ctx); err != nil {
		return RegistrationView{}, err
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

		switch {
		case !r.Open():
			return ErrRegistrationCanceled
		case r.Payment != domain.EventUnpaid:
			return ErrAlreadyPaid
		}

		e, err := tx.Events().Get(ctx, r.EventID)
		if err != nil {
			return err
		}

		participant, payer, err := payerOf(ctx, tx, r, d.PayerID)
		if err != nil {
			return err
		}

		first, last, err := eventDays(ctx, tx, loc, e.ID)
		if err != nil {
			return err
		}

		receipt, err := s.receipts.issueEventFee(ctx, tx, settings, loc, p, e, r, participant, payer, first, last,
			cmp.Or(d.Method, domain.PaymentCash), d.Recipient)
		if err != nil {
			return err
		}

		// Unpaid and priced, it is paid with the receipt now.
		now := s.clock.Now()
		r.Payment, r.ReceiptID, r.UpdatedAt = domain.EventPaid, receipt.ID, now

		if err := tx.EventRegistrations().Update(ctx, r); err != nil {
			return err
		}

		if err := s.audit(ctx, tx, p, now, domain.AuditEventRegistrationPaid, e.ID, &r, false); err != nil {
			return err
		}

		out = RegistrationView{Registration: r, Name: participant}

		return nil
	})
	if err != nil {
		return RegistrationView{}, fmt.Errorf("mark registration %v paid: %w", id, err)
	}

	return out, nil
}

// payerOf returns the name of r's participant and who pays its fee: the
// person payerID names, else the registered person, else (a guest) nobody.
// A payer who does not exist, is merged or anonymized is errUnknownPayer.
func payerOf(
	ctx context.Context, tx store.Tx, r domain.EventRegistration, payerID domain.PersonID,
) (string, domain.Person, error) {
	if r.PersonID.IsZero() {
		if !payerID.IsZero() {
			return "", domain.Person{}, &domain.FieldError{Field: "payerId", Msg: "a guest pays for themself"}
		}

		return r.GuestName, domain.Person{}, nil
	}

	person, err := tx.Persons().Get(ctx, r.PersonID)
	if err != nil {
		return "", domain.Person{}, fmt.Errorf("person %v: %w", r.PersonID, refErr(err))
	}

	payer, err := livePerson(ctx, tx, cmp.Or(payerID, r.PersonID))
	if errors.Is(err, domain.ErrNotFound) {
		return "", domain.Person{}, errUnknownPayer
	}

	return fullName(person), payer, err
}

// eventDays returns the school dates of the first and the last session of
// the event the school has not canceled, else of all its sessions; zero
// without sessions.
func eventDays(
	ctx context.Context, tx store.Tx, loc *time.Location, event domain.EventID,
) (first, last domain.Date, err error) {
	sessions, err := tx.Lessons().ListByEvent(ctx, event)
	if err != nil || len(sessions) == 0 {
		return domain.Date{}, domain.Date{}, err
	}

	held := make([]domain.Lesson, 0, len(sessions))

	for _, l := range sessions {
		if l.Status == domain.LessonScheduled {
			held = append(held, l)
		}
	}

	if len(held) > 0 {
		sessions = held
	}

	return domain.DateOf(sessions[0].StartsAt, loc), domain.DateOf(sessions[len(sessions)-1].StartsAt, loc), nil
}
