package service

import (
	"fmt"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
)

func (f *staffFixture) faultTeach(t *testing.T, now string) (*FaultStore, *TeachService) {
	t.Helper()

	fs := NewFaultStore(f.st)

	return fs, NewTeachService(fs, clock.NewFixed(mustInstant(t, now)), f.seq, newTestRenderer(t))
}

// TestTeachLessonsStoreFaults: a store failure while reading the
// instructor's lessons, their references, counts or waiting lists is
// returned, never a partial list.
func TestTeachLessonsStoreFaults(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		who   func(f *staffFixture) domain.Person
		staff func(f *staffFixture) domain.StaffID
	}{
		"own lessons":    {func(f *staffFixture) domain.Person { return f.ivy }, func(*staffFixture) domain.StaffID { return domain.StaffID{} }},
		"office, all":    {func(f *staffFixture) domain.Person { return f.olga }, func(*staffFixture) domain.StaffID { return domain.StaffID{} }},
		"office, by one": {func(f *staffFixture) domain.Person { return f.olga }, func(f *staffFixture) domain.StaffID { return f.pianist }},
		"one of own":     {func(f *staffFixture) domain.Person { return f.paul }, func(f *staffFixture) domain.StaffID { return f.pianist }},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			SweepFaults(t, FaultSweep{Setup: func(t *testing.T) (*FaultStore, func() error) {
				f := newStaffFixture(t)
				s := f.newScene(t)
				p := f.principal(t, c.who(f))
				fs, svc := f.faultTeach(t, bookingNow)
				q := TeachQuery{Staff: c.staff(f)}

				return fs, func() error {
					got, err := svc.Lessons(t.Context(), p, q)
					if err != nil {
						return err
					}

					// Without a fault, the stray class's lesson is resolved
					// with its class too.
					for _, l := range got.Lessons {
						if l.Lesson.ID == s.stray.ID && l.Level == domain.LevelAdvanced {
							return nil
						}
					}

					return fmt.Errorf("no stray lesson in %+v", got.Lessons)
				}
			}})
		})
	}
}

// TestNotifyStoreFaults: a store failure anywhere in sending a notice is
// returned; neither a mail nor the notice that counts against the limits
// stays.
func TestNotifyStoreFaults(t *testing.T) {
	t.Parallel()

	// The instructor is named by the schedule's name, the office by its
	// person's: both reads are swept.
	for name, who := range map[string]func(f *staffFixture) domain.Person{
		"instructor": func(f *staffFixture) domain.Person { return f.ivy },
		"office":     func(f *staffFixture) domain.Person { return f.olga },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			SweepFaults(t, FaultSweep{Setup: func(t *testing.T) (*FaultStore, func() error) {
				f := newStaffFixture(t)
				l := f.lessonWithPianist(t, 5)
				f.bookDirect(t, l, domain.Booking{PersonID: f.mia.ID})
				f.bookDirect(t, l, domain.Booking{GuestName: "Gina Guest", GuestEmail: "gina@example.org"})
				p := f.principal(t, who(f))
				fs, svc := f.faultTeach(t, bookingNow)

				return fs, func() error {
					_, err := svc.Notify(t.Context(), p, l.ID, "The hall is closed, we meet in Room One.")

					return err
				}
			}})
		})
	}
}
