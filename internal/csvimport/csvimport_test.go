package csvimport_test

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"golang.org/x/text/encoding/charmap"

	"github.com/cwbudde/studio-schedule/internal/csvimport"
	"github.com/cwbudde/studio-schedule/internal/domain"
)

func TestParse(t *testing.T) {
	t.Parallel()

	cp1252, err := charmap.Windows1252.NewEncoder().String("Name;Straße\r\nJürgen;Hauptstraße 1 – Hof\r\n")
	if err != nil {
		t.Fatal(err)
	}

	for name, tc := range map[string]struct {
		data     string
		encoding string
		delim    rune
		header   []string
		rows     [][]string
		lines    []int
	}{
		"windows-1252": {
			cp1252, csvimport.Windows1252, ';',
			[]string{"Name", "Straße"},
			[][]string{{"Jürgen", "Hauptstraße 1 – Hof"}},
			[]int{2},
		},
		"utf-8 with BOM and commas": {
			"\xEF\xBB\xBFa,b\n1,2\n", csvimport.UTF8, ',', []string{"a", "b"}, [][]string{{"1", "2"}}, []int{2},
		},
		"tabs": {
			"a\tb\tc;d\n1\t2\t3;4\n", csvimport.UTF8, '\t',
			[]string{"a", "b", "c;d"},
			[][]string{{"1", "2", "3;4"}},
			[]int{2},
		},
		"a quoted delimiter does not count": {
			"\"a,b,c\";d\n1;2\n", csvimport.UTF8, ';', []string{"a,b,c", "d"}, [][]string{{"1", "2"}}, []int{2},
		},
		"semicolon on a tie": {"a\n1\n", csvimport.UTF8, ';', []string{"a"}, [][]string{{"1"}}, []int{2}},
		"trimmed, short rows filled, empty rows dropped": {
			" a ; b \n x ;\n;\n\n y \n", csvimport.UTF8, ';',
			[]string{"a", "b"},
			[][]string{{"x", ""}, {"y", ""}},
			[]int{2, 5},
		},
		"empty trailing cells": {"a;b\n1;2;;\n", csvimport.UTF8, ';', []string{"a", "b"}, [][]string{{"1", "2"}}, []int{2}},
		"a cell over two lines": {
			"a;b\n\"x\ny\";2\n3;4\n", csvimport.UTF8, ';',
			[]string{"a", "b"},
			[][]string{{"x\ny", "2"}, {"3", "4"}},
			[]int{2, 4},
		},
		"header only": {"a;b\n", csvimport.UTF8, ';', []string{"a", "b"}, nil, nil},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := csvimport.Parse([]byte(tc.data))
			if err != nil {
				t.Fatal(err)
			}

			if got.Encoding != tc.encoding || got.Delimiter != tc.delim || !slices.Equal(got.Header, tc.header) {
				t.Errorf("got %s %q %q", got.Encoding, got.Delimiter, got.Header)
			}

			if len(got.Rows) != len(tc.rows) {
				t.Fatalf("rows %+v, want %q", got.Rows, tc.rows)
			}

			for i, r := range got.Rows {
				if !slices.Equal(r.Cells, tc.rows[i]) || r.Line != tc.lines[i] {
					t.Errorf("row %d: %+v, want %q on line %d", i, r, tc.rows[i], tc.lines[i])
				}
			}
		})
	}
}

func TestParseRejects(t *testing.T) {
	t.Parallel()

	tooMany := "a\n" + strings.Repeat("1\n", csvimport.MaxRows+1)

	var columns []string
	for i := range csvimport.MaxColumns + 1 {
		columns = append(columns, fmt.Sprintf("c%d", i))
	}

	for name, data := range map[string]string{
		"empty":            "",
		"blank":            " \r\n\n",
		"empty header":     "a;;b\n",
		"repeated header":  "Name;name\n",
		"too many cells":   "a;b\n1;2;3\n",
		"too large":        strings.Repeat("x", csvimport.MaxBytes+1),
		"too many rows":    tooMany,
		"too many columns": strings.Join(columns, ";") + "\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := csvimport.Parse([]byte(data))

			var fe *csvimport.FileError
			if !errors.As(err, &fe) || !errors.Is(err, domain.ErrValidation) {
				t.Errorf("got %v, want a FileError", err)
			}
		})
	}
}

func TestParseDate(t *testing.T) {
	t.Parallel()

	for in, want := range map[string]domain.Date{
		"03.04.2015": {Year: 2015, Month: time.April, Day: 3},
		"3.4.2015":   {Year: 2015, Month: time.April, Day: 3},
		"2015-04-03": {Year: 2015, Month: time.April, Day: 3},
		"29.02.2016": {Year: 2016, Month: time.February, Day: 29},
	} {
		if got, err := csvimport.ParseDate(in); err != nil || got != want {
			t.Errorf("ParseDate(%q) = %v, %v; want %v", in, got, err, want)
		}
	}

	for _, in := range []string{
		"", "2015", "03/04/2015", "31.02.2016", "29.02.2015", "03.04.15", "2015-4-3x", "1.1.+201", "-1.1.2015",
		"001.01.2015", "15-04-03", "2015.04.03",
	} {
		if got, err := csvimport.ParseDate(in); !errors.Is(err, csvimport.ErrDate) {
			t.Errorf("ParseDate(%q) = %v, %v; want ErrDate", in, got, err)
		}
	}
}
