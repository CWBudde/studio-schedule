package storetest

import (
	"context"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// notice builds a notice about l by sender at at; it is not written.
func (e *env) notice(l domain.Lesson, sender domain.Account, at time.Time) domain.LessonNotice {
	e.t.Helper()

	n := domain.LessonNotice{
		ID: newID[domain.LessonNoticeID](e), LessonID: l.ID, SenderID: sender.ID, SentAt: at, Recipients: 3,
	}
	valid(e.t, n)

	return n
}

func (e *env) createNotices(ns ...domain.LessonNotice) {
	e.t.Helper()

	e.mustWrite(func(ctx context.Context, tx store.Tx) error {
		for _, n := range ns {
			if err := tx.Notices().Create(ctx, n); err != nil {
				return err
			}
		}

		return nil
	})
}

func testNotices(t *testing.T, newStore func(*testing.T) store.Store) {
	t.Helper()

	cases(t, newStore, map[string]func(e *env){
		"CreateAndList": func(e *env) {
			s := e.setting()
			l1, l2 := e.lesson(s, t0, 5), e.lesson(s, t0.Add(time.Hour), 5)
			ivy := e.account(e.person("Ivy", "Instructor"), "ivy@example.org")
			olga := e.account(e.person("Olga", "Office"), "olga@example.org")

			old := e.notice(l1, ivy, t0.Add(-2*time.Hour))
			second := e.notice(l2, ivy, t0)
			first := e.notice(l1, ivy, t0) // same time: ordered by ID
			office := e.notice(l1, olga, t0.Add(time.Minute))
			// A fraction of a microsecond is cut off like in PostgreSQL.
			fine := e.notice(l2, olga, t0.Add(time.Hour+500*time.Nanosecond))
			e.createNotices(old, second, first, office, fine)
			fine.SentAt = t0.Add(time.Hour)

			bySender := func(a domain.Account, since time.Time) []domain.LessonNotice {
				e.t.Helper()

				return mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.LessonNotice, error) {
					return tx.Notices().ListBySender(ctx, a.ID, since)
				})
			}

			byLesson := func(l domain.Lesson, since time.Time) []domain.LessonNotice {
				e.t.Helper()

				return mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.LessonNotice, error) {
					return tx.Notices().ListByLesson(ctx, l.ID, since)
				})
			}

			// The bound is inclusive.
			wantSlice(e.t, bySender(ivy, t0), []domain.LessonNotice{second, first})
			wantSlice(e.t, bySender(ivy, t0.Add(-3*time.Hour)), []domain.LessonNotice{old, second, first})
			wantSlice(e.t, bySender(ivy, t0.Add(time.Microsecond)), nil)
			wantSlice(e.t, bySender(olga, t0), []domain.LessonNotice{office, fine})
			wantSlice(e.t, byLesson(l1, t0.Add(-3*time.Hour)), []domain.LessonNotice{old, first, office})
			wantSlice(e.t, byLesson(l1, t0.Add(time.Minute)), []domain.LessonNotice{office})
			wantSlice(e.t, byLesson(l2, t0.Add(time.Hour)), []domain.LessonNotice{fine})
		},
		"Create": func(e *env) {
			s := e.setting()
			l := e.lesson(s, t0, 5)
			ivy := e.account(e.person("Ivy", "Instructor"), "ivy@example.org")
			n := e.notice(l, ivy, t0)

			err := e.read(func(ctx context.Context, tx store.Tx) error { return tx.Notices().Create(ctx, n) })
			wantErr(e.t, err, store.ErrReadOnly)

			e.createNotices(n)

			dup := e.notice(l, ivy, t0)
			dup.ID = n.ID
			err = e.write(func(ctx context.Context, tx store.Tx) error { return tx.Notices().Create(ctx, dup) })
			wantErr(e.t, err, domain.ErrConflict)

			noLesson := e.notice(l, ivy, t0)
			noLesson.LessonID = newID[domain.LessonID](e)
			err = e.write(func(ctx context.Context, tx store.Tx) error { return tx.Notices().Create(ctx, noLesson) })
			wantErr(e.t, err, domain.ErrConflict)

			noSender := e.notice(l, ivy, t0)
			noSender.SenderID = newID[domain.AccountID](e)
			err = e.write(func(ctx context.Context, tx store.Tx) error { return tx.Notices().Create(ctx, noSender) })
			wantErr(e.t, err, domain.ErrConflict)
		},
		"OutliveTheirLesson": func(e *env) {
			s := e.setting()
			l, kept := e.lesson(s, t0, 5), e.lesson(s, t0.Add(time.Hour), 5)
			ivy := e.account(e.person("Ivy", "Instructor"), "ivy@example.org")
			gone, n := e.notice(l, ivy, t0), e.notice(kept, ivy, t0.Add(time.Minute))
			e.createNotices(gone, n)

			// A notice does not keep its lesson from being deleted, and stays
			// for its sender without the lesson.
			e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Lessons().Delete(ctx, l.ID) })

			gone.LessonID = domain.LessonID{}
			got := mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.LessonNotice, error) {
				return tx.Notices().ListBySender(ctx, ivy.ID, t0.Add(-time.Hour))
			})
			wantSlice(e.t, got, []domain.LessonNotice{gone, n})

			got = mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.LessonNotice, error) {
				return tx.Notices().ListByLesson(ctx, l.ID, t0.Add(-time.Hour))
			})
			wantSlice(e.t, got, nil)
		},
	})
}
