package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/mail"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// mailLogFixture is the booking fixture with mails of its own in the outbox.
type mailLogFixture struct {
	*bookingFixture
}

// addMail stores a mail of the template created at created; status sets
// its state at that instant.
func (f *mailLogFixture) addMail(
	t *testing.T, to, template string, created time.Time, status domain.MailStatus,
) domain.OutboxMail {
	t.Helper()

	m := domain.OutboxMail{
		ID: ids.New[domain.OutboxMailID](f.seq), To: to, Subject: "About " + template, Text: "Secret text of " + to,
		HTML: "<p>Body</p>", Template: template, CreatedAt: created, NextAttemptAt: created,
	}

	switch status {
	case domain.MailSent:
		m.SentAt, m.NextAttemptAt, m.Attempts = created.Add(time.Minute), time.Time{}, 1
	case domain.MailFailed:
		m.NextAttemptAt, m.Attempts, m.LastError = time.Time{}, MaxMailAttempts, "550 no such user"
	}

	f.write(t, func(ctx context.Context, tx store.Tx) error { return tx.Outbox().Create(ctx, m) })

	return m
}

func (f *mailLogFixture) service(at time.Time) *MailLogService {
	return NewMailLogService(f.st, clock.NewFixed(at), f.seq)
}

func TestMailLogList(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := &mailLogFixture{newBookingFixture(t)}
	now := mustInstant(t, bookingNow)

	for i := range MailLogPageSize + 2 {
		f.addMail(t, fmt.Sprintf("m%02d@example.org", i), mail.TemplateReceipt, now.Add(time.Duration(i)*time.Second),
			domain.MailPending)
	}

	reset := f.addMail(t, "anna@example.org", mail.TemplateResetPassword, now.Add(time.Hour), domain.MailSent)

	svc := f.service(now)

	page, err := svc.List(ctx, MailLogQuery{})
	if err != nil {
		t.Fatal(err)
	}

	if len(page.Mails) != MailLogPageSize || page.Mails[0].Mail.ID != reset.ID || page.Next.IsZero() {
		t.Fatalf("first page: %d mails, first %v, next %+v", len(page.Mails), page.Mails[0].Mail.ID, page.Next)
	}

	// The list never carries bodies.
	for _, e := range page.Mails {
		if e.Mail.Text != "" || e.Mail.HTML != "" {
			t.Fatalf("list carries the body of %v", e.Mail.ID)
		}
	}

	if page.Mails[0].Body != MailBodyWithheld || page.Mails[1].Body != MailBodyShown {
		t.Errorf("bodies %s, %s; want withheld, shown", page.Mails[0].Body, page.Mails[1].Body)
	}

	next, err := svc.List(ctx, MailLogQuery{After: page.Next})
	if err != nil {
		t.Fatal(err)
	}

	if len(next.Mails) != 3 || !next.Next.IsZero() || next.Mails[2].Mail.To != "m00@example.org" {
		t.Errorf("second page: %+v", next)
	}

	filtered, err := svc.List(ctx, MailLogQuery{Status: domain.MailSent, Recipient: "ANNA"})
	if err != nil || len(filtered.Mails) != 1 || filtered.Mails[0].Mail.ID != reset.ID {
		t.Errorf("filtered: %+v, %v", filtered, err)
	}

	_, err = svc.List(ctx, MailLogQuery{Status: "lost"})
	wantFieldError(t, err, "status")
}

// TestMailLogBodies: the bodies of the secret-link templates never leave
// the service, others' do until the purge removes them.
func TestMailLogBodies(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := &mailLogFixture{newBookingFixture(t)}
	now := mustInstant(t, bookingNow)
	svc := f.service(now)

	for _, template := range mail.TemplateNames() {
		m := f.addMail(t, "anna@example.org", template, now, domain.MailSent)

		got, err := svc.Mail(ctx, m.ID)
		if err != nil {
			t.Fatal(err)
		}

		secret := mail.SecretLink(template)
		if hidden := got.Mail.Text == "" && got.Mail.HTML == ""; hidden != secret {
			t.Errorf("%s: body hidden %v, want %v", template, hidden, secret)
		}

		if want := map[bool]MailBody{true: MailBodyWithheld, false: MailBodyShown}[secret]; got.Body != want {
			t.Errorf("%s: body %s, want %s", template, got.Body, want)
		}
	}

	if _, err := svc.Mail(ctx, domain.OutboxMailID{9}); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown mail: %v", err)
	}
}

func TestMailLogRequeue(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := &mailLogFixture{newBookingFixture(t)}
	now := mustInstant(t, bookingNow)
	svc := f.service(now.Add(time.Hour))
	admin := f.principal(t, f.olga)

	failed := f.addMail(t, "anna@example.org", mail.TemplateReceipt, now, domain.MailFailed)
	failedLink := f.addMail(t, "anna@example.org", mail.TemplateInvite, now, domain.MailFailed)
	pending := f.addMail(t, "anna@example.org", mail.TemplateReceipt, now, domain.MailPending)
	sent := f.addMail(t, "anna@example.org", mail.TemplateReceipt, now, domain.MailSent)

	got, err := svc.Requeue(ctx, admin, failed.ID)
	if err != nil {
		t.Fatal(err)
	}

	if got.Mail.Status() != domain.MailPending || got.Mail.Attempts != 0 ||
		!got.Mail.NextAttemptAt.Equal(now.Add(time.Hour)) || got.Mail.Text != "" {
		t.Errorf("requeued: %+v", got.Mail)
	}

	for name, tc := range map[string]struct {
		id   domain.OutboxMailID
		want error
	}{
		"secret link": {failedLink.ID, ErrSecretLinkMail},
		"pending":     {pending.ID, ErrMailNotFailed},
		"sent":        {sent.ID, ErrMailNotFailed},
		"twice":       {failed.ID, ErrMailNotFailed},
		"unknown":     {domain.OutboxMailID{9}, domain.ErrNotFound},
	} {
		if _, err := svc.Requeue(ctx, admin, tc.id); !errors.Is(err, tc.want) {
			t.Errorf("%s: %v, want %v", name, err, tc.want)
		}
	}

	// Recorded once, in the audit log, with the mail.
	var events []domain.AuditEvent

	f.write(t, func(ctx context.Context, tx store.Tx) error {
		var err error
		events, err = tx.Audit().List(ctx, 10)

		return err
	})

	want := domain.AuditEvent{
		ID: events[0].ID, At: now.Add(time.Hour), ActorID: admin.AccountID, Action: domain.AuditMailRequeued,
		Details: domain.AuditDetails{MailID: failed.ID, Recipient: "anna@example.org", Template: mail.TemplateReceipt},
	}
	if len(events) != 1 || events[0] != want {
		t.Errorf("audit events %+v, want %+v", events, want)
	}
}

// TestMailLogPurge: the bodies of mails sent more than 30 days ago go, the
// mails created more than 180 days ago too.
func TestMailLogPurge(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := &mailLogFixture{newBookingFixture(t)}
	now := mustInstant(t, bookingNow)

	// Sent a minute after creation (addMail).
	sentEdge := f.addMail(t, "a@example.org", mail.TemplateReceipt, now.Add(-MailBodyRetention-time.Minute), domain.MailSent)
	sentOld := f.addMail(t, "b@example.org", mail.TemplateReceipt, now.Add(-MailBodyRetention-time.Hour), domain.MailSent)
	failedOld := f.addMail(t, "c@example.org", mail.TemplateReceipt, now.Add(-MailBodyRetention-time.Hour), domain.MailFailed)
	keptRow := f.addMail(t, "d@example.org", mail.TemplateReceipt, now.Add(-MailRetention), domain.MailSent)
	goneRow := f.addMail(t, "e@example.org", mail.TemplateReceipt, now.Add(-MailRetention-time.Microsecond), domain.MailFailed)

	bodies, deleted, err := f.service(now).Purge(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if bodies != 2 || deleted != 1 {
		t.Errorf("purged %d bodies, deleted %d mails; want 2, 1", bodies, deleted)
	}

	svc := f.service(now)

	for m, want := range map[domain.OutboxMail]MailBody{
		sentEdge: MailBodyShown, sentOld: MailBodyPurged, failedOld: MailBodyShown, keptRow: MailBodyPurged,
	} {
		got, err := svc.Mail(ctx, m.ID)
		if err != nil || got.Body != want {
			t.Errorf("%s: %s, %v; want %s", m.To, got.Body, err, want)
		}
	}

	if _, err := svc.Mail(ctx, goneRow.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("old mail: %v, want deleted", err)
	}
}
