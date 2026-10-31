package service

import (
	"fmt"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/clock"
)

func (f *staffFixture) faultMyLessons(t *testing.T, now string) (*FaultStore, *MyLessonsService) {
	t.Helper()

	fs := NewFaultStore(f.st)

	return fs, NewMyLessonsService(fs, clock.NewFixed(mustInstant(t, now)))
}

// TestMyLessonsUpcomingStoreFaults: a store failure while reading the
// family, its bookings, waiting-list entries, their lessons, references or
// payments is returned, never a page with bookings missing.
func TestMyLessonsUpcomingStoreFaults(t *testing.T) {
	t.Parallel()

	SweepFaults(t, FaultSweep{Setup: func(t *testing.T) (*FaultStore, func() error) {
		f := newStaffFixture(t)
		f.newScene(t)
		anna := f.principal(t, f.anna)
		fs, svc := f.faultMyLessons(t, bookingNow)

		return fs, func() error {
			got, err := svc.Upcoming(t.Context(), anna)
			if err != nil {
				return err
			}

			// Without a fault: Tom's one-off, Mia's four class dates and her
			// place on the list.
			if len(got.Upcoming) != 5 || len(got.Waitlist) != 1 {
				return fmt.Errorf("unexpected page: %d bookings, %d entries", len(got.Upcoming), len(got.Waitlist))
			}

			return nil
		}
	}})
}

// TestMyLessonsHistoryStoreFaults: a store failure while reading a page of
// the history, or whether there are earlier pages, is returned.
func TestMyLessonsHistoryStoreFaults(t *testing.T) {
	t.Parallel()

	SweepFaults(t, FaultSweep{Setup: func(t *testing.T) (*FaultStore, func() error) {
		f := newStaffFixture(t)
		f.newScene(t)
		f.bookAs(t, f.mia, f.regular(t, "2026-06-10T15:00:00Z")) // before the page
		anna := f.principal(t, f.anna)
		fs, svc := f.faultMyLessons(t, "2026-12-01T10:00:00Z")

		return fs, func() error {
			got, err := svc.History(t.Context(), anna, time.Time{})
			if err != nil {
				return err
			}

			if len(got.Items) != 5 || !got.HasMore {
				return fmt.Errorf("unexpected page: %d bookings, more %v", len(got.Items), got.HasMore)
			}

			return nil
		}
	}})
}
