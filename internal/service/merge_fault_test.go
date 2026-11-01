package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/seed"
	"github.com/cwbudde/studio-schedule/internal/service"
	"github.com/cwbudde/studio-schedule/internal/store"
	"github.com/cwbudde/studio-schedule/internal/store/memory"
)

// mergeFaultNow is the merge sweep's clock: Wednesday 21 October 2026.
var mergeFaultNow = time.Date(2026, time.October, 21, 10, 0, 0, 0, time.UTC)

// mergeFaultWorld is a school in which a merge touches every kind of record
// it moves: bookings that move, are canceled or stay as history, waiting-list
// entries that move or are withdrawn (the survivor's too), memberships of
// classes in two terms, passes owned and paid for, guardian links that move
// or are dropped, a staff link, an account, dismissals and an earlier
// tombstone. All fictional.
type mergeFaultWorld struct {
	t   *testing.T
	mem *memory.Store
	seq *ids.Sequence

	survivor, merged domain.PersonID
}

func newMergeFaultWorld(t *testing.T) *mergeFaultWorld {
	t.Helper()

	w := &mergeFaultWorld{t: t, mem: memory.New(), seq: ids.NewSequence()}
	now := mergeFaultNow

	settings := seed.DemoSettings()
	settings.RedemptionMode = domain.RedeemOnBooking

	location := domain.Location{ID: ids.New[domain.LocationID](w.seq), Name: "Studio Nowhere"}
	room := domain.Room{ID: ids.New[domain.RoomID](w.seq), LocationID: location.ID, Name: "Room One", Capacity: 12, Position: 1}
	courseType := domain.CourseType{ID: ids.New[domain.CourseTypeID](w.seq), Name: "Imaginary Ballet"}
	instructor := domain.Staff{
		ID: ids.New[domain.StaffID](w.seq), DisplayName: "Ada Example", Kind: domain.StaffInstructor, Active: true,
	}
	passType := domain.PassType{
		ID: ids.New[domain.PassTypeID](w.seq), Name: "10-visit pass", Kind: domain.PassVisits, Visits: 10,
		Active: true, CreatedAt: now,
	}

	terms := []domain.Term{
		{
			ID: ids.New[domain.TermID](w.seq), Name: "Autumn", StartsOn: domain.Date{Year: 2026, Month: time.September, Day: 1},
			EndsOn: domain.Date{Year: 2027, Month: time.January, Day: 31},
		},
		{
			ID: ids.New[domain.TermID](w.seq), Name: "Spring", StartsOn: domain.Date{Year: 2027, Month: time.February, Day: 1},
			EndsOn: domain.Date{Year: 2027, Month: time.June, Day: 30},
		},
	}

	var classes []domain.Class

	// The merged person is a member of a class in each term; the survivor of
	// the second.
	for i, term := range terms {
		classes = append(classes, domain.Class{
			ID: ids.New[domain.ClassID](w.seq), TermID: term.ID, CourseTypeID: courseType.ID, RoomID: room.ID,
			InstructorID: instructor.ID, Level: domain.LevelOpen, Weekday: time.Weekday(i + 1),
			StartTime: domain.TimeOfDay{Hour: 17}, Duration: time.Hour, Capacity: 10,
		})
	}

	lesson := func(hours int) domain.Lesson {
		start := now.Add(time.Duration(hours) * time.Hour)

		return domain.Lesson{
			ID: ids.New[domain.LessonID](w.seq), CourseTypeID: courseType.ID, RoomID: room.ID, InstructorID: instructor.ID,
			StartsAt: start, EndsAt: start.Add(time.Hour), Status: domain.LessonScheduled,
			Visibility: domain.VisibilityPublic, Kind: domain.LessonRegular, Capacity: 5,
		}
	}

	var (
		both       = lesson(24)  // both booked: the merged person's is canceled
		history    = lesson(-48) // both attended: both stay
		moving     = lesson(48)  // the merged person booked, the survivor waits
		waitBooked = lesson(72)  // the survivor booked, the merged person waits
		waitBoth   = lesson(96)  // both wait
		waitMove   = lesson(120) // the merged person waits
		elsewhere  = lesson(144) // the survivor waits; the merge leaves it alone
	)

	person := func(given, family string) domain.Person {
		return domain.Person{ID: ids.New[domain.PersonID](w.seq), GivenName: given, FamilyName: family}
	}

	survivor, merged := person("Sara", "Example"), person("Sara", "Example")
	guardian, kid, other := person("Gerd", "Example"), person("Kim", "Example"), person("Sara", "Example")
	tombstone := person("Sarah", "Example")
	merged.MemberNo, merged.Notes = "M-0007", "Imported."
	w.survivor, w.merged = survivor.ID, merged.ID

	office := person("Olga", "Office")
	officeAcc := domain.Account{
		ID: ids.New[domain.AccountID](w.seq), PersonID: office.ID, Email: "olga.office@example.org",
		Role: domain.RoleOffice, ConfirmedAt: now,
	}
	mergedAcc := domain.Account{
		ID: ids.New[domain.AccountID](w.seq), PersonID: merged.ID, Email: "sara@example.org",
		Role: domain.RoleMember, ConfirmedAt: now,
	}

	booking := func(l domain.Lesson, p domain.Person, status domain.BookingStatus) domain.Booking {
		return domain.Booking{
			ID: ids.New[domain.BookingID](w.seq), LessonID: l.ID, PersonID: p.ID, Status: status,
			Source: domain.BookingSourceSelf, BookedAt: now.Add(-72 * time.Hour),
		}
	}
	entry := func(l domain.Lesson, p domain.Person) domain.WaitlistEntry {
		return domain.WaitlistEntry{
			ID: ids.New[domain.WaitlistEntryID](w.seq), LessonID: l.ID, PersonID: p.ID,
			Status: domain.WaitlistWaiting, JoinedAt: now.Add(-time.Hour),
		}
	}
	pass := func(owner, payer domain.Person, left int) domain.Pass {
		p := domain.Pass{
			ID: ids.New[domain.PassID](w.seq), PassTypeID: passType.ID, PersonID: owner.ID, Kind: domain.PassVisits,
			VisitsTotal: 10, VisitsRemaining: left, ValidFrom: domain.Date{Year: 2026, Month: time.October, Day: 1},
			Status: domain.PassActive, Origin: domain.PassAssigned, SoldAt: now.Add(-96 * time.Hour),
		}
		if !payer.ID.IsZero() {
			p.Origin, p.PayerID, p.PricePaid = domain.PassSold, payer.ID, 12000
		}

		return p
	}

	bookings := []domain.Booking{
		booking(both, survivor, domain.BookingBooked),
		booking(both, merged, domain.BookingBooked),
		booking(history, survivor, domain.BookingAttended),
		booking(history, merged, domain.BookingAttended),
		booking(moving, merged, domain.BookingBooked),
		booking(waitBooked, survivor, domain.BookingBooked),
	}
	entries := []domain.WaitlistEntry{
		entry(moving, survivor), entry(waitBooked, merged), entry(waitBoth, survivor), entry(waitBoth, merged),
		entry(waitMove, merged), entry(elsewhere, survivor),
	}
	mergedPass := pass(merged, domain.Person{}, 7)
	passes := []domain.Pass{mergedPass, pass(kid, merged, 10), pass(survivor, domain.Person{}, 5)}

	err := w.mem.InTx(t.Context(), store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		errs := []error{
			tx.Settings().Put(ctx, settings),
			tx.Locations().Create(ctx, location),
			tx.Rooms().Create(ctx, room),
			tx.CourseTypes().Create(ctx, courseType),
			tx.Staff().Create(ctx, instructor),
			tx.PassTypes().Create(ctx, passType),
		}

		for _, term := range terms {
			errs = append(errs, tx.Terms().Create(ctx, term))
		}

		for _, c := range classes {
			errs = append(errs, tx.Classes().Create(ctx, c))
		}

		for _, l := range []domain.Lesson{both, history, moving, waitBooked, waitBoth, waitMove, elsewhere} {
			errs = append(errs, tx.Lessons().Create(ctx, l))
		}

		for _, p := range []domain.Person{survivor, merged, guardian, kid, other, office} {
			errs = append(errs, tx.Persons().Create(ctx, p))
		}

		tombstone.MergedInto = merged.ID
		errs = append(errs,
			tx.Persons().Create(ctx, tombstone),
			tx.Accounts().Create(ctx, officeAcc),
			tx.Accounts().Create(ctx, mergedAcc),
			tx.Persons().AddGuardian(ctx, domain.Guardian{GuardianID: guardian.ID, DependentID: survivor.ID}),
			tx.Persons().AddGuardian(ctx, domain.Guardian{GuardianID: guardian.ID, DependentID: merged.ID}),
			tx.Persons().AddGuardian(ctx, domain.Guardian{GuardianID: merged.ID, DependentID: kid.ID}),
			tx.Staff().Create(ctx, domain.Staff{
				ID: ids.New[domain.StaffID](w.seq), PersonID: merged.ID, DisplayName: "Sara E.",
				Kind: domain.StaffAccompanist, Active: true,
			}),
			tx.DuplicateDismissals().Create(ctx, domain.NewDuplicateDismissal(merged.ID, other.ID, officeAcc.ID, now)),
			tx.DuplicateDismissals().Create(ctx, domain.NewDuplicateDismissal(merged.ID, survivor.ID, officeAcc.ID, now)),
			tx.Classes().AddMember(ctx, domain.ClassMember{ClassID: classes[0].ID, PersonID: merged.ID}),
			tx.Classes().AddMember(ctx, domain.ClassMember{ClassID: classes[1].ID, PersonID: merged.ID}),
			tx.Classes().AddMember(ctx, domain.ClassMember{ClassID: classes[1].ID, PersonID: survivor.ID}),
		)

		for _, b := range bookings {
			errs = append(errs, tx.Bookings().Create(ctx, b))
		}

		for _, e := range entries {
			errs = append(errs, tx.Waitlist().Create(ctx, e))
		}

		for _, p := range passes {
			errs = append(errs, tx.Passes().Create(ctx, p))
		}

		// The merged person's pass paid for its three bookings.
		for _, b := range []domain.Booking{bookings[1], bookings[3], bookings[4]} {
			errs = append(errs, tx.Redemptions().Create(ctx, domain.PassRedemption{
				PassID: mergedPass.ID, BookingID: b.ID, At: b.BookedAt,
			}))
		}

		return errors.Join(errs...)
	})
	if err != nil {
		t.Fatal(err)
	}

	return w
}

// office is the principal of the office account.
func (w *mergeFaultWorld) office(st store.Store) service.Principal {
	w.t.Helper()

	var acc domain.Account

	err := st.InTx(w.t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		var err error
		acc, err = tx.Accounts().GetByEmail(ctx, "olga.office@example.org")

		return err
	})
	if err != nil {
		w.t.Fatal(err)
	}

	return service.Principal{AccountID: acc.ID, PersonID: acc.PersonID, Email: acc.Email, Role: acc.Role}
}

// TestMergeStoreFaults: a store failure anywhere in a merge is returned and
// the merge leaves nothing behind: no moved booking, entry, membership,
// pass, link, staff link, account or dismissal, no tombstone and no audit
// event.
func TestMergeStoreFaults(t *testing.T) {
	t.Parallel()

	sweepMerge := func(apply bool) func(t *testing.T) {
		return func(t *testing.T) {
			t.Parallel()

			service.SweepFaults(t, service.FaultSweep{Setup: func(t *testing.T) (*service.FaultStore, func() error) {
				w := newMergeFaultWorld(t)
				office := w.office(w.mem)
				fs := service.NewFaultStore(w.mem)
				svc := service.NewPersonService(fs, clock.NewFixed(mergeFaultNow), w.seq, service.Mails{})

				return fs, func() error {
					sum, err := svc.Merge(t.Context(), office, service.MergeRequest{
						SurvivorID: w.survivor, MergedID: w.merged, Take: service.MergeTake{MemberNo: true},
					}, apply)
					if err == nil && (sum.WaitlistWithdrawn != 3 || sum.WaitlistMoved != 1 || sum.History != 1 ||
						len(sum.Canceled) != 1 || sum.LinksDropped != 1 || sum.LinksMoved != 1 || sum.StaffMoved != 1 || !sum.AccountMoved) {
						t.Fatalf("summary %+v", sum)
					}

					return err
				}
			}})
		}
	}

	t.Run("apply", sweepMerge(true))
	t.Run("preview", sweepMerge(false))
}

// TestMergeNeedsAnActor: the merge is audited with the account that ran
// it; a principal without an account (nobody signed in) fails the audit
// event's validation, and the merge is rolled back as a whole.
func TestMergeNeedsAnActor(t *testing.T) {
	t.Parallel()

	w := newMergeFaultWorld(t)
	fs := service.NewFaultStore(w.mem)
	before := fs.Snapshot(t)
	svc := service.NewPersonService(fs, clock.NewFixed(mergeFaultNow), w.seq, service.Mails{})

	_, err := svc.Merge(t.Context(), service.Principal{}, service.MergeRequest{SurvivorID: w.survivor, MergedID: w.merged}, true)
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("merge without an actor: %v, want a validation error", err)
	}

	if changed := before.Changed(fs.Snapshot(t)); len(changed) > 0 {
		t.Errorf("the failed merge changed %v", changed)
	}
}

// TestMergeRefusesInvalidStoredData: the merge never writes a person that
// fails validation. A survivor whose stored data does not pass today's
// rules (an email the stores accept, kept from before the rule) refuses the
// merge on that field, and nothing is written; the office corrects the
// person first.
func TestMergeRefusesInvalidStoredData(t *testing.T) {
	t.Parallel()

	w := newMergeFaultWorld(t)

	err := w.mem.InTx(t.Context(), store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		p, err := tx.Persons().Get(ctx, w.survivor)
		if err != nil {
			return err
		}

		p.Email = "sara at example.org"

		return tx.Persons().Update(ctx, p)
	})
	if err != nil {
		t.Fatal(err)
	}

	fs := service.NewFaultStore(w.mem)
	before := fs.Snapshot(t)
	svc := service.NewPersonService(fs, clock.NewFixed(mergeFaultNow), w.seq, service.Mails{})

	_, err = svc.Merge(t.Context(), w.office(w.mem), service.MergeRequest{SurvivorID: w.survivor, MergedID: w.merged}, true)
	wantField(t, err, "email")

	if changed := before.Changed(fs.Snapshot(t)); len(changed) > 0 {
		t.Errorf("the refused merge changed %v", changed)
	}
}
