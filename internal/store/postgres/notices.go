package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/store/postgres/internal/sqlc"
)

type noticeRepo struct{ q *sqlc.Queries }

func toLessonNotice(row sqlc.LessonNotice) domain.LessonNotice {
	return domain.LessonNotice{
		ID:         fromPgID[domain.LessonNoticeID](row.ID),
		LessonID:   fromPgID[domain.LessonID](row.LessonID),
		SenderID:   fromPgID[domain.AccountID](row.SenderID),
		SentAt:     fromPgInstant(row.SentAt),
		Recipients: int(row.Recipients),
	}
}

func (r noticeRepo) Create(ctx context.Context, n domain.LessonNotice) error {
	recipients, err := int4(n.Recipients)
	if err != nil {
		return err
	}

	return mapErr(r.q.CreateLessonNotice(ctx, sqlc.CreateLessonNoticeParams{
		ID:         pgID(n.ID),
		LessonID:   pgID(n.LessonID),
		SenderID:   pgID(n.SenderID),
		SentAt:     pgInstant(n.SentAt),
		Recipients: recipients,
	}))
}

func (r noticeRepo) ListBySender(
	ctx context.Context, sender domain.AccountID, since time.Time,
) ([]domain.LessonNotice, error) {
	rows, err := r.q.ListLessonNoticesBySender(ctx, sqlc.ListLessonNoticesBySenderParams{
		SenderID: pgID(sender), Since: pgtype.Timestamptz{Time: instant(since), Valid: true},
	})

	return convertRows(rows, err, toLessonNotice)
}

func (r noticeRepo) ListByLesson(
	ctx context.Context, lesson domain.LessonID, since time.Time,
) ([]domain.LessonNotice, error) {
	rows, err := r.q.ListLessonNoticesByLesson(ctx, sqlc.ListLessonNoticesByLessonParams{
		LessonID: pgID(lesson), Since: pgtype.Timestamptz{Time: instant(since), Valid: true},
	})

	return convertRows(rows, err, toLessonNotice)
}
