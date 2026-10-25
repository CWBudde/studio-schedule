package service

import (
	"context"
	"fmt"

	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// Retention periods of personal data (ADR 0016). Guests have no person
// record; their name and email on a booking serve the lesson and its
// follow-up, not more. The audit log and the import history serve
// security and support questions for a while. Months and years are
// calendar months and years back from now (time.AddDate in UTC).
const (
	// GuestRetentionMonths: a guest's name, email and trial note are
	// anonymized this many months after the lesson ended.
	GuestRetentionMonths = 12
	// AuditRetentionYears: audit events are deleted this many years after
	// they happened.
	AuditRetentionYears = 3
	// ImportRetentionYears: import runs are deleted this many years after
	// they started.
	ImportRetentionYears = 1
)

// RetentionService deletes or anonymizes personal data after its retention
// period: the daily purge-personal-data job.
type RetentionService struct {
	store store.Store
	clock clock.Clock
}

// NewRetentionService returns a RetentionService.
func NewRetentionService(st store.Store, clk clock.Clock) *RetentionService {
	return &RetentionService{store: st, clock: clk}
}

// Purged counts what a purge changed.
type Purged struct {
	Guests, AuditEvents, ImportRuns int
}

// Purge anonymizes the guests of the bookings of lessons that ended more
// than GuestRetentionMonths ago, and deletes the audit events and import
// runs older than their retention periods, in one transaction.
func (s *RetentionService) Purge(ctx context.Context) (Purged, error) {
	var out Purged

	now := s.clock.Now().UTC()

	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		var err error
		if out.Guests, err = tx.Bookings().AnonymizeGuests(ctx, now.AddDate(0, -GuestRetentionMonths, 0)); err != nil {
			return err
		}

		if out.AuditEvents, err = tx.Audit().DeleteBefore(ctx, now.AddDate(-AuditRetentionYears, 0, 0)); err != nil {
			return err
		}

		out.ImportRuns, err = tx.Imports().DeleteStartedBefore(ctx, now.AddDate(-ImportRetentionYears, 0, 0))

		return err
	})
	if err != nil {
		return Purged{}, fmt.Errorf("purge personal data: %w", err)
	}

	return out, nil
}
