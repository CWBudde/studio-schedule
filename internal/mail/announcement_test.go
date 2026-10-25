package mail_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/mail"
)

// announcementData is an announcement for two children of the recipient,
// over two paragraphs, with characters HTML must escape.
func announcementData() mail.AnnouncementData {
	return mail.AnnouncementData{
		SchoolName: "Studio Example", GivenName: "Clara", Participants: []string{"Ella Example", "Finn Example"},
		Title:   "Costume fitting <Saturday>",
		Message: "Please come at 10:00 & bring the shoes.\nThe fitting takes an hour.\n\nSee you!",
	}
}

// TestRenderAnnouncementGolden pins the announcement in both locales.
func TestRenderAnnouncementGolden(t *testing.T) {
	t.Parallel()

	r := newRenderer(t)

	for _, locale := range []string{"en", "de"} {
		t.Run(locale, func(t *testing.T) {
			t.Parallel()

			got, err := r.Render(locale, mail.TemplateAnnouncement, announcementData())
			if err != nil {
				t.Fatalf("Render: %v", err)
			}

			golden(t, locale+"/announcement.subject.golden", got.Subject+"\n")
			golden(t, locale+"/announcement.txt.golden", got.Text)
			golden(t, locale+"/announcement.html.golden", got.HTML)
		})
	}
}

// TestRenderAnnouncementMessage keeps the text and its line breaks, escapes
// it in HTML, and names the participants only when there are any.
func TestRenderAnnouncementMessage(t *testing.T) {
	t.Parallel()

	r := newRenderer(t)
	data := announcementData()

	got, err := r.Render("en", mail.TemplateAnnouncement, data)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(got.Text, data.Message) || !strings.Contains(got.Text, "for Ella Example, Finn Example") {
		t.Errorf("text part:\n%s", got.Text)
	}

	for _, want := range []string{
		"Please come at 10:00 &amp; bring the shoes.<br>The fitting takes an hour.<br><br>See you!",
		"Costume fitting &lt;Saturday&gt;",
	} {
		if !strings.Contains(got.HTML, want) {
			t.Errorf("HTML part does not contain %q:\n%s", want, got.HTML)
		}
	}

	if strings.Contains(got.HTML, "<Saturday>") {
		t.Errorf("unescaped title:\n%s", got.HTML)
	}

	data.Participants = nil

	got, err = r.Render("de", mail.TemplateAnnouncement, data)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(got.Text, "hat eine Mitteilung:") {
		t.Errorf("participants named without any:\n%s", got.Text)
	}
}

func TestRenderValidatesAnnouncementData(t *testing.T) {
	t.Parallel()

	r := newRenderer(t)

	for name, modify := range map[string]func(d *mail.AnnouncementData){
		"no school":  func(d *mail.AnnouncementData) { d.SchoolName = " " },
		"no title":   func(d *mail.AnnouncementData) { d.Title = "" },
		"no message": func(d *mail.AnnouncementData) { d.Message = "\n" },
	} {
		data := announcementData()
		modify(&data)

		if _, err := r.Render("en", mail.TemplateAnnouncement, data); !errors.Is(err, domain.ErrValidation) {
			t.Errorf("%s: Render = %v, want an error wrapping domain.ErrValidation", name, err)
		}
	}
}

// TestSecretLink names exactly the templates that send a single-use link.
func TestSecretLink(t *testing.T) {
	t.Parallel()

	secret := map[string]bool{
		mail.TemplateConfirmEmail: true, mail.TemplateChangeEmail: true, mail.TemplateResetPassword: true,
		mail.TemplateInvite: true, mail.TemplateWaitlistInvite: true,
	}

	for _, name := range mail.TemplateNames() {
		if got := mail.SecretLink(name); got != secret[name] {
			t.Errorf("SecretLink(%s) = %v, want %v", name, got, secret[name])
		}
	}

	if mail.SecretLink("") || mail.SecretLink("unknown") {
		t.Error("SecretLink of no or an unknown template")
	}
}
