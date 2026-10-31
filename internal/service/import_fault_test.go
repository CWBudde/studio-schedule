package service_test

import (
	"context"
	"errors"
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

// importFaultNow is the import sweeps' clock.
var importFaultNow = time.Date(2026, time.October, 21, 10, 0, 0, 0, time.UTC)

// importFaultFile updates a member, finding one guardian by its account's
// email (which gives the person that email) and linking it after the cycle
// check, and another already linked by name; creates a member with a guardian found by name (updated, with a
// dependent of its own) and a new one; leaves a member unchanged; fails a
// row; finds a guardian by the email of a member an earlier row created;
// and repeats the member number of a member an earlier row created (read
// under its lock, then failed). All fictional.
var importFaultFile = service.ImportFile{Name: "members.csv", Data: []byte(
	"no;given;family;mail;born;g1 given;g1 family;g1 mail;g1 phone;g2 given;g2 family\n" +
		"M-1;Mia;Example-Neu;;;Maren;Example;maren@example.org;;Felix;Example\n" +
		"N-1;Nina;Example;nina@example.org;2017-05-04;Olaf;Example;;+49 000 0000601;Gina;Example\n" +
		"M-2;Tom;Example;;;;;;;;\n" +
		"N-2;;Example;;;;;;;;\n" +
		"N-3;Ned;Example;;;Nina;Example;nina@example.org;;;\n" +
		"N-4;Nell;Example;;;;;;;;\n" +
		"N-4;Nell;Example;;;;;;;;\n")}

var importFaultMapping = service.ImportMapping{Columns: map[service.ImportField]string{
	service.FieldMemberNo: "no", service.FieldGivenName: "given", service.FieldFamilyName: "family",
	service.FieldEmail: "mail", service.FieldBirthDate: "born",
	service.FieldGuardian1GivenName: "g1 given", service.FieldGuardian1FamilyName: "g1 family",
	service.FieldGuardian1Email: "g1 mail", service.FieldGuardian1Phone: "g1 phone",
	service.FieldGuardian2GivenName: "g2 given", service.FieldGuardian2FamilyName: "g2 family",
}}

// importFaultWorld is the school importFaultFile runs on, with an
// ImportService on a FaultStore around it.
type importFaultWorld struct {
	fs    *service.FaultStore
	svc   *service.ImportService
	actor domain.AccountID
}

func newImportFaultWorld(t *testing.T) *importFaultWorld {
	t.Helper()

	mem := memory.New()
	seq := ids.NewSequence()
	person := func(given, family, email, no string) domain.Person {
		return domain.Person{ID: ids.New[domain.PersonID](seq), GivenName: given, FamilyName: family, Email: email, MemberNo: no}
	}

	mia := person("Mia", "Example", "mia@example.org", "M-1")
	maren := person("Maren", "Example", "", "")
	felix := person("Felix", "Example", "felix@example.org", "")
	kai := person("Kai", "Example", "", "")
	olaf := person("Olaf", "Example", "", "")
	sib := person("Sina", "Example", "", "")
	tom := person("Tom", "Example", "", "M-2")
	account := domain.Account{
		ID: ids.New[domain.AccountID](seq), PersonID: maren.ID, Email: "maren@example.org", Role: domain.RoleOffice,
		ConfirmedAt: importFaultNow,
	}

	err := mem.InTx(t.Context(), store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		errs := []error{tx.Settings().Put(ctx, seed.DemoSettings())}

		for _, p := range []domain.Person{mia, maren, felix, kai, olaf, sib, tom} {
			errs = append(errs, tx.Persons().Create(ctx, p))
		}

		for _, g := range []domain.Guardian{
			{GuardianID: felix.ID, DependentID: mia.ID},
			{GuardianID: mia.ID, DependentID: kai.ID},
			{GuardianID: olaf.ID, DependentID: sib.ID},
		} {
			errs = append(errs, tx.Persons().AddGuardian(ctx, g))
		}

		errs = append(errs, tx.Accounts().Create(ctx, account), tx.Imports().Create(ctx, domain.ImportRun{
			ID: ids.New[domain.ImportRunID](seq), FileName: "earlier.csv", ActorID: account.ID,
			StartedAt: importFaultNow.Add(-time.Hour), FinishedAt: importFaultNow.Add(-time.Hour),
		}))

		return errors.Join(errs...)
	})
	if err != nil {
		t.Fatal(err)
	}

	fs := service.NewFaultStore(mem)

	return &importFaultWorld{fs: fs, svc: service.NewImportService(fs, clock.NewFixed(importFaultNow), seq), actor: account.ID}
}

// TestImportStoreFaults: a store failure anywhere in an import, applied or
// a dry run, is returned and nothing of it stays: no person, no link, no
// run. Listing the runs returns it too.
func TestImportStoreFaults(t *testing.T) {
	t.Parallel()

	want := domain.ImportCounts{
		Rows: 7, Created: 3, Updated: 1, Unchanged: 1, Failed: 2, GuardiansCreated: 1, GuardiansUpdated: 2, LinksAdded: 4,
	}

	for _, apply := range []bool{true, false} {
		name := "dry run"
		if apply {
			name = "applied"
		}

		t.Run(name, func(t *testing.T) {
			t.Parallel()

			service.SweepFaults(t, service.FaultSweep{Setup: func(t *testing.T) (*service.FaultStore, func() error) {
				w := newImportFaultWorld(t)

				return w.fs, func() error {
					res, err := w.svc.Import(t.Context(), w.actor, importFaultFile, importFaultMapping, apply)
					if err == nil && res.Counts != want {
						t.Fatalf("counts %+v, want %+v", res.Counts, want)
					}

					return err
				}
			}})
		})
	}

	t.Run("runs", func(t *testing.T) {
		t.Parallel()

		service.SweepFaults(t, service.FaultSweep{Setup: func(t *testing.T) (*service.FaultStore, func() error) {
			w := newImportFaultWorld(t)

			return w.fs, func() error {
				runs, err := w.svc.ImportRuns(t.Context())
				if err == nil && (len(runs) != 1 || runs[0].Actor.Account.ID != w.actor) {
					t.Fatalf("runs %+v", runs)
				}

				return err
			}
		}})
	})
}

// TestImportPersonGoneWhileLocking: a person the import would find, deleted
// between the read and the lock (the lock reports it unknown), makes the
// import read the persons again; it then runs on what is there.
func TestImportPersonGoneWhileLocking(t *testing.T) {
	t.Parallel()

	w := newImportFaultWorld(t)
	w.fs.FailWith("Persons.Lock", 1, domain.ErrNotFound)

	res, err := w.svc.Import(t.Context(), w.actor, importFaultFile, importFaultMapping, false)
	if err != nil {
		t.Fatal(err)
	}

	if res.Counts.Rows != 7 || res.Counts.Failed != 2 {
		t.Errorf("counts %+v", res.Counts)
	}

	if got := w.fs.Count("Persons.List"); got != 2 {
		t.Errorf("the persons were read %d times, want twice", got)
	}
}
