package service_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/mail"
	"github.com/cwbudde/studio-schedule/internal/service"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// fakeMailer records sent mails and fails while err is set. afterSend runs
// after a successful send.
type fakeMailer struct {
	mu        sync.Mutex
	sent      []mail.Message
	err       error
	afterSend func()
}

func (m *fakeMailer) Send(_ context.Context, msg mail.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.err != nil {
		return m.err
	}

	m.sent = append(m.sent, msg)

	if m.afterSend != nil {
		m.afterSend()
	}

	return nil
}

func (m *fakeMailer) fail(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.err = err
}

// outboxFixture is an auth fixture with one confirmation mail in the outbox.
func outboxFixture(t *testing.T) (*authFixture, *fakeMailer, *service.OutboxService) {
	t.Helper()

	f := newAuthFixture(t)
	f.register("ada@example.org", "correct horse")

	mailer := &fakeMailer{}

	return f, mailer, service.NewOutboxService(f.st, f.clock, mailer)
}

func (f *authFixture) onlyMail() domain.OutboxMail {
	f.t.Helper()

	mails := f.mails()
	if len(mails) != 1 {
		f.t.Fatalf("want one mail, got %d", len(mails))
	}

	return mails[0]
}

func deliver(t *testing.T, o *service.OutboxService) int {
	t.Helper()

	n, err := o.Deliver(t.Context())
	if err != nil {
		t.Fatalf("Deliver: %v", err)
	}

	return n
}

func TestOutboxDelivers(t *testing.T) {
	t.Parallel()

	f, mailer, o := outboxFixture(t)
	queued := f.onlyMail()

	if n := deliver(t, o); n != 1 {
		t.Fatalf("Deliver sent %d, want 1", n)
	}

	if len(mailer.sent) != 1 || mailer.sent[0].To != "ada@example.org" || mailer.sent[0].Subject != queued.Subject ||
		mailer.sent[0].Text != queued.Text || mailer.sent[0].HTML != queued.HTML {
		t.Fatalf("sent %+v, want the queued mail", mailer.sent)
	}

	m := f.onlyMail()
	if m.Status() != domain.MailSent || m.Attempts != 1 || !m.SentAt.Equal(f.clock.Now()) {
		t.Fatalf("mail = %+v, want sent after one attempt", m)
	}

	if n := deliver(t, o); n != 0 || len(mailer.sent) != 1 {
		t.Fatal("a sent mail was sent again")
	}
}

func TestOutboxRetriesThenGivesUp(t *testing.T) {
	t.Parallel()

	f, mailer, o := outboxFixture(t)
	mailer.fail(errors.New("421 try again later"))

	for attempt := 1; attempt <= service.MaxMailAttempts; attempt++ {
		if n := deliver(t, o); n != 0 {
			t.Fatalf("attempt %d: sent %d", attempt, n)
		}

		m := f.onlyMail()
		if m.Attempts != attempt || m.LastError != "421 try again later" {
			t.Fatalf("attempt %d: mail = %+v", attempt, m)
		}

		if attempt < service.MaxMailAttempts {
			if m.Status() != domain.MailPending || !m.NextAttemptAt.After(f.clock.Now()) {
				t.Fatalf("attempt %d: want a later retry, got %+v", attempt, m)
			}

			// Not due yet: nothing happens.
			deliver(t, o)

			if got := f.onlyMail(); got.Attempts != attempt {
				t.Fatalf("attempt %d: retried before it was due", attempt)
			}

			f.clock.advance(m.NextAttemptAt.Sub(f.clock.Now()))
		}
	}

	if m := f.onlyMail(); m.Status() != domain.MailFailed {
		t.Fatalf("after %d attempts: status %s, want failed", service.MaxMailAttempts, m.Status())
	}
}

func TestOutboxPermanentFailure(t *testing.T) {
	t.Parallel()

	f, mailer, o := outboxFixture(t)
	mailer.fail(fmt.Errorf("%w: 550 no such user", mail.ErrPermanent))

	deliver(t, o)

	if m := f.onlyMail(); m.Status() != domain.MailFailed || m.Attempts != 1 {
		t.Fatalf("mail = %+v, want failed after one attempt", m)
	}
}

func TestOutboxRecoversAfterFailure(t *testing.T) {
	t.Parallel()

	f, mailer, o := outboxFixture(t)
	mailer.fail(errors.New("connection refused"))
	deliver(t, o)

	mailer.fail(nil)
	f.clock.advance(time.Hour)

	if n := deliver(t, o); n != 1 {
		t.Fatalf("sent %d, want 1", n)
	}

	if m := f.onlyMail(); m.Status() != domain.MailSent || m.LastError != "" || m.Attempts != 2 {
		t.Fatalf("mail = %+v, want sent on the second attempt", m)
	}
}

func TestOutboxWithoutMailer(t *testing.T) {
	t.Parallel()

	f := newAuthFixture(t)
	f.register("ada@example.org", "correct horse")
	o := service.NewOutboxService(f.st, f.clock, nil)

	if n := deliver(t, o); n != 0 {
		t.Fatalf("sent %d without a mailer", n)
	}

	mails, err := o.List(t.Context(), 10)
	if err != nil || len(mails) != 1 || mails[0].Status() != domain.MailPending {
		t.Fatalf("List = %+v, %v; want the pending mail", mails, err)
	}
}

func TestOutboxRollbackSendsNothing(t *testing.T) {
	t.Parallel()

	// A registration that fails after writing its account leaves no mail.
	f := newAuthFixture(t)

	err := f.st.InTx(t.Context(), store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		s, err := tx.Settings().Get(ctx)
		s.SchoolName = "" // the confirmation mail cannot be rendered

		return errors.Join(err, tx.Settings().Put(ctx, s))
	})
	if err != nil {
		t.Fatal(err)
	}

	err = f.svc.Register(t.Context(), service.Registration{
		GivenName: "Ada", FamilyName: "L", Email: "ada@example.org", Password: "correct horse",
	})
	if err == nil {
		t.Fatal("want an error without a school name")
	}

	if n := len(f.mails()); n != 0 {
		t.Fatalf("a failed registration left %d mails", n)
	}

	_, err = f.svc.Login(t.Context(), "ada@example.org", "correct horse")
	wantIs(t, err, service.ErrInvalidCredentials) // no account either
}

func TestOutboxRecordsSendDespiteCancel(t *testing.T) {
	t.Parallel()

	f, mailer, o := outboxFixture(t)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	mailer.afterSend = cancel // shutdown right after the server accepted the mail

	// The next claim sees the canceled context.
	if _, err := o.Deliver(ctx); err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("Deliver: %v", err)
	}

	if m := f.onlyMail(); m.Status() != domain.MailSent {
		t.Fatalf("mail = %+v, want it recorded as sent", m)
	}
}

func TestOutboxBatchClaimsOneAtATime(t *testing.T) {
	t.Parallel()

	f, mailer, o := outboxFixture(t)
	f.register("bea@example.org", "correct horse")

	// While the first mail is being sent, the second is still unclaimed.
	mailer.afterSend = func() {
		if len(mailer.sent) == 1 {
			for _, m := range f.mails() {
				if m.To == "bea@example.org" && m.Attempts != 0 {
					t.Error("the second mail was claimed before the first was sent")
				}
			}
		}
	}

	if n := deliver(t, o); n != 2 {
		t.Fatalf("sent %d, want 2", n)
	}
}
