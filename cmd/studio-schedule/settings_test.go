package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/app"
	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/httpapi"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/service"
	"github.com/cwbudde/studio-schedule/internal/store"
	"github.com/cwbudde/studio-schedule/internal/store/memory"
)

// secondAdmin is the confirmed admin of the school newSchool sets up.
const secondAdmin = "admin@second-studio.example.org"

// schoolSettings returns the settings of st.
func schoolSettings(t *testing.T, st store.Store) domain.Settings {
	t.Helper()

	s, err := service.NewSettingsService(st, clock.NewFixed(testNow), ids.NewSequence()).Export(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	return s
}

// newSchool sets a new school up in the empty store st as init does (New
// York, US dollars, German) and confirms its admin secondAdmin: no lessons,
// prices or receipts fix its time zone and currency yet.
func newSchool(t *testing.T, st store.Store) {
	t.Helper()

	ctx, clk := t.Context(), clock.NewFixed(testNow)

	res, err := service.NewSetupService(st, clk, ids.NewSequence(), "https://second-studio.example.test/").Init(ctx,
		service.SetupData{
			SchoolName: "Second Example Studio", Timezone: "America/New_York", Locale: "de", Currency: "USD",
			AdminEmail: secondAdmin, AdminGivenName: "Rosa", AdminFamilyName: "Example",
		})
	if err != nil {
		t.Fatal(err)
	}

	link, err := url.Parse(res.Link)
	if err != nil {
		t.Fatal(err)
	}

	auth := app.NewServices(app.Deps{Store: st, Clock: clk, IDs: ids.NewSequence()}).Auth
	if err := auth.ResetPassword(ctx, link.Query().Get("token"), "violet-harbor-lantern-42"); err != nil {
		t.Fatal(err)
	}
}

// schools runs fn on the memory store and on PostgreSQL, each with a run
// on the demo data and one on a new school (newSchool).
func schools(t *testing.T, fn func(t *testing.T, demo, second cliRun)) {
	t.Helper()

	t.Run("memory", func(t *testing.T) {
		t.Parallel()

		st := memory.New()
		newSchool(t, st)
		fn(t, cliRun{st: demoStore(t)}, cliRun{st: st})
	})
	t.Run("postgres", func(t *testing.T) {
		t.Parallel()

		url := newTestDatabase(t)
		newSchool(t, openTestStore(t, url))
		fn(t, cliRun{}.with("DATABASE_URL", demoDatabase(t)), cliRun{}.with("DATABASE_URL", url))
	})
}

// settingsFile writes data as a file in a temporary directory and returns
// its path.
func settingsFile(t *testing.T, data string) string {
	t.Helper()

	file := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(file, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}

	return file
}

// TestSettingsGet prints every area, and one, as the office account: text
// with the human forms of durations, amounts and instants, and JSON as the
// API's response bodies.
func TestSettingsGet(t *testing.T) {
	t.Parallel()

	stores(t, func(t *testing.T, r cliRun) {
		got := r.exec(t, "settings", "get", "--as", demoOffice)
		if got.code != 0 || got.stderr != "" {
			t.Fatalf("get: exit %d, stderr %q", got.code, got.stderr)
		}

		golden(t, "settings/get.txt", got.stdout)

		got = r.exec(t, "settings", "get", "--as", demoOffice, "--output", "json")
		if got.code != 0 {
			t.Fatalf("get, JSON: exit %d, stderr %q", got.code, got.stderr)
		}

		golden(t, "settings/get.json", got.stdout)

		var all struct {
			School       httpapi.SchoolSettings      `json:"school"`
			Regional     httpapi.RegionalSettings    `json:"regional"`
			BookingRules httpapi.BookingRules        `json:"bookingRules"`
			Receipts     httpapi.ReceiptSettingsBody `json:"receipts"`
			Passes       httpapi.PassSettings        `json:"passes"`
		}
		if err := json.Unmarshal([]byte(got.stdout), &all); err != nil || all.School.SchoolName == "" ||
			!all.Regional.TimezoneLocked || all.BookingRules.CancellationCutoffMinutes != 120 ||
			all.Receipts.AddressRequiredAbove != 25_000 || all.Passes.RedemptionMode != "attendance" {
			t.Errorf("all areas %+v, %v", all, err)
		}

		got = r.exec(t, "settings", "get", "booking-rules", "--as", demoAdmin, "--output", "json")
		if got.code != 0 {
			t.Fatalf("get booking-rules: exit %d, stderr %q", got.code, got.stderr)
		}

		golden(t, "settings/get-booking-rules.json", got.stdout)

		got = r.exec(t, "settings", "get", "receipts", "--as", demoOffice)
		if got.code != 0 || !strings.Contains(got.stdout, "receipts  addressRequiredAbove  250.00 EUR\n") ||
			strings.Contains(got.stdout, "school") {
			t.Errorf("get receipts: exit %d, stdout %q", got.code, got.stdout)
		}
	})
}

// TestSettingsGetTerminal: on a terminal the table has a header line.
func TestSettingsGetTerminal(t *testing.T) {
	t.Parallel()

	got := cliRun{st: demoStore(t), terminal: true}.with("NO_COLOR", "1").
		exec(t, "settings", "get", "passes", "--as", demoOffice)
	if got.code != 0 || got.stdout != "Area    Key             Value\npasses  redemptionMode  attendance\n" {
		t.Errorf("exit %d, stdout %q", got.code, got.stdout)
	}
}

// TestSettingsSet changes keys of an area as an admin and keeps the
// others; the change is stored, and a wider release window generates its
// lessons at once.
func TestSettingsSet(t *testing.T) {
	t.Parallel()

	stores(t, func(t *testing.T, r cliRun) {
		st := storeOf(t, r)
		before := schoolSettings(t, st)

		got := r.exec(t, "settings", "set", "school", "schoolName=  Studio Example  ", "legalNotice=", "--as", demoAdmin)
		if got.code != 0 || got.stderr != "" {
			t.Fatalf("set school: exit %d, stderr %q", got.code, got.stderr)
		}

		golden(t, "settings/set-school.txt", got.stdout)

		got = r.exec(t, "settings", "set", "booking-rules", "releaseWeeksAhead=3", "cancellationCutoffMinutes=90",
			"releasedThrough=2026-12-24", "--as", demoAdmin, "--output", "json")
		if got.code != 0 || got.stderr != "" {
			t.Fatalf("set booking-rules: exit %d, stderr %q", got.code, got.stderr)
		}

		golden(t, "settings/set-booking-rules.json", got.stdout)

		var rules httpapi.BookingRules
		if err := json.Unmarshal([]byte(got.stdout), &rules); err != nil || rules.GeneratedLessons == 0 {
			t.Errorf("booking rules %+v, %v: want lessons generated", rules, err)
		}

		for _, args := range [][]string{
			{"branding", "theme=werkstatt", "accentColor=#1F5FA8"},
			{"regional", "defaultLocale=de-AT", "timezone=Europe/Berlin"},
			{"receipts", "phone=+49 000 1111111", "taxMode=small_business"},
			{"passes", "redemptionMode=booking"},
		} {
			if got := r.exec(t, append([]string{"settings", "set", "--as", demoAdmin}, args...)...); got.code != 0 {
				t.Errorf("set %v: exit %d, stderr %q", args, got.code, got.stderr)
			}
		}

		after := schoolSettings(t, st)

		want := before
		want.SchoolName, want.LegalNotice = "Studio Example", ""
		want.ReleaseWeeksAhead, want.CancellationCutoff = 3, 90*60*1e9
		want.ReleasedThrough = domain.Date{Year: 2026, Month: 12, Day: 24}
		want.Theme, want.AccentColor, want.DefaultLocale = domain.ThemeWerkstatt, "#1f5fa8", "de"
		want.Receipts.Issuer.Phone, want.Receipts.TaxMode = "+49 000 1111111", domain.TaxSmallBusiness
		want.RedemptionMode = domain.RedeemOnBooking

		if after != want {
			t.Errorf("settings after the changes\n%+v\nwant\n%+v", after, want)
		}

		got = r.exec(t, "settings", "set", "passes", "redemptionMode=attendance", "--as", demoAdmin, "-o", "json")
		if got.code != 0 || got.stdout != "{\n  \"redemptionMode\": \"attendance\"\n}\n" {
			t.Errorf("set passes, JSON: exit %d, stdout %q", got.code, got.stdout)
		}
	})
}

// TestSettingsSetRefusals: what set refuses changes nothing: a usage error
// (exit 2) for the arguments and invalid values, each named as key=value,
// a conflict (exit 4) for a locked time zone or currency.
func TestSettingsSetRefusals(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		args   []string
		code   int
		stderr string
	}{
		"unknown area": {
			args: []string{"colors", "accent=red"}, code: 2,
			stderr: "error: unknown settings area \"colors\": want one of school, branding, regional, booking-rules, " +
				"receipts, passes\n",
		},
		"no key": {args: []string{"school"}, code: 2, stderr: "error: requires at least 2 arg(s), only received 1\n"},
		"no =":   {args: []string{"school", "schoolName"}, code: 2, stderr: "error: \"schoolName\": want key=value\n"},
		"unknown key": {
			args: []string{"regional", "timezoneLocked=false"}, code: 2,
			stderr: "error: timezoneLocked=false: unknown key of the regional settings; keys: timezone, " +
				"defaultLocale, currency\n",
		},
		"twice": {
			args: []string{"school", "schoolName=A", "schoolName=B"}, code: 2,
			stderr: "error: schoolName=B: the key schoolName is given twice\n",
		},
		"not a number": {
			args: []string{"booking-rules", "releaseWeeksAhead=two"}, code: 2,
			stderr: "error: releaseWeeksAhead=two: not a whole number\n",
		},
		"not a time": {
			args: []string{"booking-rules", "releaseTime=25:00"}, code: 2,
			stderr: "error: releaseTime=25:00: validation failed: invalid time of day \"25:00\"\n",
		},
		"not a date": {args: []string{"booking-rules", "releasedThrough=2026-13-01"}, code: 2},
		"bad weekday": {
			args: []string{"booking-rules", "releaseWeekday=9"}, code: 2,
			stderr: "error: releaseWeekday=9: invalid release weekday\n",
		},
		"long cutoff": {
			args: []string{"booking-rules", "bookingCutoffMinutes=50000"}, code: 2,
			stderr: "error: bookingCutoffMinutes=50000: booking cutoff must be between 0 and 30 days\n",
		},
		"far released-through": {
			args: []string{"booking-rules", "releasedThrough=2028-01-01"}, code: 2,
			stderr: "error: releasedThrough=2028-01-01: released-through date must be at most 52 weeks after today\n",
		},
		"low contrast": {
			args: []string{"branding", "accentColor=#ffff00"}, code: 2,
			stderr: "error: accentColor=#ffff00: accent color must reach a contrast of 4.5:1 against the text on it\n",
		},
		"bad prefix": {
			args: []string{"receipts", "receiptPrefix=ab"}, code: 2,
			stderr: "error: receiptPrefix=ab: receipt prefix must be 1-16 of A-Z and 0-9\n",
		},
		"bad mode": {
			args: []string{"passes", "redemptionMode=never"}, code: 2,
			stderr: "error: redemptionMode=never: invalid redemption mode\n",
		},
		"locked time zone": {
			args: []string{"regional", "timezone=America/New_York"}, code: 4,
			stderr: "error: set time and language: conflict: the time zone cannot change once lessons exist\n",
		},
		"locked currency": {
			args: []string{"regional", "currency=USD"}, code: 4,
			stderr: "error: set time and language: conflict: the currency cannot change once prices or receipts exist\n",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			st := demoStore(t)
			before := schoolSettings(t, st)

			got := cliRun{st: st}.exec(t, append([]string{"settings", "set", "--as", demoAdmin}, tc.args...)...)
			if got.code != tc.code || got.stdout != "" || (tc.stderr != "" && got.stderr != tc.stderr) {
				t.Errorf("set %v: exit %d, stdout %q, stderr %q; want exit %d, stderr %q", tc.args, got.code,
					got.stdout, got.stderr, tc.code, tc.stderr)
			}

			if after := schoolSettings(t, st); after != before {
				t.Errorf("set %v changed the settings:\n%+v", tc.args, after)
			}
		})
	}
}

// TestSettingsSetFieldErrorJSON: with --output json the field error is a
// problem document naming the key=value.
func TestSettingsSetFieldErrorJSON(t *testing.T) {
	t.Parallel()

	got := cliRun{st: demoStore(t)}.exec(t, "settings", "set", "booking-rules", "waitlistInviteExpiryMinutes=0",
		"--as", demoAdmin, "--output", "json")

	var problem struct {
		Status int `json:"status"`
		Errors []struct {
			Message  string `json:"message"`
			Location string `json:"location"`
		} `json:"errors"`
	}
	if err := json.Unmarshal([]byte(got.stderr), &problem); err != nil || got.code != 2 || problem.Status != 422 ||
		len(problem.Errors) != 1 || problem.Errors[0].Location != "waitlistInviteExpiryMinutes=0" {
		t.Errorf("exit %d, stderr %q (%v)", got.code, got.stderr, err)
	}
}

// TestSettingsAccess: the reads need the office, the writes an admin; the
// account of --as is checked before anything runs.
func TestSettingsAccess(t *testing.T) {
	t.Parallel()

	file := settingsFile(t, `{"version": 1, "passes": {"redemptionMode": "booking"}}`)

	for name, tc := range map[string]struct {
		args   []string
		code   int
		stderr string
	}{
		"get without --as": {
			args: []string{"get"}, code: 2,
			stderr: "error: studio-schedule settings get acts as an account: give --as <email> or set STUDIO_AS\n",
		},
		"get, unknown --as": {
			args: []string{"get", "--as", "nobody@example.org"}, code: 3,
			stderr: "error: --as nobody@example.org: not found: no account has this email\n",
		},
		"get as a member": {
			args: []string{"get", "--as", demoMember}, code: 4,
			stderr: "error: --as " + demoMember + ": forbidden: the account's role member is below office, " +
				"which this command needs\n",
		},
		"get as an instructor": {args: []string{"get", "school", "--as", demoInstructor}, code: 4},
		"export as a member":   {args: []string{"export", "--as", demoMember}, code: 4},
		"set as the office": {
			args: []string{"set", "passes", "redemptionMode=booking", "--as", demoOffice}, code: 4,
			stderr: "error: --as " + demoOffice + ": forbidden: the account's role office is below admin, " +
				"which this command needs\n",
		},
		"import as the office": {args: []string{"import", "--yes", file, "--as", demoOffice}, code: 4},
		"get, unknown area":    {args: []string{"get", "colors", "--as", demoOffice}, code: 2},
		"get, two areas":       {args: []string{"get", "school", "regional", "--as", demoOffice}, code: 2},
		"unknown command": {
			args: []string{"show"}, code: 2, stderr: "error: unknown command \"show\" for \"studio-schedule settings\"\n",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			st := demoStore(t)
			before := schoolSettings(t, st)

			got := cliRun{st: st}.exec(t, append([]string{"settings"}, tc.args...)...)
			if got.code != tc.code || got.stdout != "" || (tc.stderr != "" && got.stderr != tc.stderr) {
				t.Errorf("settings %v: exit %d, stdout %q, stderr %q; want exit %d, stderr %q", tc.args, got.code,
					got.stdout, got.stderr, tc.code, tc.stderr)
			}

			if after := schoolSettings(t, st); after != before {
				t.Errorf("settings %v changed the settings", tc.args)
			}
		})
	}
}

// TestSettingsExportImport exports the demo school's settings and imports
// them into a second school: the dry run prints the changes and writes
// nothing, the import needs a confirmation, writes every area and leaves
// the second school with the demo's settings, logo and background aside.
func TestSettingsExportImport(t *testing.T) {
	t.Parallel()

	schools(t, func(t *testing.T, demo, second cliRun) {
		got := demo.exec(t, "settings", "export", "--as", demoOffice)
		if got.code != 0 || got.stderr != "" {
			t.Fatalf("export: exit %d, stderr %q", got.code, got.stderr)
		}

		golden(t, "settings/export.json", got.stdout)

		if j := demo.exec(t, "settings", "export", "--as", demoOffice, "-o", "json"); j.stdout != got.stdout {
			t.Errorf("export with --output json differs:\n%s", j.stdout)
		}

		file := filepath.Join(t.TempDir(), "studio.json")
		if f := demo.exec(t, "settings", "export", "--as", demoOffice, file); f.code != 0 || f.stdout != "" {
			t.Fatalf("export to a file: exit %d, stdout %q, stderr %q", f.code, f.stdout, f.stderr)
		}

		if data, err := os.ReadFile(file); err != nil || string(data) != got.stdout {
			t.Errorf("the file holds %q (%v), want the export", data, err)
		}

		secondSt := storeOf(t, second)
		before := schoolSettings(t, secondSt)

		got = second.exec(t, "settings", "import", "--as", secondAdmin, "--dry-run", file)
		if got.code != 0 || got.stderr != "" {
			t.Fatalf("dry run: exit %d, stderr %q", got.code, got.stderr)
		}

		golden(t, "settings/import-dry-run.txt", got.stdout)

		if after := schoolSettings(t, secondSt); after != before {
			t.Errorf("the dry run wrote: %+v", after)
		}

		// Without a terminal and --yes it refuses and writes nothing.
		got = second.exec(t, "settings", "import", "--as", secondAdmin, file)
		if got.code != 2 || got.stdout != "" || got.stderr != "error: studio-schedule settings import changes data: "+
			"confirm with --yes when stdin is not a terminal; nothing was changed\n" {
			t.Errorf("without --yes: exit %d, stdout %q, stderr %q", got.code, got.stdout, got.stderr)
		}

		// On a terminal, "n" keeps the settings.
		terminal := second
		terminal.terminal, terminal.stdin = true, "n\n"

		got = terminal.with("NO_COLOR", "1").exec(t, "settings", "import", "--as", secondAdmin, file)
		if got.code != 2 || got.stdout != "" ||
			got.stderr != "Overwrite the settings with those of "+file+"? [y/N] error: not confirmed; nothing was changed\n" {
			t.Errorf("answered n: exit %d, stdout %q, stderr %q", got.code, got.stdout, got.stderr)
		}

		if after := schoolSettings(t, secondSt); after != before {
			t.Errorf("a refused import wrote: %+v", after)
		}

		got = second.exec(t, "settings", "import", "--as", secondAdmin, "--yes", file, "--output", "json")
		if got.code != 0 || got.stderr != "" {
			t.Fatalf("import: exit %d, stderr %q", got.code, got.stderr)
		}

		golden(t, "settings/import.json", got.stdout)

		// The second school now exports what the demo exported.
		if again := second.exec(t, "settings", "export", "--as", secondAdmin); again.stdout != demo.exec(t,
			"settings", "export", "--as", demoOffice).stdout {
			t.Errorf("the second school exports\n%s", again.stdout)
		}

		// Importing the same again changes nothing.
		got = second.exec(t, "settings", "import", "--as", secondAdmin, "--yes", file)
		if got.code != 0 || got.stdout != "imported: 0 settings changed\n" {
			t.Errorf("the same again: exit %d, stdout %q, stderr %q", got.code, got.stdout, got.stderr)
		}
	})
}

// TestSettingsImportAnswered: on a terminal, "y" imports, and prints the
// changes as text.
func TestSettingsImportAnswered(t *testing.T) {
	t.Parallel()

	st := memory.New()
	newSchool(t, st)

	file := settingsFile(t, `{"version": 1, "school": {"schoolName": "Third Example Studio"}, `+
		`"regional": {"timezone": "Europe/Vienna", "defaultLocale": "en", "currency": "EUR"}}`)

	got := cliRun{st: st, terminal: true, stdin: "y\n"}.with("NO_COLOR", "1").
		exec(t, "settings", "import", "--as", secondAdmin, file)
	if got.code != 0 || got.stderr != "Overwrite the settings with those of "+file+"? [y/N] " {
		t.Fatalf("exit %d, stderr %q", got.code, got.stderr)
	}

	golden(t, "settings/import-answered.txt", got.stdout)

	if s := schoolSettings(t, st); s.SchoolName != "Third Example Studio" || s.Timezone != "Europe/Vienna" ||
		s.Currency != "EUR" || s.DefaultLocale != "en" {
		t.Errorf("settings after the import: %+v", s)
	}
}

// TestSettingsImportWidens: booking rules that widen the release window
// generate its lessons at once, as set does.
func TestSettingsImportWidens(t *testing.T) {
	t.Parallel()

	stores(t, func(t *testing.T, r cliRun) {
		file := settingsFile(t, `{"version": 1, "bookingRules": {"releaseWeekday": 0, "releaseTime": "18:00", `+
			`"releaseWeeksAhead": 3, "bookingCutoffMinutes": 0, "cancellationCutoffMinutes": 120, `+
			`"waitlistInviteExpiryMinutes": 720, "defaultVisibleDays": 14}}`)

		got := r.exec(t, "settings", "import", "--as", demoAdmin, "--yes", file)
		if got.code != 0 || got.stderr != "" {
			t.Fatalf("import: exit %d, stderr %q", got.code, got.stderr)
		}

		golden(t, "settings/import-widens.txt", got.stdout)

		if s := schoolSettings(t, storeOf(t, r)); s.ReleaseWeeksAhead != 3 {
			t.Errorf("release weeks ahead %d, want 3", s.ReleaseWeeksAhead)
		}
	})
}

// TestSettingsImportRefusals: what import refuses changes nothing, the
// other areas of the document included: a document it cannot read (exit
// 2), an invalid value named by its place in the document (exit 2), a
// locked time zone or currency (exit 4).
func TestSettingsImportRefusals(t *testing.T) {
	t.Parallel()

	missing := filepath.Join(t.TempDir(), "missing.json")
	rules := func(field string) string {
		return `{"version": 1, "school": {"schoolName": "Renamed Studio"}, "bookingRules": {"releaseWeekday": 0, ` +
			`"releaseTime": "18:00", "releaseWeeksAhead": 1, "bookingCutoffMinutes": 0, ` +
			`"cancellationCutoffMinutes": 120, "waitlistInviteExpiryMinutes": 720, "defaultVisibleDays": 14, ` +
			field + `}}`
	}

	for name, tc := range map[string]struct {
		file   string
		code   int
		stderr string
	}{
		"missing file": {
			file: missing, code: 2, stderr: "error: open " + missing + ": no such file or directory\n",
		},
		"not JSON":    {file: settingsFile(t, `{"version": 1,`), code: 2},
		"no version":  {file: settingsFile(t, `{"school": {"schoolName": "Renamed Studio"}}`), code: 2},
		"version two": {file: settingsFile(t, `{"version": 2, "school": {"schoolName": "Renamed Studio"}}`), code: 2},
		"no area":     {file: settingsFile(t, `{"version": 1}`), code: 2},
		"unknown area": {
			file: settingsFile(t, `{"version": 1, "school": {"schoolName": "Renamed Studio"}, "colors": {}}`), code: 2,
		},
		"area not an object": {file: settingsFile(t, `{"version": 1, "school": "Renamed Studio"}`), code: 2},
		"unknown key": {
			file: settingsFile(t, `{"version": 1, "passes": {"redemptionMode": "booking", "mode": "booking"}}`),
			code: 2, stderr: "error: passes: json: unknown field \"mode\"\n",
		},
		"missing key": {
			file: settingsFile(t, `{"version": 1, "regional": {"timezone": "Europe/Berlin", "currency": "EUR"}}`),
			code: 2, stderr: "error: regional.defaultLocale: missing; an area replaces all of its settings\n",
		},
		"wrong type": {file: settingsFile(t, `{"version": 1, "passes": {"redemptionMode": 1}}`), code: 2},
		"bad weekday": {
			file: settingsFile(t, strings.Replace(rules(`"releasedThrough": "2026-12-24"`), `"releaseWeekday": 0`,
				`"releaseWeekday": 9`, 1)),
			code: 2, stderr: "error: bookingRules.releaseWeekday: invalid release weekday\n",
		},
		"far released-through": {
			file: settingsFile(t, rules(`"releasedThrough": "2028-01-01"`)), code: 2,
			stderr: "error: bookingRules.releasedThrough: released-through date must be at most 52 weeks after today\n",
		},
		"long cutoff": {
			file: settingsFile(t, strings.Replace(rules(`"releasedThrough": ""`), `"bookingCutoffMinutes": 0`,
				`"bookingCutoffMinutes": 50000`, 1)),
			code: 2, stderr: "error: bookingRules.bookingCutoffMinutes: booking cutoff must be between 0 and 30 days\n",
		},
		"locked time zone": {
			file: settingsFile(t, `{"version": 1, "school": {"schoolName": "Renamed Studio"}, `+
				`"regional": {"timezone": "America/New_York", "defaultLocale": "en", "currency": "EUR"}}`),
			code: 4, stderr: "error: import settings: conflict: the time zone cannot change once lessons exist\n",
		},
		"locked currency": {
			file: settingsFile(t, `{"version": 1, "school": {"schoolName": "Renamed Studio"}, `+
				`"regional": {"timezone": "Europe/Berlin", "defaultLocale": "en", "currency": "USD"}}`),
			code:   4,
			stderr: "error: import settings: conflict: the currency cannot change once prices or receipts exist\n",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			st := demoStore(t)
			before := schoolSettings(t, st)

			for _, dryRun := range []bool{true, false} {
				args := []string{"settings", "import", "--as", demoAdmin, "--yes", tc.file}
				if dryRun {
					args = append(args, "--dry-run")
				}

				got := cliRun{st: st}.exec(t, args...)
				if got.code != tc.code || got.stdout != "" || (tc.stderr != "" && got.stderr != tc.stderr) {
					t.Errorf("import (dry run %t): exit %d, stdout %q, stderr %q; want exit %d, stderr %q", dryRun,
						got.code, got.stdout, got.stderr, tc.code, tc.stderr)
				}
			}

			if after := schoolSettings(t, st); after != before {
				t.Errorf("the import changed the settings:\n%+v", after)
			}
		})
	}
}

// TestSettingsWithoutSchool: a database without settings has nothing to
// show or change (exit 3), in text and JSON.
func TestSettingsWithoutSchool(t *testing.T) {
	t.Parallel()

	st := memory.New()
	src := ids.NewSequence()

	err := st.InTx(t.Context(), store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		p := domain.Person{ID: ids.New[domain.PersonID](src), GivenName: "Rosa", FamilyName: "Example"}
		if err := tx.Persons().Create(ctx, p); err != nil {
			return err
		}

		return tx.Accounts().Create(ctx, domain.Account{
			ID: ids.New[domain.AccountID](src), PersonID: p.ID, Email: secondAdmin, Role: domain.RoleAdmin,
			ConfirmedAt: testNow,
		})
	})
	if err != nil {
		t.Fatal(err)
	}

	file := settingsFile(t, `{"version": 1, "passes": {"redemptionMode": "booking"}}`)

	for _, args := range [][]string{
		{"get"},
		{"get", "school", "-o", "json"},
		{"set", "school", "schoolName=Example"},
		{"set", "school", "schoolName=Example", "-o", "json"},
		{"export"},
		{"import", "--yes", file},
	} {
		got := cliRun{st: st}.exec(t, append([]string{"settings", "--as", secondAdmin}, args...)...)
		if got.code != 3 || got.stdout != "" || !strings.Contains(got.stderr, "settings: not found") {
			t.Errorf("settings %v: exit %d, stdout %q, stderr %q", args, got.code, got.stdout, got.stderr)
		}
	}
}

// failingWrites is a memory store whose writing transactions fail after
// the first: the change of a command is stored, and the generation of
// lessons in its own transaction after it fails.
type failingWrites struct {
	*memory.Store

	writes atomic.Int32
}

func (s *failingWrites) InTx(ctx context.Context, mode store.TxMode, fn func(context.Context, store.Tx) error) error {
	if mode == store.ReadWrite && s.writes.Add(1) > 1 {
		return errors.New("the database went away")
	}

	return s.Store.InTx(ctx, mode, fn)
}

// TestSettingsGenerateFails: when the lessons of a wider window cannot be
// generated at once, the change is stored all the same and stderr warns.
func TestSettingsGenerateFails(t *testing.T) {
	t.Parallel()

	const warning = "warning: generate lessons for the wider window: generate lessons: the database went away; the hourly " +
		"job generates them\n"

	st := &failingWrites{Store: demoStore(t)}

	got := cliRun{st: st}.exec(t, "settings", "set", "booking-rules", "releaseWeeksAhead=4", "--as", demoAdmin,
		"-o", "json")
	if got.code != 0 || got.stderr != warning || !strings.Contains(got.stdout, `"generatedLessons": 0`) {
		t.Errorf("set: exit %d, stdout %q, stderr %q", got.code, got.stdout, got.stderr)
	}

	if s := schoolSettings(t, st.Store); s.ReleaseWeeksAhead != 4 {
		t.Errorf("release weeks ahead %d, want 4", s.ReleaseWeeksAhead)
	}

	st = &failingWrites{Store: demoStore(t)}
	file := settingsFile(t, `{"version": 1, "bookingRules": {"releaseWeekday": 0, "releaseTime": "18:00", `+
		`"releaseWeeksAhead": 4, "bookingCutoffMinutes": 0, "cancellationCutoffMinutes": 120, `+
		`"waitlistInviteExpiryMinutes": 720, "defaultVisibleDays": 14}}`)

	got = cliRun{st: st}.exec(t, "settings", "import", "--as", demoAdmin, "--yes", file)
	if got.code != 0 || got.stderr != warning || got.stdout != "booking-rules  releaseWeeksAhead  1  4\n"+
		"imported: 1 settings changed\n" {
		t.Errorf("import: exit %d, stdout %q, stderr %q", got.code, got.stdout, got.stderr)
	}
}

// TestSettingsExportFails: a file that cannot be written fails the export.
func TestSettingsExportFails(t *testing.T) {
	t.Parallel()

	file := filepath.Join(t.TempDir(), "missing", "settings.json")

	got := cliRun{st: demoStore(t)}.exec(t, "settings", "export", "--as", demoOffice, file)
	if got.code != 1 || !strings.Contains(got.stderr, "no such file or directory") {
		t.Errorf("exit %d, stderr %q", got.code, got.stderr)
	}
}

// TestSettingsKeysDistinct: an import's field errors are named by their
// key alone (importFields), so no two areas may share one.
func TestSettingsKeysDistinct(t *testing.T) {
	t.Parallel()

	seen := map[string]string{}

	for _, a := range settingsAreas {
		for _, k := range a.keys() {
			if other, ok := seen[k.name]; ok {
				t.Errorf("the key %s is in %s and %s", k.name, other, a.name())
			}

			seen[k.name] = a.name()
		}
	}
}

func TestMinutesText(t *testing.T) {
	t.Parallel()

	for minutes, want := range map[int]string{0: "0m", 45: "45m", 90: "1h 30m", 1440: "1d", 1501: "1d 1h 1m"} {
		if got := minutesText(minutes); got != want {
			t.Errorf("minutesText(%d) = %q, want %q", minutes, got, want)
		}
	}
}
