// Package servicetest holds service scenarios that run on every store: the
// memory store's tests in package service and the PostgreSQL store's tests
// call them with a fresh store each (ADR 0015: the merge and the import are
// verified on both). Fixtures are fictional.
package servicetest

import (
	"context"
	"errors"
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

// now is the scenarios' clock: Wednesday 21 October 2026, noon in Berlin.
var now = time.Date(2026, time.October, 21, 10, 0, 0, 0, time.UTC)

// world is a small school: settings that redeem on booking, a room, a
// course type, an instructor, a term with two classes, an office account,
// and three lessons in the next days.
type world struct {
	t   *testing.T
	st  store.Store
	seq *ids.Sequence

	settings   domain.Settings
	room       domain.RoomID
	courseType domain.CourseTypeID
	instructor domain.StaffID
	term       domain.TermID
	classes    [2]domain.ClassID
	lessons    [3]domain.Lesson
	passType   domain.PassTypeID
	office     service.Principal
}

func newWorld(t *testing.T, st store.Store) *world {
	t.Helper()

	w := &world{t: t, st: st, seq: ids.NewSequence()}
	w.settings = seed.DemoSettings()
	w.settings.RedemptionMode = domain.RedeemOnBooking

	location := domain.Location{ID: ids.New[domain.LocationID](w.seq), Name: "Studio Nowhere"}
	room := domain.Room{ID: ids.New[domain.RoomID](w.seq), LocationID: location.ID, Name: "Room One", Capacity: 12, Position: 1}
	courseType := domain.CourseType{ID: ids.New[domain.CourseTypeID](w.seq), Name: "Imaginary Ballet"}
	staff := domain.Staff{
		ID: ids.New[domain.StaffID](w.seq), DisplayName: "Ada Example", Kind: domain.StaffInstructor, Active: true,
	}
	term := domain.Term{
		ID: ids.New[domain.TermID](w.seq), Name: "Autumn", StartsOn: domain.Date{Year: 2026, Month: time.September, Day: 1},
		EndsOn: domain.Date{Year: 2027, Month: time.January, Day: 31},
	}
	passType := domain.PassType{
		ID: ids.New[domain.PassTypeID](w.seq), Name: "10-visit pass", Kind: domain.PassVisits, Visits: 10,
		Active: true, CreatedAt: now,
	}

	w.room, w.courseType, w.instructor, w.term, w.passType = room.ID, courseType.ID, staff.ID, term.ID, passType.ID

	var classes []domain.Class

	for i := range w.classes {
		c := domain.Class{
			ID: ids.New[domain.ClassID](w.seq), TermID: term.ID, CourseTypeID: courseType.ID, RoomID: room.ID,
			InstructorID: staff.ID, AllLevels: true, Weekday: time.Weekday(i + 1),
			StartTime: domain.TimeOfDay{Hour: 17}, Duration: time.Hour, Capacity: 10,
		}
		w.classes[i] = c.ID
		classes = append(classes, c)
	}

	for i := range w.lessons {
		start := now.Add(time.Duration(24*(i+1)) * time.Hour)
		w.lessons[i] = domain.Lesson{
			ID: ids.New[domain.LessonID](w.seq), CourseTypeID: courseType.ID, RoomID: room.ID, InstructorID: staff.ID,
			StartsAt: start, EndsAt: start.Add(time.Hour), Status: domain.LessonScheduled,
			Visibility: domain.VisibilityPublic, Kind: domain.LessonRegular, Capacity: 5,
		}
	}

	w.write(func(ctx context.Context, tx store.Tx) error {
		errs := []error{
			tx.Settings().Put(ctx, w.settings),
			tx.Locations().Create(ctx, location),
			tx.Rooms().Create(ctx, room),
			tx.CourseTypes().Create(ctx, courseType),
			tx.Staff().Create(ctx, staff),
			tx.Terms().Create(ctx, term),
			tx.PassTypes().Create(ctx, passType),
		}
		for _, c := range classes {
			errs = append(errs, tx.Classes().Create(ctx, c))
		}

		for _, l := range w.lessons {
			errs = append(errs, tx.Lessons().Create(ctx, l))
		}

		return errors.Join(errs...)
	})

	office := w.person("Olga", "Office", "olga.office@example.net")
	acc := w.account(office, "olga.office@example.net")
	acc.Role = domain.RoleOffice
	w.write(func(ctx context.Context, tx store.Tx) error { return tx.Accounts().Update(ctx, acc) })
	w.office = service.Principal{AccountID: acc.ID, PersonID: office.ID, Email: acc.Email, Role: domain.RoleOffice}

	return w
}

func (w *world) write(fn func(ctx context.Context, tx store.Tx) error) {
	w.t.Helper()

	if err := w.st.InTx(w.t.Context(), store.ReadWrite, fn); err != nil {
		w.t.Fatal(err)
	}
}

// read returns what fn reads in a read-only transaction.
func read[T any](w *world, fn func(ctx context.Context, tx store.Tx) (T, error)) T {
	w.t.Helper()

	var v T

	err := w.st.InTx(w.t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		var err error
		v, err = fn(ctx, tx)

		return err
	})
	if err != nil {
		w.t.Fatal(err)
	}

	return v
}

func (w *world) person(given, family, email string) domain.Person {
	w.t.Helper()

	p := domain.Person{ID: ids.New[domain.PersonID](w.seq), GivenName: given, FamilyName: family, Email: email}
	w.write(func(ctx context.Context, tx store.Tx) error { return tx.Persons().Create(ctx, p) })

	return p
}

func (w *world) update(p domain.Person) domain.Person {
	w.t.Helper()

	w.write(func(ctx context.Context, tx store.Tx) error { return tx.Persons().Update(ctx, p) })

	return p
}

func (w *world) get(id domain.PersonID) domain.Person {
	w.t.Helper()

	return read(w, func(ctx context.Context, tx store.Tx) (domain.Person, error) { return tx.Persons().Get(ctx, id) })
}

func (w *world) account(p domain.Person, email string) domain.Account {
	w.t.Helper()

	a := domain.Account{
		ID: ids.New[domain.AccountID](w.seq), PersonID: p.ID, Email: email, Role: domain.RoleMember, ConfirmedAt: now,
	}
	w.write(func(ctx context.Context, tx store.Tx) error { return tx.Accounts().Create(ctx, a) })

	return a
}

func (w *world) link(guardian, dependent domain.Person) {
	w.t.Helper()

	w.write(func(ctx context.Context, tx store.Tx) error {
		return tx.Persons().AddGuardian(ctx, domain.Guardian{GuardianID: guardian.ID, DependentID: dependent.ID})
	})
}

func (w *world) member(c domain.ClassID, p domain.Person) {
	w.t.Helper()

	w.write(func(ctx context.Context, tx store.Tx) error {
		return tx.Classes().AddMember(ctx, domain.ClassMember{ClassID: c, PersonID: p.ID})
	})
}

// booking stores a booking of p in l, booked a day ago; canceled with a
// zero CanceledAt cancels it then.
func (w *world) booking(l domain.Lesson, p domain.Person, canceled bool) domain.Booking {
	w.t.Helper()

	b := domain.Booking{
		ID: ids.New[domain.BookingID](w.seq), LessonID: l.ID, PersonID: p.ID, Status: domain.BookingBooked,
		Source: domain.BookingSourceSelf, BookedAt: now.Add(-24 * time.Hour),
	}
	if canceled {
		b.Status, b.CanceledAt, b.Released = domain.BookingCanceled, now.Add(-time.Hour), true
	}

	w.write(func(ctx context.Context, tx store.Tx) error { return tx.Bookings().Create(ctx, b) })

	return b
}

// pass stores a visit pass of owner with visits left of 10, sold to payer
// if set.
func (w *world) pass(owner, payer domain.Person, left int) domain.Pass {
	w.t.Helper()

	p := domain.Pass{
		ID: ids.New[domain.PassID](w.seq), PassTypeID: w.passType, PersonID: owner.ID, Kind: domain.PassVisits,
		VisitsTotal: 10, VisitsRemaining: left, ValidFrom: domain.Date{Year: 2026, Month: time.October, Day: 1},
		Status: domain.PassActive, Origin: domain.PassAssigned, SoldAt: now.Add(-48 * time.Hour),
	}
	if !payer.ID.IsZero() {
		p.Origin, p.PayerID, p.PricePaid = domain.PassSold, payer.ID, 12000
	}

	w.write(func(ctx context.Context, tx store.Tx) error { return tx.Passes().Create(ctx, p) })

	return p
}

func (w *world) redeem(p domain.Pass, b domain.Booking) {
	w.t.Helper()

	w.write(func(ctx context.Context, tx store.Tx) error {
		return tx.Redemptions().Create(ctx, domain.PassRedemption{PassID: p.ID, BookingID: b.ID, At: b.BookedAt})
	})
}

// receipt stores a receipt paid by payer.
func (w *world) receipt(payer domain.Person) domain.Receipt {
	w.t.Helper()

	var r domain.Receipt

	w.write(func(ctx context.Context, tx store.Tx) error {
		seq, err := tx.ReceiptCounters().Next(ctx, 2026)
		if err != nil {
			return err
		}

		line := domain.NewReceiptLine(1, "10-visit pass", 1, 12000, 1900, domain.TaxStandard)
		r = domain.Receipt{
			ID: ids.New[domain.ReceiptID](w.seq), Number: domain.ReceiptNumber("R", 2026, seq), Year: 2026,
			Sequence: seq, Kind: domain.ReceiptSale, IssuedAt: now.Add(-48 * time.Hour),
			IssueDate: domain.Date{Year: 2026, Month: time.October, Day: 19}, Locale: "en",
			Issuer: domain.ReceiptIssuer{
				Name: "Studio Example", Address: "Example Street 1", TaxID: "00/000/00000", Email: "office@example.net",
			},
			TaxMode: domain.TaxStandard, PayerID: payer.ID, RecipientName: payer.GivenName + " " + payer.FamilyName,
			PaymentMethod: domain.PaymentCash, Currency: "EUR", Net: line.Net, VAT: line.VAT, Gross: line.Gross,
			CreatedBy: w.office.AccountID, Lines: []domain.ReceiptLine{line},
		}

		return tx.Receipts().Create(ctx, r)
	})

	return r
}

func (w *world) persons() *service.PersonService {
	return service.NewPersonService(w.st, clock.NewFixed(now), w.seq,
		service.Mails{Renderer: w.renderer(), AppURL: "https://school.example.org/"})
}

func (w *world) renderer() *mail.Renderer {
	w.t.Helper()

	r, err := mail.NewRenderer()
	if err != nil {
		w.t.Fatal(err)
	}

	return r
}

func (w *world) imports() *service.ImportService {
	return service.NewImportService(w.st, clock.NewFixed(now), w.seq)
}
