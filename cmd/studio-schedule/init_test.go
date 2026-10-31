package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/config"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/service"
	"github.com/cwbudde/studio-schedule/internal/store"
	"github.com/cwbudde/studio-schedule/internal/store/memory"
)

// executeInit runs init on st with stdin and the environment env (a
// database URL included, which the memory store ignores).
func executeInit(st store.Store, stdin string, env map[string]string, args ...string) (string, error) {
	var out bytes.Buffer

	cmd := newInitCmdWith(func(key string) (string, bool) {
		v, ok := env[key]

		return v, ok
	}, func(context.Context, string) (store.Store, func(), error) { return st, func() {}, nil })
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)

	err := cmd.Execute()

	return out.String(), err
}

var withDB = map[string]string{"DATABASE_URL": "postgres://db.example.test/studio", "BASE_URL": "https://schedule.example.test"}

func settingsOf(t *testing.T, st store.Store) domain.Settings {
	t.Helper()

	var s domain.Settings

	err := st.InTx(t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		var err error
		s, err = tx.Settings().Get(ctx)

		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	return s
}

var setupLink = regexp.MustCompile(`https://schedule\.example\.test/reset-password\?token=[A-Za-z0-9_-]{43}`)

func TestInitWithFlags(t *testing.T) {
	t.Parallel()

	st := memory.New()

	out, err := executeInit(st, "", withDB, "--school-name", "Fictional Dance Hall", "--timezone", "Europe/Vienna",
		"--locale", "de", "--currency", "EUR", "--admin-email", "admin@fictional-dance.example",
		"--admin-given-name", "Ada", "--admin-family-name", "Example")
	if err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}

	if !setupLink.MatchString(out) || !strings.Contains(out, "admin@fictional-dance.example") {
		t.Errorf("output lacks the setup link:\n%s", out)
	}

	if strings.Contains(out, "School name") {
		t.Errorf("init asked although every flag was given:\n%s", out)
	}

	if s := settingsOf(t, st); s.SchoolName != "Fictional Dance Hall" || s.Timezone != "Europe/Vienna" ||
		s.DefaultLocale != "de" {
		t.Errorf("settings %+v", s)
	}

	// Never twice.
	out, err = executeInit(st, "", withDB, "--school-name", "Other", "--timezone", "UTC", "--locale", "en",
		"--currency", "EUR", "--admin-email", "other@fictional-dance.example", "--admin-given-name", "Ben",
		"--admin-family-name", "Example")
	if !errors.Is(err, errAlreadySetUp) {
		t.Fatalf("second init: %v\n%s", err, out)
	}
}

func TestInitAsks(t *testing.T) {
	t.Parallel()

	st := memory.New()

	// The language and the currency take their defaults.
	out, err := executeInit(st, "Fictional Dance Hall\nEurope/Berlin\n\n\nadmin@fictional-dance.example\nAda\nExample\n", withDB)
	if err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}

	for _, prompt := range []string{"School name: ", "Default language (en or de) [en]: ", "Currency (ISO 4217) [EUR]: "} {
		if !strings.Contains(out, prompt) {
			t.Errorf("no prompt %q:\n%s", prompt, out)
		}
	}

	if s := settingsOf(t, st); s.DefaultLocale != "en" || s.Currency != "EUR" || s.Timezone != "Europe/Berlin" {
		t.Errorf("settings %+v", s)
	}

	if !setupLink.MatchString(out) {
		t.Errorf("no setup link:\n%s", out)
	}
}

func TestInitErrors(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		env   map[string]string
		stdin string
		args  []string
		want  string
	}{
		"no database": {env: map[string]string{}, want: errNoDatabaseURL.Error()},
		"no input":    {env: withDB, want: "missing --school-name"},
		"input ends":  {env: withDB, stdin: "Fictional Dance Hall\n", want: "missing --timezone"},
		"bad zone": {
			env: withDB, stdin: "Fictional Dance Hall\nNowhere/Town\nen\nEUR\nadmin@fictional-dance.example\nAda\nExample\n",
			want: "invalid --timezone",
		},
		"bad email": {
			env: withDB, args: []string{"--admin-email", "nobody"},
			stdin: "Fictional Dance Hall\nUTC\nen\nEUR\nAda\nExample\n", want: "invalid --admin-email",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			st := memory.New()

			out, err := executeInit(st, tc.stdin, tc.env, tc.args...)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q\n%s", err, tc.want, out)
			}

			err = st.InTx(t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
				_, err := tx.Settings().Get(ctx)

				return err
			})
			if !errors.Is(err, domain.ErrNotFound) {
				t.Errorf("settings stored after a failed init: %v", err)
			}
		})
	}
}

// initFlags are the flags of every value init asks for: a run with them
// reads no input.
func initFlags(email string) []string {
	return []string{
		"--school-name", "Fictional Dance Hall", "--timezone", "Europe/Vienna", "--locale", "de", "--currency", "chf",
		"--admin-email", email, "--admin-given-name", "Ada", "--admin-family-name", "Example",
	}
}

// initExecute runs the root command with stdin and env, so init opens the
// real PostgreSQL store.
func initExecute(stdin string, env map[string]string, args ...string) (string, error) {
	var out bytes.Buffer

	cmd := newRootCmd(func(key string) (string, bool) {
		v, ok := env[key]

		return v, ok
	})
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(append([]string{"init"}, args...))

	err := cmd.Execute()

	return out.String(), err
}

// TestInitOnPostgres sets a school up in a new migrated database through
// the command line: the settings, an admin whose setup link works once, and
// a refusal to run again (with the database from the flag this time).
func TestInitOnPostgres(t *testing.T) {
	t.Parallel()

	databaseURL := newTestDatabase(t)
	before := clock.System{}.Now()

	out, err := initExecute("", map[string]string{"DATABASE_URL": databaseURL, "BASE_URL": "https://schedule.example.test"},
		initFlags(" Admin@Fictional-Dance.example ")...)
	if err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}

	link := setupLink.FindString(out)
	if link == "" || !strings.Contains(out, `The school "Fictional Dance Hall" is set up, with Admin@Fictional-Dance.example as its admin.`) {
		t.Fatalf("output lacks the setup link or the admin:\n%s", out)
	}

	u, err := url.Parse(link)
	if err != nil {
		t.Fatal(err)
	}

	st := openTestStore(t, databaseURL)

	err = st.InTx(t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		s, err := tx.Settings().Get(ctx)
		if err != nil {
			return err
		}

		// The currency is upper-cased, the rest starts with the defaults.
		if s.SchoolName != "Fictional Dance Hall" || s.Timezone != "Europe/Vienna" || s.DefaultLocale != "de" ||
			s.Currency != "CHF" || s.ReceiptPrefix != "R" {
			t.Errorf("settings %+v", s)
		}

		acc, err := tx.Accounts().GetByEmail(ctx, "admin@fictional-dance.example")
		if err != nil {
			return err
		}

		if acc.Role != domain.RoleAdmin || acc.Email != "Admin@Fictional-Dance.example" || acc.Locale != "de" {
			t.Errorf("account %+v", acc)
		}

		p, err := tx.Persons().Get(ctx, acc.PersonID)
		if err != nil {
			return err
		}

		if p.GivenName != "Ada" || p.FamilyName != "Example" {
			t.Errorf("admin person %+v", p)
		}

		tok, err := tx.AccountTokens().Get(ctx, domain.HashToken(u.Query().Get("token")))
		if err != nil {
			return err
		}

		if tok.AccountID != acc.ID || tok.Purpose != domain.TokenResetPassword ||
			tok.ExpiresAt.Before(before.Add(service.InviteTokenTTL)) ||
			tok.ExpiresAt.After(clock.System{}.Now().Add(service.InviteTokenTTL)) {
			t.Errorf("setup token %+v", tok)
		}

		// The expiry the output names is the token's, in UTC.
		if want := tok.ExpiresAt.UTC().Format("2006-01-02 15:04 UTC"); !strings.Contains(out, "before "+want+" to set") {
			t.Errorf("output does not name the expiry %s:\n%s", want, out)
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// Never twice: --database-url instead of the env var reaches the same
	// database.
	out, err = initExecute("", nil, append(initFlags("other@fictional-dance.example"), "--database-url", databaseURL)...)
	if !errors.Is(err, errAlreadySetUp) {
		t.Fatalf("second init: %v\n%s", err, out)
	}

	if s := settingsOf(t, st); s.Timezone != "Europe/Vienna" {
		t.Errorf("the second init changed the settings: %+v", s)
	}
}

// TestInitRefusesOtherSchemas: init runs only on a database with exactly
// the binary's migrations, like serve.
func TestInitRefusesOtherSchemas(t *testing.T) {
	t.Parallel()

	unmigrated := newUnmigratedDatabase(t)
	newer := newTestDatabase(t)
	execSQL(t, newer, "INSERT INTO goose_db_version (version_id, is_applied) VALUES (99999999999999, true)")

	for databaseURL, want := range map[string]error{unmigrated: errSchemaOutdated, newer: errSchemaNewer} {
		out, err := initExecute("", map[string]string{"DATABASE_URL": databaseURL}, initFlags("admin@fictional-dance.example")...)
		if !errors.Is(err, want) {
			t.Errorf("init: %v, want %v\n%s", err, want, out)
		}
	}
}

// TestInitEmailInUse: a database without settings whose persons already
// include an account with the admin's email (e.g. a failed earlier setup
// repaired by hand) fails as a whole, and the error is not reported as an
// invalid --admin-email: the address is valid, it is taken.
func TestInitEmailInUse(t *testing.T) {
	t.Parallel()

	databaseURL := newTestDatabase(t)
	st := openTestStore(t, databaseURL)

	err := st.InTx(t.Context(), store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		p := domain.Person{ID: ids.New[domain.PersonID](ids.V7{}), GivenName: "Ben", FamilyName: "Example"}
		if err := tx.Persons().Create(ctx, p); err != nil {
			return err
		}

		return tx.Accounts().Create(ctx, domain.Account{
			ID: ids.New[domain.AccountID](ids.V7{}), PersonID: p.ID, Email: "admin@fictional-dance.example",
			Role: domain.RoleMember,
		})
	})
	if err != nil {
		t.Fatal(err)
	}

	out, err := initExecute("", map[string]string{"DATABASE_URL": databaseURL}, initFlags("ADMIN@fictional-dance.example")...)
	if !errors.Is(err, service.ErrEmailInUse) || strings.Contains(err.Error(), "--admin-email") {
		t.Fatalf("init: %v, want ErrEmailInUse\n%s", err, out)
	}

	err = st.InTx(t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		_, err := tx.Settings().Get(ctx)

		return err
	})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("settings stored by the failed init: %v", err)
	}
}

// TestInitConfig: the setup link's origin is --base-url over BASE_URL, and
// without either the local server's at the HTTP_ADDR port; an invalid one
// fails before the database is opened.
func TestInitConfig(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		env  map[string]string
		args []string
		link string
	}{
		"flag over env": {
			env:  map[string]string{"DATABASE_URL": "postgres://db.example.test/studio", "BASE_URL": "https://old.example.test"},
			args: []string{"--base-url", "https://schedule.example.test"},
			link: "https://schedule.example.test/reset-password?token=",
		},
		"local server": {
			env:  map[string]string{"DATABASE_URL": "postgres://db.example.test/studio", "HTTP_ADDR": ":9000"},
			link: "http://localhost:9000/reset-password?token=",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			out, err := executeInit(memory.New(), "", tc.env, append(initFlags("admin@fictional-dance.example"), tc.args...)...)
			if err != nil || !strings.Contains(out, "\n  "+tc.link) || strings.Contains(out, "old.example.test") {
				t.Errorf("init: %v, want a link at %s\n%s", err, tc.link, out)
			}
		})
	}

	// Nothing listens on port 1: an attempt to connect would fail otherwise.
	unreachable := "postgres://app@127.0.0.1:1/studio?connect_timeout=2"

	for name, tc := range map[string]struct {
		env  map[string]string
		args []string
	}{
		"env":  {env: map[string]string{"DATABASE_URL": unreachable, "BASE_URL": "ftp://schedule.example.test"}},
		"flag": {env: map[string]string{"DATABASE_URL": unreachable}, args: []string{"--base-url", "/relative"}},
	} {
		t.Run("invalid "+name, func(t *testing.T) {
			t.Parallel()

			out, err := initExecute("", tc.env, append(initFlags("admin@fictional-dance.example"), tc.args...)...)
			if !errors.Is(err, config.ErrInvalid) {
				t.Errorf("init: %v, want config.ErrInvalid\n%s", err, out)
			}
		})
	}
}

// TestInitAskEdges: the last line may lack its newline; at the end of the
// input a value with a default takes it, and a blank line is no value.
func TestInitAskEdges(t *testing.T) {
	t.Parallel()

	admin := []string{
		"--admin-email", "admin@fictional-dance.example", "--admin-given-name", "Ada",
		"--admin-family-name", "Example",
	}

	st := memory.New()

	// "de" ends the input: the currency, asked next, takes its default.
	out, err := executeInit(st, "Fictional Dance Hall\nUTC\nde", withDB, admin...)
	if err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}

	if s := settingsOf(t, st); s.DefaultLocale != "de" || s.Currency != "EUR" || s.Timezone != "UTC" {
		t.Errorf("settings %+v", s)
	}

	for stdin, want := range map[string]string{
		"\n":                       "missing --school-name",
		" \t \n":                   "missing --school-name",
		"Fictional Dance Hall\n\n": "missing --timezone",
	} {
		if out, err := executeInit(memory.New(), stdin, withDB, admin...); err == nil || err.Error() != want {
			t.Errorf("stdin %q: %v, want %q\n%s", stdin, err, want, out)
		}
	}
}

// TestInitPromptFails: a prompt that cannot be written (stderr closed)
// stops init before it reads or stores anything.
func TestInitPromptFails(t *testing.T) {
	t.Parallel()

	st := memory.New()
	broken := errors.New("stderr is closed")

	cmd := newInitCmdWith(func(key string) (string, bool) {
		v, ok := withDB[key]

		return v, ok
	}, func(context.Context, string) (store.Store, func(), error) { return st, func() {}, nil })
	cmd.SetIn(strings.NewReader("Fictional Dance Hall\nUTC\n\n\nadmin@fictional-dance.example\nAda\nExample\n"))
	cmd.SetOut(io.Discard)
	cmd.SetErr(initFailingWriter{broken})
	cmd.SetArgs(nil)

	if err := cmd.Execute(); !errors.Is(err, broken) {
		t.Fatalf("init: %v, want the write error", err)
	}

	err := st.InTx(t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		_, err := tx.Settings().Get(ctx)

		return err
	})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("settings stored: %v", err)
	}
}

// initFailingWriter fails every write with err.
type initFailingWriter struct{ err error }

func (w initFailingWriter) Write([]byte) (int, error) { return 0, w.err }
