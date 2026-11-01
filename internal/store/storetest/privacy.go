package storetest

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// testPrivacy covers what ADR 0016 adds: anonymized persons, deleting an
// account and the retention of personal data.
func testPrivacy(t *testing.T, newStore func(*testing.T) store.Store) {
	t.Helper()

	cases(t, newStore, map[string]func(e *env){
		"AnonymizedPerson": func(e *env) {
			p := e.person("Mira", "Minimal")
			kept := e.person("Kai", "Kept")

			anon := p.Anonymize(t0.Add(500 * time.Nanosecond)) // cut to microseconds
			valid(e.t, anon)
			e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Persons().Update(ctx, anon) })

			anon.AnonymizedAt = t0
			got := mustQuery(e, func(ctx context.Context, tx store.Tx) (domain.Person, error) {
				return tx.Persons().Get(ctx, p.ID)
			})
			wantEqual(e.t, got, anon)

			// Lists leave it out; by ID it is found.
			listed := mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.Person, error) {
				return tx.Persons().List(ctx)
			})
			wantSlice(e.t, listed, []domain.Person{kept})

			byIDs := mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.Person, error) {
				return tx.Persons().ListByIDs(ctx, []domain.PersonID{p.ID})
			})
			wantSlice(e.t, byIDs, []domain.Person{anon})
		},
		"DeleteAccount": func(e *env) { testDeleteAccount(e) },
		"ListAccountsByRole": func(e *env) {
			promote := func(a domain.Account, change func(*domain.Account)) domain.Account {
				change(&a)
				valid(e.t, a)
				e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Accounts().Update(ctx, a) })

				return a
			}
			admin := func(a *domain.Account) { a.Role = domain.RoleAdmin }

			zed := promote(e.account(e.person("Zed", "Admin"), "zed@example.org"), admin)
			ada := promote(e.account(e.person("Ada", "Admin"), "Ada@example.org"), func(a *domain.Account) {
				a.Role, a.DisabledAt = domain.RoleAdmin, t0
			})
			e.account(e.person("Ben", "Member"), "ben@example.org")
			office := promote(e.account(e.person("Olga", "Office"), "olga@example.org"), func(a *domain.Account) {
				a.Role = domain.RoleOffice
			})

			byRole := func(role domain.Role) []domain.Account {
				return mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.Account, error) {
					return tx.Accounts().ListByRole(ctx, role)
				})
			}

			// Disabled ones too, ordered by lower-cased email.
			wantSlice(e.t, byRole(domain.RoleAdmin), []domain.Account{ada, zed})
			wantSlice(e.t, byRole(domain.RoleOffice), []domain.Account{office})
			wantSlice(e.t, byRole(domain.RoleInstructor), nil)
		},
		"AuditActorRequired": func(e *env) {
			p := e.person("Dora", "Deleted")
			admin := e.account(e.person("Ada", "Admin"), "ada@example.org")

			// Only an account deletion by its holder has no actor.
			deleted := domain.AuditEvent{
				ID: newID[domain.AuditEventID](e), At: t0, Action: domain.AuditAccountDeleted,
				TargetKind: domain.AuditTargetPerson, TargetPersonID: p.ID,
			}
			valid(e.t, deleted)
			e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Audit().Create(ctx, deleted) })

			for _, ev := range []domain.AuditEvent{
				{
					ID: newID[domain.AuditEventID](e), At: t0, Action: domain.AuditPersonAnonymized,
					TargetKind: domain.AuditTargetPerson, TargetPersonID: p.ID,
				},
				{
					ID: newID[domain.AuditEventID](e), At: t0, Action: domain.AuditAccountDisabled,
					TargetKind: domain.AuditTargetAccount, TargetID: admin.ID,
				},
			} {
				err := e.write(func(ctx context.Context, tx store.Tx) error { return tx.Audit().Create(ctx, ev) })
				wantErr(e.t, err, domain.ErrValidation)
			}

			wantSlice(e.t, mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.AuditEvent, error) {
				return tx.Audit().List(ctx, 10)
			}), []domain.AuditEvent{deleted})
		},
		"DeleteAccountKept": func(e *env) {
			s := e.setting()
			l := e.lesson(s, t0, 10)

			for i, c := range []struct {
				name string
				keep func(acc domain.Account) error
			}{
				{"receipt", func(acc domain.Account) error {
					r := e.newReceipt(2026, 1, e.person("Paul", "Payer"))
					r.CreatedBy = acc.ID

					return e.write(func(ctx context.Context, tx store.Tx) error { return tx.Receipts().Create(ctx, r) })
				}},
				{"audit actor", func(acc domain.Account) error {
					ev := domain.AuditEvent{
						ID: newID[domain.AuditEventID](e), At: t0, ActorID: acc.ID, Action: domain.AuditPersonAnonymized,
						TargetKind: domain.AuditTargetPerson, TargetPersonID: e.person("Tia", "Target").ID,
					}
					valid(e.t, ev)

					return e.write(func(ctx context.Context, tx store.Tx) error { return tx.Audit().Create(ctx, ev) })
				}},
				{"announcement", func(acc domain.Account) error {
					a := e.newAnnouncement(acc, "Closed", t0)

					return e.write(func(ctx context.Context, tx store.Tx) error { return tx.Announcements().Create(ctx, a) })
				}},
				{"mailing", func(acc domain.Account) error {
					a := e.newAnnouncement(e.account(e.person("Otto", "Office"), "otto@example.org"), "Closed", t0)
					e.createAnnouncements(a)
					m := e.mailing(a, acc, t0)

					return e.write(func(ctx context.Context, tx store.Tx) error {
						return tx.Announcements().CreateMailing(ctx, m)
					})
				}},
				{"dismissal", func(acc domain.Account) error {
					d := domain.NewDuplicateDismissal(e.person("Ann", "A").ID, e.person("Ann", "B").ID, acc.ID, t0)

					return e.write(func(ctx context.Context, tx store.Tx) error { return tx.DuplicateDismissals().Create(ctx, d) })
				}},
			} {
				name := c.name
				acc := e.account(e.person("Kim", "Kept"), fmt.Sprintf("kept%d@example.org", i))
				if err := c.keep(acc); err != nil {
					e.t.Fatalf("%s: %v", name, err)
				}

				// Something it would delete or clear, to see that nothing
				// changes.
				cred := domain.Credential{AccountID: acc.ID, PasswordHash: "h", ChangedAt: t0}
				ev := domain.BookingEvent{
					ID: newID[domain.BookingEventID](e), BookingID: e.booking(l, e.person("Bo", name), t0).ID, At: t0,
					Status: domain.BookingBooked, ActorAccountID: acc.ID,
				}
				e.mustWrite(func(ctx context.Context, tx store.Tx) error {
					if err := tx.Credentials().Put(ctx, cred); err != nil {
						return err
					}

					return tx.Bookings().AddEvent(ctx, ev)
				})

				err := e.write(func(ctx context.Context, tx store.Tx) error { return tx.Accounts().Delete(ctx, acc.ID) })
				if !errorsIs(err, domain.ErrConflict) {
					e.t.Fatalf("%s: delete: want ErrConflict, got %v", name, err)
				}

				wantEqual(e.t, mustQuery(e, func(ctx context.Context, tx store.Tx) (domain.Account, error) {
					return tx.Accounts().Get(ctx, acc.ID)
				}), acc)
				wantEqual(e.t, mustQuery(e, func(ctx context.Context, tx store.Tx) (domain.Credential, error) {
					return tx.Credentials().Get(ctx, acc.ID)
				}), cred)
				wantSlice(e.t, mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.BookingEvent, error) {
					return tx.Bookings().ListEvents(ctx, ev.BookingID)
				}), []domain.BookingEvent{ev})
			}
		},
		"DeleteAccountUnknown": func(e *env) {
			err := e.write(func(ctx context.Context, tx store.Tx) error {
				return tx.Accounts().Delete(ctx, newID[domain.AccountID](e))
			})
			wantErr(e.t, err, domain.ErrNotFound)

			acc := e.account(e.person("Ada", "Account"), "ada@example.org")
			err = e.read(func(ctx context.Context, tx store.Tx) error { return tx.Accounts().Delete(ctx, acc.ID) })
			wantErr(e.t, err, store.ErrReadOnly)
		},
		"AnonymizeGuests": func(e *env) {
			s := e.setting()
			cutoff := t0.Add(24 * time.Hour)
			// Lessons last an hour: ended a microsecond before the cutoff,
			// exactly at it, and after it.
			before := e.lesson(s, cutoff.Add(-time.Hour-time.Microsecond), 10)
			at := e.lesson(s, cutoff.Add(-time.Hour), 10)

			guest := func(l domain.Lesson, name, email string, trial bool) domain.Booking {
				b := domain.Booking{
					ID: newID[domain.BookingID](e), LessonID: l.ID, GuestName: name, GuestEmail: email,
					Status: domain.BookingBooked, Source: domain.BookingSourceStaff, BookedAt: t0, Trial: trial,
				}
				if trial {
					b.TrialNote = "First time."
				}

				valid(e.t, b)
				e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Bookings().Create(ctx, b) })

				return b
			}

			old := guest(before, "Gina Guest", "gina@example.org", true)
			canceled := guest(before, "Gus Guest", "", false)
			canceled.Status, canceled.CanceledAt = domain.BookingCanceled, t0.Add(time.Minute)
			e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Bookings().Update(ctx, canceled) })

			edge := guest(at, "Edda Edge", "edda@example.org", false)
			member := e.booking(before, e.person("Mia", "Member"), t0)

			anonymize := func() int {
				var n int

				e.mustWrite(func(ctx context.Context, tx store.Tx) error {
					var err error
					n, err = tx.Bookings().AnonymizeGuests(ctx, cutoff)

					return err
				})

				return n
			}

			wantEqual(e.t, anonymize(), 2)
			wantEqual(e.t, anonymize(), 0) // anonymized already

			get := func(id domain.BookingID) domain.Booking {
				return mustQuery(e, func(ctx context.Context, tx store.Tx) (domain.Booking, error) {
					return tx.Bookings().Get(ctx, id)
				})
			}

			old.GuestName, old.GuestEmail, old.TrialNote = domain.AnonymousGuestName, "", ""
			wantEqual(e.t, get(old.ID), old)

			canceled.GuestName = domain.AnonymousGuestName
			wantEqual(e.t, get(canceled.ID), canceled)
			wantEqual(e.t, get(edge.ID), edge)
			wantEqual(e.t, get(member.ID), member)

			_, err := query(e, func(ctx context.Context, tx store.Tx) (int, error) {
				return tx.Bookings().AnonymizeGuests(ctx, cutoff)
			})
			wantErr(e.t, err, store.ErrReadOnly)
		},
		"DeleteAuditBefore": func(e *env) {
			admin := e.account(e.person("Ada", "Admin"), "ada@example.org")
			target := e.account(e.person("Ben", "Member"), "ben@example.org")

			event := func(at time.Time) domain.AuditEvent {
				ev := domain.AuditEvent{
					ID: newID[domain.AuditEventID](e), At: at, ActorID: admin.ID, Action: domain.AuditAccountDisabled,
					TargetKind: domain.AuditTargetAccount, TargetID: target.ID,
				}
				valid(e.t, ev)
				e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Audit().Create(ctx, ev) })

				return ev
			}

			event(t0.Add(-time.Microsecond))
			edge := event(t0)

			var n int

			e.mustWrite(func(ctx context.Context, tx store.Tx) error {
				var err error
				n, err = tx.Audit().DeleteBefore(ctx, t0)

				return err
			})
			wantEqual(e.t, n, 1)
			wantSlice(e.t, mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.AuditEvent, error) {
				return tx.Audit().List(ctx, 10)
			}), []domain.AuditEvent{edge})

			_, err := query(e, func(ctx context.Context, tx store.Tx) (int, error) { return tx.Audit().DeleteBefore(ctx, t0) })
			wantErr(e.t, err, store.ErrReadOnly)
		},
		"DeleteImportRunsBefore": func(e *env) {
			run := func(at time.Time) domain.ImportRun {
				r := domain.ImportRun{
					ID: newID[domain.ImportRunID](e), FileName: "members.csv", StartedAt: at,
					FinishedAt: at.Add(time.Second), Counts: domain.ImportCounts{Rows: 1, Unchanged: 1},
				}
				valid(e.t, r)
				e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Imports().Create(ctx, r) })

				return r
			}

			run(t0.Add(-time.Microsecond))
			edge := run(t0)

			var n int

			e.mustWrite(func(ctx context.Context, tx store.Tx) error {
				var err error
				n, err = tx.Imports().DeleteStartedBefore(ctx, t0)

				return err
			})
			wantEqual(e.t, n, 1)

			runs := mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.ImportRun, error) {
				return tx.Imports().List(ctx, 10)
			})
			wantEqual(e.t, len(runs), 1)
			wantEqual(e.t, runs[0].ID, edge.ID)

			_, err := query(e, func(ctx context.Context, tx store.Tx) (int, error) {
				return tx.Imports().DeleteStartedBefore(ctx, t0)
			})
			wantErr(e.t, err, store.ErrReadOnly)
		},
		"WaitlistByPersons": func(e *env) {
			s := e.setting()
			l1 := e.lesson(s, t0, 1)
			l2 := e.lesson(s, t0.Add(time.Hour), 1)
			p := e.person("Wanda", "Waiting")
			other := e.person("Otto", "Other")

			late := e.entry(l1, p, t0.Add(2*time.Minute))
			ended := e.entry(l2, p, t0.Add(time.Minute))
			ended.Status = domain.WaitlistWithdrawn
			e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Waitlist().Update(ctx, ended) })
			theirs := e.entry(l2, other, t0)

			list := func(persons ...domain.PersonID) []domain.WaitlistEntry {
				return mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.WaitlistEntry, error) {
					return tx.Waitlist().ListByPersons(ctx, persons)
				})
			}

			wantSlice(e.t, list(p.ID), []domain.WaitlistEntry{ended, late})
			wantSlice(e.t, list(p.ID, other.ID), []domain.WaitlistEntry{theirs, ended, late})
			wantSlice(e.t, list(), nil)
		},
	})
}

// testDeleteAccount deletes an account with everything that refers to it
// and checks what goes, what forgets it, and what stays.
func testDeleteAccount(e *env) {
	s := e.setting()
	l := e.lesson(s, t0, 10)
	p := e.person("Dora", "Deleted")
	acc := e.account(p, "dora@example.org")
	admin := e.account(e.person("Ada", "Admin"), "ada@example.org")
	other := e.account(e.person("Oli", "Other"), "oli@example.org")
	otherSession := e.session(other, "other", t0.Add(time.Hour))

	b := e.booking(l, p, t0)
	ev := domain.BookingEvent{
		ID: newID[domain.BookingEventID](e), BookingID: b.ID, At: t0, Status: domain.BookingBooked, ActorAccountID: acc.ID,
	}
	w := e.newEntry(e.lesson(s, t0.Add(time.Hour), 1), e.person("Wim", "Waiting"), t0)
	w.JoinedBy = acc.ID
	canceled := e.newLesson(s, t0.Add(2*time.Hour), 10)
	canceled.Status, canceled.CanceledAt, canceled.CanceledBy = domain.LessonCanceled, t0, acc.ID
	pass := e.newPass(e.passType("10 visits"), p, 10, t0)
	pass.SoldBy = acc.ID
	run := domain.ImportRun{
		ID: newID[domain.ImportRunID](e), FileName: "members.csv", ActorID: acc.ID, StartedAt: t0,
		FinishedAt: t0.Add(time.Second), Counts: domain.ImportCounts{Rows: 1, Unchanged: 1},
	}
	notice := e.notice(l, acc, t0)
	feed := e.feed(acc, domain.FeedBookings, domain.LocationID{}, t0)
	announcement := e.newAnnouncement(admin, "Closed", t0)
	impersonated := domain.AuditEvent{
		ID: newID[domain.AuditEventID](e), At: t0, ActorID: admin.ID, Action: domain.AuditImpersonationStarted,
		TargetKind: domain.AuditTargetAccount, TargetID: acc.ID,
	}
	token := e.accountToken(acc, "link", t0.Add(time.Hour))
	event := e.newEvent(s.courseType, "summer-intensive")
	event.CreatedBy = acc.ID
	registration := e.newRegistration(event, e.person("Rhea", "Registered"), t0)
	registration.RegisteredBy = acc.ID

	e.mustWrite(func(ctx context.Context, tx store.Tx) error {
		for _, write := range []func() error{
			func() error { return tx.Bookings().AddEvent(ctx, ev) },
			func() error { return tx.Waitlist().Create(ctx, w) },
			func() error { return tx.Lessons().Create(ctx, canceled) },
			func() error { return tx.Passes().Create(ctx, pass) },
			func() error { return tx.Imports().Create(ctx, run) },
			func() error { return tx.Notices().Create(ctx, notice) },
			func() error { return tx.CalendarFeeds().Create(ctx, feed) },
			func() error { return tx.Announcements().Create(ctx, announcement) },
			func() error {
				return tx.Announcements().Dismiss(ctx, domain.AnnouncementDismissal{
					AnnouncementID: announcement.ID, AccountID: acc.ID, DismissedAt: t0,
				})
			},
			func() error { return tx.Audit().Create(ctx, impersonated) },
			func() error { return tx.Events().Create(ctx, event) },
			func() error { return tx.EventRegistrations().Create(ctx, registration) },
			func() error {
				return tx.Credentials().Put(ctx, domain.Credential{AccountID: acc.ID, PasswordHash: "h", ChangedAt: t0})
			},
		} {
			if err := write(); err != nil {
				return err
			}
		}

		return nil
	})

	own := e.session(acc, "own", t0.Add(time.Hour))
	started := e.session(other, "started", t0.Add(time.Hour))
	started.ImpersonatorID = acc.ID
	e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Sessions().Update(ctx, started) })

	e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Accounts().Delete(ctx, acc.ID) })

	e.mustWrite(func(ctx context.Context, tx store.Tx) error {
		gone := []error{}

		_, err := tx.Accounts().Get(ctx, acc.ID)
		gone = append(gone, err)
		_, err = tx.Credentials().Get(ctx, acc.ID)
		gone = append(gone, err)
		_, err = tx.Sessions().GetByToken(ctx, own.TokenHash)
		gone = append(gone, err)
		_, err = tx.Sessions().GetByToken(ctx, started.TokenHash)
		gone = append(gone, err)
		_, err = tx.AccountTokens().Get(ctx, token.Hash)
		gone = append(gone, err)
		_, err = tx.CalendarFeeds().Get(ctx, feed.ID)
		gone = append(gone, err)

		for i, err := range gone {
			if !errorsIs(err, domain.ErrNotFound) {
				e.t.Errorf("record %d: want ErrNotFound, got %v", i, err)
			}
		}

		if got, err := tx.Sessions().GetByToken(ctx, otherSession.TokenHash); err != nil || got != otherSession {
			e.t.Errorf("another account's session: %v, %v", got, err)
		}

		if got, err := tx.Persons().Get(ctx, p.ID); err != nil || got != p {
			e.t.Errorf("the person stays: %v, %v", got, err)
		}

		dismissed, err := tx.Announcements().ListDismissed(ctx, acc.ID, []domain.AnnouncementID{announcement.ID})
		if err != nil || len(dismissed) != 0 {
			e.t.Errorf("dismissals: %v, %v", dismissed, err)
		}

		events, err := tx.Bookings().ListEvents(ctx, b.ID)
		if err != nil {
			return err
		}

		ev.ActorAccountID = domain.AccountID{}
		wantSlice(e.t, events, []domain.BookingEvent{ev})

		if got, err := tx.Waitlist().Get(ctx, w.ID); err != nil || !got.JoinedBy.IsZero() {
			e.t.Errorf("waiting-list entry: %v, %v", got, err)
		}

		if got, err := tx.Lessons().Get(ctx, canceled.ID); err != nil || !got.CanceledBy.IsZero() {
			e.t.Errorf("canceled lesson: %v, %v", got, err)
		}

		if got, err := tx.Passes().Get(ctx, pass.ID); err != nil || !got.SoldBy.IsZero() {
			e.t.Errorf("pass: %v, %v", got, err)
		}

		event.CreatedBy = domain.AccountID{}
		if got, err := tx.Events().Get(ctx, event.ID); err != nil || got != event {
			e.t.Errorf("event: %+v, %v", got, err)
		}

		registration.RegisteredBy = domain.AccountID{}
		if got, err := tx.EventRegistrations().Get(ctx, registration.ID); err != nil || got != registration {
			e.t.Errorf("event registration: %+v, %v", got, err)
		}

		runs, err := tx.Imports().List(ctx, 10)
		if err != nil || len(runs) != 1 || !runs[0].ActorID.IsZero() {
			e.t.Errorf("import runs: %v, %v", runs, err)
		}

		notices, err := tx.Notices().ListByLesson(ctx, l.ID, time.Time{})
		if err != nil {
			return err
		}

		notice.SenderID = domain.AccountID{}
		wantSlice(e.t, notices, []domain.LessonNotice{notice})

		audit, err := tx.Audit().List(ctx, 10)
		if err != nil {
			return err
		}

		// Re-pointed to the person, and still a valid event.
		wantSlice(e.t, audit, []domain.AuditEvent{impersonated.Retarget(p.ID)})

		for _, ev := range audit {
			valid(e.t, ev)
		}

		// The person can get a new account with the same address.
		again := domain.Account{ID: newID[domain.AccountID](e), PersonID: p.ID, Email: acc.Email, Role: domain.RoleMember}

		return tx.Accounts().Create(ctx, again)
	})
}
