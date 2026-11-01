package main

import (
	"context"
	"fmt"
	netmail "net/mail"
	"slices"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cwbudde/studio-schedule/cmd/studio-schedule/cli"
	"github.com/cwbudde/studio-schedule/internal/config"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/httpapi"
	"github.com/cwbudde/studio-schedule/internal/mail"
	"github.com/cwbudde/studio-schedule/internal/service"
)

// The mail commands (ADR 0014, ADR 0020): a test mail through the mail
// server, and the mail log as admins see it.
func newMailCmd(app *cli.App) *cobra.Command {
	return cli.Group("mail", "Test the mail server, list the mail log and queue failed mails again",
		newMailTestCmd(app), newMailListCmd(app), newMailRequeueCmd(app))
}

// mailTestJSON documents what `mail test --output json` prints.
const mailTestJSON = "one object: to (the recipient), server (the mail server, " + config.EnvSMTPHost +
	"), locale (the language of the mail) and subject"

// mailTestResult is the JSON of mail test.
type mailTestResult struct {
	To      string `json:"to"`
	Server  string `json:"server"`
	Locale  string `json:"locale"`
	Subject string `json:"subject"`
}

func newMailTestCmd(app *cli.App) *cobra.Command {
	var to, locale string

	return app.Command(cli.Spec{
		Use:   "test --to <address>",
		Short: "Send a test mail through the configured mail server",
		Long: "Render a short test mail and send it to --to straight through the mail server of " +
			config.EnvSMTPHost + " and the other SMTP_* variables, as serve sends mail, but not through the " +
			"outbox: it sends a real mail. It is an operator check like doctor's smtp line, which only logs in: " +
			"it prints ok when the server accepted the mail and exits with status 1 and the server's error " +
			"when it did not. Without " + config.EnvSMTPHost + " it exits with status 2. The mail is in " +
			"English, or in the language of --locale.",
		Example: "  studio-schedule mail test --to office@example.org\n" +
			"  studio-schedule mail test --to office@example.org --locale de",
		JSON: mailTestJSON,
		Flags: func(cmd *cobra.Command) {
			cmd.Flags().StringVar(&to, "to", "", "the address the test mail goes to (required)")
			cmd.Flags().StringVar(&locale, "locale", mail.DefaultLocale, "the language of the test mail: en or de")
			_ = cmd.MarkFlagRequired("to")
		},
		Run: func(ctx context.Context, env *cli.Env, _ []string) error {
			if a, err := netmail.ParseAddress(to); err != nil || a.Address != to {
				return cli.Usagef("--to: %q is not a bare email address", to)
			}

			cfg, err := env.Config(config.Flags{})
			if err != nil {
				return err
			}

			d, err := env.Delivery(cfg)
			if err != nil {
				return err
			}

			if d.Mailer == nil {
				return cli.Usagef("%s is not set: there is no mail server to test", config.EnvSMTPHost)
			}

			loc, rendered, err := testMail(locale, cfg.SMTP.Host)
			if err != nil {
				return err
			}

			if err := d.Mailer.Send(ctx, rendered.Message(to)); err != nil {
				return sendError{err}
			}

			res := mailTestResult{To: to, Server: cfg.SMTP.Host, Locale: loc, Subject: rendered.Subject}

			return env.Out.Print(res, func(t *cli.Text) {
				t.Line("%s: the test mail to %s went through %s", t.Paint(cli.Green, "ok"), to, cfg.SMTP.Host)
			})
		},
	})
}

// testMail renders the test mail in the template locale of locale; a locale
// without templates is a usage error.
func testMail(locale, server string) (loc string, rendered mail.Rendered, err error) {
	r, err := mail.NewRenderer()
	if err == nil {
		var ok bool
		if loc, ok = r.MatchLocale(locale); !ok {
			return "", mail.Rendered{}, cli.Usagef("--locale: %q is not one of %s", locale,
				strings.Join(r.Locales(), ", "))
		}

		rendered, err = r.Render(loc, mail.TemplateTest, mail.TestData{Server: server})
	}

	return loc, rendered, err
}

// sendError is a test mail the mail server did not take. It keeps the
// server's message but not its error chain: whatever that wraps (a
// permanent failure, an invalid address), the command failed (exit 1).
type sendError struct{ err error }

func (e sendError) Error() string { return "the test mail was not sent: " + e.err.Error() }

func newMailListCmd(app *cli.App) *cobra.Command {
	var status, template, cursor string

	return app.Command(cli.Spec{
		Use:   "list",
		Short: "List the mail log, newest first",
		Long: fmt.Sprintf("List the mails of the outbox as the admins' mail log does (ADR 0014), newest first, "+
			"at most %d per page, acting as the admin account of --as: when, the delivery state, the "+
			"template, the recipient, the subject, the attempts and the last error. Bodies are never "+
			"printed. --status and --template filter; when another page follows, a hint on stderr gives its "+
			"--cursor (with --output json, the nextCursor of the page). The bodies of mails sent more than 30 "+
			"days ago are removed, mails older than 180 days deleted.", service.MailLogPageSize),
		Example: "  studio-schedule mail list --as admin@example.org --status failed\n" +
			"  studio-schedule mail list --as admin@example.org --template receipt --output json",
		Access: httpapi.AccessAdmin,
		JSON:   "the response body of the operation searchMails",
		Flags: func(cmd *cobra.Command) {
			cmd.Flags().StringVar(&status, "status", "", "only mails in this state: pending, sent or failed")
			cmd.Flags().StringVar(&template, "template", "", "only mails of this template, e.g. receipt")
			cmd.Flags().StringVar(&cursor, "cursor", "", "where the page starts: the cursor of the previous "+
				"page's hint (nextCursor)")
		},
		Fields: map[string]string{"status": "--status"},
		Run: func(ctx context.Context, env *cli.Env, _ []string) error {
			if template != "" && !slices.Contains(mail.TemplateNames(), template) {
				return cli.Usagef("--template: %q is not one of %s", template, strings.Join(mail.TemplateNames(), ", "))
			}

			after, err := httpapi.ParseMailCursor(cursor)
			if err != nil {
				return cli.Usagef("--cursor: %q is no cursor of a page", cursor)
			}

			page, err := env.Services.MailLog.List(ctx, service.MailLogQuery{
				Status: domain.MailStatus(status), Template: template, After: after,
			})
			if err != nil {
				return err
			}

			body := httpapi.NewMailLogPage(page)

			err = env.Out.Print(body, func(t *cli.Text) {
				t.Table("ID", "Created", "Status", "Template", "To", "Subject", "Attempts", "Last error")

				for _, m := range body.Items {
					t.Row(m.ID.String(), t.Time(m.CreatedAt), t.Paint(mailStatusStyle[m.Status], m.Status),
						string(m.Template), m.To, m.Subject, strconv.Itoa(m.Attempts), m.LastError)
				}
			})
			if err == nil && body.NextCursor != "" && !env.Out.JSON() {
				_, err = fmt.Fprintf(env.Stderr(), "more mails: run again with --cursor %s\n", body.NextCursor)
			}

			return err
		},
	})
}

// mailStatusStyle colors a mail's state on a color terminal.
var mailStatusStyle = map[string]cli.Style{
	string(domain.MailSent): cli.Green, string(domain.MailFailed): cli.Red, string(domain.MailPending): cli.Faint,
}

// mailRequeueJSON documents what `mail requeue --output json` prints.
const mailRequeueJSON = "with an ID, the response body of the operation requeueMail; with --failed, one " +
	"object: requeued (the number of failed mails queued again) and secretLink (the number of failed mails " +
	"with a secret link, which stay failed)"

// requeueResult is the JSON of mail requeue --failed.
type requeueResult struct {
	Requeued   int `json:"requeued"`
	SecretLink int `json:"secretLink"`
}

func newMailRequeueCmd(app *cli.App) *cobra.Command {
	var failed bool

	return app.Command(cli.Spec{
		Use:   "requeue <id> | --failed",
		Short: "Queue a failed mail, or every failed one, again",
		Long: "Queue a failed mail of the mail log again, or with --failed every failed mail, acting as the " +
			"admin account of --as (ADR 0014): its next attempt is now and its attempts start over, so the " +
			"server's deliver-mail job sends it again. Each mail is recorded in the audit log as mail-requeued. " +
			"A mail with a secret link (confirm-email, change-email, reset-password, invite, waitlist-invite) " +
			"is never queued again: with an ID that is a conflict, with --failed it stays failed and is counted. " +
			"A pending or sent mail is a conflict too.",
		Example: "  studio-schedule mail requeue --as admin@example.org 0190a7c4-1b2c-7d3e-8f40-5a6b7c8d9e0f\n" +
			"  studio-schedule mail requeue --as admin@example.org --failed --yes",
		Args:        cobra.MaximumNArgs(1),
		Access:      httpapi.AccessAdmin,
		Destructive: true,
		JSON:        mailRequeueJSON,
		Flags: func(cmd *cobra.Command) {
			cmd.Flags().BoolVar(&failed, "failed", false, "queue every failed mail again instead of one")
		},
		Run: func(ctx context.Context, env *cli.Env, args []string) error {
			switch {
			case failed && len(args) == 1:
				return cli.Usagef("give a mail's ID or --failed, not both")
			case failed:
				return requeueFailed(ctx, env)
			case len(args) == 0:
				return cli.Usagef("give the ID of a failed mail, or --failed for every failed mail")
			}

			id, err := domain.ParseID[domain.OutboxMailID](args[0])
			if err != nil {
				return cli.Usage(fmt.Errorf("<id>: %w", err))
			}

			if err := env.Confirm(fmt.Sprintf("Queue mail %s again?", id)); err != nil {
				return err
			}

			e, err := env.Services.MailLog.Requeue(ctx, env.Principal, id)
			if err != nil {
				return err
			}

			return env.Out.Print(httpapi.NewMailLogItem(e), func(t *cli.Text) {
				t.Line("queued mail %s to %s again", id, e.Mail.To)
			})
		},
	})
}

// requeueFailed queues every failed mail again, after the confirmation.
func requeueFailed(ctx context.Context, env *cli.Env) error {
	if err := env.Confirm("Queue every failed mail again?"); err != nil {
		return err
	}

	c, err := env.Services.MailLog.RequeueFailed(ctx, env.Principal)
	if err != nil {
		return err
	}

	return env.Out.Print(requeueResult{Requeued: c.Requeued, SecretLink: c.SecretLink}, func(t *cli.Text) {
		line := "queued " + plural(c.Requeued, "failed mail", "failed mails") + " again"
		if c.SecretLink > 0 {
			line += "; " + plural(c.SecretLink, "mail", "mails") + " with a secret link left failed"
		}

		t.Line("%s", line)
	})
}

// plural is n with the noun one, or many unless n is 1.
func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}

	return strconv.Itoa(n) + " " + many
}
