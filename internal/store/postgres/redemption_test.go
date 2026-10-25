package postgres

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/mail"
	"github.com/cwbudde/studio-schedule/internal/seed"
	"github.com/cwbudde/studio-schedule/internal/service"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// slowPasses widens the window between reading a person's passes and using
// one, so that concurrent redemptions overlap there: without the lock they
// would all use the same last visit.
type slowPasses struct{ store.PassRepo }

const passReadDelay = 20 * time.Millisecond

func (r slowPasses) LockByPersons(ctx context.Context, persons ...domain.PersonID) ([]domain.Pass, error) {
	passes, err := r.PassRepo.LockByPersons(ctx, persons...)
	time.Sleep(passReadDelay)

	return passes, err
}

func (r slowPasses) ListByPersons(ctx context.Context, persons []domain.PersonID) ([]domain.Pass, error) {
	passes, err := r.PassRepo.ListByPersons(ctx, persons)
	time.Sleep(passReadDelay)

	return passes, err
}

type slowPassTx struct{ store.Tx }

func (t slowPassTx) Passes() store.PassRepo { return slowPasses{t.Tx.Passes()} }

type slowPassStore struct{ store.Store }

func (s slowPassStore) InTx(ctx context.Context, mode store.TxMode, fn func(context.Context, store.Tx) error) error {
	return s.Store.InTx(ctx, mode, func(ctx context.Context, tx store.Tx) error { return fn(ctx, slowPassTx{tx}) })
}

// TestRedeemLastVisitConcurrently books many lessons for a member with one
// visit left from many goroutines through the booking service, which
// redeems on booking: under PostgreSQL's row locks exactly one booking uses
// the visit (ADR 0012). The memory store's variant is in package service.
func TestRedeemLastVisitConcurrently(t *testing.T) {
	t.Parallel()

	const n = 8

	ctx := t.Context()
	st := newTestStore(t)
	seq := ids.NewSequence()
	now := time.Date(2026, time.October, 21, 10, 0, 0, 0, time.UTC)

	settings := seed.DemoSettings()
	settings.RedemptionMode = domain.RedeemOnBooking

	person := domain.Person{ID: ids.New[domain.PersonID](seq), GivenName: "Mia", FamilyName: "Example"}
	account := domain.Account{
		ID: ids.New[domain.AccountID](seq), PersonID: person.ID, Email: "mia@example.org", Role: domain.RoleMember,
		ConfirmedAt: now,
	}
	location := domain.Location{ID: ids.New[domain.LocationID](seq), Name: "Studio Nowhere"}
	room := domain.Room{ID: ids.New[domain.RoomID](seq), LocationID: location.ID, Name: "Room One", Capacity: 12}
	courseType := domain.CourseType{ID: ids.New[domain.CourseTypeID](seq), Name: "Imaginary Ballet"}
	staff := domain.Staff{
		ID: ids.New[domain.StaffID](seq), DisplayName: "Ada Example", Kind: domain.StaffInstructor, Active: true,
	}
	passType := domain.PassType{
		ID: ids.New[domain.PassTypeID](seq), Name: "Single visit", Kind: domain.PassVisits, Visits: 1, Active: true,
		CreatedAt: now,
	}
	pass := domain.Pass{
		ID: ids.New[domain.PassID](seq), PassTypeID: passType.ID, PersonID: person.ID, Kind: domain.PassVisits,
		VisitsTotal: 1, VisitsRemaining: 1, ValidFrom: domain.Date{Year: 2026, Month: time.October, Day: 1},
		Status: domain.PassActive, Origin: domain.PassAssigned, SoldAt: now,
	}

	lessons := make([]domain.Lesson, n)
	for i := range lessons {
		start := time.Date(2026, time.October, 22, 8+i, 0, 0, 0, time.UTC)
		lessons[i] = domain.Lesson{
			ID: ids.New[domain.LessonID](seq), CourseTypeID: courseType.ID, RoomID: room.ID, InstructorID: staff.ID,
			StartsAt: start, EndsAt: start.Add(time.Hour), Status: domain.LessonScheduled,
			Visibility: domain.VisibilityPublic, Kind: domain.LessonRegular, Capacity: 5,
		}
	}

	err := st.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		errs := []error{
			tx.Settings().Put(ctx, settings),
			tx.Persons().Create(ctx, person),
			tx.Accounts().Create(ctx, account),
			tx.Locations().Create(ctx, location),
			tx.Rooms().Create(ctx, room),
			tx.CourseTypes().Create(ctx, courseType),
			tx.Staff().Create(ctx, staff),
			tx.PassTypes().Create(ctx, passType),
			tx.Passes().Create(ctx, pass),
		}
		for _, l := range lessons {
			errs = append(errs, tx.Lessons().Create(ctx, l))
		}

		return errors.Join(errs...)
	})
	if err != nil {
		t.Fatal(err)
	}

	renderer, err := mail.NewRenderer()
	if err != nil {
		t.Fatal(err)
	}

	svc := service.NewBookingService(slowPassStore{st}, clock.NewFixed(now), ids.V7{},
		service.Mails{Renderer: renderer, AppURL: "https://school.example.org/"})
	p := service.Principal{AccountID: account.ID, PersonID: person.ID, Email: account.Email, Role: domain.RoleMember}

	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		bookings []domain.BookingID
	)

	for _, l := range lessons {
		wg.Go(func() {
			b, err := svc.Book(ctx, p, l.ID, person.ID)
			if err != nil {
				t.Error(err)

				return
			}

			mu.Lock()
			defer mu.Unlock()

			bookings = append(bookings, b.ID)
		})
	}

	wg.Wait()

	var (
		redemptions []domain.PassRedemption
		left        domain.Pass
	)

	err = st.InTx(ctx, store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		var err error
		if redemptions, err = tx.Redemptions().ListByBookings(ctx, bookings); err != nil {
			return err
		}

		left, err = tx.Passes().Get(ctx, pass.ID)

		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(bookings) != n || len(redemptions) != 1 || left.VisitsRemaining != 0 {
		t.Errorf("%d bookings, %d redemptions, %d visits left; want %d, 1 and 0",
			len(bookings), len(redemptions), left.VisitsRemaining, n)
	}
}
