package postgres

import (
	"context"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/store/postgres/internal/sqlc"
)

type calendarFeedRepo struct{ q *sqlc.Queries }

func toCalendarFeed(row sqlc.CalendarFeed) domain.CalendarFeed {
	return domain.CalendarFeed{
		ID:         fromPgID[domain.CalendarFeedID](row.ID),
		AccountID:  fromPgID[domain.AccountID](row.AccountID),
		Scope:      domain.FeedScope(row.Scope),
		LocationID: fromPgID[domain.LocationID](row.LocationID),
		TokenHash:  fromPgHash(row.TokenHash),
		CreatedAt:  fromPgInstant(row.CreatedAt),
		LastUsedAt: fromPgInstant(row.LastUsedAt),
		RevokedAt:  fromPgInstant(row.RevokedAt),
	}
}

func (r calendarFeedRepo) Get(ctx context.Context, id domain.CalendarFeedID) (domain.CalendarFeed, error) {
	row, err := r.q.GetCalendarFeed(ctx, pgID(id))
	if err != nil {
		return domain.CalendarFeed{}, notFound("calendar feed", err)
	}

	return toCalendarFeed(row), nil
}

func (r calendarFeedRepo) GetByTokenHash(ctx context.Context, hash [32]byte) (domain.CalendarFeed, error) {
	row, err := r.q.GetCalendarFeedByToken(ctx, hash[:])
	if err != nil {
		return domain.CalendarFeed{}, notFound("calendar feed", err)
	}

	return toCalendarFeed(row), nil
}

func (r calendarFeedRepo) Create(ctx context.Context, f domain.CalendarFeed) error {
	return mapErr(r.q.CreateCalendarFeed(ctx, sqlc.CreateCalendarFeedParams{
		ID:         pgID(f.ID),
		AccountID:  pgID(f.AccountID),
		Scope:      string(f.Scope),
		LocationID: pgID(f.LocationID),
		TokenHash:  f.TokenHash[:],
		CreatedAt:  pgInstant(f.CreatedAt),
		LastUsedAt: pgInstant(f.LastUsedAt),
		RevokedAt:  pgInstant(f.RevokedAt),
	}))
}

func (r calendarFeedRepo) ListByAccount(ctx context.Context, account domain.AccountID) ([]domain.CalendarFeed, error) {
	rows, err := r.q.ListCalendarFeedsByAccount(ctx, pgID(account))

	return convertRows(rows, err, toCalendarFeed)
}

func (r calendarFeedRepo) Revoke(ctx context.Context, id domain.CalendarFeedID, at time.Time) error {
	n, err := r.q.RevokeCalendarFeed(ctx, sqlc.RevokeCalendarFeedParams{RevokedAt: pgInstant(at), ID: pgID(id)})

	return affected("active calendar feed", n, err)
}

func (r calendarFeedRepo) RevokeActive(
	ctx context.Context, account domain.AccountID, scope domain.FeedScope, location domain.LocationID, at time.Time,
) (int, error) {
	n, err := r.q.RevokeActiveCalendarFeed(ctx, sqlc.RevokeActiveCalendarFeedParams{
		RevokedAt: pgInstant(at), AccountID: pgID(account), Scope: string(scope), LocationID: pgID(location),
	})
	if err != nil {
		return 0, mapErr(err)
	}

	return int(n), nil
}

func (r calendarFeedRepo) TouchLastUsed(ctx context.Context, id domain.CalendarFeedID, at time.Time) error {
	n, err := r.q.TouchCalendarFeed(ctx, sqlc.TouchCalendarFeedParams{LastUsedAt: pgInstant(at), ID: pgID(id)})

	return affected("calendar feed", n, err)
}
