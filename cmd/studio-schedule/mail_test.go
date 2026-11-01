package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/cmd/studio-schedule/cli"
	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/config"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/mail"
	"github.com/cwbudde/studio-schedule/internal/service"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// logMail is the test mail n of the template to to, created hours
// before testNow, in the state status (as internal/service's addMail).
func logMail(n int, to, template string, hours int, status domain.MailStatus) domain.OutboxMail {
	id, err := domain.ParseID[domain.OutboxMailID](fmt.Sprintf("00000000-0000-7000-8000-%012x", 0xa000+n))
	if err != nil {
		panic(err)
	}

	created := testNow.Add(-time.Duration(hours) * time.Hour)
	m := domain.OutboxMail{
		ID: id, To: to, Subject: "About " + template, Text: "Secret text of " + to, HTML: "<p>Body</p>",
		Template: template, CreatedAt: created, NextAttemptAt: created.Add(time.Minute), Attempts: 1,
		LastError: "421 try again later",
	}

	switch status {
	case domain.MailSent:
		m.SentAt, m.NextAttemptAt, m.LastError = created.Add(time.Minute), time.Time{}, ""
	case domain.MailFailed:
		m.NextAttemptAt, m.Attempts, m.LastError = time.Time{}, service.MaxMailAttempts, "550 no such user"
	}

	return m
}

// addLogMails stores mails in the outbox of st.
func addLogMails(t *testing.T, st store.Store, mails ...domain.OutboxMail) {
	t.Helper()

	err := st.InTx(t.Context(), store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		for _, m := range mails {
			if err := tx.Outbox().Create(ctx, m); err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// The mails of the mail log's tests: a failed receipt, a failed invite
// (a secret link), a pending announcement and a sent booking.
var (
	failedReceipt = logMail(1, "ella.example@example.org", mail.TemplateReceipt, 5, domain.MailFailed)
	failedInvite  = logMail(2, "finn.example@example.org", mail.TemplateInvite, 4, domain.MailFailed)
	pendingNews   = logMail(3, "ella.example@example.org", mail.TemplateAnnouncement, 3, domain.MailPending)
	sentBooking   = logMail(4, "gina.example@example.org", mail.TemplateBookingConfirmed, 2, domain.MailSent)
)

// addMailLog stores the mails of the mail log's tests.
func addMailLog(t *testing.T, st store.Store) {
	t.Helper()

	addLogMails(t, st, failedReceipt, failedInvite, pendingNews, sentBooking)
}

// logMailOf returns the mail id of st.
func logMailOf(t *testing.T, st store.Store, id domain.OutboxMailID) domain.OutboxMail {
	t.Helper()

	var m domain.OutboxMail

	err := st.InTx(t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		var err error
		m, err = tx.Outbox().Get(ctx, id)

		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	return m
}

// requeueEvents returns the mail-requeued events of st, the latest first.
func requeueEvents(t *testing.T, st store.Store) []domain.AuditEvent {
	t.Helper()

	var out []domain.AuditEvent

	err := st.InTx(t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		events, err := tx.Audit().List(ctx, 1000)
		for _, e := range events {
			if e.Action == domain.AuditMailRequeued {
				out = append(out, e)
			}
		}

		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	return out
}

// TestMailList lists the mail log on both stores, newest first and
// without bodies, filtered by status and template.
func TestMailList(t *testing.T) {
	t.Parallel()

	stores(t, func(t *testing.T, r cliRun) {
		addMailLog(t, storeOf(t, r))

		text := r.exec(t, "mail", "list", "--as", demoAdmin)
		if text.code != 0 || text.stderr != "" {
			t.Fatalf("mail list: exit %d, stderr %q", text.code, text.stderr)
		}

		golden(t, "mail/list.txt", text.stdout)

		js := r.exec(t, "mail", "list", "--as", demoAdmin, "--output", "json")
		if js.code != 0 {
			t.Fatalf("mail list --output json: exit %d, stderr %q", js.code, js.stderr)
		}

		golden(t, "mail/list.json", js.stdout)

		if strings.Contains(text.stdout+js.stdout, "Secret text") {
			t.Error("mail list prints a body")
		}

		failed := r.exec(t, "mail", "list", "--as", demoAdmin, "--status", "failed")
		if failed.code != 0 {
			t.Fatalf("--status failed: exit %d, stderr %q", failed.code, failed.stderr)
		}

		golden(t, "mail/list-failed.txt", failed.stdout)

		receipts := r.exec(t, "mail", "list", "--as", demoAdmin, "--template", "receipt")
		if lines := strings.Split(strings.TrimSpace(receipts.stdout), "\n"); receipts.code != 0 || len(lines) != 1 ||
			!strings.HasPrefix(lines[0], failedReceipt.ID.String()) {
			t.Errorf("--template receipt: exit %d, stdout %q", receipts.code, receipts.stdout)
		}
	})
}

// TestMailListRefusals: invalid filters are usage errors naming the flag;
// the mail log needs an admin.
func TestMailListRefusals(t *testing.T) {
	t.Parallel()

	r := cliRun{st: demoStore(t)}

	for _, c := range []struct {
		name   string
		args   []string
		code   int
		stderr string
	}{
		{"status", []string{"--as", demoAdmin, "--status", "lost"}, 2, "error: --status: invalid mail status\n"},
		{"template", []string{"--as", demoAdmin, "--template", "postcard"}, 2, `error: --template: "postcard" is not one of`},
		{"cursor", []string{"--as", demoAdmin, "--cursor", "not-a-cursor"}, 2, `error: --cursor: "not-a-cursor" is no cursor`},
		{"no --as", nil, 2, "acts as an account: give --as"},
		{"unknown", []string{"--as", "nobody@example.org"}, 3, "error: --as nobody@example.org"},
		{"member", []string{"--as", demoMember}, 4, "below admin"},
		{"office", []string{"--as", demoOffice}, 4, "below admin"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			got := r.exec(t, append([]string{"mail", "list"}, c.args...)...)
			if got.code != c.code || !strings.Contains(got.stderr, c.stderr) || got.stdout != "" {
				t.Errorf("exit %d, stdout %q, stderr %q; want %d with %q", got.code, got.stdout, got.stderr, c.code, c.stderr)
			}
		})
	}
}

// TestMailListPaging: a page holds MailLogPageSize mails, a hint on stderr
// gives the next page's cursor, and --cursor lists it.
func TestMailListPaging(t *testing.T) {
	t.Parallel()

	st := demoStore(t)
	r := cliRun{st: st}

	for i := range service.MailLogPageSize + 2 {
		addLogMails(t, st, logMail(100+i, fmt.Sprintf("m%02d@example.org", i), mail.TemplateReceipt, 100-i,
			domain.MailPending))
	}

	first := r.exec(t, "mail", "list", "--as", demoAdmin)

	hint, cursor, ok := strings.Cut(strings.TrimSpace(first.stderr), "--cursor ")
	if first.code != 0 || !ok || hint != "more mails: run again with " ||
		strings.Count(first.stdout, "\n") != service.MailLogPageSize {
		t.Fatalf("first page: exit %d, %d lines, stderr %q", first.code, strings.Count(first.stdout, "\n"), first.stderr)
	}

	next := r.exec(t, "mail", "list", "--as", demoAdmin, "--cursor", cursor)
	if next.code != 0 || next.stderr != "" || strings.Count(next.stdout, "\n") != 2 ||
		!strings.Contains(next.stdout, "m00@example.org") {
		t.Errorf("second page: exit %d, stdout %q, stderr %q", next.code, next.stdout, next.stderr)
	}

	// JSON has the cursor in the page, and no hint.
	js := r.exec(t, "mail", "list", "--as", demoAdmin, "--output", "json")

	var page struct {
		NextCursor string `json:"nextCursor"`
	}
	if err := json.Unmarshal([]byte(js.stdout), &page); err != nil || page.NextCursor != cursor || js.stderr != "" {
		t.Errorf("JSON page: cursor %q (%v), stderr %q; want %q", page.NextCursor, err, js.stderr, cursor)
	}
}

// TestMailRequeue queues a failed mail again on both stores, after the
// confirmation, audited as mail-requeued through the command line.
func TestMailRequeue(t *testing.T) {
	t.Parallel()

	stores(t, func(t *testing.T, r cliRun) {
		st := storeOf(t, r)
		addMailLog(t, st)

		id := failedReceipt.ID.String()

		refused := r.exec(t, "mail", "requeue", "--as", demoAdmin, id)
		if refused.code != 2 || !strings.Contains(refused.stderr, "confirm with --yes") ||
			logMailOf(t, st, failedReceipt.ID).Status() != domain.MailFailed || len(requeueEvents(t, st)) != 0 {
			t.Fatalf("without --yes: exit %d, stderr %q", refused.code, refused.stderr)
		}

		got := r.exec(t, "mail", "requeue", "--as", demoAdmin, "--yes", id, "--output", "json")
		if got.code != 0 || got.stderr != "" {
			t.Fatalf("requeue: exit %d, stderr %q", got.code, got.stderr)
		}

		golden(t, "mail/requeue.json", got.stdout)

		if m := logMailOf(t, st, failedReceipt.ID); m.Status() != domain.MailPending || m.Attempts != 0 {
			t.Errorf("requeued mail %+v, want pending without attempts", m)
		}

		events := requeueEvents(t, st)
		via := domain.AuditVia{Channel: domain.ChannelCLI, OSUser: testOSUser}

		if len(events) != 1 || events[0].ActorID != accountID(t, st, demoAdmin) ||
			events[0].Details.MailID != failedReceipt.ID || events[0].Details.Via != via {
			t.Errorf("audit events %+v, want one mail-requeued by the admin via the command line", events)
		}

		for _, c := range []struct {
			name   string
			args   []string
			code   int
			stderr string
		}{
			{"again", []string{id}, 4, "only a failed mail can be queued again"},
			{"pending", []string{pendingNews.ID.String()}, 4, "only a failed mail"},
			{"secret link", []string{failedInvite.ID.String()}, 4, "a mail with a secret link is not queued again"},
			{"unknown", []string{"00000000-0000-7000-8000-00000000ffff"}, 3, "not found"},
			{"bad id", []string{"mail-1"}, 2, `error: <id>: validation failed: invalid ID "mail-1"`},
			{"both", []string{id, "--failed"}, 2, "not both"},
			{"neither", nil, 2, "give the ID of a failed mail, or --failed"},
		} {
			res := r.exec(t, append([]string{"mail", "requeue", "--as", demoAdmin, "--yes"}, c.args...)...)
			if res.code != c.code || !strings.Contains(res.stderr, c.stderr) {
				t.Errorf("%s: exit %d, stderr %q; want %d with %q", c.name, res.code, res.stderr, c.code, c.stderr)
			}
		}

		if n := len(requeueEvents(t, st)); n != 1 {
			t.Errorf("%d mail-requeued events after the refusals, want 1", n)
		}

		if m := logMailOf(t, st, failedInvite.ID); m.Status() != domain.MailFailed {
			t.Errorf("the invite is %s, want failed", m.Status())
		}
	})
}

// TestMailRequeueText prints what it queued.
func TestMailRequeueText(t *testing.T) {
	t.Parallel()

	r := cliRun{st: demoStore(t)}
	addMailLog(t, r.st)

	got := r.exec(t, "mail", "requeue", "--as", demoAdmin, "--yes", failedReceipt.ID.String())
	if want := "queued mail " + failedReceipt.ID.String() + " to ella.example@example.org again\n"; got.code != 0 ||
		got.stdout != want {
		t.Errorf("exit %d, stdout %q; want %q", got.code, got.stdout, want)
	}
}

// TestMailRequeueFailed queues every failed mail again on both stores,
// each audited; the one with a secret link stays failed.
func TestMailRequeueFailed(t *testing.T) {
	t.Parallel()

	stores(t, func(t *testing.T, r cliRun) {
		st := storeOf(t, r)
		second := logMail(5, "hugo.example@example.org", mail.TemplateLessonCanceled, 1, domain.MailFailed)
		addMailLog(t, st)
		addLogMails(t, st, second)

		refused := r.exec(t, "mail", "requeue", "--as", demoAdmin, "--failed")
		if refused.code != 2 || len(requeueEvents(t, st)) != 0 {
			t.Fatalf("without --yes: exit %d, stderr %q", refused.code, refused.stderr)
		}

		js := r.exec(t, "mail", "requeue", "--as", demoAdmin, "--failed", "--yes", "--output", "json")
		if js.code != 0 || js.stderr != "" {
			t.Fatalf("exit %d, stderr %q", js.code, js.stderr)
		}

		golden(t, "mail/requeue-failed.json", js.stdout)

		if n := len(requeueEvents(t, st)); n != 2 {
			t.Errorf("%d mail-requeued events, want 2", n)
		}

		for id, want := range map[domain.OutboxMailID]domain.MailStatus{
			failedReceipt.ID: domain.MailPending, second.ID: domain.MailPending, failedInvite.ID: domain.MailFailed,
		} {
			if s := logMailOf(t, st, id).Status(); s != want {
				t.Errorf("mail %v is %s, want %s", id, s, want)
			}
		}

		// Only the secret link is left.
		none := r.exec(t, "mail", "requeue", "--as", demoAdmin, "--failed", "--yes")
		if want := "queued 0 failed mails again; 1 mail with a secret link left failed\n"; none.code != 0 ||
			none.stdout != want {
			t.Errorf("again: exit %d, stdout %q; want %q", none.code, none.stdout, want)
		}
	})
}

// TestMailRequeueTerminal asks on a terminal: y queues, anything else
// changes nothing.
func TestMailRequeueTerminal(t *testing.T) {
	t.Parallel()

	r := cliRun{st: demoStore(t), terminal: true, stdin: "n\n"}
	addLogMails(t, r.st, failedReceipt, logMail(6, "ivy.example@example.org", mail.TemplateReceipt, 1,
		domain.MailFailed))

	no := r.exec(t, "mail", "requeue", "--as", demoAdmin, "--failed")
	if no.code != 2 || no.stderr != "Queue every failed mail again? [y/N] error: not confirmed; nothing was changed\n" ||
		len(requeueEvents(t, r.st)) != 0 {
		t.Errorf("n: exit %d, stderr %q", no.code, no.stderr)
	}

	r.stdin = "y\n"

	yes := r.exec(t, "mail", "requeue", "--as", demoAdmin, "--failed")
	if yes.code != 0 || !strings.HasSuffix(yes.stdout, "queued 2 failed mails again\n") ||
		len(requeueEvents(t, r.st)) != 2 {
		t.Errorf("y: exit %d, stdout %q, stderr %q", yes.code, yes.stdout, yes.stderr)
	}

	one := r.exec(t, "mail", "requeue", "--as", demoAdmin, failedReceipt.ID.String())
	if one.code != 4 || !strings.HasPrefix(one.stderr, "Queue mail "+failedReceipt.ID.String()+" again? [y/N] ") {
		t.Errorf("one, already pending: exit %d, stderr %q", one.code, one.stderr)
	}
}

// smtpEnv is the environment of a mail server at srv without TLS.
func smtpEnv(r cliRun, srv *smtpServer) cliRun {
	return r.with("SMTP_HOST", "127.0.0.1", "SMTP_PORT", strconv.Itoa(srv.port()), "SMTP_TLS", "none",
		"SMTP_FROM", "noreply@example.org")
}

// TestMailTest sends the test mail straight through the mail server, in
// English or German, without a database.
func TestMailTest(t *testing.T) {
	t.Parallel()

	srv := startSMTPServer(t, &smtpServer{})
	r := smtpEnv(cliRun{}, srv)

	text := r.exec(t, "mail", "test", "--to", "office@example.org")
	if want := "ok: the test mail to office@example.org went through 127.0.0.1\n"; text.code != 0 ||
		text.stdout != want || text.stderr != "" {
		t.Fatalf("exit %d, stdout %q, stderr %q; want %q", text.code, text.stdout, text.stderr, want)
	}

	js := r.exec(t, "mail", "test", "--to", "office@example.org", "--locale", "de-AT", "--output", "json")
	if js.code != 0 {
		t.Fatalf("JSON: exit %d, stderr %q", js.code, js.stderr)
	}

	golden(t, "mail/test.json", js.stdout)

	got := srv.received()
	if len(got) != 2 || !strings.Contains(got[0], "Subject: Test mail from studio-schedule") ||
		!strings.Contains(got[0], "To: <office@example.org>") ||
		!strings.Contains(got[1], "Subject: Testmail von studio-schedule") {
		t.Errorf("the server received %q, want the English and the German test mail", got)
	}

	if verbs := srv.recorded(); !strings.Contains(verbs, "MAIL RCPT DATA") {
		t.Errorf("the server got %s, want MAIL RCPT DATA", verbs)
	}
}

// TestMailTestFails: a mail server that refuses fails the command (exit 1)
// with its error; without one, or with invalid input, it is a usage error
// and nothing is sent.
func TestMailTestFails(t *testing.T) {
	t.Parallel()

	rcpt := startSMTPServer(t, &smtpServer{rejectRCPT: true})
	login := startSMTPServer(t, &smtpServer{reject: true})

	for _, c := range []struct {
		name   string
		run    cliRun
		args   []string
		code   int
		stderr string
	}{
		{"recipient rejected", smtpEnv(cliRun{}, rcpt), nil, 1, "error: the test mail was not sent: "},
		{
			"login rejected",
			smtpEnv(cliRun{}, login).with("SMTP_USERNAME", "mailer", "SMTP_PASSWORD", "smtp-secret-pw"),
			nil, 1, "535",
		},
		{"no mail server", cliRun{}, nil, 2, "error: SMTP_HOST is not set: there is no mail server to test\n"},
		{
			"invalid mail server", cliRun{}.with("SMTP_HOST", "127.0.0.1", "SMTP_TLS", "ssl",
				"SMTP_FROM", "noreply@example.org"),
			nil, 1, "invalid smtp configuration",
		},
		{"invalid config", smtpEnv(cliRun{}, rcpt).with("LOG_LEVEL", "loud"), nil, 2, "LOG_LEVEL"},
		{"invalid --to", smtpEnv(cliRun{}, rcpt), []string{"--to", "Office <office@example.org>"}, 2, "error: --to: "},
		{"locale", smtpEnv(cliRun{}, rcpt), []string{"--locale", "fr"}, 2, `error: --locale: "fr" is not one of de, en`},
		{"--as", smtpEnv(cliRun{}, rcpt), []string{"--as", demoAdmin}, 2, "error: --as: "},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			args := c.args
			if !strings.Contains(strings.Join(args, " "), "--to") {
				args = append(args, "--to", "office@example.org")
			}

			got := c.run.exec(t, append([]string{"mail", "test"}, args...)...)
			if got.code != c.code || !strings.Contains(got.stderr, c.stderr) || got.stdout != "" {
				t.Errorf("exit %d, stdout %q, stderr %q; want %d with %q", got.code, got.stdout, got.stderr, c.code, c.stderr)
			}

			if strings.Contains(got.stderr, "smtp-secret-pw") {
				t.Errorf("stderr %q has the password", got.stderr)
			}
		})
	}

	if got := rcpt.received(); len(got) != 0 {
		t.Errorf("the refusing server accepted %q", got)
	}

	missing := cliRun{}.exec(t, "mail", "test")
	if missing.code != 2 || !strings.Contains(missing.stderr, `"to" not set`) {
		t.Errorf("without --to: exit %d, stderr %q", missing.code, missing.stderr)
	}
}

// TestMailTestRender: a mail server without a name renders no test mail.
func TestMailTestRender(t *testing.T) {
	t.Parallel()

	r := cliRun{delivery: func(config.Config, clock.Clock, *slog.Logger) (cli.Delivery, error) {
		return cli.Delivery{Mailer: refusingMailer{}}, nil
	}}

	got := r.exec(t, "mail", "test", "--to", "office@example.org")
	if got.code != 2 || !strings.Contains(got.stderr, "a test mail names its mail server") {
		t.Errorf("exit %d, stderr %q", got.code, got.stderr)
	}
}

// refusingMailer fails every mail.
type refusingMailer struct{}

func (refusingMailer) Send(context.Context, mail.Message) error {
	return fmt.Errorf("%w: no", mail.ErrPermanent)
}

// readOnlyStore refuses every transaction that writes.
type readOnlyStore struct{ store.Store }

func (s readOnlyStore) InTx(ctx context.Context, mode store.TxMode, fn func(context.Context, store.Tx) error) error {
	if mode == store.ReadWrite {
		return errors.New("the disk is full")
	}

	return s.Store.InTx(ctx, mode, fn)
}

// TestMailRequeueStoreFails: a store that cannot write fails the command
// (exit 1).
func TestMailRequeueStoreFails(t *testing.T) {
	t.Parallel()

	st := demoStore(t)
	addLogMails(t, st, failedReceipt)

	got := cliRun{st: readOnlyStore{st}}.exec(t, "mail", "requeue", "--as", demoAdmin, "--failed", "--yes")
	if got.code != 1 || !strings.Contains(got.stderr, "the disk is full") || got.stdout != "" {
		t.Errorf("exit %d, stdout %q, stderr %q", got.code, got.stdout, got.stderr)
	}
}
