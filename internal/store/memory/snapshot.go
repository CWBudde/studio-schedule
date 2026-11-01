package memory

import (
	"bytes"
	"crypto/sha256"
	"encoding"
	"encoding/hex"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
)

// A snapshot is the complete data of a store as JSON. The web demo saves one
// in the browser after each change and restores it on the next page load
// (docs/demo-mode.md).
//
// JSON rather than encoding/gob: the WebAssembly build links encoding/json
// anyway (the API speaks it), so snapshots add no code to the binary, and the
// domain types already round-trip through their text marshalers, except that
// an unset (zero) ID is not a valid ID; jsonOptions handles that. Each table
// is written as a list of its rows; the keys are derived again on restore.
//
// A snapshot fits only the build that wrote it. Its header names the build
// and the layout of the store (a fingerprint of the Go types of all tables),
// and Restore refuses any other with ErrSnapshotMismatch: the demo then
// starts from the seed instead of misreading old data.

// ErrSnapshotMismatch reports a snapshot written by another build, or for
// another layout of the store.
var ErrSnapshotMismatch = errors.New("memory: snapshot from another version")

// snapshotFormat is the version of the snapshot's own structure.
const snapshotFormat = 1

// The document is written and read in one pass, in this order:
//
//	{"format": 1, "layout": "…", "build": "…", "tables": {"settings": {…}, "persons": [rows…], …}}
//
// The header comes first, so a snapshot of another version is refused before
// any table is decoded.

// jsonOptions make every row survive the round trip unchanged: nil slices
// and maps stay nil, a duration is its number of nanoseconds, and an unset
// ID, which domain.ID refuses to parse, decodes to the zero ID again.
var jsonOptions = json.JoinOptions(
	json.FormatNilSliceAsNull(true),
	json.FormatNilMapAsNull(true),
	json.WithMarshalers(json.MarshalToFunc(func(enc *jsontext.Encoder, d time.Duration) error {
		return enc.WriteToken(jsontext.Int(int64(d)))
	})),
	json.WithUnmarshalers(json.JoinUnmarshalers(json.UnmarshalFromFunc(func(dec *jsontext.Decoder, d *time.Duration) error {
		tok, err := dec.ReadToken()
		if err != nil {
			return err
		}

		if tok.Kind() != '0' {
			return fmt.Errorf("a duration must be a number, not %v", tok.Kind())
		}

		n, err := tok.Int()
		if err != nil {
			return err
		}

		*d = time.Duration(n)

		return nil
	}), json.UnmarshalFromFunc(func(dec *jsontext.Decoder, v encoding.TextUnmarshaler) error {
		if !isID(reflect.TypeOf(v).Elem()) {
			return errors.ErrUnsupported // the default: UnmarshalText
		}

		tok, err := dec.ReadToken()
		if err != nil {
			return err
		}

		if tok.Kind() != '"' {
			return fmt.Errorf("an ID must be a string, not %v", tok.Kind())
		}

		if tok.String() == nilID {
			reflect.ValueOf(v).Elem().SetZero()

			return nil
		}

		return v.UnmarshalText([]byte(tok.String()))
	}))),
)

const nilID = "00000000-0000-0000-0000-000000000000"

var idType = reflect.TypeFor[domain.PersonID]()

// isID reports whether t is one of the typed IDs (an instance of domain.ID).
func isID(t reflect.Type) bool {
	return t.PkgPath() == idType.PkgPath() && strings.HasPrefix(t.Name(), "ID[")
}

// codec writes and reads one field of state, and tells whether a
// transaction changed it. Every field of state needs one; a test enforces
// this.
type codec struct {
	field   string
	encode  func(*jsontext.Encoder, *state) error
	decode  func(*jsontext.Decoder, *state) error
	changed func(before, after *state) bool
}

// rows is the codec of a table whose key is derived from the row.
func rows[K comparable, V any](field string, tbl func(*state) *table[K, V], key func(V) K) codec {
	return codec{
		field: field,
		encode: func(enc *jsontext.Encoder, s *state) error {
			if err := enc.WriteToken(jsontext.BeginArray); err != nil {
				return err
			}

			for _, v := range tbl(s).rows {
				if err := json.MarshalEncode(enc, v, jsonOptions); err != nil {
					return err
				}
			}

			return enc.WriteToken(jsontext.EndArray)
		},
		decode: func(dec *jsontext.Decoder, s *state) error {
			var in []V
			if err := json.UnmarshalDecode(dec, &in, jsonOptions); err != nil {
				return err
			}

			m := make(map[K]V, len(in))
			for _, v := range in {
				k := key(v)
				if _, dup := m[k]; dup {
					return fmt.Errorf("duplicate key %v", k)
				}

				m[k] = v
			}

			*tbl(s) = table[K, V]{rows: m}

			return nil
		},
		changed: tableChanged(tbl),
	}
}

// derived is the codec of an index derived from other tables (reindex
// builds it): the snapshot writes null for it, and Restore ignores what it
// finds there.
func derived[K comparable, V any](field string, tbl func(*state) *table[K, V]) codec {
	return codec{
		field:   field,
		encode:  func(enc *jsontext.Encoder, _ *state) error { return enc.WriteToken(jsontext.Null) },
		decode:  func(dec *jsontext.Decoder, _ *state) error { return dec.SkipValue() },
		changed: tableChanged(tbl),
	}
}

// transient is the codec of a table the snapshot leaves out: the job runs,
// which the demo's jobs add every minute. Saving them would save the demo
// every minute and let its saved data grow by thousands of rows a day, for
// a history nobody reads in the browser. The snapshot writes null for the
// table, Restore leaves it empty, and a transaction that writes only to it
// changes nothing worth saving (OnCommit is not called).
func transient(field string) codec {
	return codec{
		field:   field,
		encode:  func(enc *jsontext.Encoder, _ *state) error { return enc.WriteToken(jsontext.Null) },
		decode:  func(dec *jsontext.Decoder, _ *state) error { return dec.SkipValue() },
		changed: func(_, _ *state) bool { return false },
	}
}

// reindex builds the indexes derived from the tables of a restored state.
func reindex(st *state) {
	st.memberNos = table[string, domain.PersonID]{rows: map[string]domain.PersonID{}}

	for _, p := range st.persons.rows {
		if p.MemberNo != "" {
			st.memberNos.rows[p.MemberNo] = p.ID
		}
	}
}

// keys is the codec of a table that is a set of keys.
func keys[K comparable](field string, tbl func(*state) *table[K, struct{}]) codec {
	return codec{
		field: field,
		encode: func(enc *jsontext.Encoder, s *state) error {
			if err := enc.WriteToken(jsontext.BeginArray); err != nil {
				return err
			}

			for k := range tbl(s).rows {
				if err := json.MarshalEncode(enc, k, jsonOptions); err != nil {
					return err
				}
			}

			return enc.WriteToken(jsontext.EndArray)
		},
		decode: func(dec *jsontext.Decoder, s *state) error {
			var in []K
			if err := json.UnmarshalDecode(dec, &in, jsonOptions); err != nil {
				return err
			}

			m := make(map[K]struct{}, len(in))
			for _, k := range in {
				if _, dup := m[k]; dup {
					return fmt.Errorf("duplicate key %v", k)
				}

				m[k] = struct{}{}
			}

			*tbl(s) = table[K, struct{}]{rows: m}

			return nil
		},
		changed: tableChanged(tbl),
	}
}

// tableChanged reports a write to the table: the transaction's first write
// copies the table and marks it with its own generation.
func tableChanged[K comparable, V any](tbl func(*state) *table[K, V]) func(before, after *state) bool {
	return func(before, after *state) bool { return tbl(before).gen != tbl(after).gen }
}

var codecs = []codec{
	{
		field: "settings",
		encode: func(enc *jsontext.Encoder, s *state) error {
			return json.MarshalEncode(enc, s.settings, jsonOptions)
		},
		decode: func(dec *jsontext.Decoder, s *state) error {
			return json.UnmarshalDecode(dec, &s.settings, jsonOptions)
		},
		// Put stores a new pointer.
		changed: func(before, after *state) bool { return before.settings != after.settings },
	},
	{
		field: "logo",
		// The logo alone: its digest is derived again on restore.
		encode: func(enc *jsontext.Encoder, s *state) error {
			var l *domain.Logo
			if s.logo != nil {
				l = &s.logo.logo
			}

			return json.MarshalEncode(enc, l, jsonOptions)
		},
		decode: func(dec *jsontext.Decoder, s *state) error {
			var l *domain.Logo
			if err := json.UnmarshalDecode(dec, &l, jsonOptions); err != nil {
				return err
			}

			s.logo = nil
			if l != nil {
				s.logo = newStoredLogo(*l)
			}

			return nil
		},
		// PutLogo stores a new pointer (or nil).
		changed: func(before, after *state) bool { return before.logo != after.logo },
	},
	{
		field: "background",
		// The image alone: its digest is derived again on restore.
		encode: func(enc *jsontext.Encoder, s *state) error {
			var b *domain.Background
			if s.background != nil {
				b = &s.background.background
			}

			return json.MarshalEncode(enc, b, jsonOptions)
		},
		decode: func(dec *jsontext.Decoder, s *state) error {
			var b *domain.Background
			if err := json.UnmarshalDecode(dec, &b, jsonOptions); err != nil {
				return err
			}

			s.background = nil
			if b != nil {
				s.background = newStoredBackground(*b)
			}

			return nil
		},
		// PutBackground stores a new pointer (or nil).
		changed: func(before, after *state) bool { return before.background != after.background },
	},
	rows("persons", func(s *state) *table[domain.PersonID, domain.Person] { return &s.persons },
		func(v domain.Person) domain.PersonID { return v.ID }),
	derived("memberNos", func(s *state) *table[string, domain.PersonID] { return &s.memberNos }),
	keys("guardians", func(s *state) *table[domain.Guardian, struct{}] { return &s.guardians }),
	rows("accounts", func(s *state) *table[domain.AccountID, domain.Account] { return &s.accounts },
		func(v domain.Account) domain.AccountID { return v.ID }),
	rows("credentials", func(s *state) *table[domain.AccountID, domain.Credential] { return &s.credentials },
		func(v domain.Credential) domain.AccountID { return v.AccountID }),
	rows("sessions", func(s *state) *table[domain.SessionID, domain.Session] { return &s.sessions },
		func(v domain.Session) domain.SessionID { return v.ID }),
	rows("accountTokens", func(s *state) *table[[32]byte, domain.AccountToken] { return &s.accountTokens },
		func(v domain.AccountToken) [32]byte { return v.Hash }),
	rows("locations", func(s *state) *table[domain.LocationID, domain.Location] { return &s.locations },
		func(v domain.Location) domain.LocationID { return v.ID }),
	rows("rooms", func(s *state) *table[domain.RoomID, domain.Room] { return &s.rooms },
		func(v domain.Room) domain.RoomID { return v.ID }),
	rows("staff", func(s *state) *table[domain.StaffID, domain.Staff] { return &s.staff },
		func(v domain.Staff) domain.StaffID { return v.ID }),
	rows("courseTypes", func(s *state) *table[domain.CourseTypeID, domain.CourseType] { return &s.courseTypes },
		func(v domain.CourseType) domain.CourseTypeID { return v.ID }),
	rows("terms", func(s *state) *table[domain.TermID, domain.Term] { return &s.terms },
		func(v domain.Term) domain.TermID { return v.ID }),
	rows("classes", func(s *state) *table[domain.ClassID, domain.Class] { return &s.classes },
		func(v domain.Class) domain.ClassID { return v.ID }),
	rows("classMembers", func(s *state) *table[memberKey, domain.ClassMember] { return &s.classMembers },
		func(v domain.ClassMember) memberKey { return memberKey{v.ClassID, v.PersonID} }),
	rows("lessons", func(s *state) *table[domain.LessonID, domain.Lesson] { return &s.lessons },
		func(v domain.Lesson) domain.LessonID { return v.ID }),
	rows("bookings", func(s *state) *table[domain.BookingID, domain.Booking] { return &s.bookings },
		func(v domain.Booking) domain.BookingID { return v.ID }),
	rows("bookingEvents", func(s *state) *table[domain.BookingEventID, domain.BookingEvent] { return &s.bookingEvents },
		func(v domain.BookingEvent) domain.BookingEventID { return v.ID }),
	rows("waitlist", func(s *state) *table[domain.WaitlistEntryID, domain.WaitlistEntry] { return &s.waitlist },
		func(v domain.WaitlistEntry) domain.WaitlistEntryID { return v.ID }),
	rows("notices", func(s *state) *table[domain.LessonNoticeID, domain.LessonNotice] { return &s.notices },
		func(v domain.LessonNotice) domain.LessonNoticeID { return v.ID }),
	rows("calendarFeeds", func(s *state) *table[domain.CalendarFeedID, domain.CalendarFeed] { return &s.calendarFeeds },
		func(v domain.CalendarFeed) domain.CalendarFeedID { return v.ID }),
	rows("passTypes", func(s *state) *table[domain.PassTypeID, domain.PassType] { return &s.passTypes },
		func(v domain.PassType) domain.PassTypeID { return v.ID }),
	rows("passes", func(s *state) *table[domain.PassID, domain.Pass] { return &s.passes },
		func(v domain.Pass) domain.PassID { return v.ID }),
	rows("redemptions", func(s *state) *table[domain.BookingID, domain.PassRedemption] { return &s.redemptions },
		func(v domain.PassRedemption) domain.BookingID { return v.BookingID }),
	rows("receipts", func(s *state) *table[domain.ReceiptID, domain.Receipt] { return &s.receipts },
		func(v domain.Receipt) domain.ReceiptID { return v.ID }),
	rows("receiptPDFs", func(s *state) *table[domain.ReceiptID, receiptPDF] { return &s.receiptPDFs },
		func(v receiptPDF) domain.ReceiptID { return v.ReceiptID }),
	rows("receiptCounters", func(s *state) *table[int, receiptCounter] { return &s.receiptCounters },
		func(v receiptCounter) int { return v.Year }),
	rows("outbox", func(s *state) *table[domain.OutboxMailID, domain.OutboxMail] { return &s.outbox },
		func(v domain.OutboxMail) domain.OutboxMailID { return v.ID }),
	rows("audit", func(s *state) *table[domain.AuditEventID, domain.AuditEvent] { return &s.audit },
		func(v domain.AuditEvent) domain.AuditEventID { return v.ID }),
	rows("announcements", func(s *state) *table[domain.AnnouncementID, domain.Announcement] { return &s.announcements },
		func(v domain.Announcement) domain.AnnouncementID { return v.ID }),
	rows("announcementDismissed",
		func(s *state) *table[dismissalKey, domain.AnnouncementDismissal] { return &s.announcementDismissed },
		func(v domain.AnnouncementDismissal) dismissalKey { return dismissalKey{v.AnnouncementID, v.AccountID} }),
	rows("announcementMailings",
		func(s *state) *table[domain.AnnouncementMailingID, domain.AnnouncementMailing] {
			return &s.announcementMailings
		},
		func(v domain.AnnouncementMailing) domain.AnnouncementMailingID { return v.ID }),
	rows("importRuns", func(s *state) *table[domain.ImportRunID, domain.ImportRun] { return &s.importRuns },
		func(v domain.ImportRun) domain.ImportRunID { return v.ID }),
	rows("dismissals", func(s *state) *table[pairKey, domain.DuplicateDismissal] { return &s.dismissals },
		func(v domain.DuplicateDismissal) pairKey { return pairKey{v.PersonA, v.PersonB} }),
	rows("pushSubs",
		func(s *state) *table[domain.PushSubscriptionID, domain.PushSubscription] { return &s.pushSubs },
		func(v domain.PushSubscription) domain.PushSubscriptionID { return v.ID }),
	rows("pushOutbox", func(s *state) *table[domain.PushMessageID, domain.PushMessage] { return &s.pushOutbox },
		func(v domain.PushMessage) domain.PushMessageID { return v.ID }),
	transient("jobRuns"),
	rows("events", func(s *state) *table[domain.EventID, domain.Event] { return &s.events },
		func(v domain.Event) domain.EventID { return v.ID }),
	rows("eventImages", func(s *state) *table[domain.EventID, eventImage] { return &s.eventImages },
		func(v eventImage) domain.EventID { return v.EventID }),
	rows("eventRegistrations",
		func(s *state) *table[domain.EventRegistrationID, domain.EventRegistration] {
			return &s.eventRegistrations
		},
		func(v domain.EventRegistration) domain.EventRegistrationID { return v.ID }),
}

// OnCommit registers fn to run after every read-write transaction that
// changed data (the web demo saves its data then). fn runs while the store is
// locked: it must return quickly and must not use the store.
func (s *Store) OnCommit(fn func()) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.onCommit = fn
}

// changed reports whether a transaction that started from before and ended
// with after wrote anything.
func changed(before, after *state) bool {
	for _, c := range codecs {
		if c.changed(before, after) {
			return true
		}
	}

	return false
}

// Snapshot encodes the complete data of the store. build names the program
// version; Restore accepts the snapshot only for the same build.
func (s *Store) Snapshot(build string) ([]byte, error) {
	// Committed tables are never written again (a transaction copies a table
	// before its first write), so the encoding can run without the lock.
	s.mu.Lock()
	st := s.state
	s.mu.Unlock()

	var buf bytes.Buffer

	enc := jsontext.NewEncoder(&buf, jsonOptions)

	err := writeTokens(enc,
		jsontext.BeginObject,
		jsontext.String("format"), jsontext.Int(snapshotFormat),
		jsontext.String("layout"), jsontext.String(layout()),
		jsontext.String("build"), jsontext.String(build),
		jsontext.String("tables"), jsontext.BeginObject,
	)
	if err != nil {
		return nil, fmt.Errorf("memory: snapshot: %w", err)
	}

	for _, c := range codecs {
		if err := enc.WriteToken(jsontext.String(c.field)); err != nil {
			return nil, fmt.Errorf("memory: snapshot: %w", err)
		}

		if err := c.encode(enc, &st); err != nil {
			return nil, fmt.Errorf("memory: snapshot of %s: %w", c.field, err)
		}
	}

	if err := writeTokens(enc, jsontext.EndObject, jsontext.EndObject); err != nil {
		return nil, fmt.Errorf("memory: snapshot: %w", err)
	}

	return buf.Bytes(), nil
}

func writeTokens(enc *jsontext.Encoder, tokens ...jsontext.Token) error {
	for _, t := range tokens {
		if err := enc.WriteToken(t); err != nil {
			return err
		}
	}

	return nil
}

// Restore replaces the data of the store with a snapshot written by build.
// It fails with ErrSnapshotMismatch for a snapshot of another build or
// layout; on any error the store keeps its data.
func (s *Store) Restore(data []byte, build string) error {
	st, err := decodeSnapshot(jsontext.NewDecoder(bytes.NewReader(data), jsonOptions), build)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// The restored tables have generation 0, which no transaction has, so
	// the next write copies them.
	s.state = st

	return nil
}

func decodeSnapshot(dec *jsontext.Decoder, build string) (state, error) {
	var (
		st     state
		format int64
		lay    string
		built  string
		tables bool
	)

	if err := beginObject(dec); err != nil {
		return st, fmt.Errorf("memory: restore: %w", err)
	}

	for {
		tok, err := dec.ReadToken()
		if err != nil {
			return st, fmt.Errorf("memory: restore: %w", err)
		}

		if tok.Kind() == '}' {
			break
		}

		name := tok.String()

		switch name {
		case "format":
			err = json.UnmarshalDecode(dec, &format)
		case "layout":
			err = json.UnmarshalDecode(dec, &lay)
		case "build":
			err = json.UnmarshalDecode(dec, &built)
		case "tables":
			if format != snapshotFormat || lay != layout() || built != build {
				return st, fmt.Errorf("%w (format %d, build %q)", ErrSnapshotMismatch, format, built)
			}

			tables = true
			if err = decodeTables(dec, &st); err == nil {
				reindex(&st)
			}
		default:
			return st, fmt.Errorf("%w: unknown member %q", ErrSnapshotMismatch, name)
		}

		if err != nil {
			return st, fmt.Errorf("memory: restore: %w", err)
		}
	}

	if !tables {
		return st, fmt.Errorf("%w: no tables", ErrSnapshotMismatch)
	}

	return st, nil
}

func beginObject(dec *jsontext.Decoder) error {
	tok, err := dec.ReadToken()
	if err != nil {
		return err
	}

	if tok.Kind() != '{' {
		return fmt.Errorf("want an object, not %v", tok.Kind())
	}

	return nil
}

func decodeTables(dec *jsontext.Decoder, st *state) error {
	if err := beginObject(dec); err != nil {
		return fmt.Errorf("tables: %w", err)
	}

	byField := make(map[string]codec, len(codecs))
	for _, c := range codecs {
		byField[c.field] = c
	}

	for {
		tok, err := dec.ReadToken()
		if err != nil {
			return err
		}

		if tok.Kind() == '}' {
			break
		}

		c, ok := byField[tok.String()]
		if !ok {
			return fmt.Errorf("%w: unknown or repeated table %q", ErrSnapshotMismatch, tok.String())
		}

		delete(byField, c.field)

		if err := c.decode(dec, st); err != nil {
			return fmt.Errorf("%s: %w", c.field, err)
		}
	}

	if len(byField) > 0 {
		return fmt.Errorf("%w: no tables %v", ErrSnapshotMismatch, slices.Sorted(maps.Keys(byField)))
	}

	return nil
}

var layout = sync.OnceValue(func() string {
	var b strings.Builder

	describe(&b, reflect.TypeFor[state](), map[reflect.Type]bool{})

	sum := sha256.Sum256([]byte(b.String()))

	return hex.EncodeToString(sum[:8])
})

// describe writes the structure of t: field names, tags and types of every
// struct of this module it reaches. Types of other modules (time.Time, uuid)
// are described by name only.
func describe(b *strings.Builder, t reflect.Type, seen map[reflect.Type]bool) {
	if t.Name() != "" {
		b.WriteString(t.PkgPath() + "." + t.Name())

		if !strings.HasPrefix(t.PkgPath(), "github.com/cwbudde/studio-schedule/") || seen[t] {
			return
		}

		seen[t] = true
	}

	switch t.Kind() {
	case reflect.Pointer, reflect.Slice:
		b.WriteString("[" + t.Kind().String() + " ")
		describe(b, t.Elem(), seen)
		b.WriteString("]")
	case reflect.Array:
		fmt.Fprintf(b, "[%d]", t.Len())
		describe(b, t.Elem(), seen)
	case reflect.Map:
		b.WriteString("map[")
		describe(b, t.Key(), seen)
		b.WriteString("]")
		describe(b, t.Elem(), seen)
	case reflect.Struct:
		b.WriteString("{")

		for i := range t.NumField() {
			f := t.Field(i)
			fmt.Fprintf(b, "%s %q ", f.Name, f.Tag)
			describe(b, f.Type, seen)
			b.WriteString(";")
		}

		b.WriteString("}")
	default:
		b.WriteString(t.Kind().String())
	}
}
