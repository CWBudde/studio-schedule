package mail_test

import (
	"errors"
	"reflect"
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
	textOnly.ReplyTo = "ivy@example.org"

	if err := textOnly.Validate(); err != nil {
		t.Fatalf("Validate() without HTML = %v, want nil", err)
	}

	withPDF := valid
	withPDF.Attachments = []mail.Attachment{pdf("SD-2026-0001.pdf")}

	if err := withPDF.Validate(); err != nil {
		t.Fatalf("Validate() with an attachment = %v, want nil", err)
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
		{"reply-to with display name", func(m *mail.Message) { m.ReplyTo = "Ivy <ivy@example.org>" }},
		{"reply-to with newline", func(m *mail.Message) { m.ReplyTo = "ivy@example.org\r\nBcc: eve@example.org" }},
		{"attachment without name", func(m *mail.Message) { m.Attachments = []mail.Attachment{pdf("")} }},
		{"attachment name with quote", func(m *mail.Message) { m.Attachments = []mail.Attachment{pdf(`a".pdf`)} }},
		{"attachment name with newline", func(m *mail.Message) { m.Attachments = []mail.Attachment{pdf("a\r\nb.pdf")} }},
		{"attachment name with slash", func(m *mail.Message) { m.Attachments = []mail.Attachment{pdf("../a.pdf")} }},
		{"attachment type with params", func(m *mail.Message) {
			a := pdf("a.pdf")
			a.ContentType = "application/pdf; name=x"
			m.Attachments = []mail.Attachment{a}
		}},
		{"empty attachment", func(m *mail.Message) {
			a := pdf("a.pdf")
			a.Content = nil
			m.Attachments = []mail.Attachment{a}
		}},
		{"attachments too large", func(m *mail.Message) {
			a := pdf("a.pdf")
			a.Content = make([]byte, mail.MaxAttachmentsSize+1)
			m.Attachments = []mail.Attachment{a}
		}},
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

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Message() = %+v, want %+v", got, want)
	}
}

func pdf(name string) mail.Attachment {
	return mail.Attachment{Filename: name, ContentType: "application/pdf", Content: []byte("%PDF-1.7")}
}
