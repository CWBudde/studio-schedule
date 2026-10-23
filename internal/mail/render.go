package mail

import (
	"embed"
	"errors"
	"fmt"
	htmltemplate "html/template"
	"io/fs"
	"path"
	"slices"
	"strings"
	texttemplate "text/template"
)

// DefaultLocale is used when no template locale matches the requested one.
const DefaultLocale = "en"

// layoutFile is the shared HTML frame; each <name>.html defines "content".
const layoutFile = "layout.html"

// ErrUnknownTemplate is wrapped by Render for a template name that does not
// exist.
var ErrUnknownTemplate = errors.New("unknown mail template")

//go:embed templates
var embedded embed.FS

// templateFS returns the templates directory as the root of a file system.
func templateFS() fs.FS {
	sub, err := fs.Sub(embedded, "templates")
	if err != nil {
		panic(err) // the directory is embedded at compile time
	}

	return sub
}

// Rendered is a rendered template, ready to be sent.
type Rendered struct {
	Subject string
	Text    string
	HTML    string
}

// Message returns the rendered mail addressed to to.
func (r Rendered) Message(to string) Message {
	return Message{To: to, Subject: r.Subject, Text: r.Text, HTML: r.HTML}
}

// templateSet holds the three parsed parts of one template in one locale.
type templateSet struct {
	subject *texttemplate.Template
	text    *texttemplate.Template
	html    *htmltemplate.Template
}

// Renderer renders the embedded mail templates. It is safe for concurrent
// use.
type Renderer struct {
	locales []string
	sets    map[string]map[string]templateSet // locale → name → parts
}

// NewRenderer parses all embedded templates. It fails if a locale lacks a
// template part or a template does not parse, so mistakes surface at
// startup rather than when a mail is sent.
func NewRenderer() (*Renderer, error) {
	fsys := templateFS()

	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, fmt.Errorf("mail templates: %w", err)
	}

	r := &Renderer{sets: map[string]map[string]templateSet{}}

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}

		loc := e.Name()
		r.locales = append(r.locales, loc)
		r.sets[loc] = map[string]templateSet{}

		for _, name := range TemplateNames() {
			set, err := parseSet(fsys, loc, name)
			if err != nil {
				return nil, fmt.Errorf("mail template %s/%s: %w", loc, name, err)
			}

			r.sets[loc][name] = set
		}
	}

	if !slices.Contains(r.locales, DefaultLocale) {
		return nil, fmt.Errorf("mail templates: default locale %q missing", DefaultLocale)
	}

	return r, nil
}

func parseSet(fsys fs.FS, loc, name string) (templateSet, error) {
	base := path.Join(loc, name)

	subject, err := texttemplate.New(name+".subject.txt").Option("missingkey=error").ParseFS(fsys, base+".subject.txt")
	if err != nil {
		return templateSet{}, err
	}

	text, err := texttemplate.New(name+".txt").Option("missingkey=error").ParseFS(fsys, base+".txt")
	if err != nil {
		return templateSet{}, err
	}

	html, err := htmltemplate.New(layoutFile).
		Funcs(htmltemplate.FuncMap{"lang": func() string { return loc }}).
		Option("missingkey=error").
		ParseFS(fsys, layoutFile, base+".html")
	if err != nil {
		return templateSet{}, err
	}

	if html.Lookup("content") == nil {
		return templateSet{}, errors.New(`html part does not define "content"`)
	}

	return templateSet{subject: subject, text: text, html: html}, nil
}

// Locales returns the available template locales.
func (r *Renderer) Locales() []string { return slices.Clone(r.locales) }

// ResolveLocale returns the template locale used for the requested one: an
// exact match, else its base language ("de-AT" → "de"), else DefaultLocale.
// Matching ignores case and accepts "_" as separator.
func (r *Renderer) ResolveLocale(locale string) string {
	loc := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(locale), "_", "-"))

	if _, ok := r.sets[loc]; ok {
		return loc
	}

	if base, _, ok := strings.Cut(loc, "-"); ok {
		if _, ok := r.sets[base]; ok {
			return base
		}
	}

	return DefaultLocale
}

// Render renders template name in the best matching locale (see
// ResolveLocale). data must be the type documented with the template name;
// if it has a Validate method, it is called first. The subject is collapsed
// to a single line, so data cannot inject mail headers.
func (r *Renderer) Render(locale, name string, data any) (Rendered, error) {
	loc := r.ResolveLocale(locale)

	set, ok := r.sets[loc][name]
	if !ok {
		return Rendered{}, fmt.Errorf("%w: %q", ErrUnknownTemplate, name)
	}

	if v, ok := data.(interface{ Validate() error }); ok {
		if err := v.Validate(); err != nil {
			return Rendered{}, fmt.Errorf("mail template %s: %w", name, err)
		}
	}

	var subject, text, html strings.Builder

	if err := set.subject.Execute(&subject, data); err != nil {
		return Rendered{}, fmt.Errorf("mail template %s/%s subject: %w", loc, name, err)
	}

	if err := set.text.Execute(&text, data); err != nil {
		return Rendered{}, fmt.Errorf("mail template %s/%s text: %w", loc, name, err)
	}

	if err := set.html.Execute(&html, data); err != nil {
		return Rendered{}, fmt.Errorf("mail template %s/%s html: %w", loc, name, err)
	}

	out := Rendered{
		Subject: singleLine(subject.String()),
		Text:    text.String(),
		HTML:    html.String(),
	}

	if out.Subject == "" {
		return Rendered{}, fmt.Errorf("mail template %s/%s: empty subject", loc, name)
	}

	return out, nil
}

// singleLine collapses all whitespace runs, line breaks included, into single
// spaces and trims the ends.
func singleLine(s string) string { return strings.Join(strings.Fields(s), " ") }
