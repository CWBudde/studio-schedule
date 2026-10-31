package service_test

import (
	"context"
	"slices"
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

// personsFaultWorld is the demo data in a memory store, with the office's
// PersonService on a FaultStore around it.
type personsFaultWorld struct {
	t   *testing.T
	mem *memory.Store
	fs  *service.FaultStore
	svc *service.PersonService
}

func newPersonsFaultWorld(t *testing.T) *personsFaultWorld {
	t.Helper()

	mem := memory.New()
	if err := seed.LoadDemo(t.Context(), mem); err != nil {
		t.Fatal(err)
	}

	fs := service.NewFaultStore(mem)
	now := time.Date(2026, time.October, 21, 22, 30, 0, 0, time.UTC)

	// ids.V7: a sequence would repeat the demo data's IDs. The IDs do not
	// change which store calls an operation makes.
	return &personsFaultWorld{
		t: t, mem: mem, fs: fs, svc: service.NewPersonService(fs, clock.NewFixed(now), ids.V7{}, service.Mails{}),
	}
}

func (w *personsFaultWorld) person(given, family string) domain.Person {
	w.t.Helper()

	return (&familyFixture{t: w.t, st: w.mem}).person(given, family)
}

// add stores p (a new ID if it has none) and the guardian links of p to
// its dependents, directly in the memory store.
func (w *personsFaultWorld) add(p domain.Person, dependents ...domain.PersonID) domain.Person {
	w.t.Helper()

	if p.ID.IsZero() {
		p.ID = ids.New[domain.PersonID](ids.V7{})
	}

	err := w.mem.InTx(w.t.Context(), store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		if err := tx.Persons().Create(ctx, p); err != nil {
			return err
		}

		for _, d := range dependents {
			if err := tx.Persons().AddGuardian(ctx, domain.Guardian{GuardianID: p.ID, DependentID: d}); err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		w.t.Fatal(err)
	}

	return p
}

// TestPersonStoreFaults: every operation of the office's person
// administration returns a store failure and leaves the store as it was.
func TestPersonStoreFaults(t *testing.T) {
	t.Parallel()

	born := domain.Date{Year: 2016, Month: time.May, Day: 4}
	// Each case prepares its fixture (writing to the memory store directly,
	// before SweepFaults takes its snapshot) and returns the operation.
	for name, prepare := range map[string]func(t *testing.T, w *personsFaultWorld) func() error{
		"SearchPersons": func(t *testing.T, w *personsFaultWorld) func() error {
			return func() error {
				_, err := w.svc.SearchPersons(t.Context(), "amberly")

				return err
			}
		},
		// Ella has guardians, an account-less family and class memberships.
		"Person": func(t *testing.T, w *personsFaultWorld) func() error {
			ella := w.person("Ella", "Amberly")

			return func() error {
				d, err := w.svc.Person(t.Context(), ella.ID)
				if err == nil && (len(d.Guardians) != 2 || len(d.Classes) == 0) {
					t.Fatalf("Ella's detail %+v", d)
				}

				return err
			}
		},
		// A tombstone's detail names its survivor.
		"PersonMerged": func(t *testing.T, w *personsFaultWorld) func() error {
			clara := w.person("Clara", "Amberly")
			tombstone := w.add(domain.Person{GivenName: "Klara", FamilyName: "Amberly", MergedInto: clara.ID})

			return func() error {
				d, err := w.svc.Person(t.Context(), tombstone.ID)
				if err == nil && d.MergedInto.ID != clara.ID {
					t.Fatalf("the tombstone's survivor %+v", d.MergedInto)
				}

				return err
			}
		},
		"CreatePerson": func(t *testing.T, w *personsFaultWorld) func() error {
			return func() error {
				_, err := w.svc.CreatePerson(t.Context(), service.PersonRecord{
					GivenName: "Nils", FamilyName: "Example", BirthDate: born,
				})

				return err
			}
		},
		"UpdatePerson": func(t *testing.T, w *personsFaultWorld) func() error {
			ella := w.person("Ella", "Amberly")

			return func() error {
				_, err := w.svc.UpdatePerson(t.Context(), ella.ID, service.PersonRecord{
					GivenName: "Ella", FamilyName: "Amberly", MemberNo: "E-1", BirthDate: born,
				})

				return err
			}
		},
		"DeletePerson": func(t *testing.T, w *personsFaultWorld) func() error {
			p := w.add(domain.Person{GivenName: "Nils", FamilyName: "Example"})

			return func() error { return w.svc.DeletePerson(t.Context(), p.ID) }
		},
		// Clara as a dependent: the cycle check walks Ella and Finn.
		"AddGuardian": func(t *testing.T, w *personsFaultWorld) func() error {
			clara := w.person("Clara", "Amberly")
			gina := w.add(domain.Person{GivenName: "Gina", FamilyName: "Example"})

			return func() error {
				_, err := w.svc.AddGuardian(t.Context(), clara.ID, gina.ID)

				return err
			}
		},
		"RemoveGuardian": func(t *testing.T, w *personsFaultWorld) func() error {
			ella, tom := w.person("Ella", "Amberly"), w.person("Tom", "Amberly")

			return func() error { return w.svc.RemoveGuardian(t.Context(), ella.ID, tom.ID) }
		},
		"Duplicates": func(t *testing.T, w *personsFaultWorld) func() error {
			return func() error {
				_, err := w.svc.Duplicates(t.Context())

				return err
			}
		},
		"DismissDuplicate": func(t *testing.T, w *personsFaultWorld) func() error {
			office := (&familyFixture{t: t, st: w.mem}).principal("olga.ledgerby@example.net")
			a := w.add(domain.Person{GivenName: "Nils", FamilyName: "Example"})
			b := w.add(domain.Person{GivenName: "Nils", FamilyName: "Example"})

			return func() error { return w.svc.DismissDuplicate(t.Context(), office, a.ID, b.ID) }
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			service.SweepFaults(t, service.FaultSweep{Setup: func(t *testing.T) (*service.FaultStore, func() error) {
				w := newPersonsFaultWorld(t)

				return w.fs, prepare(t, w)
			}})
		})
	}
}

// TestDismissDuplicateNeedsAnActor: a dismissal records the account that
// made it; without one (nobody signed in) it fails validation and nothing
// is stored.
func TestDismissDuplicateNeedsAnActor(t *testing.T) {
	t.Parallel()

	w := newPersonsFaultWorld(t)
	a := w.add(domain.Person{GivenName: "Nils", FamilyName: "Example"})
	b := w.add(domain.Person{GivenName: "Nils", FamilyName: "Example"})
	before := w.fs.Snapshot(t)

	err := w.svc.DismissDuplicate(t.Context(), service.Principal{Role: domain.RoleOffice}, a.ID, b.ID)
	wantIs(t, err, domain.ErrValidation)

	if changed := before.Changed(w.fs.Snapshot(t)); len(changed) > 0 {
		t.Errorf("the refused dismissal changed %v", changed)
	}
}

// TestGuardianCycleCheckVisitsOnce:the cycle check of a new guardian link
// walks the dependent's descendants; one it reaches on two paths (a child
// of two of them) is visited once, and a link without a cycle is added.
func TestGuardianCycleCheckVisitsOnce(t *testing.T) {
	t.Parallel()

	w := newPersonsFaultWorld(t)
	child := w.add(domain.Person{GivenName: "Kim", FamilyName: "Example"})
	a := w.add(domain.Person{GivenName: "Ari", FamilyName: "Example"}, child.ID)
	b := w.add(domain.Person{GivenName: "Bo", FamilyName: "Example"}, child.ID)
	dependent := w.add(domain.Person{GivenName: "Dana", FamilyName: "Example"}, a.ID, b.ID)
	guardian := w.add(domain.Person{GivenName: "Gus", FamilyName: "Example"})

	if _, err := w.svc.AddGuardian(t.Context(), dependent.ID, guardian.ID); err != nil {
		t.Fatalf("AddGuardian: %v", err)
	}

	// Dana's dependents and the child are each locked once: the walk does
	// not go over Kim twice.
	if got := w.fs.Count("Persons.Lock"); got != 4 {
		t.Errorf("%d person locks, want 4: the pair, Ari, Bo and Kim once", got)
	}

	// The child cannot become a guardian of Dana, found on either path.
	_, err := w.svc.AddGuardian(t.Context(), dependent.ID, child.ID)
	wantField(t, err, "guardianId")
}

// TestGuardianLinkPersonsGone: a person deleted while a guardian link is
// added. A descendant the cycle check finds deleted meanwhile (its lock
// reports it unknown) has no links left to walk, and the link is added; a
// dependent or guardian the first lock reports unknown is not found.
func TestGuardianLinkPersonsGone(t *testing.T) {
	t.Parallel()

	setup := func(t *testing.T) (*personsFaultWorld, domain.Person, domain.Person) {
		w := newPersonsFaultWorld(t)
		kid := w.add(domain.Person{GivenName: "Kim", FamilyName: "Example"})
		dependent := w.add(domain.Person{GivenName: "Dana", FamilyName: "Example"}, kid.ID)
		guardian := w.add(domain.Person{GivenName: "Gus", FamilyName: "Example"})

		return w, dependent, guardian
	}

	t.Run("descendant", func(t *testing.T) {
		t.Parallel()

		w, dependent, guardian := setup(t)
		w.fs.FailWith("Persons.Lock", 2, domain.ErrNotFound) // Kim's

		if _, err := w.svc.AddGuardian(t.Context(), dependent.ID, guardian.ID); err != nil {
			t.Fatalf("AddGuardian: %v", err)
		}

		d, err := w.svc.Person(t.Context(), dependent.ID)
		if err != nil || !slices.ContainsFunc(d.Guardians, func(p domain.Person) bool { return p.ID == guardian.ID }) {
			t.Errorf("Dana's guardians %+v, %v; want Gus", d.Guardians, err)
		}
	})

	t.Run("pair", func(t *testing.T) {
		t.Parallel()

		w, dependent, guardian := setup(t)
		before := w.fs.Snapshot(t)
		w.fs.FailWith("Persons.Lock", 1, domain.ErrNotFound)

		_, err := w.svc.AddGuardian(t.Context(), dependent.ID, guardian.ID)
		wantIs(t, err, domain.ErrNotFound)

		if changed := before.Changed(w.fs.Snapshot(t)); len(changed) > 0 {
			t.Errorf("the refused link changed %v", changed)
		}
	})
}
