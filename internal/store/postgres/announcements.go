package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/store"
	"github.com/cwbudde/studio-schedule/internal/store/postgres/internal/sqlc"
)

type announcementRepo struct{ q *sqlc.Queries }

func toAnnouncement(row sqlc.Announcement) domain.Announcement {
	return domain.Announcement{
		ID:              fromPgID[domain.AnnouncementID](row.ID),
		AuthorID:        fromPgID[domain.AccountID](row.AuthorID),
		Title:           row.Title,
		Body:            row.Body,
		Audience:        domain.Audience(row.Audience),
		Role:            domain.Role(row.Role.String),
		ClassID:         fromPgID[domain.ClassID](row.ClassID),
		CourseTypeID:    fromPgID[domain.CourseTypeID](row.CourseTypeID),
		LessonID:        fromPgID[domain.LessonID](row.LessonID),
		PublishAt:       fromPgInstant(row.PublishAt),
		ExpiresAt:       fromPgInstant(row.ExpiresAt),
		Email:           row.Email,
		EmailedAt:       fromPgInstant(row.EmailedAt),
		EmailRecipients: int(row.EmailRecipients),
		MailCursor:      row.MailCursor.String,
		CreatedAt:       fromPgInstant(row.CreatedAt),
		UpdatedAt:       fromPgInstant(row.UpdatedAt),
	}
}

// pgAt maps a required instant for a comparison.
func pgAt(t time.Time) pgtype.Timestamptz { return pgtype.Timestamptz{Time: instant(t), Valid: true} }

func (r announcementRepo) Get(ctx context.Context, id domain.AnnouncementID) (domain.Announcement, error) {
	row, err := r.q.GetAnnouncement(ctx, pgID(id))
	if err != nil {
		return domain.Announcement{}, notFound("announcement", err)
	}

	return toAnnouncement(row), nil
}

func (r announcementRepo) Create(ctx context.Context, a domain.Announcement) error {
	recipients, err := int4(a.EmailRecipients)
	if err != nil {
		return err
	}

	return mapErr(r.q.CreateAnnouncement(ctx, sqlc.CreateAnnouncementParams{
		ID:              pgID(a.ID),
		AuthorID:        pgID(a.AuthorID),
		Title:           a.Title,
		Body:            a.Body,
		Audience:        string(a.Audience),
		Role:            pgText(string(a.Role)),
		ClassID:         pgID(a.ClassID),
		CourseTypeID:    pgID(a.CourseTypeID),
		LessonID:        pgID(a.LessonID),
		PublishAt:       pgInstant(a.PublishAt),
		ExpiresAt:       pgInstant(a.ExpiresAt),
		Email:           a.Email,
		EmailedAt:       pgInstant(a.EmailedAt),
		EmailRecipients: recipients,
		MailCursor:      pgText(a.MailCursor),
		CreatedAt:       pgInstant(a.CreatedAt),
		UpdatedAt:       pgInstant(a.UpdatedAt),
	}))
}

func (r announcementRepo) Update(ctx context.Context, a domain.Announcement) error {
	n, err := r.q.UpdateAnnouncement(ctx, sqlc.UpdateAnnouncementParams{
		ID:           pgID(a.ID),
		AuthorID:     pgID(a.AuthorID),
		Title:        a.Title,
		Body:         a.Body,
		Audience:     string(a.Audience),
		Role:         pgText(string(a.Role)),
		ClassID:      pgID(a.ClassID),
		CourseTypeID: pgID(a.CourseTypeID),
		LessonID:     pgID(a.LessonID),
		PublishAt:    pgInstant(a.PublishAt),
		ExpiresAt:    pgInstant(a.ExpiresAt),
		Email:        a.Email,
		CreatedAt:    pgInstant(a.CreatedAt),
		UpdatedAt:    pgInstant(a.UpdatedAt),
	})

	return affected("announcement", n, err)
}

func (r announcementRepo) SetMailState(ctx context.Context, a domain.Announcement) error {
	recipients, err := int4(a.EmailRecipients)
	if err != nil {
		return err
	}

	n, err := r.q.SetAnnouncementMailState(ctx, sqlc.SetAnnouncementMailStateParams{
		ID: pgID(a.ID), EmailedAt: pgInstant(a.EmailedAt), EmailRecipients: recipients, MailCursor: pgText(a.MailCursor),
	})

	return affected("announcement", n, err)
}

func (r announcementRepo) Lock(ctx context.Context, id domain.AnnouncementID) (domain.Announcement, error) {
	row, err := r.q.LockAnnouncement(ctx, pgID(id))
	if err != nil {
		return domain.Announcement{}, notFound("announcement", err)
	}

	return toAnnouncement(row), nil
}

func (r announcementRepo) Delete(ctx context.Context, id domain.AnnouncementID) error {
	n, err := r.q.DeleteAnnouncement(ctx, pgID(id))

	return affected("announcement", n, err)
}

func (r announcementRepo) List(ctx context.Context) ([]domain.Announcement, error) {
	rows, err := r.q.ListAnnouncements(ctx)

	return convertRows(rows, err, toAnnouncement)
}

func (r announcementRepo) ListShown(ctx context.Context, now time.Time) ([]domain.Announcement, error) {
	rows, err := r.q.ListShownAnnouncements(ctx, pgAt(now))

	return convertRows(rows, err, toAnnouncement)
}

func (r announcementRepo) ListDueForMail(ctx context.Context, now time.Time, limit int) ([]domain.Announcement, error) {
	rows, err := r.q.ListAnnouncementsDueForMail(ctx, sqlc.ListAnnouncementsDueForMailParams{
		Now: pgAt(now), MaxRows: rowLimit(limit),
	})

	return convertRows(rows, err, toAnnouncement)
}

func (r announcementRepo) CountScheduledForMail(
	ctx context.Context, from, to time.Time, except domain.AnnouncementID,
) (int, error) {
	n, err := r.q.CountAnnouncementsScheduledForMail(ctx, sqlc.CountAnnouncementsScheduledForMailParams{
		FromAt: pgAt(from), ToAt: pgAt(to), ExceptID: pgID(except),
	})
	if err != nil {
		return 0, mapErr(err)
	}

	return int(n), nil
}

func (r announcementRepo) LockMail(ctx context.Context) error {
	locked, err := r.q.LockAnnouncementMail(ctx)

	switch {
	case err != nil:
		return mapErr(err)
	case !locked:
		return store.ErrReadOnly
	}

	return nil
}

func (r announcementRepo) Dismiss(ctx context.Context, d domain.AnnouncementDismissal) error {
	return mapErr(r.q.DismissAnnouncement(ctx, sqlc.DismissAnnouncementParams{
		AnnouncementID: pgID(d.AnnouncementID), AccountID: pgID(d.AccountID), DismissedAt: pgInstant(d.DismissedAt),
	}))
}

func (r announcementRepo) ListDismissed(
	ctx context.Context, account domain.AccountID, announcements []domain.AnnouncementID,
) ([]domain.AnnouncementID, error) {
	rows, err := r.q.ListDismissedAnnouncements(ctx, sqlc.ListDismissedAnnouncementsParams{
		AccountID: pgID(account), Announcements: pgIDs(announcements),
	})

	return convertRows(rows, err, fromPgID[domain.AnnouncementID])
}

func (r announcementRepo) CreateMailing(ctx context.Context, m domain.AnnouncementMailing) error {
	recipients, err := int4(m.Recipients)
	if err != nil {
		return err
	}

	return mapErr(r.q.CreateAnnouncementMailing(ctx, sqlc.CreateAnnouncementMailingParams{
		ID: pgID(m.ID), AnnouncementID: pgID(m.AnnouncementID), SenderID: pgID(m.SenderID),
		SentAt: pgInstant(m.SentAt), Recipients: recipients,
	}))
}

func (r announcementRepo) ListMailings(ctx context.Context, from, to time.Time) ([]domain.AnnouncementMailing, error) {
	rows, err := r.q.ListAnnouncementMailings(ctx, sqlc.ListAnnouncementMailingsParams{
		FromAt: pgAt(from), ToAt: pgAt(to),
	})

	return convertRows(rows, err, func(row sqlc.AnnouncementMailing) domain.AnnouncementMailing {
		return domain.AnnouncementMailing{
			ID:             fromPgID[domain.AnnouncementMailingID](row.ID),
			AnnouncementID: fromPgID[domain.AnnouncementID](row.AnnouncementID),
			SenderID:       fromPgID[domain.AccountID](row.SenderID),
			SentAt:         fromPgInstant(row.SentAt),
			Recipients:     int(row.Recipients),
		}
	})
}
