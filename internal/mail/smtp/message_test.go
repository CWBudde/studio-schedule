package smtp

import (
	"bytes"
	"encoding/base64"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	netmail "net/mail"
	"strings"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/mail"
)

var testTime = time.Date(2026, 10, 23, 14, 30, 0, 0, time.FixedZone("CEST", 2*60*60))

func newTestSender(t *testing.T, cfg Config) *Sender {
	t.Helper()

	if cfg.Host == "" {
		cfg.Host = "smtp.example.org"
	}

	if cfg.From == "" {
		cfg.From = "Studio Example <noreply@example.org>"
	}

	s, err := New(cfg, clock.NewFixed(testTime))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	s.random = bytes.NewReader(bytes.Repeat([]byte{0xab}, 64))

	return s
}

func testMessage() mail.Message {
	return mail.Message{
		To:      "anna@example.org",
		Subject: "Bestätigen Sie Ihre E-Mail-Adresse für Studio Example",
		Text:    "Hallo Anna,\n\nbitte bestätigen Sie Ihre Adresse.\n.\nEine sehr lange Zeile " + strings.Repeat("ä", 100) + "\n",
		HTML:    "<p>Hallo Anna,</p>\n<p>bitte bestätigen Sie Ihre Adresse.</p>\n",
	}
}

func parse(t *testing.T, raw []byte) *netmail.Message {
	t.Helper()

	for i, line := range strings.Split(string(raw), "\r\n") {
		if len(line) > 998 {
			t.Fatalf("line %d is %d bytes long", i, len(line))
		}
	}

	if bytes.Contains(bytes.ReplaceAll(raw, []byte("\r\n"), nil), []byte("\n")) {
		t.Fatal("message contains a bare LF")
	}

	msg, err := netmail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("ReadMessage: %v\n%s", err, raw)
	}

	return msg
}

func decodePart(t *testing.T, p *multipart.Part) string {
	t.Helper()

	if got := p.Header.Get("Content-Transfer-Encoding"); got != "quoted-printable" {
		t.Fatalf("part encoding = %q, want quoted-printable", got)
	}

	b, err := io.ReadAll(quotedprintable.NewReader(p))
	if err != nil {
		t.Fatal(err)
	}

	return string(b)
}

func TestBuildMessageMultipart(t *testing.T) {
	t.Parallel()

	s := newTestSender(t, Config{})
	m := testMessage()

	raw, err := s.buildMessage(m)
	if err != nil {
		t.Fatal(err)
	}

	msg := parse(t, raw)

	dec := new(mime.WordDecoder)

	subject, err := dec.DecodeHeader(msg.Header.Get("Subject"))
	if err != nil {
		t.Fatal(err)
	}

	if subject != m.Subject {
		t.Errorf("Subject = %q, want %q", subject, m.Subject)
	}

	if raw := msg.Header.Get("Subject"); !strings.HasPrefix(raw, "=?utf-8?") {
		t.Errorf("non-ASCII subject is not RFC 2047 encoded: %q", raw)
	}

	from, err := msg.Header.AddressList("From")
	if err != nil || len(from) != 1 || from[0].Address != "noreply@example.org" || from[0].Name != "Studio Example" {
		t.Errorf("From = %v (%v)", from, err)
	}

	to, err := msg.Header.AddressList("To")
	if err != nil || len(to) != 1 || to[0].Address != m.To {
		t.Errorf("To = %v (%v)", to, err)
	}

	date, err := msg.Header.Date()
	if err != nil || !date.Equal(testTime) {
		t.Errorf("Date = %v (%v), want %v", date, err, testTime)
	}

	if got, want := msg.Header.Get("Message-Id"), "<abababababababababababababababab@example.org>"; got != want {
		t.Errorf("Message-ID = %q, want %q", got, want)
	}

	if got := msg.Header.Get("Mime-Version"); got != "1.0" {
		t.Errorf("MIME-Version = %q", got)
	}

	if _, ok := msg.Header["Reply-To"]; ok {
		t.Errorf("Reply-To = %q without one in the message", msg.Header.Get("Reply-To"))
	}

	mediaType, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/alternative" {
		t.Fatalf("Content-Type = %q (%v)", msg.Header.Get("Content-Type"), err)
	}

	mr := multipart.NewReader(msg.Body, params["boundary"])

	wantParts := []struct{ contentType, body string }{
		{"text/plain; charset=utf-8", m.Text},
		{"text/html; charset=utf-8", m.HTML},
	}

	for i, want := range wantParts {
		// NextRawPart keeps the transfer encoding, which NextPart hides.
		p, err := mr.NextRawPart()
		if err != nil {
			t.Fatalf("part %d: %v", i, err)
		}

		if got := p.Header.Get("Content-Type"); got != want.contentType {
			t.Errorf("part %d Content-Type = %q, want %q", i, got, want.contentType)
		}

		// net/textproto normalizes line endings to CRLF on the wire.
		if got := strings.ReplaceAll(decodePart(t, p), "\r\n", "\n"); got != want.body {
			t.Errorf("part %d body = %q, want %q", i, got, want.body)
		}
	}

	if _, err := mr.NextRawPart(); !errors.Is(err, io.EOF) {
		t.Errorf("want exactly two parts, NextPart = %v", err)
	}
}

// TestBuildMessageReplyTo sets the Reply-To header of a lesson notice.
func TestBuildMessageReplyTo(t *testing.T) {
	t.Parallel()

	s := newTestSender(t, Config{})
	m := testMessage()
	m.ReplyTo = "ivy@example.org"

	raw, err := s.buildMessage(m)
	if err != nil {
		t.Fatal(err)
	}

	replyTo, err := parse(t, raw).Header.AddressList("Reply-To")
	if err != nil || len(replyTo) != 1 || replyTo[0].Address != m.ReplyTo || replyTo[0].Name != "" {
		t.Errorf("Reply-To = %v (%v)", replyTo, err)
	}
}

func TestBuildMessageTextOnly(t *testing.T) {
	t.Parallel()

	s := newTestSender(t, Config{From: "noreply@example.org"})
	m := testMessage()
	m.HTML = ""
	m.Subject = "Plain ASCII subject"

	raw, err := s.buildMessage(m)
	if err != nil {
		t.Fatal(err)
	}

	msg := parse(t, raw)

	if got := msg.Header.Get("Subject"); got != m.Subject {
		t.Errorf("ASCII Subject = %q, want it unencoded", got)
	}

	if got := msg.Header.Get("Content-Type"); got != "text/plain; charset=utf-8" {
		t.Errorf("Content-Type = %q", got)
	}

	if got := msg.Header.Get("Content-Transfer-Encoding"); got != "quoted-printable" {
		t.Errorf("Content-Transfer-Encoding = %q", got)
	}

	body, err := io.ReadAll(quotedprintable.NewReader(msg.Body))
	if err != nil {
		t.Fatal(err)
	}

	if got := strings.ReplaceAll(string(body), "\r\n", "\n"); got != m.Text {
		t.Errorf("body = %q, want %q", got, m.Text)
	}
}

func TestBuildMessageRejectsInvalid(t *testing.T) {
	t.Parallel()

	s := newTestSender(t, Config{})
	m := testMessage()
	m.Subject = "Hi\r\nBcc: eve@example.org"

	if _, err := s.buildMessage(m); err == nil {
		t.Fatal("buildMessage accepted a subject with CRLF")
	}
}

// TestBuildMessageAttachment sends a receipt PDF along: multipart/mixed with
// the body first, then the attachment in base64.
func TestBuildMessageAttachment(t *testing.T) {
	t.Parallel()

	pdf := bytes.Repeat([]byte("%PDF-1.7 receipt \x00\xff"), 40)

	for name, html := range map[string]string{"with HTML": "<p>Hallo</p>", "text only": ""} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			s := newTestSender(t, Config{})
			m := testMessage()
			m.HTML = html
			m.Attachments = []mail.Attachment{{Filename: "SD-2026-0001.pdf", ContentType: "application/pdf", Content: pdf}}

			raw, err := s.buildMessage(m)
			if err != nil {
				t.Fatal(err)
			}

			msg := parse(t, raw)

			mediaType, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
			if err != nil || mediaType != "multipart/mixed" {
				t.Fatalf("Content-Type = %q (%v)", msg.Header.Get("Content-Type"), err)
			}

			mr := multipart.NewReader(msg.Body, params["boundary"])

			body, err := mr.NextRawPart()
			if err != nil {
				t.Fatal(err)
			}

			bodyType, bodyParams, _ := mime.ParseMediaType(body.Header.Get("Content-Type"))

			switch html {
			case "":
				if bodyType != "text/plain" {
					t.Fatalf("body Content-Type = %q", body.Header.Get("Content-Type"))
				}

				if got := strings.ReplaceAll(decodePart(t, body), "\r\n", "\n"); got != m.Text {
					t.Errorf("body = %q, want %q", got, m.Text)
				}
			default:
				if bodyType != "multipart/alternative" {
					t.Fatalf("body Content-Type = %q", body.Header.Get("Content-Type"))
				}

				alt := multipart.NewReader(body, bodyParams["boundary"])
				for i := range 2 {
					if _, err := alt.NextRawPart(); err != nil {
						t.Fatalf("alternative %d: %v", i, err)
					}
				}
			}

			att, err := mr.NextRawPart()
			if err != nil {
				t.Fatal(err)
			}

			if got := att.Header.Get("Content-Transfer-Encoding"); got != "base64" {
				t.Errorf("attachment encoding = %q", got)
			}

			if _, p, _ := mime.ParseMediaType(att.Header.Get("Content-Disposition")); p["filename"] != "SD-2026-0001.pdf" {
				t.Errorf("Content-Disposition = %q", att.Header.Get("Content-Disposition"))
			}

			if mt, _, _ := mime.ParseMediaType(att.Header.Get("Content-Type")); mt != "application/pdf" {
				t.Errorf("attachment Content-Type = %q", att.Header.Get("Content-Type"))
			}

			got, err := io.ReadAll(base64.NewDecoder(base64.StdEncoding, att))
			if err != nil || !bytes.Equal(got, pdf) {
				t.Errorf("attachment = %q (%v), want the PDF", got, err)
			}

			if _, err := mr.NextRawPart(); !errors.Is(err, io.EOF) {
				t.Errorf("want exactly two parts, NextPart = %v", err)
			}
		})
	}
}
