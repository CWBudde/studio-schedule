package service

import (
	"context"
	"fmt"
	"time"

	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/mail"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// The mail log (ADR 0014): the outbox as admins see it. Every mail's
// metadata, the bodies of all but the mails with a secret link, a failed
// mail queued again, and the purge that keeps the outbox from holding
// personal data forever.

const (
	// MailLogPageSize is how many mails one page of the log lists at most.
	MailLogPageSize = 50
	// MailBodyRetention is how long the bodies of sent mails are kept.
	MailBodyRetention = 30 * 24 * time.Hour
	// MailRetention is how long mails are kept at all.
	MailRetention = 180 * 24 * time.Hour
)

var (
	// ErrMailNotFailed refuses to queue a mail again that is pending or
	// sent.
	ErrMailNotFailed = fmt.Errorf("%w: only a failed mail can be queued again", domain.ErrConflict)
	// ErrSecretLinkMail refuses to queue a mail with a secret link again:
	// its link may be stale or used; the flows that send a new link do it
	// properly (reset password, a new invite or confirmation).
	ErrSecretLinkMail = fmt.Errorf("%w: a mail with a secret link is not queued again", domain.ErrConflict)
)

// MailBody tells whether the log shows a mail's body.
type MailBody string

// Mail bodies.
const (
	MailBodyShown    MailBody = "shown"
	MailBodyWithheld MailBody = "withheld" // a secret link; never shown
	MailBodyPurged   MailBody = "purged"   // removed after MailBodyRetention
)

// MailLogEntry is a mail of the log. Its Text and HTML are set only by
// MailLogService.Mail, and only with Body MailBodyShown.
type MailLogEntry struct {
	Mail domain.OutboxMail
	// Attachment is the file name of the attached PDF, empty without one.
	Attachment string
	Body       MailBody
}

// MailCursor is where a page of the log continues: after the mail with
// this creation time and ID. The zero cursor is the start.
type MailCursor struct {
	CreatedAt time.Time
	ID        domain.OutboxMailID
}

// IsZero reports whether c is the start.
func (c MailCursor) IsZero() bool { return c.CreatedAt.IsZero() }

// MailLogQuery selects a page of the log.
type MailLogQuery struct {
	// Status, Template and Recipient filter as store.OutboxFilter does.
	Status    domain.MailStatus
	Template  string
	Recipient string
	After     MailCursor
}

// MailLogPage is a page of the log, newest first; Next is the cursor of
// the following page, zero after the last.
type MailLogPage struct {
	Mails []MailLogEntry
	Next  MailCursor
}

// MailLogService lists the outbox for admins, queues failed mails again and
// purges old ones.
type MailLogService struct {
	store store.Store
	clock clock.Clock
	ids   ids.Source
}

// NewMailLogService returns a MailLogService.
func NewMailLogService(st store.Store, clk clock.Clock, src ids.Source) *MailLogService {
	return &MailLogService{store: st, clock: clk, ids: src}
}

// List returns a page of the mails q selects, newest first, without their
// bodies.
func (s *MailLogService) List(ctx context.Context, q MailLogQuery) (MailLogPage, error) {
	if q.Status != "" && !q.Status.Valid() {
		return MailLogPage{}, &domain.FieldError{Field: "status", Msg: "invalid mail status"}
	}

	var page MailLogPage

	err := s.store.InTx(ctx, store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		// One more than a page tells whether another page follows.
		mails, err := tx.Outbox().List(ctx, store.OutboxFilter{
			Status: q.Status, Template: q.Template, Recipient: q.Recipient,
			AfterCreatedAt: q.After.CreatedAt, AfterID: q.After.ID, Limit: MailLogPageSize + 1,
		})
		if err != nil {
			return err
		}

		page = MailLogPage{}

		if len(mails) > MailLogPageSize {
			mails = mails[:MailLogPageSize]
			last := mails[len(mails)-1]
			page.Next = MailCursor{CreatedAt: last.CreatedAt, ID: last.ID}
		}

		page.Mails, err = mailLogEntries(ctx, tx, mails)
		for i := range page.Mails {
			page.Mails[i].Mail.Text, page.Mails[i].Mail.HTML = "", ""
		}

		return err
	})
	if err != nil {
		return MailLogPage{}, fmt.Errorf("list mail log: %w", err)
	}

	return page, nil
}

// Mail returns a mail of the log with its bodies, unless its template sends
// a secret link (withheld) or the purge removed them.
func (s *MailLogService) Mail(ctx context.Context, id domain.OutboxMailID) (MailLogEntry, error) {
	var out MailLogEntry

	err := s.store.InTx(ctx, store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		m, err := tx.Outbox().Get(ctx, id)
		if err != nil {
			return err
		}

		entries, err := mailLogEntries(ctx, tx, []domain.OutboxMail{m})
		if err == nil {
			out = entries[0]
		}

		return err
	})
	if err != nil {
		return MailLogEntry{}, fmt.Errorf("mail %v: %w", id, err)
	}

	if out.Body != MailBodyShown {
		out.Mail.Text, out.Mail.HTML = "", ""
	}

	return out, nil
}

// Requeue queues a failed mail again: its next attempt is now and its
// attempts start over, so it gets every retry again. A pending or sent
// mail is ErrMailNotFailed, one with a secret link ErrSecretLinkMail. The
// audit log records who queued which mail (p's admin while impersonating).
func (s *MailLogService) Requeue(ctx context.Context, p Principal, id domain.OutboxMailID) (MailLogEntry, error) {
	var out MailLogEntry

	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		m, err := tx.Outbox().Get(ctx, id)

		switch {
		case err != nil:
			return err
		case mail.SecretLink(m.Template):
			return ErrSecretLinkMail
		case m.Status() != domain.MailFailed:
			return ErrMailNotFailed
		}

		if m, err = s.requeue(ctx, tx, p, m); err != nil {
			return err
		}

		entries, err := mailLogEntries(ctx, tx, []domain.OutboxMail{m})
		if err == nil {
			out = entries[0]
			out.Mail.Text, out.Mail.HTML = "", ""
		}

		return err
	})
	if err != nil {
		return MailLogEntry{}, fmt.Errorf("requeue mail %v: %w", id, err)
	}

	return out, nil
}

// RequeueCounts are the failed mails RequeueFailed found.
type RequeueCounts struct {
	// Requeued were queued again.
	Requeued int
	// SecretLink stay failed: a mail with a secret link is never queued
	// again (ErrSecretLinkMail).
	SecretLink int
}

// RequeueFailed queues every failed mail again in one transaction, as
// Requeue does each, and records each in the audit log; the failed mails
// with a secret link stay failed and are counted apart.
func (s *MailLogService) RequeueFailed(ctx context.Context, p Principal) (RequeueCounts, error) {
	var counts RequeueCounts

	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		counts = RequeueCounts{}
		f := store.OutboxFilter{Status: domain.MailFailed, Limit: MailLogPageSize}

		for {
			mails, err := tx.Outbox().List(ctx, f)
			if err != nil {
				return err
			}

			for _, m := range mails {
				if mail.SecretLink(m.Template) {
					counts.SecretLink++

					continue
				}

				if _, err := s.requeue(ctx, tx, p, m); err != nil {
					return err
				}

				counts.Requeued++
			}

			if len(mails) < f.Limit {
				return nil
			}

			last := mails[len(mails)-1]
			f.AfterCreatedAt, f.AfterID = last.CreatedAt, last.ID
		}
	})
	if err != nil {
		return RequeueCounts{}, fmt.Errorf("requeue failed mails: %w", err)
	}

	return counts, nil
}

// requeue queues the failed mail m again and records it in the audit log
// as p's; it returns the mail as queued.
func (s *MailLogService) requeue(ctx context.Context, tx store.Tx, p Principal, m domain.OutboxMail) (domain.OutboxMail, error) {
	now := s.clock.Now()
	m.Attempts, m.NextAttemptAt = 0, now

	if err := m.Validate(); err != nil {
		return domain.OutboxMail{}, err
	}

	if err := tx.Outbox().Update(ctx, m); err != nil {
		return domain.OutboxMail{}, err
	}

	e := domain.AuditEvent{
		ID: ids.New[domain.AuditEventID](s.ids), At: now, ActorID: actorOf(p), Action: domain.AuditMailRequeued,
		Details: domain.AuditDetails{MailID: m.ID, Recipient: m.To, Template: m.Template, Via: p.Via},
	}
	if err := e.Validate(); err != nil {
		return domain.OutboxMail{}, err
	}

	return m, tx.Audit().Create(ctx, e)
}

// Purge removes the bodies of the mails sent more than MailBodyRetention
// ago and deletes the mails created more than MailRetention ago, whatever
// their state. It returns how many bodies it removed and how many mails it
// deleted. Receipts stay: a mail only refers to its receipt's PDF.
func (s *MailLogService) Purge(ctx context.Context) (bodies, deleted int, err error) {
	err = s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		now := s.clock.Now()

		var err error
		if bodies, err = tx.Outbox().PurgeBodies(ctx, now.Add(-MailBodyRetention)); err != nil {
			return err
		}

		deleted, err = tx.Outbox().DeleteCreatedBefore(ctx, now.Add(-MailRetention))

		return err
	})
	if err != nil {
		return 0, 0, fmt.Errorf("purge outbox: %w", err)
	}

	return bodies, deleted, nil
}

// MailCounts are the numbers of unsent mails in the outbox.
type MailCounts struct {
	Pending int // another attempt is due
	Failed  int // delivery was given up
}

// Unsent counts the mails of the outbox that are not sent, for the
// server's metrics.
func (s *MailLogService) Unsent(ctx context.Context) (MailCounts, error) {
	var c store.OutboxCounts

	err := s.store.InTx(ctx, store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		var err error
		c, err = tx.Outbox().CountUnsent(ctx)

		return err
	})
	if err != nil {
		return MailCounts{}, fmt.Errorf("count unsent mails: %w", err)
	}

	return MailCounts{Pending: c.Pending, Failed: c.Failed}, nil
}

// mailLogEntries adds the attachment names and body states to mails, with
// one query for the receipts.
func mailLogEntries(ctx context.Context, tx store.Tx, mails []domain.OutboxMail) ([]MailLogEntry, error) {
	var receiptIDs []domain.ReceiptID

	for _, m := range mails {
		if !m.ReceiptID.IsZero() {
			receiptIDs = append(receiptIDs, m.ReceiptID)
		}
	}

	receipts, err := tx.Receipts().ListByIDs(ctx, receiptIDs)
	if err != nil {
		return nil, err
	}

	numbers := make(map[domain.ReceiptID]string, len(receipts))
	for _, r := range receipts {
		numbers[r.ID] = r.Number
	}

	out := make([]MailLogEntry, len(mails))

	for i, m := range mails {
		out[i] = MailLogEntry{Mail: m, Body: MailBodyShown}

		switch {
		case mail.SecretLink(m.Template):
			out[i].Body = MailBodyWithheld
		case m.Purged():
			out[i].Body = MailBodyPurged
		}

		if !m.ReceiptID.IsZero() {
			n, ok := numbers[m.ReceiptID]
			if !ok {
				return nil, fmt.Errorf("receipt of mail %v: %w", m.ID, errBrokenReference)
			}

			out[i].Attachment = n + ".pdf"
		}
	}

	return out, nil
}
