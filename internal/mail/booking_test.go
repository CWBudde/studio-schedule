package mail_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/mail"
)

// bookingData is Anna's booking for her daughter Mia of the Tuesday lesson
// of lessonData.
func bookingData(t *testing.T) mail.BookingData {
	t.Helper()

	l := lessonData(t)

	return mail.BookingData{
		SchoolName: l.SchoolName, GivenName: "Anna", Participant: "Mia Example",
		Lessons: []mail.BookedLesson{{
			CourseType: l.CourseType, StartsAt: l.StartsAt, EndsAt: l.EndsAt, Room: l.Room, Location: l.Location,
		}},
	}
}

// canceledData cancels the booking of bookingData late.
func canceledData(t *testing.T) mail.BookingData {
	t.Helper()

	d := bookingData(t)
	d.Lessons[0].Late = true

	return d
}

// canceledSeveralData cancels the following two weeks too, in time; the
// last lesson was moved to Room Two.
func canceledSeveralData(t *testing.T) mail.BookingData {
	t.Helper()

	d := canceledData(t)
	next := d.Lessons[0]
	next.Late = false

	for range 2 {
		next.StartsAt, next.EndsAt = next.StartsAt.AddDate(0, 0, 7), next.EndsAt.AddDate(0, 0, 7)
		d.Lessons = append(d.Lessons, next)
	}

	d.Lessons[2].Room = "Room Two"

	return d
}

// TestRenderBookingGolden pins the booking mails in both locales.
func TestRenderBookingGolden(t *testing.T) {
	t.Parallel()

	r := newRenderer(t)

	tests := []struct {
		golden, template string
		data             mail.BookingData
	}{
		{mail.TemplateBookingConfirmed, mail.TemplateBookingConfirmed, bookingData(t)},
		{mail.TemplateBookingCanceled, mail.TemplateBookingCanceled, canceledData(t)},
		{"booking-canceled-several", mail.TemplateBookingCanceled, canceledSeveralData(t)},
	}

	for _, tt := range tests {
		for _, locale := range []string{"en", "de"} {
			t.Run(tt.golden+"/"+locale, func(t *testing.T) {
				t.Parallel()

				got, err := r.Render(locale, tt.template, tt.data)
				if err != nil {
					t.Fatalf("Render: %v", err)
				}

				golden(t, locale+"/"+tt.golden+".subject.golden", got.Subject+"\n")
				golden(t, locale+"/"+tt.golden+".txt.golden", got.Text)
				golden(t, locale+"/"+tt.golden+".html.golden", got.HTML)
			})
		}
	}
}

// TestRenderBookingContent checks the essentials of the booking mails in
// both parts and both locales: participant, school-local date and times,
// place, and the late flag only where it applies.
func TestRenderBookingContent(t *testing.T) {
	t.Parallel()

	r := newRenderer(t)
	inTime := bookingData(t)

	tests := []struct {
		name, locale string
		data         mail.BookingData
		want         []string
		absent       string
	}{
		{mail.TemplateBookingConfirmed, "en", bookingData(t), []string{
			"Tuesday, 27 October 2026, 18:00–19:15", "Room One, Studio Nowhere", "Booked for: Mia Example", "confirmed",
		}, "Late"},
		{mail.TemplateBookingConfirmed, "de", bookingData(t), []string{
			"Dienstag, 27. Oktober 2026, 18:00–19:15 Uhr", "Room One, Studio Nowhere", "Gebucht für: Mia Example",
			"bestätigt",
		}, "Späte"},
		{mail.TemplateBookingCanceled, "en", canceledData(t), []string{
			"Tuesday, 27 October 2026, 18:00–19:15", "is canceled", "Late cancellation",
		}, "are canceled"},
		{mail.TemplateBookingCanceled, "de", canceledData(t), []string{
			"Dienstag, 27. Oktober 2026, 18:00–19:15 Uhr", "ist storniert", "Späte Absage",
		}, "sind storniert"},
		{mail.TemplateBookingCanceled, "en", inTime, []string{"is canceled"}, "Late"},
		{mail.TemplateBookingCanceled, "de", inTime, []string{"ist storniert"}, "Späte"},
		{mail.TemplateBookingCanceled, "en", canceledSeveralData(t), []string{
			"are canceled", "Tuesday, 3 November 2026, 18:00–19:15", "Tuesday, 10 November 2026, 18:00–19:15",
			"Room Two, Studio Nowhere",
		}, ""},
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
			for _, w := range append(tt.want, "Anna", "Mia Example", "Studio Example", "Imaginary Ballet") {
				if !strings.Contains(part, w) {
					t.Errorf("%s/%s part does not contain %q:\n%s", tt.locale, tt.name, w, part)
				}
			}

			if tt.absent != "" && strings.Contains(part, tt.absent) {
				t.Errorf("%s/%s part contains %q:\n%s", tt.locale, tt.name, tt.absent, part)
			}
		}
	}

	if got, err := r.Render("en", mail.TemplateBookingCanceled, canceledSeveralData(t)); err != nil ||
		strings.Count(got.Text, "Late cancellation") != 1 || !strings.Contains(got.Subject, "3 bookings") {
		t.Errorf("several: %v\n%s\n%s", err, got.Subject, got.Text)
	}
}

func TestBookingDataValidate(t *testing.T) {
	t.Parallel()

	r := newRenderer(t)

	for name, change := range map[string]func(*mail.BookingData){
		"no school":      func(d *mail.BookingData) { d.SchoolName = " " },
		"no participant": func(d *mail.BookingData) { d.Participant = "" },
		"no lesson":      func(d *mail.BookingData) { d.Lessons = nil },
		"no room":        func(d *mail.BookingData) { d.Lessons[0].Room = "" },
		"ends at start":  func(d *mail.BookingData) { d.Lessons[0].EndsAt = d.Lessons[0].StartsAt },
		"no start":       func(d *mail.BookingData) { d.Lessons[0].StartsAt = time.Time{} },
	} {
		d := bookingData(t)
		change(&d)

		if _, err := r.Render("en", mail.TemplateBookingConfirmed, d); !errors.Is(err, domain.ErrValidation) {
			t.Errorf("%s: want ErrValidation, got %v", name, err)
		}
	}
}
