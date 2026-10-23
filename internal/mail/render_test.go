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

func confirmData() mail.ConfirmEmailData {
	return mail.ConfirmEmailData{
		SchoolName:   "Studio Example",
		GivenName:    "Anna",
		Link:         "https://schedule.example.org/confirm-email?token=abc123",
		ExpiresHours: 24,
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

func TestRenderConfirmEmailGolden(t *testing.T) {
	t.Parallel()

	r := newRenderer(t)

	for _, locale := range []string{"en", "de"} {
		t.Run(locale, func(t *testing.T) {
			t.Parallel()

			got, err := r.Render(locale, mail.TemplateConfirmEmail, confirmData())
			if err != nil {
				t.Fatalf("Render: %v", err)
			}

			golden(t, locale+"/confirm-email.subject.golden", got.Subject+"\n")
			golden(t, locale+"/confirm-email.txt.golden", got.Text)
			golden(t, locale+"/confirm-email.html.golden", got.HTML)
		})
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
		modify func(d *mail.ConfirmEmailData)
	}{
		{"no school", func(d *mail.ConfirmEmailData) { d.SchoolName = " " }},
		{"no link", func(d *mail.ConfirmEmailData) { d.Link = "" }},
		{"relative link", func(d *mail.ConfirmEmailData) { d.Link = "/confirm-email?token=abc" }},
		{"javascript link", func(d *mail.ConfirmEmailData) { d.Link = "javascript:alert(1)" }},
		{"no expiry", func(d *mail.ConfirmEmailData) { d.ExpiresHours = 0 }},
	}

	for _, tt := range tests {
		data := confirmData()
		tt.modify(&data)

		if _, err := r.Render("en", mail.TemplateConfirmEmail, data); !errors.Is(err, domain.ErrValidation) {
			t.Errorf("%s: Render = %v, want an error wrapping domain.ErrValidation", tt.name, err)
		}
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
		mail.TemplateConfirmEmail: confirmData(),
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
