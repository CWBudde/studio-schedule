package postgres

import (
	"context"
	"math"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/store/postgres/internal/sqlc"
)

type outboxRepo struct{ q *sqlc.Queries }

func toOutboxMail(row sqlc.OutboxMail) domain.OutboxMail {
	return domain.OutboxMail{
		ID:            fromPgID[domain.OutboxMailID](row.ID),
		To:            row.Recipient,
		Subject:       row.Subject,
		Text:          row.BodyText,
		HTML:          row.BodyHtml.String,
		Template:      row.Template.String,
		CreatedAt:     fromPgInstant(row.CreatedAt),
		Attempts:      int(row.Attempts),
		NextAttemptAt: fromPgInstant(row.NextAttemptAt),
		SentAt:        fromPgInstant(row.SentAt),
		LastError:     row.LastError.String,
		ReplyTo:       row.ReplyTo.String,
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
		BodyText:      m.Text,
		BodyHtml:      pgText(m.HTML),
		Template:      pgText(m.Template),
		CreatedAt:     pgInstant(m.CreatedAt),
		Attempts:      attempts,
		NextAttemptAt: pgInstant(m.NextAttemptAt),
		SentAt:        pgInstant(m.SentAt),
		LastError:     pgText(m.LastError),
		ReplyTo:       pgText(m.ReplyTo),
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
		BodyText:      m.Text,
		BodyHtml:      pgText(m.HTML),
		Template:      pgText(m.Template),
		CreatedAt:     pgInstant(m.CreatedAt),
		Attempts:      attempts,
		NextAttemptAt: pgInstant(m.NextAttemptAt),
		SentAt:        pgInstant(m.SentAt),
		LastError:     pgText(m.LastError),
		ReplyTo:       pgText(m.ReplyTo),
	})

	return affected("outbox mail", n, err)
}

func (r outboxRepo) ListDue(ctx context.Context, now time.Time, limit int) ([]domain.OutboxMail, error) {
	rows, err := r.q.ListDueOutboxMails(ctx, sqlc.ListDueOutboxMailsParams{Now: pgInstant(now), MaxRows: rowLimit(limit)})

	return convertRows(rows, err, toOutboxMail)
}

func (r outboxRepo) List(ctx context.Context, limit int) ([]domain.OutboxMail, error) {
	rows, err := r.q.ListOutboxMails(ctx, rowLimit(limit))

	return convertRows(rows, err, toOutboxMail)
}
