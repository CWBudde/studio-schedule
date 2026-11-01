package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/cmd/studio-schedule/cli"
	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/config"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/mail"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// TestJobsList lists the jobs before and after runs, and runs one, on
// both stores: the latest run with its start in the school's time zone.
func TestJobsList(t *testing.T) {
	t.Parallel()

	stores(t, func(t *testing.T, r cliRun) {
		never := r.exec(t, "jobs", "list")
		if never.code != cli.ExitOK || never.stderr != "" {
			t.Fatalf("jobs list: exit %d, stderr %q", never.code, never.stderr)
		}

		golden(t, "jobs/list-never.txt", never.stdout)

		run := r.exec(t, "jobs", "run", "generate-lessons")
		if run.code != cli.ExitOK || run.stderr != "" {
			t.Fatalf("jobs run: exit %d, stderr %q", run.code, run.stderr)
		}

		golden(t, "jobs/run.txt", run.stdout)

		js := r.exec(t, "jobs", "run", "purge-sessions", "--output", "json")
		if js.code != cli.ExitOK {
			t.Fatalf("jobs run --output json: exit %d, stderr %q", js.code, js.stderr)
		}

		golden(t, "jobs/run.json", js.stdout)

		golden(t, "jobs/list.txt", r.exec(t, "jobs", "list").stdout)
		golden(t, "jobs/list.json", r.exec(t, "jobs", "list", "-o", "json").stdout)
	})
}

// jobsMailer records the mails it sends.
type jobsMailer struct {
	mu   sync.Mutex
	sent []mail.Message
}

func (m *jobsMailer) Send(_ context.Context, msg mail.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.sent = append(m.sent, msg)

	return nil
}

// jobsPusher accepts every push message.
type jobsPusher struct{}

func (jobsPusher) PublicKey() string { return "BPublicKeyOfTheJobsTests" }

func (jobsPusher) Send(context.Context, domain.PushSubscription, []byte, time.Duration) error {
	return nil
}

// deliverThrough is a cli.DeliveryFunc with m and a push sender.
func deliverThrough(m mail.Mailer) cli.DeliveryFunc {
	return func(config.Config, clock.Clock, *slog.Logger) (cli.Delivery, error) {
		return cli.Delivery{Mailer: m, Pusher: jobsPusher{}}, nil
	}
}

// jobsQueueMail stores a pending mail to to, due at testNow, and returns it.
func jobsQueueMail(t *testing.T, st store.Store, to string) domain.OutboxMail {
	t.Helper()

	m := domain.OutboxMail{
		ID: ids.New[domain.OutboxMailID](ids.NewSequence()), To: to, Subject: "Costume fitting",
		Text: "Please bring the shoes.", CreatedAt: testNow.Add(-time.Hour), NextAttemptAt: testNow.Add(-time.Hour),
	}

	err := st.InTx(t.Context(), store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		return tx.Outbox().Create(ctx, m)
	})
	if err != nil {
		t.Fatal(err)
	}

	return m
}

// jobsOutboxMail returns the mail id of st.
func jobsOutboxMail(t *testing.T, st store.Store, id domain.OutboxMailID) domain.OutboxMail {
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

// TestJobsDelivery: with the senders that serve would have, the jobs
// include deliver-mail and deliver-push, and deliver-mail sends the due
// mail through the mailer, on both stores.
func TestJobsDelivery(t *testing.T) {
	t.Parallel()

	stores(t, func(t *testing.T, r cliRun) {
		mailer := &jobsMailer{}
		r.delivery = deliverThrough(mailer)
		st := storeOf(t, r)
		m := jobsQueueMail(t, st, "ella.example@example.org")

		golden(t, "jobs/list-delivery.txt", r.exec(t, "jobs", "list").stdout)

		res := r.exec(t, "jobs", "run", "deliver-mail")
		if res.code != cli.ExitOK || !strings.Contains(res.stdout, "deliver-mail") {
			t.Fatalf("jobs run deliver-mail: exit %d, stdout %q, stderr %q", res.code, res.stdout, res.stderr)
		}

		if len(mailer.sent) != 1 || mailer.sent[0].To != m.To || mailer.sent[0].Subject != m.Subject {
			t.Errorf("sent %+v, want the queued mail", mailer.sent)
		}

		if got := jobsOutboxMail(t, st, m.ID); got.Status() != domain.MailSent {
			t.Errorf("the mail is %s, want sent", got.Status())
		}
	})
}

// TestJobsSenders builds the senders from the environment as serve does:
// deliver-mail with SMTP_HOST, and an SMTP configuration the sender
// refuses fails the command.
func TestJobsSenders(t *testing.T) {
	t.Parallel()

	r := cliRun{st: demoStore(t)}.with("SMTP_HOST", "mail.example.test", "SMTP_FROM", "noreply@example.org")

	res := r.exec(t, "jobs", "list")
	if res.code != cli.ExitOK || !strings.Contains(res.stdout, "deliver-mail") ||
		strings.Contains(res.stdout, "deliver-push") {
		t.Errorf("jobs list with SMTP_HOST: exit %d, stdout %q, stderr %q", res.code, res.stdout, res.stderr)
	}

	// A login over plain SMTP is only for a mail server on localhost.
	refused := r.with("SMTP_TLS", "none", "SMTP_USERNAME", "mailer", "SMTP_PASSWORD", "smtp-secret-pw")

	for _, args := range [][]string{{"jobs", "list"}, {"jobs", "run", "purge-sessions"}} {
		res = refused.exec(t, args...)
		if res.code != cli.ExitFailed || !strings.Contains(res.stderr, "error: mail server:") ||
			strings.Contains(res.stderr, "smtp-secret-pw") {
			t.Errorf("%v with plain SMTP to a remote host: exit %d, stderr %q", args, res.code, res.stderr)
		}
	}
}

// TestNewDelivery builds no senders without a configuration, and fails
// with a web push configuration the sender refuses (config.Load refuses
// it before, so only a Config built by hand has one).
func TestNewDelivery(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.DiscardHandler)

	if d, err := newDelivery(config.Config{}, clock.System{}, logger); err != nil || d != (cli.Delivery{}) {
		t.Errorf("newDelivery without a configuration = %+v, %v; want no senders", d, err)
	}

	_, err := newDelivery(config.Config{Push: config.Push{PrivateKey: "not base64!"}}, clock.System{}, logger)
	if err == nil || !strings.HasPrefix(err.Error(), "web push: ") {
		t.Errorf("newDelivery with invalid VAPID keys = %v, want a web push error", err)
	}
}

// TestJobsRunRefused: an unknown job is not found (exit 3) and lists the
// jobs; a job whose lock another holder has is a conflict (exit 4) and
// recorded as skipped; on both stores.
func TestJobsRunRefused(t *testing.T) {
	t.Parallel()

	stores(t, func(t *testing.T, r cliRun) {
		res := r.exec(t, "jobs", "run", "tidy-up")
		if res.code != cli.ExitNotFound ||
			res.stderr != `error: not found: no job "tidy-up"; the jobs are generate-lessons, purge-sessions, `+
				`waitlist-expiry, announcements, purge-mail, purge-personal-data`+"\n" {
			t.Errorf("unknown job: exit %d, stderr %q", res.code, res.stderr)
		}

		// Another process (on PostgreSQL another connection) holds the lock.
		holder := r.st
		if holder == nil {
			holder = openTestStore(t, r.env["DATABASE_URL"])
		}

		release, ok, err := holder.TryLockJob(t.Context(), "generate-lessons")
		if err != nil || !ok {
			t.Fatalf("TryLockJob = %v, %v", ok, err)
		}

		res = r.exec(t, "jobs", "run", "generate-lessons")
		release()

		if res.code != cli.ExitConflict || res.stdout != "" ||
			res.stderr != `error: conflict: job "generate-lessons" is running elsewhere`+"\n" {
			t.Errorf("held lock: exit %d, stdout %q, stderr %q", res.code, res.stdout, res.stderr)
		}

		if list := r.exec(t, "jobs", "list").stdout; !strings.Contains(list, "skipped") {
			t.Errorf("the skipped run is not listed:\n%s", list)
		}
	})
}

// jobRunsFailingStore fails to read the recorded job runs.
type jobRunsFailingStore struct{ store.Store }

func (s jobRunsFailingStore) InTx(ctx context.Context, mode store.TxMode, fn func(context.Context, store.Tx) error) error {
	return s.Store.InTx(ctx, mode, func(ctx context.Context, tx store.Tx) error { return fn(ctx, jobRunsFailingTx{tx}) })
}

type jobRunsFailingTx struct{ store.Tx }

func (tx jobRunsFailingTx) JobRuns() store.JobRunRepo { return failingJobRuns{tx.Tx.JobRuns()} }

type failingJobRuns struct{ store.JobRunRepo }

func (failingJobRuns) Latest(context.Context) ([]domain.JobRun, error) {
	return nil, errors.New("the disk is unreadable")
}

// TestJobsListFails reports runs it cannot read, with exit status 1.
func TestJobsListFails(t *testing.T) {
	t.Parallel()

	res := cliRun{st: jobRunsFailingStore{demoStore(t)}}.exec(t, "jobs", "list")
	if res.code != cli.ExitFailed || res.stdout != "" ||
		res.stderr != "error: job statuses: the disk is unreadable\n" {
		t.Errorf("exit %d, stdout %q, stderr %q", res.code, res.stdout, res.stderr)
	}
}

// lockFailingStore cannot be asked for job locks; its error wraps a
// conflict, which a failed run must not turn into exit status 4.
type lockFailingStore struct{ store.Store }

func (lockFailingStore) TryLockJob(context.Context, string) (func(), bool, error) {
	return nil, false, fmt.Errorf("%w: the lock table is busy", domain.ErrConflict)
}

// TestJobsRunFailed prints the failed run and exits with status 1, as text
// and as JSON with the error as a problem document.
func TestJobsRunFailed(t *testing.T) {
	t.Parallel()

	r := cliRun{st: lockFailingStore{demoStore(t)}}

	res := r.exec(t, "jobs", "run", "generate-lessons")
	if res.code != cli.ExitFailed || res.stderr != `error: the job failed: job "generate-lessons": lock: conflict: `+
		`the lock table is busy`+"\n" {
		t.Errorf("failed job: exit %d, stderr %q", res.code, res.stderr)
	}

	golden(t, "jobs/run-failed.txt", res.stdout)

	res = r.exec(t, "jobs", "run", "generate-lessons", "--output", "json")

	var (
		run     jobRun
		problem struct{ Status int }
	)

	if err := json.Unmarshal([]byte(res.stdout), &run); err != nil || run.Result != string(domain.JobFailed) {
		t.Errorf("stdout %q (%v), want the failed run", res.stdout, err)
	}

	if err := json.Unmarshal([]byte(res.stderr), &problem); err != nil || problem.Status != 500 || res.code != 1 {
		t.Errorf("exit %d, stderr %q (%v), want a problem with status 500", res.code, res.stderr, err)
	}
}

// TestJobsUsage: the jobs are operator commands (no --as), need a
// database and a job name.
func TestJobsUsage(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		run    cliRun
		args   []string
		stderr string
	}{
		"as":          {cliRun{st: demoStore(t)}, []string{"jobs", "list", "--as", demoAdmin}, "acts as no account"},
		"no database": {cliRun{}, []string{"jobs", "list"}, "no database URL"},
		"no name":     {cliRun{st: demoStore(t)}, []string{"jobs", "run"}, "accepts 1 arg(s)"},
		"unknown":     {cliRun{st: demoStore(t)}, []string{"jobs", "start"}, `unknown command "start"`},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			res := c.run.exec(t, c.args...)
			if res.code != cli.ExitUsage || !strings.Contains(res.stderr, c.stderr) {
				t.Errorf("exit %d, stderr %q; want 2 with %q", res.code, res.stderr, c.stderr)
			}
		})
	}
}

// TestEvery drops the zero minutes and seconds of an interval.
func TestEvery(t *testing.T) {
	t.Parallel()

	for d, want := range map[time.Duration]string{
		time.Minute: "1m", 10 * time.Minute: "10m", time.Hour: "1h", 24 * time.Hour: "24h",
		90 * time.Minute: "1h30m", 30 * time.Second: "30s",
	} {
		if got := every(d); got != want {
			t.Errorf("every(%v) = %q, want %q", d, got, want)
		}
	}
}
