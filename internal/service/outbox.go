package service

import (
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

// enqueueMail writes a rendered mail to the outbox in tx; the outbox job
// sends it after the transaction commits.
func enqueueMail(
	ctx context.Context, tx store.Tx, src ids.Source, now time.Time, to, template string, r mail.Rendered,
) error {
	m := domain.OutboxMail{
		ID: ids.New[domain.OutboxMailID](src), To: to, Subject: r.Subject, Text: r.Text, HTML: r.HTML,
		Template: template, CreatedAt: now, NextAttemptAt: now,
	}
	if err := m.Validate(); err != nil {
		return fmt.Errorf("outbox mail: %w", err)
	}

	return tx.Outbox().Create(ctx, m)
}

// OutboxService sends the mails of the outbox and lists them.
type OutboxService struct {
	store  store.Store
	clock  clock.Clock
	mailer mail.Mailer
}

// NewOutboxService returns an OutboxService that delivers through mailer.
// Without a mailer (the demo, local development) mails stay in the outbox.
func NewOutboxService(st store.Store, clk clock.Clock, mailer mail.Mailer) *OutboxService {
	return &OutboxService{store: st, clock: clk, mailer: mailer}
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

		sendErr := s.mailer.Send(ctx, mail.Message{To: m.To, Subject: m.Subject, Text: m.Text, HTML: m.HTML})
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

// List returns the newest mails of the outbox, at most limit.
func (s *OutboxService) List(ctx context.Context, limit int) ([]domain.OutboxMail, error) {
	var mails []domain.OutboxMail

	err := s.store.InTx(ctx, store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		var err error
		mails, err = tx.Outbox().List(ctx, limit)

		return err
	})
	if err != nil {
		return nil, fmt.Errorf("list outbox mails: %w", err)
	}

	return mails, nil
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
