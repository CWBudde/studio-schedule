package service

import (
	"context"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/mail"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// failedReceiptMail sells Anna a pass and marks its receipt mail failed.
func failedReceiptMail(t *testing.T, f *passFixture) domain.OutboxMail {
	t.Helper()

	m := receiptMail(t, f)
	m.Attempts, m.NextAttemptAt, m.LastError = MaxMailAttempts, time.Time{}, "550 no such user"
	f.write(t, func(ctx context.Context, tx store.Tx) error { return tx.Outbox().Update(ctx, m) })

	return m
}

// TestMailLogAttachments: the log names the PDF a mail attaches, in the
// list, a single mail and a requeued one.
func TestMailLogAttachments(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newPassFixture(t, domain.RedeemOnAttendance)
	m := failedReceiptMail(t, f)
	svc := NewMailLogService(f.st, clock.NewFixed(mustInstant(t, bookingNow)), f.seq)

	page, err := svc.List(ctx, MailLogQuery{Template: mail.TemplateReceipt})
	if err != nil || len(page.Mails) != 1 || page.Mails[0].Attachment != "SD-2026-0001.pdf" {
		t.Errorf("list %+v, %v; want the receipt mail with its PDF", page.Mails, err)
	}

	if got, err := svc.Mail(ctx, m.ID); err != nil || got.Attachment != "SD-2026-0001.pdf" || got.Mail.Text == "" {
		t.Errorf("mail %+v, %v; want its PDF and body", got, err)
	}

	got, err := svc.Requeue(ctx, f.office, m.ID)
	if err != nil || got.Attachment != "SD-2026-0001.pdf" || got.Mail.Status() != domain.MailPending {
		t.Errorf("requeued %+v, %v; want it pending with its PDF", got, err)
	}
}

// TestMailLogStoreFaults: the log's reads return a store failure; a failed
// requeue (of one mail or of every failed one) neither queues a mail again
// nor records it in the audit log;
// a failed purge keeps every body and mail.
func TestMailLogStoreFaults(t *testing.T) {
	t.Parallel()

	now := mustInstant(t, bookingNow)

	sweep := func(op func(t *testing.T, svc *MailLogService, f *passFixture, m domain.OutboxMail) error) FaultSweep {
		return FaultSweep{Setup: func(t *testing.T) (*FaultStore, func() error) {
			f := newPassFixture(t, domain.RedeemOnAttendance)
			m := failedReceiptMail(t, f)
			// Old enough for both purges.
			lf := &mailLogFixture{f.bookingFixture}
			lf.addMail(t, "old@example.org", mail.TemplateReceipt, now.Add(-MailRetention-time.Hour), domain.MailSent)
			lf.addMail(t, "sent@example.org", mail.TemplateReceipt, now.Add(-MailBodyRetention-time.Hour), domain.MailSent)

			fs := NewFaultStore(f.st)
			svc := NewMailLogService(fs, clock.NewFixed(now), f.seq)

			return fs, func() error { return op(t, svc, f, m) }
		}}
	}

	for name, op := range map[string]func(t *testing.T, svc *MailLogService, f *passFixture, m domain.OutboxMail) error{
		"List": func(t *testing.T, svc *MailLogService, _ *passFixture, _ domain.OutboxMail) error {
			_, err := svc.List(t.Context(), MailLogQuery{})

			return err
		},
		"Mail": func(t *testing.T, svc *MailLogService, _ *passFixture, m domain.OutboxMail) error {
			_, err := svc.Mail(t.Context(), m.ID)

			return err
		},
		"Requeue": func(t *testing.T, svc *MailLogService, f *passFixture, m domain.OutboxMail) error {
			_, err := svc.Requeue(t.Context(), f.office, m.ID)

			return err
		},
		"RequeueFailed": func(t *testing.T, svc *MailLogService, f *passFixture, _ domain.OutboxMail) error {
			c, err := svc.RequeueFailed(t.Context(), f.office)
			if err == nil && c.Requeued != 1 {
				t.Fatalf("requeued %+v, want the failed receipt mail", c)
			}

			return err
		},
		"Purge": func(t *testing.T, svc *MailLogService, _ *passFixture, _ domain.OutboxMail) error {
			bodies, deleted, err := svc.Purge(t.Context())
			// The old mail loses its body before it goes.
			if err == nil && (bodies != 2 || deleted != 1) {
				t.Fatalf("purged %d bodies, %d mails; want 2, 1", bodies, deleted)
			}

			return err
		},
		"Unsent": func(t *testing.T, svc *MailLogService, _ *passFixture, _ domain.OutboxMail) error {
			c, err := svc.Unsent(t.Context())
			if err == nil && c.Failed != 1 {
				t.Fatalf("counts %+v, want the failed receipt mail", c)
			}

			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			SweepFaults(t, sweep(op))
		})
	}
}
