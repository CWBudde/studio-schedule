package service

import (
	"context"
	"fmt"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// EventRegistrationReport is the office's registration list with the numbers
// of any receipts issued for the registrations.
type EventRegistrationReport struct {
	Registrations  []RegistrationView
	ReceiptNumbers map[domain.ReceiptID]string
}

// ListRegistrationsBySlug returns the office's registrations for the event
// named by slug, ordered as in ListRegistrations, with their receipt numbers.
// It accepts a draft's slug and returns domain.ErrNotFound for an unknown one.
func (s *EventService) ListRegistrationsBySlug(ctx context.Context, slug string) (EventRegistrationReport, error) {
	var out EventRegistrationReport

	err := s.store.InTx(ctx, store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		e, err := tx.Events().BySlug(ctx, slug)
		if err != nil {
			return err
		}

		regs, err := tx.EventRegistrations().ListByEvent(ctx, e.ID)
		if err != nil {
			return err
		}

		out.Registrations, err = registrationViews(ctx, tx, regs)
		if err != nil {
			return err
		}

		var ids []domain.ReceiptID

		for _, r := range regs {
			if !r.ReceiptID.IsZero() {
				ids = append(ids, r.ReceiptID)
			}
		}

		if len(ids) == 0 {
			return nil
		}

		receipts, err := tx.Receipts().ListByIDs(ctx, ids)
		if err != nil {
			return err
		}

		out.ReceiptNumbers = make(map[domain.ReceiptID]string, len(receipts))
		for _, receipt := range receipts {
			out.ReceiptNumbers[receipt.ID] = receipt.Number
		}

		return nil
	})
	if err != nil {
		return EventRegistrationReport{}, fmt.Errorf("registrations of event %q: %w", slug, err)
	}

	return out, nil
}
