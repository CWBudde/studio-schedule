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
