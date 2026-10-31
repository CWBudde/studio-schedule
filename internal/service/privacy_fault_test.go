package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// privacyFaults is Paula with everything anonymizing removes and an export
// lists, on the class fixture at bookingNow: a guardian (Greta), a
// dependent (Kai) who has another guardian (Piet), an account with a
// session, an admin's running impersonation of it, a calendar feed, a push
// subscription and a dismissed announcement, a class membership, a staff
// record, a duplicate dismissal, a past and a future booking paid by her
// pass, a waiting-list entry, and a receipt with its cancellation. Olga is
// the office, Ada and Abe are admins.
type privacyFaults struct {
	*genFixture

	now                           time.Time
	paula, greta, kai, piet, olga domain.Person
	paulaAcc, adaAcc, abeAcc      domain.Account
	office, admin                 Principal
	future, past, waited          domain.Lesson
	futureBooking, pastBooking    domain.Booking
	pass                          domain.Pass
	shown, dismissed              domain.Announcement
	receipt, cancellation, unpaid domain.Receipt
	impersonation, ownSession     domain.Session
	staff                         domain.Staff
	entry                         domain.WaitlistEntry
	passType                      domain.PassTypeID
	classID                       domain.ClassID
	receiptSeq                    int
	accounts                      map[domain.PersonID]domain.Account
}

func newPrivacyFaults(t *testing.T) *privacyFaults {
	t.Helper()

	settings := bookingSettings()
	settings.RedemptionMode = domain.RedeemOnBooking

	f := &privacyFaults{genFixture: newGenFixture(t, settings), now: mustInstant(t, bookingNow)}
	f.accounts = map[domain.PersonID]domain.Account{}

	f.paula = f.personWith(t, "Paula", "paula@example.org", domain.RoleMember)
	f.greta = f.personWith(t, "Greta", "", "")
	f.kai = f.personWith(t, "Kai", "", "")
	f.piet = f.personWith(t, "Piet", "", "")
	f.olga = f.personWith(t, "Olga", "olga@example.net", domain.RoleOffice)
	ada := f.personWith(t, "Ada", "ada@example.net", domain.RoleAdmin)
	abe := f.personWith(t, "Abe", "abe@example.net", domain.RoleAdmin)
	f.paulaAcc, f.adaAcc, f.abeAcc = f.accounts[f.paula.ID], f.accounts[ada.ID], f.accounts[abe.ID]
	f.office = Principal{AccountID: f.accounts[f.olga.ID].ID, PersonID: f.olga.ID, Role: domain.RoleOffice}
	f.admin = Principal{AccountID: f.adaAcc.ID, PersonID: ada.ID, Role: domain.RoleAdmin}

	f.classID = f.addClass(t, time.Thursday, domain.TimeOfDay{Hour: 17}).ID
	f.future = f.lessonAt(t, f.now.Add(30*time.Hour))
	f.waited = f.lessonAt(t, f.now.Add(54*time.Hour))
	f.past = f.lessonAt(t, f.now.Add(-48*time.Hour))
	f.passType = ids.New[domain.PassTypeID](f.seq)

	f.ownSession = f.session(f.paulaAcc.ID, domain.AccountID{}, "paula")
	f.impersonation = f.session(f.paulaAcc.ID, f.adaAcc.ID, "ada as paula")
	f.staff = domain.Staff{
		ID: ids.New[domain.StaffID](f.seq), PersonID: f.paula.ID, DisplayName: "Paula P.",
		Kind: domain.StaffInstructor, Active: true,
	}
	f.futureBooking = f.booking(t, f.future, domain.BookingBooked, f.now.Add(-24*time.Hour))
	f.pastBooking = f.booking(t, f.past, domain.BookingAttended, f.now.Add(-72*time.Hour))
	f.pass = domain.Pass{
		ID: ids.New[domain.PassID](f.seq), PassTypeID: f.passType, PersonID: f.paula.ID, Kind: domain.PassVisits,
		VisitsTotal: 10, VisitsRemaining: 8, ValidFrom: mustDate(t, "2026-10-01"), Status: domain.PassActive,
		Origin: domain.PassAssigned, SoldAt: f.now.Add(-96 * time.Hour),
	}
	f.entry = domain.WaitlistEntry{
		ID: ids.New[domain.WaitlistEntryID](f.seq), LessonID: f.waited.ID, PersonID: f.paula.ID,
		Status: domain.WaitlistWaiting, JoinedAt: f.now.Add(-time.Hour),
	}
	f.shown = f.announcement(t, "Studio closed on Friday")
	f.dismissed = f.announcement(t, "New timetable")
	f.receipt = f.newReceipt(t, f.paula, domain.ReceiptID{})
	f.cancellation = f.newReceipt(t, f.paula, f.receipt.ID)
	f.unpaid = f.newReceipt(t, f.piet, domain.ReceiptID{})

	f.write(t, func(ctx context.Context, tx store.Tx) error {
		return errors.Join(
			tx.Persons().AddGuardian(ctx, domain.Guardian{GuardianID: f.greta.ID, DependentID: f.paula.ID}),
			tx.Persons().AddGuardian(ctx, domain.Guardian{GuardianID: f.paula.ID, DependentID: f.kai.ID}),
			tx.Persons().AddGuardian(ctx, domain.Guardian{GuardianID: f.piet.ID, DependentID: f.kai.ID}),
			tx.Classes().AddMember(ctx, domain.ClassMember{ClassID: f.classID, PersonID: f.paula.ID}),
			tx.Staff().Create(ctx, f.staff),
			tx.DuplicateDismissals().Create(ctx,
				domain.NewDuplicateDismissal(f.paula.ID, f.piet.ID, f.office.AccountID, f.now.Add(-time.Hour))),
			tx.Sessions().Create(ctx, f.ownSession),
			tx.Sessions().Create(ctx, f.impersonation),
			tx.CalendarFeeds().Create(ctx, domain.CalendarFeed{
				ID: ids.New[domain.CalendarFeedID](f.seq), AccountID: f.paulaAcc.ID, Scope: domain.FeedBookings,
				CreatedAt: f.now.Add(-time.Hour), TokenHash: domain.HashToken("paula's feed"),
			}),
			tx.PushSubscriptions().Save(ctx, domain.PushSubscription{
				ID: ids.New[domain.PushSubscriptionID](f.seq), AccountID: f.paulaAcc.ID,
				Endpoint: "https://push.example.org/paula", P256DH: [65]byte{4}, Auth: [16]byte{1},
				CreatedAt: f.now.Add(-time.Hour), LastUsedAt: f.now.Add(-time.Hour),
			}),
			tx.PassTypes().Create(ctx, domain.PassType{
				ID: f.passType, Name: "10-visit pass", Kind: domain.PassVisits, Visits: 10, Active: true,
				CreatedAt: f.now.Add(-100 * time.Hour),
			}),
			tx.Passes().Create(ctx, f.pass),
			tx.Redemptions().Create(ctx, domain.PassRedemption{
				PassID: f.pass.ID, BookingID: f.futureBooking.ID, At: f.futureBooking.BookedAt,
			}),
			tx.Redemptions().Create(ctx, domain.PassRedemption{
				PassID: f.pass.ID, BookingID: f.pastBooking.ID, At: f.pastBooking.BookedAt,
			}),
			tx.Waitlist().Create(ctx, f.entry),
			tx.Announcements().Dismiss(ctx, domain.AnnouncementDismissal{
				AnnouncementID: f.dismissed.ID, AccountID: f.paulaAcc.ID, DismissedAt: f.now.Add(-time.Minute),
			}),
		)
	})

	return f
}

// personWith stores a person, and an account with role if email is set.
func (f *privacyFaults) personWith(t *testing.T, given, email string, role domain.Role) domain.Person {
	t.Helper()

	p := domain.Person{ID: ids.New[domain.PersonID](f.seq), GivenName: given, FamilyName: "Private", Email: email}
	acc := domain.Account{
		ID: ids.New[domain.AccountID](f.seq), PersonID: p.ID, Email: email, Role: role,
		ConfirmedAt: f.now.Add(-30 * 24 * time.Hour),
	}

	f.write(t, func(ctx context.Context, tx store.Tx) error {
		if err := tx.Persons().Create(ctx, p); err != nil || email == "" {
			return err
		}

		f.accounts[p.ID] = acc

		return tx.Accounts().Create(ctx, acc)
	})

	return p
}

// lessonAt stores a public regular one-off lesson of an hour from start.
func (f *privacyFaults) lessonAt(t *testing.T, start time.Time) domain.Lesson {
	t.Helper()

	l := domain.Lesson{
		ID: ids.New[domain.LessonID](f.seq), CourseTypeID: f.courseType, RoomID: f.room, InstructorID: f.instructor,
		StartsAt: start, EndsAt: start.Add(time.Hour), Status: domain.LessonScheduled,
		Visibility: domain.VisibilityPublic, Kind: domain.LessonRegular, Capacity: 5,
	}
	f.write(t, func(ctx context.Context, tx store.Tx) error { return tx.Lessons().Create(ctx, l) })

	return l
}

func (f *privacyFaults) session(account, impersonator domain.AccountID, token string) domain.Session {
	return domain.Session{
		ID: ids.New[domain.SessionID](f.seq), AccountID: account, TokenHash: domain.HashToken(token),
		CreatedAt: f.now.Add(-time.Minute), LastSeenAt: f.now.Add(-time.Minute), ExpiresAt: f.now.Add(time.Hour),
		ImpersonatorID: impersonator,
	}
}

func (f *privacyFaults) booking(t *testing.T, l domain.Lesson, status domain.BookingStatus, at time.Time) domain.Booking {
	t.Helper()

	b := domain.Booking{
		ID: ids.New[domain.BookingID](f.seq), LessonID: l.ID, PersonID: f.paula.ID, Status: status,
		Source: domain.BookingSourceSelf, BookedAt: at,
	}
	f.write(t, func(ctx context.Context, tx store.Tx) error { return tx.Bookings().Create(ctx, b) })

	return b
}

func (f *privacyFaults) announcement(t *testing.T, title string) domain.Announcement {
	t.Helper()

	a := domain.Announcement{
		ID: ids.New[domain.AnnouncementID](f.seq), AuthorID: f.office.AccountID, Title: title, Body: "Details follow.",
		Audience: domain.AudienceEveryone, PublishAt: f.now.Add(-time.Hour), CreatedAt: f.now.Add(-time.Hour),
		UpdatedAt: f.now.Add(-time.Hour),
	}
	f.write(t, func(ctx context.Context, tx store.Tx) error { return tx.Announcements().Create(ctx, a) })

	return a
}

// newReceipt stores a sale receipt paid by payer, or the cancellation of
// cancels.
func (f *privacyFaults) newReceipt(t *testing.T, payer domain.Person, cancels domain.ReceiptID) domain.Receipt {
	t.Helper()

	f.receiptSeq++
	line := domain.NewReceiptLine(1, "10-visit pass", 1, 12000, 1900, domain.TaxStandard)
	r := domain.Receipt{
		ID: ids.New[domain.ReceiptID](f.seq), Number: domain.ReceiptNumber("R", 2026, f.receiptSeq), Year: 2026,
		Sequence: f.receiptSeq, Kind: domain.ReceiptSale, IssuedAt: f.now.Add(-48 * time.Hour),
		IssueDate: mustDate(t, "2026-10-19"), Locale: "en",
		Issuer:  domain.ReceiptIssuer{Name: "Studio Example", Address: "Example Street 1", TaxID: "00/000/00000"},
		TaxMode: domain.TaxStandard, PayerID: payer.ID, RecipientName: payer.GivenName + " Private",
		PaymentMethod: domain.PaymentCash, Currency: "EUR", Net: line.Net, VAT: line.VAT, Gross: line.Gross,
		CreatedBy: f.office.AccountID, Lines: []domain.ReceiptLine{line},
	}

	if !cancels.IsZero() {
		line = line.Negated()
		r.Kind, r.CancelsReceiptID, r.Lines = domain.ReceiptCancellation, cancels, []domain.ReceiptLine{line}
		r.Net, r.VAT, r.Gross = line.Net, line.VAT, line.Gross
	}

	f.write(t, func(ctx context.Context, tx store.Tx) error { return tx.Receipts().Create(ctx, r) })

	return r
}

// persons returns a PersonService on st at the fixture's time.
func (f *privacyFaults) persons(t *testing.T, st store.Store) *PersonService {
	t.Helper()

	return NewPersonService(st, clock.NewFixed(f.now), f.seq, testMails(newTestRenderer(t)))
}
