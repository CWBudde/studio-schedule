package service

import (
	"context"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// The announcement fault tests run on the announcement fixture with a
// lesson of the Monday class that Mia is booked for, so that every audience
// reaches members, bookings and guardians.

// newAnnouncementFaultFixture returns the announcement fixture with Mia
// booked for a lesson of her class on Monday.
func newAnnouncementFaultFixture(t *testing.T) *announcementFixture {
	t.Helper()

	f := newAnnouncementFixture(t)
	at := mustInstant(t, "2026-10-26T16:00:00Z")
	classLesson := domain.Lesson{
		ID: ids.New[domain.LessonID](f.seq), ClassID: f.class.ID, CourseTypeID: f.courseType, RoomID: f.room,
		InstructorID: f.instructor, StartsAt: at, EndsAt: at.Add(75 * time.Minute), Status: domain.LessonScheduled,
		Visibility: domain.VisibilityPublic, Kind: domain.LessonRegular, Capacity: 9,
	}
	f.write(t, func(ctx context.Context, tx store.Tx) error { return tx.Lessons().Create(ctx, classLesson) })
	f.bookFor(t, classLesson, f.mia)

	return f
}

// faultService returns an AnnouncementService at now on a FaultStore over
// the fixture's store; wake counts into wakes.
func (f *announcementFixture) faultService(t *testing.T, now time.Time, wakes *int) (*FaultStore, *AnnouncementService) {
	t.Helper()

	fs := NewFaultStore(f.st)

	return fs, NewAnnouncementService(fs, clock.NewFixed(now), f.seq, newTestRenderer(t), func() { *wakes++ })
}

// audiences are the audiences the sweeps write and read, one of each kind
// that needs a reference.
func (f *announcementFixture) audiences() map[string]AnnouncementAudience {
	return map[string]AnnouncementAudience{
		"everyone":    audience(domain.AudienceEveryone),
		"role":        {Audience: domain.AudienceRole, Role: domain.RoleMember},
		"class":       {Audience: domain.AudienceClass, ClassID: f.class.ID},
		"course type": {Audience: domain.AudienceCourseType, CourseTypeID: f.courseType},
		"lesson":      {Audience: domain.AudienceLesson, LessonID: f.lesson.ID},
	}
}

// unchangedNoWake is the check of a write that failed: nothing was stored
// and the announcements job was not woken.
func unchangedNoWake(wakes *int) func(t *testing.T, fs *FaultStore, before StoreData, call FaultCall) {
	return func(t *testing.T, fs *FaultStore, before StoreData, call FaultCall) {
		t.Helper()

		if changed := before.Changed(fs.Snapshot(t)); len(changed) > 0 {
			t.Errorf("with %s failing, the store changed: %v", call, changed)
		}

		if *wakes != 0 {
			t.Errorf("with %s failing, the job was woken %d times", call, *wakes)
		}
	}
}

// TestAnnouncementCreateStoreFaults: a store failure while an emailed
// announcement is written, for any audience, is returned; nothing is saved,
// nothing takes a place in the daily limit, and the job is not woken.
func TestAnnouncementCreateStoreFaults(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"everyone", "class", "course type", "lesson"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var wakes int

			SweepFaults(t, FaultSweep{
				Setup: func(t *testing.T) (*FaultStore, func() error) {
					f := newAnnouncementFaultFixture(t)
					wakes = 0
					fs, svc := f.faultService(t, mustInstant(t, bookingNow), &wakes)
					olga := f.principal(t, f.olga)

					return fs, func() error {
						_, err := svc.Create(t.Context(), olga, AnnouncementInput{
							AnnouncementAudience: f.audiences()[name], Title: "Studio closed", Body: "On Friday.",
							Email: true,
						})

						return err
					}
				},
				Check: unchangedNoWake(&wakes),
			})
		})
	}
}

// TestAnnouncementUpdateStoreFaults: a failed edit, which turns email on
// and so checks the daily limit, keeps the announcement as it was.
func TestAnnouncementUpdateStoreFaults(t *testing.T) {
	t.Parallel()

	var wakes int

	SweepFaults(t, FaultSweep{
		Setup: func(t *testing.T) (*FaultStore, func() error) {
			f := newAnnouncementFaultFixture(t)
			now := mustInstant(t, bookingNow)
			a := f.create(t, f.serviceAt(t, now), AnnouncementInput{
				AnnouncementAudience: audience(domain.AudienceEveryone), Title: "Draft", Body: "Hi.",
			})
			wakes = 0
			fs, svc := f.faultService(t, now, &wakes)

			return fs, func() error {
				_, err := svc.Update(t.Context(), a.ID, AnnouncementInput{
					AnnouncementAudience: AnnouncementAudience{Audience: domain.AudienceClass, ClassID: f.class.ID},
					Title:                "Costume fitting", Body: "On Saturday.", Email: true,
				})

				return err
			}
		},
		Check: unchangedNoWake(&wakes),
	})
}

// TestAnnouncementDeleteStoreFaults: deleting an announcement while it is
// being sent records its mailing; if anything fails, neither the deletion
// nor the mailing stays.
func TestAnnouncementDeleteStoreFaults(t *testing.T) {
	t.Parallel()

	SweepFaults(t, FaultSweep{Setup: func(t *testing.T) (*FaultStore, func() error) {
		f := newAnnouncementFaultFixture(t)
		now := mustInstant(t, bookingNow)
		svc := f.serviceAt(t, now)
		svc.recipientBatch = 1
		a := f.create(t, svc, AnnouncementInput{
			AnnouncementAudience: audience(domain.AudienceEveryone), Title: "Hello all", Body: "Hi.", Email: true,
		})
		f.sendDue(t, svc)

		var wakes int

		fs, faulty := f.faultService(t, now, &wakes)

		return fs, func() error { return faulty.Delete(t.Context(), a.ID) }
	}})
}

// TestAnnouncementSendDueStoreFaults: a run of the announcements job is
// one transaction. If any store call in it fails, no mail is queued, no
// mailing recorded, and no announcement's cursor or recipient count moves:
// the next run sends the same batch, nobody is skipped and nobody mailed
// twice.
func TestAnnouncementSendDueStoreFaults(t *testing.T) {
	t.Parallel()

	SweepFaults(t, FaultSweep{Setup: func(t *testing.T) (*FaultStore, func() error) {
		f := newAnnouncementFaultFixture(t)
		now := mustInstant(t, bookingNow)
		writer := f.serviceAt(t, now)

		// Due in this order (ListDueForMail goes by PublishAt): the class
		// (Anna for Mia), the course type (Anna, Stella), the lesson
		// (Stella), each mailed in full, and everyone (four accounts),
		// which the batch of six cuts after two. The first was written for
		// two days ago (three a day may be emailed).
		f.create(t, f.serviceAt(t, now.Add(-48*time.Hour)), AnnouncementInput{
			AnnouncementAudience: f.audiences()["class"], Title: "To the class", Body: "Hi.", Email: true,
		})

		for i, name := range []string{"course type", "lesson", "everyone"} {
			f.create(t, writer, AnnouncementInput{
				AnnouncementAudience: f.audiences()[name], Title: "To " + name, Body: "Hi.",
				PublishAt: now.Add(time.Duration(i-3) * time.Hour), Email: true,
			})
		}

		var wakes int

		fs, svc := f.faultService(t, now, &wakes)
		svc.recipientBatch = 6

		return fs, func() error {
			n, err := svc.SendDue(t.Context())
			if err == nil && n != 6 {
				t.Fatalf("SendDue queued %d, want 6", n)
			}

			return err
		}
	}})
}

// TestAnnouncementReadStoreFaults: the reads return a store failure instead
// of a partial answer (an announcement missing from an account's banner, a
// recipient count too small).
func TestAnnouncementReadStoreFaults(t *testing.T) {
	t.Parallel()

	// setup writes an announcement for each audience, publishes them an
	// hour ago, and returns the operation that op makes of the service.
	setup := func(op func(t *testing.T, f *announcementFixture, svc *AnnouncementService) error) func(
		t *testing.T,
	) (*FaultStore, func() error) {
		return func(t *testing.T) (*FaultStore, func() error) {
			f := newAnnouncementFaultFixture(t)
			now := mustInstant(t, bookingNow)
			writer := f.serviceAt(t, now)

			for name, aud := range f.audiences() {
				f.create(t, writer, AnnouncementInput{
					AnnouncementAudience: aud, Title: "To " + name, Body: "Hi.", PublishAt: now.Add(-time.Hour),
				})
			}

			var wakes int

			fs, svc := f.faultService(t, now, &wakes)

			return fs, func() error { return op(t, f, svc) }
		}
	}

	t.Run("List", func(t *testing.T) {
		t.Parallel()

		SweepFaults(t, FaultSweep{Setup: setup(func(t *testing.T, _ *announcementFixture, svc *AnnouncementService) error {
			_, err := svc.List(t.Context())

			return err
		})})
	})

	t.Run("ForAccount", func(t *testing.T) {
		t.Parallel()

		SweepFaults(t, FaultSweep{Setup: setup(func(t *testing.T, f *announcementFixture, svc *AnnouncementService) error {
			got, err := svc.ForAccount(t.Context(), f.principal(t, f.anna))
			if err == nil && len(got) != 4 {
				t.Fatalf("Anna sees %q, want four", titles(got))
			}

			return err
		})})
	})

	t.Run("Dismiss", func(t *testing.T) {
		t.Parallel()

		SweepFaults(t, FaultSweep{Setup: setup(func(t *testing.T, f *announcementFixture, svc *AnnouncementService) error {
			shown, err := f.serviceAt(t, mustInstant(t, bookingNow)).ForAccount(t.Context(), f.principal(t, f.anna))
			if err != nil {
				t.Fatal(err)
			}

			for _, v := range shown {
				if v.Announcement.Audience == domain.AudienceCourseType {
					return svc.Dismiss(t.Context(), f.principal(t, f.anna), v.Announcement.ID)
				}
			}

			t.Fatal("Anna does not see the course type's announcement")

			return nil
		})})
	})

	for _, name := range []string{"everyone", "role", "class", "course type", "lesson"} {
		t.Run("CountRecipients/"+name, func(t *testing.T) {
			t.Parallel()

			SweepFaults(t, FaultSweep{Setup: setup(func(t *testing.T, f *announcementFixture, svc *AnnouncementService) error {
				_, err := svc.CountRecipients(t.Context(), f.audiences()[name])

				return err
			})})
		})
	}
}
