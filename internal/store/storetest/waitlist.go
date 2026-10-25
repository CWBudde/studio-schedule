package storetest

import (
	"context"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// newEntry returns a waiting entry of p for l without creating it.
func (e *env) newEntry(l domain.Lesson, p domain.Person, joined time.Time) domain.WaitlistEntry {
	e.t.Helper()

	w := domain.WaitlistEntry{
		ID: newID[domain.WaitlistEntryID](e), LessonID: l.ID, PersonID: p.ID, Status: domain.WaitlistWaiting,
		JoinedAt: joined,
	}
	valid(e.t, w)

	return w
}

func (e *env) entry(l domain.Lesson, p domain.Person, joined time.Time) domain.WaitlistEntry {
	e.t.Helper()

	w := e.newEntry(l, p, joined)
	e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Waitlist().Create(ctx, w) })

	return w
}

// invite turns w into an invited entry with the token hash, invited a
// minute after it joined and expiring at expires, and stores it.
func (e *env) invite(w domain.WaitlistEntry, token byte, expires time.Time) domain.WaitlistEntry {
	e.t.Helper()

	w.Status, w.InvitedAt, w.ExpiresAt = domain.WaitlistInvited, w.JoinedAt.Add(time.Minute), expires
	w.InviteTokenHash = [32]byte{31: token}
	valid(e.t, w)
	e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Waitlist().Update(ctx, w) })

	return w
}

func testWaitlist(t *testing.T, newStore func(*testing.T) store.Store) {
	t.Helper()

	t.Run("CRUD", func(t *testing.T) {
		testCRUD(t, newStore, crud[domain.WaitlistEntryID, domain.WaitlistEntry]{
			id: func(w domain.WaitlistEntry) domain.WaitlistEntryID { return w.ID },
			get: func(ctx context.Context, tx store.Tx, id domain.WaitlistEntryID) (domain.WaitlistEntry, error) {
				return tx.Waitlist().Get(ctx, id)
			},
			create: func(ctx context.Context, tx store.Tx, w domain.WaitlistEntry) error {
				return tx.Waitlist().Create(ctx, w)
			},
			update: func(ctx context.Context, tx store.Tx, w domain.WaitlistEntry) error {
				return tx.Waitlist().Update(ctx, w)
			},
			fixtures: func(e *env) (domain.WaitlistEntry, domain.WaitlistEntry) {
				p := e.person("Wanda", "Waiter")
				minimal := e.newEntry(e.lesson(e.setting(), t0, 4), p, t0.Add(-48*time.Hour))
				full := minimal
				full.Status, full.StayOnList = domain.WaitlistAccepted, true
				full.JoinedBy = e.account(p, "wanda@example.org").ID
				full.InviteTokenHash = [32]byte{0: 7, 31: 9}
				full.InvitedAt, full.ExpiresAt = t0.Add(-24*time.Hour), t0.Add(-12*time.Hour)
				valid(e.t, full)

				return minimal, full
			},
		})
	})

	cases(t, newStore, map[string]func(e *env){
		"OneOpenEntryPerPersonAndLesson": func(e *env) {
			s := e.setting()
			l, other := e.lesson(s, t0, 2), e.lesson(s, t0.Add(time.Hour), 2)
			p := e.person("Wim", "Waiter")
			first := e.entry(l, p, t0.Add(-time.Hour))

			again := e.newEntry(l, p, t0.Add(-30*time.Minute))
			err := e.write(func(ctx context.Context, tx store.Tx) error { return tx.Waitlist().Create(ctx, again) })
			wantErr(e.t, err, domain.ErrDuplicate)

			// An invited entry is open too.
			e.invite(first, 1, t0.Add(-10*time.Minute))
			err = e.write(func(ctx context.Context, tx store.Tx) error { return tx.Waitlist().Create(ctx, again) })
			wantErr(e.t, err, domain.ErrDuplicate)

			// Another lesson, or another person, is fine.
			e.entry(other, p, t0.Add(-time.Hour))
			e.entry(l, e.person("Wilma", "Waiter"), t0.Add(-time.Hour))

			// Once the entry ended, the person may join again; an update
			// that would reopen the old one is a duplicate then.
			ended := first
			ended.Status, ended.InviteTokenHash, ended.InvitedAt, ended.ExpiresAt = domain.WaitlistWithdrawn,
				[32]byte{}, time.Time{}, time.Time{}
			e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Waitlist().Update(ctx, ended) })
			e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Waitlist().Create(ctx, again) })

			reopened := ended
			reopened.Status = domain.WaitlistWaiting
			err = e.write(func(ctx context.Context, tx store.Tx) error { return tx.Waitlist().Update(ctx, reopened) })
			wantErr(e.t, err, domain.ErrDuplicate)
		},
		"MissingReferencesAreConflicts": func(e *env) {
			s := e.setting()
			l, p := e.lesson(s, t0, 2), e.person("Wes", "Waiter")

			for name, change := range map[string]func(*domain.WaitlistEntry){
				"lesson":    func(w *domain.WaitlistEntry) { w.LessonID = newID[domain.LessonID](e) },
				"person":    func(w *domain.WaitlistEntry) { w.PersonID = newID[domain.PersonID](e) },
				"joined by": func(w *domain.WaitlistEntry) { w.JoinedBy = newID[domain.AccountID](e) },
			} {
				w := e.newEntry(l, p, t0)
				change(&w)

				err := e.write(func(ctx context.Context, tx store.Tx) error { return tx.Waitlist().Create(ctx, w) })
				wantErr(e.t, err, domain.ErrConflict)
				wantNotErr(e.t, err, domain.ErrDuplicate)

				if e.t.Failed() {
					e.t.Fatalf("missing %s", name)
				}
			}
		},
		"TokenLookupAndUniqueness": func(e *env) {
			s := e.setting()
			l := e.lesson(s, t0, 2)
			a := e.invite(e.entry(l, e.person("Ana", "Waiter"), t0.Add(-3*time.Hour)), 1, t0)
			b := e.entry(l, e.person("Bea", "Waiter"), t0.Add(-2*time.Hour))

			got := mustQuery(e, func(ctx context.Context, tx store.Tx) (domain.WaitlistEntry, error) {
				return tx.Waitlist().GetByTokenHash(ctx, a.InviteTokenHash)
			})
			wantEqual(e.t, got, a)

			for _, hash := range [][32]byte{{31: 2}, {}} {
				_, err := query(e, func(ctx context.Context, tx store.Tx) (domain.WaitlistEntry, error) {
					return tx.Waitlist().GetByTokenHash(ctx, hash)
				})
				wantErr(e.t, err, domain.ErrNotFound)
			}

			// A hash belongs to one entry.
			b.Status, b.InvitedAt, b.ExpiresAt, b.InviteTokenHash = domain.WaitlistInvited, t0.Add(-time.Hour), t0,
				a.InviteTokenHash
			err := e.write(func(ctx context.Context, tx store.Tx) error { return tx.Waitlist().Update(ctx, b) })
			wantErr(e.t, err, domain.ErrConflict)
			wantNotErr(e.t, err, domain.ErrDuplicate)

			// Back on the list, an entry keeps the old hash without its
			// invite, and is still found by it.
			a.Status, a.InvitedAt, a.ExpiresAt = domain.WaitlistWaiting, time.Time{}, time.Time{}
			a.JoinedAt = t0.Add(time.Hour)
			valid(e.t, a)
			e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Waitlist().Update(ctx, a) })

			got = mustQuery(e, func(ctx context.Context, tx store.Tx) (domain.WaitlistEntry, error) {
				return tx.Waitlist().GetByTokenHash(ctx, a.InviteTokenHash)
			})
			wantEqual(e.t, got, a)

			// A new invite replaces it: the old hash finds nothing.
			old := a.InviteTokenHash
			a = e.invite(a, 9, t0.Add(3*time.Hour))

			_, err = query(e, func(ctx context.Context, tx store.Tx) (domain.WaitlistEntry, error) {
				return tx.Waitlist().GetByTokenHash(ctx, old)
			})
			wantErr(e.t, err, domain.ErrNotFound)

			// An ended entry keeps its hash and is still found.
			a.Status = domain.WaitlistAccepted
			e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Waitlist().Update(ctx, a) })

			got = mustQuery(e, func(ctx context.Context, tx store.Tx) (domain.WaitlistEntry, error) {
				return tx.Waitlist().GetByTokenHash(ctx, a.InviteTokenHash)
			})
			wantEqual(e.t, got, a)
		},
		"ListsAreOrderedByJoinTime": func(e *env) {
			s := e.setting()
			l, other := e.lesson(s, t0, 2), e.lesson(s, t0.Add(time.Hour), 2)
			ana, bea, cid := e.person("Ana", "Waiter"), e.person("Bea", "Waiter"), e.person("Cid", "Waiter")

			late := e.entry(l, ana, t0.Add(-time.Hour))
			tie2 := e.entry(l, bea, t0.Add(-2*time.Hour))
			tie1 := e.entry(l, cid, t0.Add(-2*time.Hour)) // a later ID: after tie2
			early := e.entry(other, ana, t0.Add(-3*time.Hour))
			ended := e.newEntry(other, bea, t0.Add(-4*time.Hour))
			ended.Status = domain.WaitlistExpired
			e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Waitlist().Create(ctx, ended) })
			invited := e.invite(e.entry(other, cid, t0.Add(-90*time.Minute)), 3, t0)

			got := mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.WaitlistEntry, error) {
				return tx.Waitlist().ListByLesson(ctx, l.ID)
			})
			wantSlice(e.t, got, []domain.WaitlistEntry{tie2, tie1, late})

			got = mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.WaitlistEntry, error) {
				return tx.Waitlist().ListByLesson(ctx, other.ID)
			})
			wantSlice(e.t, got, []domain.WaitlistEntry{ended, early, invited})

			got = mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.WaitlistEntry, error) {
				return tx.Waitlist().ListOpenByLessons(ctx, []domain.LessonID{l.ID, other.ID, newID[domain.LessonID](e)})
			})
			wantSlice(e.t, got, []domain.WaitlistEntry{early, tie2, tie1, invited, late})

			got = mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.WaitlistEntry, error) {
				return tx.Waitlist().ListOpenByLessons(ctx, nil)
			})
			wantSlice(e.t, got, nil)

			got = mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.WaitlistEntry, error) {
				return tx.Waitlist().ListOpenByPersons(ctx, []domain.PersonID{ana.ID, bea.ID, newID[domain.PersonID](e)})
			})
			wantSlice(e.t, got, []domain.WaitlistEntry{early, tie2, late})

			got = mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.WaitlistEntry, error) {
				return tx.Waitlist().ListOpenByPersons(ctx, []domain.PersonID{cid.ID})
			})
			wantSlice(e.t, got, []domain.WaitlistEntry{tie1, invited})

			got = mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.WaitlistEntry, error) {
				return tx.Waitlist().ListOpenByPersons(ctx, nil)
			})
			wantSlice(e.t, got, nil)
		},
		"CountByLessons": func(e *env) {
			s := e.setting()
			l, other, none := e.lesson(s, t0, 2), e.lesson(s, t0.Add(time.Hour), 2), e.lesson(s, t0.Add(2*time.Hour), 2)
			ana, bea := e.person("Ana", "Waiter"), e.person("Bea", "Waiter")

			e.entry(l, ana, t0.Add(-time.Hour))
			ended := e.newEntry(l, bea, t0.Add(-2*time.Hour))
			ended.Status = domain.WaitlistWithdrawn
			e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Waitlist().Create(ctx, ended) })
			e.entry(other, bea, t0.Add(-time.Hour))

			// Ended entries count; lessons without one are left out.
			got := mustQuery(e, func(ctx context.Context, tx store.Tx) (map[domain.LessonID]int, error) {
				return tx.Waitlist().CountByLessons(ctx, []domain.LessonID{l.ID, none.ID, newID[domain.LessonID](e)})
			})
			if len(got) != 1 || got[l.ID] != 2 {
				e.t.Fatalf("CountByLessons = %v, want %v: 2", got, l.ID)
			}

			got = mustQuery(e, func(ctx context.Context, tx store.Tx) (map[domain.LessonID]int, error) {
				return tx.Waitlist().CountByLessons(ctx, nil)
			})
			if len(got) != 0 {
				e.t.Fatalf("CountByLessons(nil) = %v", got)
			}
		},
		"ListDueLessons": func(e *env) {
			s := e.setting()
			now := t0
			ahead := []domain.Lesson{e.lesson(s, now.Add(5*time.Hour), 2), e.lesson(s, now.Add(6*time.Hour), 2)}
			started := e.lesson(s, now, 2) // starts exactly now
			quiet := e.lesson(s, now.Add(7*time.Hour), 2)
			past := e.lesson(s, now.Add(-time.Hour), 2)
			p, q := e.person("Pia", "Waiter"), e.person("Quin", "Waiter")

			e.invite(e.entry(ahead[1], p, now.Add(-3*time.Hour)), 1, now)                   // expires exactly now
			e.invite(e.entry(ahead[0], p, now.Add(-3*time.Hour)), 2, now.Add(-time.Minute)) // overdue
			e.entry(ahead[0], q, now.Add(-2*time.Hour))
			e.entry(started, q, now.Add(-2*time.Hour)) // waiting, but the lesson started
			e.invite(e.entry(quiet, p, now.Add(-3*time.Hour)), 3, now.Add(time.Microsecond))
			e.entry(quiet, q, now.Add(-2*time.Hour))

			// An ended entry of a past lesson needs no work.
			done := e.newEntry(past, p, now.Add(-3*time.Hour))
			done.Status = domain.WaitlistExpired
			e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Waitlist().Create(ctx, done) })

			got := mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.LessonID, error) {
				return tx.Waitlist().ListDueLessons(ctx, now)
			})

			want := []domain.LessonID{ahead[0].ID, ahead[1].ID, started.ID}
			// Ordered by ID; the fixtures' IDs grow in creation order.
			wantSlice(e.t, got, want)
		},
		"EntriesGoWithTheirLessonAndPerson": func(e *env) {
			s := e.setting()
			l, kept := e.lesson(s, t0, 2), e.lesson(s, t0.Add(time.Hour), 2)
			p, gone := e.person("Pia", "Waiter"), e.person("Gus", "Gone")
			viaLesson := e.invite(e.entry(l, p, t0.Add(-time.Hour)), 1, t0)
			viaPerson := e.entry(kept, gone, t0.Add(-time.Hour))
			stays := e.entry(kept, p, t0.Add(-time.Hour))

			e.mustWrite(func(ctx context.Context, tx store.Tx) error {
				if err := tx.Lessons().Delete(ctx, l.ID); err != nil {
					return err
				}

				return tx.Persons().Delete(ctx, gone.ID)
			})

			for _, id := range []domain.WaitlistEntryID{viaLesson.ID, viaPerson.ID} {
				_, err := query(e, func(ctx context.Context, tx store.Tx) (domain.WaitlistEntry, error) {
					return tx.Waitlist().Get(ctx, id)
				})
				wantErr(e.t, err, domain.ErrNotFound)
			}

			got := mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.WaitlistEntry, error) {
				return tx.Waitlist().ListByLesson(ctx, kept.ID)
			})
			wantSlice(e.t, got, []domain.WaitlistEntry{stays})
		},
	})
}
