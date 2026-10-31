package app_test

import (
	"context"
	"errors"
	"log/slog"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/cwbudde/studio-schedule/internal/app"
	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/mail"
	"github.com/cwbudde/studio-schedule/internal/seed"
	"github.com/cwbudde/studio-schedule/internal/store"
	"github.com/cwbudde/studio-schedule/internal/store/memory"
)

// jobsNow is inside the demo term, after the demo's full workshop.
var jobsNow = time.Date(2026, time.October, 23, 8, 0, 0, 0, time.UTC)

// seededStore returns a memory store with the demo data.
func seededStore(t *testing.T) *memory.Store {
	t.Helper()

	st := memory.New()
	if err := seed.LoadDemo(t.Context(), st); err != nil {
		t.Fatal(err)
	}

	return st
}

// writeTx runs fn in a read-write transaction of st.
func writeTx(t *testing.T, st store.Store, fn func(ctx context.Context, tx store.Tx) error) {
	t.Helper()

	if err := st.InTx(t.Context(), store.ReadWrite, fn); err != nil {
		t.Fatal(err)
	}
}

// logHas reports whether logs has a line with msg whose attribute attr
// is a positive number.
func logHas(logs, msg, attr string) bool {
	re := regexp.MustCompile(`msg="` + regexp.QuoteMeta(msg) + `".* ` + attr + `=[1-9]`)

	return re.MatchString(logs)
}

// TestJobsPurgesLogged: the purge jobs log only when they removed
// something. A day after a week's session, a sent mail and the demo's
// guest bookings, nothing is due; 400 days later the session has expired,
// the mail is past the mail retention and the guests past theirs.
func TestJobsPurgesLogged(t *testing.T) {
	t.Parallel()

	st := seededStore(t)
	clk := &stepClock{t: jobsNow}
	account := seed.Demo().Accounts[0].ID
	src := ids.V7{}
	session := domain.Session{
		ID: ids.New[domain.SessionID](src), AccountID: account, TokenHash: [32]byte{0xc3},
		CreatedAt: jobsNow, LastSeenAt: jobsNow, ExpiresAt: jobsNow.AddDate(0, 0, 7),
	}
	sent := domain.OutboxMail{
		ID: ids.New[domain.OutboxMailID](src), To: "ada.fernhill@example.org", Subject: "Hello", Text: "Hi",
		CreatedAt: jobsNow, SentAt: jobsNow,
	}

	writeTx(t, st, func(ctx context.Context, tx store.Tx) error {
		return errors.Join(tx.Sessions().Create(ctx, session), tx.Outbox().Create(ctx, sent))
	})

	var logs syncBuffer

	runner := app.Jobs(app.Deps{
		Store: st, Clock: clk, IDs: src, Logger: slog.New(slog.NewTextHandler(&logs, nil)),
	})

	clk.advance(24 * time.Hour)

	if err := runner.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}

	purges := []string{"expired sessions and links deleted", "old mails purged", "personal data purged"}
	for _, msg := range purges {
		if strings.Contains(logs.String(), msg) {
			t.Errorf("logged %q with nothing to purge:\n%s", msg, logs.String())
		}
	}

	clk.advance(400 * 24 * time.Hour)

	if err := runner.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}

	out := logs.String()
	if !logHas(out, purges[0], "deleted") || !logHas(out, purges[1], "deleted") ||
		!logHas(out, purges[2], "guests") {
		t.Errorf("want the three purges logged with what they removed:\n%s", out)
	}

	err := st.InTx(t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		if _, err := tx.Sessions().GetByToken(ctx, session.TokenHash); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("the expired session: %v, want it deleted", err)
		}

		if _, err := tx.Outbox().Get(ctx, sent.ID); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("the old mail: %v, want it deleted", err)
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// noSettingsStore is a store whose transactions find no school
// settings, as a database with data but without settings (broken data).
type noSettingsStore struct{ store.Store }

func (s noSettingsStore) InTx(ctx context.Context, mode store.TxMode, fn func(context.Context, store.Tx) error) error {
	return s.Store.InTx(ctx, mode, func(ctx context.Context, tx store.Tx) error {
		return fn(ctx, noSettingsTx{tx})
	})
}

type noSettingsTx struct{ store.Tx }

func (tx noSettingsTx) Settings() store.SettingsRepo { return noSettingsRepo{tx.Tx.Settings()} }

type noSettingsRepo struct{ store.SettingsRepo }

func (noSettingsRepo) Get(context.Context) (domain.Settings, error) {
	return domain.Settings{}, domain.ErrNotFound
}

// TestJobsWithDataButNoSettings: waiting-list entries to settle and an
// announcement to email, but no school settings to do it with. The jobs
// leave both alone and succeed, so they do not fail every minute.
func TestJobsWithDataButNoSettings(t *testing.T) {
	t.Parallel()

	st := seededStore(t)
	src := ids.V7{}
	announcement := seed.Demo().Announcements[0]
	announcement.ID, announcement.Email = ids.New[domain.AnnouncementID](src), true
	announcement.PublishAt, announcement.CreatedAt, announcement.UpdatedAt = jobsNow.Add(-time.Hour),
		jobsNow.Add(-time.Hour), jobsNow.Add(-time.Hour)

	writeTx(t, st, func(ctx context.Context, tx store.Tx) error {
		return tx.Announcements().Create(ctx, announcement)
	})

	var logs syncBuffer

	runner := app.Jobs(app.Deps{
		Store: noSettingsStore{st}, Clock: clock.NewFixed(jobsNow), IDs: src,
		Logger: slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
	})

	if err := runner.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce with data but no settings: %v", err)
	}

	err := st.InTx(t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		for _, want := range seed.Demo().WaitlistEntries {
			e, err := tx.Waitlist().Get(ctx, want.ID)
			if err != nil {
				return err
			}

			if e.Status != want.Status {
				t.Errorf("entry %v is %s, want it left %s", e.ID, e.Status, want.Status)
			}
		}

		a, err := tx.Announcements().Get(ctx, announcement.ID)
		if err != nil {
			return err
		}

		if !a.EmailedAt.IsZero() || a.EmailRecipients != 0 {
			t.Errorf("announcement %+v, want it not emailed", a)
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	if out := logs.String(); strings.Contains(out, "waiting lists settled") ||
		strings.Contains(out, "announcement mails queued") {
		t.Errorf("logged work done:\n%s", out)
	}
}

// recordingMailer is a mail.Mailer that keeps what it is given.
type recordingMailer struct {
	mu   sync.Mutex
	sent []mail.Message
}

func (m *recordingMailer) Send(_ context.Context, msg mail.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.sent = append(m.sent, msg)

	return nil
}

func (m *recordingMailer) to() []string {
	m.mu.Lock()
	defer m.mu.Unlock()

	var to []string
	for _, msg := range m.sent {
		to = append(to, msg.To)
	}

	return to
}

// fakePushSender is a service.PushSender that keeps the payloads it is given.
type fakePushSender struct {
	mu       sync.Mutex
	payloads []string
}

func (*fakePushSender) PublicKey() string { return "BTestKey" }

func (p *fakePushSender) Send(_ context.Context, _ domain.PushSubscription, payload []byte, _ time.Duration) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.payloads = append(p.payloads, string(payload))

	return nil
}

func (p *fakePushSender) sent() []string {
	p.mu.Lock()
	defer p.mu.Unlock()

	return slices.Clone(p.payloads)
}

// TestJobsDeliverInBackground: with a mailer and a push sender, the
// deliver-mail and deliver-push jobs exist. They are left out at startup
// (a slow server must not delay it), and send what is queued on their
// first tick, a minute later, logging how much they sent. The test runs
// in a synctest bubble, whose clock jumps to the tick.
func TestJobsDeliverInBackground(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		st := seededStore(t)
		src := ids.V7{}
		queued := domain.OutboxMail{
			ID: ids.New[domain.OutboxMailID](src), To: "ada.fernhill@example.org", Subject: "Hello", Text: "Hi",
			CreatedAt: jobsNow, NextAttemptAt: jobsNow,
		}
		sub := domain.PushSubscription{
			ID: ids.New[domain.PushSubscriptionID](src), AccountID: seed.Demo().Accounts[0].ID,
			Endpoint: "https://push.example.net/push/sub-1", P256DH: [65]byte{4, 1}, Auth: [16]byte{1},
			CreatedAt: jobsNow, LastUsedAt: jobsNow,
		}
		message := domain.PushMessage{
			ID: ids.New[domain.PushMessageID](src), SubscriptionID: sub.ID, Payload: `{"title":"hello"}`,
			CreatedAt: jobsNow, ExpiresAt: jobsNow.Add(time.Hour), NextAttemptAt: jobsNow,
		}

		writeTx(t, st, func(ctx context.Context, tx store.Tx) error {
			return errors.Join(tx.Outbox().Create(ctx, queued), tx.PushSubscriptions().Save(ctx, sub),
				tx.PushOutbox().Create(ctx, message))
		})

		var logs syncBuffer

		mailer, pusher := &recordingMailer{}, &fakePushSender{}
		runner := app.Jobs(app.Deps{
			Store: st, Clock: clock.NewFixed(jobsNow), IDs: src, PDF: fakePDF{},
			Logger: slog.New(slog.NewTextHandler(&logs, nil)), Mailer: mailer, Pusher: pusher,
		})

		if err := runner.RunOnce(t.Context()); err != nil {
			t.Fatal(err)
		}

		if len(mailer.to()) != 0 || len(pusher.sent()) != 0 {
			t.Fatalf("sent at startup: mails to %v, push %v", mailer.to(), pusher.sent())
		}

		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})

		go func() {
			runner.Run(ctx)
			close(done)
		}()

		time.Sleep(time.Minute + time.Second)
		synctest.Wait()
		cancel()
		<-done

		if to := mailer.to(); !slices.Contains(to, "ada.fernhill@example.org") {
			t.Errorf("mails sent to %v, want the queued one among them", to)
		}

		if got := pusher.sent(); !slices.Equal(got, []string{message.Payload}) {
			t.Errorf("push messages sent: %v", got)
		}

		out := logs.String()
		if !logHas(out, "mails sent", "sent") || !strings.Contains(out, `msg="push messages sent" sent=1`) {
			t.Errorf("want the deliveries logged:\n%s", out)
		}
	})
}
