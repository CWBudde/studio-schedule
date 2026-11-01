package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/webpush"
)

// The secrets of the configuration in testConfigEnv, which config check
// must never print.
const (
	testDatabasePassword = "db-secret-pw"
	testSMTPPassword     = "smtp-secret-pw"
)

// testConfigEnv is a full, fictional configuration with every setting set,
// and the VAPID key pair in it.
func testConfigEnv(t *testing.T) (map[string]string, webpush.Keys) {
	t.Helper()

	keys, err := webpush.GenerateKeys()
	if err != nil {
		t.Fatal(err)
	}

	return map[string]string{
		"DATABASE_URL":            "postgres://studio:" + testDatabasePassword + "@db.example.test:5432/studio",
		"HTTP_ADDR":               ":8081",
		"BASE_URL":                "https://schedule.example.org/",
		"DEMO_MODE":               "true",
		"SMTP_HOST":               "mail.example.test",
		"SMTP_PORT":               "2525",
		"SMTP_TLS":                "starttls",
		"SMTP_USERNAME":           "mailer",
		"SMTP_PASSWORD":           testSMTPPassword,
		"SMTP_FROM":               "Example Dance School <noreply@example.org>",
		"TRUSTED_PROXIES":         "10.0.0.0/8, 192.0.2.1",
		"HSTS_INCLUDE_SUBDOMAINS": "false",
		"METRICS_ADDR":            "127.0.0.1:9090",
		"LOG_FORMAT":              "json",
		"LOG_LEVEL":               "debug",
		"VAPID_PUBLIC_KEY":        keys.PublicKey(),
		"VAPID_PRIVATE_KEY":       keys.PrivateKey(),
		"VAPID_SUBJECT":           "mailto:office@example.org",
		"STUDIO_AS":               "office@example.org",
	}, keys
}

// checkNoSecrets fails if out contains a secret of testConfigEnv.
func checkNoSecrets(t *testing.T, out string, keys webpush.Keys) {
	t.Helper()

	for _, secret := range []string{testDatabasePassword, testSMTPPassword, keys.PrivateKey()} {
		if strings.Contains(out, secret) {
			t.Errorf("the output contains the secret %q:\n%s", secret, out)
		}
	}
}

// TestConfigCheck prints every effective setting by its variable, a flag
// winning over the environment, with the secrets redacted.
func TestConfigCheck(t *testing.T) {
	t.Parallel()

	env, keys := testConfigEnv(t)
	run := cliRun{env: env}

	for _, format := range []string{"text", "json"} {
		res := run.exec(t, "config", "check", "--addr", "127.0.0.1:8082", "--output", format)
		if res.code != 0 || res.stderr != "" {
			t.Fatalf("%s: exit %d, stderr %q", format, res.code, res.stderr)
		}

		checkNoSecrets(t, res.stdout, keys)

		// The public key is new in every run.
		golden(t, "config/check."+map[string]string{"text": "txt", "json": "json"}[format], strings.ReplaceAll(res.stdout, keys.PublicKey(), "<public key>"))
	}
}

// TestConfigCheckDefaults prints the defaults of an empty environment: no
// database, so DEMO_MODE is what the environment says.
func TestConfigCheckDefaults(t *testing.T) {
	t.Parallel()

	res := cliRun{env: map[string]string{"DEMO_MODE": "true"}}.exec(t, "config", "check", "--output", "json")
	if res.code != 0 {
		t.Fatalf("exit %d, stderr %q", res.code, res.stderr)
	}

	var got configView
	if err := json.Unmarshal([]byte(res.stdout), &got); err != nil {
		t.Fatal(err)
	}

	if got.HTTPAddr != ":8080" || !got.DemoMode || !got.HSTSIncludeSubdomains || got.Log.Format != "text" ||
		got.Log.Level != "info" || got.DatabaseURL != "" || got.TrustedProxies == nil {
		t.Errorf("config check = %+v, want the defaults with the demo mode and an empty list of proxies", got)
	}
}

// TestConfigCheckInvalid exits with status 2 and names the variable or the
// flag of the bad value, without printing a configuration.
func TestConfigCheckInvalid(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		name string
		env  map[string]string
		args []string
		want string
	}{
		{"variable", map[string]string{"LOG_LEVEL": "loud"}, nil, `LOG_LEVEL: "loud" is not a log level`},
		{"flag", nil, []string{"--base-url", "ftp://schedule.example.org"}, "--base-url: must be an absolute http"},
		{"database url", nil, []string{"--database-url", "mysql://db.example.test/studio"}, "--database-url: scheme"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			res := cliRun{env: c.env}.exec(t, append([]string{"config", "check"}, c.args...)...)
			if res.code != 2 || !strings.Contains(res.stderr, c.want) || res.stdout != "" {
				t.Errorf("exit %d, stdout %q, stderr %q; want 2 and an error naming %q", res.code, res.stdout,
					res.stderr, c.want)
			}
		})
	}
}

// TestConfigCheckTerminal prints the header line on a terminal, in bold
// unless NO_COLOR is set.
func TestConfigCheckTerminal(t *testing.T) {
	t.Parallel()

	res := cliRun{terminal: true}.exec(t, "config", "check")
	if !strings.HasPrefix(res.stdout, "\x1b[1mSetting\x1b[0m") {
		t.Errorf("stdout = %q, want a bold header line", res.stdout)
	}

	res = cliRun{terminal: true, env: map[string]string{"NO_COLOR": "1"}}.exec(t, "config", "check")
	if !strings.HasPrefix(res.stdout, "Setting ") || !strings.Contains(res.stdout, "\nHTTP_ADDR ") {
		t.Errorf("stdout = %q, want a plain header line", res.stdout)
	}

	res = cliRun{}.exec(t, "config", "check")
	if !strings.HasPrefix(res.stdout, "DATABASE_URL ") {
		t.Errorf("stdout = %q, want the records alone without a terminal", res.stdout)
	}
}

// TestConfigOperatorCommands take no --as: they act as no account.
func TestConfigOperatorCommands(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{{"config", "check"}, {"doctor"}} {
		res := cliRun{}.exec(t, append(args, "--as", demoAdmin)...)
		if res.code != 2 || !strings.Contains(res.stderr, "--as: ") || res.stdout != "" {
			t.Errorf("%v --as: exit %d, stdout %q, stderr %q; want 2 and a usage error", args, res.code,
				res.stdout, res.stderr)
		}
	}
}

// TestConfigGroup prints its help without a subcommand and refuses an
// unknown one.
func TestConfigGroup(t *testing.T) {
	t.Parallel()

	if res := (cliRun{}).exec(t, "config"); res.code != 0 || !strings.Contains(res.stdout, "check") {
		t.Errorf("config: exit %d, stdout %q; want its help", res.code, res.stdout)
	}

	if res := (cliRun{}).exec(t, "config", "show"); res.code != 2 {
		t.Errorf("config show: exit %d, want 2", res.code)
	}
}
