// Package memory provides an in-memory store.Store, used for tests, the demo
// server and the WebAssembly build.
//
// All transactions are serialised by a single mutex, so there is no
// concurrency inside the store and Lessons().Lock needs no lock of its own.
// A transaction works on a copy of the state whose tables are cloned on their
// first write (copy-on-write). On commit the copy replaces the state; on error
// or panic it is dropped. Read-only transactions never clone anything.
//
// The store enforces the same keys, references and delete restrictions as the
// PostgreSQL schema, so both behave identically under the storetest contract.
package memory

import (
	"context"
	"maps"
	"sync"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// Store is an in-memory implementation of store.Store. The zero value is not
// usable; create one with New.
type Store struct {
	mu    sync.Mutex
	gen   uint64 // incremented per read-write transaction
	state state
}

// state is the complete data set. Tables are copied on write, so copying a
// state is cheap.
type state struct {
	settings *domain.Settings

	persons       table[domain.PersonID, domain.Person]
	guardians     table[domain.Guardian, struct{}]
	accounts      table[domain.AccountID, domain.Account]
	credentials   table[domain.AccountID, domain.Credential]
	sessions      table[domain.SessionID, domain.Session]
	accountTokens table[[32]byte, domain.AccountToken]
	locations     table[domain.LocationID, domain.Location]
	rooms         table[domain.RoomID, domain.Room]
	staff         table[domain.StaffID, domain.Staff]
	courseTypes   table[domain.CourseTypeID, domain.CourseType]
	terms         table[domain.TermID, domain.Term]
	classes       table[domain.ClassID, domain.Class]
	classMembers  table[memberKey, domain.ClassMember]
	lessons       table[domain.LessonID, domain.Lesson]
	bookings      table[domain.BookingID, domain.Booking]
	bookingEvents table[domain.BookingEventID, domain.BookingEvent]
	outbox        table[domain.OutboxMailID, domain.OutboxMail]
	audit         table[domain.AuditEventID, domain.AuditEvent]
}

type memberKey struct {
	class  domain.ClassID
	person domain.PersonID
}

// table is a copy-on-write map. The transaction with generation g clones the
// map on its first write; later writes of the same transaction reuse the
// clone. Values are plain structs without references, so a shallow clone
// isolates them.
type table[K comparable, V any] struct {
	rows map[K]V
	gen  uint64 // generation of the transaction that owns rows
}

func (t *table[K, V]) get(k K) (V, bool) {
	v, ok := t.rows[k]

	return v, ok
}

func (t *table[K, V]) has(k K) bool {
	_, ok := t.rows[k]

	return ok
}

func (t *table[K, V]) own(gen uint64) {
	if t.gen == gen && t.rows != nil {
		return
	}

	rows := maps.Clone(t.rows)
	if rows == nil {
		rows = make(map[K]V)
	}

	t.rows, t.gen = rows, gen
}

func (t *table[K, V]) put(gen uint64, k K, v V) {
	t.own(gen)
	t.rows[k] = v
}

func (t *table[K, V]) delete(gen uint64, k K) {
	t.own(gen)
	delete(t.rows, k)
}

// exists reports whether some row satisfies pred.
func (t *table[K, V]) exists(pred func(V) bool) bool {
	for _, v := range t.rows {
		if pred(v) {
			return true
		}
	}

	return false
}

// filter returns the rows that satisfy pred, in no particular order.
func (t *table[K, V]) filter(pred func(V) bool) []V {
	var out []V

	for _, v := range t.rows {
		if pred(v) {
			out = append(out, v)
		}
	}

	return out
}

var _ store.Store = (*Store)(nil)

// New returns an empty store.
func New() *Store {
	return &Store{}
}

type txKey struct{}

// InTx implements store.Store.
func (s *Store) InTx(ctx context.Context, mode store.TxMode, fn func(ctx context.Context, tx store.Tx) error) error {
	if ctx.Value(txKey{}) == s {
		return store.ErrNestedTx
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// The request may have been canceled while waiting for the lock.
	if err := ctx.Err(); err != nil {
		return err
	}

	t := &tx{state: s.state, mode: mode}
	if mode == store.ReadWrite {
		s.gen++
		t.gen = s.gen
	}

	if err := fn(context.WithValue(ctx, txKey{}, s), t); err != nil {
		return err
	}

	// Like a PostgreSQL commit, a canceled context fails the transaction.
	if err := ctx.Err(); err != nil {
		return err
	}

	if mode == store.ReadWrite {
		s.state = t.state
	}

	return nil
}

type tx struct {
	state state
	mode  store.TxMode
	gen   uint64
}

func (t *tx) Settings() store.SettingsRepo      { return settingsRepo{t} }
func (t *tx) Persons() store.PersonRepo         { return personRepo{t} }
func (t *tx) Accounts() store.AccountRepo       { return accountRepo{t} }
func (t *tx) Credentials() store.CredentialRepo { return credentialRepo{t} }
func (t *tx) Sessions() store.SessionRepo       { return sessionRepo{t} }
func (t *tx) AccountTokens() store.AccountTokenRepo {
	return accountTokenRepo{t}
}
func (t *tx) Locations() store.LocationRepo     { return locationRepo{t} }
func (t *tx) Rooms() store.RoomRepo             { return roomRepo{t} }
func (t *tx) Staff() store.StaffRepo            { return staffRepo{t} }
func (t *tx) CourseTypes() store.CourseTypeRepo { return courseTypeRepo{t} }
func (t *tx) Terms() store.TermRepo             { return termRepo{t} }
func (t *tx) Classes() store.ClassRepo          { return classRepo{t} }
func (t *tx) Lessons() store.LessonRepo         { return lessonRepo{t} }
func (t *tx) Bookings() store.BookingRepo       { return bookingRepo{t} }
func (t *tx) Outbox() store.OutboxRepo          { return outboxRepo{t} }
func (t *tx) Audit() store.AuditRepo            { return auditRepo{t} }

func (t *tx) checkWritable() error {
	if t.mode != store.ReadWrite {
		return store.ErrReadOnly
	}

	return nil
}

// instant normalises a time the way PostgreSQL stores a timestamptz: UTC
// with microsecond precision.
func instant(t time.Time) time.Time {
	if t.IsZero() {
		return time.Time{}
	}

	return t.UTC().Truncate(time.Microsecond)
}

type settingsRepo struct{ tx *tx }

func (r settingsRepo) Get(context.Context) (domain.Settings, error) {
	if r.tx.state.settings == nil {
		return domain.Settings{}, notFound("settings")
	}

	return *r.tx.state.settings, nil
}

func (r settingsRepo) Put(_ context.Context, s domain.Settings) error {
	if err := r.tx.checkWritable(); err != nil {
		return err
	}

	r.tx.state.settings = &s

	return nil
}
