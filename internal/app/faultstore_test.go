package app_test

import (
	"context"
	"errors"
	"sync"

	"github.com/cwbudde/studio-schedule/internal/store"
)

// faultMessage is the text of errFault; no response may show it.
const faultMessage = "fault: injected store failure"

// errFault is the error a poisoned transaction fails with. It wraps no
// domain error, so the API can only report it as a server error.
var errFault = errors.New(faultMessage)

// faultSessions is the repository the session middleware reads on every
// signed-in request (AuthService.Authenticate).
const faultSessions = "Sessions"

// faultStore wraps a store and makes the transactions fail that touch a
// poisoned repository, named like its Tx accessor ("Settings",
// "Persons", ...). Such a transaction runs to its end and then fails with
// errFault, so the wrapped store discards its changes; one whose callback
// fails on its own keeps that error.
//
// So that signed-in requests still authenticate, a transaction that
// touched Sessions is exempt unless Sessions itself is poisoned. The first
// skip transactions that would fail pass, to reach a later transaction of
// the same request (e.g. logout after the authentication).
type faultStore struct {
	store.Store

	mu       sync.Mutex
	poisoned map[string]bool
	skip     int
}

// poison makes the transactions that touch one of repos fail, after the
// first skip of them.
func (s *faultStore) poison(skip int, repos ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.poisoned, s.skip = make(map[string]bool, len(repos)), skip
	for _, r := range repos {
		s.poisoned[r] = true
	}
}

// fails reports whether a transaction that touched the repositories
// touched fails, and counts it against skip.
func (s *faultStore) fails(touched map[string]bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if touched[faultSessions] && !s.poisoned[faultSessions] {
		return false
	}

	for r := range touched {
		if !s.poisoned[r] {
			continue
		}

		if s.skip > 0 {
			s.skip--

			return false
		}

		return true
	}

	return false
}

// InTx implements store.Store.
func (s *faultStore) InTx(ctx context.Context, mode store.TxMode, fn func(ctx context.Context, tx store.Tx) error) error {
	return s.Store.InTx(ctx, mode, func(ctx context.Context, tx store.Tx) error {
		ftx := &faultTx{Tx: tx, touched: map[string]bool{}}
		if err := fn(ctx, ftx); err != nil {
			return err
		}

		if s.fails(ftx.touched) {
			return errFault
		}

		return nil
	})
}

// faultTx records the repositories a transaction touches.
type faultTx struct {
	store.Tx

	touched map[string]bool
}

func (t *faultTx) touch(repo string) { t.touched[repo] = true }

func (t *faultTx) Settings() store.SettingsRepo {
	t.touch("Settings")

	return t.Tx.Settings()
}

func (t *faultTx) Persons() store.PersonRepo {
	t.touch("Persons")

	return t.Tx.Persons()
}

func (t *faultTx) Accounts() store.AccountRepo {
	t.touch("Accounts")

	return t.Tx.Accounts()
}

func (t *faultTx) Credentials() store.CredentialRepo {
	t.touch("Credentials")

	return t.Tx.Credentials()
}

func (t *faultTx) Sessions() store.SessionRepo {
	t.touch(faultSessions)

	return t.Tx.Sessions()
}

func (t *faultTx) AccountTokens() store.AccountTokenRepo {
	t.touch("AccountTokens")

	return t.Tx.AccountTokens()
}

func (t *faultTx) Locations() store.LocationRepo {
	t.touch("Locations")

	return t.Tx.Locations()
}

func (t *faultTx) Rooms() store.RoomRepo {
	t.touch("Rooms")

	return t.Tx.Rooms()
}

func (t *faultTx) Staff() store.StaffRepo {
	t.touch("Staff")

	return t.Tx.Staff()
}

func (t *faultTx) CourseTypes() store.CourseTypeRepo {
	t.touch("CourseTypes")

	return t.Tx.CourseTypes()
}

func (t *faultTx) Terms() store.TermRepo {
	t.touch("Terms")

	return t.Tx.Terms()
}

func (t *faultTx) Classes() store.ClassRepo {
	t.touch("Classes")

	return t.Tx.Classes()
}

func (t *faultTx) Lessons() store.LessonRepo {
	t.touch("Lessons")

	return t.Tx.Lessons()
}

func (t *faultTx) Bookings() store.BookingRepo {
	t.touch("Bookings")

	return t.Tx.Bookings()
}

func (t *faultTx) Waitlist() store.WaitlistRepo {
	t.touch("Waitlist")

	return t.Tx.Waitlist()
}

func (t *faultTx) Notices() store.LessonNoticeRepo {
	t.touch("Notices")

	return t.Tx.Notices()
}

func (t *faultTx) CalendarFeeds() store.CalendarFeedRepo {
	t.touch("CalendarFeeds")

	return t.Tx.CalendarFeeds()
}

func (t *faultTx) PassTypes() store.PassTypeRepo {
	t.touch("PassTypes")

	return t.Tx.PassTypes()
}

func (t *faultTx) Passes() store.PassRepo {
	t.touch("Passes")

	return t.Tx.Passes()
}

func (t *faultTx) Redemptions() store.PassRedemptionRepo {
	t.touch("Redemptions")

	return t.Tx.Redemptions()
}

func (t *faultTx) Receipts() store.ReceiptRepo {
	t.touch("Receipts")

	return t.Tx.Receipts()
}

func (t *faultTx) ReceiptCounters() store.ReceiptCounterRepo {
	t.touch("ReceiptCounters")

	return t.Tx.ReceiptCounters()
}

func (t *faultTx) Outbox() store.OutboxRepo {
	t.touch("Outbox")

	return t.Tx.Outbox()
}

func (t *faultTx) Audit() store.AuditRepo {
	t.touch("Audit")

	return t.Tx.Audit()
}

func (t *faultTx) Announcements() store.AnnouncementRepo {
	t.touch("Announcements")

	return t.Tx.Announcements()
}

func (t *faultTx) Imports() store.ImportRunRepo {
	t.touch("Imports")

	return t.Tx.Imports()
}

func (t *faultTx) DuplicateDismissals() store.DuplicateDismissalRepo {
	t.touch("DuplicateDismissals")

	return t.Tx.DuplicateDismissals()
}

func (t *faultTx) PushSubscriptions() store.PushSubscriptionRepo {
	t.touch("PushSubscriptions")

	return t.Tx.PushSubscriptions()
}

func (t *faultTx) PushOutbox() store.PushOutboxRepo {
	t.touch("PushOutbox")

	return t.Tx.PushOutbox()
}
