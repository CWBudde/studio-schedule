package service_test

import (
	"context"
	"slices"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/service"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// signedUp registers and confirms email with role r and returns its
// session.
func (f *authFixture) signedUp(email string, r domain.Role) service.SignedIn {
	f.t.Helper()

	f.register(email, "correct horse")

	s, err := f.svc.Confirm(f.t.Context(), f.lastToken(), "correct horse")
	if err != nil {
		f.t.Fatalf("Confirm(%s): %v", email, err)
	}

	if r != domain.RoleMember {
		f.update(email, func(a *domain.Account) { a.Role = r })
		s.Principal.Role = r
	}

	return s
}

func (f *authFixture) update(email string, change func(*domain.Account)) {
	f.t.Helper()

	acc := f.account(email)
	change(&acc)

	err := f.st.InTx(f.t.Context(), store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		return tx.Accounts().Update(ctx, acc)
	})
	if err != nil {
		f.t.Fatal(err)
	}
}

func (f *authFixture) auditLog() []domain.AuditEvent {
	f.t.Helper()

	var events []domain.AuditEvent

	err := f.st.InTx(f.t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		var err error
		events, err = tx.Audit().List(ctx, 100)

		return err
	})
	if err != nil {
		f.t.Fatal(err)
	}

	slices.Reverse(events) // oldest first

	return events
}

func TestImpersonation(t *testing.T) {
	t.Parallel()

	f := newAuthFixture(t)
	ctx := t.Context()
	admin := f.signedUp("admin@example.org", domain.RoleAdmin)
	member := f.signedUp("bea@example.org", domain.RoleMember)

	as, err := f.svc.Impersonate(ctx, admin.Principal, member.Principal.AccountID)
	if err != nil {
		t.Fatal(err)
	}

	if as.Principal.AccountID != member.Principal.AccountID || as.Principal.Role != domain.RoleMember ||
		as.Principal.ImpersonatorID != admin.Principal.AccountID {
		t.Fatalf("principal = %+v, want the member impersonated by the admin", as.Principal)
	}

	if !as.ExpiresAt.Equal(f.clock.Now().Add(service.ImpersonationMaxAge)) {
		t.Errorf("expires %v, want one hour", as.ExpiresAt)
	}

	// The admin's own session ended.
	_, _, err = f.svc.Authenticate(ctx, admin.Token)
	wantIs(t, err, service.ErrNoSession)

	p, renewed, err := f.svc.Authenticate(ctx, as.Token)
	if err != nil || p != as.Principal || renewed.Token != "" {
		t.Fatalf("Authenticate = %+v, %+v, %v", p, renewed, err)
	}

	me, err := f.svc.Me(ctx, p)
	if err != nil || me.Impersonator == nil || me.Impersonator.Account.Email != "admin@example.org" ||
		me.Account.Email != "bea@example.org" {
		t.Fatalf("Me = %+v, %v", me, err)
	}

	// No sliding expiry: after two days of use it is still over.
	f.clock.advance(service.ImpersonationMaxAge - 1)

	if _, renewed, err := f.svc.Authenticate(ctx, as.Token); err != nil || renewed.Token != "" {
		t.Fatalf("near the end: renewed %+v, %v", renewed, err)
	}

	back, err := f.svc.StopImpersonation(ctx, p)
	if err != nil {
		t.Fatal(err)
	}

	if back.Principal.AccountID != admin.Principal.AccountID || !back.Principal.ImpersonatorID.IsZero() ||
		back.Token == admin.Token || back.Token == as.Token {
		t.Fatalf("back = %+v, want a new admin session", back)
	}

	_, _, err = f.svc.Authenticate(ctx, as.Token)
	wantIs(t, err, service.ErrNoSession)

	if _, _, err := f.svc.Authenticate(ctx, back.Token); err != nil {
		t.Fatalf("admin session after return: %v", err)
	}

	log := f.auditLog()
	want := []domain.AuditAction{domain.AuditImpersonationStarted, domain.AuditImpersonationEnded}

	if len(log) != 2 {
		t.Fatalf("audit log = %+v, want start and end", log)
	}

	for i, e := range log {
		if e.Action != want[i] || e.ActorID != admin.Principal.AccountID || e.TargetID != member.Principal.AccountID {
			t.Errorf("audit event %d = %+v", i, e)
		}
	}

	_, err = f.svc.StopImpersonation(ctx, back.Principal)
	wantIs(t, err, service.ErrNotImpersonating)
}

func TestImpersonationExpires(t *testing.T) {
	t.Parallel()

	f := newAuthFixture(t)
	admin := f.signedUp("admin@example.org", domain.RoleAdmin)
	member := f.signedUp("bea@example.org", domain.RoleMember)

	as, err := f.svc.Impersonate(t.Context(), admin.Principal, member.Principal.AccountID)
	if err != nil {
		t.Fatal(err)
	}

	f.clock.advance(service.ImpersonationMaxAge)

	_, _, err = f.svc.Authenticate(t.Context(), as.Token)
	wantIs(t, err, service.ErrNoSession)
}

func TestImpersonationEndsWithTheAdmin(t *testing.T) {
	t.Parallel()

	for name, change := range map[string]func(*domain.Account){
		"admin disabled": func(a *domain.Account) { a.DisabledAt = a.ConfirmedAt },
		"admin demoted":  func(a *domain.Account) { a.Role = domain.RoleOffice },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newAuthFixture(t)
			admin := f.signedUp("admin@example.org", domain.RoleAdmin)
			member := f.signedUp("bea@example.org", domain.RoleMember)

			as, err := f.svc.Impersonate(t.Context(), admin.Principal, member.Principal.AccountID)
			if err != nil {
				t.Fatal(err)
			}

			f.update("admin@example.org", change)

			_, _, err = f.svc.Authenticate(t.Context(), as.Token)
			wantIs(t, err, service.ErrNoSession)

			_, err = f.svc.StopImpersonation(t.Context(), as.Principal)
			wantIs(t, err, service.ErrNoSession)
		})
	}
}

func TestImpersonationLogoutIsAudited(t *testing.T) {
	t.Parallel()

	f := newAuthFixture(t)
	admin := f.signedUp("admin@example.org", domain.RoleAdmin)
	member := f.signedUp("bea@example.org", domain.RoleMember)

	as, err := f.svc.Impersonate(t.Context(), admin.Principal, member.Principal.AccountID)
	if err != nil {
		t.Fatal(err)
	}

	if err := f.svc.Logout(t.Context(), as.Token); err != nil {
		t.Fatal(err)
	}

	if log := f.auditLog(); len(log) != 2 || log[1].Action != domain.AuditImpersonationEnded {
		t.Fatalf("audit log = %+v, want start and end", log)
	}

	// Signing out of a normal session writes nothing.
	again := f.signedUp("cleo@example.org", domain.RoleMember)
	if err := f.svc.Logout(t.Context(), again.Token); err != nil {
		t.Fatal(err)
	}

	if n := len(f.auditLog()); n != 2 {
		t.Fatalf("%d audit events, want 2", n)
	}
}

func TestImpersonationExpiredLogoutNotAudited(t *testing.T) {
	t.Parallel()

	f := newAuthFixture(t)
	admin := f.signedUp("admin@example.org", domain.RoleAdmin)
	member := f.signedUp("bea@example.org", domain.RoleMember)

	as, err := f.svc.Impersonate(t.Context(), admin.Principal, member.Principal.AccountID)
	if err != nil {
		t.Fatal(err)
	}

	// Signing out hours after it expired (its row not purged yet) does not
	// record a late end.
	f.clock.advance(5 * service.ImpersonationMaxAge)

	if err := f.svc.Logout(t.Context(), as.Token); err != nil {
		t.Fatal(err)
	}

	if log := f.auditLog(); len(log) != 1 || log[0].Action != domain.AuditImpersonationStarted {
		t.Fatalf("audit log = %+v, want only the start", log)
	}
}

func TestImpersonationRejected(t *testing.T) {
	t.Parallel()

	f := newAuthFixture(t)
	ctx := t.Context()
	admin := f.signedUp("admin@example.org", domain.RoleAdmin)
	other := f.signedUp("root@example.org", domain.RoleAdmin)
	office := f.signedUp("office@example.org", domain.RoleOffice)
	member := f.signedUp("bea@example.org", domain.RoleMember)
	disabled := f.signedUp("dora@example.org", domain.RoleMember)
	f.update("dora@example.org", func(a *domain.Account) { a.DisabledAt = a.ConfirmedAt })
	f.register("una@example.org", "correct horse") // unconfirmed
	unconfirmed := f.account("una@example.org")

	unknown := domain.AccountID{0xff}

	tests := []struct {
		name   string
		by     service.Principal
		target domain.AccountID
		want   error
	}{
		{"by office", office.Principal, member.Principal.AccountID, service.ErrNotImpersonable},
		{"by a member", member.Principal, office.Principal.AccountID, service.ErrNotImpersonable},
		{"oneself", admin.Principal, admin.Principal.AccountID, service.ErrNotImpersonable},
		{"another admin", admin.Principal, other.Principal.AccountID, service.ErrNotImpersonable},
		{"disabled", admin.Principal, disabled.Principal.AccountID, service.ErrAccountInactive},
		{"unconfirmed", admin.Principal, unconfirmed.ID, service.ErrAccountInactive},
		{"unknown", admin.Principal, unknown, domain.ErrNotFound},
	}

	for _, tt := range tests {
		_, err := f.svc.Impersonate(ctx, tt.by, tt.target)
		if err == nil {
			t.Fatalf("%s: impersonation succeeded", tt.name)
		}

		wantIs(t, err, tt.want)
	}

	// Nested: an impersonated session cannot impersonate again, even if the
	// target were an admin's account.
	as, err := f.svc.Impersonate(ctx, admin.Principal, office.Principal.AccountID)
	if err != nil {
		t.Fatal(err)
	}

	nested := as.Principal
	nested.Role = domain.RoleAdmin

	_, err = f.svc.Impersonate(ctx, nested, member.Principal.AccountID)
	wantIs(t, err, service.ErrNotImpersonable)

	if log := f.auditLog(); len(log) != 1 {
		t.Fatalf("audit log = %+v, want only the one started impersonation", log)
	}
}

func TestAccounts(t *testing.T) {
	t.Parallel()

	f := newAuthFixture(t)
	f.signedUp("bea@example.org", domain.RoleMember)
	f.signedUp("admin@example.org", domain.RoleAdmin)

	got, err := f.svc.Accounts(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != 2 || got[0].Account.Email != "admin@example.org" || got[1].Account.Email != "bea@example.org" ||
		got[0].Person.ID != got[0].Account.PersonID || got[0].Person.GivenName == "" {
		t.Fatalf("Accounts = %+v", got)
	}
}
