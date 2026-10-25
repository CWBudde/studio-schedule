package service_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/service"
	"github.com/cwbudde/studio-schedule/internal/store"
	"github.com/cwbudde/studio-schedule/internal/store/memory"
)

// pairNames returns "Given Family / Given Family (strength: reasons)" for
// each pair.
func pairNames(pairs []service.DuplicatePair) []string {
	out := make([]string, 0, len(pairs))
	for _, p := range pairs {
		reasons := ""
		for _, r := range p.Reasons {
			reasons += " " + string(r)
		}

		out = append(out, p.A.Person.GivenName+" "+p.A.Person.FamilyName+" / "+p.B.Person.GivenName+" "+
			p.B.Person.FamilyName+" ("+string(p.Strength)+":"+reasons+")")
	}

	return out
}

func TestDuplicates(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newPersonsFixture(t)
	var seq ids.V7 // apart from the demo's sequence

	duplicates := func() []service.DuplicatePair {
		t.Helper()

		pairs, err := f.svc.Duplicates(ctx)
		if err != nil {
			t.Fatal(err)
		}

		return pairs
	}

	// The demo's two pairs: Lena by email (ignoring case) and name with one
	// birth date missing, Jonas by name alone.
	want := []string{
		"Lena Quillfeather / Lena Quillfeather (strong: email name)",
		"Jonas Brightwater / Jonas Brightwater (weak: name)",
	}
	if got := pairNames(duplicates()); !slices.Equal(got, want) {
		t.Fatalf("pairs %q, want %q", got, want)
	}

	// An account's email counts, names compare folded, and the same name
	// and birth date is strong; another birth date is no match.
	clara := f.person("Clara", "Amberly")
	born := domain.Date{Year: 1990, Month: time.May, Day: 1}
	err := f.st.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		return errors.Join(
			tx.Persons().Create(ctx, domain.Person{
				ID: ids.New[domain.PersonID](seq), GivenName: "Klara", FamilyName: "Other", Email: "CLARA.amberly@example.org",
			}),
			tx.Persons().Create(ctx, domain.Person{
				ID: ids.New[domain.PersonID](seq), GivenName: "Jörg", FamilyName: "Müller-Lang", BirthDate: born,
			}),
			tx.Persons().Create(ctx, domain.Person{
				ID: ids.New[domain.PersonID](seq), GivenName: "joerg", FamilyName: "mueller lang", BirthDate: born,
			}),
			tx.Persons().Create(ctx, domain.Person{
				ID: ids.New[domain.PersonID](seq), GivenName: "Jörg", FamilyName: "Müller-Lang",
				BirthDate: domain.Date{Year: 1991, Month: time.May, Day: 1},
			}),
		)
	})
	if err != nil {
		t.Fatal(err)
	}

	got := pairNames(duplicates())
	for _, w := range []string{
		"Clara Amberly / Klara Other (strong: email)", "Jörg Müller-Lang / joerg mueller lang (strong: name-birth-date)",
	} {
		if !slices.Contains(got, w) {
			t.Errorf("pairs %q lack %q", got, w)
		}
	}

	if len(got) != 4 {
		t.Errorf("pairs %q, want 4", got)
	}

	// Dismissed, a pair is gone, whichever way it is named.
	jonas := duplicates()[3]
	office := service.Principal{AccountID: f.account(t, "olga.ledgerby@example.net").ID}

	for range 2 {
		if err := f.svc.DismissDuplicate(ctx, office, jonas.B.Person.ID, jonas.A.Person.ID); err != nil {
			t.Fatal(err)
		}
	}

	if got := pairNames(duplicates()); len(got) != 3 || slices.Contains(got, want[1]) {
		t.Errorf("after the dismissal: %q", got)
	}

	if err := f.svc.DismissDuplicate(ctx, office, clara.ID, clara.ID); !errors.Is(err, domain.ErrValidation) {
		t.Errorf("dismissing a person with itself: %v", err)
	}

	if err := f.svc.DismissDuplicate(ctx, office, clara.ID, ids.New[domain.PersonID](seq)); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("dismissing an unknown person: %v", err)
	}

	// After a merge the pair is gone too.
	pairs := duplicates()
	lena := pairs[slices.IndexFunc(pairs, func(p service.DuplicatePair) bool { return p.A.Person.GivenName == "Lena" })]
	_, err = f.svc.Merge(ctx, office, service.MergeRequest{SurvivorID: lena.A.Person.ID, MergedID: lena.B.Person.ID}, true)
	if err != nil {
		t.Fatal(err)
	}

	if got := pairNames(duplicates()); slices.Contains(got, want[0]) {
		t.Errorf("after the merge: %q", got)
	}
}

// account returns the account with the email.
func (f *personsFixture) account(t *testing.T, email string) domain.Account {
	t.Helper()

	var acc domain.Account

	err := f.st.InTx(t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		var err error
		acc, err = tx.Accounts().GetByEmail(ctx, email)

		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	return acc
}

func TestPersonMemberNo(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newPersonsFixture(t)

	ella := f.person("Ella", "Amberly")
	rec := service.PersonRecord{GivenName: "Ella", FamilyName: "Amberly", BirthDate: ella.BirthDate, MemberNo: " M-0500 "}

	p, err := f.svc.UpdatePerson(ctx, ella.ID, rec)
	if err != nil || p.MemberNo != "M-0500" {
		t.Fatalf("update: %+v, %v", p, err)
	}

	// Searchable by it.
	if found := f.person("m-0500", ""); found.ID != ella.ID {
		t.Errorf("search by member number found %+v", found)
	}

	// Unique: the seeded Lena has M-1042.
	rec.MemberNo = "M-1042"
	if _, err := f.svc.UpdatePerson(ctx, ella.ID, rec); !errors.Is(err, domain.ErrDuplicate) {
		t.Errorf("a taken member number: %v", err)
	}

	_, err = f.svc.CreatePerson(ctx, service.PersonRecord{GivenName: "New", FamilyName: "Person", MemberNo: "M-1042"})
	if !errors.Is(err, domain.ErrDuplicate) {
		t.Errorf("create with a taken member number: %v", err)
	}
}

// TestDuplicatesWithinAFamily: a guardian and its dependent, or two
// dependents of one guardian, share an email often; they never match by it
// alone, but still by name and birth date. Anyone else with the email does.
func TestDuplicatesWithinAFamily(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	st := memory.New()
	seq := ids.NewSequence()
	svc := service.NewPersonService(st, clock.NewFixed(time.Date(2026, time.October, 21, 10, 0, 0, 0, time.UTC)), seq,
		service.Mails{})
	born := domain.Date{Year: 2015, Month: time.March, Day: 4}

	person := func(given, family, email string, birth domain.Date) domain.Person {
		return domain.Person{ID: ids.New[domain.PersonID](seq), GivenName: given, FamilyName: family, Email: email, BirthDate: birth}
	}

	parent := person("Pat", "Family", "family@example.org", domain.Date{})
	kid := person("Kim", "Family", "family@example.org", born)
	twin := person("Kim", "Family", "FAMILY@example.org", born) // the same child, entered twice
	sibling := person("Lou", "Family", "family@example.org", domain.Date{})
	neighbor := person("Nina", "Next", "family@example.org", domain.Date{})

	err := st.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		errs := make([]error, 0, 8)
		for _, p := range []domain.Person{parent, kid, twin, sibling, neighbor} {
			errs = append(errs, tx.Persons().Create(ctx, p))
		}

		for _, d := range []domain.Person{kid, twin, sibling} {
			errs = append(errs, tx.Persons().AddGuardian(ctx, domain.Guardian{GuardianID: parent.ID, DependentID: d.ID}))
		}

		return errors.Join(errs...)
	})
	if err != nil {
		t.Fatal(err)
	}

	pairs, err := svc.Duplicates(ctx)
	if err != nil {
		t.Fatal(err)
	}

	// The twins by name and birth date only; the neighbor by email with
	// everyone of the family.
	want := []string{
		"Kim Family / Kim Family (strong: name-birth-date)",
		"Kim Family / Nina Next (strong: email)",
		"Kim Family / Nina Next (strong: email)",
		"Lou Family / Nina Next (strong: email)",
		"Pat Family / Nina Next (strong: email)",
	}
	if got := pairNames(pairs); !slices.Equal(got, want) {
		t.Errorf("pairs %q, want %q", got, want)
	}
}
