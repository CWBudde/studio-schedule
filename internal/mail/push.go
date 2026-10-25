package mail

import (
	"fmt"
	"io/fs"
	"path"
	"strings"
	texttemplate "text/template"
)

// PushTemplateNames returns the templates that also have a web push
// notification (ADR 0017): the file templates/<locale>/<name>.push.txt,
// whose first line is the notification's title and the rest its body. A
// notification shows on a lock screen, so it names the lesson and its
// time, never a person.
func PushTemplateNames() []string { return []string{TemplateWaitlistInvite} }

// PushText is a rendered notification.
type PushText struct {
	Title string
	Body  string
}

// parsePush parses the push part of template name in locale.
func parsePush(fsys fs.FS, loc, name string) (*texttemplate.Template, error) {
	file := name + ".push.txt"

	return texttemplate.New(file).Funcs(funcs(loc)).Option("missingkey=error").ParseFS(fsys, path.Join(loc, file))
}

// RenderPush renders the notification of template name (one of
// PushTemplateNames) in the best matching locale, with the data of its
// mail. Title and body are single lines.
func (r *Renderer) RenderPush(locale, name string, data any) (PushText, error) {
	loc := r.ResolveLocale(locale)

	t, ok := r.push[loc][name]
	if !ok {
		return PushText{}, fmt.Errorf("%w: no notification for %q", ErrUnknownTemplate, name)
	}

	if v, ok := data.(interface{ Validate() error }); ok {
		if err := v.Validate(); err != nil {
			return PushText{}, fmt.Errorf("push template %s: %w", name, err)
		}
	}

	var out strings.Builder
	if err := t.Execute(&out, data); err != nil {
		return PushText{}, fmt.Errorf("push template %s/%s: %w", loc, name, err)
	}

	title, body, _ := strings.Cut(strings.TrimSpace(out.String()), "\n")
	text := PushText{Title: singleLine(title), Body: singleLine(body)}

	if text.Title == "" || text.Body == "" {
		return PushText{}, fmt.Errorf("push template %s/%s: empty title or body", loc, name)
	}

	return text, nil
}
