package postgres

import (
	"context"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/store/postgres/internal/sqlc"
)

type auditRepo struct{ q *sqlc.Queries }

func (r auditRepo) Create(ctx context.Context, e domain.AuditEvent) error {
	return mapErr(r.q.CreateAuditEvent(ctx, sqlc.CreateAuditEventParams{
		ID:         pgID(e.ID),
		OccurredAt: pgInstant(e.At),
		ActorID:    pgID(e.ActorID),
		Action:     string(e.Action),
		TargetID:   pgID(e.TargetID),
	}))
}

func (r auditRepo) List(ctx context.Context, limit int) ([]domain.AuditEvent, error) {
	rows, err := r.q.ListAuditEvents(ctx, rowLimit(limit))
	if err != nil {
		return nil, mapErr(err)
	}

	events := make([]domain.AuditEvent, 0, len(rows))
	for _, row := range rows {
		events = append(events, domain.AuditEvent{
			ID:       fromPgID[domain.AuditEventID](row.ID),
			At:       fromPgInstant(row.OccurredAt),
			ActorID:  fromPgID[domain.AccountID](row.ActorID),
			Action:   domain.AuditAction(row.Action),
			TargetID: fromPgID[domain.AccountID](row.TargetID),
		})
	}

	return events, nil
}
