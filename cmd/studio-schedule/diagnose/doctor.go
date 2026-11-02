// Package diagnose computes the reports of the operator checks: doctor's
// checks of a deployment and db check's integrity findings. The commands
// `doctor` and `db check` print them, and the MCP server's tools of the
// same names return them (ADR 0022), so a tool and its command cannot
// drift. The package is server-only, like the command layer it serves.
package diagnose

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/cwbudde/studio-schedule/cmd/studio-schedule/cli"
	"github.com/cwbudde/studio-schedule/internal/app"
	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/config"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/mail/smtp"
	"github.com/cwbudde/studio-schedule/internal/service"
)

// The results of a check of doctor.
const (
	CheckOK   = "ok"
	CheckFail = "fail"
	CheckWarn = "warn"
	CheckSkip = "skip"
)

// receiptsTimeout bounds the receipt check: the first render compiles the
// module, which takes a while without a warm cache.
const receiptsTimeout = 2 * time.Minute

// DoctorJSON documents the JSON of a DoctorReport.
const DoctorJSON = `one object: ok (bool, false when a check failed) and checks, an array of {name, result, ` +
	`detail} in the order config, database, schema, settings, smtp, vapid, base-url, receipts, where ` +
	`result is ok, fail, warn or skip`

// DoctorReport is the report of doctor.
type DoctorReport struct {
	OK     bool          `json:"ok"`
	Checks []DoctorCheck `json:"checks"`
}

// DoctorCheck is one check of a DoctorReport.
type DoctorCheck struct {
	Name   string `json:"name"`
	Result string `json:"result"`
	Detail string `json:"detail"`
}

// Env is what doctor needs of the command layer; *cli.Env is one.
type Env interface {
	// Config loads the configuration as serve does, with flags.
	Config(flags config.Flags) (config.Config, error)
	// Open opens the database of cfg and returns its services.
	Open(ctx context.Context, cfg config.Config) (svc app.Services, release func(), err error)
	// PDF starts the receipt renderer; ok is false without one.
	PDF() (r service.ReceiptRenderer, release func(), ok bool)
}

// outcome is the result of a check and its detail.
type outcome struct{ result, detail string }

func passed(detail string) outcome  { return outcome{CheckOK, detail} }
func failed(detail string) outcome  { return outcome{CheckFail, detail} }
func warned(detail string) outcome  { return outcome{CheckWarn, detail} }
func skipped(detail string) outcome { return outcome{CheckSkip, detail} }

// add adds the outcome of the check name; a failure fails the report. The
// detail is one line, also where an error has several (pgx's do).
func (r *DoctorReport) add(name string, o outcome) {
	detail := strings.ReplaceAll(strings.TrimSpace(o.detail), "\n", "; ")
	r.Checks = append(r.Checks, DoctorCheck{Name: name, Result: o.result, Detail: detail})
	if o.result == CheckFail {
		r.OK = false
	}
}

// Doctor runs the checks of a deployment in their order, with the
// configuration of env and flags (serve's explicit flags) and clk as the
// mail sender's clock.
func Doctor(ctx context.Context, env Env, clk clock.Clock, flags config.Flags) DoctorReport {
	r := DoctorReport{OK: true}

	cfg, err := env.Config(flags)
	if err != nil {
		r.add("config", failed(err.Error()))

		for _, name := range []string{"database", "schema", "settings", "smtp", "vapid", "base-url"} {
			r.add(name, skipped("the configuration is invalid"))
		}
	} else {
		r.add("config", passed("valid"))
		checkDatabase(ctx, env, cfg, &r)
		r.add("smtp", checkSMTP(ctx, clk, cfg))
		r.add("vapid", checkVAPID(cfg))
		r.add("base-url", checkBaseURL(cfg))
	}

	r.add("receipts", checkReceipts(ctx, env))

	return r
}

// checkDatabase adds the checks database, schema and settings.
func checkDatabase(ctx context.Context, env Env, cfg config.Config, r *DoctorReport) {
	if cfg.DatabaseURL == "" {
		r.add("database", failed("no database: set "+config.EnvDatabaseURL+" or --"+config.FlagDatabaseURL))
		r.add("schema", skipped("no database"))
		r.add("settings", skipped("no database"))

		return
	}

	services, release, err := env.Open(ctx, cfg)

	switch {
	case errors.Is(err, cli.ErrSchemaOutdated), errors.Is(err, cli.ErrSchemaNewer):
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
func checkSMTP(ctx context.Context, clk clock.Clock, cfg config.Config) outcome {
	if cfg.SMTP.Host == "" {
		return skipped(config.EnvSMTPHost + " is not set: mails stay in the outbox")
	}

	sender, err := smtp.New(smtp.Config{
		Host: cfg.SMTP.Host, Port: cfg.SMTP.Port, Username: cfg.SMTP.Username, Password: cfg.SMTP.Password,
		From: cfg.SMTP.From, TLS: smtp.TLSMode(cfg.SMTP.TLS),
	}, clk)
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
		return warned(config.EnvBaseURL + " is not set: links point to " + cli.AppURL(cfg) +
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
func checkReceipts(ctx context.Context, env Env) outcome {
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
