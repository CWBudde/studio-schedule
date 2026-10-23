package mail_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/mail"
)

func TestMessageValidate(t *testing.T) {
	t.Parallel()

	valid := mail.Message{
		To:      "anna@example.org",
		Subject: "Confirm your email address",
		Text:    "Hello",
		HTML:    "<p>Hello</p>",
	}

	if err := valid.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}

	textOnly := valid
	textOnly.HTML = ""

	if err := textOnly.Validate(); err != nil {
		t.Fatalf("Validate() without HTML = %v, want nil", err)
	}

	tests := []struct {
		name   string
		modify func(m *mail.Message)
	}{
		{"no recipient", func(m *mail.Message) { m.To = "" }},
		{"not an address", func(m *mail.Message) { m.To = "anna" }},
		{"display name", func(m *mail.Message) { m.To = "Anna <anna@example.org>" }},
		{"two recipients", func(m *mail.Message) { m.To = "anna@example.org, ben@example.org" }},
		{"recipient with newline", func(m *mail.Message) { m.To = "anna@example.org\r\nBcc: eve@example.org" }},
		{"no subject", func(m *mail.Message) { m.Subject = " " }},
		{"subject with CR", func(m *mail.Message) { m.Subject = "Hi\rBcc: eve@example.org" }},
		{"subject with LF", func(m *mail.Message) { m.Subject = "Hi\nBcc: eve@example.org" }},
		{"subject too long", func(m *mail.Message) { m.Subject = strings.Repeat("a", mail.MaxSubjectLen+1) }},
		{"no text", func(m *mail.Message) { m.Text = "\n" }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			m := valid
			tt.modify(&m)

			err := m.Validate()
			if !errors.Is(err, domain.ErrValidation) {
				t.Fatalf("Validate() = %v, want an error wrapping domain.ErrValidation", err)
			}
		})
	}
}

func TestRenderedMessage(t *testing.T) {
	t.Parallel()

	r := mail.Rendered{Subject: "S", Text: "T", HTML: "H"}

	got := r.Message("anna@example.org")
	want := mail.Message{To: "anna@example.org", Subject: "S", Text: "T", HTML: "H"}

	if got != want {
		t.Fatalf("Message() = %+v, want %+v", got, want)
	}
}
