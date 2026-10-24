package mail_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/mail"
)

// berlin is the fictional school's time zone in the lesson mails.
func berlin(t *testing.T) *time.Location {
	t.Helper()

	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}

	return loc
}

// lessonData is a Tuesday lesson from 18:00 to 19:15 in Berlin, the first
// day of winter time, booked by Anna and her daughter.
func lessonData(t *testing.T) mail.LessonData {
	t.Helper()

	start := time.Date(2026, time.October, 27, 18, 0, 0, 0, berlin(t))

	return mail.LessonData{
		SchoolName:   "Studio Example",
		GivenName:    "Anna",
		Participants: []string{"Anna Example", "Mia Example"},
		CourseType:   "Imaginary Ballet",
		StartsAt:     start,
		EndsAt:       start.Add(75 * time.Minute),
		Room:         "Room One",
		Location:     "Studio Nowhere",
		Note:         "The instructor is ill.",
	}
}

// lessonChangedData moves the lesson to Wednesday 19:30 in Room Two.
func lessonChangedData(t *testing.T) mail.LessonChangedData {
	t.Helper()

	before := lessonData(t)
	before.Note = ""
	now := before
	now.StartsAt = time.Date(2026, time.October, 28, 19, 30, 0, 0, berlin(t))
	now.EndsAt = now.StartsAt.Add(75 * time.Minute)
	now.Room = "Room Two"

	return mail.LessonChangedData{
		LessonData: now, PreviousStartsAt: before.StartsAt, PreviousEndsAt: before.EndsAt,
		PreviousRoom: before.Room, PreviousLocation: before.Location,
	}
}

func lessonRestoredData(t *testing.T) mail.LessonData {
	t.Helper()

	d := lessonData(t)
	d.Note = ""

	return d
}

// TestRenderLessonGolden pins the cancellation, restore and change mails in
// both locales.
func TestRenderLessonGolden(t *testing.T) {
	t.Parallel()

	r := newRenderer(t)

	tests := []struct {
		name string
		data any
	}{
		{mail.TemplateLessonCanceled, lessonData(t)},
		{mail.TemplateLessonRestored, lessonRestoredData(t)},
		{mail.TemplateLessonChanged, lessonChangedData(t)},
	}

	for _, tt := range tests {
		for _, locale := range []string{"en", "de"} {
			t.Run(tt.name+"/"+locale, func(t *testing.T) {
				t.Parallel()

				got, err := r.Render(locale, tt.name, tt.data)
				if err != nil {
					t.Fatalf("Render: %v", err)
				}

				golden(t, locale+"/"+tt.name+".subject.golden", got.Subject+"\n")
				golden(t, locale+"/"+tt.name+".txt.golden", got.Text)
				golden(t, locale+"/"+tt.name+".html.golden", got.HTML)
			})
		}
	}
}

// TestRenderLessonContent checks the essentials of the lesson mails in both
// parts and both locales: school-local date and times, place, participants,
// and the note.
func TestRenderLessonContent(t *testing.T) {
	t.Parallel()

	r := newRenderer(t)

	tests := []struct {
		name, locale string
		data         any
		want         []string
	}{
		{mail.TemplateLessonCanceled, "en", lessonData(t), []string{
			"Tuesday, 27 October 2026, 18:00–19:15", "Room One, Studio Nowhere", "Anna Example, Mia Example",
			"is canceled", "The instructor is ill.",
		}},
		{mail.TemplateLessonCanceled, "de", lessonData(t), []string{
			"Dienstag, 27. Oktober 2026, 18:00–19:15 Uhr", "Room One, Studio Nowhere", "Anna Example, Mia Example",
			"fällt", "The instructor is ill.",
		}},
		{mail.TemplateLessonRestored, "en", lessonRestoredData(t), []string{
			"Tuesday, 27 October 2026, 18:00–19:15", "takes place after all", "still stands",
		}},
		{mail.TemplateLessonRestored, "de", lessonRestoredData(t), []string{
			"Dienstag, 27. Oktober 2026, 18:00–19:15 Uhr", "findet doch statt", "bleibt bestehen",
		}},
		{mail.TemplateLessonChanged, "en", lessonChangedData(t), []string{
			"Wednesday, 28 October 2026, 19:30–20:45, Room Two, Studio Nowhere",
			"Tuesday, 27 October 2026, 18:00–19:15, Room One, Studio Nowhere",
		}},
		{mail.TemplateLessonChanged, "de", lessonChangedData(t), []string{
			"Mittwoch, 28. Oktober 2026, 19:30–20:45 Uhr, Room Two, Studio Nowhere",
			"Dienstag, 27. Oktober 2026, 18:00–19:15 Uhr, Room One, Studio Nowhere",
		}},
	}

	for _, tt := range tests {
		got, err := r.Render(tt.locale, tt.name, tt.data)
		if err != nil {
			t.Fatalf("Render(%s, %s): %v", tt.locale, tt.name, err)
		}

		if !strings.Contains(got.Subject, "Studio Example") || !strings.Contains(got.Subject, "Imaginary Ballet") {
			t.Errorf("%s/%s subject %q names neither school nor course", tt.locale, tt.name, got.Subject)
		}

		for _, part := range []string{got.Text, got.HTML} {
			for _, w := range append(tt.want, "Anna", "Studio Example", "Imaginary Ballet") {
				if !strings.Contains(part, w) {
					t.Errorf("%s/%s part does not contain %q:\n%s", tt.locale, tt.name, w, part)
				}
			}
		}
	}
}

// TestRenderLessonCanceledWithoutNote leaves the note's line out.
func TestRenderLessonCanceledWithoutNote(t *testing.T) {
	t.Parallel()

	r := newRenderer(t)
	data := lessonData(t)
	data.Note = ""

	for locale, absent := range map[string]string{"en": "Note from", "de": "Hinweis von"} {
		got, err := r.Render(locale, mail.TemplateLessonCanceled, data)
		if err != nil {
			t.Fatal(err)
		}

		if strings.Contains(got.Text, absent) || strings.Contains(got.HTML, absent) {
			t.Errorf("%s mail without a note mentions one:\n%s", locale, got.Text)
		}

		if strings.Contains(got.Text, "\n\n\n") {
			t.Errorf("%s text has an empty paragraph:\n%s", locale, got.Text)
		}
	}
}

// TestRenderLessonEscapesNote keeps the office's note from injecting HTML.
func TestRenderLessonEscapesNote(t *testing.T) {
	t.Parallel()

	r := newRenderer(t)
	data := lessonData(t)
	data.Note = `<a href="https://evil.example.org">Click</a>`

	got, err := r.Render("en", mail.TemplateLessonCanceled, data)
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(got.HTML, "<a ") {
		t.Errorf("HTML contains the unescaped note:\n%s", got.HTML)
	}

	if !strings.Contains(got.Text, data.Note) {
		t.Errorf("text part altered the note:\n%s", got.Text)
	}
}

// TestRenderLessonUsesTheTimesLocation formats times in the location they
// carry, the school's, not in UTC.
func TestRenderLessonUsesTheTimesLocation(t *testing.T) {
	t.Parallel()

	r := newRenderer(t)
	data := lessonRestoredData(t)
	// 23:00 on Saturday 31 October in Berlin is 22:00 in UTC.
	data.StartsAt = time.Date(2026, time.October, 31, 23, 0, 0, 0, berlin(t))
	data.EndsAt = data.StartsAt.Add(30 * time.Minute)

	got, err := r.Render("en", mail.TemplateLessonRestored, data)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(got.Text, "Saturday, 31 October 2026, 23:00–23:30") {
		t.Errorf("text does not show the school-local time:\n%s", got.Text)
	}
}

func TestRenderValidatesLessonData(t *testing.T) {
	t.Parallel()

	r := newRenderer(t)

	for name, modify := range map[string]func(d *mail.LessonData){
		"no school":       func(d *mail.LessonData) { d.SchoolName = " " },
		"no participants": func(d *mail.LessonData) { d.Participants = nil },
		"no course type":  func(d *mail.LessonData) { d.CourseType = "" },
		"no room":         func(d *mail.LessonData) { d.Room = "" },
		"no location":     func(d *mail.LessonData) { d.Location = "" },
		"no start":        func(d *mail.LessonData) { d.StartsAt = time.Time{} },
		"end before start": func(d *mail.LessonData) {
			d.EndsAt = d.StartsAt
		},
	} {
		data := lessonData(t)
		modify(&data)

		if _, err := r.Render("en", mail.TemplateLessonCanceled, data); !errors.Is(err, domain.ErrValidation) {
			t.Errorf("%s: Render = %v, want an error wrapping domain.ErrValidation", name, err)
		}

		changed := lessonChangedData(t)
		modify(&changed.LessonData)

		if _, err := r.Render("en", mail.TemplateLessonChanged, changed); !errors.Is(err, domain.ErrValidation) {
			t.Errorf("changed %s: Render = %v, want an error wrapping domain.ErrValidation", name, err)
		}
	}

	for name, modify := range map[string]func(d *mail.LessonChangedData){
		"no previous start":    func(d *mail.LessonChangedData) { d.PreviousStartsAt = time.Time{} },
		"no previous end":      func(d *mail.LessonChangedData) { d.PreviousEndsAt = time.Time{} },
		"no previous room":     func(d *mail.LessonChangedData) { d.PreviousRoom = "" },
		"no previous location": func(d *mail.LessonChangedData) { d.PreviousLocation = "" },
	} {
		data := lessonChangedData(t)
		modify(&data)

		if _, err := r.Render("en", mail.TemplateLessonChanged, data); !errors.Is(err, domain.ErrValidation) {
			t.Errorf("%s: Render = %v, want an error wrapping domain.ErrValidation", name, err)
		}
	}
}
