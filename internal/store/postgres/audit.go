package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/store/postgres/internal/sqlc"
)

type auditRepo struct{ q *sqlc.Queries }

func (r auditRepo) Create(ctx context.Context, e domain.AuditEvent) error {
	var details []byte // NULL without details

	if !e.Details.IsZero() {
		var err error
		if details, err = json.Marshal(e.Details); err != nil {
			return fmt.Errorf("postgres: audit details: %w", err)
		}
	}

	return mapErr(r.q.CreateAuditEvent(ctx, sqlc.CreateAuditEventParams{
		ID:             pgID(e.ID),
		OccurredAt:     pgInstant(e.At),
		ActorID:        pgID(e.ActorID),
		Action:         string(e.Action),
		TargetKind:     pgText(string(e.TargetKind)),
		TargetID:       pgID(e.TargetID),
		TargetPersonID: pgID(e.TargetPersonID),
		Details:        details,
	}))
}

func (r auditRepo) DeleteBefore(ctx context.Context, before time.Time) (int, error) {
	n, err := r.q.DeleteAuditEventsBefore(ctx, pgInstant(before))

	return int(n), mapErr(err)
}

func (r auditRepo) List(ctx context.Context, limit int) ([]domain.AuditEvent, error) {
	rows, err := r.q.ListAuditEvents(ctx, rowLimit(limit))
	if err != nil {
		return nil, mapErr(err)
	}

	events := make([]domain.AuditEvent, 0, len(rows))
	for _, row := range rows {
		e := domain.AuditEvent{
			ID:             fromPgID[domain.AuditEventID](row.ID),
			At:             fromPgInstant(row.OccurredAt),
			ActorID:        fromPgID[domain.AccountID](row.ActorID),
			Action:         domain.AuditAction(row.Action),
			TargetKind:     domain.AuditTargetKind(row.TargetKind.String),
			TargetID:       fromPgID[domain.AccountID](row.TargetID),
			TargetPersonID: fromPgID[domain.PersonID](row.TargetPersonID),
		}

		if row.Details != nil {
			if err := json.Unmarshal(row.Details, &e.Details); err != nil {
				return nil, fmt.Errorf("postgres: details of audit event %v: %w", e.ID, err)
			}
		}

		events = append(events, e)
	}

	return events, nil
}
