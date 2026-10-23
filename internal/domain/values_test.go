package domain_test

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
)

func TestDateRoundTrip(t *testing.T) {
	t.Parallel()

	d, err := domain.ParseDate("2026-02-28")
	if err != nil {
		t.Fatalf("ParseDate: %v", err)
	}

	if want := (domain.Date{Year: 2026, Month: time.February, Day: 28}); d != want {
		t.Fatalf("ParseDate = %+v, want %+v", d, want)
	}

	if got := d.String(); got != "2026-02-28" {
		t.Fatalf("String = %q", got)
	}

	data, err := json.Marshal(d)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var back domain.Date
	if err := json.Unmarshal(data, &back); err != nil || back != d {
		t.Fatalf("JSON round trip = %+v, %v", back, err)
	}
}

func TestDateZeroJSON(t *testing.T) {
	t.Parallel()

	type payload struct {
		D domain.Date `json:"d"`
	}

	data, err := json.Marshal(payload{})
	if err != nil || string(data) != `{"d":""}` {
		t.Fatalf("Marshal zero = %s, %v", data, err)
	}

	back := payload{D: domain.Date{Year: 2026, Month: time.March, Day: 1}}
	if err := json.Unmarshal(data, &back); err != nil || !back.D.IsZero() {
		t.Fatalf("Unmarshal zero = %+v, %v", back.D, err)
	}

	if _, err := json.Marshal(payload{D: domain.Date{Year: 2026, Month: 2, Day: 30}}); err == nil {
		t.Fatal("Marshal of an invalid date succeeded")
	}
}

func TestDateInvalid(t *testing.T) {
	t.Parallel()

	for _, s := range []string{"", "2026-2-28", "2026-02-30", "26-02-28", "2026-02-28T00:00:00Z", "0000-01-01"} {
		if _, err := domain.ParseDate(s); !errors.Is(err, domain.ErrValidation) {
			t.Errorf("ParseDate(%q): want ErrValidation, got %v", s, err)
		}
	}

	for _, d := range []domain.Date{{}, {Year: 2026, Month: 13, Day: 1}, {Year: 2025, Month: time.February, Day: 29}} {
		if d.Valid() {
			t.Errorf("%+v should be invalid", d)
		}
	}
}

func TestDateCompare(t *testing.T) {
	t.Parallel()

	a := domain.Date{Year: 2026, Month: time.January, Day: 31}
	b := domain.Date{Year: 2026, Month: time.February, Day: 1}

	if a.Compare(b) >= 0 || b.Compare(a) <= 0 || a.Compare(a) != 0 {
		t.Fatalf("Compare is not ordered: %d %d %d", a.Compare(b), b.Compare(a), a.Compare(a))
	}

	if !(domain.Date{}).IsZero() || a.IsZero() {
		t.Fatal("IsZero mismatch")
	}
}

func TestTimeOfDayRoundTrip(t *testing.T) {
	t.Parallel()

	tod, err := domain.ParseTimeOfDay("07:05")
	if err != nil {
		t.Fatalf("ParseTimeOfDay: %v", err)
	}

	if want := (domain.TimeOfDay{Hour: 7, Minute: 5}); tod != want {
		t.Fatalf("ParseTimeOfDay = %+v, want %+v", tod, want)
	}

	if got := tod.String(); got != "07:05" {
		t.Fatalf("String = %q", got)
	}

	data, err := json.Marshal(tod)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var back domain.TimeOfDay
	if err := json.Unmarshal(data, &back); err != nil || back != tod {
		t.Fatalf("JSON round trip = %+v, %v", back, err)
	}
}

func TestTimeOfDayInvalid(t *testing.T) {
	t.Parallel()

	for _, s := range []string{"", "7:05", "24:00", "12:60", "12:00:00", "ab:cd"} {
		if _, err := domain.ParseTimeOfDay(s); !errors.Is(err, domain.ErrValidation) {
			t.Errorf("ParseTimeOfDay(%q): want ErrValidation, got %v", s, err)
		}
	}

	if (domain.TimeOfDay{Hour: 24}).Valid() || (domain.TimeOfDay{Minute: -1}).Valid() {
		t.Fatal("out-of-range time of day reported valid")
	}
}

func TestTimeOfDayCompare(t *testing.T) {
	t.Parallel()

	a := domain.TimeOfDay{Hour: 9, Minute: 59}
	b := domain.TimeOfDay{Hour: 10}

	if a.Compare(b) >= 0 || b.Compare(a) <= 0 || a.Compare(a) != 0 {
		t.Fatal("Compare is not ordered")
	}

	if got := b.SinceMidnight(); got != 10*time.Hour {
		t.Fatalf("SinceMidnight = %v", got)
	}
}

func TestDateAddDaysAndWeekday(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		from string
		days int
		want string
	}{
		{"2026-02-28", 1, "2026-03-01"},
		{"2028-02-28", 1, "2028-02-29"},
		{"2027-01-01", -1, "2026-12-31"},
		{"2026-10-23", 365, "2027-10-23"},
		{"2026-10-23", 0, "2026-10-23"},
	} {
		d, err := domain.ParseDate(tc.from)
		if err != nil {
			t.Fatal(err)
		}

		if got := d.AddDays(tc.days).String(); got != tc.want {
			t.Errorf("%s.AddDays(%d) = %s, want %s", tc.from, tc.days, got, tc.want)
		}
	}

	if got := (domain.Date{Year: 2026, Month: time.October, Day: 23}).Weekday(); got != time.Friday {
		t.Errorf("2026-10-23 weekday = %v, want Friday", got)
	}
}

func TestDateAt(t *testing.T) {
	t.Parallel()

	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}

	utc := func(s string) time.Time {
		t.Helper()

		v, err := time.Parse(time.RFC3339, s)
		if err != nil {
			t.Fatal(err)
		}

		return v
	}

	for name, tc := range map[string]struct {
		date domain.Date
		at   domain.TimeOfDay
		want time.Time
	}{
		"summer time":   {domain.Date{Year: 2026, Month: time.October, Day: 24}, domain.TimeOfDay{Hour: 18}, utc("2026-10-24T16:00:00Z")},
		"winter time":   {domain.Date{Year: 2026, Month: time.October, Day: 26}, domain.TimeOfDay{Hour: 18}, utc("2026-10-26T17:00:00Z")},
		"fall-back day": {domain.Date{Year: 2026, Month: time.October, Day: 25}, domain.TimeOfDay{Hour: 18}, utc("2026-10-25T17:00:00Z")},
		// 02:30 happens twice on 2026-10-25; the earlier instant (CEST) wins.
		"ambiguous": {domain.Date{Year: 2026, Month: time.October, Day: 25}, domain.TimeOfDay{Hour: 2, Minute: 30}, utc("2026-10-25T00:30:00Z")},
		// 02:30 does not exist on 2027-03-28; it moves forward to 03:30 CEST.
		"gap":              {domain.Date{Year: 2027, Month: time.March, Day: 28}, domain.TimeOfDay{Hour: 2, Minute: 30}, utc("2027-03-28T01:30:00Z")},
		"spring-ahead day": {domain.Date{Year: 2027, Month: time.March, Day: 28}, domain.TimeOfDay{Hour: 18}, utc("2027-03-28T16:00:00Z")},
		"utc":              {domain.Date{Year: 2026, Month: time.October, Day: 25}, domain.TimeOfDay{Hour: 2, Minute: 30}, utc("2026-10-25T02:30:00Z")},
	} {
		loc := berlin
		if name == "utc" {
			loc = time.UTC
		}

		if got := tc.date.At(tc.at, loc); !got.Equal(tc.want) {
			t.Errorf("%s: %v.At(%v) = %v, want %v", name, tc.date, tc.at, got.UTC(), tc.want)
		}
	}
}

func TestDateOf(t *testing.T) {
	t.Parallel()

	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}

	instant := time.Date(2026, time.October, 24, 22, 30, 0, 0, time.UTC) // 00:30 CEST on the 25th

	if got, want := domain.DateOf(instant, berlin), (domain.Date{Year: 2026, Month: time.October, Day: 25}); got != want {
		t.Errorf("DateOf(Berlin) = %v, want %v", got, want)
	}

	if got, want := domain.DateOf(instant, time.UTC), (domain.Date{Year: 2026, Month: time.October, Day: 24}); got != want {
		t.Errorf("DateOf(UTC) = %v, want %v", got, want)
	}
}
