package service

import (
	"testing"

	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/mail"
)

// TestBookStoreFaults: a store failure anywhere in a booking is returned,
// and neither the booking, its event, a redemption nor the mail stays.
func TestBookStoreFaults(t *testing.T) {
	t.Parallel()

	SweepFaults(t, FaultSweep{Setup: func(t *testing.T) (*FaultStore, func() error) {
		f := newBookingFixture(t)
		anna := f.principal(t, f.anna)
		l := f.regular(t, "2026-10-22T16:00:00Z")

		r, err := mail.NewRenderer()
		if err != nil {
			t.Fatal(err)
		}

		fs := NewFaultStore(f.st)
		svc := NewBookingService(fs, clock.NewFixed(mustInstant(t, bookingNow)), f.seq, testMails(r))

		return fs, func() error {
			_, err := svc.Book(t.Context(), anna, l.ID, f.mia.ID)

			return err
		}
	}})
}
