// Package seed provides fictional demo data for the demo server, the
// WebAssembly build and tests. It must never contain real names or data.
package seed

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// DemoSettings returns the settings of the fictional demo school.
func DemoSettings() domain.Settings {
	return domain.Settings{
		SchoolName:         "Studio Demo — Ballet & Dance",
		Timezone:           "Europe/Berlin",
		DefaultLocale:      "en",
		Currency:           "EUR",
		ReceiptPrefix:      "SD",
		ReleaseWeekday:     time.Sunday,
		ReleaseTime:        domain.TimeOfDay{Hour: 18},
		ReleaseWeeksAhead:  1,
		BookingCutoff:      0,
		CancellationCutoff: 2 * time.Hour,
		DefaultVisibleDays: 14,
		// A fictional imprint for the footer (ADR 0013).
		LegalNotice: "Studio Demo Dance School · Pirouette Lane 7 · 10999 Exampletown\n" +
			"A fictional school: every name, address and number in this demo is made up.",

		WaitlistInviteExpiry: 12 * time.Hour,
		// Like the previous system: a visit is used when attendance is
		// taken (ADR 0012).
		RedemptionMode: domain.RedeemOnAttendance,
		// A fictional issuer: the name, address and tax number exist
		// nowhere.
		Receipts: domain.ReceiptSettings{
			Issuer: domain.ReceiptIssuer{
				Name: "Studio Demo Dance School", Address: "Pirouette Lane 7\n10999 Exampletown",
				TaxID: "99/999/99999", Email: "office@studio-demo.example", Phone: "+49 000 0000000",
			},
			TaxMode: domain.TaxStandard,
			Footer:  "Bank: Example Bank · IBAN XX00 0000 0000 0000 0000 00",
		},
	}
}

// School selects one of the fictional demo schools (ADR 0018). They share
// every person, class and all the history and differ in their settings
// (their name, imprint, receipt issuer, theme, accent color, language and
// background, and so in the receipts issued with them) and in their
// places: the werkstatt school's one house holds the same classes in
// fewer rooms.
// The zero School is not valid: a caller names the school it wants.
type School string

// The demo schools.
const (
	// SchoolProgramme is the demo school in the app's own look
	// (DemoSettings), the default.
	SchoolProgramme School = "programme"
	// SchoolWerkstatt is a German demo school in the Werkstatt look, with
	// a background image.
	SchoolWerkstatt School = "werkstatt"
)

// Schools returns all demo schools, the default first.
func Schools() []School { return []School{SchoolProgramme, SchoolWerkstatt} }

// Valid reports whether s is a known demo school.
func (s School) Valid() bool { return slices.Contains(Schools(), s) }

// ParseSchool returns the demo school named name, for a choice that must
// not fail, like the web demo's (its worker passes the visitor's choice in
// DEMO_SCHOOL). An empty or unknown name, matched exactly like Valid,
// returns SchoolProgramme, the default, with ok false, so the caller can
// log the fallback.
func ParseSchool(name string) (school School, ok bool) {
	if s := School(name); s.Valid() {
		return s, true
	}

	return SchoolProgramme, false
}

// werkstattBackground is the Werkstatt school's background image, a CC0
// photo (assets/README.md).
//
//go:embed assets/werkstatt-background.jpg
var werkstattBackground string

// schoolData returns the settings, the background and the places of school:
// the programme school's are DemoSettings, none and demoLocations; the
// werkstatt school has its own identity (name, imprint, receipt issuer and
// prefix), theme, accent, language, background and places
// (werkstattLocations), and the programme school's rules.
func schoolData(school School) (domain.Settings, domain.Background, []locationSpec, error) {
	settings := DemoSettings()

	switch school {
	case SchoolProgramme:
		return settings, domain.Background{}, demoLocations, nil
	case SchoolWerkstatt:
		// A fictional name; the accent passes the 4.5:1 contrast check,
		// and its dark variant is derived from it (domain.DarkAccent).
		settings.SchoolName = "Tanzhaus am Lindenhof"
		settings.Theme = domain.ThemeWerkstatt
		settings.AccentColor = "#9c215b"
		settings.DefaultLocale = "de"
		settings.ReceiptPrefix = "TL"
		// A fictional imprint and issuer, like the programme school's: the
		// street, the town, the tax number and the bank exist nowhere.
		settings.LegalNotice = "Tanzhaus am Lindenhof · Lindenhofweg 12 · 10999 Beispielstadt\n" +
			"Eine erfundene Schule: Alle Namen, Adressen und Nummern in dieser Demo sind ausgedacht."
		settings.Receipts.Issuer = domain.ReceiptIssuer{
			Name: "Tanzhaus am Lindenhof", Address: "Lindenhofweg 12\n10999 Beispielstadt",
			TaxID: "99/999/99999", Email: "buero@tanzhaus-lindenhof.example", Phone: "+49 000 0000000",
		}
		settings.Receipts.Footer = "Bankverbindung: Beispielbank · IBAN XX00 0000 0000 0000 0000 00"

		// A fresh copy: the store owns what it is given.
		background := domain.Background{ContentType: domain.BackgroundJPEG, Data: []byte(werkstattBackground)}

		return settings, background, werkstattLocations, nil
	default:
		names := make([]string, 0, len(Schools()))
		for _, s := range Schools() {
			names = append(names, string(s))
		}

		return domain.Settings{}, domain.Background{}, nil, fmt.Errorf("seed: unknown demo school %q, want one of %s: %w",
			school, strings.Join(names, ", "), domain.ErrValidation)
	}
}

// Dataset is a complete set of school data. Every entity is valid, and all
// references point to entities of the same Dataset.
type Dataset struct {
	Settings domain.Settings
	// Background is the school's background image, zero for none.
	Background   domain.Background
	Locations    []domain.Location
	Rooms        []domain.Room
	Staff        []domain.Staff
	CourseTypes  []domain.CourseType
	Levels       []domain.Level // the level scale, by position (ADR 0021)
	Terms        []domain.Term
	Classes      []domain.Class
	Persons      []domain.Person
	Guardians    []domain.Guardian
	Accounts     []domain.Account
	Credentials  []domain.Credential
	ClassMembers []domain.ClassMember
	Lessons      []domain.Lesson
	// Bookings are the automatic bookings of the class members in the
	// lessons of their classes, the members' own bookings of the full
	// workshop and the participants an instructor added to her workshop,
	// each with its event in BookingEvents. The member bookings of the
	// lessons before the anchor (but Classical Ballet's) have their
	// attendance, a no-show or a late cancellation as a second event, and
	// those lessons have guests and trials whom their instructor added,
	// with their attendance.
	Bookings      []domain.Booking
	BookingEvents []domain.BookingEvent
	// WaitlistEntries are the waiting list of the full workshop.
	WaitlistEntries []domain.WaitlistEntry
	// PassTypes, Passes and Redemptions are the pass catalog, the passes
	// sold and given away, and the drop-in bookings (in Bookings) the
	// passes paid for when their attendance was taken (ADR 0012).
	PassTypes   []domain.PassType
	Passes      []domain.Pass
	Redemptions []domain.PassRedemption
	// Receipts are the receipts of the sold passes and the cancellation of
	// the pass sold by mistake, in the order they were issued; their PDFs
	// are rendered when first read (ADR 0012).
	Receipts []domain.Receipt
	// Announcements are the office's announcements, not emailed (ADR 0014).
	Announcements []domain.Announcement
}

// LoadDemo writes the complete demo data set for FixedDemoAnchor (Demo)
// into st in one transaction. It fails with domain.ErrConflict unless st is
// empty (has no settings and no persons).
func LoadDemo(ctx context.Context, st store.Store) error {
	return LoadDemoAt(ctx, st, FixedDemoAnchor)
}

// LoadDemoAt is LoadDemo for the demo data as seen on the visit date anchor
// (DemoAt). The web demo uses it with today's date, so its term and
// workshops always lie around the day of the visit.
func LoadDemoAt(ctx context.Context, st store.Store, anchor domain.Date) error {
	return LoadDemoSchoolAt(ctx, st, anchor, SchoolProgramme)
}

// LoadDemoSchoolAt is LoadDemoAt for the demo school school (DemoSchoolAt).
// An unknown school fails with domain.ErrValidation before st is touched.
func LoadDemoSchoolAt(ctx context.Context, st store.Store, anchor domain.Date, school School) error {
	d, err := DemoSchoolAt(anchor, school)
	if err != nil {
		return err
	}

	err = st.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		return write(ctx, tx, d)
	})
	if err != nil {
		return fmt.Errorf("seed demo data: %w", err)
	}

	return nil
}

// write stores d through the repositories of tx, referenced records first.
// It has no side effects outside tx, so the store may retry it.
func write(ctx context.Context, tx store.Tx, d Dataset) error {
	if err := checkEmpty(ctx, tx); err != nil {
		return err
	}

	if err := tx.Settings().Put(ctx, d.Settings); err != nil {
		return fmt.Errorf("settings: %w", err)
	}

	if !d.Background.IsZero() {
		if err := tx.Settings().PutBackground(ctx, d.Background); err != nil {
			return fmt.Errorf("background: %w", err)
		}
	}

	for _, p := range d.Persons {
		if err := tx.Persons().Create(ctx, p); err != nil {
			return fmt.Errorf("person %v: %w", p.ID, err)
		}
	}

	for _, g := range d.Guardians {
		if err := tx.Persons().AddGuardian(ctx, g); err != nil {
			return fmt.Errorf("guardian %v of %v: %w", g.GuardianID, g.DependentID, err)
		}
	}

	for _, a := range d.Accounts {
		if err := tx.Accounts().Create(ctx, a); err != nil {
			return fmt.Errorf("account %v: %w", a.ID, err)
		}
	}

	for _, c := range d.Credentials {
		if err := tx.Credentials().Put(ctx, c); err != nil {
			return fmt.Errorf("credential of account %v: %w", c.AccountID, err)
		}
	}

	for _, l := range d.Locations {
		if err := tx.Locations().Create(ctx, l); err != nil {
			return fmt.Errorf("location %v: %w", l.ID, err)
		}
	}

	for _, r := range d.Rooms {
		if err := tx.Rooms().Create(ctx, r); err != nil {
			return fmt.Errorf("room %v: %w", r.ID, err)
		}
	}

	for _, s := range d.Staff {
		if err := tx.Staff().Create(ctx, s); err != nil {
			return fmt.Errorf("staff %v: %w", s.ID, err)
		}
	}

	for _, c := range d.CourseTypes {
		if err := tx.CourseTypes().Create(ctx, c); err != nil {
			return fmt.Errorf("course type %v: %w", c.ID, err)
		}
	}

	for _, l := range d.Levels {
		if err := tx.Levels().Create(ctx, l); err != nil {
			return fmt.Errorf("level %v: %w", l.ID, err)
		}
	}

	for _, t := range d.Terms {
		if err := tx.Terms().Create(ctx, t); err != nil {
			return fmt.Errorf("term %v: %w", t.ID, err)
		}
	}

	for _, c := range d.Classes {
		if err := tx.Classes().Create(ctx, c); err != nil {
			return fmt.Errorf("class %v: %w", c.ID, err)
		}
	}

	for _, m := range d.ClassMembers {
		if err := tx.Classes().AddMember(ctx, m); err != nil {
			return fmt.Errorf("member %v of class %v: %w", m.PersonID, m.ClassID, err)
		}
	}

	for _, l := range d.Lessons {
		if err := tx.Lessons().Create(ctx, l); err != nil {
			return fmt.Errorf("lesson %v: %w", l.ID, err)
		}
	}

	for _, b := range d.Bookings {
		if err := tx.Bookings().Create(ctx, b); err != nil {
			return fmt.Errorf("booking %v: %w", b.ID, err)
		}
	}

	for _, e := range d.BookingEvents {
		if err := tx.Bookings().AddEvent(ctx, e); err != nil {
			return fmt.Errorf("booking event %v: %w", e.ID, err)
		}
	}

	for _, e := range d.WaitlistEntries {
		if err := tx.Waitlist().Create(ctx, e); err != nil {
			return fmt.Errorf("waitlist entry %v: %w", e.ID, err)
		}
	}

	for _, pt := range d.PassTypes {
		if err := tx.PassTypes().Create(ctx, pt); err != nil {
			return fmt.Errorf("pass type %v: %w", pt.ID, err)
		}
	}

	// Receipts before the passes, which refer to them, in the order they
	// were issued, each with the next number of its year.
	for _, r := range d.Receipts {
		seq, err := tx.ReceiptCounters().Next(ctx, r.Year)
		if err != nil {
			return fmt.Errorf("receipt number: %w", err)
		}

		if seq != r.Sequence {
			return fmt.Errorf("receipt %s: the counter is at %d: %w", r.Number, seq, domain.ErrConflict)
		}

		if err := tx.Receipts().Create(ctx, r); err != nil {
			return fmt.Errorf("receipt %s: %w", r.Number, err)
		}
	}

	for _, p := range d.Passes {
		if err := tx.Passes().Create(ctx, p); err != nil {
			return fmt.Errorf("pass %v: %w", p.ID, err)
		}
	}

	for _, r := range d.Redemptions {
		if err := tx.Redemptions().Create(ctx, r); err != nil {
			return fmt.Errorf("redemption of booking %v: %w", r.BookingID, err)
		}
	}

	for _, a := range d.Announcements {
		if err := tx.Announcements().Create(ctx, a); err != nil {
			return fmt.Errorf("announcement %v: %w", a.ID, err)
		}
	}

	return nil
}

// checkEmpty fails with domain.ErrConflict if the store has settings or any
// root record: demo data is never mixed into a store that is in use. Every
// other record references a person, location, staff member, course type,
// level, term or pass type, so checking these covers the whole store.
func checkEmpty(ctx context.Context, tx store.Tx) error {
	switch _, err := tx.Settings().Get(ctx); {
	case err == nil:
		return fmt.Errorf("%w: the store is not empty (settings exist)", domain.ErrConflict)
	case !errors.Is(err, domain.ErrNotFound):
		return fmt.Errorf("settings: %w", err)
	}

	roots := []struct {
		name  string
		count func() (int, error)
	}{
		{"persons", func() (int, error) { return count(tx.Persons().List(ctx)) }},
		{"locations", func() (int, error) { return count(tx.Locations().List(ctx)) }},
		{"staff", func() (int, error) { return count(tx.Staff().List(ctx)) }},
		{"course types", func() (int, error) { return count(tx.CourseTypes().List(ctx)) }},
		{"levels", func() (int, error) { return count(tx.Levels().List(ctx)) }},
		{"terms", func() (int, error) { return count(tx.Terms().List(ctx)) }},
		{"pass types", func() (int, error) { return count(tx.PassTypes().List(ctx)) }},
	}

	for _, r := range roots {
		n, err := r.count()
		if err != nil {
			return fmt.Errorf("%s: %w", r.name, err)
		}

		if n > 0 {
			return fmt.Errorf("%w: the store is not empty (%s exist)", domain.ErrConflict, r.name)
		}
	}

	return nil
}

func count[T any](items []T, err error) (int, error) {
	return len(items), err
}
