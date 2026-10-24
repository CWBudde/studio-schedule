package service_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/service"
	"github.com/cwbudde/studio-schedule/internal/store"
	"github.com/cwbudde/studio-schedule/internal/store/memory"
)

func TestSearchPersons(t *testing.T) {
	t.Parallel()

	svc := newPersonsFixture(t).svc
	ctx := t.Context()

	for query, want := range map[string]string{
		"AMBERLY clara":               "Clara Amberly",
		"olga.ledgerby@example":       "Olga Ledgerby",
		"  felix   keys  ":            "Felix Keysworth",
		"clara amberly nobody-at-all": "",
	} {
		found, err := svc.SearchPersons(ctx, query)
		if err != nil {
			t.Fatal(err)
		}

		var names []string
		for _, p := range found {
			names = append(names, p.GivenName+" "+p.FamilyName)
		}

		if (want == "" && len(found) != 0) || (want != "" && (len(found) != 1 || names[0] != want)) {
			t.Errorf("SearchPersons(%q) = %v, want [%s]", query, names, want)
		}
	}

	everyone, err := svc.SearchPersons(ctx, "")
	if err != nil || len(everyone) == 0 {
		t.Fatalf("empty query: %d persons, %v", len(everyone), err)
	}
}

func TestSearchPersonsLimit(t *testing.T) {
	t.Parallel()

	st := memory.New()
	seq := ids.NewSequence()

	err := st.InTx(t.Context(), store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		for range service.MaxPersonMatches + 5 {
			p := domain.Person{ID: ids.New[domain.PersonID](seq), GivenName: "Sam", FamilyName: "Example"}
			if err := tx.Persons().Create(ctx, p); err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	found, err := service.NewPersonService(st, clock.NewFixed(time.Date(2026, time.October, 22, 12, 0, 0, 0, time.UTC)), ids.V7{}, service.Mails{}).SearchPersons(t.Context(), "sam")
	if err != nil || len(found) != service.MaxPersonMatches {
		t.Errorf("found %d, %v; want %d", len(found), err, service.MaxPersonMatches)
	}
}

// personsFixture runs the office's person service on the demo data.
type personsFixture struct {
	t   *testing.T
	st  store.Store
	svc *service.PersonService
}

func newPersonsFixture(t *testing.T) *personsFixture {
	t.Helper()

	st := catalogDemoStore(t)
	// Wednesday 2026-10-21, 23:30 in Berlin: already the 22nd at school.
	now := time.Date(2026, time.October, 21, 22, 30, 0, 0, time.UTC)

	return &personsFixture{t: t, st: st, svc: service.NewPersonService(st, clock.NewFixed(now), ids.V7{}, service.Mails{})}
}

// person returns the person with the given name.
func (f *personsFixture) person(given, family string) domain.Person {
	f.t.Helper()

	found, err := f.svc.SearchPersons(f.t.Context(), given+" "+family)
	if err != nil || len(found) != 1 {
		f.t.Fatalf("person %s %s: %v, %v", given, family, found, err)
	}

	return found[0]
}

func (f *personsFixture) detail(id domain.PersonID) service.PersonDetail {
	f.t.Helper()

	d, err := f.svc.Person(f.t.Context(), id)
	if err != nil {
		f.t.Fatalf("Person(%s): %v", id, err)
	}

	return d
}

func givenNames(persons []domain.Person) []string {
	names := make([]string, 0, len(persons))
	for _, p := range persons {
		names = append(names, p.GivenName)
	}

	return names
}

func TestPersonDetail(t *testing.T) {
	t.Parallel()

	f := newPersonsFixture(t)

	ella := f.detail(f.person("Ella", "Amberly").ID)
	if got := givenNames(ella.Guardians); !slices.Equal(got, []string{"Clara", "Tom"}) {
		t.Errorf("Ella's guardians %v", got)
	}

	if ella.Account != nil || len(ella.Staff) != 0 || len(ella.Dependents) != 0 {
		t.Errorf("Ella: account %v, staff %v, dependents %v; want none", ella.Account, ella.Staff, ella.Dependents)
	}

	if len(ella.Classes) == 0 {
		t.Fatal("Ella has no class memberships")
	}

	for _, c := range ella.Classes {
		if c.Class.ID.IsZero() || c.Term.ID != c.Class.TermID || c.CourseType.ID != c.Class.CourseTypeID {
			t.Errorf("membership %+v", c)
		}
	}

	clara := f.detail(f.person("Clara", "Amberly").ID)
	if got := givenNames(clara.Dependents); !slices.Equal(got, []string{"Ella", "Finn"}) {
		t.Errorf("Clara's dependents %v", got)
	}

	if clara.Account == nil || clara.Account.Email != "clara.amberly@example.org" || clara.Account.Role != domain.RoleMember {
		t.Errorf("Clara's account %+v", clara.Account)
	}

	// A staff member with a person record.
	var staff domain.Staff

	err := f.st.InTx(t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		all, err := tx.Staff().List(ctx)
		if i := slices.IndexFunc(all, func(s domain.Staff) bool { return !s.PersonID.IsZero() }); i >= 0 {
			staff = all[i]
		}

		return err
	})
	if err != nil || staff.ID.IsZero() {
		t.Fatalf("no demo staff member with a person: %v", err)
	}

	if got := f.detail(staff.PersonID).Staff; len(got) != 1 || got[0] != staff {
		t.Errorf("staff of the staff member's person %+v, want %+v", got, staff)
	}

	_, err = f.svc.Person(t.Context(), ids.New[domain.PersonID](ids.V7{}))
	wantIs(t, err, domain.ErrNotFound)
}

func TestPersonAdmin(t *testing.T) {
	t.Parallel()

	f := newPersonsFixture(t)
	ctx := t.Context()
	today := domain.Date{Year: 2026, Month: time.October, Day: 22} // at school

	for name, tc := range map[string]struct {
		data  service.PersonRecord
		field string
	}{
		"no given name": {service.PersonRecord{GivenName: " ", FamilyName: "Example"}, "givenName"},
		"bad email":     {service.PersonRecord{GivenName: "Ida", FamilyName: "Example", Email: "ida at example"}, "email"},
		"future birth":  {service.PersonRecord{GivenName: "Ida", FamilyName: "Example", BirthDate: today.AddDays(1)}, "birthDate"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := f.svc.CreatePerson(ctx, tc.data)
			wantField(t, err, tc.field)
		})
	}

	created, err := f.svc.CreatePerson(ctx, service.PersonRecord{
		GivenName: " Ida ", FamilyName: "Example", Email: " ida@example.org ", BirthDate: today,
	})
	if err != nil || created.ID.IsZero() || created.GivenName != "Ida" || created.Email != "ida@example.org" {
		t.Fatalf("create: %+v, %v", created, err)
	}

	updated, err := f.svc.UpdatePerson(ctx, created.ID, service.PersonRecord{
		GivenName: "Ida", FamilyName: "Example-Stone", Phone: "+49 000 0000188", Notes: " Prefers mornings. ",
	})
	want := domain.Person{
		ID: created.ID, GivenName: "Ida", FamilyName: "Example-Stone", Phone: "+49 000 0000188", Notes: "Prefers mornings.",
	}

	if err != nil || updated != want || f.detail(created.ID).Person != want {
		t.Errorf("update: %+v, %v; want %+v", updated, err, want)
	}

	_, err = f.svc.UpdatePerson(ctx, ids.New[domain.PersonID](ids.V7{}), service.PersonRecord{GivenName: "A", FamilyName: "B"})
	wantIs(t, err, domain.ErrNotFound)
}

func TestGuardianAdmin(t *testing.T) {
	t.Parallel()

	f := newPersonsFixture(t)
	ctx := t.Context()
	ella, hanna := f.person("Ella", "Amberly"), f.person("Hanna", "Birchmoor")

	got, err := f.svc.AddGuardian(ctx, ella.ID, hanna.ID)
	if err != nil || got != hanna {
		t.Fatalf("AddGuardian: %+v, %v", got, err)
	}

	if names := givenNames(f.detail(ella.ID).Guardians); !slices.Equal(names, []string{"Clara", "Tom", "Hanna"}) {
		t.Errorf("Ella's guardians %v", names)
	}

	_, err = f.svc.AddGuardian(ctx, ella.ID, hanna.ID)
	wantIs(t, err, domain.ErrDuplicate)

	_, err = f.svc.AddGuardian(ctx, ella.ID, ids.New[domain.PersonID](ids.V7{}))
	wantField(t, err, "guardianId")

	_, err = f.svc.AddGuardian(ctx, ella.ID, ella.ID)
	wantField(t, err, "guardianId")

	_, err = f.svc.AddGuardian(ctx, ids.New[domain.PersonID](ids.V7{}), hanna.ID)
	wantIs(t, err, domain.ErrNotFound)

	if err := f.svc.RemoveGuardian(ctx, ella.ID, hanna.ID); err != nil {
		t.Fatalf("RemoveGuardian: %v", err)
	}

	wantIs(t, f.svc.RemoveGuardian(ctx, ella.ID, hanna.ID), domain.ErrNotFound)

	// The office removing the last guardian keeps the person.
	mia := f.person("Mia", "Birchmoor")
	if err := f.svc.RemoveGuardian(ctx, mia.ID, hanna.ID); err != nil {
		t.Fatalf("RemoveGuardian of the last guardian: %v", err)
	}

	if d := f.detail(mia.ID); len(d.Guardians) != 0 {
		t.Errorf("Mia's guardians %v, want none", givenNames(d.Guardians))
	}
}

func TestGuardianCycles(t *testing.T) {
	t.Parallel()

	f := newPersonsFixture(t)
	ctx := t.Context()
	clara, ella := f.person("Clara", "Amberly"), f.person("Ella", "Amberly")

	// Ella is Clara's dependent, so she cannot become Clara's guardian.
	_, err := f.svc.AddGuardian(ctx, clara.ID, ella.ID)
	wantField(t, err, "guardianId")

	// Nor can Ella's own dependent, a dependent of Clara through Ella.
	grandchild, err := f.svc.CreatePerson(ctx, service.PersonRecord{GivenName: "Nora", FamilyName: "Amberly"})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := f.svc.AddGuardian(ctx, grandchild.ID, ella.ID); err != nil {
		t.Fatalf("Ella becomes Nora's guardian: %v", err)
	}

	_, err = f.svc.AddGuardian(ctx, clara.ID, grandchild.ID)
	wantField(t, err, "guardianId")

	if got := givenNames(f.detail(clara.ID).Guardians); len(got) != 0 {
		t.Errorf("Clara's guardians %v, want none", got)
	}

	// A guardian of the guardian is fine: no cycle.
	if _, err := f.svc.AddGuardian(ctx, ella.ID, f.person("Hanna", "Birchmoor").ID); err != nil {
		t.Errorf("unrelated guardian: %v", err)
	}
}

// racingStore is a store whose AddGuardian and account Create fail with the
// configured errors, as when a concurrent transaction wins a race after the
// service checked.
type racingStore struct {
	store.Store

	guardianErr, accountErr, deleteErr error
}

func (s racingStore) InTx(ctx context.Context, mode store.TxMode, fn func(context.Context, store.Tx) error) error {
	return s.Store.InTx(ctx, mode, func(ctx context.Context, tx store.Tx) error {
		return fn(ctx, racingTx{Tx: tx, s: s})
	})
}

type racingTx struct {
	store.Tx

	s racingStore
}

func (t racingTx) Persons() store.PersonRepo {
	return racingPersons{t.Tx.Persons(), t.s.guardianErr, t.s.deleteErr}
}

func (t racingTx) Accounts() store.AccountRepo {
	return racingAccounts{t.Tx.Accounts(), t.s.accountErr}
}

type racingPersons struct {
	store.PersonRepo

	err, deleteErr error
}

func (r racingPersons) Delete(ctx context.Context, id domain.PersonID) error {
	if r.deleteErr != nil {
		return r.deleteErr
	}

	return r.PersonRepo.Delete(ctx, id)
}

func (r racingPersons) AddGuardian(ctx context.Context, g domain.Guardian) error {
	if r.err != nil {
		return r.err
	}

	return r.PersonRepo.AddGuardian(ctx, g)
}

type racingAccounts struct {
	store.AccountRepo

	err error
}

func (r racingAccounts) Create(ctx context.Context, a domain.Account) error {
	if r.err != nil {
		return r.err
	}

	return r.AccountRepo.Create(ctx, a)
}

func TestAddGuardianRaces(t *testing.T) {
	t.Parallel()

	f := newPersonsFixture(t)
	ella, hanna := f.person("Ella", "Amberly"), f.person("Hanna", "Birchmoor")
	add := func(storeErr error) error {
		svc := service.NewPersonService(racingStore{Store: f.st, guardianErr: storeErr}, clock.NewFixed(time.Date(2026, time.October, 22, 12, 0, 0, 0, time.UTC)), ids.V7{}, service.Mails{})
		_, err := svc.AddGuardian(t.Context(), ella.ID, hanna.ID)

		return err
	}

	// A link added meanwhile is a duplicate.
	err := add(fmt.Errorf("guardian link exists: %w", domain.ErrDuplicate))
	wantIs(t, err, service.ErrGuardianLinked)
	wantIs(t, err, domain.ErrDuplicate)

	// A person deleted meanwhile is gone, not a duplicate.
	err = add(fmt.Errorf("foreign key violated: %w", domain.ErrConflict))
	wantIs(t, err, domain.ErrNotFound)

	if errors.Is(err, domain.ErrDuplicate) {
		t.Errorf("a vanished person is reported as a duplicate: %v", err)
	}
}

func TestDeletePerson(t *testing.T) {
	t.Parallel()

	f := newPersonsFixture(t)
	ctx := t.Context()
	newPerson := func(given string) domain.Person {
		t.Helper()

		p, err := f.svc.CreatePerson(ctx, service.PersonRecord{GivenName: given, FamilyName: "Example"})
		if err != nil {
			t.Fatal(err)
		}

		return p
	}
	write := func(fn func(ctx context.Context, tx store.Tx) error) {
		t.Helper()

		if err := f.st.InTx(ctx, store.ReadWrite, fn); err != nil {
			t.Fatal(err)
		}
	}

	// Nothing refers to a new person: it goes.
	free := newPerson("Free")
	if err := f.svc.DeletePerson(ctx, free.ID); err != nil {
		t.Fatalf("DeletePerson: %v", err)
	}

	_, err := f.svc.Person(ctx, free.ID)
	wantIs(t, err, domain.ErrNotFound)
	wantIs(t, f.svc.DeletePerson(ctx, free.ID), domain.ErrNotFound)

	// Everything that refers to a person keeps it.
	booked, class := newPerson("Booked"), demoClass(t, f.st)
	write(func(ctx context.Context, tx store.Tx) error {
		lessons, err := tx.Lessons().ListByClass(ctx, class.ID, domain.Date{})
		if err != nil || len(lessons) == 0 {
			return fmt.Errorf("no demo lesson: %w", err)
		}

		return tx.Bookings().Create(ctx, domain.Booking{
			ID: ids.New[domain.BookingID](ids.V7{}), LessonID: lessons[0].ID, PersonID: booked.ID,
			Status: domain.BookingBooked, Source: domain.BookingSourceSelf, BookedAt: lessons[0].StartsAt.Add(-time.Hour),
		})
	})

	guardian, dependent := newPerson("Guardian"), newPerson("Dependent")
	if _, err := f.svc.AddGuardian(ctx, dependent.ID, guardian.ID); err != nil {
		t.Fatal(err)
	}

	var staff domain.PersonID

	write(func(ctx context.Context, tx store.Tx) error {
		all, err := tx.Staff().List(ctx)
		if i := slices.IndexFunc(all, func(s domain.Staff) bool { return !s.PersonID.IsZero() }); i >= 0 {
			staff = all[i].PersonID
		}

		return err
	})

	for name, id := range map[string]domain.PersonID{
		"account":          f.person("Clara", "Amberly").ID, // and guardian of Ella and Finn
		"class membership": newPersonsFixtureMember(t, f, newPerson("Member")),
		"booking":          booked.ID,
		"guardian":         guardian.ID,
		"dependent":        dependent.ID,
		"staff link":       staff,
	} {
		t.Run(name, func(t *testing.T) {
			if id.IsZero() {
				t.Fatal("no such person in the demo data")
			}

			err := f.svc.DeletePerson(ctx, id)
			wantIs(t, err, domain.ErrInUse)

			if _, err := f.svc.Person(ctx, id); err != nil {
				t.Errorf("the refused deletion removed the person: %v", err)
			}
		})
	}
}

// newPersonsFixtureMember makes p a member of a demo class and returns its
// ID.
func newPersonsFixtureMember(t *testing.T, f *personsFixture, p domain.Person) domain.PersonID {
	t.Helper()

	class := demoClass(t, f.st)

	err := f.st.InTx(t.Context(), store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		return tx.Classes().AddMember(ctx, domain.ClassMember{ClassID: class.ID, PersonID: p.ID})
	})
	if err != nil {
		t.Fatal(err)
	}

	return p.ID
}

// recordingStore records the person, account and staff calls of the
// services in order, to check that their checks run under the person lock.
type recordingStore struct {
	store.Store

	calls *[]string
}

func (s recordingStore) InTx(ctx context.Context, mode store.TxMode, fn func(context.Context, store.Tx) error) error {
	return s.Store.InTx(ctx, mode, func(ctx context.Context, tx store.Tx) error {
		return fn(ctx, recordingTx{Tx: tx, calls: s.calls})
	})
}

type recordingTx struct {
	store.Tx

	calls *[]string
}

func (t recordingTx) Persons() store.PersonRepo { return recordingPersons{t.Tx.Persons(), t.calls} }

func (t recordingTx) Accounts() store.AccountRepo { return recordingAccounts{t.Tx.Accounts(), t.calls} }

type recordingPersons struct {
	store.PersonRepo

	calls *[]string
}

func (r recordingPersons) Lock(ctx context.Context, ids ...domain.PersonID) error {
	for _, id := range ids {
		*r.calls = append(*r.calls, "Lock "+id.String())
	}

	return r.PersonRepo.Lock(ctx, ids...)
}

func (r recordingPersons) ListDependents(ctx context.Context, guardian domain.PersonID) ([]domain.Person, error) {
	*r.calls = append(*r.calls, "ListDependents "+guardian.String())

	return r.PersonRepo.ListDependents(ctx, guardian)
}

func (r recordingPersons) AddGuardian(ctx context.Context, g domain.Guardian) error {
	*r.calls = append(*r.calls, "AddGuardian")

	return r.PersonRepo.AddGuardian(ctx, g)
}

func (r recordingPersons) Delete(ctx context.Context, id domain.PersonID) error {
	*r.calls = append(*r.calls, "Delete")

	return r.PersonRepo.Delete(ctx, id)
}

type recordingAccounts struct {
	store.AccountRepo

	calls *[]string
}

func (r recordingAccounts) GetByPerson(ctx context.Context, person domain.PersonID) (domain.Account, error) {
	*r.calls = append(*r.calls, "GetByPerson")

	return r.AccountRepo.GetByPerson(ctx, person)
}

// wantBefore fails unless first occurs in calls, and before every call
// with the prefix then.
func wantBefore(t *testing.T, calls []string, first, then string) {
	t.Helper()

	i := slices.Index(calls, first)
	j := slices.IndexFunc(calls, func(c string) bool { return strings.HasPrefix(c, then) })

	if i < 0 || (j >= 0 && j < i) {
		t.Errorf("calls %v: want %q before %q", calls, first, then)
	}
}

func TestPersonChecksRunUnderTheLock(t *testing.T) {
	t.Parallel()

	f := newPersonsFixture(t)
	ctx := t.Context()
	clk := clock.NewFixed(time.Date(2026, time.October, 22, 12, 0, 0, 0, time.UTC))
	ella, hanna, lotta := f.person("Ella", "Amberly"), f.person("Hanna", "Birchmoor"), f.person("Lotta", "Birchmoor")

	var calls []string

	rec := recordingStore{Store: f.st, calls: &calls}

	// The cycle check reads every dependent only once it is locked.
	if _, err := service.NewPersonService(rec, clk, ids.V7{}, service.Mails{}).AddGuardian(ctx, ella.ID, hanna.ID); err != nil {
		t.Fatal(err)
	}

	wantBefore(t, calls, "Lock "+ella.ID.String(), "ListDependents")
	wantBefore(t, calls, "Lock "+hanna.ID.String(), "ListDependents")

	for i, c := range calls {
		if dep, ok := strings.CutPrefix(c, "ListDependents "); ok && !slices.Contains(calls[:i], "Lock "+dep) {
			t.Errorf("calls %v: dependents of %s read before it was locked", calls, dep)
		}
	}

	wantBefore(t, calls, "Lock "+ella.ID.String(), "AddGuardian")

	// The managed check of a guardian's edit and removal.
	family := service.NewFamilyService(rec, clk, ids.V7{}, service.Mails{})
	p := service.Principal{PersonID: hanna.ID}

	calls = nil
	if _, err := family.UpdateDependent(ctx, p, lotta.ID, service.PersonData{GivenName: "Lotta", FamilyName: "Birchmoor"}); err != nil {
		t.Fatal(err)
	}

	wantBefore(t, calls, "Lock "+lotta.ID.String(), "GetByPerson")

	calls = nil
	if err := family.RemoveDependent(ctx, p, lotta.ID); !errors.Is(err, service.ErrDependentInUse) {
		t.Fatalf("RemoveDependent of an enrolled child: %v", err)
	}

	wantBefore(t, calls, "Lock "+lotta.ID.String(), "GetByPerson")

	// The office's deletion.
	calls = nil
	if err := service.NewPersonService(rec, clk, ids.V7{}, service.Mails{}).DeletePerson(ctx, lotta.ID); !errors.Is(err, domain.ErrInUse) {
		t.Fatalf("DeletePerson of an enrolled child: %v", err)
	}

	wantBefore(t, calls, "Lock "+lotta.ID.String(), "Delete")
}
