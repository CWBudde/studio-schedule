package memory

import (
	"cmp"
	"context"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
)

// ---------------------------------------------------------------- notices

type noticeRepo struct{ tx *tx }

func (r noticeRepo) Create(_ context.Context, n domain.LessonNotice) error {
	if err := r.tx.checkWritable(); err != nil {
		return err
	}

	s := &r.tx.state

	switch {
	case s.notices.has(n.ID):
		return conflict("notice %s exists", n.ID)
	case !s.lessons.has(n.LessonID) || !s.accounts.has(n.SenderID):
		return conflict("notice references a missing record")
	}

	n.SentAt = instant(n.SentAt)
	s.notices.put(r.tx.gen, n.ID, n)

	return nil
}

func (r noticeRepo) ListBySender(
	_ context.Context, sender domain.AccountID, since time.Time,
) ([]domain.LessonNotice, error) {
	return r.list(since, func(n domain.LessonNotice) bool { return n.SenderID == sender }), nil
}

func (r noticeRepo) ListByLesson(
	_ context.Context, lesson domain.LessonID, since time.Time,
) ([]domain.LessonNotice, error) {
	return r.list(since, func(n domain.LessonNotice) bool { return n.LessonID == lesson }), nil
}

// list returns the notices sent at or after since that match, ordered by
// SentAt and ID.
func (r noticeRepo) list(since time.Time, match func(domain.LessonNotice) bool) []domain.LessonNotice {
	since = instant(since)
	notices := r.tx.state.notices.filter(func(n domain.LessonNotice) bool {
		return match(n) && !n.SentAt.Before(since)
	})

	return sorted(notices, func(a, b domain.LessonNotice) int {
		return cmp.Or(a.SentAt.Compare(b.SentAt), compareID(a.ID, b.ID))
	})
}
