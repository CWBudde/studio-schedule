package mail_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/mail"
)

// TestRenderPushWaitlistInvite pins the invite's notification in both
// locales: the lesson and its time, the reservation, and nobody's name.
func TestRenderPushWaitlistInvite(t *testing.T) {
	t.Parallel()

	r := newRenderer(t)
	data := waitlistInviteData(t)

	for locale, want := range map[string]mail.PushText{
		"en": {
			Title: "A place is free: Imaginary Ballet",
			Body:  "Tuesday, 27 October 2026, 18:00–19:15. Reserved until Monday, 26 October 2026, 21:30: open to take it.",
		},
		"de": {
			Title: "Ein Platz ist frei: Imaginary Ballet",
			Body:  "Dienstag, 27. Oktober 2026, 18:00–19:15. Reserviert bis Montag, 26. Oktober 2026, 21:30: zum Annehmen öffnen.",
		},
	} {
		got, err := r.RenderPush(locale, mail.TemplateWaitlistInvite, data)
		if err != nil {
			t.Fatalf("%s: %v", locale, err)
		}

		if got != want {
			t.Errorf("%s:\n got %+v\nwant %+v", locale, got, want)
		}

		for _, private := range []string{data.Participant, data.GivenName, "Mia", "token"} {
			if strings.Contains(got.Title+got.Body, private) {
				t.Errorf("%s: the notification names %q", locale, private)
			}
		}
	}
}

func TestRenderPushRefuses(t *testing.T) {
	t.Parallel()

	r := newRenderer(t)

	if _, err := r.RenderPush("en", mail.TemplateWaitlistExpired, waitlistExpiredData(t, true)); !errors.Is(err, mail.ErrUnknownTemplate) {
		t.Errorf("a template without a notification: want ErrUnknownTemplate, got %v", err)
	}

	bad := waitlistInviteData(t)
	bad.CourseType = ""

	if _, err := r.RenderPush("en", mail.TemplateWaitlistInvite, bad); !errors.Is(err, domain.ErrValidation) {
		t.Errorf("invalid data: want ErrValidation, got %v", err)
	}
}
