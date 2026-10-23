package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/config"
)

// The migrations themselves are tested against a real database in
// internal/store/postgres; these tests cover the command wiring.

func TestMigrateNeedsDatabaseURL(t *testing.T) {
	t.Parallel()

	for _, sub := range []string{"up", "down", "status"} {
		t.Run(sub, func(t *testing.T) {
			t.Parallel()

			out, err := execute(nil, "migrate", sub)
			if !errors.Is(err, errNoDatabaseURL) {
				t.Fatalf("migrate %s error = %v, want errNoDatabaseURL\n%s", sub, err, out)
			}

			// An empty flag does not fall back to the environment.
			out, err = execute(map[string]string{"DATABASE_URL": "postgres://db.example.test/studio"},
				"migrate", sub, "--database-url", "")
			if !errors.Is(err, errNoDatabaseURL) {
				t.Fatalf("migrate %s with empty flag: error = %v, want errNoDatabaseURL\n%s", sub, err, out)
			}
		})
	}
}

func TestMigrateRejectsInvalidDatabaseURL(t *testing.T) {
	t.Parallel()

	out, err := execute(map[string]string{"DATABASE_URL": "mysql://db.example.test/studio"}, "migrate", "status")
	if !errors.Is(err, config.ErrInvalid) {
		t.Fatalf("migrate status error = %v, want config.ErrInvalid\n%s", err, out)
	}
}

func TestMigrateErrorHidesPassword(t *testing.T) {
	t.Parallel()

	const password = "change-me-secret"

	for name, tc := range map[string]struct {
		env  map[string]string
		args []string
	}{
		"env":  {env: map[string]string{"DATABASE_URL": "postgres://app:" + password + "@127.0.0.1:1/studio?connect_timeout=2"}},
		"flag": {args: []string{"--database-url", "host=127.0.0.1 port=1 user=app password=" + password + " connect_timeout=2"}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			// Nothing listens on port 1, so connecting fails.
			out, err := execute(tc.env, append([]string{"migrate", "status"}, tc.args...)...)
			if err == nil {
				t.Fatalf("migrate status succeeded\n%s", out)
			}

			if strings.Contains(err.Error()+out, password) {
				t.Errorf("output leaks the password: %v\n%s", err, out)
			}
		})
	}
}

func TestMigrateHelp(t *testing.T) {
	t.Parallel()

	out := run(t, "migrate", "--help")

	for _, want := range []string{"up", "down", "status", "--database-url", "DATABASE_URL"} {
		if !strings.Contains(out, want) {
			t.Errorf("migrate --help lacks %q:\n%s", want, out)
		}
	}
}
