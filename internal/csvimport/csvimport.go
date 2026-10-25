// Package csvimport reads the CSV files of the member import (ADR 0015) as
// spreadsheets export them: UTF-8 (with or without a byte order mark) or
// Windows-1252, separated by semicolons, commas or tabs, with dates as
// DD.MM.YYYY or YYYY-MM-DD. It knows nothing about persons; the service maps
// the columns.
package csvimport

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"

	"github.com/cwbudde/studio-schedule/internal/domain"
)

// Limits of a file.
const (
	// MaxBytes is the largest file, before decoding.
	MaxBytes = 1 << 20
	// MaxRows is the most data rows a file may have.
	MaxRows = 5000
	// MaxColumns is the most columns a file may have.
	MaxColumns = 100
)

// Encodings Parse tells apart.
const (
	UTF8        = "utf-8"
	Windows1252 = "windows-1252"
)

// FileError reports a file that cannot be read as a table, with why. It
// wraps domain.ErrValidation.
type FileError struct {
	Msg string
}

func (e *FileError) Error() string { return "csvimport: " + e.Msg }

func (e *FileError) Unwrap() error { return domain.ErrValidation }

// Table is a parsed file: its header and its data rows. Cells are trimmed;
// rows that are entirely empty are left out.
type Table struct {
	// Encoding is UTF8 or Windows1252.
	Encoding string
	// Delimiter is ';', ',' or '\t'.
	Delimiter rune
	Header    []string
	Rows      []Row
}

// Row is a data row with its line in the file (the header's is 1, or later
// when a quoted cell spans lines). It has as many cells as the header:
// missing ones are empty, extra ones an error of Parse.
type Row struct {
	Line  int
	Cells []string
}

// Parse decodes and splits a file. A file that is empty, too large, not
// a table, without a header, with an empty or repeated (ignoring case)
// column name, or with more cells in a row than the header names, is a
// *FileError.
func Parse(data []byte) (Table, error) {
	if len(data) > MaxBytes {
		return Table{}, fileErr("the file is larger than %d KiB", MaxBytes>>10)
	}

	text, encoding := decode(data)
	if strings.TrimSpace(text) == "" {
		return Table{}, fileErr("the file is empty")
	}

	delim := detectDelimiter(text)

	r := csv.NewReader(strings.NewReader(text))
	r.Comma = delim
	r.FieldsPerRecord = -1
	r.LazyQuotes = true

	t := Table{Encoding: encoding, Delimiter: delim}

	for {
		record, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			return Table{}, fileErr("%v", err)
		}

		line, _ := r.FieldPos(0)

		cells := make([]string, len(record))
		for i, c := range record {
			cells[i] = strings.TrimSpace(c)
		}

		if t.Header == nil {
			if err := checkHeader(cells); err != nil {
				return Table{}, err
			}

			t.Header = cells

			continue
		}

		if empty(cells) {
			continue
		}

		if len(cells) > len(t.Header) {
			if !empty(cells[len(t.Header):]) {
				return Table{}, fileErr("line %d has more cells than the header names", line)
			}

			cells = cells[:len(t.Header)]
		}

		for len(cells) < len(t.Header) {
			cells = append(cells, "")
		}

		if len(t.Rows) == MaxRows {
			return Table{}, fileErr("the file has more than %d rows", MaxRows)
		}

		t.Rows = append(t.Rows, Row{Line: line, Cells: cells})
	}

	if t.Header == nil {
		return Table{}, fileErr("the file has no header")
	}

	return t, nil
}

// utf8BOM is the byte order mark some programs put before UTF-8.
var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

func fileErr(format string, args ...any) error {
	return &FileError{Msg: fmt.Sprintf(format, args...)}
}

// decode returns the text of data and its encoding: UTF-8 without its byte
// order mark if data is valid UTF-8, and Windows-1252 otherwise, which
// decodes any byte (the encoding German spreadsheets save CSV in).
func decode(data []byte) (string, string) {
	data = bytes.TrimPrefix(data, utf8BOM)
	if utf8.Valid(data) {
		return string(data), UTF8
	}

	text, err := charmap.Windows1252.NewDecoder().Bytes(data)
	if err != nil { // Windows-1252 maps every byte; never happens
		return strings.ToValidUTF8(string(data), string(utf8.RuneError)), Windows1252
	}

	return string(text), Windows1252
}

// detectDelimiter returns the separator the header line uses most, outside
// quotes: ';' (what spreadsheets with a decimal comma write), ',' or a tab;
// ';' on a tie or none.
func detectDelimiter(text string) rune {
	counts := map[rune]int{}
	quoted := false

	for _, c := range text {
		switch {
		case c == '"':
			quoted = !quoted
		case quoted:
		case c == '\n':
			return best(counts)
		case c == ';' || c == ',' || c == '\t':
			counts[c]++
		}
	}

	return best(counts)
}

func best(counts map[rune]int) rune {
	delim := ';'

	for _, c := range []rune{',', '\t'} {
		if counts[c] > counts[delim] {
			delim = c
		}
	}

	return delim
}

func checkHeader(cells []string) error {
	if len(cells) > MaxColumns {
		return fileErr("the file has more than %d columns", MaxColumns)
	}

	seen := make(map[string]bool, len(cells))

	for i, c := range cells {
		key := strings.ToLower(c)

		switch {
		case c == "":
			return fileErr("column %d of the header has no name", i+1)
		case seen[key]:
			return fileErr("the header names the column %q twice", c)
		}

		seen[key] = true
	}

	return nil
}

func empty(cells []string) bool {
	for _, c := range cells {
		if c != "" {
			return false
		}
	}

	return true
}

// ErrDate reports a date in neither form ParseDate takes.
var ErrDate = errors.New("csvimport: a date must be DD.MM.YYYY or YYYY-MM-DD")

// ParseDate reads a date as DD.MM.YYYY (days and months may have one
// digit) or YYYY-MM-DD. A date that does not exist, such as 30.02.2015, is
// ErrDate as well.
func ParseDate(s string) (domain.Date, error) {
	var parts []string

	switch {
	case strings.Count(s, ".") == 2:
		p := strings.Split(s, ".")
		parts = []string{p[2], p[1], p[0]}
	case strings.Count(s, "-") == 2:
		parts = strings.Split(s, "-")
	default:
		return domain.Date{}, ErrDate
	}

	n := make([]int, 3)

	for i, p := range parts {
		limit := 2
		if i == 0 {
			limit = 4
		}

		v, err := strconv.Atoi(p)
		if err != nil || p == "" || len(p) > limit || (i == 0 && len(p) != 4) || p[0] == '+' || p[0] == '-' {
			return domain.Date{}, ErrDate
		}

		n[i] = v
	}

	d := domain.Date{Year: n[0], Month: time.Month(n[1]), Day: n[2]}
	if !d.Valid() {
		return domain.Date{}, ErrDate
	}

	return d, nil
}
