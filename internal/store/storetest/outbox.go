package storetest

import (
	"context"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// outboxMail builds a pending mail created at createdAt and due at due; it
// is not written.
func (e *env) outboxMail(createdAt, due time.Time) domain.OutboxMail {
	e.t.Helper()

	m := domain.OutboxMail{
		ID: newID[domain.OutboxMailID](e), To: "ada@example.org", Subject: "Hello", Text: "Hi Ada",
		CreatedAt: createdAt, NextAttemptAt: due,
	}
	valid(e.t, m)

	return m
}

func (e *env) createMails(ms ...domain.OutboxMail) {
	e.t.Helper()

	e.mustWrite(func(ctx context.Context, tx store.Tx) error {
		for _, m := range ms {
			if err := tx.Outbox().Create(ctx, m); err != nil {
				return err
			}
		}

		return nil
	})
}

func testOutbox(t *testing.T, newStore func(*testing.T) store.Store) {
	t.Helper()

	t.Run("CRUD", func(t *testing.T) {
		testCRUD(t, newStore, crud[domain.OutboxMailID, domain.OutboxMail]{
			id: func(m domain.OutboxMail) domain.OutboxMailID { return m.ID },
			get: func(ctx context.Context, tx store.Tx, id domain.OutboxMailID) (domain.OutboxMail, error) {
				return tx.Outbox().Get(ctx, id)
			},
			create: func(ctx context.Context, tx store.Tx, m domain.OutboxMail) error { return tx.Outbox().Create(ctx, m) },
			update: func(ctx context.Context, tx store.Tx, m domain.OutboxMail) error { return tx.Outbox().Update(ctx, m) },
			fixtures: func(e *env) (domain.OutboxMail, domain.OutboxMail) {
				minimal := domain.OutboxMail{
					ID: newID[domain.OutboxMailID](e), To: "ada@example.org", Subject: "Hello", Text: "Hi Ada",
					CreatedAt: t0,
				}
				full := minimal
				full.HTML = "<p>Hi Ada</p>"
				full.Template = "confirm-email"
				full.Attempts = 2
				full.SentAt = t0.Add(time.Minute)
				full.LastError = "450 mailbox busy"
				valid(e.t, minimal)
				valid(e.t, full)

				return minimal, full
			},
		})
	})

	listDue := func(e *env, now time.Time, limit int) []domain.OutboxMail {
		e.t.Helper()

		var due []domain.OutboxMail

		e.mustWrite(func(ctx context.Context, tx store.Tx) error {
			var err error
			due, err = tx.Outbox().ListDue(ctx, now, limit)

			return err
		})

		return due
	}

	cases(t, newStore, map[string]func(e *env){
		"ListDue": func(e *env) {
			later := e.outboxMail(t0, t0.Add(time.Minute))
			first := e.outboxMail(t0, t0.Add(-time.Hour))
			edge := e.outboxMail(t0, t0)
			sent := e.outboxMail(t0, time.Time{})
			sent.SentAt = t0
			failed := e.outboxMail(t0, time.Time{})
			e.createMails(later, first, edge, sent, failed)

			wantSlice(e.t, listDue(e, t0, 10), []domain.OutboxMail{first, edge})
			wantSlice(e.t, listDue(e, t0, 1), []domain.OutboxMail{first})
			wantSlice(e.t, listDue(e, t0.Add(time.Hour), 10), []domain.OutboxMail{first, edge, later})
		},
		"ListDueNeedsReadWrite": func(e *env) {
			// It locks the mails it returns.
			_, err := query(e, func(ctx context.Context, tx store.Tx) ([]domain.OutboxMail, error) {
				return tx.Outbox().ListDue(ctx, t0, 10)
			})
			wantErr(e.t, err, store.ErrReadOnly)
		},
		"ListNewestFirst": func(e *env) {
			old := e.outboxMail(t0.Add(-time.Hour), t0)
			a := e.outboxMail(t0, t0)
			b := e.outboxMail(t0, t0) // same time: the higher ID comes first
			e.createMails(old, b, a)

			got := mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.OutboxMail, error) {
				return tx.Outbox().List(ctx, 10)
			})
			wantSlice(e.t, got, []domain.OutboxMail{b, a, old})

			got = mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.OutboxMail, error) {
				return tx.Outbox().List(ctx, 2)
			})
			wantSlice(e.t, got, []domain.OutboxMail{b, a})
		},
	})
}
