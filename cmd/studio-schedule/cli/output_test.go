package cli

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
)

// failingWriter fails every write.
type failingWriter struct{ err error }

func (w failingWriter) Write([]byte) (int, error) { return 0, w.err }

// table prints the same table, a line and another table.
func table(t *Text) {
	t.Table("Name", "Role")
	t.Row("Olga Ledgerby", "office")
	t.Row("Clara Amberly", "")
	t.Line("%d accounts", 2)
	t.Table("Total")
	t.Row("2")
}

func TestTextPlain(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	// Not a terminal: no header lines, a dash for the empty cell, the
	// line ends the table before it.
	if err := (&Output{w: &buf}).Print(nil, table); err != nil {
		t.Fatal(err)
	}

	want := "Olga Ledgerby  office\n" +
		"Clara Amberly  -\n" +
		"2 accounts\n" +
		"2\n"
	if got := buf.String(); got != want {
		t.Errorf("text:\n%q\nwant\n%q", got, want)
	}
}

func TestTextTerminal(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	// A terminal without color: the header lines, aligned with the rows.
	if err := (&Output{w: &buf, terminal: true}).Print(nil, table); err != nil {
		t.Fatal(err)
	}

	want := "Name           Role\n" +
		"Olga Ledgerby  office\n" +
		"Clara Amberly  -\n" +
		"2 accounts\n" +
		"Total\n" +
		"2\n"
	if got := buf.String(); got != want {
		t.Errorf("text:\n%q\nwant\n%q", got, want)
	}

	// With color: the header is bold.
	buf.Reset()

	if err := (&Output{w: &buf, terminal: true, color: true}).Print(nil, table); err != nil {
		t.Fatal(err)
	}

	if got := buf.String(); !bytes.HasPrefix(buf.Bytes(), []byte("\x1b[1mName\x1b[0m")) ||
		!bytes.Contains(buf.Bytes(), []byte("\x1b[1mTotal\x1b[0m\n2\n")) {
		t.Errorf("colored text:\n%q", got)
	}
}

// TestTextRowWithoutTable: a Row without a Table starts one without a
// header, also on a terminal.
func TestTextRowWithoutTable(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	err := (&Output{w: &buf, terminal: true}).Print(nil, func(t *Text) {
		t.Row("a", "b")
		t.Row("long", "")
	})
	if err != nil {
		t.Fatal(err)
	}

	if got, want := buf.String(), "a     b\nlong  -\n"; got != want {
		t.Errorf("text = %q, want %q", got, want)
	}
}

func TestPaint(t *testing.T) {
	t.Parallel()

	plain, color := &Output{}, &Output{color: true}

	if got := plain.Paint(Red, "failed"); got != "failed" {
		t.Errorf("without color: %q", got)
	}

	if got := color.Paint(Green, "ok"); got != "\x1b[32mok\x1b[0m" {
		t.Errorf("with color: %q", got)
	}

	if got := color.Paint(Faint, ""); got != "" {
		t.Errorf("empty: %q", got)
	}

	var buf bytes.Buffer

	err := (&Output{w: &buf, color: true}).Print(nil, func(t *Text) { t.Line("%s", t.Paint(Bold, "x")) })
	if err != nil || buf.String() != "\x1b[1mx\x1b[0m\n" {
		t.Errorf("Text.Paint: %q, %v", buf.String(), err)
	}
}

func TestTextTimeAndDate(t *testing.T) {
	t.Parallel()

	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}

	at := time.Date(2026, 9, 22, 22, 30, 0, 0, time.UTC)

	for name, tc := range map[string]struct {
		loc  *time.Location
		at   time.Time
		want string
	}{
		"school's zone":    {loc: berlin, at: at, want: "2026-09-23 00:30"},
		"UTC without one":  {at: at, want: "2026-09-22 22:30"},
		"other zone given": {loc: berlin, at: at.In(time.FixedZone("far", -10*3600)), want: "2026-09-23 00:30"},
		"zero":             {loc: berlin, want: ""},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			text := &Text{out: &Output{loc: tc.loc}}
			if got := text.Time(tc.at); got != tc.want {
				t.Errorf("Time(%v) = %q, want %q", tc.at, got, tc.want)
			}
		})
	}

	text := &Text{out: &Output{}}

	d, err := domain.ParseDate("2026-09-22")
	if err != nil {
		t.Fatal(err)
	}

	if got := text.Date(d); got != "2026-09-22" {
		t.Errorf("Date = %q", got)
	}

	if got := text.Date(domain.Date{}); got != "" {
		t.Errorf("zero Date = %q", got)
	}
}

func TestPrintJSON(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	out := &Output{w: &buf, json: true, terminal: true, color: true}
	if !out.JSON() || (&Output{}).JSON() {
		t.Error("JSON() does not report the format")
	}

	textRan := false

	err := out.Print(map[string]any{"name": "Olga Ledgerby", "count": 2}, func(*Text) { textRan = true })
	if err != nil {
		t.Fatal(err)
	}

	// One indented document, no color, no text.
	if got, want := buf.String(), "{\n  \"count\": 2,\n  \"name\": \"Olga Ledgerby\"\n}\n"; got != want || textRan {
		t.Errorf("JSON = %q (text ran: %t), want %q", got, textRan, want)
	}
}

// TestPrintWriteFails: the first write error of the output is Print's.
func TestPrintWriteFails(t *testing.T) {
	t.Parallel()

	broken := errors.New("stdout is closed")

	if err := (&Output{w: failingWriter{broken}, json: true}).Print(1, nil); !errors.Is(err, broken) {
		t.Errorf("JSON: %v, want the write error", err)
	}

	for name, text := range map[string]func(t *Text){
		"table": table,
		"line":  func(t *Text) { t.Line("x"); t.Line("y") },
		// The rows are buffered: the error comes with the end of the table.
		"rows": func(t *Text) { t.Row("a"); t.Row("b") },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			for _, terminal := range []bool{false, true} {
				err := (&Output{w: failingWriter{broken}, terminal: terminal}).Print(nil, text)
				if !errors.Is(err, broken) {
					t.Errorf("terminal %t: %v, want the write error", terminal, err)
				}
			}
		})
	}
}
