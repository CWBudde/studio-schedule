package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/cmd/studio-schedule/cli"
	"github.com/cwbudde/studio-schedule/internal/app"
	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/service"
	"github.com/cwbudde/studio-schedule/internal/store"
	"github.com/cwbudde/studio-schedule/internal/store/memory"
)

// setupAdmin is the first admin of the schools setupInit sets up.
const setupAdmin = "first.admin@example.org"

// setupPassword is the password the tests set with a setup link.
const setupPassword = "a fictional new secret"

// setupStores runs fn on an empty memory store and on an empty migrated
// PostgreSQL database, without the demo data: the cliRun and the store it
// opens.
func setupStores(t *testing.T, fn func(t *testing.T, r cliRun, st store.Store)) {
	t.Helper()

	t.Run("memory", func(t *testing.T) {
		t.Parallel()

		st := memory.New()
		fn(t, cliRun{st: st}, st)
	})
	t.Run("postgres", func(t *testing.T) {
		t.Parallel()

		databaseURL := newTestDatabase(t)
		fn(t, cliRun{env: map[string]string{"DATABASE_URL": databaseURL}}, openTestStore(t, databaseURL))
	})
}

// setupInit sets a school up in st as init does, 100 hours before testNow:
// by the runs' time its first admin's 72-hour setup link has expired.
func setupInit(t *testing.T, st store.Store) service.SetupResult {
	t.Helper()

	res, err := service.NewSetupService(st, clock.NewFixed(testNow.Add(-100*time.Hour)), idsOf(t),
		"http://localhost:8080/").Init(t.Context(), service.SetupData{
		SchoolName: "Fictional Dance Hall", Timezone: "Europe/Vienna", Locale: "en", Currency: "EUR",
		AdminEmail: setupAdmin, AdminGivenName: "Ada", AdminFamilyName: "Example",
	})
	if err != nil {
		t.Fatal(err)
	}

	return res
}

// setupAuth is the AuthService on st at the runs' time.
func setupAuth(t *testing.T, st store.Store) *service.AuthService {
	t.Helper()

	deps := app.Deps{Store: st, Clock: clock.NewFixed(testNow), IDs: idsOf(t), AppURL: "http://localhost:8080/"}

	return app.NewServices(deps).Auth
}

// linkToken returns the token of the first link in s.
func linkToken(t *testing.T, s string) string {
	t.Helper()

	param := tokenParam.FindString(s)
	if param == "" {
		t.Fatalf("%q has no link", s)
	}

	token, err := url.QueryUnescape(strings.TrimPrefix(param, "token="))
	if err != nil {
		t.Fatal(err)
	}

	return token
}

// tokenStored reports whether st has the account token of token.
func tokenStored(t *testing.T, st store.Store, token string) (domain.AccountToken, bool) {
	t.Helper()

	var tok domain.AccountToken

	err := st.InTx(t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		var err error
		tok, err = tx.AccountTokens().Get(ctx, domain.HashToken(token))

		return err
	})

	switch {
	case errors.Is(err, domain.ErrNotFound):
		return domain.AccountToken{}, false
	case err != nil:
		t.Fatal(err)
	}

	return tok, true
}

// auditEvents returns how many audit events st has, up to 100.
func auditEvents(t *testing.T, st store.Store) int {
	t.Helper()

	return len(accountsRead(t, st, func(ctx context.Context, tx store.Tx) ([]domain.AuditEvent, error) {
		return tx.Audit().List(ctx, 100)
	}))
}

// setupAccount stores an account of email with the role in st, for a new
// person, confirmed and disabled at the times unless zero.
func setupAccount(t *testing.T, st store.Store, email string, role domain.Role, confirmed, disabled time.Time) {
	t.Helper()

	err := st.InTx(t.Context(), store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		p := domain.Person{ID: ids.New[domain.PersonID](idsOf(t)), GivenName: "Ben", FamilyName: "Example"}
		if err := tx.Persons().Create(ctx, p); err != nil {
			return err
		}

		return tx.Accounts().Create(ctx, domain.Account{
			ID: ids.New[domain.AccountID](idsOf(t)), PersonID: p.ID, Email: email, Role: role, Locale: "en",
			ConfirmedAt: confirmed, DisabledAt: disabled,
		})
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestAccountsSetupLink is the lockout of a first admin whose init link
// expired, fixed end to end: --as finds no admin, the old link fails, the
// operator prints a new link, which confirms the account, and then the
// admin acts; the setup link is audited without an actor, with the OS
// user. Once the admin can sign in, setup-link is refused.
func TestAccountsSetupLink(t *testing.T) {
	t.Parallel()

	setupStores(t, func(t *testing.T, r cliRun, st store.Store) {
		initRes := setupInit(t, st)
		oldToken := linkToken(t, initRes.Link)
		auth := setupAuth(t, st)

		wantExit(t, "--as the unconfirmed admin", r.exec(t, "accounts", "list", "--as", setupAdmin),
			cli.ExitNotFound, "--as "+setupAdmin+": not found: the account is not confirmed")

		if err := auth.ResetPassword(t.Context(), oldToken, setupPassword); !errors.Is(err, service.ErrInvalidToken) {
			t.Fatalf("the expired init link: %v, want ErrInvalidToken", err)
		}

		// Unconfirmed, and as an operator command without --as.
		wantExit(t, "without --yes", r.exec(t, "accounts", "setup-link", setupAdmin), cli.ExitUsage,
			"confirm with --yes")
		wantExit(t, "--as", r.exec(t, "accounts", "setup-link", "--as", setupAdmin, "--yes", setupAdmin),
			cli.ExitUsage, "--as: studio-schedule accounts setup-link acts as no account")

		if _, ok := tokenStored(t, st, oldToken); !ok || auditEvents(t, st) != 0 {
			t.Fatal("a refused setup-link replaced the init link or wrote an audit event")
		}

		res := r.exec(t, "accounts", "setup-link", "--yes", setupAdmin)
		wantOK(t, "setup-link", res)
		golden(t, "accounts/setup-link.txt", tokenParam.ReplaceAllString(res.stdout, "token=REDACTED"))

		textToken := linkToken(t, res.stdout)

		tok, ok := tokenStored(t, st, textToken)
		if !ok || tok.AccountID != initRes.Account.ID || tok.Purpose != domain.TokenResetPassword ||
			!tok.ExpiresAt.Equal(testNow.Add(service.InviteTokenTTL)) {
			t.Errorf("token %+v (stored %v), want a setup link of the admin for 72 hours", tok, ok)
		}

		if _, ok := tokenStored(t, st, oldToken); ok {
			t.Error("the init link was not replaced")
		}

		e := accountsAudit(t, st)
		if e.Action != domain.AuditSetupLinkIssued || !e.ActorID.IsZero() || e.TargetKind != domain.AuditTargetAccount ||
			e.TargetID != initRes.Account.ID || e.Details.Delivery != domain.LinkPrinted ||
			e.Details.Via != (domain.AuditVia{Channel: domain.ChannelCLI, OSUser: testOSUser}) {
			t.Errorf("audit event %+v, want the setup link printed by the operator for the admin", e)
		}

		// JSON, by the account's ID; the new link replaces the one before.
		res = r.exec(t, "accounts", "setup-link", "--yes", "-o", "json", initRes.Account.ID.String())
		wantOK(t, "setup-link, JSON", res)
		golden(t, "accounts/setup-link.json", tokenParam.ReplaceAllString(res.stdout, "token=REDACTED"))

		var body struct {
			Link   string `json:"link"`
			Mailed bool   `json:"mailed"`
		}
		if err := json.Unmarshal([]byte(res.stdout), &body); err != nil || body.Mailed {
			t.Fatalf("JSON %q: %v; want a link not mailed", res.stdout, err)
		}

		if _, ok := tokenStored(t, st, textToken); ok {
			t.Error("the first setup link was not replaced")
		}

		if err := auth.ResetPassword(t.Context(), linkToken(t, body.Link), setupPassword); err != nil {
			t.Fatalf("the setup link: %v", err)
		}

		if acc, _ := accountOf(t, st, setupAdmin); acc.ConfirmedAt.IsZero() {
			t.Error("the setup link did not confirm the account")
		}

		wantOK(t, "--as the admin", r.exec(t, "accounts", "list", "--as", setupAdmin))

		before := accountsAudit(t, st)

		wantExit(t, "the admin can sign in", r.exec(t, "accounts", "setup-link", "--yes", setupAdmin),
			cli.ExitConflict, "refused (admin-exists)")

		if e := accountsAudit(t, st); e.ID != before.ID {
			t.Errorf("the refused setup-link wrote the audit event %+v", e)
		}
	})
}

// TestAccountsSetupLinkAdminExists: while an admin can sign in, as in the
// demo, setup-link is refused (admin-exists, before any other check) and
// changes nothing: the account's pending link works, and nothing is
// audited.
func TestAccountsSetupLinkAdminExists(t *testing.T) {
	t.Parallel()

	stores(t, func(t *testing.T, r cliRun) {
		st := storeOf(t, r)

		res := r.exec(t, "accounts", "reset-password", "--as", demoAdmin, "--yes", demoMember)
		wantOK(t, "reset-password", res)

		token := linkToken(t, res.stdout)
		before := accountsAudit(t, st)

		// The member is confirmed too, but the admin is checked first.
		wantExit(t, "setup-link", r.exec(t, "accounts", "setup-link", "--yes", demoMember), cli.ExitConflict,
			"refused (admin-exists): setup link: conflict: an admin can sign in")

		if _, ok := tokenStored(t, st, token); !ok {
			t.Error("the refused setup-link replaced the member's reset link")
		}

		if e := accountsAudit(t, st); e.ID != before.ID {
			t.Errorf("the refused setup-link wrote the audit event %+v", e)
		}
	})
}

// TestAccountsSetupLinkRefusals: while no admin can sign in (the first
// admin has not confirmed, another is disabled), setup-link refuses a
// confirmed account (confirmed), a disabled one that has not confirmed
// and an unknown one, and asks on a terminal; the disabled admin does not
// keep the first admin out.
func TestAccountsSetupLinkRefusals(t *testing.T) {
	t.Parallel()

	setupStores(t, func(t *testing.T, r cliRun, st store.Store) {
		initRes := setupInit(t, st)
		confirmed := testNow.Add(-50 * time.Hour)

		setupAccount(t, st, "ben.member@example.org", domain.RoleMember, confirmed, time.Time{})
		setupAccount(t, st, "former.admin@example.org", domain.RoleAdmin, confirmed, testNow.Add(-time.Hour))
		setupAccount(t, st, "gone.member@example.org", domain.RoleMember, time.Time{}, testNow.Add(-time.Hour))

		for _, c := range []struct {
			ref  string
			code int
			want string
		}{
			{"ben.member@example.org", cli.ExitConflict, "refused (confirmed): setup link: conflict: the account is confirmed"},
			{"FORMER.ADMIN@example.org", cli.ExitConflict, "refused (confirmed)"},
			{"gone.member@example.org", cli.ExitConflict, "the account may not set a password"},
			{"nobody@example.org", cli.ExitNotFound, `no account has the email or ID "nobody@example.org"`},
		} {
			wantExit(t, c.ref, r.exec(t, "accounts", "setup-link", "--yes", c.ref), c.code, c.want)
		}

		res := cliRun{st: r.st, env: r.env, terminal: true, stdin: "n\n"}.exec(t, "accounts", "setup-link", setupAdmin)
		wantExit(t, "answer n", res, cli.ExitUsage, "not confirmed; nothing was changed")

		if q := "Issue a setup link for " + setupAdmin + "? Its pending link stops working. [y/N] "; !strings.Contains(
			res.stderr, q) {
			t.Errorf("stderr %q lacks the question", res.stderr)
		}

		if _, ok := tokenStored(t, st, linkToken(t, initRes.Link)); !ok || auditEvents(t, st) != 0 {
			t.Fatal("a refused setup-link replaced the init link or wrote an audit event")
		}

		res = cliRun{st: r.st, env: r.env, terminal: true, stdin: "y\n"}.exec(t, "accounts", "setup-link", setupAdmin)
		if res.code != 0 {
			t.Fatalf("answer y: exit %d, stderr %q", res.code, res.stderr)
		}

		if err := setupAuth(t, st).ResetPassword(t.Context(), linkToken(t, res.stdout), setupPassword); err != nil {
			t.Errorf("the setup link: %v", err)
		}
	})
}

// TestAccountsSetupLinkStoreFailure: a database failure in the lookup of
// the account or in issuing the link fails the command with exit 1.
func TestAccountsSetupLinkStoreFailure(t *testing.T) {
	t.Parallel()

	for _, fail := range []int{2, 3} {
		mem := memory.New()
		setupInit(t, mem)

		st := accountsFailing{Store: mem, fail: fail, n: new(int)}
		wantExit(t, fmt.Sprintf("transaction %d failing", fail),
			cliRun{st: st}.exec(t, "accounts", "setup-link", "--yes", setupAdmin), cli.ExitFailed,
			errAccountsGone.Error())
	}
}
