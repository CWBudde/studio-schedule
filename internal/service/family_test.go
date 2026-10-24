package service_test

import (
	"context"
	"fmt"
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

// familyFixture runs on the demo data. Clara Amberly (the demo member) and
// Tom, who has no account, are both guardians of Ella and Finn; Hanna
// Birchmoor alone is the guardian of Lotta and Mia. All four children are
// class members.
type familyFixture struct {
	t   *testing.T
	st  store.Store
	svc *service.FamilyService
}

func newFamilyFixture(t *testing.T) *familyFixture {
	t.Helper()

	st := memory.New()
	if err := seed.LoadDemo(t.Context(), st); err != nil {
		t.Fatal(err)
	}

	// Wednesday 2026-10-21, 23:30 in Berlin: already the 22nd at school.
	now := time.Date(2026, time.October, 21, 22, 30, 0, 0, time.UTC)

	return &familyFixture{t: t, st: st, svc: service.NewFamilyService(st, clock.NewFixed(now), ids.V7{})}
}

// principal returns the principal of the account with email.
func (f *familyFixture) principal(email string) service.Principal {
	f.t.Helper()

	var acc domain.Account

	err := f.st.InTx(f.t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		var err error
		acc, err = tx.Accounts().GetByEmail(ctx, email)

		return err
	})
	if err != nil {
		f.t.Fatalf("account %s: %v", email, err)
	}

	return service.Principal{AccountID: acc.ID, PersonID: acc.PersonID, Email: acc.Email, Role: acc.Role}
}

// person returns the person with the given name.
func (f *familyFixture) person(given, family string) domain.Person {
	f.t.Helper()

	var persons []domain.Person

	err := f.st.InTx(f.t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		var err error
		persons, err = tx.Persons().List(ctx)

		return err
	})
	if err != nil {
		f.t.Fatal(err)
	}

	i := slices.IndexFunc(persons, func(p domain.Person) bool { return p.GivenName == given && p.FamilyName == family })
	if i < 0 {
		f.t.Fatalf("no person %s %s", given, family)
	}

	return persons[i]
}

// exists reports whether the person with id is stored.
func (f *familyFixture) exists(id domain.PersonID) bool {
	f.t.Helper()

	err := f.st.InTx(f.t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		_, err := tx.Persons().Get(ctx, id)

		return err
	})

	return err == nil
}

// dependents returns the given names of p's dependents in order.
func (f *familyFixture) dependents(p service.Principal) []string {
	f.t.Helper()

	family, err := f.svc.Family(f.t.Context(), p)
	if err != nil {
		f.t.Fatalf("Family: %v", err)
	}

	var names []string
	for _, d := range family.Dependents {
		names = append(names, d.Person.GivenName)
	}

	return names
}

const (
	clara = "clara.amberly@example.org"
	hanna = "hanna.birchmoor@example.com"
)

func TestFamily(t *testing.T) {
	t.Parallel()

	f := newFamilyFixture(t)

	got, err := f.svc.Family(t.Context(), f.principal(clara))
	if err != nil {
		t.Fatal(err)
	}

	if got.Me.GivenName != "Clara" || got.Me.FamilyName != "Amberly" {
		t.Errorf("me = %+v", got.Me)
	}

	if len(got.Dependents) != 2 || got.Dependents[0].Person.GivenName != "Ella" || got.Dependents[1].Person.GivenName != "Finn" {
		t.Fatalf("dependents = %+v, want Ella and Finn", got.Dependents)
	}

	for _, d := range got.Dependents {
		if len(d.Guardians) != 1 || d.Guardians[0].GivenName != "Tom" {
			t.Errorf("other guardians of %s = %+v, want Tom", d.Person.GivenName, d.Guardians)
		}
	}

	if names := f.dependents(f.principal("irene.inkwell@example.com")); len(names) != 0 {
		t.Errorf("someone without children has dependents %v", names)
	}
}

func TestDependentsOfOthersAreNotFound(t *testing.T) {
	t.Parallel()

	f := newFamilyFixture(t)
	ella := f.person("Ella", "Amberly")
	hannaP := f.principal(hanna)

	_, err := f.svc.UpdateDependent(t.Context(), hannaP, ella.ID, service.PersonData{GivenName: "X", FamilyName: "Y"})
	wantIs(t, err, domain.ErrNotFound)
	wantIs(t, f.svc.RemoveDependent(t.Context(), hannaP, ella.ID), domain.ErrNotFound)

	// Your own person is not your dependent either, nor is an unknown one.
	claraP := f.principal(clara)
	wantIs(t, f.svc.RemoveDependent(t.Context(), claraP, claraP.PersonID), domain.ErrNotFound)
	wantIs(t, f.svc.RemoveDependent(t.Context(), claraP, ids.New[domain.PersonID](ids.V7{})), domain.ErrNotFound)

	if got := f.person("Ella", "Amberly"); got != ella {
		t.Errorf("Ella changed: %+v", got)
	}
}

func TestDependentsLifecycle(t *testing.T) {
	t.Parallel()

	f := newFamilyFixture(t)
	p := f.principal(clara)

	added, err := f.svc.AddDependent(t.Context(), p, service.PersonData{
		GivenName: " Paula ", FamilyName: "Amberly", BirthDate: domain.Date{Year: 2021, Month: time.May, Day: 4},
	})
	if err != nil {
		t.Fatalf("AddDependent: %v", err)
	}

	if added.GivenName != "Paula" || added.ID.IsZero() {
		t.Errorf("added %+v", added)
	}

	if got := f.dependents(p); !slices.Equal(got, []string{"Ella", "Finn", "Paula"}) {
		t.Errorf("dependents %v", got)
	}

	updated, err := f.svc.UpdateDependent(t.Context(), p, added.ID, service.PersonData{
		GivenName: "Paula", FamilyName: "Amberly-Stone", Phone: "+49 000 0000199",
	})
	if err != nil {
		t.Fatalf("UpdateDependent: %v", err)
	}

	if updated.FamilyName != "Amberly-Stone" || updated.Phone == "" || !updated.BirthDate.IsZero() {
		t.Errorf("updated %+v", updated)
	}

	// Clara is Paula's only guardian, and nothing refers to Paula: removing
	// her deletes the person.
	if err := f.svc.RemoveDependent(t.Context(), p, added.ID); err != nil {
		t.Fatalf("RemoveDependent: %v", err)
	}

	if f.exists(added.ID) {
		t.Error("the orphaned dependent was not deleted")
	}
}

func TestRemoveDependent(t *testing.T) {
	t.Parallel()

	t.Run("another guardian keeps the child", func(t *testing.T) {
		t.Parallel()

		f := newFamilyFixture(t)
		p := f.principal(clara)
		ella := f.person("Ella", "Amberly")

		if err := f.svc.RemoveDependent(t.Context(), p, ella.ID); err != nil {
			t.Fatalf("RemoveDependent: %v", err)
		}

		if got := f.dependents(p); !slices.Equal(got, []string{"Finn"}) {
			t.Errorf("Clara's dependents %v, want Finn", got)
		}

		if !f.exists(ella.ID) {
			t.Error("Ella was deleted although Tom is still her guardian")
		}
	})

	t.Run("a referenced child of its last guardian stays", func(t *testing.T) {
		t.Parallel()

		f := newFamilyFixture(t)
		p := f.principal(hanna)
		mia := f.person("Mia", "Birchmoor") // a class member

		err := f.svc.RemoveDependent(t.Context(), p, mia.ID)
		wantIs(t, err, service.ErrDependentInUse)
		wantIs(t, err, domain.ErrConflict)

		if got := f.dependents(p); !slices.Equal(got, []string{"Lotta", "Mia"}) {
			t.Errorf("Hanna's dependents %v: the refused removal changed something", got)
		}
	})
}

func TestUpdatePerson(t *testing.T) {
	t.Parallel()

	f := newFamilyFixture(t)
	p := f.principal(clara)
	before := f.person("Clara", "Amberly")
	today := domain.Date{Year: 2026, Month: time.October, Day: 22} // at school

	got, err := f.svc.UpdatePerson(t.Context(), p, service.PersonData{
		GivenName: " Clara Maria ", FamilyName: "Amberly", BirthDate: today,
	})
	if err != nil {
		t.Fatalf("UpdatePerson: %v", err)
	}

	want := before
	want.GivenName, want.Phone, want.BirthDate = "Clara Maria", "", today

	if got != want || f.person("Clara Maria", "Amberly") != want {
		t.Errorf("person = %+v, want %+v (email and notes kept)", got, want)
	}

	for name, d := range map[string]service.PersonData{
		"blank name":   {GivenName: " ", FamilyName: "Amberly"},
		"future birth": {GivenName: "Clara", FamilyName: "Amberly", BirthDate: today.AddDays(1)},
		"invalid date": {GivenName: "Clara", FamilyName: "Amberly", BirthDate: domain.Date{Year: 2026, Month: time.February, Day: 30}},
	} {
		_, err := f.svc.UpdatePerson(t.Context(), p, d)
		if err == nil {
			t.Errorf("%s: accepted", name)

			continue
		}

		wantIs(t, err, domain.ErrValidation)

		if name == "future birth" {
			wantIs(t, err, service.ErrFutureBirthDate)
		}
	}
}

func TestFamilyWhileImpersonating(t *testing.T) {
	t.Parallel()

	f := newFamilyFixture(t)
	p := f.principal(clara)
	p.ImpersonatorID = f.principal("arno.keystone@example.net").AccountID

	if _, err := f.svc.AddDependent(t.Context(), p, service.PersonData{GivenName: "Paula", FamilyName: "Amberly"}); err != nil {
		t.Errorf("AddDependent while impersonating: %v", err)
	}
}

// write runs fn in a read-write transaction of the fixture's store.
func (f *familyFixture) write(fn func(ctx context.Context, tx store.Tx) error) {
	f.t.Helper()

	if err := f.st.InTx(f.t.Context(), store.ReadWrite, fn); err != nil {
		f.t.Fatal(err)
	}
}

// giveAccount creates an account for p, as the office does for a dependent
// who grows up.
func (f *familyFixture) giveAccount(p domain.Person, email string) {
	f.t.Helper()

	f.write(func(ctx context.Context, tx store.Tx) error {
		return tx.Accounts().Create(ctx, domain.Account{
			ID: ids.New[domain.AccountID](ids.V7{}), PersonID: p.ID, Email: email, Role: domain.RoleMember,
		})
	})
}

// linkStaff links a new staff member to p.
func (f *familyFixture) linkStaff(p domain.Person) {
	f.t.Helper()

	f.write(func(ctx context.Context, tx store.Tx) error {
		return tx.Staff().Create(ctx, domain.Staff{
			ID: ids.New[domain.StaffID](ids.V7{}), PersonID: p.ID, DisplayName: p.GivenName, Kind: domain.StaffAccompanist,
		})
	})
}

// linkDependent makes p the guardian of a new person.
func (f *familyFixture) linkDependent(p domain.Person) {
	f.t.Helper()

	f.write(func(ctx context.Context, tx store.Tx) error {
		child := domain.Person{ID: ids.New[domain.PersonID](ids.V7{}), GivenName: "Nora", FamilyName: p.FamilyName}
		if err := tx.Persons().Create(ctx, child); err != nil {
			return err
		}

		return tx.Persons().AddGuardian(ctx, domain.Guardian{GuardianID: p.ID, DependentID: child.ID})
	})
}

func TestManagedDependents(t *testing.T) {
	t.Parallel()

	for name, manage := range map[string]func(f *familyFixture, p domain.Person){
		"own account": func(f *familyFixture, p domain.Person) { f.giveAccount(p, "lotta.birchmoor@example.com") },
		"staff link":  func(f *familyFixture, p domain.Person) { f.linkStaff(p) },
		// A teenager with a child of their own: deleting them would orphan it.
		"guardian of someone": func(f *familyFixture, p domain.Person) { f.linkDependent(p) },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newFamilyFixture(t)
			p := f.principal(hanna)
			lotta := f.person("Lotta", "Birchmoor") // Hanna is her only guardian; a class member
			manage(f, lotta)

			family, err := f.svc.Family(t.Context(), p)
			if err != nil {
				t.Fatal(err)
			}

			for _, d := range family.Dependents {
				if want := d.Person.ID == lotta.ID; d.Managed != want {
					t.Errorf("%s managed = %v, want %v", d.Person.GivenName, d.Managed, want)
				}
			}

			// The guardian no longer edits what its owner or the office manages.
			_, err = f.svc.UpdateDependent(t.Context(), p, lotta.ID, service.PersonData{GivenName: "L", FamilyName: "B"})
			wantIs(t, err, service.ErrDependentManaged)
			wantIs(t, err, domain.ErrConflict)

			if got := f.person("Lotta", "Birchmoor"); got != lotta {
				t.Errorf("Lotta changed: %+v", got)
			}

			// Removing her only ends the guardianship, though Hanna was the
			// last guardian and Lotta is enrolled.
			if err := f.svc.RemoveDependent(t.Context(), p, lotta.ID); err != nil {
				t.Fatalf("RemoveDependent: %v", err)
			}

			if !f.exists(lotta.ID) {
				t.Error("the managed dependent was deleted")
			}

			if got := f.dependents(p); !slices.Equal(got, []string{"Mia"}) {
				t.Errorf("Hanna's dependents %v, want Mia", got)
			}
		})
	}
}

func TestDependentInUseIsInUse(t *testing.T) {
	t.Parallel()

	f := newFamilyFixture(t)

	err := f.svc.RemoveDependent(t.Context(), f.principal(hanna), f.person("Mia", "Birchmoor").ID)
	wantIs(t, err, service.ErrDependentInUse)
	wantIs(t, err, domain.ErrInUse)
}

// TestRemoveDependentDeleteRefused checks the removal of a dependent whose
// deletion the store refuses although the service found nothing that would
// make it managed: it is in use, and nothing changes.
func TestRemoveDependentDeleteRefused(t *testing.T) {
	t.Parallel()

	f := newFamilyFixture(t)
	p := f.principal(clara)

	added, err := f.svc.AddDependent(t.Context(), p, service.PersonData{GivenName: "Paula", FamilyName: "Amberly"})
	if err != nil {
		t.Fatal(err)
	}

	clk := clock.NewFixed(time.Date(2026, time.October, 21, 22, 30, 0, 0, time.UTC))
	refusing := service.NewFamilyService(racingStore{Store: f.st, deleteErr: fmt.Errorf("foreign key violated: %w", domain.ErrConflict)}, clk, ids.V7{})

	err = refusing.RemoveDependent(t.Context(), p, added.ID)
	wantIs(t, err, service.ErrDependentInUse)
	wantIs(t, err, domain.ErrInUse)

	if got := f.dependents(p); !slices.Contains(got, "Paula") {
		t.Errorf("Clara's dependents %v: the refused removal changed something", got)
	}
}
