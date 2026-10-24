package service_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/service"
	"github.com/cwbudde/studio-schedule/internal/store"
)

func TestAuditLog(t *testing.T) {
	t.Parallel()

	f := newAuthFixture(t)
	ctx := t.Context()
	admin := f.signedUp("admin@example.org", domain.RoleAdmin)
	member := f.signedUp("bea@example.org", domain.RoleMember)

	got, err := f.svc.AuditLog(ctx)
	if err != nil || len(got) != 0 {
		t.Fatalf("AuditLog before any event = %+v, %v", got, err)
	}

	as, err := f.svc.Impersonate(ctx, admin.Principal, member.Principal.AccountID)
	if err != nil {
		t.Fatal(err)
	}

	f.clock.advance(time.Minute)

	if _, err := f.svc.StopImpersonation(ctx, as.Principal); err != nil {
		t.Fatal(err)
	}

	got, err = f.svc.AuditLog(ctx)
	if err != nil {
		t.Fatal(err)
	}

	want := []domain.AuditAction{domain.AuditImpersonationEnded, domain.AuditImpersonationStarted}
	if len(got) != len(want) {
		t.Fatalf("AuditLog = %+v, want end and start", got)
	}

	for i, e := range got {
		if e.Event.Action != want[i] ||
			e.Actor.Account.Email != "admin@example.org" || e.Actor.Person.ID != e.Actor.Account.PersonID ||
			e.Target.Account.Email != "bea@example.org" || e.Target.Person.ID != e.Target.Account.PersonID ||
			e.Actor.Person.GivenName == "" {
			t.Errorf("entry %d = %+v", i, e)
		}
	}

	if !got[0].Event.At.After(got[1].Event.At) {
		t.Errorf("entries not newest first: %v, %v", got[0].Event.At, got[1].Event.At)
	}
}

func TestAuditLogLimit(t *testing.T) {
	t.Parallel()

	f := newAuthFixture(t)
	admin := f.signedUp("admin@example.org", domain.RoleAdmin)
	member := f.signedUp("bea@example.org", domain.RoleMember)
	start := f.clock.Now()

	err := f.st.InTx(t.Context(), store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		for i := range service.AuditLogLimit + 5 {
			err := tx.Audit().Create(ctx, domain.AuditEvent{
				ID: domain.AuditEventID{0xa0, byte(i >> 8), byte(i)}, At: start.Add(time.Duration(i) * time.Minute),
				ActorID: admin.Principal.AccountID, Action: domain.AuditImpersonationStarted,
				TargetID: member.Principal.AccountID,
			})
			if err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	got, err := f.svc.AuditLog(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != service.AuditLogLimit {
		t.Fatalf("%d entries, want %d", len(got), service.AuditLogLimit)
	}

	newest := start.Add(time.Duration(service.AuditLogLimit+4) * time.Minute)
	if !got[0].Event.At.Equal(newest) {
		t.Errorf("first entry at %v, want the newest %v", got[0].Event.At, newest)
	}
}

func TestAuditLogUnknownAccount(t *testing.T) {
	t.Parallel()

	f := newAuthFixture(t)
	ctx := t.Context()
	admin := f.signedUp("admin@example.org", domain.RoleAdmin)
	member := f.signedUp("bea@example.org", domain.RoleMember)
	office := f.signedUp("otto@example.org", domain.RoleOffice)

	for _, target := range []domain.AccountID{member.Principal.AccountID, office.Principal.AccountID} {
		as, err := f.svc.Impersonate(ctx, admin.Principal, target)
		if err != nil {
			t.Fatal(err)
		}

		if admin, err = f.svc.StopImpersonation(ctx, as.Principal); err != nil {
			t.Fatal(err)
		}
	}

	officePerson, err := f.svc.Me(ctx, office.Principal)
	if err != nil {
		t.Fatal(err)
	}

	// The database forbids deleting an account an audit event names, or its
	// person, so only a store that cannot find them shows how AuditLog
	// degrades: an account without its person keeps its email, an account
	// that cannot be found keeps only its ID.
	var calls []string

	st := lookupStore{
		Store: f.st, calls: &calls,
		missingAccount: member.Principal.AccountID, missingPerson: officePerson.Person.ID,
	}
	svc := service.NewAuthService(st, f.clock, ids.NewSequence(), nil, nil, "")

	got, err := svc.AuditLog(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != 4 {
		t.Fatalf("AuditLog = %+v, want four entries", got)
	}

	targets := map[domain.AccountID]service.AccountEntry{}
	for _, e := range got {
		targets[e.Target.Account.ID] = e.Target

		if e.Actor.Account.Email != "admin@example.org" || e.Actor.Person.GivenName == "" {
			t.Errorf("actor = %+v, want the admin with their person", e.Actor)
		}
	}

	if got, want := targets[member.Principal.AccountID], (service.AccountEntry{Account: domain.Account{ID: member.Principal.AccountID}}); got != want {
		t.Errorf("unknown account = %+v, want only the ID", got)
	}

	if got := targets[office.Principal.AccountID]; got.Account.Email != "otto@example.org" || got.Person != (domain.Person{}) {
		t.Errorf("account without person = %+v, want the account with its email and no person", got)
	}

	want := []string{"Accounts.ListByIDs", "Persons.ListByIDs"}
	if !slices.Equal(calls, want) {
		t.Errorf("lookups = %v, want %v: one batched lookup each", calls, want)
	}
}

// lookupStore records account and person lookups and cannot find the
// account missingAccount and the person missingPerson.
type lookupStore struct {
	store.Store

	missingAccount domain.AccountID
	missingPerson  domain.PersonID
	calls          *[]string
}

func (s lookupStore) InTx(ctx context.Context, mode store.TxMode, fn func(context.Context, store.Tx) error) error {
	return s.Store.InTx(ctx, mode, func(ctx context.Context, tx store.Tx) error {
		return fn(ctx, lookupTx{Tx: tx, s: s})
	})
}

type lookupTx struct {
	store.Tx

	s lookupStore
}

func (t lookupTx) Accounts() store.AccountRepo { return lookupAccounts{t.Tx.Accounts(), t.s} }

func (t lookupTx) Persons() store.PersonRepo { return lookupPersons{t.Tx.Persons(), t.s} }

type lookupAccounts struct {
	store.AccountRepo

	s lookupStore
}

func (r lookupAccounts) Get(ctx context.Context, id domain.AccountID) (domain.Account, error) {
	*r.s.calls = append(*r.s.calls, "Accounts.Get")

	return r.AccountRepo.Get(ctx, id)
}

func (r lookupAccounts) List(ctx context.Context) ([]domain.Account, error) {
	*r.s.calls = append(*r.s.calls, "Accounts.List")

	return r.AccountRepo.List(ctx)
}

func (r lookupAccounts) ListByIDs(ctx context.Context, ids []domain.AccountID) ([]domain.Account, error) {
	*r.s.calls = append(*r.s.calls, "Accounts.ListByIDs")

	found, err := r.AccountRepo.ListByIDs(ctx, ids)

	return slices.DeleteFunc(found, func(a domain.Account) bool { return a.ID == r.s.missingAccount }), err
}

type lookupPersons struct {
	store.PersonRepo

	s lookupStore
}

func (r lookupPersons) Get(ctx context.Context, id domain.PersonID) (domain.Person, error) {
	*r.s.calls = append(*r.s.calls, "Persons.Get")

	return r.PersonRepo.Get(ctx, id)
}

func (r lookupPersons) List(ctx context.Context) ([]domain.Person, error) {
	*r.s.calls = append(*r.s.calls, "Persons.List")

	return r.PersonRepo.List(ctx)
}

func (r lookupPersons) ListByIDs(ctx context.Context, ids []domain.PersonID) ([]domain.Person, error) {
	*r.s.calls = append(*r.s.calls, "Persons.ListByIDs")

	found, err := r.PersonRepo.ListByIDs(ctx, ids)

	return slices.DeleteFunc(found, func(p domain.Person) bool { return p.ID == r.s.missingPerson }), err
}
