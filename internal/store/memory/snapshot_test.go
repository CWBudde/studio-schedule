package memory

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"testing"
	"time"
	"unsafe"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/seed"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// stateFields returns the names of the fields of state: every table of the
// store, plus the settings.
func stateFields() []string {
	t := reflect.TypeFor[state]()
	names := make([]string, t.NumField())

	for i := range t.NumField() {
		names[i] = t.Field(i).Name
	}

	return names
}

// field returns field name of st as an accessible value (state's fields and
// the tables' rows are unexported).
func field(st *state, name string) reflect.Value {
	f := reflect.ValueOf(st).Elem().FieldByName(name)

	return reflect.NewAt(f.Type(), unsafe.Pointer(f.UnsafeAddr())).Elem()
}

// content is the data of field name: the settings, or the rows of a table
// without the generation of the transaction that wrote them.
func content(st *state, name string) reflect.Value {
	f := field(st, name)
	if f.Kind() == reflect.Struct {
		rows := f.FieldByName("rows")

		return reflect.NewAt(rows.Type(), unsafe.Pointer(rows.UnsafeAddr())).Elem()
	}

	return f
}

func empty(v reflect.Value) bool {
	if v.Kind() == reflect.Pointer {
		return v.IsNil()
	}

	return v.Len() == 0
}

// TestSnapshotCoversEveryTable fails when a table is added to the store
// without a codec, so it would be missing from the demo's saved data.
func TestSnapshotCoversEveryTable(t *testing.T) {
	t.Parallel()

	var codecNames []string
	for _, c := range codecs {
		codecNames = append(codecNames, c.field)
	}

	fields := stateFields()
	slices.Sort(fields)
	slices.Sort(codecNames)

	if !slices.Equal(fields, codecNames) {
		t.Fatalf("snapshot codecs %v do not match the store's tables %v", codecNames, fields)
	}
}

// fullStore holds the demo data plus a row in each table the seed leaves
// empty, so the round trip covers every table.
func fullStore(t *testing.T) *Store {
	t.Helper()

	ctx := t.Context()
	st := New()

	if err := seed.LoadDemo(ctx, st); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	src := ids.NewSequence()
	at := time.Date(2026, 9, 14, 17, 30, 0, 0, time.UTC)

	err := st.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		var (
			account domain.AccountID
			lesson  domain.LessonID
			person  domain.PersonID
		)

		// The first account and one-off lesson (the demo's class lessons
		// have member bookings), by ID, whose person is not booked into it
		// yet: the demo books persons into some one-off lessons too.
		accounts := sorted(st.state.accounts.filter(all), func(a, b domain.Account) int { return compareID(a.ID, b.ID) })
		oneOffs := sorted(st.state.lessons.filter(func(l domain.Lesson) bool { return l.ClassID.IsZero() }),
			func(a, b domain.Lesson) int { return compareID(a.ID, b.ID) })

	pick:
		for _, a := range accounts {
			for _, l := range oneOffs {
				if !st.state.bookings.exists(func(b domain.Booking) bool {
					return b.LessonID == l.ID && b.PersonID == a.PersonID
				}) {
					account, person, lesson = a.ID, a.PersonID, l.ID

					break pick
				}
			}
		}

		if lesson.IsZero() {
			return errors.New("no account and one-off lesson without a booking")
		}

		booking := domain.Booking{
			ID: ids.New[domain.BookingID](src), LessonID: lesson, PersonID: person,
			Status: domain.BookingBooked, Source: domain.BookingSourceSelf, BookedAt: at,
		}
		passType := domain.PassType{
			ID: ids.New[domain.PassTypeID](src), Name: "Snapshot pass", Kind: domain.PassVisits, Visits: 5,
			ValidityDays: 90, Price: 5000, VATRate: 700, Active: true, CreatedAt: at,
		}
		pass := domain.Pass{
			ID: ids.New[domain.PassID](src), PassTypeID: passType.ID, PersonID: person, Kind: domain.PassVisits,
			VisitsTotal: 5, VisitsRemaining: 4, ValidFrom: domain.DateOf(at, time.UTC), Status: domain.PassActive,
			Origin: domain.PassSold, SoldAt: at, SoldBy: account, PricePaid: 5000, Note: "snapshot",
		}

		seq, err := tx.ReceiptCounters().Next(ctx, 2099)
		if err != nil {
			return err
		}

		line := domain.NewReceiptLine(1, "Snapshot pass", 1, 5000, 700, domain.TaxStandard)
		receipt := domain.Receipt{
			ID: ids.New[domain.ReceiptID](src), Number: domain.ReceiptNumber("SNAP", 2099, seq), Year: 2099, Sequence: seq,
			Kind: domain.ReceiptSale, IssuedAt: at, IssueDate: domain.Date{Year: 2099, Month: 1, Day: 2}, Locale: "en",
			Issuer:  domain.ReceiptIssuer{Name: "Snapshot School", Address: "Street 1", TaxID: "1", Email: "s@example.org"},
			TaxMode: domain.TaxStandard, PayerID: person, RecipientName: "Snapshot Payer", PaymentMethod: domain.PaymentCard,
			Currency: "EUR", Net: line.Net, VAT: line.VAT, Gross: line.Gross, CreatedBy: account,
			Lines: []domain.ReceiptLine{line},
		}

		announcement := domain.Announcement{
			ID: ids.New[domain.AnnouncementID](src), AuthorID: account, Title: "Snapshot", Body: "Hello",
			Audience: domain.AudienceLesson, LessonID: lesson, PublishAt: at, ExpiresAt: at.Add(time.Hour),
			Email: true, EmailedAt: at, EmailRecipients: 2, CreatedAt: at, UpdatedAt: at,
		}

		return errors.Join(
			tx.Receipts().Create(ctx, receipt),
			tx.Receipts().PutPDF(ctx, receipt.ID, []byte("%PDF-1.7 snapshot")),
			tx.Settings().PutLogo(ctx, domain.Logo{ContentType: domain.LogoPNG, Data: []byte("\x89PNG\r\n\x1a\nx")}),
			tx.Sessions().Create(ctx, domain.Session{
				ID: ids.New[domain.SessionID](src), AccountID: account, TokenHash: [32]byte{1, 2, 3},
				CreatedAt: at, LastSeenAt: at, ExpiresAt: at.Add(time.Hour),
			}),
			tx.AccountTokens().Create(ctx, domain.AccountToken{
				Hash: [32]byte{4, 5, 6}, AccountID: account, Purpose: domain.TokenResetPassword,
				Email: "someone@example.org", CreatedAt: at, ExpiresAt: at.Add(time.Hour),
			}),
			tx.Bookings().Create(ctx, booking),
			tx.Bookings().AddEvent(ctx, domain.BookingEvent{
				ID: ids.New[domain.BookingEventID](src), BookingID: booking.ID, At: at,
				Status: domain.BookingBooked, ActorAccountID: account, Note: "booked",
			}),
			tx.PassTypes().Create(ctx, passType),
			tx.Passes().Create(ctx, pass),
			tx.Redemptions().Create(ctx, domain.PassRedemption{PassID: pass.ID, BookingID: booking.ID, At: at}),
			tx.Notices().Create(ctx, domain.LessonNotice{
				ID: ids.New[domain.LessonNoticeID](src), LessonID: lesson, SenderID: account, SentAt: at, Recipients: 1,
			}),
			tx.CalendarFeeds().Create(ctx, domain.CalendarFeed{
				ID: ids.New[domain.CalendarFeedID](src), AccountID: account, Scope: domain.FeedBookings,
				TokenHash: [32]byte{7, 8, 9}, CreatedAt: at, LastUsedAt: at,
			}),
			tx.Outbox().Create(ctx, domain.OutboxMail{
				ID: ids.New[domain.OutboxMailID](src), To: "someone@example.org", Subject: "Hello",
				Text: "Hello", CreatedAt: at, NextAttemptAt: at, ReplyTo: "staff@example.org", ReceiptID: receipt.ID,
			}),
			tx.Audit().Create(ctx, domain.AuditEvent{
				ID: ids.New[domain.AuditEventID](src), At: at, ActorID: account,
				Action: domain.AuditImpersonationStarted, TargetKind: domain.AuditTargetAccount, TargetID: account,
			}),
			tx.Audit().Create(ctx, domain.AuditEvent{
				ID: ids.New[domain.AuditEventID](src), At: at, ActorID: account, Action: domain.AuditMailRequeued,
				TargetKind: domain.AuditTargetPerson, TargetPersonID: person,
				Details: domain.AuditDetails{MailID: ids.New[domain.OutboxMailID](src), Recipient: "x@example.org"},
			}),
			tx.Announcements().Create(ctx, announcement),
			tx.Announcements().Dismiss(ctx, domain.AnnouncementDismissal{
				AnnouncementID: announcement.ID, AccountID: account, DismissedAt: at,
			}),
			tx.Announcements().CreateMailing(ctx, domain.AnnouncementMailing{
				ID: ids.New[domain.AnnouncementMailingID](src), AnnouncementID: announcement.ID, SenderID: account,
				SentAt: at, Recipients: 2,
			}),
			tx.Imports().Create(ctx, domain.ImportRun{
				ID: ids.New[domain.ImportRunID](src), FileName: "members.csv", ActorID: account, StartedAt: at,
				FinishedAt: at.Add(time.Second), Counts: domain.ImportCounts{Rows: 2, Created: 1, Failed: 1},
				Errors: []domain.ImportRowError{{Line: 3, Code: "required", Field: "familyName"}},
			}),
			tx.DuplicateDismissals().Create(ctx, domain.NewDuplicateDismissal(person, accounts[len(accounts)-1].PersonID,
				account, at)),
		)
	})
	if err != nil {
		t.Fatalf("adding rows: %v", err)
	}

	for _, name := range stateFields() {
		if empty(content(&st.state, name)) {
			t.Fatalf("table %s is empty; fill it so the round trip covers it", name)
		}
	}

	return st
}

func TestSnapshotRoundTrip(t *testing.T) {
	t.Parallel()

	original := fullStore(t)

	data, err := original.Snapshot("v1.2.3")
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	restored := New()
	if err := restored.Restore(data, "v1.2.3"); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	for _, name := range stateFields() {
		want, got := content(&original.state, name), content(&restored.state, name)
		if !reflect.DeepEqual(want.Interface(), got.Interface()) {
			t.Errorf("%s differs after the round trip:\nwant %v\n got %v", name, want, got)
		}
	}

	// The restored store keeps working: a write copies the restored tables
	// and leaves the snapshot's data alone.
	ctx := t.Context()

	err = restored.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		locations, err := tx.Locations().List(ctx)
		if err != nil {
			return err
		}

		l := locations[0]
		l.Name = "Renamed studio"

		return tx.Locations().Update(ctx, l)
	})
	if err != nil {
		t.Fatalf("writing to the restored store: %v", err)
	}

	again, err := restored.Snapshot("v1.2.3")
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	if !bytes.Contains(again, []byte("Renamed studio")) {
		t.Error("a second snapshot misses the change made after the restore")
	}
}

func TestSnapshotOfEmptyStore(t *testing.T) {
	t.Parallel()

	data, err := New().Snapshot("dev")
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	restored := New()
	if err := restored.Restore(data, "dev"); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	if restored.state.settings != nil || len(restored.state.persons.rows) != 0 {
		t.Error("an empty snapshot restored data")
	}
}

func TestRestoreRefusesOtherVersions(t *testing.T) {
	t.Parallel()

	data, err := fullStore(t).Snapshot("v1.0.0")
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	edit := func(change func(map[string]any)) []byte {
		var doc map[string]any
		if err := json.Unmarshal(data, &doc); err != nil {
			t.Fatal(err)
		}

		change(doc)

		out, err := json.Marshal(doc)
		if err != nil {
			t.Fatal(err)
		}

		return out
	}

	tests := []struct {
		name  string
		data  []byte
		build string
		want  error
	}{
		{name: "other build", data: data, build: "v1.0.1", want: ErrSnapshotMismatch},
		{name: "other format", data: edit(func(d map[string]any) { d["format"] = 0 }), build: "v1.0.0", want: ErrSnapshotMismatch},
		{name: "other layout", data: edit(func(d map[string]any) { d["layout"] = "x" }), build: "v1.0.0", want: ErrSnapshotMismatch},
		{name: "missing table", data: edit(func(d map[string]any) {
			delete(d["tables"].(map[string]any), "lessons")
		}), build: "v1.0.0", want: ErrSnapshotMismatch},
		{name: "duplicate key", data: edit(func(d map[string]any) {
			tables := d["tables"].(map[string]any)
			rows := tables["locations"].([]any)
			tables["locations"] = append(rows, rows[0])
		}), build: "v1.0.0"},
		{name: "not json", data: []byte("{"), build: "v1.0.0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			st := New()

			err := st.Restore(tt.data, tt.build)
			if err == nil {
				t.Fatal("Restore succeeded")
			}

			if tt.want != nil && !errors.Is(err, tt.want) {
				t.Errorf("Restore error = %v, want %v", err, tt.want)
			}

			// A refused snapshot leaves the store as it was: empty.
			if st.state.settings != nil || len(st.state.locations.rows) != 0 {
				t.Error("a refused snapshot changed the store")
			}
		})
	}
}
