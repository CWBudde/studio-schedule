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
//   - Work that must wait for the commit, such as waking a job that reads
//     what the transaction wrote, is registered with Tx.AfterCommit: it
//     runs once the transaction committed, and never for one rolled back.
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
	// TryLockJob takes the lock of the background job name (ADR 0020)
	// without waiting. It reports ok false while another holder has it:
	// another process on the same database, or another caller in this one.
	// release frees the lock; calling it again does nothing. The lock sits
	// beside the transactions: the holder runs InTx while it holds it, and
	// it is no part of any transaction. It also ends with the process and,
	// on PostgreSQL, with the connection that holds it. err reports a store
	// that could not be asked (ok is then false).
	TryLockJob(ctx context.Context, name string) (release func(), ok bool, err error)
}

// Tx gives access to the repositories bound to one transaction.
type Tx interface {
	// AfterCommit registers fn to run after the transaction committed, in
	// the order of registration, outside any transaction (fn may use the
	// store). A transaction that rolls back, or a run of the callback that
	// is retried, never runs its functions.
	AfterCommit(fn func())

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
	Waitlist() WaitlistRepo
	Notices() LessonNoticeRepo
	CalendarFeeds() CalendarFeedRepo
	PassTypes() PassTypeRepo
	Passes() PassRepo
	Redemptions() PassRedemptionRepo
	Receipts() ReceiptRepo
	ReceiptCounters() ReceiptCounterRepo
	Outbox() OutboxRepo
	Audit() AuditRepo
	Announcements() AnnouncementRepo
	Imports() ImportRunRepo
	DuplicateDismissals() DuplicateDismissalRepo
	PushSubscriptions() PushSubscriptionRepo
	PushOutbox() PushOutboxRepo
	JobRuns() JobRunRepo
	Events() EventRepo
	EventRegistrations() EventRegistrationRepo
}

// SettingsRepo stores the single, school-wide settings record.
type SettingsRepo interface {
	// Get returns the settings or domain.ErrNotFound if none were stored.
	Get(ctx context.Context) (domain.Settings, error)
	// Put creates or replaces the settings.
	Put(ctx context.Context, s domain.Settings) error
	// Logo returns the school's logo, the zero Logo if it has none, or
	// domain.ErrNotFound if no settings were stored. It is apart from Get,
	// which every request reads, because only receipts need it.
	Logo(ctx context.Context) (domain.Logo, error)
	// PutLogo replaces the logo; the zero Logo removes it. It fails with
	// domain.ErrNotFound if no settings were stored.
	PutLogo(ctx context.Context, l domain.Logo) error
	// LogoInfo returns the content type and the digest of the logo
	// (domain.Logo.Digest, kept when the logo is written) without reading
	// it, the zero LogoInfo without one, or domain.ErrNotFound if no
	// settings were stored.
	LogoInfo(ctx context.Context) (domain.LogoInfo, error)
	// Background returns the school's background image (ADR 0018), the
	// zero Background if it has none, or domain.ErrNotFound if no settings
	// were stored. It is apart from Get, like the logo.
	Background(ctx context.Context) (domain.Background, error)
	// PutBackground replaces the background; the zero Background removes
	// it. It fails with domain.ErrNotFound if no settings were stored.
	PutBackground(ctx context.Context, b domain.Background) error
	// BackgroundInfo returns the content type and the digest of the
	// background (domain.Background.Digest, kept when it is written)
	// without reading it, the zero BackgroundInfo without one, or
	// domain.ErrNotFound if no settings were stored.
	BackgroundInfo(ctx context.Context) (domain.BackgroundInfo, error)
	// Usage reports which data depends on the time zone and the currency
	// (ADR 0013); it needs no stored settings.
	Usage(ctx context.Context) (SettingsUsage, error)
}

// SettingsUsage is the data that fixes settings chosen at the first setup.
type SettingsUsage struct {
	// Lessons: some lesson exists, whose instants were computed in the
	// school's time zone.
	Lessons bool
	// Prices: some pass type or event has a price above 0, some pass was
	// paid, or some event registration has a price above 0.
	Prices bool
	// Receipts: some receipt was issued.
	Receipts bool
}

// PersonRepo stores persons and the guardian links between them. Member
// numbers are unique (exactly, not ignoring case). A merged person (a
// tombstone, ADR 0015) is found by Get, ListByIDs and ListMerged only, an
// anonymized one (ADR 0016) by those and its links.
type PersonRepo interface {
	Get(ctx context.Context, id domain.PersonID) (domain.Person, error)
	// Create fails with domain.ErrDuplicate if another person has the
	// member number, and with domain.ErrConflict (but not ErrDuplicate) if
	// the ID is taken or the person it is merged into does not exist.
	Create(ctx context.Context, p domain.Person) error
	// Update fails under the same rules as Create.
	Update(ctx context.Context, p domain.Person) error
	// Delete fails with domain.ErrConflict while the person is referenced
	// (account, staff, guardian link, class member, booking or a booking
	// merged from them, an event registration, a pass they own or paid for,
	// a receipt they paid, an audit event about them, a person merged into
	// them). The person's
	// waiting-list entries and duplicate dismissals are deleted with it.
	Delete(ctx context.Context, id domain.PersonID) error
	// GetByMemberNo finds the person with the member number.
	GetByMemberNo(ctx context.Context, memberNo string) (domain.Person, error)
	// List returns all persons that are neither merged nor anonymized,
	// ordered by family name, given name and ID.
	List(ctx context.Context) ([]domain.Person, error)
	// ListMerged returns the persons merged into the person, ordered like
	// List.
	ListMerged(ctx context.Context, into domain.PersonID) ([]domain.Person, error)
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
	// ListTombstoneReferences returns what still refers to a merged or an
	// anonymized person although merging or anonymizing it would have
	// moved or ended it (ADR 0015, ADR 0016; db check), ordered by person
	// ID, kind and the referring record's ID. See TombstoneRefKind for
	// what counts.
	ListTombstoneReferences(ctx context.Context) ([]TombstoneReference, error)
}

// TombstoneReference is a record that refers to a merged or an anonymized
// person (PersonRepo.ListTombstoneReferences).
type TombstoneReference struct {
	// Person is the merged or anonymized person. Anonymized: it is
	// anonymized and not merged; the rules of a merged person apply to one
	// that is both.
	Person     domain.PersonID
	Anonymized bool
	Kind       TombstoneRefKind
	// Ref is the ID of the referring record in its text form: the
	// booking, waiting-list entry, class, pass, staff record or account,
	// or the other person of a guardian link, a duplicate dismissal or a
	// merge.
	Ref string
}

// TombstoneRefKind says what refers to a merged or an anonymized person.
// Merging a person moves everything but its receipts, its closed
// waiting-list entries and a disabled account to the survivor; anonymizing
// one keeps its past bookings and its passes (ADR 0015, ADR 0016).
type TombstoneRefKind string

// The references to a tombstone, in the order ListTombstoneReferences
// sorts them.
const (
	// TombstoneBooking: a booking of a merged person, or an active (not
	// canceled) booking of an anonymized one of a lesson that starts after
	// it was anonymized.
	TombstoneBooking TombstoneRefKind = "booking"
	// TombstoneWaitlist: an open (waiting or invited) waiting-list entry.
	TombstoneWaitlist TombstoneRefKind = "waitlist-entry"
	// TombstoneMember: a class membership; Ref is the class.
	TombstoneMember TombstoneRefKind = "class-member"
	// TombstoneGuardian: the person is a guardian; Ref is the dependent.
	TombstoneGuardian TombstoneRefKind = "guardian"
	// TombstoneDependent: the person is a dependent; Ref is the guardian.
	TombstoneDependent TombstoneRefKind = "dependent"
	// TombstonePass: a pass the person owns: any of a merged person, one
	// sold after it was anonymized of an anonymized one.
	TombstonePass TombstoneRefKind = "pass"
	// TombstonePaidPass: a pass the person paid for, under the same rule.
	TombstonePaidPass TombstoneRefKind = "paid-pass"
	// TombstoneStaff: a staff record names the person.
	TombstoneStaff TombstoneRefKind = "staff"
	// TombstoneAccount: the account of an anonymized person, or one of a
	// merged person that is not disabled.
	TombstoneAccount TombstoneRefKind = "account"
	// TombstoneDismissal: a duplicate dismissal names the person; Ref is
	// the other person.
	TombstoneDismissal TombstoneRefKind = "duplicate-dismissal"
	// TombstoneMerged: a person is merged into a merged person (a merge
	// moves those to the survivor); Ref is that person.
	TombstoneMerged TombstoneRefKind = "merged-person"
)

// TombstoneRefKinds returns the kinds in their order.
func TombstoneRefKinds() []TombstoneRefKind {
	return []TombstoneRefKind{
		TombstoneBooking, TombstoneWaitlist, TombstoneMember, TombstoneGuardian, TombstoneDependent, TombstonePass,
		TombstonePaidPass, TombstoneStaff, TombstoneAccount, TombstoneDismissal, TombstoneMerged,
	}
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
	// ListByRole returns the accounts with the role, enabled or not, ordered
	// like List.
	ListByRole(ctx context.Context, role domain.Role) ([]domain.Account, error)
	// MaxVisibleDays returns the largest VisibleDays override of any
	// account, or 0 if none has one.
	MaxVisibleDays(ctx context.Context) (int, error)
	// Delete deletes an account; its person stays (ADR 0016). What is the
	// account's own goes with it: its password, its sessions and the
	// impersonation sessions it started, its email tokens, calendar feeds
	// and announcement dismissals. Records that only name it as who did
	// something forget it (zero): booking events, waiting-list entries it
	// added, lessons it canceled, passes it sold, import runs and lesson
	// notices it sent, events it created and event registrations it made. Audit events that target it target its person
	// instead. While receipts it created, audit events it acted in,
	// announcements it wrote or mailed, or duplicate dismissals it recorded
	// name it, the deletion fails with domain.ErrConflict and changes
	// nothing. An unknown account is domain.ErrNotFound.
	Delete(ctx context.Context, id domain.AccountID) error
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
	// except (none if zero), and returns the deleted sessions ordered by ID.
	// It signs the account out everywhere else when its password is changed
	// or reset; the caller records the impersonations it ends.
	DeleteByAccount(ctx context.Context, account domain.AccountID, except domain.SessionID) ([]domain.Session, error)
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
	// location. The calendar feeds of the location are deleted with it.
	Delete(ctx context.Context, id domain.LocationID) error
	// List returns all locations ordered by name and ID.
	List(ctx context.Context) ([]domain.Location, error)
}

// RoomRepo stores rooms.
type RoomRepo interface {
	Get(ctx context.Context, id domain.RoomID) (domain.Room, error)
	// Lock returns the room and locks it until the transaction ends (in
	// PostgreSQL: SELECT … FOR UPDATE), so a concurrent SetOrder of its
	// location waits for it. It fails with store.ErrReadOnly in a read-only
	// transaction.
	Lock(ctx context.Context, id domain.RoomID) (domain.Room, error)
	// Create and Update fail with domain.ErrConflict if the location does
	// not exist, and with domain.ErrDuplicate if another room of the
	// location has the name, ignoring ASCII case.
	Create(ctx context.Context, r domain.Room) error
	Update(ctx context.Context, r domain.Room) error
	// Delete fails with domain.ErrConflict while classes or lessons use the
	// room.
	Delete(ctx context.Context, id domain.RoomID) error
	// List returns the rooms of a location, or all rooms if location is
	// zero, ordered by location name and ID, then position, name and ID.
	List(ctx context.Context, location domain.LocationID) ([]domain.Room, error)
	// SetOrder sets the positions of the location's rooms to 1..n in the
	// order given. It fails with domain.ErrValidation unless order holds
	// every room of the location exactly once and no other room.
	SetOrder(ctx context.Context, location domain.LocationID, order []domain.RoomID) error
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
	// Delete fails with domain.ErrConflict while classes, lessons, events or
	// announcements use the course type.
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
	// to the class, or announcements are for its members.
	Delete(ctx context.Context, id domain.ClassID) error
	// ListByTerm returns the classes of a term ordered by weekday (Sunday
	// first), start time and ID.
	ListByTerm(ctx context.Context, term domain.TermID) ([]domain.Class, error)
	// ListByIDs returns the classes with the given IDs, each once and
	// ordered by ID. IDs that do not exist are left out.
	ListByIDs(ctx context.Context, ids []domain.ClassID) ([]domain.Class, error)
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
	// UpdateMember replaces the dates of a membership; a person who is not
	// a member is domain.ErrNotFound.
	UpdateMember(ctx context.Context, m domain.ClassMember) error
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
	// and the event included) does not exist.
	Create(ctx context.Context, l domain.Lesson) error
	// Update fails with domain.ErrConflict under the same rules as Create.
	Update(ctx context.Context, l domain.Lesson) error
	// Delete fails with domain.ErrConflict while bookings refer to the
	// lesson, canceled ones included. The lesson's waiting-list entries and
	// announcements are deleted with it; its notices stay, without the
	// lesson.
	Delete(ctx context.Context, id domain.LessonID) error
	// ListByClassStartingAfter returns the lessons of a class that start
	// after the instant, whatever their slot date, ordered by StartsAt and
	// ID.
	ListByClassStartingAfter(ctx context.Context, class domain.ClassID, after time.Time) ([]domain.Lesson, error)
	// ListByCourseTypeStartingAfter returns the lessons of a course type
	// that start after the instant, ordered by StartsAt and ID.
	ListByCourseTypeStartingAfter(
		ctx context.Context, courseType domain.CourseTypeID, after time.Time,
	) ([]domain.Lesson, error)
	// ListByClass returns the lessons of a class whose slot date is from or
	// later (all of them for a zero from), ordered by StartsAt and ID.
	ListByClass(ctx context.Context, class domain.ClassID, from domain.Date) ([]domain.Lesson, error)
	// ListRange returns the lessons with from <= StartsAt < to, ordered by
	// StartsAt and ID.
	ListRange(ctx context.Context, from, to time.Time) ([]domain.Lesson, error)
	// ListByIDs returns the lessons with the given IDs, each once and
	// ordered by StartsAt and ID. IDs that do not exist are left out.
	ListByIDs(ctx context.Context, ids []domain.LessonID) ([]domain.Lesson, error)
	// ListByEvent returns the sessions of an event (the lessons whose
	// EventID it is), canceled ones included, ordered by StartsAt and ID.
	ListByEvent(ctx context.Context, event domain.EventID) ([]domain.Lesson, error)
	// ListByEvents returns the sessions of the given events, canceled ones
	// included, ordered by StartsAt and ID.
	ListByEvents(ctx context.Context, events []domain.EventID) ([]domain.Lesson, error)
	// ListByStaff returns the lessons with from <= StartsAt < to whose
	// instructor or accompanist is one of the staff members, ordered by
	// StartsAt and ID.
	ListByStaff(ctx context.Context, staff []domain.StaffID, from, to time.Time) ([]domain.Lesson, error)
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
	// ErrDuplicate) if a referenced record does not exist. A booking kept
	// from a merge (domain.Booking.MergedFrom set) is left out of the
	// duplicate check on either side.
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
	// CountsByLessons returns, by lesson, the counts of the bookings that
	// are not canceled, for each of the given lessons that has one; lessons
	// without an active booking (or that do not exist) are left out.
	CountsByLessons(ctx context.Context, lessons []domain.LessonID) (map[domain.LessonID]BookingCounts, error)
	// CountLateCanceledByLessons returns, by lesson, the number of bookings
	// canceled late, for each of the given lessons that has one; other
	// lessons (and those that do not exist) are left out.
	CountLateCanceledByLessons(ctx context.Context, lessons []domain.LessonID) (map[domain.LessonID]int, error)
	// CountActivePersons returns how many different persons have an active
	// (not canceled) booking in any of the given lessons; guests do not
	// count.
	CountActivePersons(ctx context.Context, lessons []domain.LessonID) (int, error)
	// CountsByPersons returns, by person, the counts of their bookings in
	// any of the given lessons, for each person with an active (not
	// canceled) or a late-canceled booking there; guests do not count.
	CountsByPersons(ctx context.Context, lessons []domain.LessonID) (map[domain.PersonID]PersonBookingCounts, error)
	// ListActiveByLessons returns the bookings that are not canceled of any
	// of the given lessons, ordered by BookedAt and ID.
	ListActiveByLessons(ctx context.Context, lessons []domain.LessonID) ([]domain.Booking, error)
	// ListActiveByLessonsAndPersons returns the bookings that are not
	// canceled of any of the given persons in any of the given lessons,
	// ordered by BookedAt and ID.
	ListActiveByLessonsAndPersons(
		ctx context.Context, lessons []domain.LessonID, persons []domain.PersonID,
	) ([]domain.Booking, error)

	// ListByLessonsAndPersons returns the bookings of any of the given
	// persons in any of the given lessons, canceled ones included, ordered by
	// BookedAt and ID.
	ListByLessonsAndPersons(
		ctx context.Context, lessons []domain.LessonID, persons []domain.PersonID,
	) ([]domain.Booking, error)
	// ListByIDs returns the bookings with the given IDs, each once and
	// ordered by BookedAt and ID. IDs that do not exist are left out.
	ListByIDs(ctx context.Context, ids []domain.BookingID) ([]domain.Booking, error)
	// ListByPersons returns the bookings of q.Persons (only the active ones
	// with q.Active) whose lessons lie within the bounds of q, ordered by
	// the lesson's StartsAt and ID, so that the bookings of a lesson stay
	// together, then by BookedAt and ID: all ascending, or all descending
	// with q.Desc. A positive q.Limit caps how many it returns.
	ListByPersons(ctx context.Context, q BookingQuery) ([]domain.Booking, error)
	// DeleteByLessonAndSource deletes the bookings of a lesson that came
	// about by source, canceled ones included, together with their events,
	// and returns how many bookings it deleted. The office's timetable edits
	// delete a lesson with its automatic class member bookings this way
	// (ADR 0007, ADR 0009). A booking a pass paid for is not deleted: the
	// call fails with domain.ErrConflict.
	DeleteByLessonAndSource(ctx context.Context, lesson domain.LessonID, source domain.BookingSource) (int, error)
	// AnonymizeGuests replaces the name of every guest booking whose lesson
	// ended before the instant with domain.AnonymousGuestName and clears its
	// email and trial note (ADR 0016), canceled ones included, and returns
	// how many bookings it changed; one anonymized already is not changed
	// again. It needs a ReadWrite transaction.
	AnonymizeGuests(ctx context.Context, endedBefore time.Time) (int, error)

	// AddEvent appends to the history of a booking. It fails with
	// domain.ErrConflict if the booking or the actor account does not exist.
	AddEvent(ctx context.Context, e domain.BookingEvent) error
	// ListEvents returns the history of a booking ordered by At and ID.
	ListEvents(ctx context.Context, booking domain.BookingID) ([]domain.BookingEvent, error)
	// ListOverCapacity returns the lessons with more active (not canceled)
	// bookings than places, ordered by lesson ID (db check).
	ListOverCapacity(ctx context.Context) ([]LessonOccupancy, error)
}

// BookingQuery selects the bookings of persons by when their lessons take
// place (BookingRepo.ListByPersons). A zero instant leaves its bound out.
type BookingQuery struct {
	Persons []domain.PersonID
	// Active keeps the active (not canceled) bookings only.
	Active bool
	// StartsFrom <= the lesson's StartsAt < StartsBefore.
	StartsFrom, StartsBefore time.Time
	// EndsAfter < the lesson's EndsAt <= EndsBy: EndsAfter keeps the
	// lessons not over at that instant, EndsBy those over by then.
	EndsAfter, EndsBy time.Time
	// Desc orders the newest lessons first.
	Desc bool
	// Limit, if positive, is the most bookings returned.
	Limit int
}

// LessonOccupancy is a lesson's capacity and its active bookings
// (BookingRepo.ListOverCapacity).
type LessonOccupancy struct {
	Lesson   domain.LessonID
	Capacity int
	// Active counts the active (not canceled) bookings; Overrides those of
	// them with an event that records a booking beyond the capacity
	// (domain.BookingEvent.Override).
	Active, Overrides int
}

// BookingCounts counts the active (not canceled) bookings of a lesson.
type BookingCounts struct {
	Active int
	// Trials, Guests and Booked count the active bookings that are trial
	// lessons, that are a guest's (no person), and whose attendance is
	// unknown (status booked).
	Trials, Guests, Booked int
	// Attended and NoShow count the active bookings whose attendance was
	// taken: present or absent.
	Attended, NoShow int
}

// PersonBookingCounts counts the bookings of a person in a set of lessons.
type PersonBookingCounts struct {
	// Active counts the bookings that are not canceled, trials included.
	Active int
	// Attended counts the active bookings marked as attended.
	Attended int
	// LateCanceled counts the bookings canceled after the cutoff.
	LateCanceled int
}

// WaitlistRepo stores the waiting-list entries of lessons (ADR 0010). A
// person has at most one open (waiting or invited) entry per lesson, and an
// invite token hash belongs to one entry. Entries go with their lesson and
// their person (LessonRepo.Delete, PersonRepo.Delete).
type WaitlistRepo interface {
	Get(ctx context.Context, id domain.WaitlistEntryID) (domain.WaitlistEntry, error)
	// GetByTokenHash finds the entry an invite token was sent for, whatever
	// its status.
	GetByTokenHash(ctx context.Context, hash [32]byte) (domain.WaitlistEntry, error)
	// Create fails with domain.ErrDuplicate if the person has an open entry
	// for the lesson, and with domain.ErrConflict (but not ErrDuplicate) if
	// the ID or the token hash is taken or a referenced record does not
	// exist.
	Create(ctx context.Context, e domain.WaitlistEntry) error
	// Update fails under the same rules as Create.
	Update(ctx context.Context, e domain.WaitlistEntry) error
	// ListByLesson returns the entries of a lesson, ended ones included,
	// ordered by JoinedAt and ID: the order of the list.
	ListByLesson(ctx context.Context, lesson domain.LessonID) ([]domain.WaitlistEntry, error)
	// ListOpenByLessons returns the open (waiting or invited) entries of the
	// given lessons, ordered by JoinedAt and ID.
	ListOpenByLessons(ctx context.Context, lessons []domain.LessonID) ([]domain.WaitlistEntry, error)
	// CountByLessons returns, by lesson, how many entries each of the given
	// lessons has, ended ones included; lessons without one are left out.
	CountByLessons(ctx context.Context, lessons []domain.LessonID) (map[domain.LessonID]int, error)
	// ListOpenByPersons returns the open (waiting or invited) entries of
	// the given persons, ordered by JoinedAt and ID.
	ListOpenByPersons(ctx context.Context, persons []domain.PersonID) ([]domain.WaitlistEntry, error)
	// ListByPersons returns every entry of the given persons, ended ones
	// included, ordered by JoinedAt and ID.
	ListByPersons(ctx context.Context, persons []domain.PersonID) ([]domain.WaitlistEntry, error)
	// ListDueLessons returns, each once and ordered by ID, the lessons that
	// have an invited entry whose invite expired at or before now, or that
	// started at or before now and still have an open entry: the work of the
	// waitlist-expiry job.
	ListDueLessons(ctx context.Context, now time.Time) ([]domain.LessonID, error)
}

// LessonNoticeRepo stores the notices staff sent to the participants of
// lessons. A notice outlives its lesson with a zero LessonID
// (LessonRepo.Delete).
type LessonNoticeRepo interface {
	// Create fails with domain.ErrConflict if the ID is taken or the lesson
	// or the sender does not exist.
	Create(ctx context.Context, n domain.LessonNotice) error
	// ListBySender returns the notices the account sent at or after since,
	// ordered by SentAt and ID.
	ListBySender(ctx context.Context, sender domain.AccountID, since time.Time) ([]domain.LessonNotice, error)
	// ListByLesson returns the notices sent about the lesson at or after
	// since, ordered by SentAt and ID.
	ListByLesson(ctx context.Context, lesson domain.LessonID, since time.Time) ([]domain.LessonNotice, error)
}

// CalendarFeedRepo stores the calendar feeds of accounts (ADR 0011). An
// account has at most one active (not revoked) feed per scope and location,
// and a token hash belongs to one feed. Feeds go with their account and their
// location.
type CalendarFeedRepo interface {
	Get(ctx context.Context, id domain.CalendarFeedID) (domain.CalendarFeed, error)
	// GetByTokenHash finds the feed of a token, whether it is revoked or not.
	GetByTokenHash(ctx context.Context, hash [32]byte) (domain.CalendarFeed, error)
	// Create fails with domain.ErrDuplicate if the account has an active
	// feed of the same scope and location (none counting as one), and with
	// domain.ErrConflict (but not ErrDuplicate) if the ID or the token hash
	// is taken or the account or the location does not exist.
	Create(ctx context.Context, f domain.CalendarFeed) error
	// ListByAccount returns the active feeds of an account ordered by
	// CreatedAt and ID.
	ListByAccount(ctx context.Context, account domain.AccountID) ([]domain.CalendarFeed, error)
	// Revoke revokes the active feed id at the instant; a feed that does not
	// exist or is revoked already is domain.ErrNotFound.
	Revoke(ctx context.Context, id domain.CalendarFeedID, at time.Time) error
	// RevokeActive revokes the account's active feed of the scope and the
	// location (zero for none) at the instant, if it has one, and returns
	// how many it revoked (0 or 1).
	RevokeActive(
		ctx context.Context, account domain.AccountID, scope domain.FeedScope, location domain.LocationID, at time.Time,
	) (int, error)
	// TouchLastUsed sets the LastUsedAt of feed id, revoked or not, to the
	// instant; a feed that does not exist is domain.ErrNotFound.
	TouchLastUsed(ctx context.Context, id domain.CalendarFeedID, at time.Time) error
}

// PassTypeRepo stores the pass catalog (ADR 0012). Names are unique
// ignoring ASCII case.
type PassTypeRepo interface {
	Get(ctx context.Context, id domain.PassTypeID) (domain.PassType, error)
	// Create and Update fail with domain.ErrDuplicate if another pass type
	// has the name, ignoring ASCII case; Create with domain.ErrConflict (but
	// not ErrDuplicate) if the ID is taken.
	Create(ctx context.Context, t domain.PassType) error
	Update(ctx context.Context, t domain.PassType) error
	// Delete fails with domain.ErrConflict while passes are of the type.
	Delete(ctx context.Context, id domain.PassTypeID) error
	// List returns all pass types ordered by name and ID.
	List(ctx context.Context) ([]domain.PassType, error)
}

// PassRepo stores the passes of persons (ADR 0012). Passes are never
// deleted; the office voids them.
type PassRepo interface {
	Get(ctx context.Context, id domain.PassID) (domain.Pass, error)
	// Create fails with domain.ErrConflict if the ID is taken or the pass
	// type, the owner, the payer or the selling account does not exist.
	Create(ctx context.Context, p domain.Pass) error
	// Update fails under the same rules as Create.
	Update(ctx context.Context, p domain.Pass) error
	// ListByIDs returns the passes with the given IDs, each once and ordered
	// by ID. IDs that do not exist are left out.
	ListByIDs(ctx context.Context, ids []domain.PassID) ([]domain.Pass, error)
	// ListByPersons returns the passes the given persons own, ordered by
	// SoldAt and ID.
	ListByPersons(ctx context.Context, persons []domain.PersonID) ([]domain.Pass, error)
	// ListByPayers returns the passes the given persons paid for, ordered by
	// ID.
	ListByPayers(ctx context.Context, persons []domain.PersonID) ([]domain.Pass, error)
	// LockByPersons returns the passes the given persons own, ordered by ID,
	// and locks them until the transaction ends (in PostgreSQL: SELECT …
	// FOR UPDATE, in ID order, so that transactions locking the passes of
	// the same persons do not deadlock). Every redemption and refund, and
	// voiding a pass, runs under this lock, after the lesson lock if it
	// takes one (term → class → lesson → passes). A pass sold while the
	// lock is held is not locked. It needs a ReadWrite transaction and fails
	// with ErrReadOnly otherwise.
	LockByPersons(ctx context.Context, persons ...domain.PersonID) ([]domain.Pass, error)
	// GetByReceipt returns the pass whose sale the receipt confirms, or
	// domain.ErrNotFound. A receipt belongs to one pass at most; Create and
	// Update fail with domain.ErrConflict for a receipt another pass has,
	// or one that does not exist.
	GetByReceipt(ctx context.Context, receipt domain.ReceiptID) (domain.Pass, error)
	// ListByReceipts returns the passes the given receipts confirm, ordered
	// by ID. Receipts without a pass are left out.
	ListByReceipts(ctx context.Context, receipts []domain.ReceiptID) ([]domain.Pass, error)
	// ListUnbalanced returns the visit passes whose used visits
	// (VisitsTotal − VisitsRemaining) differ from their redemptions,
	// ordered by pass ID (db check).
	ListUnbalanced(ctx context.Context) ([]PassBalance, error)
}

// PassRedemptionRepo stores which bookings passes paid for. A booking has at
// most one redemption.
type PassRedemptionRepo interface {
	// Create fails with domain.ErrDuplicate if the booking has a
	// redemption, and with domain.ErrConflict (but not ErrDuplicate) if the
	// pass or the booking does not exist.
	Create(ctx context.Context, r domain.PassRedemption) error
	// GetByBooking returns the redemption of a booking, or
	// domain.ErrNotFound if no pass paid for it.
	GetByBooking(ctx context.Context, booking domain.BookingID) (domain.PassRedemption, error)
	// Delete deletes the redemption of a booking; one that has none is
	// domain.ErrNotFound.
	Delete(ctx context.Context, booking domain.BookingID) error
	// ListByPasses returns the redemptions of the given passes, ordered by
	// pass ID, At and booking ID. A positive latest keeps only the latest
	// that many of each pass (by At and booking ID).
	ListByPasses(ctx context.Context, passes []domain.PassID, latest int) ([]domain.PassRedemption, error)
	// CountByPasses returns, by pass, how many redemptions each of the
	// given passes has; passes without one are left out.
	CountByPasses(ctx context.Context, passes []domain.PassID) (map[domain.PassID]int, error)
	// ListByBookings returns the redemptions of the given bookings, ordered
	// by booking ID.
	ListByBookings(ctx context.Context, bookings []domain.BookingID) ([]domain.PassRedemption, error)
	// ListStray returns the redemptions of a booking canceled in time (not
	// late) or of a lesson the school canceled, which cost no visit (ADR
	// 0012), ordered by booking ID (db check).
	ListStray(ctx context.Context) ([]StrayRedemption, error)
}

// PassBalance is a visit pass's used visits and its redemptions
// (PassRepo.ListUnbalanced).
type PassBalance struct {
	Pass domain.PassID
	// Used is VisitsTotal − VisitsRemaining; Redeemed counts the pass's
	// redemptions.
	Used, Redeemed int
}

// StrayRedemption is a redemption of a booking that costs no visit
// (PassRedemptionRepo.ListStray): its booking was canceled in time, or its
// lesson was canceled by the school, or both.
type StrayRedemption struct {
	Redemption domain.PassRedemption
	Lesson     domain.LessonID
	// CanceledInTime: the booking is canceled and not late.
	// LessonCanceled: the lesson is canceled.
	CanceledInTime, LessonCanceled bool
}

// ReceiptFilter selects receipts for ReceiptRepo.List.
type ReceiptFilter struct {
	// Year selects the receipts of one year; zero selects all.
	Year int
	// Query matches receipts whose number or recipient name contains it,
	// ignoring ASCII case; empty matches all.
	Query string
	// Offset skips that many receipts; Limit returns at most that many
	// (zero or less: none).
	Offset, Limit int
}

// ReceiptDayTotals sums the receipts of one issue date and currency.
type ReceiptDayTotals struct {
	Date     domain.Date
	Currency string
	// Sales and Cancellations count the receipts of each kind.
	Sales, Cancellations int
	// Gross is the sum of their totals; cancellations count negative.
	Gross domain.Money
}

// ReceiptRepo stores receipts (ADR 0012). Receipts are immutable: there is
// no Update or Delete, and the PostgreSQL store rejects both with a
// trigger. Receipts come with their lines, ordered by position.
type ReceiptRepo interface {
	// Create stores a receipt with its lines. It fails with
	// domain.ErrDuplicate if the number, or the year and sequence, are
	// taken or the receipt it cancels has a cancellation already, and with
	// domain.ErrConflict if the ID is taken or the receipt it cancels, the
	// payer or the creating account does not exist.
	Create(ctx context.Context, r domain.Receipt) error
	Get(ctx context.Context, id domain.ReceiptID) (domain.Receipt, error)
	GetByNumber(ctx context.Context, number string) (domain.Receipt, error)
	// ListByIDs returns the receipts with the given IDs, each once and
	// ordered by ID. IDs that do not exist are left out.
	ListByIDs(ctx context.Context, ids []domain.ReceiptID) ([]domain.Receipt, error)
	// GetCancellation returns the cancellation of the receipt, or
	// domain.ErrNotFound if it has none.
	GetCancellation(ctx context.Context, id domain.ReceiptID) (domain.Receipt, error)
	// List returns the receipts that match f, newest first (year and
	// sequence descending, then ID), and how many match in all.
	List(ctx context.Context, f ReceiptFilter) ([]domain.Receipt, int, error)
	// ListByPayers returns the receipts the given persons paid, ordered
	// like List.
	ListByPayers(ctx context.Context, persons []domain.PersonID) ([]domain.Receipt, error)
	// ListCancellations returns the cancellations of the given receipts,
	// ordered by the ID of the receipt they cancel.
	ListCancellations(ctx context.Context, ids []domain.ReceiptID) ([]domain.Receipt, error)
	// TotalsByDay sums the receipts issued on the school dates from to to
	// (both inclusive) by issue date and currency, ordered by date and
	// currency; days without a receipt are left out.
	TotalsByDay(ctx context.Context, from, to domain.Date) ([]ReceiptDayTotals, error)
	// PutPDF stores the rendered PDF of a receipt, once: it fails with
	// domain.ErrDuplicate if the receipt has one, and with
	// domain.ErrConflict if the receipt does not exist.
	PutPDF(ctx context.Context, id domain.ReceiptID, pdf []byte) error
	// PDF returns the stored PDF of a receipt, or domain.ErrNotFound if it
	// has none (yet).
	PDF(ctx context.Context, id domain.ReceiptID) ([]byte, error)
}

// ReceiptCounterRepo hands out the gapless sequence numbers of receipts.
type ReceiptCounterRepo interface {
	// Next returns the next sequence number of the year, starting at 1. The
	// number belongs to the transaction: PostgreSQL locks the year's
	// counter until it ends, so concurrent issuers wait, and a rollback
	// gives the number back. It needs a ReadWrite transaction and fails
	// with ErrReadOnly otherwise.
	Next(ctx context.Context, year int) (int, error)
	// Years returns the years with numbers issued, the latest first.
	Years(ctx context.Context) ([]int, error)
	// ListStates returns, for every year with a counter or a receipt, the
	// counter's last number (zero without one) and the highest sequence of
	// the year's receipts (zero without any), ordered by year (db check).
	ListStates(ctx context.Context) ([]ReceiptCounterState, error)
}

// ReceiptCounterState is a year's receipt counter beside the receipts
// issued (ReceiptCounterRepo.ListStates).
type ReceiptCounterState struct {
	Year int
	// Last is the counter's last number, Highest the highest sequence of
	// the year's receipts.
	Last, Highest int
}

// OutboxRepo stores outgoing mails.
type OutboxRepo interface {
	Get(ctx context.Context, id domain.OutboxMailID) (domain.OutboxMail, error)
	// Create fails with domain.ErrConflict if the ID is taken or the
	// attached receipt does not exist.
	Create(ctx context.Context, m domain.OutboxMail) error
	Update(ctx context.Context, m domain.OutboxMail) error
	// ListDue returns at most limit pending mails whose next attempt is due
	// at now, ordered by next attempt and ID. It needs a ReadWrite
	// transaction: the PostgreSQL store locks the mails and skips those that
	// another transaction has locked, so concurrent workers never pick the
	// same mail.
	ListDue(ctx context.Context, now time.Time, limit int) ([]domain.OutboxMail, error)
	// List returns at most f.Limit mails that match f, newest first:
	// ordered by creation time and ID, both descending.
	List(ctx context.Context, f OutboxFilter) ([]domain.OutboxMail, error)
	// PurgeBodies removes the text and HTML of every mail sent before the
	// instant that still has them (ADR 0014) and returns how many it
	// changed.
	PurgeBodies(ctx context.Context, sentBefore time.Time) (int, error)
	// DeleteCreatedBefore deletes every mail created before the instant,
	// whatever its status, and returns how many it deleted.
	DeleteCreatedBefore(ctx context.Context, before time.Time) (int, error)
	// CountUnsent counts the mails that are not sent, by status (the
	// metrics of the outbox).
	CountUnsent(ctx context.Context) (OutboxCounts, error)
}

// PushSubscriptionRepo stores the web push subscriptions of accounts (ADR
// 0017). An endpoint belongs to one subscription. Subscriptions go with
// their account, and their messages go with them.
type PushSubscriptionRepo interface {
	Get(ctx context.Context, id domain.PushSubscriptionID) (domain.PushSubscription, error)
	// Save stores s by its endpoint. A new endpoint is created with s. The
	// endpoint of the same account is updated: its keys, DeviceLabel and
	// LastUsedAt change, its ID and CreatedAt stay. The endpoint of another
	// account (the device changed hands) is deleted with its messages and
	// created anew with s. It fails with domain.ErrConflict if the account
	// does not exist or s.ID is another endpoint's.
	Save(ctx context.Context, s domain.PushSubscription) error
	// ListByAccount returns the subscriptions of an account ordered by
	// CreatedAt and ID.
	ListByAccount(ctx context.Context, account domain.AccountID) ([]domain.PushSubscription, error)
	// Delete deletes subscription id with its messages; one that does not
	// exist is domain.ErrNotFound.
	Delete(ctx context.Context, id domain.PushSubscriptionID) error
	// DeleteByAccount deletes every subscription of the account with their
	// messages and returns how many it deleted (all its sessions ended).
	DeleteByAccount(ctx context.Context, account domain.AccountID) (int, error)
	// DeleteByEndpoint deletes the account's subscription of the endpoint
	// with its messages, if it has one, and returns how many it deleted (0
	// or 1).
	DeleteByEndpoint(ctx context.Context, account domain.AccountID, endpoint string) (int, error)
	// Touch sets the LastUsedAt of subscription id to the instant; one that
	// does not exist is domain.ErrNotFound.
	Touch(ctx context.Context, id domain.PushSubscriptionID, at time.Time) error
	// DeleteUnusedBefore deletes, with their messages, the subscriptions
	// last used before the instant and returns how many it deleted.
	DeleteUnusedBefore(ctx context.Context, before time.Time) (int, error)
}

// PushOutboxRepo stores outgoing web push messages (ADR 0017).
type PushOutboxRepo interface {
	Get(ctx context.Context, id domain.PushMessageID) (domain.PushMessage, error)
	// Create fails with domain.ErrConflict if the ID is taken or the
	// subscription does not exist.
	Create(ctx context.Context, m domain.PushMessage) error
	// Update changes every field but the subscription and CreatedAt.
	Update(ctx context.Context, m domain.PushMessage) error
	// ListDue returns at most limit pending messages whose next attempt is
	// due at now, ordered by next attempt and ID. Like OutboxRepo.ListDue
	// it needs a ReadWrite transaction and skips messages that another
	// transaction has locked.
	ListDue(ctx context.Context, now time.Time, limit int) ([]domain.PushMessage, error)
	// DeleteCreatedBefore deletes every message created before the
	// instant, whatever its state, and returns how many it deleted.
	DeleteCreatedBefore(ctx context.Context, before time.Time) (int, error)
}

// OutboxCounts are the numbers of unsent mails by status.
type OutboxCounts struct {
	Pending int // domain.MailPending: another attempt is due
	Failed  int // domain.MailFailed: delivery was given up
}

// OutboxFilter selects mails for OutboxRepo.List. Its zero value selects
// none (Limit 0).
type OutboxFilter struct {
	// Status keeps the mails in that state (domain.OutboxMail.Status);
	// empty keeps all.
	Status domain.MailStatus
	// Template keeps the mails of that template; empty keeps all.
	Template string
	// Recipient keeps the mails whose recipient contains it, ignoring ASCII
	// case; empty keeps all.
	Recipient string
	// After is the cursor: a non-zero After keeps only the mails that come
	// after the mail with this creation time and ID in the order of List.
	AfterCreatedAt time.Time
	AfterID        domain.OutboxMailID
	// Limit returns at most that many mails (zero or less: none).
	Limit int
}

// AuditRepo stores audit events. Events are only ever added, and deleted
// by the retention job (ADR 0016).
type AuditRepo interface {
	// Create fails with domain.ErrConflict if the ID is taken or the actor,
	// the target account or the target person does not exist.
	Create(ctx context.Context, e domain.AuditEvent) error
	// List returns at most limit events, newest first: ordered by time and
	// ID, both descending.
	List(ctx context.Context, limit int) ([]domain.AuditEvent, error)
	// DeleteBefore deletes every event that happened before the instant and
	// returns how many it deleted. It needs a ReadWrite transaction.
	DeleteBefore(ctx context.Context, before time.Time) (int, error)
}

// AnnouncementRepo stores announcements (ADR 0014), the accounts'
// dismissals of them, and their mailings. An announcement goes with its
// lesson (LessonRepo.Delete), its dismissals go with it, and its mailings
// stay without it.
type AnnouncementRepo interface {
	Get(ctx context.Context, id domain.AnnouncementID) (domain.Announcement, error)
	// Create fails with domain.ErrConflict if the ID is taken or the
	// author, class, course type or lesson does not exist.
	Create(ctx context.Context, a domain.Announcement) error
	// Update writes everything the office edits: every field but the mail
	// state (EmailedAt, EmailRecipients, MailCursor), which only
	// SetMailState writes, so an edit never undoes a mailing. It fails
	// under the same rules as Create.
	Update(ctx context.Context, a domain.Announcement) error
	// SetMailState writes the mail state of a (EmailedAt, EmailRecipients,
	// MailCursor) and nothing else.
	SetMailState(ctx context.Context, a domain.Announcement) error
	// Lock returns the announcement and locks it until the transaction ends
	// (in PostgreSQL: SELECT … FOR UPDATE), against a concurrent mailing.
	// It needs a ReadWrite transaction and fails with ErrReadOnly
	// otherwise.
	Lock(ctx context.Context, id domain.AnnouncementID) (domain.Announcement, error)
	Delete(ctx context.Context, id domain.AnnouncementID) error
	// List returns all announcements, newest first: ordered by PublishAt
	// and ID, both descending.
	List(ctx context.Context) ([]domain.Announcement, error)
	// ListShown returns the announcements shown at now (published at or
	// before it, not expired at it), ordered like List.
	ListShown(ctx context.Context, now time.Time) ([]domain.Announcement, error)
	// ListDueForMail returns at most limit announcements to be emailed
	// whose PublishAt is at or before now and that are not emailed yet
	// (being sent included), ordered by PublishAt and ID. Like OutboxRepo.ListDue it needs a
	// ReadWrite transaction and skips those another transaction has locked.
	ListDueForMail(ctx context.Context, now time.Time, limit int) ([]domain.Announcement, error)
	// CountScheduledForMail counts the announcements to be emailed and not
	// emailed yet (being sent included) with from <= PublishAt < to, except the announcement
	// except (none if zero).
	CountScheduledForMail(ctx context.Context, from, to time.Time, except domain.AnnouncementID) (int, error)
	// LockMail serializes the transactions that check or use the daily
	// limit of emailed announcements, until the transaction ends. It needs
	// a ReadWrite transaction and fails with ErrReadOnly otherwise.
	LockMail(ctx context.Context) error

	// Dismiss records a dismissal unless the account dismissed the
	// announcement already (then it keeps the first). It fails with
	// domain.ErrConflict if the announcement or the account does not exist.
	Dismiss(ctx context.Context, d domain.AnnouncementDismissal) error
	// ListDismissed returns those of the given announcements that the
	// account dismissed, each once and ordered by ID.
	ListDismissed(
		ctx context.Context, account domain.AccountID, announcements []domain.AnnouncementID,
	) ([]domain.AnnouncementID, error)

	// CreateMailing fails with domain.ErrConflict if the ID is taken or the
	// announcement or the sender does not exist.
	CreateMailing(ctx context.Context, m domain.AnnouncementMailing) error
	// ListMailings returns the mailings with from <= SentAt < to, ordered by
	// SentAt and ID.
	ListMailings(ctx context.Context, from, to time.Time) ([]domain.AnnouncementMailing, error)
}

// ImportRunRepo stores the history of applied member imports (ADR 0015).
// Runs are only ever added, and deleted by the retention job (ADR 0016).
type ImportRunRepo interface {
	// Create fails with domain.ErrConflict if the ID is taken or the actor
	// does not exist.
	Create(ctx context.Context, r domain.ImportRun) error
	// List returns at most limit runs, newest first: ordered by StartedAt
	// and ID, both descending.
	List(ctx context.Context, limit int) ([]domain.ImportRun, error)
	// DeleteStartedBefore deletes every run that started before the instant
	// and returns how many it deleted. It needs a ReadWrite transaction.
	DeleteStartedBefore(ctx context.Context, before time.Time) (int, error)
}

// DuplicateDismissalRepo stores the pairs of persons the office found not
// to be duplicates (ADR 0015). A dismissal goes with either person
// (PersonRepo.Delete).
type DuplicateDismissalRepo interface {
	// Create records a dismissal unless the pair has one (then it keeps the
	// first). It fails with domain.ErrConflict if a person or the account
	// does not exist.
	Create(ctx context.Context, d domain.DuplicateDismissal) error
	// List returns every dismissal, ordered by PersonA and PersonB.
	List(ctx context.Context) ([]domain.DuplicateDismissal, error)
	// ListByPerson returns the dismissals of the pairs the person is in,
	// ordered like List.
	ListByPerson(ctx context.Context, person domain.PersonID) ([]domain.DuplicateDismissal, error)
	// Delete removes the dismissal of the pair a and b, in either order; a
	// pair without one is domain.ErrNotFound.
	Delete(ctx context.Context, a, b domain.PersonID) error
}

// JobRunRepo stores the runs of the background jobs (ADR 0020). Runs are
// only ever added, and deleted by the retention job.
type JobRunRepo interface {
	// Create fails with domain.ErrConflict if the ID is taken.
	Create(ctx context.Context, r domain.JobRun) error
	// Latest returns the latest run of each job that has one, the run with
	// the latest StartedAt and then the greatest ID, ordered by Job.
	Latest(ctx context.Context) ([]domain.JobRun, error)
	// DeleteStartedBefore deletes every run that started before the instant
	// and returns how many it deleted. It needs a ReadWrite transaction.
	DeleteStartedBefore(ctx context.Context, before time.Time) (int, error)
}

// EventRepo stores events (ADR 0019) and their images. Slugs are unique,
// exactly (they are lowercase already). An event's sessions are lessons
// (LessonRepo.ListByEvent), its registrations EventRegistrationRepo's.
type EventRepo interface {
	Get(ctx context.Context, id domain.EventID) (domain.Event, error)
	// BySlug finds the event with the slug, or domain.ErrNotFound.
	BySlug(ctx context.Context, slug string) (domain.Event, error)
	// Create fails with domain.ErrDuplicate if another event has the slug,
	// and with domain.ErrConflict (but not ErrDuplicate) if the ID is taken
	// or the course type or the creating account does not exist.
	Create(ctx context.Context, e domain.Event) error
	// Update fails under the same rules as Create.
	Update(ctx context.Context, e domain.Event) error
	// Delete deletes an event with its image. It fails with domain.ErrInUse
	// (a domain.ErrConflict) while lessons (its sessions) or registrations,
	// canceled ones included, refer to it, and changes nothing then.
	Delete(ctx context.Context, id domain.EventID) error
	// List returns all events ordered by the start of their first session
	// (canceled sessions included), events without a session last, then by
	// ID.
	List(ctx context.Context) ([]domain.Event, error)
	// ListByIDs returns the events with the given IDs, each once and
	// ordered by ID. IDs that do not exist are left out.
	ListByIDs(ctx context.Context, ids []domain.EventID) ([]domain.Event, error)
	// Lock returns the event and locks it until the transaction ends (in
	// PostgreSQL: SELECT … FOR UPDATE). Every capacity check and
	// registration change of an event runs under this lock, before any lock
	// of its lessons (event → lessons → passes). It needs a ReadWrite
	// transaction and fails with ErrReadOnly otherwise.
	Lock(ctx context.Context, id domain.EventID) (domain.Event, error)

	// Image returns the event's image (stored like the school's background,
	// ADR 0018), the zero Background if it has none, or domain.ErrNotFound
	// if the event does not exist. It is apart from Get, which lists read.
	Image(ctx context.Context, id domain.EventID) (domain.Background, error)
	// PutImage replaces the event's image; the zero Background removes it.
	// It fails with domain.ErrNotFound if the event does not exist.
	PutImage(ctx context.Context, id domain.EventID, img domain.Background) error
	// ImageInfo returns the content type and the digest of the event's
	// image (domain.Background.Digest, kept when it is written) without
	// reading it, the zero BackgroundInfo without one, or
	// domain.ErrNotFound if the event does not exist.
	ImageInfo(ctx context.Context, id domain.EventID) (domain.BackgroundInfo, error)
}

// EventRegistrationRepo stores the registrations for events (ADR 0019). A
// person has at most one open (waiting, invited or registered) registration
// per event; an invite token hash, a receipt and a Storno belong to one
// registration each.
type EventRegistrationRepo interface {
	Get(ctx context.Context, id domain.EventRegistrationID) (domain.EventRegistration, error)
	// GetByInviteTokenHash finds the registration an invite token was sent
	// for. Only an invited registration has one, so the zero hash finds
	// none.
	GetByInviteTokenHash(ctx context.Context, hash [32]byte) (domain.EventRegistration, error)
	// GetByReceipt finds the registration whose payment the receipt
	// confirms (ReceiptID), or domain.ErrNotFound.
	GetByReceipt(ctx context.Context, receipt domain.ReceiptID) (domain.EventRegistration, error)
	// Create fails with domain.ErrDuplicate if the person has an open
	// registration for the event, and with domain.ErrConflict (but not
	// ErrDuplicate) if the ID, the token hash, the receipt or the Storno is
	// taken or a referenced record (event, person, receipt, account) does
	// not exist.
	Create(ctx context.Context, r domain.EventRegistration) error
	// Update fails under the same rules as Create.
	Update(ctx context.Context, r domain.EventRegistration) error
	// ListByEvent returns the registrations of an event, canceled ones
	// included, ordered by CreatedAt and ID: the order of its waiting list.
	ListByEvent(ctx context.Context, event domain.EventID) ([]domain.EventRegistration, error)
	// ListByPersons returns the registrations of the given persons, canceled
	// ones included, ordered by CreatedAt and ID.
	ListByPersons(ctx context.Context, persons []domain.PersonID) ([]domain.EventRegistration, error)
	// ListByReceipts returns the registrations whose payment one of the
	// receipts confirms (ReceiptID), ordered by CreatedAt and ID.
	ListByReceipts(ctx context.Context, receipts []domain.ReceiptID) ([]domain.EventRegistration, error)
	// CountsByEvents returns, by event, the open registrations of each of
	// the given events at the instant; events whose counts are all zero
	// are left out.
	CountsByEvents(
		ctx context.Context, events []domain.EventID, at time.Time,
	) (map[domain.EventID]EventRegistrationCounts, error)
	// AnonymizeGuests replaces the name of every guest registration of an
	// event whose last session ended before the instant with
	// domain.AnonymousGuestName and clears its email (ADR 0016, ADR 0019),
	// canceled ones included, and returns how many it changed; one
	// anonymized already is not changed again, and an event without
	// sessions keeps its guests. It needs a ReadWrite transaction.
	AnonymizeGuests(ctx context.Context, endedBefore time.Time) (int, error)
}

// EventRegistrationCounts counts the open registrations of an event at an
// instant (EventRegistrationRepo.CountsByEvents). The event is full when
// Registered plus Invited reach its capacity.
type EventRegistrationCounts struct {
	// Registered counts the registrations with a place.
	Registered int
	// Invited counts the invites still open at the instant (ExpiresAt after
	// it): each holds a place.
	Invited int
	// Waiting counts the registrations waiting for a place.
	Waiting int
}
