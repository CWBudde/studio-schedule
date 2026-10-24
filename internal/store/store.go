// Package store defines the persistence boundary of the application.
//
// All data access happens inside a transaction started with Store.InTx. The
// repositories obtained from a Tx are only valid while the callback runs.
//
// Rules for implementations and callers:
//
//   - InTx must never be nested. Helpers that need data access take a Tx
//     instead of starting their own transaction. A nested call is detected
//     only through the context: it fails with ErrNestedTx if it gets the
//     callback's ctx (or one derived from it). With an unrelated context it
//     deadlocks in the memory store.
//   - A context canceled before the commit rolls the transaction back.
//   - If the callback returns an error (or panics), every change made through
//     the Tx is rolled back and the error is returned unchanged.
//   - The callback may run more than once: the PostgreSQL store retries it
//     after serialization failures and deadlocks. It must not have side
//     effects outside the transaction.
//   - After a write fails, the callback must return an error: PostgreSQL
//     aborts the transaction, so later statements would fail too. A read
//     that reports domain.ErrNotFound does not abort the transaction.
//   - Writes inside a ReadOnly transaction fail with ErrReadOnly.
//   - Missing records, including the target of an Update, Delete or Remove,
//     are reported as domain.ErrNotFound (possibly wrapped).
//   - domain.ErrConflict (possibly wrapped) reports a duplicate unique key, a
//     reference to a record that does not exist, and the deletion of a record
//     that is still referenced.
//   - Stores do not validate entities; services call Validate before
//     writing.
//   - Optional values are stored as their zero value and come back unchanged.
//     Instants come back in UTC, truncated to microseconds, so entities
//     compare with == after a round trip.
//   - Every list method documents its order. The order always ends with the
//     ID as a tie-breaker, so results are deterministic. Text compares
//     byte-wise (PostgreSQL collation "C"), IDs compare by their bytes. An
//     empty result is an empty (possibly nil) slice, not an error.
//
// The in-memory implementation (package memory) serialises all transactions
// with a single mutex. The PostgreSQL implementation (package postgres) runs
// at READ COMMITTED and takes row locks where documented. Both are verified by
// the shared contract tests in package storetest.
package store

import (
	"context"
	"errors"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
)

var (
	// ErrReadOnly is returned by write operations inside a ReadOnly
	// transaction.
	ErrReadOnly = errors.New("store: write in read-only transaction")
	// ErrNestedTx is returned by InTx when it is called from inside a
	// transaction callback.
	ErrNestedTx = errors.New("store: nested transaction")
)

// TxMode selects the kind of transaction started by Store.InTx.
type TxMode int

const (
	// ReadOnly transactions may only read data.
	ReadOnly TxMode = iota
	// ReadWrite transactions may read and write data.
	ReadWrite
)

// String implements fmt.Stringer.
func (m TxMode) String() string {
	switch m {
	case ReadOnly:
		return "read-only"
	case ReadWrite:
		return "read-write"
	default:
		return "unknown"
	}
}

// Store is the entry point to persistence.
type Store interface {
	// InTx runs fn inside a transaction. The transaction commits if fn
	// returns nil and rolls back otherwise. See the package documentation
	// for the full contract.
	InTx(ctx context.Context, mode TxMode, fn func(ctx context.Context, tx Tx) error) error
}

// Tx gives access to the repositories bound to one transaction.
type Tx interface {
	Settings() SettingsRepo
	Persons() PersonRepo
	Accounts() AccountRepo
	Credentials() CredentialRepo
	Sessions() SessionRepo
	AccountTokens() AccountTokenRepo
	Locations() LocationRepo
	Rooms() RoomRepo
	Staff() StaffRepo
	CourseTypes() CourseTypeRepo
	Terms() TermRepo
	Classes() ClassRepo
	Lessons() LessonRepo
	Bookings() BookingRepo
	Outbox() OutboxRepo
	Audit() AuditRepo
}

// SettingsRepo stores the single, school-wide settings record.
type SettingsRepo interface {
	// Get returns the settings or domain.ErrNotFound if none were stored.
	Get(ctx context.Context) (domain.Settings, error)
	// Put creates or replaces the settings.
	Put(ctx context.Context, s domain.Settings) error
}

// PersonRepo stores persons and the guardian links between them.
type PersonRepo interface {
	Get(ctx context.Context, id domain.PersonID) (domain.Person, error)
	Create(ctx context.Context, p domain.Person) error
	Update(ctx context.Context, p domain.Person) error
	// Delete fails with domain.ErrConflict while the person is referenced
	// (account, staff, guardian link, class member, booking).
	Delete(ctx context.Context, id domain.PersonID) error
	// List returns all persons ordered by family name, given name and ID.
	List(ctx context.Context) ([]domain.Person, error)
	// ListByIDs returns the persons with the given IDs, each once and
	// ordered like List. IDs that do not exist are left out.
	ListByIDs(ctx context.Context, ids []domain.PersonID) ([]domain.Person, error)
	// Lock locks the persons until the transaction ends (in PostgreSQL:
	// SELECT … FOR UPDATE, in ID order, so that two transactions locking
	// the same persons in another order do not deadlock). Whatever checks
	// the links of a person before it changes them locks the person first:
	// the guardian links (no cycles), and what makes a dependent managed
	// (its account, staff links and own dependents) or keeps it from being
	// deleted. It fails with domain.ErrNotFound if a person does not exist,
	// which leaves the transaction usable. It needs a ReadWrite transaction
	// and fails with ErrReadOnly otherwise.
	Lock(ctx context.Context, ids ...domain.PersonID) error

	// AddGuardian links a guardian to a dependent. It fails with
	// domain.ErrDuplicate if the link exists and with domain.ErrConflict if
	// a person does not exist.
	AddGuardian(ctx context.Context, g domain.Guardian) error
	RemoveGuardian(ctx context.Context, g domain.Guardian) error
	// ListDependents returns the dependents of a guardian ordered like List.
	ListDependents(ctx context.Context, guardian domain.PersonID) ([]domain.Person, error)
	// ListGuardians returns the guardians of a dependent ordered like List.
	ListGuardians(ctx context.Context, dependent domain.PersonID) ([]domain.Person, error)
	// ListGuardianLinks returns the guardian links of the given dependents,
	// each once and ordered by dependent and guardian ID.
	ListGuardianLinks(ctx context.Context, dependents []domain.PersonID) ([]domain.Guardian, error)
}

// AccountRepo stores login accounts. Emails are unique ignoring the case of
// ASCII letters (other letters compare exactly), and a person has at most
// one account.
type AccountRepo interface {
	Get(ctx context.Context, id domain.AccountID) (domain.Account, error)
	// GetByEmail finds an account by email, ignoring ASCII case.
	GetByEmail(ctx context.Context, email string) (domain.Account, error)
	// GetByPerson finds the account of a person.
	GetByPerson(ctx context.Context, person domain.PersonID) (domain.Account, error)
	// Create fails with domain.ErrDuplicate if the email or the person has
	// an account, and with domain.ErrConflict if the ID is taken or the
	// person does not exist.
	Create(ctx context.Context, a domain.Account) error
	// Update fails with the same errors as Create.
	Update(ctx context.Context, a domain.Account) error
	// List returns all accounts ordered by ASCII-lower-cased email and ID.
	List(ctx context.Context) ([]domain.Account, error)
	// ListByIDs returns the accounts with the given IDs, each once and
	// ordered like List. IDs that do not exist are left out.
	ListByIDs(ctx context.Context, ids []domain.AccountID) ([]domain.Account, error)
	// ListByPersons returns the accounts of the given persons, each once and
	// ordered like List. Persons without an account are left out.
	ListByPersons(ctx context.Context, persons []domain.PersonID) ([]domain.Account, error)
	// MaxVisibleDays returns the largest VisibleDays override of any
	// account, or 0 if none has one.
	MaxVisibleDays(ctx context.Context) (int, error)
}

// CredentialRepo stores the passwords of accounts, at most one per account.
type CredentialRepo interface {
	// Get fails with domain.ErrNotFound if the account has no password.
	Get(ctx context.Context, account domain.AccountID) (domain.Credential, error)
	// Put creates or replaces the password of c.AccountID. It fails with
	// domain.ErrConflict if the account does not exist.
	Put(ctx context.Context, c domain.Credential) error
}

// SessionRepo stores signed-in sessions, found by the hash of their token.
type SessionRepo interface {
	// GetByToken finds a session by its token hash, whether it has expired
	// or not.
	GetByToken(ctx context.Context, hash [32]byte) (domain.Session, error)
	// Create fails with domain.ErrConflict if the ID or the token hash is
	// taken, or the account or the impersonator does not exist.
	Create(ctx context.Context, s domain.Session) error
	// Update fails with domain.ErrConflict under the same rules as Create.
	Update(ctx context.Context, s domain.Session) error
	Delete(ctx context.Context, id domain.SessionID) error
	// DeleteByAccount deletes every session that acts for the account, its
	// own and the impersonation sessions it started, except the session
	// except (none if zero), and returns how many it deleted. It signs the
	// account out everywhere else when its password is changed or reset.
	DeleteByAccount(ctx context.Context, account domain.AccountID, except domain.SessionID) (int, error)
	// DeleteExpired deletes every session that expired at or before now and
	// returns how many it deleted.
	DeleteExpired(ctx context.Context, now time.Time) (int, error)
}

// AccountTokenRepo stores single-use tokens sent by email, keyed by their
// hash.
type AccountTokenRepo interface {
	// Get finds a token by its hash, whether it has expired or not.
	Get(ctx context.Context, hash [32]byte) (domain.AccountToken, error)
	// Create fails with domain.ErrConflict if the hash is taken or the
	// account does not exist.
	Create(ctx context.Context, t domain.AccountToken) error
	// Delete removes a token. Using a token deletes it, so of two concurrent
	// uses only one succeeds; the other gets domain.ErrNotFound.
	Delete(ctx context.Context, hash [32]byte) error
	// DeleteByAccount removes every token of the account with the purpose,
	// e.g. older confirmation links when a new one is sent.
	DeleteByAccount(ctx context.Context, account domain.AccountID, purpose domain.TokenPurpose) error
	// DeleteExpired deletes every token that expired at or before now and
	// returns how many it deleted.
	DeleteExpired(ctx context.Context, now time.Time) (int, error)
}

// LocationRepo stores locations.
type LocationRepo interface {
	Get(ctx context.Context, id domain.LocationID) (domain.Location, error)
	// Create and Update fail with domain.ErrDuplicate if another location
	// has the name, ignoring ASCII case.
	Create(ctx context.Context, l domain.Location) error
	Update(ctx context.Context, l domain.Location) error
	// Delete fails with domain.ErrConflict while rooms belong to the
	// location.
	Delete(ctx context.Context, id domain.LocationID) error
	// List returns all locations ordered by name and ID.
	List(ctx context.Context) ([]domain.Location, error)
}

// RoomRepo stores rooms.
type RoomRepo interface {
	Get(ctx context.Context, id domain.RoomID) (domain.Room, error)
	// Create and Update fail with domain.ErrConflict if the location does
	// not exist, and with domain.ErrDuplicate if another room of the
	// location has the name, ignoring ASCII case.
	Create(ctx context.Context, r domain.Room) error
	Update(ctx context.Context, r domain.Room) error
	// Delete fails with domain.ErrConflict while classes or lessons use the
	// room.
	Delete(ctx context.Context, id domain.RoomID) error
	// List returns the rooms of a location, or all rooms if location is
	// zero, ordered by name and ID.
	List(ctx context.Context, location domain.LocationID) ([]domain.Room, error)
}

// StaffRepo stores instructors and accompanists.
type StaffRepo interface {
	Get(ctx context.Context, id domain.StaffID) (domain.Staff, error)
	// Create fails with domain.ErrConflict if a set person does not exist.
	Create(ctx context.Context, s domain.Staff) error
	Update(ctx context.Context, s domain.Staff) error
	// Delete fails with domain.ErrConflict while classes or lessons use the
	// staff member.
	Delete(ctx context.Context, id domain.StaffID) error
	// List returns all staff ordered by display name and ID.
	List(ctx context.Context) ([]domain.Staff, error)
	// ListByPerson returns the staff members linked to a person, ordered
	// like List.
	ListByPerson(ctx context.Context, person domain.PersonID) ([]domain.Staff, error)
}

// CourseTypeRepo stores course types.
type CourseTypeRepo interface {
	Get(ctx context.Context, id domain.CourseTypeID) (domain.CourseType, error)
	// Create and Update fail with domain.ErrDuplicate if another course
	// type has the name, ignoring ASCII case.
	Create(ctx context.Context, c domain.CourseType) error
	Update(ctx context.Context, c domain.CourseType) error
	// Delete fails with domain.ErrConflict while classes or lessons use the
	// course type.
	Delete(ctx context.Context, id domain.CourseTypeID) error
	// List returns all course types ordered by name and ID.
	List(ctx context.Context) ([]domain.CourseType, error)
}

// TermRepo stores terms.
type TermRepo interface {
	Get(ctx context.Context, id domain.TermID) (domain.Term, error)
	Create(ctx context.Context, t domain.Term) error
	Update(ctx context.Context, t domain.Term) error
	// Delete fails with domain.ErrConflict while classes belong to the term.
	Delete(ctx context.Context, id domain.TermID) error
	// List returns all terms ordered by start date and ID.
	List(ctx context.Context) ([]domain.Term, error)
	// Lock returns the term and locks it until the transaction ends (in
	// PostgreSQL: SELECT … FOR UPDATE). Whatever generates or deletes
	// lessons from a term's dates locks the term before its classes, so a
	// change of the dates is either complete or not begun. It needs a
	// ReadWrite transaction and fails with ErrReadOnly otherwise.
	Lock(ctx context.Context, id domain.TermID) (domain.Term, error)
}

// ClassRepo stores weekly classes and their members.
type ClassRepo interface {
	Get(ctx context.Context, id domain.ClassID) (domain.Class, error)
	// Create fails with domain.ErrConflict if the term, course type, room or
	// a staff member does not exist.
	Create(ctx context.Context, c domain.Class) error
	Update(ctx context.Context, c domain.Class) error
	// Delete fails with domain.ErrConflict while lessons or members belong
	// to the class.
	Delete(ctx context.Context, id domain.ClassID) error
	// ListByTerm returns the classes of a term ordered by weekday (Sunday
	// first), start time and ID.
	ListByTerm(ctx context.Context, term domain.TermID) ([]domain.Class, error)
	// Lock returns the class and locks it until the transaction ends (in
	// PostgreSQL: SELECT … FOR UPDATE). Class edits and lesson generation
	// lock a class before they read or change its lessons, so they never
	// work on each other's half. It needs a ReadWrite transaction and fails
	// with ErrReadOnly otherwise.
	Lock(ctx context.Context, id domain.ClassID) (domain.Class, error)

	// AddMember fails with domain.ErrDuplicate if the person is already a
	// member and with domain.ErrConflict if the class or person does not
	// exist.
	AddMember(ctx context.Context, m domain.ClassMember) error
	RemoveMember(ctx context.Context, class domain.ClassID, person domain.PersonID) error
	// ListMembers returns the members of a class ordered by person ID.
	ListMembers(ctx context.Context, class domain.ClassID) ([]domain.ClassMember, error)
	// ListMemberships returns the class memberships of a person ordered by
	// class ID.
	ListMemberships(ctx context.Context, person domain.PersonID) ([]domain.ClassMember, error)
}

// LessonRepo stores lessons.
type LessonRepo interface {
	Get(ctx context.Context, id domain.LessonID) (domain.Lesson, error)
	// Create fails with domain.ErrConflict if the class already has a lesson
	// for the same slot date, or a referenced record (the canceling account
	// included) does not exist.
	Create(ctx context.Context, l domain.Lesson) error
	// Update fails with domain.ErrConflict under the same rules as Create.
	Update(ctx context.Context, l domain.Lesson) error
	// Delete fails with domain.ErrConflict while bookings refer to the
	// lesson, canceled ones included.
	Delete(ctx context.Context, id domain.LessonID) error
	// ListByClass returns the lessons of a class whose slot date is from or
	// later (all of them for a zero from), ordered by StartsAt and ID.
	ListByClass(ctx context.Context, class domain.ClassID, from domain.Date) ([]domain.Lesson, error)
	// ListRange returns the lessons with from <= StartsAt < to, ordered by
	// StartsAt and ID.
	ListRange(ctx context.Context, from, to time.Time) ([]domain.Lesson, error)
	// ListRoomOverlaps returns the lessons in the room that are not
	// canceled and overlap [from, to) (StartsAt < to and EndsAt > from),
	// except the lesson exclude (none if zero), ordered by StartsAt and ID.
	ListRoomOverlaps(
		ctx context.Context, room domain.RoomID, from, to time.Time, exclude domain.LessonID,
	) ([]domain.Lesson, error)
	// Lock returns the lesson and locks it until the transaction ends (in
	// PostgreSQL: SELECT … FOR UPDATE). Every capacity check and booking
	// change of a lesson must run under this lock. It needs a ReadWrite
	// transaction and fails with ErrReadOnly otherwise.
	Lock(ctx context.Context, id domain.LessonID) (domain.Lesson, error)
}

// BookingRepo stores bookings and their history. A person has at most one
// active (not canceled) booking per lesson.
type BookingRepo interface {
	Get(ctx context.Context, id domain.BookingID) (domain.Booking, error)
	// Create fails with domain.ErrDuplicate if the person already has an
	// active booking for the lesson, and with domain.ErrConflict (but not
	// ErrDuplicate) if a referenced record does not exist.
	Create(ctx context.Context, b domain.Booking) error
	// Update fails under the same rules as Create.
	Update(ctx context.Context, b domain.Booking) error
	// ListByLesson returns the bookings of a lesson ordered by BookedAt and
	// ID.
	ListByLesson(ctx context.Context, lesson domain.LessonID) ([]domain.Booking, error)
	// CountActive returns the number of bookings of a lesson that are not
	// canceled.
	CountActive(ctx context.Context, lesson domain.LessonID) (int, error)
	// CountActiveByLessons returns, by lesson, the number of bookings that
	// are not canceled, for each of the given lessons that has one; lessons
	// without an active booking (or that do not exist) are left out.
	CountActiveByLessons(ctx context.Context, lessons []domain.LessonID) (map[domain.LessonID]int, error)
	// ListActiveByLessonsAndPersons returns the bookings that are not
	// canceled of any of the given persons in any of the given lessons,
	// ordered by BookedAt and ID.
	ListActiveByLessonsAndPersons(
		ctx context.Context, lessons []domain.LessonID, persons []domain.PersonID,
	) ([]domain.Booking, error)

	// AddEvent appends to the history of a booking. It fails with
	// domain.ErrConflict if the booking or the actor account does not exist.
	AddEvent(ctx context.Context, e domain.BookingEvent) error
	// ListEvents returns the history of a booking ordered by At and ID.
	ListEvents(ctx context.Context, booking domain.BookingID) ([]domain.BookingEvent, error)
}

// OutboxRepo stores outgoing mails.
type OutboxRepo interface {
	Get(ctx context.Context, id domain.OutboxMailID) (domain.OutboxMail, error)
	// Create fails with domain.ErrConflict if the ID is taken.
	Create(ctx context.Context, m domain.OutboxMail) error
	Update(ctx context.Context, m domain.OutboxMail) error
	// ListDue returns at most limit pending mails whose next attempt is due
	// at now, ordered by next attempt and ID. It needs a ReadWrite
	// transaction: the PostgreSQL store locks the mails and skips those that
	// another transaction has locked, so concurrent workers never pick the
	// same mail.
	ListDue(ctx context.Context, now time.Time, limit int) ([]domain.OutboxMail, error)
	// List returns at most limit mails, newest first: ordered by creation
	// time and ID, both descending.
	List(ctx context.Context, limit int) ([]domain.OutboxMail, error)
}

// AuditRepo stores audit events. Events are only ever added.
type AuditRepo interface {
	// Create fails with domain.ErrConflict if the ID is taken or an account
	// does not exist.
	Create(ctx context.Context, e domain.AuditEvent) error
	// List returns at most limit events, newest first: ordered by time and
	// ID, both descending.
	List(ctx context.Context, limit int) ([]domain.AuditEvent, error)
}
