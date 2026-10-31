package service

import (
	"testing"

	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
)

// TestLessonMailStoreFaults: canceling or moving a lesson mails everyone
// booked (Mia through her guardian Anna, Sam through Stella, a guest
// directly) in the same transaction. A store failure while the recipients,
// the course type or the rooms are read fails the change: neither the
// lesson's new state nor any mail stays.
func TestLessonMailStoreFaults(t *testing.T) {
	t.Parallel()

	for name, op := range map[string]func(t *testing.T, f *bookingFixture, svc *LessonService, l domain.Lesson) error{
		"cancel": func(t *testing.T, f *bookingFixture, svc *LessonService, l domain.Lesson) error {
			_, err := svc.CancelLesson(t.Context(), f.principal(t, f.olga), l.ID, "The studio is closed.")

			return err
		},
		"move": func(t *testing.T, _ *bookingFixture, svc *LessonService, l domain.Lesson) error {
			d := dataOf(t, l)
			d.StartTime.Hour++
			_, err := svc.UpdateLesson(t.Context(), l.ID, d)

			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			SweepFaults(t, FaultSweep{Setup: func(t *testing.T) (*FaultStore, func() error) {
				f := newBookingFixture(t)
				l := f.regular(t, staffLesson)
				f.bookFor(t, l, f.mia, f.sam, domain.Person{})

				fs := NewFaultStore(f.st)
				svc := NewLessonService(fs, clock.NewFixed(mustInstant(t, bookingNow)), f.seq, testMails(newTestRenderer(t)))

				return fs, func() error {
					if err := op(t, f, svc, l); err != nil {
						return err
					}

					if n := len(f.mails(t)); n != 3 {
						t.Fatalf("%d mails, want one each to Anna, Stella and the guest", n)
					}

					return nil
				}
			}})
		})
	}
}
