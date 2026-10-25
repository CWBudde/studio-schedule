package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/store/postgres/internal/sqlc"
)

// ---------------------------------------------------------------- waitlist

type waitlistRepo struct{ q *sqlc.Queries }

// pgHash maps an optional token hash; the zero hash is NULL.
func pgHash(h [32]byte) []byte {
	if h == [32]byte{} {
		return nil
	}

	return h[:]
}

// fromPgOptHash converts an optional token hash column; NULL is the zero
// hash.
func fromPgOptHash(b []byte) [32]byte {
	if b == nil {
		return [32]byte{}
	}

	return fromPgHash(b)
}

func toWaitlistEntry(row sqlc.WaitlistEntry) domain.WaitlistEntry {
	return domain.WaitlistEntry{
		ID:              fromPgID[domain.WaitlistEntryID](row.ID),
		LessonID:        fromPgID[domain.LessonID](row.LessonID),
		PersonID:        fromPgID[domain.PersonID](row.PersonID),
		Status:          domain.WaitlistStatus(row.Status),
		JoinedAt:        fromPgInstant(row.JoinedAt),
		StayOnList:      row.StayOnList,
		JoinedBy:        fromPgID[domain.AccountID](row.JoinedBy),
		InviteTokenHash: fromPgOptHash(row.InviteTokenHash),
		InvitedAt:       fromPgInstant(row.InvitedAt),
		ExpiresAt:       fromPgInstant(row.ExpiresAt),
	}
}

func waitlistParams(e domain.WaitlistEntry) sqlc.CreateWaitlistEntryParams {
	return sqlc.CreateWaitlistEntryParams{
		ID:              pgID(e.ID),
		LessonID:        pgID(e.LessonID),
		PersonID:        pgID(e.PersonID),
		Status:          string(e.Status),
		JoinedAt:        pgInstant(e.JoinedAt),
		StayOnList:      e.StayOnList,
		JoinedBy:        pgID(e.JoinedBy),
		InviteTokenHash: pgHash(e.InviteTokenHash),
		InvitedAt:       pgInstant(e.InvitedAt),
		ExpiresAt:       pgInstant(e.ExpiresAt),
	}
}

func (r waitlistRepo) Get(ctx context.Context, id domain.WaitlistEntryID) (domain.WaitlistEntry, error) {
	row, err := r.q.GetWaitlistEntry(ctx, pgID(id))
	if err != nil {
		return domain.WaitlistEntry{}, notFound("waitlist entry", err)
	}

	return toWaitlistEntry(row), nil
}

func (r waitlistRepo) GetByTokenHash(ctx context.Context, hash [32]byte) (domain.WaitlistEntry, error) {
	// A waiting entry stores no hash (NULL), so the zero hash finds nothing.
	row, err := r.q.GetWaitlistEntryByToken(ctx, hash[:])
	if err != nil {
		return domain.WaitlistEntry{}, notFound("waitlist entry", err)
	}

	return toWaitlistEntry(row), nil
}

func (r waitlistRepo) Create(ctx context.Context, e domain.WaitlistEntry) error {
	return mapErr(r.q.CreateWaitlistEntry(ctx, waitlistParams(e)))
}

func (r waitlistRepo) Update(ctx context.Context, e domain.WaitlistEntry) error {
	n, err := r.q.UpdateWaitlistEntry(ctx, sqlc.UpdateWaitlistEntryParams(waitlistParams(e)))

	return affected("waitlist entry", n, err)
}

func (r waitlistRepo) ListByLesson(ctx context.Context, lesson domain.LessonID) ([]domain.WaitlistEntry, error) {
	rows, err := r.q.ListWaitlistEntriesByLesson(ctx, pgID(lesson))

	return convertRows(rows, err, toWaitlistEntry)
}

func (r waitlistRepo) ListOpenByLessons(
	ctx context.Context, lessons []domain.LessonID,
) ([]domain.WaitlistEntry, error) {
	rows, err := r.q.ListOpenWaitlistEntriesByLessons(ctx, pgIDs(lessons))

	return convertRows(rows, err, toWaitlistEntry)
}

func (r waitlistRepo) CountByLessons(
	ctx context.Context, lessons []domain.LessonID,
) (map[domain.LessonID]int, error) {
	rows, err := r.q.CountWaitlistEntriesByLessons(ctx, pgIDs(lessons))
	if err != nil {
		return nil, mapErr(err)
	}

	out := make(map[domain.LessonID]int, len(rows))
	for _, row := range rows {
		out[fromPgID[domain.LessonID](row.LessonID)] = int(row.Entries)
	}

	return out, nil
}

func (r waitlistRepo) ListOpenByPersons(
	ctx context.Context, persons []domain.PersonID,
) ([]domain.WaitlistEntry, error) {
	rows, err := r.q.ListOpenWaitlistEntriesByPersons(ctx, pgIDs(persons))

	return convertRows(rows, err, toWaitlistEntry)
}

func (r waitlistRepo) ListDueLessons(ctx context.Context, now time.Time) ([]domain.LessonID, error) {
	rows, err := r.q.ListDueWaitlistLessons(ctx, pgInstant(now))

	return convertRows(rows, err, func(id pgtype.UUID) domain.LessonID { return fromPgID[domain.LessonID](id) })
}
