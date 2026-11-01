package mail_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/mail"
)

// eventCanceledData is a three-day intensive from Friday 3 to Sunday 5 July
// 2026 in Berlin that Anna and her daughter registered for and paid.
func eventCanceledData(t *testing.T) mail.EventCanceledData {
	t.Helper()

	first := time.Date(2026, time.July, 3, 10, 0, 0, 0, berlin(t))

	return mail.EventCanceledData{
		SchoolName: "Studio Example", GivenName: "Anna", Participants: []string{"Anna Example", "Mia Example"},
		Title: "Imaginary Intensive", FirstDay: first, LastDay: first.AddDate(0, 0, 2),
		Note: "The guest teacher is ill.", Refunded: true,
	}
}

// TestRenderEventCanceledGolden pins the event's cancellation in both
// locales.
func TestRenderEventCanceledGolden(t *testing.T) {
	t.Parallel()

	r := newRenderer(t)

	for _, locale := range []string{"en", "de"} {
		t.Run(locale, func(t *testing.T) {
			t.Parallel()

			got, err := r.Render(locale, mail.TemplateEventCanceled, eventCanceledData(t))
			if err != nil {
				t.Fatal(err)
			}

			golden(t, locale+"/event-canceled.subject.golden", got.Subject+"\n")
			golden(t, locale+"/event-canceled.txt.golden", got.Text)
			golden(t, locale+"/event-canceled.html.golden", got.HTML)
		})
	}
}

// TestRenderEventCanceledVariants: one day names one date, an event
// without sessions none; without a note or a refund their paragraphs are
// left out.
func TestRenderEventCanceledVariants(t *testing.T) {
	t.Parallel()

	r := newRenderer(t)

	oneDay := eventCanceledData(t)
	oneDay.LastDay = oneDay.FirstDay.Add(3 * time.Hour)
	oneDay.Note, oneDay.Refunded = "", false

	noSessions := oneDay
	noSessions.FirstDay, noSessions.LastDay = time.Time{}, time.Time{}

	for _, c := range []struct {
		name          string
		data          mail.EventCanceledData
		want, notWant []string
	}{
		{"one day", oneDay, []string{"Friday, 3 July 2026\n"}, []string{"–", "Note from", "refunded"}},
		{"no sessions", noSessions, []string{"Imaginary Intensive\nRegistered:"}, []string{"2026", "refunded"}},
	} {
		got, err := r.Render("en", mail.TemplateEventCanceled, c.data)
		if err != nil {
			t.Fatal(err)
		}

		for _, w := range c.want {
			if !strings.Contains(got.Text, w) {
				t.Errorf("%s: text lacks %q:\n%s", c.name, w, got.Text)
			}
		}

		for _, w := range c.notWant {
			if strings.Contains(got.Text, w) || strings.Contains(got.HTML, w) {
				t.Errorf("%s: mail has %q:\n%s", c.name, w, got.Text)
			}
		}

		if strings.Contains(got.Text, "\n\n\n") {
			t.Errorf("%s: text has an empty paragraph:\n%s", c.name, got.Text)
		}
	}
}

// TestEventCanceledDataValidate refuses data without a school, a title, a
// participant, or with a last day before the first.
func TestEventCanceledDataValidate(t *testing.T) {
	t.Parallel()

	for name, edit := range map[string]func(*mail.EventCanceledData){
		"no school":       func(d *mail.EventCanceledData) { d.SchoolName = " " },
		"no title":        func(d *mail.EventCanceledData) { d.Title = "" },
		"no participant":  func(d *mail.EventCanceledData) { d.Participants = nil },
		"last before":     func(d *mail.EventCanceledData) { d.LastDay = d.FirstDay.Add(-time.Hour) },
		"only a last day": func(d *mail.EventCanceledData) { d.FirstDay = time.Time{} },
	} {
		d := eventCanceledData(t)
		edit(&d)

		if err := d.Validate(); !errors.Is(err, domain.ErrValidation) {
			t.Errorf("%s: %v, want a validation error", name, err)
		}
	}
}
