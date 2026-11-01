package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
)

// Output formats.
const (
	FormatText = "text"
	FormatJSON = "json"
)

// Style is a color of the text output.
type Style string

// The styles. Text is only colored on a terminal without NO_COLOR.
const (
	Bold  Style = "\x1b[1m"
	Green Style = "\x1b[32m"
	Red   Style = "\x1b[31m"
	Faint Style = "\x1b[2m"
)

const resetStyle = "\x1b[0m"

// Output is the presenter of a command's result on stdout: text tables, or
// one JSON document with --output json (ADR 0020).
type Output struct {
	w        io.Writer
	json     bool
	terminal bool
	color    bool
	loc      *time.Location
}

// JSON reports whether the command prints JSON (--output json).
func (o *Output) JSON() bool { return o.json }

// Print presents a result: body as one JSON document with --output json,
// else text writes it as text. Where the command mirrors an API operation,
// body is the operation's response body (a type of internal/httpapi).
func (o *Output) Print(body any, text func(t *Text)) error {
	if o.json {
		enc := json.NewEncoder(o.w)
		enc.SetIndent("", "  ")
		// Text as the API sends it: "<" in "Studio <office@example.org>"
		// stays "<", not \u003c.
		enc.SetEscapeHTML(false)

		return enc.Encode(body)
	}

	t := &Text{out: o}
	text(t)

	return t.flush()
}

// Paint returns s in style on a color terminal, else s.
func (o *Output) Paint(style Style, s string) string {
	if !o.color || s == "" {
		return s
	}

	return string(style) + s + resetStyle
}

// location is the school's time zone, UTC before the school has settings.
func (o *Output) location() *time.Location {
	if o.loc == nil {
		return time.UTC
	}

	return o.loc
}

// Text writes a command's text output: lines and tables. The first write
// error ends the output; Print returns it.
type Text struct {
	out *Output
	// rows are the records of the current table, aligned when it ends;
	// inTable reports whether one is open.
	rows    [][]string
	inTable bool
	err     error
}

// tablePadding separates the columns of a table.
const tablePadding = 2

// Table starts a table with the given column names: one record per Row,
// the columns aligned, and the header line only on a terminal, so a script
// reads the records alone. It ends with the next Line or Table, or with the
// output.
func (t *Text) Table(header ...string) {
	t.endTable()

	t.inTable = true

	if t.out.terminal && len(header) > 0 {
		cells := make([]string, len(header))
		for i, h := range header {
			cells[i] = t.out.Paint(Bold, h)
		}

		t.rows = append(t.rows, cells)
	}
}

// Row adds a record to the table. An empty cell is a dash, so the columns
// of a record stay countable.
func (t *Text) Row(cells ...string) {
	if !t.inTable {
		t.Table()
	}

	out := make([]string, len(cells))
	for i, c := range cells {
		out[i] = c
		if c == "" {
			out[i] = "-"
		}
	}

	t.rows = append(t.rows, out)
}

// Line writes a line of text, after the table before it.
func (t *Text) Line(format string, args ...any) {
	t.endTable()

	if t.err == nil {
		_, t.err = fmt.Fprintf(t.out.w, format+"\n", args...)
	}
}

// Paint is Output.Paint.
func (t *Text) Paint(style Style, s string) string { return t.out.Paint(style, s) }

// Time formats an instant in the school's time zone, never the machine's;
// the zero time is empty.
func (t *Text) Time(at time.Time) string {
	if at.IsZero() {
		return ""
	}

	return at.In(t.out.location()).Format("2006-01-02 15:04")
}

// Date formats a school date; the zero date is empty.
func (t *Text) Date(d domain.Date) string {
	if d.IsZero() {
		return ""
	}

	return d.String()
}

// endTable ends the current table: it writes the records with every
// column but the last padded to its widest cell. The width is what a
// terminal shows, without the escape sequences of Paint, so a colored
// cell aligns with a plain one.
func (t *Text) endTable() {
	if !t.inTable {
		return
	}

	var widths []int

	for _, row := range t.rows {
		for i, c := range row[:max(len(row)-1, 0)] {
			if i == len(widths) {
				widths = append(widths, 0)
			}

			widths[i] = max(widths[i], visibleWidth(c))
		}
	}

	for _, row := range t.rows {
		var line strings.Builder

		for i, c := range row {
			line.WriteString(c)

			if i < len(row)-1 {
				line.WriteString(strings.Repeat(" ", widths[i]-visibleWidth(c)+tablePadding))
			}
		}

		if t.err == nil {
			_, t.err = fmt.Fprintln(t.out.w, line.String())
		}
	}

	t.rows, t.inTable = nil, false
}

// visibleWidth is the number of runes of s outside its escape sequences
// (ESC [ … m, the Styles).
func visibleWidth(s string) int {
	n, escape := 0, false

	for _, r := range s {
		switch {
		case escape:
			escape = r != 'm'
		case r == '\x1b':
			escape = true
		default:
			n++
		}
	}

	return n
}

func (t *Text) flush() error {
	t.endTable()

	return t.err
}

// Money is an amount in minor units of currency as text output shows it:
// the decimal number (Decimal) and the currency's code, "250.00 EUR".
func Money(m domain.Money, currency string) string {
	return Decimal(m, currency) + " " + currency
}

// Decimal is an amount in minor units as a decimal number with the
// currency's minor digits and a point, e.g. "-15.00": no grouping and no
// symbol, so a script or a spreadsheet reads it in any language.
func Decimal(m domain.Money, currency string) string {
	digits, ok := domain.CurrencyExponent(currency)
	if !ok {
		digits = 2 // the settings allow only known currencies
	}

	s := strconv.FormatInt(int64(m), 10)

	sign := ""
	if m < 0 {
		sign, s = "-", s[1:]
	}

	if digits == 0 {
		return sign + s
	}

	s = strings.Repeat("0", max(digits+1-len(s), 0)) + s

	return sign + s[:len(s)-digits] + "." + s[len(s)-digits:]
}
