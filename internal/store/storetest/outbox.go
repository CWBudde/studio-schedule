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
				full.ReplyTo = "ivy@example.org"
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

			wantSlice(e.t, listMails(e, store.OutboxFilter{Limit: 10}), []domain.OutboxMail{b, a, old})
			wantSlice(e.t, listMails(e, store.OutboxFilter{Limit: 2}), []domain.OutboxMail{b, a})
			wantSlice(e.t, listMails(e, store.OutboxFilter{}), nil)
		},
		"ListPages": func(e *env) {
			old := e.outboxMail(t0.Add(-time.Hour), t0)
			a := e.outboxMail(t0, t0)
			b := e.outboxMail(t0, t0)
			c := e.outboxMail(t0.Add(time.Hour), t0)
			e.createMails(old, a, b, c)

			page := listMails(e, store.OutboxFilter{Limit: 2})
			wantSlice(e.t, page, []domain.OutboxMail{c, b})

			// The next page starts after the last mail of the page, also
			// among mails created at the same time.
			last := page[len(page)-1]
			next := store.OutboxFilter{AfterCreatedAt: last.CreatedAt, AfterID: last.ID, Limit: 2}
			wantSlice(e.t, listMails(e, next), []domain.OutboxMail{a, old})

			next.AfterCreatedAt, next.AfterID = old.CreatedAt, old.ID
			wantSlice(e.t, listMails(e, next), nil)
		},
		"ListFilters": func(e *env) {
			pending := e.outboxMail(t0, t0)
			pending.To, pending.Template = "Ada.Lovelace@Example.org", "receipt"
			sent := e.outboxMail(t0.Add(time.Minute), time.Time{})
			sent.SentAt, sent.Template = t0.Add(time.Hour), "announcement"
			failed := e.outboxMail(t0.Add(2*time.Minute), time.Time{})
			failed.To, failed.Attempts, failed.LastError = "ÄDA@example.org", 5, "550 no such user"
			e.createMails(pending, sent, failed)

			for name, tc := range map[string]struct {
				f    store.OutboxFilter
				want []domain.OutboxMail
			}{
				"all":                 {store.OutboxFilter{}, []domain.OutboxMail{failed, sent, pending}},
				"pending":             {store.OutboxFilter{Status: domain.MailPending}, []domain.OutboxMail{pending}},
				"sent":                {store.OutboxFilter{Status: domain.MailSent}, []domain.OutboxMail{sent}},
				"failed":              {store.OutboxFilter{Status: domain.MailFailed}, []domain.OutboxMail{failed}},
				"template":            {store.OutboxFilter{Template: "receipt"}, []domain.OutboxMail{pending}},
				"unknown template":    {store.OutboxFilter{Template: "rec"}, nil},
				"recipient, any case": {store.OutboxFilter{Recipient: "LOVELACE@example"}, []domain.OutboxMail{pending}},
				"recipient part":      {store.OutboxFilter{Recipient: "ada"}, []domain.OutboxMail{sent, pending}},
				// Only ASCII letters fold.
				"non-ASCII case": {store.OutboxFilter{Recipient: "äda"}, nil},
				"non-ASCII":      {store.OutboxFilter{Recipient: "Äda"}, []domain.OutboxMail{failed}},
				"combined": {
					store.OutboxFilter{Status: domain.MailSent, Template: "announcement", Recipient: "ada@"},
					[]domain.OutboxMail{sent},
				},
				"no match combined": {store.OutboxFilter{Status: domain.MailPending, Template: "announcement"}, nil},
			} {
				tc.f.Limit = 10
				got := listMails(e, tc.f)
				if len(got) != len(tc.want) {
					e.t.Fatalf("%s: got %d mails, want %d", name, len(got), len(tc.want))
				}

				for i := range got {
					if got[i].ID != tc.want[i].ID {
						e.t.Fatalf("%s: mail %d is %v, want %v", name, i, got[i].ID, tc.want[i].ID)
					}
				}
			}
		},
		"PurgeBodies": func(e *env) {
			edge := e.outboxMail(t0.Add(-48*time.Hour), time.Time{})
			edge.SentAt, edge.HTML = t0, "<p>Hi</p>"
			old := e.outboxMail(t0.Add(-48*time.Hour), time.Time{})
			old.SentAt, old.HTML = t0.Add(-time.Microsecond), "<p>Hi</p>"
			pending := e.outboxMail(t0.Add(-48*time.Hour), t0)
			failed := e.outboxMail(t0.Add(-48*time.Hour), time.Time{})
			e.createMails(edge, old, pending, failed)

			purge := func() int {
				e.t.Helper()

				var n int

				e.mustWrite(func(ctx context.Context, tx store.Tx) error {
					var err error
					n, err = tx.Outbox().PurgeBodies(ctx, t0)

					return err
				})

				return n
			}

			wantEqual(e.t, purge(), 1)
			wantEqual(e.t, purge(), 0)

			purged := old
			purged.Text, purged.HTML = "", ""
			valid(e.t, purged)

			for _, want := range []domain.OutboxMail{edge, purged, pending, failed} {
				got := mustQuery(e, func(ctx context.Context, tx store.Tx) (domain.OutboxMail, error) {
					return tx.Outbox().Get(ctx, want.ID)
				})
				wantEqual(e.t, got, want)
			}

			// A purged mail can still be written.
			purged.Attempts = 1
			e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Outbox().Update(ctx, purged) })
		},
		"DeleteCreatedBefore": func(e *env) {
			old := e.outboxMail(t0.Add(-time.Microsecond), t0)
			edge := e.outboxMail(t0, t0)
			e.createMails(old, edge)

			var n int

			e.mustWrite(func(ctx context.Context, tx store.Tx) error {
				var err error
				n, err = tx.Outbox().DeleteCreatedBefore(ctx, t0)

				return err
			})
			wantEqual(e.t, n, 1)
			wantSlice(e.t, listMails(e, store.OutboxFilter{Limit: 10}), []domain.OutboxMail{edge})

			_, err := query(e, func(ctx context.Context, tx store.Tx) (domain.OutboxMail, error) {
				return tx.Outbox().Get(ctx, old.ID)
			})
			wantErr(e.t, err, domain.ErrNotFound)
		},
		"PurgeNeedsReadWrite": func(e *env) {
			_, err := query(e, func(ctx context.Context, tx store.Tx) (int, error) {
				return tx.Outbox().PurgeBodies(ctx, t0)
			})
			wantErr(e.t, err, store.ErrReadOnly)

			_, err = query(e, func(ctx context.Context, tx store.Tx) (int, error) {
				return tx.Outbox().DeleteCreatedBefore(ctx, t0)
			})
			wantErr(e.t, err, store.ErrReadOnly)
		},
	})
}

// listMails returns the mails f selects.
func listMails(e *env, f store.OutboxFilter) []domain.OutboxMail {
	e.t.Helper()

	return mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.OutboxMail, error) {
		return tx.Outbox().List(ctx, f)
	})
}
