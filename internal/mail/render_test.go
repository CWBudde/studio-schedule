package mail_test

import (
	"errors"
	"flag"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/mail"
)

var update = flag.Bool("update", false, "rewrite the golden files in testdata")

func newRenderer(t *testing.T) *mail.Renderer {
	t.Helper()

	r, err := mail.NewRenderer()
	if err != nil {
		t.Fatalf("NewRenderer: %v", err)
	}

	return r
}

func confirmData() mail.LinkData {
	return mail.LinkData{
		SchoolName:   "Studio Example",
		GivenName:    "Anna",
		Link:         "https://schedule.example.org/confirm-email?token=abc123",
		ExpiresHours: 24,
	}
}

func resetData() mail.LinkData {
	return mail.LinkData{
		SchoolName:   "Studio Example",
		GivenName:    "Anna",
		Link:         "https://schedule.example.org/reset-password?token=def456",
		ExpiresHours: 1,
	}
}

func changeEmailData() mail.LinkData {
	return mail.LinkData{
		SchoolName:   "Studio Example",
		GivenName:    "Anna",
		Link:         "https://schedule.example.org/change-email?token=ghi789",
		ExpiresHours: 24,
	}
}

func inviteData() mail.LinkData {
	return mail.LinkData{
		SchoolName:   "Studio Example",
		GivenName:    "Anna",
		Link:         "https://schedule.example.org/reset-password?token=jkl012",
		ExpiresHours: 72,
	}
}

func emailChangedData() mail.EmailChangedData {
	return mail.EmailChangedData{
		SchoolName: "Studio Example",
		GivenName:  "Anna",
		NewEmail:   "anna.new@example.org",
	}
}

// golden compares got with testdata/<name> and rewrites the file with -update.
func golden(t *testing.T, name, got string) {
	t.Helper()

	path := filepath.Join("testdata", name)

	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(path, []byte(got), 0o600); err != nil {
			t.Fatal(err)
		}

		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden file (run with -update to create it): %v", err)
	}

	if got != string(want) {
		t.Errorf("%s differs from the golden file (run with -update to accept)\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
	}
}

func TestRenderGolden(t *testing.T) {
	t.Parallel()

	r := newRenderer(t)

	tests := []struct {
		name string
		data any
	}{
		{mail.TemplateConfirmEmail, confirmData()},
		{mail.TemplateResetPassword, resetData()},
		{mail.TemplateChangeEmail, changeEmailData()},
		{mail.TemplateEmailChanged, emailChangedData()},
		{mail.TemplateInvite, inviteData()},
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

// TestRenderAccountMailsContent checks the essential statements of the
// password, email change and invite mails in both parts and both locales.
func TestRenderAccountMailsContent(t *testing.T) {
	t.Parallel()

	r := newRenderer(t)

	tests := []struct {
		name   string
		locale string
		data   any
		want   []string // in both parts
		button string   // HTML part only; empty for no button
	}{
		{mail.TemplateResetPassword, "en", resetData(), []string{
			"Anna", "Studio Example", resetData().Link, "1 hour.", "your password stays unchanged",
		}, "Set a new password"},
		{mail.TemplateResetPassword, "de", resetData(), []string{
			"Anna", "Studio Example", resetData().Link, "1 Stunde ", "dein Passwort bleibt unverändert",
		}, "Neues Passwort festlegen"},
		{mail.TemplateChangeEmail, "en", changeEmailData(), []string{
			"Anna", "Studio Example", changeEmailData().Link, "24 hours", "nothing changes",
		}, "Confirm new email address"},
		{mail.TemplateChangeEmail, "de", changeEmailData(), []string{
			"Anna", "Studio Example", changeEmailData().Link, "24 Stunden", "ändert sich nichts",
		}, "Neue E-Mail-Adresse bestätigen"},
		{mail.TemplateInvite, "en", inviteData(), []string{
			"Anna", "Studio Example", inviteData().Link, "72 hours", "has created an account", "Forgot password?",
		}, "Choose a password"},
		{mail.TemplateInvite, "de", inviteData(), []string{
			"Anna", "Studio Example", inviteData().Link, "72 Stunden", "ein Konto für dich angelegt", "Passwort vergessen?",
		}, "Passwort wählen"},
		{mail.TemplateEmailChanged, "en", emailChangedData(), []string{
			"Anna", "Studio Example", "anna.new@example.org", "contact", "sign in",
		}, ""},
		{mail.TemplateEmailChanged, "de", emailChangedData(), []string{
			"Anna", "Studio Example", "anna.new@example.org", "melde dich", "Anmelden",
		}, ""},
	}

	for _, tt := range tests {
		got, err := r.Render(tt.locale, tt.name, tt.data)
		if err != nil {
			t.Fatalf("Render(%s, %s): %v", tt.locale, tt.name, err)
		}

		if !strings.Contains(got.Subject, "Studio Example") {
			t.Errorf("%s/%s subject %q does not name the school", tt.locale, tt.name, got.Subject)
		}

		for _, part := range []string{got.Text, got.HTML} {
			for _, w := range tt.want {
				if !strings.Contains(part, w) {
					t.Errorf("%s/%s part does not contain %q:\n%s", tt.locale, tt.name, w, part)
				}
			}
		}

		if tt.button != "" && !strings.Contains(got.HTML, ">"+tt.button+"</a>") {
			t.Errorf("%s/%s HTML has no button %q:\n%s", tt.locale, tt.name, tt.button, got.HTML)
		}
	}
}

func TestRenderResetPasswordExpiryPlural(t *testing.T) {
	t.Parallel()

	r := newRenderer(t)
	data := resetData()
	data.ExpiresHours = 2

	for locale, want := range map[string]string{"en": "2 hours.", "de": "2 Stunden "} {
		got, err := r.Render(locale, mail.TemplateResetPassword, data)
		if err != nil {
			t.Fatal(err)
		}

		if !strings.Contains(got.Text, want) {
			t.Errorf("%s text does not contain %q:\n%s", locale, want, got.Text)
		}
	}
}

// TestRenderEmailChangedHasNoLink makes sure the notice to the old address
// carries no link: it informs, it does not invite an action by link.
func TestRenderEmailChangedHasNoLink(t *testing.T) {
	t.Parallel()

	r := newRenderer(t)

	for _, locale := range []string{"en", "de"} {
		got, err := r.Render(locale, mail.TemplateEmailChanged, emailChangedData())
		if err != nil {
			t.Fatal(err)
		}

		if strings.Contains(got.HTML, "<a ") || strings.Contains(got.Text, "http") {
			t.Errorf("%s email-changed mail contains a link:\n%s\n%s", locale, got.Text, got.HTML)
		}
	}
}

func TestRenderEmailChangedEscapesHTML(t *testing.T) {
	t.Parallel()

	r := newRenderer(t)
	data := emailChangedData()
	data.NewEmail = `"><script>alert(1)</script>@example.org`

	got, err := r.Render("en", mail.TemplateEmailChanged, data)
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(got.HTML, "<script>") {
		t.Errorf("HTML contains the unescaped address:\n%s", got.HTML)
	}

	if !strings.Contains(got.HTML, "&lt;script&gt;") {
		t.Errorf("HTML does not contain the escaped address:\n%s", got.HTML)
	}

	if !strings.Contains(got.Text, data.NewEmail) {
		t.Errorf("text part altered the address:\n%s", got.Text)
	}
}

func TestRenderConfirmEmailContent(t *testing.T) {
	t.Parallel()

	r := newRenderer(t)
	data := confirmData()

	tests := []struct {
		locale string
		want   []string
	}{
		{"en", []string{"Anna", "Studio Example", "24 hours", "ignore"}},
		{"de", []string{"Anna", "Studio Example", "24 Stunden", "ignorieren", "bestätige"}},
	}

	for _, tt := range tests {
		got, err := r.Render(tt.locale, mail.TemplateConfirmEmail, data)
		if err != nil {
			t.Fatalf("Render(%s): %v", tt.locale, err)
		}

		if !strings.Contains(got.Subject, data.SchoolName) {
			t.Errorf("%s subject %q does not name the school", tt.locale, got.Subject)
		}

		for _, part := range []string{got.Text, got.HTML} {
			for _, w := range append(tt.want, data.Link) {
				// The HTML part escapes "&" etc.; the test link has none.
				if !strings.Contains(part, w) {
					t.Errorf("%s part does not contain %q:\n%s", tt.locale, w, part)
				}
			}
		}

		if !strings.Contains(got.HTML, `lang="`+tt.locale+`"`) {
			t.Errorf("%s HTML does not declare its language", tt.locale)
		}
	}
}

func TestRenderExpiryPlural(t *testing.T) {
	t.Parallel()

	r := newRenderer(t)
	data := confirmData()
	data.ExpiresHours = 1

	for locale, want := range map[string]string{"en": "1 hour.", "de": "1 Stunde "} {
		got, err := r.Render(locale, mail.TemplateConfirmEmail, data)
		if err != nil {
			t.Fatal(err)
		}

		if !strings.Contains(got.Text, want) {
			t.Errorf("%s text does not contain %q:\n%s", locale, want, got.Text)
		}
	}
}

func TestRenderWithoutGivenName(t *testing.T) {
	t.Parallel()

	r := newRenderer(t)
	data := confirmData()
	data.GivenName = ""

	for locale, want := range map[string]string{"en": "Hello,\n", "de": "Hallo,\n"} {
		got, err := r.Render(locale, mail.TemplateConfirmEmail, data)
		if err != nil {
			t.Fatal(err)
		}

		if !strings.HasPrefix(got.Text, want) {
			t.Errorf("%s text starts with %q, want %q", locale, strings.SplitN(got.Text, "\n", 2)[0], want)
		}
	}
}

func TestRenderEscapesHTML(t *testing.T) {
	t.Parallel()

	r := newRenderer(t)
	data := confirmData()
	data.GivenName = `<script>alert("x")</script>`
	data.SchoolName = `Studio & <b>Co</b>`
	data.Link = `https://schedule.example.org/confirm?t="><script>x</script>`

	got, err := r.Render("en", mail.TemplateConfirmEmail, data)
	if err != nil {
		t.Fatal(err)
	}

	for _, bad := range []string{"<script>", "<b>", `"><`} {
		if strings.Contains(got.HTML, bad) {
			t.Errorf("HTML contains unescaped %q:\n%s", bad, got.HTML)
		}
	}

	if !strings.Contains(got.HTML, "&lt;script&gt;") {
		t.Errorf("HTML does not contain the escaped name:\n%s", got.HTML)
	}

	// The text part is not HTML and keeps the data as is.
	if !strings.Contains(got.Text, data.GivenName) {
		t.Errorf("text part altered the name:\n%s", got.Text)
	}
}

func TestRenderSubjectHeaderInjection(t *testing.T) {
	t.Parallel()

	r := newRenderer(t)
	data := confirmData()
	data.SchoolName = "Studio\r\nBcc: eve@example.org\n  Example "

	got, err := r.Render("en", mail.TemplateConfirmEmail, data)
	if err != nil {
		t.Fatal(err)
	}

	if strings.ContainsAny(got.Subject, "\r\n") {
		t.Fatalf("subject contains a line break: %q", got.Subject)
	}

	if got.Subject != strings.TrimSpace(got.Subject) || strings.Contains(got.Subject, "  ") {
		t.Errorf("subject whitespace not collapsed: %q", got.Subject)
	}

	if err := got.Message("anna@example.org").Validate(); err != nil {
		t.Errorf("rendered message does not validate: %v", err)
	}
}

func TestRenderLocaleFallback(t *testing.T) {
	t.Parallel()

	r := newRenderer(t)
	data := confirmData()

	render := func(locale string) mail.Rendered {
		t.Helper()

		got, err := r.Render(locale, mail.TemplateConfirmEmail, data)
		if err != nil {
			t.Fatalf("Render(%q): %v", locale, err)
		}

		return got
	}

	en, de := render("en"), render("de")

	tests := []struct {
		locale string
		want   mail.Rendered
	}{
		{"de-AT", de},
		{"de_CH", de},
		{"DE", de},
		{" de-de ", de},
		{"en-GB", en},
		{"fr", en},
		{"fr-CA", en},
		{"", en},
	}

	for _, tt := range tests {
		if got := render(tt.locale); got != tt.want {
			t.Errorf("Render(%q) did not fall back to the expected locale; subject %q", tt.locale, got.Subject)
		}
	}
}

func TestResolveLocale(t *testing.T) {
	t.Parallel()

	r := newRenderer(t)

	for in, want := range map[string]string{"de-AT": "de", "en": "en", "fr": "en", "": "en", "DE": "de"} {
		if got := r.ResolveLocale(in); got != want {
			t.Errorf("ResolveLocale(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMatchLocale(t *testing.T) {
	t.Parallel()

	r := newRenderer(t)

	for in, want := range map[string]string{"de-AT": "de", "en": "en", "de_de": "de", "fr": "", "": ""} {
		got, ok := r.MatchLocale(in)
		if got != want || ok != (want != "") {
			t.Errorf("MatchLocale(%q) = %q, %v, want %q", in, got, ok, want)
		}
	}
}

func TestRenderUnknownTemplate(t *testing.T) {
	t.Parallel()

	r := newRenderer(t)

	_, err := r.Render("en", "no-such-template", confirmData())
	if !errors.Is(err, mail.ErrUnknownTemplate) {
		t.Fatalf("Render = %v, want ErrUnknownTemplate", err)
	}
}

func TestRenderValidatesData(t *testing.T) {
	t.Parallel()

	r := newRenderer(t)

	tests := []struct {
		name   string
		modify func(d *mail.LinkData)
	}{
		{"no school", func(d *mail.LinkData) { d.SchoolName = " " }},
		{"no link", func(d *mail.LinkData) { d.Link = "" }},
		{"relative link", func(d *mail.LinkData) { d.Link = "/confirm-email?token=abc" }},
		{"javascript link", func(d *mail.LinkData) { d.Link = "javascript:alert(1)" }},
		{"no expiry", func(d *mail.LinkData) { d.ExpiresHours = 0 }},
	}

	for _, name := range []string{mail.TemplateConfirmEmail, mail.TemplateResetPassword, mail.TemplateChangeEmail} {
		for _, tt := range tests {
			data := confirmData()
			tt.modify(&data)

			if _, err := r.Render("en", name, data); !errors.Is(err, domain.ErrValidation) {
				t.Errorf("%s %s: Render = %v, want an error wrapping domain.ErrValidation", name, tt.name, err)
			}
		}
	}
}

func TestLinkDataValidateMessage(t *testing.T) {
	t.Parallel()

	data := confirmData()
	data.Link = "/relative"

	err := data.Validate()
	if err == nil || !strings.Contains(err.Error(), "link must be an absolute http(s) URL") {
		t.Fatalf("Validate = %v, want the generic link message", err)
	}

	if strings.Contains(err.Error(), "confirmation") {
		t.Errorf("Validate = %v, message still names the confirmation", err)
	}
}

func TestRenderValidatesEmailChangedData(t *testing.T) {
	t.Parallel()

	r := newRenderer(t)

	tests := []struct {
		name   string
		modify func(d *mail.EmailChangedData)
	}{
		{"no school", func(d *mail.EmailChangedData) { d.SchoolName = " " }},
		{"no new email", func(d *mail.EmailChangedData) { d.NewEmail = "" }},
		{"blank new email", func(d *mail.EmailChangedData) { d.NewEmail = "  " }},
	}

	for _, tt := range tests {
		data := emailChangedData()
		tt.modify(&data)

		if err := data.Validate(); !errors.Is(err, domain.ErrValidation) {
			t.Errorf("%s: Validate = %v, want an error wrapping domain.ErrValidation", tt.name, err)
		}

		if _, err := r.Render("en", mail.TemplateEmailChanged, data); !errors.Is(err, domain.ErrValidation) {
			t.Errorf("%s: Render = %v, want an error wrapping domain.ErrValidation", tt.name, err)
		}
	}

	data := emailChangedData()
	data.GivenName = ""

	if err := data.Validate(); err != nil {
		t.Errorf("Validate without given name = %v, want nil", err)
	}
}

func TestRenderWrongData(t *testing.T) {
	t.Parallel()

	r := newRenderer(t)

	// A map without the fields the template uses must fail, not render
	// "<no value>".
	if _, err := r.Render("en", mail.TemplateConfirmEmail, map[string]string{}); err == nil {
		t.Fatal("Render with missing fields succeeded")
	}

	if _, err := r.Render("en", mail.TemplateConfirmEmail, struct{}{}); err == nil {
		t.Fatal("Render with the wrong data type succeeded")
	}
}

// TestTemplateParity checks that every template exists in every locale, with
// all three parts, and that the templates directory holds nothing else.
func TestTemplateParity(t *testing.T) {
	t.Parallel()

	r := newRenderer(t)
	locales := r.Locales()

	if !slices.Contains(locales, mail.DefaultLocale) || !slices.Contains(locales, "de") {
		t.Fatalf("Locales() = %v, want en and de", locales)
	}

	want := map[string]bool{}

	for _, loc := range locales {
		for _, name := range mail.TemplateNames() {
			for _, part := range []string{"subject.txt", "txt", "html"} {
				want[loc+"/"+name+"."+part] = true
			}
		}
	}

	found := map[string]bool{}

	err := fs.WalkDir(mail.TemplateFS(), ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || path == "layout.html" {
			return err
		}

		found[path] = true

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	for p := range want {
		if !found[p] {
			t.Errorf("missing template file %s", p)
		}
	}

	for p := range found {
		if !want[p] {
			t.Errorf("unexpected template file %s (not a known template or locale)", p)
		}
	}
}

// TestRenderAllTemplates renders every template in every locale with the
// data type registered for it, so a template referring to a missing field
// fails here.
func TestRenderAllTemplates(t *testing.T) {
	t.Parallel()

	r := newRenderer(t)

	samples := map[string]any{
		mail.TemplateConfirmEmail:     confirmData(),
		mail.TemplateResetPassword:    resetData(),
		mail.TemplateChangeEmail:      changeEmailData(),
		mail.TemplateEmailChanged:     emailChangedData(),
		mail.TemplateInvite:           inviteData(),
		mail.TemplateLessonCanceled:   lessonData(t),
		mail.TemplateLessonRestored:   lessonRestoredData(t),
		mail.TemplateLessonChanged:    lessonChangedData(t),
		mail.TemplateBookingConfirmed: bookingData(t),
		mail.TemplateBookingCanceled:  canceledData(t),
		mail.TemplateWaitlistInvite:   waitlistInviteData(t),
		mail.TemplateWaitlistExpired:  waitlistExpiredData(t, true),
		mail.TemplateLessonNotice:     noticeData(t),
		mail.TemplateReceipt:          receiptData(false),
		mail.TemplateAnnouncement:     announcementData(),
	}

	for _, name := range mail.TemplateNames() {
		data, ok := samples[name]
		if !ok {
			t.Errorf("no sample data for template %s", name)

			continue
		}

		for _, loc := range r.Locales() {
			got, err := r.Render(loc, name, data)
			if err != nil {
				t.Errorf("Render(%s, %s): %v", loc, name, err)

				continue
			}

			if err := got.Message("anna@example.org").Validate(); err != nil {
				t.Errorf("Render(%s, %s) gives an invalid message: %v", loc, name, err)
			}
		}
	}
}
