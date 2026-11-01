package servicetest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/service"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// setupLink: IssueSetupLink is the way back in for a first admin whose init
// link expired before they confirmed (ADR 0020, amendment). While no admin
// can sign in, it returns a new link of InviteTokenTTL for an invited
// account, replacing its pending one, and audits it without an actor with
// the operator's channel; the link confirms the account. It refuses a
// confirmed, disabled or registered account and an unknown one, and, once
// an admin can sign in, anything (admin-exists), changing nothing.
func setupLink(t *testing.T, w *world) {
	ctx := t.Context()
	svc := w.auth()

	// The first admin as init left them: invited, with a link that expired.
	invited := func(given, email string, role domain.Role) domain.Account {
		acc := w.account(w.person(given, "Example", email), email)
		acc.Role, acc.ConfirmedAt = role, time.Time{}
		w.write(func(ctx context.Context, tx store.Tx) error { return tx.Accounts().Update(ctx, acc) })

		return acc
	}
	admin := invited("Fia", "fia.first@example.org", domain.RoleAdmin)
	w.write(func(ctx context.Context, tx store.Tx) error {
		return tx.AccountTokens().Create(ctx, domain.AccountToken{
			Hash: domain.HashToken("init link"), AccountID: admin.ID, Purpose: domain.TokenResetPassword,
			Email: admin.Email, CreatedAt: now.Add(-100 * time.Hour), ExpiresAt: now.Add(-28 * time.Hour),
		})
	})

	if err := svc.ResetPassword(ctx, "init link", "a fictional new secret"); !errors.Is(err, service.ErrInvalidToken) {
		t.Fatalf("the expired init link: %v, want it invalid", err)
	}

	if _, err := svc.Actor(ctx, admin.Email, cli); !errors.Is(err, service.ErrActorInactive) {
		t.Fatalf("--as the first admin: %v, want inactive", err)
	}

	// The refusals while no admin can sign in. A disabled admin is none.
	disabledAdmin := w.account(w.person("Dan", "Disabled", ""), "dan@example.org")
	disabledAdmin.Role, disabledAdmin.DisabledAt = domain.RoleAdmin, now.Add(-time.Hour)
	disabled := invited("Dee", "dee@example.org", domain.RoleMember)
	disabled.DisabledAt = now.Add(-time.Hour)
	registered := invited("Rea", "rea@example.org", domain.RoleMember)
	w.write(func(ctx context.Context, tx store.Tx) error {
		for _, acc := range []domain.Account{disabledAdmin, disabled} {
			if err := tx.Accounts().Update(ctx, acc); err != nil {
				return err
			}
		}

		return tx.Credentials().Put(ctx, domain.Credential{AccountID: registered.ID, PasswordHash: "x", ChangedAt: now})
	})

	events := len(w.audit())

	for ref, want := range map[string]error{
		w.office.Email:            service.ErrAccountConfirmed,
		disabledAdmin.ID.String(): service.ErrAccountConfirmed,
		disabled.Email:            service.ErrNoPasswordReset,
		registered.Email:          service.ErrNoPasswordReset,
		"nobody@example.org":      domain.ErrNotFound,
	} {
		if _, err := svc.IssueSetupLink(ctx, ref, cli); !errors.Is(err, want) {
			t.Errorf("IssueSetupLink(%s): %v, want %v", ref, err, want)
		}
	}

	if _, err := svc.IssueSetupLink(ctx, admin.Email, domain.AuditVia{}); !errors.Is(err, domain.ErrValidation) {
		t.Errorf("without the operator's channel: %v, want a validation error", err)
	}

	if n := len(w.audit()); n != events {
		t.Errorf("%d audit events after the refusals, want %d", n, events)
	}

	// The link, twice: the second replaces the first.
	first, err := svc.IssueSetupLink(ctx, "  FIA.First@example.org ", cli)
	if err != nil {
		t.Fatal(err)
	}

	second, err := svc.IssueSetupLink(ctx, admin.ID.String(), cli)
	if err != nil {
		t.Fatal(err)
	}

	if second.Mailed || !second.ExpiresAt.Equal(now.Add(service.InviteTokenTTL)) ||
		second.Account.Account.ID != admin.ID || second.Account.Person.GivenName != "Fia" {
		t.Errorf("setup link %+v, want Fia's for %s", second, service.InviteTokenTTL)
	}

	log, err := svc.AuditLog(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if e := log[0]; e.Event.Action != domain.AuditSetupLinkIssued || !e.Event.ActorID.IsZero() ||
		e.Event.TargetKind != domain.AuditTargetAccount || e.Target.Account.ID != admin.ID ||
		e.Event.Details != (domain.AuditDetails{Delivery: domain.LinkPrinted, Via: cli}) {
		t.Errorf("newest audit entry %+v, want the setup link printed by the operator", e)
	}

	if err := svc.ResetPassword(ctx, tokenOf(t, first.Link), "a fictional new secret"); !errors.Is(err,
		service.ErrInvalidToken) {
		t.Errorf("the replaced link: %v, want it invalid", err)
	}

	if err := svc.ResetPassword(ctx, tokenOf(t, second.Link), "a fictional new secret"); err != nil {
		t.Fatalf("the setup link: %v", err)
	}

	if p, err := svc.Actor(ctx, admin.Email, cli); err != nil || p.Role != domain.RoleAdmin {
		t.Errorf("--as the confirmed admin: %+v, %v", p, err)
	}

	// Now an admin can sign in: refused before anything else, also for an
	// unknown account, and nothing changes.
	other := invited("Ola", "ola@example.org", domain.RoleAdmin)
	events = len(w.audit())

	for _, ref := range []string{other.Email, "nobody@example.org"} {
		if _, err := svc.IssueSetupLink(ctx, ref, cli); !errors.Is(err, service.ErrAdminExists) {
			t.Errorf("IssueSetupLink(%s) with an admin: %v, want admin-exists", ref, err)
		}
	}

	if n := len(w.audit()); n != events {
		t.Errorf("%d audit events after admin-exists, want %d", n, events)
	}
}
