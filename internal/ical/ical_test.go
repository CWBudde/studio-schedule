package ical_test

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/cwbudde/studio-schedule/internal/ical"
)

func mustZone(t *testing.T, name string) *time.Location {
	t.Helper()

	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatal(err)
	}

	return loc
}

func TestFold(t *testing.T) {
	t.Parallel()

	for name, in := range map[string]string{
		"short":      "SUMMARY:Ballet",
		"exactly 75": "SUMMARY:" + strings.Repeat("a", 75-len("SUMMARY:")),
		"ascii":      "DESCRIPTION:" + strings.Repeat("abcdefghij", 20),
		// Two- and three-byte runes and a four-byte emoji, so cuts land
		// inside sequences.
		"multi-byte": "SUMMARY:" + strings.Repeat("Übung–Ä😀ß", 15),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			out := ical.Fold(in)
			if !strings.HasSuffix(out, "\r\n") {
				t.Fatalf("%q does not end in CRLF", out)
			}

			lines := strings.Split(strings.TrimSuffix(out, "\r\n"), "\r\n")

			var joined strings.Builder

			for i, l := range lines {
				if len(l) > 75 {
					t.Errorf("line %d has %d octets: %q", i, len(l), l)
				}

				if i > 0 {
					if !strings.HasPrefix(l, " ") {
						t.Errorf("continuation line %d does not start with a space: %q", i, l)
					}

					l = l[1:]
				}

				if !utf8.ValidString(l) {
					t.Errorf("line %d splits a UTF-8 sequence: %q", i, l)
				}

				joined.WriteString(l)
			}

			if joined.String() != in {
				t.Errorf("unfolded %q, want %q", joined.String(), in)
			}

			if len(in) <= 75 && len(lines) != 1 {
				t.Errorf("a line of %d octets was folded", len(in))
			}
		})
	}
}

func TestText(t *testing.T) {
	t.Parallel()

	for in, want := range map[string]string{
		"Ballet":                  "Ballet",
		`a\b`:                     `a\\b`,
		"Room A; North, Studio":   `Room A\; North\, Studio`,
		"line 1\nline 2":          `line 1\nline 2`,
		"line 1\r\nline 2\rend":   `line 1\nline 2\nend`,
		"tab\there\x00\x07\x7fok": "tab\there" + "ok",
		"Grüße – ü":               "Grüße – ü",
	} {
		if got := ical.Text(in); got != want {
			t.Errorf("Text(%q) = %q, want %q", in, got, want)
		}
	}
}

// vtimezone returns the VTIMEZONE of a calendar in zone with one event at
// start, unfolded.
func vtimezone(t *testing.T, zone *time.Location, start time.Time) string {
	t.Helper()

	doc := string(ical.Encode(ical.Calendar{
		Name: "Test", Zone: zone, Stamp: start,
		Events: []ical.Event{{UID: "1@example.org", Start: start, End: start.Add(time.Hour), Summary: "x"}},
	}).Body)

	begin, end := strings.Index(doc, "BEGIN:VTIMEZONE"), strings.Index(doc, "END:VTIMEZONE")
	if begin < 0 || end < 0 {
		t.Fatalf("no VTIMEZONE in\n%s", doc)
	}

	return doc[begin : end+len("END:VTIMEZONE\r\n")]
}

func TestTimezone(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		zone  string
		start time.Time
		want  string
	}{
		{
			// The October change of 2025 sets the offset in effect when 2026
			// begins; then both changes of 2026.
			zone:  "Europe/Berlin",
			start: time.Date(2026, time.March, 30, 16, 30, 0, 0, time.UTC),
			want: "BEGIN:VTIMEZONE\r\nTZID:Europe/Berlin\r\n" +
				"BEGIN:STANDARD\r\nDTSTART:20251026T030000\r\nTZOFFSETFROM:+0200\r\nTZOFFSETTO:+0100\r\nTZNAME:CET\r\nEND:STANDARD\r\n" +
				"BEGIN:DAYLIGHT\r\nDTSTART:20260329T020000\r\nTZOFFSETFROM:+0100\r\nTZOFFSETTO:+0200\r\nTZNAME:CEST\r\nEND:DAYLIGHT\r\n" +
				"BEGIN:STANDARD\r\nDTSTART:20261025T030000\r\nTZOFFSETFROM:+0200\r\nTZOFFSETTO:+0100\r\nTZNAME:CET\r\nEND:STANDARD\r\n" +
				"END:VTIMEZONE\r\n",
		},
		{
			zone:  "America/New_York",
			start: time.Date(2026, time.July, 1, 22, 0, 0, 0, time.UTC),
			want: "BEGIN:VTIMEZONE\r\nTZID:America/New_York\r\n" +
				"BEGIN:STANDARD\r\nDTSTART:20251102T020000\r\nTZOFFSETFROM:-0400\r\nTZOFFSETTO:-0500\r\nTZNAME:EST\r\nEND:STANDARD\r\n" +
				"BEGIN:DAYLIGHT\r\nDTSTART:20260308T020000\r\nTZOFFSETFROM:-0500\r\nTZOFFSETTO:-0400\r\nTZNAME:EDT\r\nEND:DAYLIGHT\r\n" +
				"BEGIN:STANDARD\r\nDTSTART:20261101T020000\r\nTZOFFSETFROM:-0400\r\nTZOFFSETTO:-0500\r\nTZNAME:EST\r\nEND:STANDARD\r\n" +
				"END:VTIMEZONE\r\n",
		},
		{
			// No daylight saving time: one sub-component.
			zone:  "Asia/Tokyo",
			start: time.Date(2026, time.May, 4, 9, 0, 0, 0, time.UTC),
			want: "BEGIN:VTIMEZONE\r\nTZID:Asia/Tokyo\r\n" +
				"BEGIN:STANDARD\r\nDTSTART:19700101T000000\r\nTZOFFSETFROM:+0900\r\nTZOFFSETTO:+0900\r\nTZNAME:JST\r\nEND:STANDARD\r\n" +
				"END:VTIMEZONE\r\n",
		},
		{
			// The southern hemisphere: daylight saving time over New Year.
			zone:  "Australia/Sydney",
			start: time.Date(2026, time.June, 1, 8, 0, 0, 0, time.UTC),
			want: "BEGIN:VTIMEZONE\r\nTZID:Australia/Sydney\r\n" +
				"BEGIN:DAYLIGHT\r\nDTSTART:20251005T020000\r\nTZOFFSETFROM:+1000\r\nTZOFFSETTO:+1100\r\nTZNAME:AEDT\r\nEND:DAYLIGHT\r\n" +
				"BEGIN:STANDARD\r\nDTSTART:20260405T030000\r\nTZOFFSETFROM:+1100\r\nTZOFFSETTO:+1000\r\nTZNAME:AEST\r\nEND:STANDARD\r\n" +
				"BEGIN:DAYLIGHT\r\nDTSTART:20261004T020000\r\nTZOFFSETFROM:+1000\r\nTZOFFSETTO:+1100\r\nTZNAME:AEDT\r\nEND:DAYLIGHT\r\n" +
				"END:VTIMEZONE\r\n",
		},
	} {
		t.Run(tc.zone, func(t *testing.T) {
			t.Parallel()

			if got := vtimezone(t, mustZone(t, tc.zone), tc.start); got != tc.want {
				t.Errorf("got\n%s\nwant\n%s", got, tc.want)
			}
		})
	}
}

// TestTimezoneSpansYears covers events in two years: the transitions of
// both.
func TestTimezoneSpansYears(t *testing.T) {
	t.Parallel()

	zone := mustZone(t, "Europe/Berlin")
	doc := string(ical.Encode(ical.Calendar{
		Name: "Test", Zone: zone, Stamp: time.Date(2026, time.December, 1, 0, 0, 0, 0, time.UTC),
		Events: []ical.Event{
			{UID: "1@x", Start: time.Date(2026, time.December, 1, 17, 0, 0, 0, zone), End: time.Date(2026, time.December, 1, 18, 0, 0, 0, zone)},
			{UID: "2@x", Start: time.Date(2027, time.April, 1, 17, 0, 0, 0, zone), End: time.Date(2027, time.April, 1, 18, 0, 0, 0, zone)},
		},
	}).Body)

	for _, onset := range []string{"20251026T030000", "20260329T020000", "20261025T030000", "20270328T020000", "20271031T030000"} {
		if !strings.Contains(doc, "DTSTART:"+onset+"\r\n") {
			t.Errorf("no transition at %s in\n%s", onset, doc)
		}
	}
}

func TestEncode(t *testing.T) {
	t.Parallel()

	zone := mustZone(t, "Europe/Berlin")
	stamp := time.Date(2026, time.October, 20, 8, 15, 0, 0, time.UTC)

	cal := ical.Calendar{
		Name: "Example School – Schedule", Zone: zone, Stamp: stamp,
		Events: []ical.Event{
			{
				UID: "a@school.example", Start: time.Date(2026, time.October, 26, 16, 30, 0, 0, zone),
				End: time.Date(2026, time.October, 26, 17, 30, 0, 0, zone), Summary: "Ballet, beginners",
				Location: "North; Studio A", Description: "Line 1\nLine 2", URL: "https://school.example/schedule?lesson=a",
				Status: ical.StatusConfirmed,
			},
			{
				// 02:30 on the night the clocks go back exists twice: the
				// second one is written in UTC.
				UID: "b@school.example", Start: time.Date(2026, time.October, 25, 1, 30, 0, 0, time.UTC),
				End: time.Date(2026, time.October, 25, 2, 0, 0, 0, time.UTC), Summary: "Night",
				Status: ical.StatusCancelled,
			},
			{
				// The first 02:30 of that night keeps its local time.
				UID: "c@school.example", Start: time.Date(2026, time.October, 25, 0, 30, 0, 0, time.UTC),
				End: time.Date(2026, time.October, 25, 1, 0, 0, 0, time.UTC), Summary: "Earlier",
				Status: ical.StatusConfirmed,
			},
		},
	}

	doc := string(ical.Encode(cal).Body)

	for _, want := range []string{
		"BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//studio-schedule//EN\r\nCALSCALE:GREGORIAN\r\nMETHOD:PUBLISH\r\n",
		"NAME:Example School – Schedule\r\nX-WR-CALNAME:Example School – Schedule\r\nX-WR-TIMEZONE:Europe/Berlin\r\n",
		"REFRESH-INTERVAL;VALUE=DURATION:PT1H\r\nX-PUBLISHED-TTL:PT1H\r\n",
		"BEGIN:VEVENT\r\nUID:a@school.example\r\nDTSTAMP:20261020T081500Z\r\n" +
			"DTSTART;TZID=Europe/Berlin:20261026T163000\r\nDTEND;TZID=Europe/Berlin:20261026T173000\r\n" +
			"SUMMARY:Ballet\\, beginners\r\nLOCATION:North\\; Studio A\r\nDESCRIPTION:Line 1\\nLine 2\r\n" +
			"URL;VALUE=URI:https://school.example/schedule?lesson=a\r\nSTATUS:CONFIRMED\r\nSEQUENCE:0\r\n" +
			"TRANSP:OPAQUE\r\nEND:VEVENT\r\n",
		"DTSTART:20261025T013000Z\r\nDTEND;TZID=Europe/Berlin:20261025T030000\r\nSUMMARY:Night\r\n" +
			"STATUS:CANCELLED\r\nSEQUENCE:0\r\nTRANSP:TRANSPARENT\r\nEND:VEVENT\r\n", //nolint:misspell // RFC 5545
		"DTSTART;TZID=Europe/Berlin:20261025T023000\r\nDTEND:20261025T010000Z\r\nSUMMARY:Earlier\r\n",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("document lacks\n%s\nin\n%s", want, doc)
		}
	}

	if !strings.HasSuffix(doc, "END:VCALENDAR\r\n") || strings.Contains(strings.ReplaceAll(doc, "\r\n", ""), "\n") {
		t.Errorf("lines must end in CRLF:\n%q", doc)
	}

	// The fingerprint ignores the stamp, not the content.
	later := cal
	later.Stamp = stamp.Add(time.Hour)

	if ical.Encode(later).Fingerprint != ical.Encode(cal).Fingerprint {
		t.Error("the fingerprint changed with the stamp")
	}

	changed := cal
	changed.Events = append([]ical.Event{}, cal.Events...)
	changed.Events[0].Status = ical.StatusCancelled

	if ical.Encode(changed).Fingerprint == ical.Encode(cal).Fingerprint {
		t.Error("the fingerprint did not change with the content")
	}
}
