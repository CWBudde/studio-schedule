// Package ical writes iCalendar documents (RFC 5545) for calendar feeds (ADR
// 0011): one VCALENDAR with the VTIMEZONE of its time zone and one VEVENT per
// event. It has no dependencies beyond the standard library, so the WASM
// build stays small.
//
// Lines end in CRLF and are folded at 75 octets without splitting a UTF-8
// sequence; TEXT values are escaped. Event times are local to the calendar's
// zone (DTSTART;TZID=…), whose VTIMEZONE lists the zone's transitions in the
// years the events cover, computed from Go's zone data.
package ical

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"strings"
	"time"
	"unicode/utf8"
)

// ContentType is the media type of an iCalendar document.
const ContentType = "text/calendar; charset=utf-8"

// ProductID identifies the program that wrote a document (PRODID).
const ProductID = "-//studio-schedule//EN"

// RefreshInterval is how often calendars are asked to fetch a feed again
// (REFRESH-INTERVAL, X-PUBLISHED-TTL).
const RefreshInterval = "PT1H"

// Status is the STATUS of an event.
type Status string

// Event statuses.
const (
	StatusConfirmed Status = "CONFIRMED"
	StatusCancelled Status = "CANCELLED" //nolint:misspell // RFC 5545 spells it so

)

// Calendar is a document: a named calendar of events in one time zone.
type Calendar struct {
	// Name is the calendar's display name (NAME, X-WR-CALNAME).
	Name string
	// Zone is the time zone of the event times; required.
	Zone *time.Location
	// Stamp is when the document was generated: the DTSTAMP of every event.
	Stamp time.Time
	// Events are written in their order.
	Events []Event
}

// Event is one VEVENT.
type Event struct {
	// UID identifies the event across fetches; required.
	UID        string
	Start, End time.Time
	// Summary, Location and Description are plain text; Description may
	// have several lines. Location and Description are optional.
	Summary, Location, Description string
	// URL is an absolute link to the event; optional.
	URL    string
	Status Status
}

// Document is an encoded calendar.
type Document struct {
	Body []byte
	// Fingerprint is a hash of the body without its DTSTAMPs, so it changes
	// only when the content does: a feed's ETag.
	Fingerprint string
}

// Encode returns the document of c and its fingerprint, in one pass.
func Encode(c Calendar) Document {
	w := &writer{hash: sha256.New()}

	w.line("BEGIN", "VCALENDAR")
	w.line("VERSION", "2.0")
	w.line("PRODID", ProductID)
	w.line("CALSCALE", "GREGORIAN")
	w.line("METHOD", "PUBLISH")
	w.line("NAME", Text(c.Name))
	w.line("X-WR-CALNAME", Text(c.Name))
	w.line("X-WR-TIMEZONE", Text(c.Zone.String()))
	w.line("REFRESH-INTERVAL;VALUE=DURATION", RefreshInterval)
	w.line("X-PUBLISHED-TTL", RefreshInterval)

	from, to := coveredYears(c)
	writeTimezone(w, c.Zone, from, to)

	stamp := utcTime(c.Stamp)

	for _, e := range c.Events {
		w.line("BEGIN", "VEVENT")
		w.line("UID", Text(e.UID))
		w.stamp("DTSTAMP", stamp)
		w.dateTime("DTSTART", e.Start, c.Zone)
		w.dateTime("DTEND", e.End, c.Zone)
		w.line("SUMMARY", Text(e.Summary))

		if e.Location != "" {
			w.line("LOCATION", Text(e.Location))
		}

		if e.Description != "" {
			w.line("DESCRIPTION", Text(e.Description))
		}

		if e.URL != "" {
			w.line("URL;VALUE=URI", stripControls(e.URL))
		}

		w.line("STATUS", string(e.Status))
		w.line("SEQUENCE", "0")

		// A canceled event keeps its place in the calendar but blocks no time.
		if e.Status == StatusCancelled {
			w.line("TRANSP", "TRANSPARENT")
		} else {
			w.line("TRANSP", "OPAQUE")
		}

		w.line("END", "VEVENT")
	}

	w.line("END", "VCALENDAR")

	return Document{Body: w.body.Bytes(), Fingerprint: hex.EncodeToString(w.hash.Sum(nil))}
}

// coveredYears returns the start of the first and the end of the last year
// (in UTC, a day wider on each side to cover every offset) of the events, or
// of the stamp's year without events.
func coveredYears(c Calendar) (time.Time, time.Time) {
	first, last := c.Stamp, c.Stamp

	for i, e := range c.Events {
		if i == 0 || e.Start.Before(first) {
			first = e.Start
		}

		if i == 0 || e.End.After(last) {
			last = e.End
		}
	}

	from := time.Date(first.In(c.Zone).Year(), time.January, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, -1)
	to := time.Date(last.In(c.Zone).Year()+1, time.January, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, 1)

	return from, to
}

// writer writes content lines, folded, to the body and, except the
// DTSTAMPs, to the hash of the fingerprint. Neither fails.
type writer struct {
	body bytes.Buffer
	hash hash.Hash
}

// maxLineOctets is the longest a content line may be, without its CRLF.
const maxLineOctets = 75

// line writes "name:value" folded into lines of at most 75 octets: each
// continuation line starts with a space. A UTF-8 sequence is never split.
// name may carry parameters ("DTSTART;TZID=…"); value must be escaped
// already where its type requires it.
func (w *writer) line(name, value string) {
	l := Fold(name + ":" + value)
	w.body.WriteString(l)
	_, _ = io.WriteString(w.hash, l)
}

// stamp writes a line to the body only: it is left out of the
// fingerprint.
func (w *writer) stamp(name, value string) {
	w.body.WriteString(Fold(name + ":" + value))
}

// dateTime writes t as a local time of zone with its TZID, or in UTC if the
// local time does not name t: a local time that occurs twice (when the clocks
// go back) names the first of them (RFC 5545, 3.3.5).
func (w *writer) dateTime(name string, t time.Time, zone *time.Location) {
	t = t.Truncate(time.Second)
	if !firstWithWallTime(t, zone).Equal(t) {
		w.line(name, utcTime(t))

		return
	}

	w.line(name+";TZID="+paramValue(zone.String()), t.In(zone).Format(localLayout))
}

// firstWithWallTime returns the first instant whose local time in zone is
// that of t. Only the offsets in effect within a day of t can repeat it.
func firstWithWallTime(t time.Time, zone *time.Location) time.Time {
	local := t.In(zone)
	wall := time.Date(local.Year(), local.Month(), local.Day(), local.Hour(), local.Minute(), local.Second(), 0, time.UTC)
	first := t

	for _, probe := range []time.Time{t.Add(-26 * time.Hour), t.Add(26 * time.Hour)} {
		_, offset := probe.In(zone).Zone()
		candidate := wall.Add(-time.Duration(offset) * time.Second)

		if _, o := candidate.In(zone).Zone(); o == offset && candidate.Before(first) {
			first = candidate
		}
	}

	return first
}

// localLayout is a DATE-TIME in local ("floating" or TZID) form.
const localLayout = "20060102T150405"

// utcTime formats t as a DATE-TIME in UTC.
func utcTime(t time.Time) string { return t.UTC().Format(localLayout) + "Z" }

// Fold folds the content line s at 75 octets and ends every resulting line
// with CRLF. Continuation lines start with a space, which counts toward their
// 75 octets. A UTF-8 sequence is never split.
func Fold(s string) string {
	var b strings.Builder

	b.Grow(len(s) + len(s)/maxLineOctets*3 + 2)

	limit := maxLineOctets
	for len(s) > limit {
		cut := limit
		for cut > 0 && !utf8.RuneStart(s[cut]) {
			cut--
		}

		b.WriteString(s[:cut])
		b.WriteString("\r\n ")
		s = s[cut:]
		limit = maxLineOctets - 1 // the leading space
	}

	b.WriteString(s)
	b.WriteString("\r\n")

	return b.String()
}

// Text escapes s as a TEXT value: backslash, semicolon and comma get a
// backslash, line breaks become \n, and other control characters except tab
// are left out (TEXT must not contain them).
func Text(s string) string {
	var b strings.Builder

	b.Grow(len(s))

	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '\\' || c == ';' || c == ',':
			b.WriteByte('\\')
			b.WriteByte(c)
		case c == '\r':
			if i+1 < len(s) && s[i+1] == '\n' {
				i++
			}

			b.WriteString(`\n`)
		case c == '\n':
			b.WriteString(`\n`)
		case c == '\t' || (c >= 0x20 && c != 0x7f):
			b.WriteByte(c)
		}
	}

	return b.String()
}

// stripControls leaves out the control characters of s, for values of other
// types than TEXT (a URI, say).
func stripControls(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}

		return r
	}, s)
}

// paramValue returns s as a parameter value, quoted if it has characters
// that a bare one must not (";", ":", ","). Double quotes are left out.
func paramValue(s string) string {
	s = strings.ReplaceAll(stripControls(s), `"`, "")
	if strings.ContainsAny(s, ";:,") {
		return `"` + s + `"`
	}

	return s
}

// utcOffset formats an offset in seconds east of UTC as ±HHMM, or ±HHMMSS
// if it has seconds.
func utcOffset(secs int) string {
	sign := '+'
	if secs < 0 {
		sign, secs = '-', -secs
	}

	h, m, s := secs/3600, secs/60%60, secs%60
	if s != 0 {
		return fmt.Sprintf("%c%02d%02d%02d", sign, h, m, s)
	}

	return fmt.Sprintf("%c%02d%02d", sign, h, m)
}
