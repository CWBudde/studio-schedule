package servicetest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/auth/password"
	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/mail"
	"github.com/cwbudde/studio-schedule/internal/service"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// cli is the channel of the scenarios' command-line principals.
var cli = domain.AuditVia{Channel: domain.ChannelCLI, OSUser: "operator"}

// Audit runs the scenarios of the command line's actor (ADR 0020, "The
// actor") on fresh stores from newStore: Actor finds an account by its
// email, every audit event written for a principal records the
// principal's channel, which the audit log returns, and an operator's setup
// link is audited with the operator's channel and no actor.
func Audit(t *testing.T, newStore func(t *testing.T) store.Store) {
	t.Helper()

	for name, fn := range map[string]func(t *testing.T, w *world){
		"Actor":     actor,
		"AuditVia":  auditVia,
		"SetupLink": setupLink,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fn(t, newWorld(t, newStore(t)))
		})
	}
}

// actor: Actor ignores the email's case and surrounding spaces, and refuses
// an unknown, unconfirmed or disabled account.
func actor(t *testing.T, w *world) {
	ctx := t.Context()
	f := newAdminFixture(w)
	svc := w.auth()

	p, err := svc.Actor(ctx, "  ADA@Example.ORG ", cli)
	if err != nil {
		t.Fatal(err)
	}

	want := f.admin
	want.Via = cli

	if p != want {
		t.Errorf("Actor = %+v, want %+v", p, want)
	}

	unconfirmed := w.account(w.person("Uma", "Pending", ""), "uma@example.org")
	unconfirmed.ConfirmedAt = time.Time{}
	w.write(func(ctx context.Context, tx store.Tx) error { return tx.Accounts().Update(ctx, unconfirmed) })

	if _, err := svc.SetAccountDisabled(ctx, f.admin, f.member.ID, true); err != nil {
		t.Fatal(err)
	}

	for email, want := range map[string]error{
		"nobody@example.org": service.ErrNoActor,
		"uma@example.org":    service.ErrActorInactive,
		"mia@example.org":    service.ErrActorInactive,
	} {
		if _, err := svc.Actor(ctx, email, cli); !errors.Is(err, want) || !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("Actor(%s): %v, want %v", email, err, want)
		}
	}
}

// auditVia runs every operation that writes an audit event once for a
// principal of the command line and once for one of the API, and checks
// the channel of each event, as stored and as AuditLog returns it. The
// impersonations a change ends are recorded without a channel: their actor
// is the impersonator, who did not act through the command line.
func auditVia(t *testing.T, w *world) {
	ctx := t.Context()
	f := newAdminFixture(w)
	svc := w.auth()
	persons := w.persons()
	mails := service.NewMailLogService(w.st, clock.NewFixed(now), w.seq)

	cliAdmin, err := svc.Actor(ctx, f.admin.Email, cli)
	if err != nil {
		t.Fatal(err)
	}

	cliOffice, err := svc.Actor(ctx, w.office.Email, cli)
	if err != nil {
		t.Fatal(err)
	}

	admins := map[bool]service.Principal{true: cliAdmin, false: f.admin}
	office := map[bool]service.Principal{true: cliOffice, false: w.office}
	want := map[domain.AuditEventID]domain.AuditVia{}

	// expect records the channel of the newest event, which must be action.
	expect := func(action domain.AuditAction, via domain.AuditVia) {
		t.Helper()

		e := w.audit()[0]
		if e.Action != action || e.Details.Via != via {
			t.Fatalf("newest event %+v, want %s via %+v", e, action, via)
		}

		want[e.ID] = via
	}
	viaOf := func(viaCLI bool) domain.AuditVia { return map[bool]domain.AuditVia{true: cli}[viaCLI] }

	for _, viaCLI := range []bool{true, false} {
		via := viaOf(viaCLI)

		if _, err := svc.SetAccountDisabled(ctx, admins[viaCLI], f.member.ID, true); err != nil {
			t.Fatal(err)
		}

		expect(domain.AuditAccountDisabled, via)

		if _, err := svc.SetAccountDisabled(ctx, admins[viaCLI], f.member.ID, false); err != nil {
			t.Fatal(err)
		}

		expect(domain.AuditAccountEnabled, via)

		role := map[bool]domain.Role{true: domain.RoleInstructor, false: domain.RoleMember}[viaCLI]
		if _, err := svc.SetAccountRole(ctx, admins[viaCLI], f.member.ID, role); err != nil {
			t.Fatal(err)
		}

		expect(domain.AuditAccountRoleChanged, via)

		failed := domain.OutboxMail{
			ID: newID[domain.OutboxMailID](w), To: "mia@example.org", Subject: "Your receipt", Text: "Attached.",
			Template: mail.TemplateReceipt, CreatedAt: now.Add(-time.Hour), Attempts: 5, LastError: "refused",
		}
		w.write(func(ctx context.Context, tx store.Tx) error { return tx.Outbox().Create(ctx, failed) })

		if _, err := mails.Requeue(ctx, admins[viaCLI], failed.ID); err != nil {
			t.Fatal(err)
		}

		expect(domain.AuditMailRequeued, via)

		if _, err := persons.Anonymize(ctx, office[viaCLI], w.person("Ann", "Gone", "").ID); err != nil {
			t.Fatal(err)
		}

		expect(domain.AuditPersonAnonymized, via)

		survivor, merged := w.person("Sam", "Twice", ""), w.person("Sam", "Twice", "")

		_, err := persons.Merge(ctx, office[viaCLI], service.MergeRequest{SurvivorID: survivor.ID, MergedID: merged.ID}, true)
		if err != nil {
			t.Fatal(err)
		}

		expect(domain.AuditPersonMerged, via)

		// An admin signed in through the API impersonates the account the
		// principal then deletes: the impersonation ends without a channel.
		doomed := w.account(w.person("Dora", "Doomed", ""),
			map[bool]string{true: "dora.cli@example.org", false: "dora@example.org"}[viaCLI])

		imp, err := svc.Impersonate(ctx, withSession(f.other, w.session(f.other.AccountID)), doomed.ID)
		if err != nil {
			t.Fatal(err)
		}

		expect(domain.AuditImpersonationStarted, domain.AuditVia{})

		if err := svc.DeleteAccount(ctx, admins[viaCLI], doomed.ID); err != nil {
			t.Fatal(err)
		}

		expect(domain.AuditAccountDeleted, via)

		ended := w.audit()[1]
		if ended.Action != domain.AuditImpersonationEnded || ended.ActorID != f.other.AccountID ||
			!ended.Details.Via.IsZero() {
			t.Fatalf("the ended impersonation %+v, want it without a channel", ended)
		}

		want[ended.ID] = domain.AuditVia{}

		if _, _, err := svc.Authenticate(ctx, imp.Token); !errors.Is(err, service.ErrNoSession) {
			t.Errorf("the impersonation of the deleted account: %v", err)
		}

		// The admin impersonates and returns through the principal's
		// channel (which only a principal with a session can).
		// Each start ends the admin's session; each gets a new one.
		admin := func() service.Principal { return withSession(admins[viaCLI], w.session(f.admin.AccountID)) }

		imp, err = svc.Impersonate(ctx, admin(), f.member.ID)
		if err != nil {
			t.Fatal(err)
		}

		expect(domain.AuditImpersonationStarted, via)

		impersonating := imp.Principal
		impersonating.Via = via

		if _, err := svc.StopImpersonation(ctx, impersonating); err != nil {
			t.Fatal(err)
		}

		expect(domain.AuditImpersonationEnded, via)

		// A sign-out has no principal: it ends an impersonation without a
		// channel.
		if imp, err = svc.Impersonate(ctx, admin(), f.member.ID); err != nil {
			t.Fatal(err)
		}

		expect(domain.AuditImpersonationStarted, via)

		if err := svc.Logout(ctx, imp.Token); err != nil {
			t.Fatal(err)
		}

		expect(domain.AuditImpersonationEnded, domain.AuditVia{})
	}

	// Deleting one's own account records the holder's channel.
	hash, err := password.NewWithParams(password.Params{Memory: 8, Time: 1, Threads: 1, SaltLen: 16, KeyLen: 32}, nil)
	if err != nil {
		t.Fatal(err)
	}

	for _, viaCLI := range []bool{true, false} {
		person := w.person("Lea", "Leaving", "")
		acc := w.account(person, map[bool]string{true: "lea.cli@example.org", false: "lea@example.org"}[viaCLI])
		w.setPassword(hash, acc.ID)

		holder := service.Principal{
			AccountID: acc.ID, PersonID: person.ID, Email: acc.Email, Role: acc.Role, Via: viaOf(viaCLI),
		}
		if err := svc.DeleteOwnAccount(ctx, holder, "correct horse", "en"); err != nil {
			t.Fatal(err)
		}

		expect(domain.AuditAccountDeleted, viaOf(viaCLI))
	}

	log, err := svc.AuditLog(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if len(log) != len(want) {
		t.Fatalf("audit log of %d events, want %d", len(log), len(want))
	}

	for _, e := range log {
		if v, ok := want[e.Event.ID]; !ok || e.Event.Details.Via != v {
			t.Errorf("audit log event %+v, want via %+v", e.Event, v)
		}
	}
}

// session stores a session of the account id and returns it.
func (w *world) session(id domain.AccountID) domain.Session {
	w.t.Helper()

	s := domain.Session{
		ID: newID[domain.SessionID](w), AccountID: id,
		CreatedAt: now.Add(-time.Hour), LastSeenAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour),
	}
	s.TokenHash = domain.HashToken(s.ID.String())
	w.write(func(ctx context.Context, tx store.Tx) error { return tx.Sessions().Create(ctx, s) })

	return s
}

// withSession returns p signed in with sess.
func withSession(p service.Principal, sess domain.Session) service.Principal {
	p.SessionID = sess.ID

	return p
}
