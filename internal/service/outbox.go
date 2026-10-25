package service

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/mail"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// Outbox delivery: each mail is claimed for one attempt just before it is
// sent, by moving its next attempt into the future. That delay doubles as a
// lease: it is longer than a send may take (smtp.DefaultTimeout), so no
// other worker picks the mail meanwhile, and a crash mid-send retries later.
// The mail is sent outside any transaction and then marked sent or failed.
const (
	// MaxMailAttempts is how often delivery is tried before it is given up.
	MaxMailAttempts = 5
	// outboxBatch is how many mails one Deliver run sends at most.
	outboxBatch = 20
	// maxMailErrorLen bounds the stored error text.
	maxMailErrorLen = 500
)

// mailRetryDelays are the waits after the 1st, 2nd, ... attempt.
var mailRetryDelays = []time.Duration{5 * time.Minute, 15 * time.Minute, time.Hour, 3 * time.Hour}

func mailRetryDelay(attempts int) time.Duration {
	return mailRetryDelays[min(max(attempts, 1), len(mailRetryDelays))-1]
}

// mailOpts are the optional parts of a queued mail; the zero value has
// none.
type mailOpts struct {
	// replyTo is the address replies go to, e.g. the staff member who sent
	// a lesson notice; empty for the sender's.
	replyTo string
	// receipt is the receipt whose PDF the mail attaches (optional).
	receipt domain.ReceiptID
}

// enqueueMail writes a rendered mail to the outbox in tx; the outbox job
// sends it after the transaction commits.
func enqueueMail(
	ctx context.Context, tx store.Tx, src ids.Source, now time.Time, to, template string, opts mailOpts,
	r mail.Rendered,
) error {
	m := domain.OutboxMail{
		ID: ids.New[domain.OutboxMailID](src), To: to, Subject: r.Subject, Text: r.Text, HTML: r.HTML,
		Template: template, CreatedAt: now, NextAttemptAt: now, ReplyTo: opts.replyTo, ReceiptID: opts.receipt,
	}
	if err := m.Validate(); err != nil {
		return fmt.Errorf("outbox mail: %w", err)
	}

	return tx.Outbox().Create(ctx, m)
}

// sendMails renders template with mails for each recipient, with the data
// that data makes of it, in the recipient's language (else the school's
// default; ADR 0008), and queues it with opts at now.
func sendMails(
	ctx context.Context, tx store.Tx, src ids.Source, mails MailRenderer, settings domain.Settings, now time.Time,
	recipients []recipient, template string, opts mailOpts, data func(recipient) any,
) error {
	for _, r := range recipients {
		rendered, err := mails.Render(cmp.Or(r.locale, settings.DefaultLocale), template, data(r))
		if err != nil {
			// Broken data or templates, not the caller's input: %v keeps
			// domain.ErrValidation out, so the client gets a 500.
			return fmt.Errorf("render %s mail: %v", template, err) //nolint:errorlint // see above
		}

		err = enqueueMail(ctx, tx, src, now, r.email, template, opts, rendered)

		switch {
		case errors.Is(err, domain.ErrValidation):
			// A stored address that is no address is broken data too: a
			// 500 like above.
			return fmt.Errorf("queue %s mail: %v", template, err) //nolint:errorlint // see above
		case err != nil:
			return err
		}
	}

	return nil
}

// OutboxService sends the mails of the outbox and lists them.
type OutboxService struct {
	store    store.Store
	clock    clock.Clock
	mailer   mail.Mailer
	receipts *ReceiptService
}

// NewOutboxService returns an OutboxService that delivers through mailer,
// attaching the receipt PDFs that receipts keep. Without a mailer (the demo,
// local development) mails stay in the outbox.
func NewOutboxService(st store.Store, clk clock.Clock, mailer mail.Mailer, receipts *ReceiptService) *OutboxService {
	return &OutboxService{store: st, clock: clk, mailer: mailer, receipts: receipts}
}

// Deliver makes one delivery attempt for every due mail, up to a batch, and
// returns how many it sent. Delivery failures are recorded on the mail, not
// returned; the error reports store problems and cancellation only.
func (s *OutboxService) Deliver(ctx context.Context) (int, error) {
	if s.mailer == nil {
		return 0, nil
	}

	sent := 0

	for range outboxBatch {
		m, ok, err := s.claim(ctx)
		if err != nil || !ok {
			return sent, err
		}

		msg := mail.Message{To: m.To, Subject: m.Subject, Text: m.Text, HTML: m.HTML, ReplyTo: m.ReplyTo}

		sendErr := s.attach(ctx, m, &msg)
		if sendErr == nil {
			sendErr = s.mailer.Send(ctx, msg)
		}
		if sendErr != nil && ctx.Err() != nil {
			// Shutting down: the claim expires and the mail is retried.
			return sent, ctx.Err()
		}

		// A sent mail is recorded even if ctx ends now, or it would be sent
		// again.
		if err := s.record(context.WithoutCancel(ctx), m.ID, sendErr); err != nil {
			return sent, err
		}

		if sendErr == nil {
			sent++
		}
	}

	return sent, nil
}

// attach adds the PDF of the mail's receipt to msg. Failing to read it fails
// the attempt, which is retried like a failed delivery.
func (s *OutboxService) attach(ctx context.Context, m domain.OutboxMail, msg *mail.Message) error {
	if m.ReceiptID.IsZero() {
		return nil
	}

	if s.receipts == nil {
		return errors.New("no receipts to attach")
	}

	file, err := s.receipts.PDF(ctx, m.ReceiptID)
	if err != nil {
		return fmt.Errorf("attach receipt: %w", err)
	}

	msg.Attachments = []mail.Attachment{{Filename: file.Filename, ContentType: PDFContentType, Content: file.PDF}}

	return nil
}

// PDFContentType is the media type of PDF files.
const PDFContentType = "application/pdf"

// Attachment returns the file a mail attaches (domain.ErrNotFound for an
// unknown mail, or one without an attachment).
func (s *OutboxService) Attachment(ctx context.Context, id domain.OutboxMailID) (ReceiptFile, error) {
	var m domain.OutboxMail

	err := s.store.InTx(ctx, store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		var err error
		m, err = tx.Outbox().Get(ctx, id)

		return err
	})

	switch {
	case err != nil:
		return ReceiptFile{}, fmt.Errorf("mail %v: %w", id, err)
	case m.ReceiptID.IsZero() || s.receipts == nil:
		return ReceiptFile{}, fmt.Errorf("attachment of mail %v: %w", id, domain.ErrNotFound)
	}

	return s.receipts.PDF(ctx, m.ReceiptID)
}

// claim takes the next due mail for one attempt; ok is false if none is due.
func (s *OutboxService) claim(ctx context.Context) (domain.OutboxMail, bool, error) {
	var (
		m  domain.OutboxMail
		ok bool
	)

	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		now := s.clock.Now()

		due, err := tx.Outbox().ListDue(ctx, now, 1)
		if err != nil || len(due) == 0 {
			ok = false

			return err
		}

		m, ok = due[0], true
		m.Attempts++
		m.NextAttemptAt = now.Add(mailRetryDelay(m.Attempts))

		return tx.Outbox().Update(ctx, m)
	})
	if err != nil {
		return domain.OutboxMail{}, false, fmt.Errorf("claim outbox mail: %w", err)
	}

	return m, ok, nil
}

// record stores the outcome of one attempt.
func (s *OutboxService) record(ctx context.Context, id domain.OutboxMailID, sendErr error) error {
	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		m, err := tx.Outbox().Get(ctx, id)
		if err != nil {
			return err
		}

		if sendErr == nil {
			m.SentAt, m.NextAttemptAt, m.LastError = s.clock.Now(), time.Time{}, ""
		} else {
			m.LastError = truncate(sendErr.Error(), maxMailErrorLen)
			if errors.Is(sendErr, mail.ErrPermanent) || m.Attempts >= MaxMailAttempts {
				m.NextAttemptAt = time.Time{} // given up
			}
		}

		return tx.Outbox().Update(ctx, m)
	})
	if err != nil {
		return fmt.Errorf("record outbox delivery: %w", err)
	}

	return nil
}

// MailboxEntry is a mail of the outbox with the name of its attachment.
type MailboxEntry struct {
	Mail domain.OutboxMail
	// Attachment is the file name of the attached PDF, empty without one.
	Attachment string
}

// List returns the newest mails of the outbox, at most limit, with the
// names of their attachments.
func (s *OutboxService) List(ctx context.Context, limit int) ([]MailboxEntry, error) {
	var out []MailboxEntry

	err := s.store.InTx(ctx, store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		mails, err := tx.Outbox().List(ctx, limit)
		if err != nil {
			return err
		}

		out = make([]MailboxEntry, len(mails))

		for i, m := range mails {
			out[i].Mail = m
			if m.ReceiptID.IsZero() {
				continue
			}

			r, err := tx.Receipts().Get(ctx, m.ReceiptID)
			if err != nil {
				return fmt.Errorf("receipt of mail %v: %w", m.ID, err)
			}

			out[i].Attachment = r.Number + ".pdf"
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list outbox mails: %w", err)
	}

	return out, nil
}

// truncate shortens s to at most n bytes without splitting a UTF-8 rune.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}

	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}

	return s[:n]
}
