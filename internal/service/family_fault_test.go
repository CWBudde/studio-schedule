package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/seed"
	"github.com/cwbudde/studio-schedule/internal/service"
	"github.com/cwbudde/studio-schedule/internal/store"
	"github.com/cwbudde/studio-schedule/internal/store/memory"
)

// familyFaultNow is the family sweeps' clock, as newFamilyFixture's.
var familyFaultNow = time.Date(2026, time.October, 21, 22, 30, 0, 0, time.UTC)

// familyFaultWorld is the demo data in a memory store, with a FamilyService
// on a FaultStore around it.
type familyFaultWorld struct {
	t   *testing.T
	mem *memory.Store
	fs  *service.FaultStore
	svc *service.FamilyService
}

func newFamilyFaultWorld(t *testing.T) *familyFaultWorld {
	t.Helper()

	mem := memory.New()
	if err := seed.LoadDemo(t.Context(), mem); err != nil {
		t.Fatal(err)
	}

	fs := service.NewFaultStore(mem)

	// ids.V7: a sequence would repeat the demo data's IDs. The IDs do not
	// change which store calls an operation makes.
	return &familyFaultWorld{
		t: t, mem: mem, fs: fs, svc: service.NewFamilyService(fs, clock.NewFixed(familyFaultNow), ids.V7{}, service.Mails{}),
	}
}

func (w *familyFaultWorld) principal(email string) service.Principal {
	w.t.Helper()

	return (&familyFixture{t: w.t, st: w.mem}).principal(email)
}

func (w *familyFaultWorld) person(given, family string) domain.Person {
	w.t.Helper()

	return (&familyFixture{t: w.t, st: w.mem}).person(given, family)
}

// orphan adds a dependent of guardian that nothing else refers to.
func (w *familyFaultWorld) orphan(guardian domain.PersonID) domain.Person {
	w.t.Helper()

	p := domain.Person{ID: ids.New[domain.PersonID](ids.V7{}), GivenName: "Nora", FamilyName: "Example"}

	err := w.mem.InTx(w.t.Context(), store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		if err := tx.Persons().Create(ctx, p); err != nil {
			return err
		}

		return tx.Persons().AddGuardian(ctx, domain.Guardian{GuardianID: guardian, DependentID: p.ID})
	})
	if err != nil {
		w.t.Fatal(err)
	}

	return p
}

// TestFamilyStoreFaults: every family operation returns a store failure and
// leaves the store as it was.
func TestFamilyStoreFaults(t *testing.T) {
	t.Parallel()

	born := domain.Date{Year: 2018, Month: time.March, Day: 9}

	// Each case prepares its fixture (writing to the memory store directly,
	// before SweepFaults takes its snapshot) and returns the operation.
	for name, prepare := range map[string]func(t *testing.T, w *familyFaultWorld) func() error{
		"Family": func(t *testing.T, w *familyFaultWorld) func() error {
			p := w.principal(clara)

			return func() error {
				f, err := w.svc.Family(t.Context(), p)
				if err == nil && len(f.Dependents) != 2 {
					t.Fatalf("Clara's dependents %+v", f.Dependents)
				}

				return err
			}
		},
		"UpdatePerson": func(t *testing.T, w *familyFaultWorld) func() error {
			p := w.principal(clara)

			return func() error {
				_, err := w.svc.UpdatePerson(t.Context(), p, service.PersonData{
					GivenName: "Clara", FamilyName: "Amberly", BirthDate: domain.Date{Year: 1988, Month: time.June, Day: 1},
				})

				return err
			}
		},
		"AddDependent": func(t *testing.T, w *familyFaultWorld) func() error {
			p := w.principal(clara)

			return func() error {
				_, err := w.svc.AddDependent(t.Context(), p, service.PersonData{
					GivenName: "Paula", FamilyName: "Amberly", BirthDate: born,
				})

				return err
			}
		},
		"UpdateDependent": func(t *testing.T, w *familyFaultWorld) func() error {
			p, ella := w.principal(clara), w.person("Ella", "Amberly")

			return func() error {
				_, err := w.svc.UpdateDependent(t.Context(), p, ella.ID,
					service.PersonData{GivenName: "Ella", FamilyName: "Amberly-Stone", BirthDate: born})

				return err
			}
		},
		// The last guardian removes a dependent nothing else refers to: it
		// is deleted.
		"RemoveDependent": func(t *testing.T, w *familyFaultWorld) func() error {
			p := w.principal(hanna)
			nora := w.orphan(p.PersonID)

			return func() error { return w.svc.RemoveDependent(t.Context(), p, nora.ID) }
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			service.SweepFaults(t, service.FaultSweep{Setup: func(t *testing.T) (*service.FaultStore, func() error) {
				w := newFamilyFaultWorld(t)

				return w.fs, prepare(t, w)
			}})
		})
	}
}
