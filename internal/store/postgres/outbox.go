package postgres

import (
	"context"
	"math"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/store"
	"github.com/cwbudde/studio-schedule/internal/store/postgres/internal/sqlc"
)

type outboxRepo struct{ q *sqlc.Queries }

func toOutboxMail(row sqlc.OutboxMail) domain.OutboxMail {
	return domain.OutboxMail{
		ID:            fromPgID[domain.OutboxMailID](row.ID),
		To:            row.Recipient,
		Subject:       row.Subject,
		Text:          row.BodyText.String,
		HTML:          row.BodyHtml.String,
		Template:      row.Template.String,
		CreatedAt:     fromPgInstant(row.CreatedAt),
		Attempts:      int(row.Attempts),
		NextAttemptAt: fromPgInstant(row.NextAttemptAt),
		SentAt:        fromPgInstant(row.SentAt),
		LastError:     row.LastError.String,
		ReplyTo:       row.ReplyTo.String,
		ReceiptID:     fromPgID[domain.ReceiptID](row.ReceiptID),
	}
}

// rowLimit converts a list limit for a LIMIT clause: negative is 0, and
// anything beyond an integer is all rows.
func rowLimit(limit int) int32 { return int32(min(max(limit, 0), math.MaxInt32)) }

func (r outboxRepo) Get(ctx context.Context, id domain.OutboxMailID) (domain.OutboxMail, error) {
	row, err := r.q.GetOutboxMail(ctx, pgID(id))
	if err != nil {
		return domain.OutboxMail{}, notFound("outbox mail", err)
	}

	return toOutboxMail(row), nil
}

func (r outboxRepo) Create(ctx context.Context, m domain.OutboxMail) error {
	attempts, err := int4(m.Attempts)
	if err != nil {
		return err
	}

	return mapErr(r.q.CreateOutboxMail(ctx, sqlc.CreateOutboxMailParams{
		ID:            pgID(m.ID),
		Recipient:     m.To,
		Subject:       m.Subject,
		BodyText:      pgText(m.Text),
		BodyHtml:      pgText(m.HTML),
		Template:      pgText(m.Template),
		CreatedAt:     pgInstant(m.CreatedAt),
		Attempts:      attempts,
		NextAttemptAt: pgInstant(m.NextAttemptAt),
		SentAt:        pgInstant(m.SentAt),
		LastError:     pgText(m.LastError),
		ReplyTo:       pgText(m.ReplyTo),
		ReceiptID:     pgID(m.ReceiptID),
	}))
}

func (r outboxRepo) Update(ctx context.Context, m domain.OutboxMail) error {
	attempts, err := int4(m.Attempts)
	if err != nil {
		return err
	}

	n, err := r.q.UpdateOutboxMail(ctx, sqlc.UpdateOutboxMailParams{
		ID:            pgID(m.ID),
		Recipient:     m.To,
		Subject:       m.Subject,
		BodyText:      pgText(m.Text),
		BodyHtml:      pgText(m.HTML),
		Template:      pgText(m.Template),
		CreatedAt:     pgInstant(m.CreatedAt),
		Attempts:      attempts,
		NextAttemptAt: pgInstant(m.NextAttemptAt),
		SentAt:        pgInstant(m.SentAt),
		LastError:     pgText(m.LastError),
		ReplyTo:       pgText(m.ReplyTo),
		ReceiptID:     pgID(m.ReceiptID),
	})

	return affected("outbox mail", n, err)
}

func (r outboxRepo) ListDue(ctx context.Context, now time.Time, limit int) ([]domain.OutboxMail, error) {
	rows, err := r.q.ListDueOutboxMails(ctx, sqlc.ListDueOutboxMailsParams{Now: pgInstant(now), MaxRows: rowLimit(limit)})

	return convertRows(rows, err, toOutboxMail)
}

func (r outboxRepo) List(ctx context.Context, f store.OutboxFilter) ([]domain.OutboxMail, error) {
	p := sqlc.ListOutboxMailsParams{
		Status: string(f.Status), Template: f.Template, Recipient: f.Recipient, MaxRows: rowLimit(f.Limit),
	}
	if !f.AfterCreatedAt.IsZero() {
		p.BeforeCreatedAt, p.BeforeID = pgInstant(f.AfterCreatedAt), pgtype.UUID{Bytes: f.AfterID, Valid: true}
	}

	rows, err := r.q.ListOutboxMails(ctx, p)

	return convertRows(rows, err, toOutboxMail)
}

func (r outboxRepo) PurgeBodies(ctx context.Context, sentBefore time.Time) (int, error) {
	n, err := r.q.PurgeOutboxBodies(ctx, pgAt(sentBefore))

	return int(n), mapErr(err)
}

func (r outboxRepo) CountUnsent(ctx context.Context) (store.OutboxCounts, error) {
	row, err := r.q.CountUnsentOutboxMails(ctx)
	if err != nil {
		return store.OutboxCounts{}, mapErr(err)
	}

	return store.OutboxCounts{Pending: int(row.Pending), Failed: int(row.Failed)}, nil
}

func (r outboxRepo) DeleteCreatedBefore(ctx context.Context, before time.Time) (int, error) {
	n, err := r.q.DeleteOldOutboxMails(ctx, pgAt(before))

	return int(n), mapErr(err)
}
