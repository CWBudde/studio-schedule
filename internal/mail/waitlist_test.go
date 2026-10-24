package mail_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/mail"
)

// waitlistData is Anna's daughter Mia on the waiting list of the Tuesday
// lesson of lessonData.
func waitlistData(t *testing.T) mail.WaitlistData {
	t.Helper()

	l := lessonData(t)

	return mail.WaitlistData{
		SchoolName: l.SchoolName, GivenName: "Anna", Participant: "Mia Example", CourseType: l.CourseType,
		StartsAt: l.StartsAt, EndsAt: l.EndsAt, Room: l.Room, Location: l.Location,
	}
}

// waitlistInviteData invites Mia until Monday 21:30, the evening before.
func waitlistInviteData(t *testing.T) mail.WaitlistInviteData {
	t.Helper()

	return mail.WaitlistInviteData{
		WaitlistData: waitlistData(t),
		Link:         "https://school.example.org/waitlist/accept?token=abc123",
		ExpiresAt:    time.Date(2026, time.October, 26, 21, 30, 0, 0, berlin(t)),
	}
}

func waitlistExpiredData(t *testing.T, stillWaiting bool) mail.WaitlistExpiredData {
	t.Helper()

	return mail.WaitlistExpiredData{WaitlistData: waitlistData(t), StillWaiting: stillWaiting}
}

// TestRenderWaitlistGolden pins the waiting-list mails in both locales.
func TestRenderWaitlistGolden(t *testing.T) {
	t.Parallel()

	r := newRenderer(t)

	tests := []struct {
		golden, template string
		data             any
	}{
		{mail.TemplateWaitlistInvite, mail.TemplateWaitlistInvite, waitlistInviteData(t)},
		{mail.TemplateWaitlistExpired, mail.TemplateWaitlistExpired, waitlistExpiredData(t, true)},
		{"waitlist-expired-left", mail.TemplateWaitlistExpired, waitlistExpiredData(t, false)},
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

// TestRenderWaitlistContent checks the essentials in both parts and both
// locales: the person, the lesson in school time, the expiry and the link,
// and whether the person stays on the list.
func TestRenderWaitlistContent(t *testing.T) {
	t.Parallel()

	r := newRenderer(t)
	link := waitlistInviteData(t).Link

	tests := []struct {
		name, locale string
		data         any
		want         []string
		absent       string
	}{
		{mail.TemplateWaitlistInvite, "en", waitlistInviteData(t), []string{
			"Tuesday, 27 October 2026, 18:00–19:15", "Room One, Studio Nowhere",
			"reserved for Mia Example until Monday, 26 October 2026, 21:30", link,
		}, ""},
		{mail.TemplateWaitlistInvite, "de", waitlistInviteData(t), []string{
			"Dienstag, 27. Oktober 2026, 18:00–19:15 Uhr", "reserviert bis Montag, 26. Oktober 2026, 21:30 Uhr", link,
		}, ""},
		{mail.TemplateWaitlistExpired, "en", waitlistExpiredData(t, true), []string{
			"not taken in time", "stays on the waiting list",
		}, "no longer"},
		{mail.TemplateWaitlistExpired, "en", waitlistExpiredData(t, false), []string{
			"is no longer on the waiting list",
		}, "stays"},
		{mail.TemplateWaitlistExpired, "de", waitlistExpiredData(t, true), []string{
			"nicht rechtzeitig genommen", "bleibt auf der Warteliste",
		}, "nicht mehr"},
		{mail.TemplateWaitlistExpired, "de", waitlistExpiredData(t, false), []string{
			"steht nicht mehr auf der Warteliste",
		}, "bleibt"},
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
}

func TestWaitlistDataValidate(t *testing.T) {
	t.Parallel()

	r := newRenderer(t)

	for name, change := range map[string]func(*mail.WaitlistInviteData){
		"no school":      func(d *mail.WaitlistInviteData) { d.SchoolName = " " },
		"no participant": func(d *mail.WaitlistInviteData) { d.Participant = "" },
		"no room":        func(d *mail.WaitlistInviteData) { d.Room = "" },
		"ends at start":  func(d *mail.WaitlistInviteData) { d.EndsAt = d.StartsAt },
		"relative link":  func(d *mail.WaitlistInviteData) { d.Link = "/waitlist/accept?token=x" },
		"no expiry":      func(d *mail.WaitlistInviteData) { d.ExpiresAt = time.Time{} },
	} {
		d := waitlistInviteData(t)
		change(&d)

		if _, err := r.Render("en", mail.TemplateWaitlistInvite, d); !errors.Is(err, domain.ErrValidation) {
			t.Errorf("%s: want ErrValidation, got %v", name, err)
		}
	}

	d := waitlistExpiredData(t, false)
	d.CourseType = ""

	if _, err := r.Render("en", mail.TemplateWaitlistExpired, d); !errors.Is(err, domain.ErrValidation) {
		t.Errorf("expired without course type: want ErrValidation, got %v", err)
	}
}
