package main

import (
	"bytes"
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/domain"
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
