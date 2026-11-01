package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/cmd/studio-schedule/cli"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/mail"
	"github.com/cwbudde/studio-schedule/internal/service"
	"github.com/cwbudde/studio-schedule/internal/store"
	"github.com/cwbudde/studio-schedule/internal/store/memory"
)

// accountsRead runs fn in a read-only transaction of st.
func accountsRead[T any](t *testing.T, st store.Store, fn func(ctx context.Context, tx store.Tx) (T, error)) T {
	t.Helper()

	var out T

	err := st.InTx(t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		var err error
		out, err = fn(ctx, tx)

		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	return out
}

// accountOf returns the account of email in st; ok is false if there is
// none.
func accountOf(t *testing.T, st store.Store, email string) (acc domain.Account, ok bool) {
	t.Helper()

	err := st.InTx(t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		var err error
		acc, err = tx.Accounts().GetByEmail(ctx, email)

		return err
	})

	switch {
	case errors.Is(err, domain.ErrNotFound):
		return domain.Account{}, false
	case err != nil:
		t.Fatal(err)
	}

	return acc, true
}

// accountsMails returns the mails of template in the outbox of st.
func accountsMails(t *testing.T, st store.Store, template string) []domain.OutboxMail {
	t.Helper()

	return accountsRead(t, st, func(ctx context.Context, tx store.Tx) ([]domain.OutboxMail, error) {
		return tx.Outbox().List(ctx, store.OutboxFilter{Template: template, Limit: 100})
	})
}

// accountsSession stores the session n of acc in st, started by
// impersonator unless zero, created at created and valid until expires,
// and returns it.
func accountsSession(
	t *testing.T, st store.Store, acc, impersonator domain.AccountID, n byte, created, expires time.Time,
) domain.Session {
	t.Helper()

	var id domain.SessionID

	id[0], id[6], id[8], id[15] = 0xee, 0x70, 0x80, n

	s := domain.Session{
		ID: id, AccountID: acc, TokenHash: domain.HashToken(fmt.Sprintf("session-%d", n)),
		CreatedAt: created, LastSeenAt: created, ExpiresAt: expires, ImpersonatorID: impersonator,
	}

	err := st.InTx(t.Context(), store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		return tx.Sessions().Create(ctx, s)
	})
	if err != nil {
		t.Fatal(err)
	}

	return s
}

// accountsSessionLive reports whether the session s is still in st.
func accountsSessionLive(t *testing.T, st store.Store, s domain.Session) bool {
	t.Helper()

	err := st.InTx(t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		_, err := tx.Sessions().GetByToken(ctx, s.TokenHash)

		return err
	})

	switch {
	case errors.Is(err, domain.ErrNotFound):
		return false
	case err != nil:
		t.Fatal(err)
	}

	return true
}

// accountsAudit returns the newest audit event of st.
func accountsAudit(t *testing.T, st store.Store) domain.AuditEvent {
	t.Helper()

	events := accountsRead(t, st, func(ctx context.Context, tx store.Tx) ([]domain.AuditEvent, error) {
		return tx.Audit().List(ctx, 1)
	})
	if len(events) == 0 {
		t.Fatal("no audit event")
	}

	return events[0]
}

// wantExit fails unless res exited with code and its stderr contains want.
func wantExit(t *testing.T, what string, res result, code int, want string) {
	t.Helper()

	if res.code != code || !strings.Contains(res.stderr, want) {
		t.Errorf("%s: exit %d, stderr %q; want exit %d with %q", what, res.code, res.stderr, code, want)
	}
}

// wantOK fails unless res exited with 0 and printed nothing on stderr.
func wantOK(t *testing.T, what string, res result) {
	t.Helper()

	if res.code != 0 || res.stderr != "" {
		t.Fatalf("%s: exit %d, stderr %q", what, res.code, res.stderr)
	}
}

// TestAccountsAccess: every command of the group acts as an admin account
// (accounts create as the office), refused before anything runs for an
// unknown --as (exit 3), a role too low (exit 4) and without --as (exit
// 2).
func TestAccountsAccess(t *testing.T) {
	t.Parallel()

	r := cliRun{st: demoStore(t)}

	for _, args := range [][]string{
		{"accounts", "list"},
		{"accounts", "show", demoMember},
		{"accounts", "set-role", demoMember, "office"},
		{"accounts", "disable", "--yes", demoMember},
		{"accounts", "enable", demoMember},
		{"accounts", "reset-password", "--yes", demoMember},
		{"accounts", "sessions", "revoke", "--yes", "--all"},
		{"accounts", "delete", "--yes", demoMember},
	} {
		what := strings.Join(args, " ")
		wantExit(t, what+" as the office", r.exec(t, append(args, "--as", demoOffice)...),
			cli.ExitConflict, "below admin")
		wantExit(t, what+" as nobody", r.exec(t, append(args, "--as", "nobody@example.org")...),
			cli.ExitNotFound, "no account has this email")
		wantExit(t, what+" without --as", r.exec(t, args...), cli.ExitUsage, "give --as")
	}

	create := []string{
		"accounts", "create", "--person", "0190a5d2-ac96-774b-bcce-b302099a8057", "--role", "member",
		"--yes", "new@example.org",
	}
	wantExit(t, "create as an instructor", r.exec(t, append(create, "--as", demoInstructor)...),
		cli.ExitConflict, "below office")

	if acc, ok := accountOf(t, r.st, demoMember); !ok || !acc.DisabledAt.IsZero() {
		t.Error("a refused command changed the member's account")
	}
}

// TestAccountsListShow lists the demo's accounts and shows one, by its
// email in any case and by its ID, as text and as the API's JSON.
func TestAccountsListShow(t *testing.T) {
	t.Parallel()

	stores(t, func(t *testing.T, r cliRun) {
		res := r.exec(t, "accounts", "list", "--as", demoAdmin)
		wantOK(t, "list", res)
		golden(t, "accounts/list.txt", res.stdout)

		res = r.exec(t, "accounts", "list", "--as", demoAdmin, "-o", "json")
		wantOK(t, "list, JSON", res)
		golden(t, "accounts/list.json", res.stdout)

		res = r.exec(t, "accounts", "show", "--as", demoAdmin, strings.ToUpper(demoMember))
		wantOK(t, "show", res)
		golden(t, "accounts/show.txt", res.stdout)

		acc, _ := accountOf(t, storeOf(t, r), demoMember)

		res = r.exec(t, "accounts", "show", "--as", demoAdmin, "-o", "json", acc.ID.String())
		wantOK(t, "show by ID, JSON", res)
		golden(t, "accounts/show.json", res.stdout)

		wantExit(t, "show nobody", r.exec(t, "accounts", "show", "--as", demoAdmin, "nobody@example.org"),
			cli.ExitNotFound, `no account has the email or ID "nobody@example.org"`)
		wantExit(t, "show without an account", r.exec(t, "accounts", "show", "--as", demoAdmin), cli.ExitUsage,
			"accepts 1 arg")
	})
}

// TestAccountsSetRole promotes the office account to admin and back,
// refuses an admin's own account (own-account) and a role an account
// cannot have, and records the change via the command line.
func TestAccountsSetRole(t *testing.T) {
	t.Parallel()

	stores(t, func(t *testing.T, r cliRun) {
		st := storeOf(t, r)

		res := r.exec(t, "accounts", "set-role", "--as", demoAdmin, demoOffice, "admin")
		wantOK(t, "promote", res)
		golden(t, "accounts/set-role.txt", res.stdout)

		if acc, _ := accountOf(t, st, demoOffice); acc.Role != domain.RoleAdmin {
			t.Errorf("role %s, want admin", acc.Role)
		}

		e := accountsAudit(t, st)
		if e.Action != domain.AuditAccountRoleChanged || e.Details.Via.Channel != domain.ChannelCLI ||
			e.Details.Via.OSUser != testOSUser {
			t.Errorf("audit event %+v, want a role change via the command line", e)
		}

		res = r.exec(t, "accounts", "set-role", "--as", demoAdmin, "-o", "json", demoOffice, "office")
		wantOK(t, "demote, JSON", res)
		golden(t, "accounts/set-role.json", res.stdout)

		wantExit(t, "own account", r.exec(t, "accounts", "set-role", "--as", demoAdmin, demoAdmin, "member"),
			cli.ExitConflict, "refused (own-account)")
		wantExit(t, "guest", r.exec(t, "accounts", "set-role", "--as", demoAdmin, demoMember, "guest"),
			cli.ExitUsage, "<role>: invalid account role")
		wantExit(t, "unknown", r.exec(t, "accounts", "set-role", "--as", demoAdmin, "nobody@example.org", "member"),
			cli.ExitNotFound, "no account has the email")

		if acc, _ := accountOf(t, st, demoAdmin); acc.Role != domain.RoleAdmin {
			t.Errorf("the refused demotion changed the admin's role to %s", acc.Role)
		}
	})
}

// TestAccountsDisableEnable disables an account, which ends its sessions,
// only when confirmed, and enables it again.
func TestAccountsDisableEnable(t *testing.T) {
	t.Parallel()

	stores(t, func(t *testing.T, r cliRun) {
		st := storeOf(t, r)
		member, _ := accountOf(t, st, demoMember)
		sess := accountsSession(t, st, member.ID, domain.AccountID{}, 1, testNow, testNow.Add(time.Hour))

		wantExit(t, "disable without --yes", r.exec(t, "accounts", "disable", "--as", demoAdmin, demoMember),
			cli.ExitUsage, "confirm with --yes")

		if acc, _ := accountOf(t, st, demoMember); !acc.DisabledAt.IsZero() || !accountsSessionLive(t, st, sess) {
			t.Fatal("the unconfirmed disable changed the account")
		}

		res := r.exec(t, "accounts", "disable", "--as", demoAdmin, "--yes", demoMember)
		wantOK(t, "disable", res)
		golden(t, "accounts/disable.txt", res.stdout)

		if acc, _ := accountOf(t, st, demoMember); acc.DisabledAt.IsZero() || accountsSessionLive(t, st, sess) {
			t.Error("the account is not disabled, or its session lives")
		}

		res = r.exec(t, "accounts", "enable", "--as", demoAdmin, "-o", "json", demoMember)
		wantOK(t, "enable", res)
		golden(t, "accounts/enable.json", res.stdout)

		if acc, _ := accountOf(t, st, demoMember); !acc.DisabledAt.IsZero() {
			t.Error("the account is still disabled")
		}

		wantExit(t, "own account", r.exec(t, "accounts", "disable", "--as", demoAdmin, "--yes", demoAdmin),
			cli.ExitConflict, "refused (own-account)")
	})
}

// TestAccountsConfirmTerminal asks on a terminal: "y" disables the
// account, anything else changes nothing and exits with 2.
func TestAccountsConfirmTerminal(t *testing.T) {
	t.Parallel()

	st := demoStore(t)

	res := cliRun{st: st, terminal: true, stdin: "n\n"}.exec(t, "accounts", "disable", "--as", demoAdmin, demoMember)
	wantExit(t, "answer n", res, cli.ExitUsage, "not confirmed; nothing was changed")

	if !strings.Contains(res.stderr, "Disable "+demoMember+" and end its sessions? [y/N] ") {
		t.Errorf("stderr %q lacks the question", res.stderr)
	}

	if acc, _ := accountOf(t, st, demoMember); !acc.DisabledAt.IsZero() {
		t.Fatal("answer n disabled the account")
	}

	res = cliRun{st: st, terminal: true, stdin: "y\n"}.exec(t, "accounts", "disable", "--as", demoAdmin, demoMember)
	if res.code != 0 {
		t.Fatalf("answer y: exit %d, stderr %q", res.code, res.stderr)
	}

	if acc, _ := accountOf(t, st, demoMember); acc.DisabledAt.IsZero() {
		t.Error("answer y did not disable the account")
	}
}

// TestAccountsDelete deletes an account only when confirmed, keeps its
// person, refuses an admin's own account (own-account) and an account the
// audit log names as an actor (in-use).
func TestAccountsDelete(t *testing.T) {
	t.Parallel()

	stores(t, func(t *testing.T, r cliRun) {
		st := storeOf(t, r)
		member, _ := accountOf(t, st, demoMember)

		wantExit(t, "without --yes", r.exec(t, "accounts", "delete", "--as", demoAdmin, demoMember),
			cli.ExitUsage, "confirm with --yes")

		if _, ok := accountOf(t, st, demoMember); !ok {
			t.Fatal("the unconfirmed delete deleted the account")
		}

		res := r.exec(t, "accounts", "delete", "--as", demoAdmin, "--yes", "-o", "json", demoMember)
		wantOK(t, "delete", res)
		golden(t, "accounts/delete.json", res.stdout)

		if _, ok := accountOf(t, st, demoMember); ok {
			t.Error("the account is not deleted")
		}

		accountsRead(t, st, func(ctx context.Context, tx store.Tx) (domain.Person, error) {
			return tx.Persons().Get(ctx, member.PersonID) // the person stays
		})

		if e := accountsAudit(t, st); e.Action != domain.AuditAccountDeleted || e.TargetPersonID != member.PersonID {
			t.Errorf("audit event %+v, want the deletion", e)
		}

		wantExit(t, "own account", r.exec(t, "accounts", "delete", "--as", demoAdmin, "--yes", demoAdmin),
			cli.ExitConflict, "refused (own-account)")

		// The office account as an admin acts once; the audit log keeps it.
		wantOK(t, "promote", r.exec(t, "accounts", "set-role", "--as", demoAdmin, demoOffice, "admin"))
		wantOK(t, "act", r.exec(t, "accounts", "set-role", "--as", demoOffice, demoInstructor, "office"))

		res = r.exec(t, "accounts", "delete", "--as", demoAdmin, "--yes", demoOffice)
		wantExit(t, "in use", res, cli.ExitConflict, "refused (in-use)")

		res = r.exec(t, "accounts", "delete", "--as", demoAdmin, "--yes", demoInstructor)
		wantOK(t, "delete the instructor", res)
		golden(t, "accounts/delete.txt", res.stdout)
	})
}

// tokenParam matches the token of a reset link.
var tokenParam = regexp.MustCompile(`token=[^\s"]+`)

// TestAccountsResetPassword prints a reset link that sets the password of
// the account, for the admin who has forgotten theirs, or mails it with
// --mail without printing it; it refuses a disabled account and changes
// nothing without --yes.
func TestAccountsResetPassword(t *testing.T) {
	t.Parallel()

	stores(t, func(t *testing.T, r cliRun) {
		st := storeOf(t, r)
		admin, _ := accountOf(t, st, demoAdmin)

		wantExit(t, "without --yes", r.exec(t, "accounts", "reset-password", "--as", demoAdmin, demoAdmin),
			cli.ExitUsage, "confirm with --yes")

		res := r.exec(t, "accounts", "reset-password", "--as", demoAdmin, "--yes", demoAdmin)
		wantOK(t, "print", res)
		golden(t, "accounts/reset-password.txt", tokenParam.ReplaceAllString(res.stdout, "token=REDACTED"))

		link := tokenParam.FindString(res.stdout)
		if link == "" {
			t.Fatalf("stdout %q has no link", res.stdout)
		}

		token, err := url.QueryUnescape(strings.TrimPrefix(link, "token="))
		if err != nil {
			t.Fatal(err)
		}

		tok := accountsRead(t, st, func(ctx context.Context, tx store.Tx) (domain.AccountToken, error) {
			return tx.AccountTokens().Get(ctx, domain.HashToken(token))
		})
		if tok.AccountID != admin.ID || tok.Purpose != domain.TokenResetPassword ||
			!tok.ExpiresAt.Equal(testNow.Add(service.ResetTokenTTL)) {
			t.Errorf("token %+v, want a reset link of the admin for an hour", tok)
		}

		if n := len(accountsMails(t, st, mail.TemplateResetPassword)); n != 0 {
			t.Errorf("%d reset mails, want none without --mail", n)
		}

		wantResetAudit(t, st, admin.ID, admin.ID, domain.LinkPrinted)

		// JSON; the new link replaces the one before.
		res = r.exec(t, "accounts", "reset-password", "--as", demoAdmin, "--yes", "-o", "json", demoAdmin)
		wantOK(t, "print, JSON", res)
		golden(t, "accounts/reset-password.json", tokenParam.ReplaceAllString(res.stdout, "token=REDACTED"))

		err = st.InTx(t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
			_, err := tx.AccountTokens().Get(ctx, domain.HashToken(token))

			return err
		})
		if !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("the first link: %v, want it replaced", err)
		}

		res = r.exec(t, "accounts", "reset-password", "--as", demoAdmin, "--yes", "--mail", demoMember)
		wantOK(t, "mail", res)
		golden(t, "accounts/reset-password-mail.txt", res.stdout)

		mails := accountsMails(t, st, mail.TemplateResetPassword)
		if len(mails) != 1 || mails[0].To != demoMember || strings.Contains(res.stdout, "token=") {
			t.Errorf("reset mails %+v, stdout %q: want one to the member and no link printed", mails, res.stdout)
		}

		member, _ := accountOf(t, st, demoMember)
		wantResetAudit(t, st, admin.ID, member.ID, domain.LinkMailed)

		wantOK(t, "disable", r.exec(t, "accounts", "disable", "--as", demoAdmin, "--yes", demoMember))
		wantExit(t, "disabled", r.exec(t, "accounts", "reset-password", "--as", demoAdmin, "--yes", demoMember),
			cli.ExitConflict, "the account may not set a password")
	})
}

// wantResetAudit fails unless the newest audit event of st is a reset link
// issued by actor via the command line for target, delivered as delivery.
func wantResetAudit(t *testing.T, st store.Store, actor, target domain.AccountID, delivery domain.LinkDelivery) {
	t.Helper()

	e := accountsAudit(t, st)
	if e.Action != domain.AuditPasswordResetIssued || e.ActorID != actor || e.TargetKind != domain.AuditTargetAccount ||
		e.TargetID != target || e.Details.Delivery != delivery ||
		e.Details.Via != (domain.AuditVia{Channel: domain.ChannelCLI, OSUser: testOSUser}) {
		t.Errorf("audit event %+v, want a reset link for %s %s by %s via the command line", e, target, delivery, actor)
	}
}

// wantRevokeAudit fails unless the newest audit event of st is the end of
// n sessions of target (of every account if zero) by actor via the command
// line.
func wantRevokeAudit(t *testing.T, st store.Store, actor, target domain.AccountID, n int) {
	t.Helper()

	kind := domain.AuditTargetAccount
	if target.IsZero() {
		kind = ""
	}

	e := accountsAudit(t, st)
	if e.Action != domain.AuditSessionsRevoked || e.ActorID != actor || e.TargetKind != kind ||
		e.TargetID != target || e.Details.Sessions != n ||
		e.Details.Via != (domain.AuditVia{Channel: domain.ChannelCLI, OSUser: testOSUser}) {
		t.Errorf("audit event %+v, want %d sessions of %s revoked by %s via the command line", e, n, target, actor)
	}
}

// TestAccountsSessionsRevoke ends the running sessions of one account, and
// of every account with --all, and says how many ended.
func TestAccountsSessionsRevoke(t *testing.T) {
	t.Parallel()

	stores(t, func(t *testing.T, r cliRun) {
		st := storeOf(t, r)
		member, _ := accountOf(t, st, demoMember)
		office, _ := accountOf(t, st, demoOffice)
		admin, _ := accountOf(t, st, demoAdmin)

		live := accountsSession(t, st, member.ID, domain.AccountID{}, 1, testNow, testNow.Add(time.Hour))
		accountsSession(t, st, member.ID, domain.AccountID{}, 2, testNow.Add(-2*time.Hour), testNow.Add(-time.Hour))
		accountsSession(t, st, member.ID, admin.ID, 3, testNow, testNow.Add(time.Hour)) // an impersonation
		other := accountsSession(t, st, office.ID, domain.AccountID{}, 4, testNow, testNow.Add(time.Hour))

		wantExit(t, "without --yes", r.exec(t, "accounts", "sessions", "revoke", "--as", demoAdmin, demoMember),
			cli.ExitUsage, "confirm with --yes")

		wantExit(t, "--all without --yes", r.exec(t, "accounts", "sessions", "revoke", "--as", demoAdmin, "--all"),
			cli.ExitUsage, "confirm with --yes")

		if !accountsSessionLive(t, st, live) || !accountsSessionLive(t, st, other) {
			t.Fatal("the unconfirmed revoke ended a session")
		}

		for _, args := range [][]string{{}, {"--all", demoMember}} {
			res := r.exec(t, append([]string{"accounts", "sessions", "revoke", "--as", demoAdmin, "--yes"}, args...)...)
			wantExit(t, fmt.Sprintf("revoke %q", args), res, cli.ExitUsage,
				"name one account (<email|id>) or give --all")
		}

		res := r.exec(t, "accounts", "sessions", "revoke", "--as", demoAdmin, "--yes", demoMember)
		wantOK(t, "revoke", res)
		golden(t, "accounts/revoke.txt", res.stdout) // two: the expired one does not count

		if accountsSessionLive(t, st, live) || !accountsSessionLive(t, st, other) {
			t.Error("the member's session lives, or the office's ended")
		}

		// The revoke is audited after the impersonation it ended.
		wantRevokeAudit(t, st, admin.ID, member.ID, 2)

		events := accountsRead(t, st, func(ctx context.Context, tx store.Tx) ([]domain.AuditEvent, error) {
			return tx.Audit().List(ctx, 2)
		})
		if e := events[len(events)-1]; len(events) != 2 || e.Action != domain.AuditImpersonationEnded ||
			e.ActorID != admin.ID || e.TargetID != member.ID {
			t.Errorf("audit event %+v, want the impersonation ended", e)
		}

		res = r.exec(t, "accounts", "sessions", "revoke", "--as", demoAdmin, "--yes", "--all")
		wantOK(t, "revoke --all", res)

		if !strings.HasPrefix(res.stdout, "Ended 1 session of ") || accountsSessionLive(t, st, other) {
			t.Errorf("revoke --all: %q; want the office's session ended", res.stdout)
		}

		wantRevokeAudit(t, st, admin.ID, domain.AccountID{}, 1)

		res = r.exec(t, "accounts", "sessions", "revoke", "--as", demoAdmin, "--yes", "--all", "-o", "json")
		wantOK(t, "revoke --all again", res)

		var body struct {
			SessionsEnded int `json:"sessionsEnded"`
			Accounts      int `json:"accounts"`
		}
		if err := json.Unmarshal([]byte(res.stdout), &body); err != nil || body.SessionsEnded != 0 ||
			body.Accounts < 4 {
			t.Errorf("revoke --all again: %q, %v; want no session ended, of every account", res.stdout, err)
		}

		wantRevokeAudit(t, st, admin.ID, domain.AccountID{}, 0)
	})
}

// accountlessPerson returns a person of st without an account: the first
// by ID.
func accountlessPerson(t *testing.T, st store.Store) domain.Person {
	t.Helper()

	persons := accountsRead(t, st, func(ctx context.Context, tx store.Tx) ([]domain.Person, error) {
		persons, err := tx.Persons().List(ctx)
		if err != nil {
			return nil, err
		}

		var out []domain.Person

		for _, p := range persons {
			switch _, err := tx.Accounts().GetByPerson(ctx, p.ID); {
			case errors.Is(err, domain.ErrNotFound):
				out = append(out, p)
			case err != nil:
				return nil, err
			}
		}

		return out, nil
	})

	slices.SortFunc(persons, func(a, b domain.Person) int { return strings.Compare(a.ID.String(), b.ID.String()) })

	if len(persons) == 0 {
		t.Fatal("every person has an account")
	}

	return persons[0]
}

// TestAccountsCreate creates an account for a person as the office, which
// mails an invitation, and refuses an admin account for the office, a
// person with an account, a taken email and invalid input.
func TestAccountsCreate(t *testing.T) {
	t.Parallel()

	stores(t, func(t *testing.T, r cliRun) {
		st := storeOf(t, r)
		person := accountlessPerson(t, st)
		member, _ := accountOf(t, st, demoMember)
		create := func(args ...string) []string {
			return append([]string{"accounts", "create", "--as", demoOffice}, args...)
		}

		wantExit(t, "without --yes", r.exec(t, create("--person", person.ID.String(), "--role", "member",
			"new.member@example.org")...), cli.ExitUsage, "confirm with --yes")

		if _, ok := accountOf(t, st, "new.member@example.org"); ok {
			t.Fatal("the unconfirmed create created the account")
		}

		wantExit(t, "admin by the office", r.exec(t, create("--yes", "--person", person.ID.String(), "--role",
			"admin", "new.member@example.org")...), cli.ExitConflict, "only an admin may create admin accounts")

		res := r.exec(t, create("--yes", "--person", person.ID.String(), "--role", "member", "-o", "json",
			"new.member@example.org")...)
		wantOK(t, "create", res)
		golden(t, "accounts/create.json", res.stdout)

		acc, ok := accountOf(t, st, "new.member@example.org")
		if !ok || acc.PersonID != person.ID || acc.Role != domain.RoleMember || !acc.ConfirmedAt.IsZero() {
			t.Errorf("account %+v, want a pending member account of the person", acc)
		}

		if mails := accountsMails(t, st, mail.TemplateInvite); len(mails) != 1 ||
			mails[0].To != "new.member@example.org" {
			t.Errorf("invitations %+v, want one", mails)
		}

		next := accountlessPerson(t, st).ID.String()

		for what, c := range map[string]struct {
			args []string
			code int
			want string
		}{
			"has an account": {
				[]string{"--person", person.ID.String(), "--role", "member", "other@example.org"},
				cli.ExitConflict, "the person has an account already",
			},
			"taken email": {
				[]string{"--person", next, "--role", "member", demoMember},
				cli.ExitConflict, "the email address belongs to another account",
			},
			"bad email": {
				[]string{"--person", next, "--role", "member", "not-an-email"},
				cli.ExitUsage, "<email>: invalid account email",
			},
			"bad role": {
				[]string{"--person", next, "--role", "guest", "x@example.org"},
				cli.ExitUsage, "--role: invalid account role",
			},
			"bad person": {
				[]string{"--person", "Clara", "--role", "member", "x@example.org"},
				cli.ExitUsage, `--person: "Clara" is not an ID`,
			},
			"unknown person": {
				[]string{"--person", "0190a5d2-ac96-774b-bcce-b302099a8057", "--role", "member", "x@example.org"},
				cli.ExitNotFound, "not found",
			},
			"no role": {
				[]string{"--person", member.PersonID.String(), "x@example.org"},
				cli.ExitUsage, `required flag(s) "role" not set`,
			},
		} {
			wantExit(t, what, r.exec(t, create(append([]string{"--yes"}, c.args...)...)...), c.code, c.want)
		}

		res = r.exec(t, "accounts", "create", "--as", demoAdmin, "--yes", "--person", next, "--role", "admin",
			"new.admin@example.org")
		wantOK(t, "admin by an admin", res)
		golden(t, "accounts/create.txt", res.stdout)
	})
}

// TestAccountGuard names each guard of the account administration and
// keeps the error, so its exit status stays 4; other errors pass
// unchanged. The last-admin guard cannot be reached from the command line
// alone (--as needs an admin who can sign in, and their own account is
// own-account first), but a change through the API at the same time can
// make it the answer.
func TestAccountGuard(t *testing.T) {
	t.Parallel()

	for err, want := range map[error]string{
		service.ErrOwnAccount:       "refused (own-account): ",
		service.ErrLastAdmin:        "refused (last-admin): ",
		service.ErrAccountKept:      "refused (in-use): ",
		service.ErrAdminExists:      "refused (admin-exists): ",
		service.ErrAccountConfirmed: "refused (confirmed): ",
	} {
		got := accountGuard(fmt.Errorf("delete account: %w", err))
		if !strings.HasPrefix(got.Error(), want) || !errors.Is(got, err) || !errors.Is(got, domain.ErrConflict) {
			t.Errorf("accountGuard(%v) = %v", err, got)
		}
	}

	other := errors.New("other")
	if got := accountGuard(other); got != other { //nolint:errorlint // the same error
		t.Errorf("accountGuard(other) = %v", got)
	}
}

// accountsFailing is a store whose transaction number fail (counted from
// 1) fails, as a database that goes away during a command.
type accountsFailing struct {
	*memory.Store

	fail int
	n    *int
}

var errAccountsGone = errors.New("the database went away")

func (s accountsFailing) InTx(ctx context.Context, mode store.TxMode, fn func(context.Context, store.Tx) error) error {
	if *s.n++; *s.n == s.fail {
		return errAccountsGone
	}

	return s.Store.InTx(ctx, mode, fn)
}

// TestAccountsStoreFailure: a database failure in any of a command's
// service calls (after the school's settings and the --as account) fails
// the command with exit 1 and its message.
func TestAccountsStoreFailure(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		args []string
		fail int
	}{
		{[]string{"list"}, 3},
		{[]string{"show", demoMember}, 3},
		{[]string{"set-role", demoMember, "office"}, 4},
		{[]string{"disable", "--yes", demoMember}, 4},
		{[]string{"enable", demoMember}, 3},
		{[]string{"reset-password", "--yes", demoMember}, 4},
		{[]string{"reset-password", "--yes", demoMember}, 3},
		{[]string{"sessions", "revoke", "--yes", demoMember}, 3},
		{[]string{"sessions", "revoke", "--yes", demoMember}, 4},
		{[]string{"sessions", "revoke", "--yes", "--all"}, 3},
		{[]string{"delete", "--yes", demoMember}, 3},
		{[]string{"delete", "--yes", demoMember}, 4},
		{[]string{"set-role", demoMember, "office"}, 3},
		{[]string{"disable", "--yes", demoMember}, 3},
	} {
		st := accountsFailing{Store: demoStore(t), fail: c.fail, n: new(int)}
		args := append([]string{"accounts", "--as", demoAdmin}, c.args...)
		wantExit(t, fmt.Sprintf("%v, transaction %d failing", c.args, c.fail),
			cliRun{st: st}.exec(t, args...), cli.ExitFailed, errAccountsGone.Error())
	}
}
