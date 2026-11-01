package smtp

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	netmail "net/mail"
	netsmtp "net/smtp"
	"net/textproto"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/mail"
)

// DefaultTimeout bounds one Send, from dialing to the end of DATA, and one
// Verify, unless the context ends earlier.
const DefaultTimeout = 30 * time.Second

// Sender sends mail over SMTP, one connection per message. It implements
// mail.Mailer and is safe for concurrent use.
type Sender struct {
	cfg     Config
	from    *netmail.Address
	clock   clock.Clock
	timeout time.Duration

	// random feeds Message-IDs and MIME boundaries; tests replace it.
	random io.Reader
	// rootCAs overrides the system roots for verifying the server; tests
	// set it.
	rootCAs *x509.CertPool
}

var _ mail.Mailer = (*Sender)(nil)

// New validates cfg and returns a Sender. clk dates the messages; nil means
// the system clock. The errors wrap ErrInvalidConfig and never contain the
// password.
func New(cfg Config, clk clock.Clock) (*Sender, error) {
	cfg = cfg.withDefaults()

	from, err := cfg.validate()
	if err != nil {
		return nil, err
	}

	if clk == nil {
		clk = clock.System{}
	}

	return &Sender{cfg: cfg, from: from, clock: clk, timeout: DefaultTimeout, random: rand.Reader}, nil
}

// String implements fmt.Stringer with the password masked.
func (s Sender) String() string { return "smtp.Sender" + s.cfg.String() }

// GoString implements fmt.GoStringer (%#v) with the password masked.
func (s Sender) GoString() string { return "&smtp.Sender{cfg:" + s.cfg.GoString() + "}" }

// LogValue implements slog.LogValuer with the password masked.
func (s Sender) LogValue() slog.Value { return s.cfg.LogValue() }

// Send validates m and delivers it. It returns once the server has accepted
// the message, or with an error if the server rejects it, the connection
// fails, or ctx ends (the error then wraps ctx.Err()). Errors for which
// Permanent is true also wrap mail.ErrPermanent.
func (s *Sender) Send(ctx context.Context, m mail.Message) error {
	err := s.send(ctx, m)
	if err != nil && Permanent(err) {
		return fmt.Errorf("%w: %w", mail.ErrPermanent, err)
	}

	return err
}

func (s *Sender) send(ctx context.Context, m mail.Message) error {
	raw, err := s.buildMessage(m)
	if err != nil {
		return err
	}

	// The server has accepted the message once transfer returns nil; a
	// failing QUIT must not make the caller send it again.
	return s.converse(ctx, "send", func(c *netsmtp.Client) error {
		if err := s.hello(c); err != nil {
			return err
		}

		return s.transfer(c, m.To, raw)
	})
}

// Verify checks that the server accepts the Sender's connection and login
// without sending anything: it connects, reads the greeting, upgrades with
// STARTTLS in that TLS mode, logs in when a user name is set, and quits; no
// MAIL, RCPT or DATA. It fails as Send would, within the same timeout and
// with the same errors.
func (s *Sender) Verify(ctx context.Context) error {
	return s.converse(ctx, "verify", s.hello)
}

// converse connects to the server, reads its greeting, runs the dialog,
// and quits. A failing dialog is reported as step; a failing QUIT is
// ignored, because the dialog has done its work.
func (s *Sender) converse(ctx context.Context, step string, dialog func(c *netsmtp.Client) error) error {
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	if err := ctx.Err(); err != nil {
		return fmt.Errorf("smtp: %w", err)
	}

	conn, err := s.dial(ctx)
	if err != nil {
		return s.fail(ctx, "connect", err)
	}

	// Closing the connection when ctx ends unblocks any pending read or
	// write; the deadline is a second guard.
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()

	if d, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(d)
	}

	c, err := netsmtp.NewClient(conn, s.cfg.Host)
	if err != nil {
		_ = conn.Close()

		return s.fail(ctx, "greeting", err)
	}
	defer func() { _ = c.Close() }()

	if err := dialog(c); err != nil {
		return s.fail(ctx, step, err)
	}

	_ = c.Quit()

	return nil
}

// hello runs STARTTLS (in that TLS mode) and AUTH (with a user name), the
// dialog before a mail transaction.
func (s *Sender) hello(c *netsmtp.Client) error {
	if s.cfg.TLS == TLSStartTLS {
		if ok, _ := c.Extension("STARTTLS"); !ok {
			return errors.New("server does not offer STARTTLS")
		}

		if err := c.StartTLS(s.tlsConfig()); err != nil {
			return fmt.Errorf("STARTTLS: %w", err)
		}
	}

	if s.cfg.Username != "" {
		if ok, _ := c.Extension("AUTH"); !ok {
			return errors.New("server does not offer AUTH")
		}

		// PlainAuth itself refuses an unencrypted connection to a host
		// other than localhost.
		if err := c.Auth(netsmtp.PlainAuth("", s.cfg.Username, s.cfg.Password, s.cfg.Host)); err != nil {
			return fmt.Errorf("AUTH: %w", err)
		}
	}

	return nil
}

// transfer runs the mail transaction, from MAIL FROM to the end of DATA.
func (s *Sender) transfer(c *netsmtp.Client, to string, raw []byte) error {
	if err := c.Mail(s.from.Address); err != nil {
		return fmt.Errorf("MAIL FROM: %w", err)
	}

	if err := c.Rcpt(to); err != nil {
		return fmt.Errorf("RCPT TO: %w", err)
	}

	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("DATA: %w", err)
	}

	if _, err := w.Write(raw); err != nil {
		_ = w.Close()

		return fmt.Errorf("DATA: %w", err)
	}

	if err := w.Close(); err != nil {
		return fmt.Errorf("DATA: %w", err)
	}

	return nil
}

func (s *Sender) dial(ctx context.Context) (net.Conn, error) {
	addr := net.JoinHostPort(s.cfg.Host, strconv.Itoa(s.cfg.Port))
	d := &net.Dialer{}

	if s.cfg.TLS == TLSImplicit {
		td := &tls.Dialer{NetDialer: d, Config: s.tlsConfig()}

		return td.DialContext(ctx, "tcp", addr)
	}

	return d.DialContext(ctx, "tcp", addr)
}

func (s *Sender) tlsConfig() *tls.Config {
	return &tls.Config{ServerName: s.cfg.Host, MinVersion: tls.VersionTLS12, RootCAs: s.rootCAs}
}

// fail wraps err for the failed step; if ctx has ended, the error wraps
// ctx.Err() as well, because that is the actual cause. The connection's
// deadline is ctx's deadline and can fire before ctx reports it, so a
// deadline error counts as ctx's.
func (s *Sender) fail(ctx context.Context, step string, err error) error {
	ctxErr := ctx.Err()
	if ctxErr == nil && errors.Is(err, os.ErrDeadlineExceeded) {
		ctxErr = context.DeadlineExceeded
	}

	if ctxErr != nil {
		return fmt.Errorf("smtp %s %s:%d: %w (%w)", step, s.cfg.Host, s.cfg.Port, ctxErr, err)
	}

	return fmt.Errorf("smtp %s %s:%d: %w", step, s.cfg.Host, s.cfg.Port, err)
}

// Permanent reports whether err will recur when the same message is sent
// again: the message is invalid, or the server answered with a 5xx reply.
// Network errors, timeouts and 4xx replies are transient.
func Permanent(err error) bool {
	if errors.Is(err, domain.ErrValidation) {
		return true
	}

	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}

	var tpErr *textproto.Error

	return errors.As(err, &tpErr) && tpErr.Code >= 500 && tpErr.Code < 600
}

// buildMessage validates m and renders it as an RFC 5322 message with CRLF
// line endings. The body is text/plain alone, or multipart/alternative with
// text/plain and text/html, both quoted-printable UTF-8. With attachments
// the message is multipart/mixed: the body first, then each attachment,
// base64-encoded.
func (s *Sender) buildMessage(m mail.Message) ([]byte, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}

	id, err := s.randomHex()
	if err != nil {
		return nil, err
	}

	var b bytes.Buffer

	header := func(key, value string) { b.WriteString(key + ": " + value + "\r\n") }

	header("From", s.from.String())
	header("To", (&netmail.Address{Address: m.To}).String())

	if m.ReplyTo != "" {
		header("Reply-To", (&netmail.Address{Address: m.ReplyTo}).String())
	}

	header("Subject", mime.QEncoding.Encode("utf-8", m.Subject))
	header("Date", s.clock.Now().Format(time.RFC1123Z))
	header("Message-ID", "<"+id+"@"+domainOf(s.from.Address)+">")
	header("MIME-Version", "1.0")

	contentType, body, err := s.body(m)
	if err != nil {
		return nil, err
	}

	if len(m.Attachments) == 0 {
		header("Content-Type", contentType)

		if contentType == textPlain {
			header("Content-Transfer-Encoding", "quoted-printable")
		}

		b.WriteString("\r\n")
		b.Write(body)

		return b.Bytes(), nil
	}

	boundary, err := s.randomHex()
	if err != nil {
		return nil, err
	}

	// The multipart writer writes into b, and a bytes.Buffer never fails a
	// write; with a valid boundary, the writer's errors cannot occur.
	mw := multipart.NewWriter(&b)
	_ = mw.SetBoundary("mixed-" + boundary)

	header("Content-Type", mime.FormatMediaType("multipart/mixed", map[string]string{"boundary": mw.Boundary()}))
	b.WriteString("\r\n")

	part := textproto.MIMEHeader{"Content-Type": {contentType}}
	if contentType == textPlain {
		part.Set("Content-Transfer-Encoding", "quoted-printable")
	}

	w, _ := mw.CreatePart(part)
	_, _ = w.Write(body)

	for _, a := range m.Attachments {
		w, _ := mw.CreatePart(textproto.MIMEHeader{
			"Content-Type":              {mime.FormatMediaType(a.ContentType, map[string]string{"name": a.Filename})},
			"Content-Disposition":       {mime.FormatMediaType("attachment", map[string]string{"filename": a.Filename})},
			"Content-Transfer-Encoding": {"base64"},
		})
		writeBase64(w, a.Content)
	}

	_ = mw.Close()

	return b.Bytes(), nil
}

const textPlain = "text/plain; charset=utf-8"

// body returns the Content-Type and the encoded body of m's text: text/plain
// alone (quoted-printable), or multipart/alternative with its HTML. Only the
// random boundary can fail.
func (s *Sender) body(m mail.Message) (string, []byte, error) {
	var b bytes.Buffer

	if m.HTML == "" {
		writeQuotedPrintable(&b, m.Text)

		return textPlain, b.Bytes(), nil
	}

	boundary, err := s.randomHex()
	if err != nil {
		return "", nil, err
	}

	// As in buildMessage: the writer writes into a bytes.Buffer, so its
	// errors cannot occur.
	mw := multipart.NewWriter(&b)
	_ = mw.SetBoundary("alt-" + boundary)

	for _, part := range []struct{ contentType, body string }{
		{textPlain, m.Text},
		{"text/html; charset=utf-8", m.HTML},
	} {
		w, _ := mw.CreatePart(textproto.MIMEHeader{
			"Content-Type":              {part.contentType},
			"Content-Transfer-Encoding": {"quoted-printable"},
		})
		writeQuotedPrintable(w, part.body)
	}

	_ = mw.Close()

	return mime.FormatMediaType("multipart/alternative", map[string]string{"boundary": mw.Boundary()}), b.Bytes(), nil
}

// writeBase64 encodes data in lines of 76 characters (RFC 2045). w writes
// into a bytes.Buffer, so its errors cannot occur.
func writeBase64(w io.Writer, data []byte) {
	enc := base64.StdEncoding.EncodeToString(data)

	for len(enc) > 0 {
		n := min(len(enc), 76)
		_, _ = io.WriteString(w, enc[:n]+"\r\n")
		enc = enc[n:]
	}
}

// writeQuotedPrintable encodes body with CRLF line breaks, whatever line
// breaks it came with. w writes into a bytes.Buffer, so its errors cannot
// occur.
func writeQuotedPrintable(w io.Writer, body string) {
	body = strings.ReplaceAll(body, "\r\n", "\n")

	qp := quotedprintable.NewWriter(w)
	_, _ = io.WriteString(qp, body)
	_ = qp.Close()
}

// randomHex returns 16 random bytes as hex.
func (s *Sender) randomHex() (string, error) {
	var buf [16]byte
	if _, err := io.ReadFull(s.random, buf[:]); err != nil {
		return "", fmt.Errorf("smtp: random: %w", err)
	}

	return hex.EncodeToString(buf[:]), nil
}

// domainOf returns the part of addr after the last "@".
func domainOf(addr string) string {
	return addr[strings.LastIndexByte(addr, '@')+1:]
}
