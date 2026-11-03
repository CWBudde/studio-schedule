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

// eventRegistrationData is Mia's registration, by her mother Anna, for a
// two-session workshop on Saturday 4 and Sunday 5 July 2026 in Berlin, with
// a fee to pay at the desk and a cancellation deadline.
func eventRegistrationData(t *testing.T) mail.EventRegistrationData {
	t.Helper()

	first := time.Date(2026, time.July, 4, 10, 0, 0, 0, berlin(t))
	session := func(start time.Time) mail.BookedLesson {
		return mail.BookedLesson{
			CourseType: "Workshop", StartsAt: start, EndsAt: start.Add(3 * time.Hour),
			Room: "Studio 1", Location: "Example Street 1",
		}
	}

	return mail.EventRegistrationData{
		SchoolName: "Studio Example", GivenName: "Anna", Participant: "Mia Example", Title: "Imaginary Workshop",
		Sessions: []mail.BookedLesson{session(first), session(first.AddDate(0, 0, 1))},
		Fee:      "€45.00", CancelUntil: time.Date(2026, time.July, 1, 23, 59, 0, 0, berlin(t)),
	}
}

// eventRegistrationCanceledData is the cancellation of that registration
// after the fee was paid, so it is refunded.
func eventRegistrationCanceledData(t *testing.T) mail.EventRegistrationData {
	t.Helper()

	d := eventRegistrationData(t)
	d.Sessions, d.Fee, d.CancelUntil, d.Refunded = nil, "", time.Time{}, true

	return d
}

// TestRenderEventRegistrationGolden pins the confirmation of an event
// registration and of its cancellation in both locales.
func TestRenderEventRegistrationGolden(t *testing.T) {
	t.Parallel()

	r := newRenderer(t)

	for _, c := range []struct {
		template string
		data     func(*testing.T) mail.EventRegistrationData
	}{
		{mail.TemplateEventRegistered, eventRegistrationData},
		{mail.TemplateEventRegistrationCanceled, eventRegistrationCanceledData},
	} {
		for _, locale := range []string{"en", "de"} {
			t.Run(c.template+"/"+locale, func(t *testing.T) {
				t.Parallel()

				got, err := r.Render(locale, c.template, c.data(t))
				if err != nil {
					t.Fatal(err)
				}

				golden(t, locale+"/"+c.template+".subject.golden", got.Subject+"\n")
				golden(t, locale+"/"+c.template+".txt.golden", got.Text)
				golden(t, locale+"/"+c.template+".html.golden", got.HTML)
			})
		}
	}
}

// TestRenderEventRegistrationVariants: a free event has no fee paragraph,
// one without a deadline no cancellation paragraph, one without sessions
// no session lines; a cancellation without a refund says nothing about
// one.
func TestRenderEventRegistrationVariants(t *testing.T) {
	t.Parallel()

	r := newRenderer(t)

	free := eventRegistrationData(t)
	free.Fee = ""

	noDeadline := eventRegistrationData(t)
	noDeadline.CancelUntil = time.Time{}

	bare := eventRegistrationData(t)
	bare.Sessions, bare.Fee, bare.CancelUntil = nil, "", time.Time{}

	notRefunded := eventRegistrationCanceledData(t)
	notRefunded.Refunded = false

	for _, c := range []struct {
		name, template string
		data           mail.EventRegistrationData
		want, notWant  []string
	}{
		{
			"registered", mail.TemplateEventRegistered, eventRegistrationData(t),
			[]string{
				"Saturday, 4 July 2026, 10:00–13:00\nStudio 1, Example Street 1",
				"Sunday, 5 July 2026, 10:00–13:00", "the fee of €45.00 at the desk", "Wednesday, 1 July 2026, 23:59",
			},
			nil,
		},
		{
			"free", mail.TemplateEventRegistered, free,
			[]string{"Saturday, 4 July 2026", "until\nWednesday, 1 July 2026"},
			[]string{"fee", "receipt"},
		},
		{
			"no deadline", mail.TemplateEventRegistered, noDeadline,
			[]string{"€45.00"},
			[]string{"cancel", "Wednesday"},
		},
		{
			"bare", mail.TemplateEventRegistered, bare,
			[]string{"Imaginary Workshop\nRegistered: Mia Example\n\nKind regards"},
			[]string{"2026", "fee", "cancel the"},
		},
		{
			"refunded", mail.TemplateEventRegistrationCanceled, eventRegistrationCanceledData(t),
			[]string{"Registered: Mia Example\n\nThe fee you paid is refunded."},
			[]string{"2026"},
		},
		{
			"not refunded", mail.TemplateEventRegistrationCanceled, notRefunded,
			[]string{"Registered: Mia Example\n\nKind regards"},
			[]string{"refunded", "receipt"},
		},
	} {
		got, err := r.Render("en", c.template, c.data)
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

// TestEventRegistrationDataValidate refuses data without a school, a
// title or a participant, or with a session that does not end after it
// starts.
func TestEventRegistrationDataValidate(t *testing.T) {
	t.Parallel()

	for name, edit := range map[string]func(*mail.EventRegistrationData){
		"no school":      func(d *mail.EventRegistrationData) { d.SchoolName = " " },
		"no title":       func(d *mail.EventRegistrationData) { d.Title = "" },
		"no participant": func(d *mail.EventRegistrationData) { d.Participant = "\t" },
		"empty session":  func(d *mail.EventRegistrationData) { d.Sessions[1].EndsAt = d.Sessions[1].StartsAt },
		"session ends before": func(d *mail.EventRegistrationData) {
			d.Sessions[0].EndsAt = d.Sessions[0].StartsAt.Add(-time.Hour)
		},
	} {
		d := eventRegistrationData(t)
		edit(&d)

		if err := d.Validate(); !errors.Is(err, domain.ErrValidation) {
			t.Errorf("%s: %v, want a validation error", name, err)
		}
	}

	if err := eventRegistrationData(t).Validate(); err != nil {
		t.Errorf("valid data: %v", err)
	}
}
