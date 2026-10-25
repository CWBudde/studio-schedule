package storetest

import (
	"context"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// newAnnouncement builds an announcement for everyone by author, published
// at publish; it is not written.
func (e *env) newAnnouncement(author domain.Account, title string, publish time.Time) domain.Announcement {
	e.t.Helper()

	a := domain.Announcement{
		ID: newID[domain.AnnouncementID](e), AuthorID: author.ID, Title: title, Body: "The studio is closed.",
		Audience: domain.AudienceEveryone, PublishAt: publish, CreatedAt: t0, UpdatedAt: t0,
	}
	valid(e.t, a)

	return a
}

func (e *env) createAnnouncements(as ...domain.Announcement) {
	e.t.Helper()

	e.mustWrite(func(ctx context.Context, tx store.Tx) error {
		for _, a := range as {
			if err := tx.Announcements().Create(ctx, a); err != nil {
				return err
			}
		}

		return nil
	})
}

func (e *env) mailing(a domain.Announcement, sender domain.Account, at time.Time) domain.AnnouncementMailing {
	e.t.Helper()

	m := domain.AnnouncementMailing{
		ID: newID[domain.AnnouncementMailingID](e), AnnouncementID: a.ID, SenderID: sender.ID, SentAt: at, Recipients: 4,
	}
	valid(e.t, m)

	return m
}

func testAnnouncements(t *testing.T, newStore func(*testing.T) store.Store) {
	t.Helper()

	t.Run("CRUD", func(t *testing.T) {
		testCRUD(t, newStore, crud[domain.AnnouncementID, domain.Announcement]{
			id: func(a domain.Announcement) domain.AnnouncementID { return a.ID },
			get: func(ctx context.Context, tx store.Tx, id domain.AnnouncementID) (domain.Announcement, error) {
				return tx.Announcements().Get(ctx, id)
			},
			create: func(ctx context.Context, tx store.Tx, a domain.Announcement) error {
				return tx.Announcements().Create(ctx, a)
			},
			update: func(ctx context.Context, tx store.Tx, a domain.Announcement) error {
				// Email on before a mail state, a mail state off before email.
				repo := tx.Announcements()
				first, second := repo.Update, repo.SetMailState

				if !a.MailStarted() {
					first, second = second, first
				}

				if err := first(ctx, a); err != nil {
					return err
				}

				return second(ctx, a)
			},
			remove: func(ctx context.Context, tx store.Tx, id domain.AnnouncementID) error {
				return tx.Announcements().Delete(ctx, id)
			},
			fixtures: func(e *env) (domain.Announcement, domain.Announcement) {
				olga := e.account(e.person("Olga", "Office"), "olga@example.org")
				minimal := e.newAnnouncement(olga, "Holiday", t0)
				full := minimal
				full.Audience, full.Role = domain.AudienceRole, domain.RoleInstructor
				full.ExpiresAt = t0.Add(48 * time.Hour)
				full.Email, full.EmailedAt, full.EmailRecipients = true, t0, 7
				full.UpdatedAt = t0.Add(time.Hour)
				valid(e.t, full)

				return minimal, full
			},
		})
	})

	cases(t, newStore, map[string]func(e *env){
		"Audiences": func(e *env) {
			olga := e.account(e.person("Olga", "Office"), "olga@example.org")
			s := e.setting()
			class := e.class(s, time.Monday, domain.TimeOfDay{Hour: 18})
			lesson := e.lesson(s, t0, 5)

			forClass := e.newAnnouncement(olga, "Class", t0)
			forClass.Audience, forClass.ClassID = domain.AudienceClass, class.ID
			forType := e.newAnnouncement(olga, "Type", t0)
			forType.Audience, forType.CourseTypeID = domain.AudienceCourseType, s.courseType.ID
			forLesson := e.newAnnouncement(olga, "Lesson", t0)
			forLesson.Audience, forLesson.LessonID = domain.AudienceLesson, lesson.ID
			e.createAnnouncements(forClass, forType, forLesson)

			for _, want := range []domain.Announcement{forClass, forType, forLesson} {
				got := mustQuery(e, func(ctx context.Context, tx store.Tx) (domain.Announcement, error) {
					return tx.Announcements().Get(ctx, want.ID)
				})
				wantEqual(e.t, got, want)
			}

			// Class and course type stay while announcements are for them.
			err := e.write(func(ctx context.Context, tx store.Tx) error { return tx.Classes().Delete(ctx, class.ID) })
			wantErr(e.t, err, domain.ErrConflict)
			err = e.write(func(ctx context.Context, tx store.Tx) error {
				return tx.CourseTypes().Delete(ctx, s.courseType.ID)
			})
			wantErr(e.t, err, domain.ErrConflict)

			// A lesson's announcement goes with it.
			e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Lessons().Delete(ctx, lesson.ID) })
			_, err = query(e, func(ctx context.Context, tx store.Tx) (domain.Announcement, error) {
				return tx.Announcements().Get(ctx, forLesson.ID)
			})
			wantErr(e.t, err, domain.ErrNotFound)
		},
		"Conflicts": func(e *env) {
			olga := e.account(e.person("Olga", "Office"), "olga@example.org")
			a := e.newAnnouncement(olga, "Holiday", t0)
			e.createAnnouncements(a)

			create := func(a domain.Announcement) error {
				return e.write(func(ctx context.Context, tx store.Tx) error { return tx.Announcements().Create(ctx, a) })
			}

			wantErr(e.t, create(a), domain.ErrConflict) // same ID

			for name, modify := range map[string]func(*domain.Announcement){
				"author": func(a *domain.Announcement) { a.AuthorID = newID[domain.AccountID](e) },
				"class":  func(a *domain.Announcement) { a.Audience, a.ClassID = domain.AudienceClass, newID[domain.ClassID](e) },
				"course type": func(a *domain.Announcement) {
					a.Audience, a.CourseTypeID = domain.AudienceCourseType, newID[domain.CourseTypeID](e)
				},
				"lesson": func(a *domain.Announcement) {
					a.Audience, a.LessonID = domain.AudienceLesson, newID[domain.LessonID](e)
				},
			} {
				b := e.newAnnouncement(olga, "Broken "+name, t0)
				modify(&b)
				valid(e.t, b)
				wantErr(e.t, create(b), domain.ErrConflict)

				err := e.write(func(ctx context.Context, tx store.Tx) error {
					b.ID = a.ID

					return tx.Announcements().Update(ctx, b)
				})
				wantErr(e.t, err, domain.ErrConflict)
			}
		},
		"Lists": func(e *env) {
			olga := e.account(e.person("Olga", "Office"), "olga@example.org")
			past := e.newAnnouncement(olga, "Past", t0.Add(-48*time.Hour))
			past.ExpiresAt = t0
			live := e.newAnnouncement(olga, "Live", t0.Add(-time.Hour))
			live.ExpiresAt = t0.Add(time.Microsecond)
			sameTime := e.newAnnouncement(olga, "Same time", t0.Add(-time.Hour))
			edge := e.newAnnouncement(olga, "Edge", t0)
			future := e.newAnnouncement(olga, "Future", t0.Add(time.Microsecond))
			e.createAnnouncements(future, live, past, edge, sameTime)

			all := mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.Announcement, error) {
				return tx.Announcements().List(ctx)
			})
			// Newest first; equal times by ID descending.
			wantSlice(e.t, all, []domain.Announcement{future, edge, sameTime, live, past})

			shown := mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.Announcement, error) {
				return tx.Announcements().ListShown(ctx, t0)
			})
			wantSlice(e.t, shown, []domain.Announcement{edge, sameTime, live})
		},
		"Mail": func(e *env) {
			olga := e.account(e.person("Olga", "Office"), "olga@example.org")
			later := e.newAnnouncement(olga, "Later", t0.Add(time.Hour))
			due := e.newAnnouncement(olga, "Due", t0.Add(-time.Hour))
			edge := e.newAnnouncement(olga, "Edge", t0)
			sent := e.newAnnouncement(olga, "Sent", t0.Add(-2*time.Hour))
			sent.EmailedAt, sent.EmailRecipients = t0.Add(-2*time.Hour), 3
			inApp := e.newAnnouncement(olga, "In the app", t0.Add(-time.Hour))

			for _, a := range []*domain.Announcement{&later, &due, &edge, &sent} {
				a.Email = true
				valid(e.t, *a)
			}

			e.createAnnouncements(later, due, edge, sent, inApp)

			listDue := func(now time.Time, limit int) []domain.Announcement {
				e.t.Helper()

				var out []domain.Announcement

				e.mustWrite(func(ctx context.Context, tx store.Tx) error {
					var err error
					out, err = tx.Announcements().ListDueForMail(ctx, now, limit)

					return err
				})

				return out
			}

			wantSlice(e.t, listDue(t0, 10), []domain.Announcement{due, edge})
			wantSlice(e.t, listDue(t0, 1), []domain.Announcement{due})
			wantSlice(e.t, listDue(t0.Add(time.Hour), 10), []domain.Announcement{due, edge, later})

			count := func(from, to time.Time, except domain.AnnouncementID) int {
				e.t.Helper()

				return mustQuery(e, func(ctx context.Context, tx store.Tx) (int, error) {
					return tx.Announcements().CountScheduledForMail(ctx, from, to, except)
				})
			}

			wantEqual(e.t, count(t0.Add(-24*time.Hour), t0.Add(24*time.Hour), domain.AnnouncementID{}), 3)
			wantEqual(e.t, count(t0.Add(-24*time.Hour), t0.Add(24*time.Hour), due.ID), 2)
			wantEqual(e.t, count(t0, t0.Add(time.Hour), domain.AnnouncementID{}), 1) // from inclusive, to exclusive

			_, err := query(e, func(ctx context.Context, tx store.Tx) ([]domain.Announcement, error) {
				return tx.Announcements().ListDueForMail(ctx, t0, 10)
			})
			wantErr(e.t, err, store.ErrReadOnly)

			err = e.read(func(ctx context.Context, tx store.Tx) error { return tx.Announcements().LockMail(ctx) })
			wantErr(e.t, err, store.ErrReadOnly)
			e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Announcements().LockMail(ctx) })
		},
		"MailState": func(e *env) {
			olga := e.account(e.person("Olga", "Office"), "olga@example.org")
			a := e.newAnnouncement(olga, "Holiday", t0)
			a.Email = true
			e.createAnnouncements(a)

			get := func() domain.Announcement {
				e.t.Helper()

				return mustQuery(e, func(ctx context.Context, tx store.Tx) (domain.Announcement, error) {
					return tx.Announcements().Get(ctx, a.ID)
				})
			}

			// Being sent: the mail state alone is written.
			sending := a
			sending.Title, sending.EmailRecipients, sending.MailCursor = "Ignored", 2, "bea@example.org"
			valid(e.t, sending)
			e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Announcements().SetMailState(ctx, sending) })

			want := a
			want.EmailRecipients, want.MailCursor = 2, "bea@example.org"
			wantEqual(e.t, get(), want)

			// An edit from a stale copy keeps the mail state.
			edited := a
			edited.Title, edited.UpdatedAt = "Edited", t0.Add(time.Hour)
			e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Announcements().Update(ctx, edited) })

			want.Title, want.UpdatedAt = edited.Title, edited.UpdatedAt
			wantEqual(e.t, get(), want)

			// Being sent, it is still due and still counts as scheduled.
			due := mustQuery(e, func(ctx context.Context, tx store.Tx) (int, error) {
				return tx.Announcements().CountScheduledForMail(ctx, t0, t0.Add(time.Hour), domain.AnnouncementID{})
			})
			wantEqual(e.t, due, 1)

			// Emailed.
			emailed := want
			emailed.EmailedAt, emailed.EmailRecipients, emailed.MailCursor = t0.Add(time.Minute), 3, ""
			e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Announcements().SetMailState(ctx, emailed) })
			wantEqual(e.t, get(), emailed)

			err := e.write(func(ctx context.Context, tx store.Tx) error {
				return tx.Announcements().SetMailState(ctx, e.newAnnouncement(olga, "Missing", t0))
			})
			wantErr(e.t, err, domain.ErrNotFound)
		},
		"Lock": func(e *env) {
			olga := e.account(e.person("Olga", "Office"), "olga@example.org")
			a := e.newAnnouncement(olga, "Holiday", t0)
			a.Email = true
			e.createAnnouncements(a)

			err := e.read(func(ctx context.Context, tx store.Tx) error {
				_, err := tx.Announcements().Lock(ctx, a.ID)

				return err
			})
			wantErr(e.t, err, store.ErrReadOnly)

			err = e.write(func(ctx context.Context, tx store.Tx) error {
				_, err := tx.Announcements().Lock(ctx, newID[domain.AnnouncementID](e))

				return err
			})
			wantErr(e.t, err, domain.ErrNotFound)

			testLockAnnouncement(e, a)
		},
		"Dismissals": func(e *env) {
			olga := e.account(e.person("Olga", "Office"), "olga@example.org")
			ben := e.account(e.person("Ben", "Member"), "ben@example.org")
			a := e.newAnnouncement(olga, "A", t0)
			b := e.newAnnouncement(olga, "B", t0)
			e.createAnnouncements(a, b)

			dismiss := func(d domain.AnnouncementDismissal) error {
				return e.write(func(ctx context.Context, tx store.Tx) error { return tx.Announcements().Dismiss(ctx, d) })
			}
			dismissed := func(account domain.Account, as ...domain.AnnouncementID) []domain.AnnouncementID {
				e.t.Helper()

				return mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.AnnouncementID, error) {
					return tx.Announcements().ListDismissed(ctx, account.ID, as)
				})
			}

			if err := dismiss(domain.AnnouncementDismissal{AnnouncementID: b.ID, AccountID: ben.ID, DismissedAt: t0}); err != nil {
				e.t.Fatal(err)
			}
			// Twice is fine.
			if err := dismiss(domain.AnnouncementDismissal{AnnouncementID: b.ID, AccountID: ben.ID, DismissedAt: t0}); err != nil {
				e.t.Fatal(err)
			}

			wantSlice(e.t, dismissed(ben, a.ID, b.ID, b.ID), []domain.AnnouncementID{b.ID})
			wantSlice(e.t, dismissed(olga, a.ID, b.ID), nil)
			wantSlice(e.t, dismissed(ben), nil)

			wantErr(e.t, dismiss(domain.AnnouncementDismissal{
				AnnouncementID: newID[domain.AnnouncementID](e), AccountID: ben.ID, DismissedAt: t0,
			}), domain.ErrConflict)
			wantErr(e.t, dismiss(domain.AnnouncementDismissal{
				AnnouncementID: a.ID, AccountID: newID[domain.AccountID](e), DismissedAt: t0,
			}), domain.ErrConflict)

			// The dismissals go with the announcement.
			e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Announcements().Delete(ctx, b.ID) })
			wantSlice(e.t, dismissed(ben, b.ID), nil)

			err := e.read(func(ctx context.Context, tx store.Tx) error {
				return tx.Announcements().Dismiss(ctx, domain.AnnouncementDismissal{
					AnnouncementID: a.ID, AccountID: ben.ID, DismissedAt: t0,
				})
			})
			wantErr(e.t, err, store.ErrReadOnly)
		},
		"Mailings": func(e *env) {
			olga := e.account(e.person("Olga", "Office"), "olga@example.org")
			a := e.newAnnouncement(olga, "A", t0)
			e.createAnnouncements(a)

			before := e.mailing(a, olga, t0.Add(-time.Microsecond))
			first := e.mailing(a, olga, t0)
			second := e.mailing(a, olga, t0)
			end := e.mailing(a, olga, t0.Add(time.Hour))
			e.mustWrite(func(ctx context.Context, tx store.Tx) error {
				for _, m := range []domain.AnnouncementMailing{end, second, before, first} {
					if err := tx.Announcements().CreateMailing(ctx, m); err != nil {
						return err
					}
				}

				return nil
			})

			list := func() []domain.AnnouncementMailing {
				e.t.Helper()

				return mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.AnnouncementMailing, error) {
					return tx.Announcements().ListMailings(ctx, t0, t0.Add(time.Hour))
				})
			}

			wantSlice(e.t, list(), []domain.AnnouncementMailing{first, second})

			// Mailings outlive their announcement, without it.
			e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Announcements().Delete(ctx, a.ID) })

			first.AnnouncementID, second.AnnouncementID = domain.AnnouncementID{}, domain.AnnouncementID{}
			wantSlice(e.t, list(), []domain.AnnouncementMailing{first, second})

			err := e.write(func(ctx context.Context, tx store.Tx) error {
				return tx.Announcements().CreateMailing(ctx, e.mailing(a, olga, t0))
			})
			wantErr(e.t, err, domain.ErrConflict)
		},
	})
}

// testLockAnnouncement: while one transaction holds an announcement's lock
// and emails it, another one that locks it waits, and then reads it
// emailed, so an edit cannot work from a stale copy (the race of an edit
// and the announcements job).
func testLockAnnouncement(e *env, a domain.Announcement) {
	locked, release := make(chan struct{}), make(chan struct{})
	mailed := make(chan error, 1)

	go func() {
		mailed <- e.write(func(ctx context.Context, tx store.Tx) error {
			got, err := tx.Announcements().Lock(ctx, a.ID)
			if err != nil {
				close(locked)

				return err
			}

			close(locked)
			<-release

			got.EmailedAt, got.EmailRecipients = t0.Add(time.Minute), 3

			return tx.Announcements().SetMailState(ctx, got)
		})
	}()

	<-locked

	seen := make(chan domain.Announcement, 1)
	edited := make(chan error, 1)

	go func() {
		edited <- e.write(func(ctx context.Context, tx store.Tx) error {
			got, err := tx.Announcements().Lock(ctx, a.ID)
			seen <- got

			return err
		})
	}()

	// Give the second transaction time to block on the lock, then let the
	// first one commit.
	select {
	case got := <-seen:
		e.t.Fatalf("the second lock did not wait: read %+v", got)
	case <-time.After(100 * time.Millisecond):
	}

	close(release)

	if err := <-mailed; err != nil {
		e.t.Fatal(err)
	}

	if err := <-edited; err != nil {
		e.t.Fatal(err)
	}

	if got := <-seen; got.EmailedAt.IsZero() || got.EmailRecipients != 3 {
		e.t.Errorf("after waiting, read %+v; want it emailed", got)
	}
}
