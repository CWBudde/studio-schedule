package memory

import (
	"cmp"
	"context"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
)

// ---------------------------------------------------------------- announcements

type announcementRepo struct{ tx *tx }

func (r announcementRepo) Get(_ context.Context, id domain.AnnouncementID) (domain.Announcement, error) {
	a, ok := r.tx.state.announcements.get(id)
	if !ok {
		return domain.Announcement{}, notFound("announcement")
	}

	return a, nil
}

func (r announcementRepo) Create(_ context.Context, a domain.Announcement) error {
	if err := r.tx.checkWritable(); err != nil {
		return err
	}

	if r.tx.state.announcements.has(a.ID) {
		return conflict("announcement %s exists", a.ID)
	}

	return r.put(a)
}

func (r announcementRepo) Update(_ context.Context, a domain.Announcement) error {
	if err := r.tx.checkWritable(); err != nil {
		return err
	}

	old, ok := r.tx.state.announcements.get(a.ID)
	if !ok {
		return notFound("announcement")
	}

	a.EmailedAt, a.EmailRecipients, a.MailCursor = old.EmailedAt, old.EmailRecipients, old.MailCursor

	return r.put(a)
}

func (r announcementRepo) SetMailState(_ context.Context, a domain.Announcement) error {
	if err := r.tx.checkWritable(); err != nil {
		return err
	}

	stored, ok := r.tx.state.announcements.get(a.ID)
	if !ok {
		return notFound("announcement")
	}

	stored.EmailedAt, stored.EmailRecipients, stored.MailCursor = a.EmailedAt, a.EmailRecipients, a.MailCursor

	return r.put(stored)
}

func (r announcementRepo) Lock(ctx context.Context, id domain.AnnouncementID) (domain.Announcement, error) {
	if err := r.tx.checkWritable(); err != nil {
		return domain.Announcement{}, err
	}

	return r.Get(ctx, id)
}

func (r announcementRepo) put(a domain.Announcement) error {
	s := &r.tx.state

	switch {
	case !s.accounts.has(a.AuthorID),
		!a.ClassID.IsZero() && !s.classes.has(a.ClassID),
		!a.CourseTypeID.IsZero() && !s.courseTypes.has(a.CourseTypeID),
		!a.LessonID.IsZero() && !s.lessons.has(a.LessonID):
		return conflict("announcement references a missing record")
	}

	a.PublishAt = instant(a.PublishAt)
	a.ExpiresAt = instant(a.ExpiresAt)
	a.EmailedAt = instant(a.EmailedAt)
	a.CreatedAt = instant(a.CreatedAt)
	a.UpdatedAt = instant(a.UpdatedAt)
	s.announcements.put(r.tx.gen, a.ID, a)

	return nil
}

func (r announcementRepo) Delete(_ context.Context, id domain.AnnouncementID) error {
	if err := r.tx.checkWritable(); err != nil {
		return err
	}

	if !r.tx.state.announcements.has(id) {
		return notFound("announcement")
	}

	r.remove(id)

	return nil
}

// remove deletes the announcement with its dismissals; its mailings stay
// without it.
func (r announcementRepo) remove(id domain.AnnouncementID) {
	s := &r.tx.state

	for k := range s.announcementDismissed.rows {
		if k.announcement == id {
			s.announcementDismissed.delete(r.tx.gen, k)
		}
	}

	for _, m := range s.announcementMailings.filter(func(m domain.AnnouncementMailing) bool {
		return m.AnnouncementID == id
	}) {
		m.AnnouncementID = domain.AnnouncementID{}
		s.announcementMailings.put(r.tx.gen, m.ID, m)
	}

	s.announcements.delete(r.tx.gen, id)
}

func (r announcementRepo) List(context.Context) ([]domain.Announcement, error) {
	return r.newestFirst(r.tx.state.announcements.filter(all)), nil
}

func (r announcementRepo) ListShown(_ context.Context, now time.Time) ([]domain.Announcement, error) {
	now = instant(now)

	return r.newestFirst(r.tx.state.announcements.filter(func(a domain.Announcement) bool {
		return a.ShownAt(now)
	})), nil
}

func (announcementRepo) newestFirst(as []domain.Announcement) []domain.Announcement {
	return sorted(as, func(a, b domain.Announcement) int {
		return cmp.Or(b.PublishAt.Compare(a.PublishAt), compareID(b.ID, a.ID))
	})
}

// dueForMail reports whether a is to be emailed and is not yet.
func dueForMail(a domain.Announcement) bool { return a.Email && a.EmailedAt.IsZero() }

func (r announcementRepo) ListDueForMail(_ context.Context, now time.Time, limit int) ([]domain.Announcement, error) {
	// Like the PostgreSQL store, which locks the announcements it returns.
	if err := r.tx.checkWritable(); err != nil {
		return nil, err
	}

	now = instant(now)
	due := sorted(r.tx.state.announcements.filter(func(a domain.Announcement) bool {
		return dueForMail(a) && !a.PublishAt.After(now)
	}), func(a, b domain.Announcement) int {
		return cmp.Or(a.PublishAt.Compare(b.PublishAt), compareID(a.ID, b.ID))
	})

	return due[:min(max(limit, 0), len(due))], nil
}

func (r announcementRepo) CountScheduledForMail(
	_ context.Context, from, to time.Time, except domain.AnnouncementID,
) (int, error) {
	from, to = instant(from), instant(to)

	return len(r.tx.state.announcements.filter(func(a domain.Announcement) bool {
		return dueForMail(a) && a.ID != except && !a.PublishAt.Before(from) && a.PublishAt.Before(to)
	})), nil
}

// LockMail needs no lock of its own: the memory store serializes all
// transactions.
func (r announcementRepo) LockMail(context.Context) error { return r.tx.checkWritable() }

func (r announcementRepo) Dismiss(_ context.Context, d domain.AnnouncementDismissal) error {
	if err := r.tx.checkWritable(); err != nil {
		return err
	}

	s := &r.tx.state

	if !s.announcements.has(d.AnnouncementID) || !s.accounts.has(d.AccountID) {
		return conflict("dismissal references a missing record")
	}

	k := dismissalKey{d.AnnouncementID, d.AccountID}
	if s.announcementDismissed.has(k) {
		return nil
	}

	d.DismissedAt = instant(d.DismissedAt)
	s.announcementDismissed.put(r.tx.gen, k, d)

	return nil
}

func (r announcementRepo) ListDismissed(
	_ context.Context, account domain.AccountID, announcements []domain.AnnouncementID,
) ([]domain.AnnouncementID, error) {
	var out []domain.AnnouncementID

	for id := range setOf(announcements) {
		if r.tx.state.announcementDismissed.has(dismissalKey{id, account}) {
			out = append(out, id)
		}
	}

	return sorted(out, compareID), nil
}

func (r announcementRepo) CreateMailing(_ context.Context, m domain.AnnouncementMailing) error {
	if err := r.tx.checkWritable(); err != nil {
		return err
	}

	s := &r.tx.state

	switch {
	case s.announcementMailings.has(m.ID):
		return conflict("mailing %s exists", m.ID)
	case !s.announcements.has(m.AnnouncementID) || !s.accounts.has(m.SenderID):
		return conflict("mailing references a missing record")
	}

	m.SentAt = instant(m.SentAt)
	s.announcementMailings.put(r.tx.gen, m.ID, m)

	return nil
}

func (r announcementRepo) ListMailings(_ context.Context, from, to time.Time) ([]domain.AnnouncementMailing, error) {
	from, to = instant(from), instant(to)
	ms := r.tx.state.announcementMailings.filter(func(m domain.AnnouncementMailing) bool {
		return !m.SentAt.Before(from) && m.SentAt.Before(to)
	})

	return sorted(ms, func(a, b domain.AnnouncementMailing) int {
		return cmp.Or(a.SentAt.Compare(b.SentAt), compareID(a.ID, b.ID))
	}), nil
}
