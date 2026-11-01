package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/cwbudde/studio-schedule/cmd/studio-schedule/cli"
	"github.com/cwbudde/studio-schedule/internal/app"
	"github.com/cwbudde/studio-schedule/internal/config"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/mail/smtp"
	"github.com/cwbudde/studio-schedule/internal/service"
)

// The results of a check of doctor.
const (
	checkOK   = "ok"
	checkFail = "fail"
	checkWarn = "warn"
	checkSkip = "skip"
)

// receiptsTimeout bounds the receipt check: the first render compiles the
// module, which takes a while without a warm cache.
const receiptsTimeout = 2 * time.Minute

// errChecksFailed makes doctor exit with status 1 after its report.
var errChecksFailed = errors.New("a check failed")

// doctorJSON documents what `doctor --output json` prints.
const doctorJSON = `one object: ok (bool, false when a check failed) and checks, an array of {name, result, ` +
	`detail} in the order config, database, schema, settings, smtp, vapid, base-url, receipts, where ` +
	`result is ok, fail, warn or skip`

func newDoctorCmd(app *cli.App) *cobra.Command {
	var (
		flags serveFlags
		cmd   *cobra.Command
	)

	return app.Command(cli.Spec{
		Use:   "doctor",
		Short: "Check the configuration, the database, the mail server and the receipt module",
		Long: "Run every check of a deployment and print one line per check with its result (ok, fail, warn " +
			"or skip) and a detail: config (the configuration as serve loads it, from the environment and " +
			"serve's flags), database (it connects), schema (exactly the binary's migrations are applied), " +
			"settings (the school settings exist, with a time zone and currency the app accepts), smtp (a " +
			"connection and login to " + config.EnvSMTPHost + ", nothing is sent; skipped without it), vapid " +
			"(the web push key pair; skipped without it), base-url (https; plain http on localhost or no " +
			config.EnvBaseURL + " is a warning, for local development only, elsewhere a failure) and " +
			"receipts (a fictional sample receipt renders as a PDF; skipped in a build without the receipt " +
			"renderer). A check that needs a failed one is skipped. It exits with status 1 when a check " +
			"fails; warnings and skipped checks do not fail. With --output json it prints " + doctorJSON + ".",
		Example: "  studio-schedule doctor\n  studio-schedule doctor --output json",
		JSON:    doctorJSON,
		Flags:   func(c *cobra.Command) { cmd = c; flags.register(c) },
		Run: func(ctx context.Context, env *cli.Env, _ []string) error {
			report := runDoctor(ctx, env, flags.explicit(cmd))

			err := env.Out.Print(report, func(t *cli.Text) {
				t.Table("Check", "Result", "Detail")

				for _, c := range report.Checks {
					t.Row(c.Name, t.Paint(resultStyle[c.Result], c.Result), c.Detail)
				}
			})

			if err != nil || report.OK {
				return err
			}

			return errChecksFailed
		},
	})
}

// resultStyle colors a result on a color terminal.
var resultStyle = map[string]cli.Style{checkOK: cli.Green, checkFail: cli.Red, checkWarn: cli.Faint, checkSkip: cli.Faint}

// doctorReport is the JSON of doctor.
type doctorReport struct {
	OK     bool          `json:"ok"`
	Checks []doctorCheck `json:"checks"`
}

type doctorCheck struct {
	Name   string `json:"name"`
	Result string `json:"result"`
	Detail string `json:"detail"`
}

// outcome is the result of a check and its detail.
type outcome struct{ result, detail string }

func passed(detail string) outcome  { return outcome{checkOK, detail} }
func failed(detail string) outcome  { return outcome{checkFail, detail} }
func warned(detail string) outcome  { return outcome{checkWarn, detail} }
func skipped(detail string) outcome { return outcome{checkSkip, detail} }

// add adds the outcome of the check name; a failure fails the report. The
// detail is one line, also where an error has several (pgx's do).
func (r *doctorReport) add(name string, o outcome) {
	detail := strings.ReplaceAll(strings.TrimSpace(o.detail), "\n", "; ")
	r.Checks = append(r.Checks, doctorCheck{Name: name, Result: o.result, Detail: detail})
	if o.result == checkFail {
		r.OK = false
	}
}

// runDoctor runs the checks in their order.
func runDoctor(ctx context.Context, env *cli.Env, flags config.Flags) doctorReport {
	r := doctorReport{OK: true}

	cfg, err := env.Config(flags)
	if err != nil {
		r.add("config", failed(err.Error()))

		for _, name := range []string{"database", "schema", "settings", "smtp", "vapid", "base-url"} {
			r.add(name, skipped("the configuration is invalid"))
		}
	} else {
		r.add("config", passed("valid"))
		checkDatabase(ctx, env, cfg, &r)
		r.add("smtp", checkSMTP(ctx, env, cfg))
		r.add("vapid", checkVAPID(cfg))
		r.add("base-url", checkBaseURL(cfg))
	}

	r.add("receipts", checkReceipts(ctx, env))

	return r
}

// checkDatabase adds the checks database, schema and settings.
func checkDatabase(ctx context.Context, env *cli.Env, cfg config.Config, r *doctorReport) {
	if cfg.DatabaseURL == "" {
		r.add("database", failed("no database: set "+config.EnvDatabaseURL+" or --"+config.FlagDatabaseURL))
		r.add("schema", skipped("no database"))
		r.add("settings", skipped("no database"))

		return
	}

	services, release, err := env.Open(ctx, cfg)

	switch {
	case errors.Is(err, errSchemaOutdated), errors.Is(err, errSchemaNewer):
		r.add("database", passed("connected"))
		r.add("schema", failed(err.Error()))
		r.add("settings", skipped("the schema does not match"))

		return
	case err != nil:
		r.add("database", failed(err.Error()))
		r.add("schema", skipped("the database is unreachable"))
		r.add("settings", skipped("the database is unreachable"))

		return
	}

	defer release()

	r.add("database", passed("connected"))
	r.add("schema", passed("up to date"))
	r.add("settings", checkSettings(ctx, services))
}

// checkSettings checks the school settings by the domain's own rules,
// among them a time zone this host can load and a known currency.
func checkSettings(ctx context.Context, services app.Services) outcome {
	s, err := services.Settings.Public(ctx)

	switch {
	case errors.Is(err, domain.ErrNotFound):
		return failed("no school settings: run init")
	case err != nil:
		return failed(err.Error())
	}

	if err := s.Validate(); err != nil {
		return failed(err.Error())
	}

	return passed(s.Timezone + ", " + s.Currency)
}

// checkSMTP connects and logs in to the mail server, without sending.
func checkSMTP(ctx context.Context, env *cli.Env, cfg config.Config) outcome {
	if cfg.SMTP.Host == "" {
		return skipped(config.EnvSMTPHost + " is not set: mails stay in the outbox")
	}

	sender, err := smtp.New(smtp.Config{
		Host: cfg.SMTP.Host, Port: cfg.SMTP.Port, Username: cfg.SMTP.Username, Password: cfg.SMTP.Password,
		From: cfg.SMTP.From, TLS: smtp.TLSMode(cfg.SMTP.TLS),
	}, env.Clock)
	if err != nil {
		return failed(err.Error())
	}

	if err := sender.Verify(ctx); err != nil {
		return failed(err.Error())
	}

	if cfg.SMTP.Username == "" {
		return passed("connected to " + cfg.SMTP.Host + " (no login configured)")
	}

	return passed("logged in to " + cfg.SMTP.Host + " as " + cfg.SMTP.Username)
}

// checkVAPID reports the web push keys. config.Load refuses a pair that
// does not parse or match, so a loaded one is fine.
func checkVAPID(cfg config.Config) outcome {
	if !cfg.Push.Enabled() {
		return skipped("the VAPID_* variables are not set: web push is off")
	}

	return passed("keys parse and match")
}

// checkBaseURL wants an https BASE_URL: over plain http, session cookies
// are not Secure and HSTS is off.
func checkBaseURL(cfg config.Config) outcome {
	if cfg.BaseURL == "" {
		return warned(config.EnvBaseURL + " is not set: links point to " + appURL(cfg) +
			"; local development only")
	}

	u, _ := url.Parse(cfg.BaseURL) // validated by config.Load

	switch {
	case u.Scheme == "https":
		return passed(cfg.BaseURL)
	case u.Hostname() == "localhost", u.Hostname() == "127.0.0.1", u.Hostname() == "::1":
		return warned(cfg.BaseURL + " is plain http: local development only")
	default:
		return failed(cfg.BaseURL + " is plain http: session cookies are not Secure; use https")
	}
}

// checkReceipts renders a fictional sample receipt.
func checkReceipts(ctx context.Context, env *cli.Env) outcome {
	renderer, release, found := env.PDF()
	if !found {
		return skipped("this build has no receipt renderer")
	}

	defer release()

	ctx, cancel := context.WithTimeout(ctx, receiptsTimeout)
	defer cancel()

	out, err := renderer.Render(ctx, service.SampleReceiptInput())

	switch {
	case err != nil:
		return failed(err.Error())
	case !bytes.HasPrefix(out, []byte("%PDF-")):
		return failed(fmt.Sprintf("the renderer returned %d bytes that are no PDF", len(out)))
	default:
		return passed("a sample receipt renders as PDF")
	}
}
