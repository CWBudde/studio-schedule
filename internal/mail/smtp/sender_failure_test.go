package smtp

import (
	"bytes"
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/textproto"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/mail"
)

// checkTransient fails unless err is a transient error: neither Permanent
// nor wrapping mail.ErrPermanent.
func checkTransient(t *testing.T, err error) {
	t.Helper()

	if Permanent(err) || errors.Is(err, mail.ErrPermanent) {
		t.Errorf("Permanent(%v) = true or mail.ErrPermanent, want a transient error", err)
	}
}

func checkRecorded(t *testing.T, srv *fakeServer, want int) {
	t.Helper()

	if n := len(srv.recorded()); n != want {
		t.Errorf("server recorded %d mails, want %d", n, want)
	}
}

func TestSendConnectionRefused(t *testing.T) {
	t.Parallel()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()

	s := newTestSender(t, Config{Host: "127.0.0.1", Port: port, TLS: TLSNone})

	err = s.Send(t.Context(), testMessage())
	if want := fmt.Sprintf("smtp connect 127.0.0.1:%d", port); err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("Send = %v, want an error containing %q", err, want)
	}

	if !errors.Is(err, syscall.ECONNREFUSED) {
		t.Errorf("Send = %v, want it to wrap ECONNREFUSED", err)
	}

	checkTransient(t, err)
}

// TestSendServerReplies pins how each rejecting reply of the server ends a
// Send: the step in the error, the reply code, and whether it is permanent.
func TestSendServerReplies(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		opt       fakeOption
		auth      bool
		code      int
		step      string // the failing step, after "smtp "
		detail    string // the failing command in the error, "" for none
		permanent bool
		recorded  int
	}{
		{"greeting 554", func(s *fakeServer) { s.greeting = "554 go away" }, false, 554, "greeting", "", true, 0},
		{"greeting 421", func(s *fakeServer) { s.greeting = "421 busy" }, false, 421, "greeting", "", false, 0},
		{"AUTH 535", withReply("AUTH", "535 authentication failed"), true, 535, "send", "AUTH: ", true, 0},
		{"AUTH 454", withReply("AUTH", "454 temporary authentication failure"), true, 454, "send", "AUTH: ", false, 0},
		{"MAIL 550", withReply("MAIL", "550 sender rejected"), false, 550, "send", "MAIL FROM: ", true, 0},
		{"MAIL 553", withReply("MAIL", "553 sender address invalid"), false, 553, "send", "MAIL FROM: ", true, 0},
		{"MAIL 451", withReply("MAIL", "451 try again later"), false, 451, "send", "MAIL FROM: ", false, 0},
		{"RCPT 450", withReply("RCPT", "450 mailbox busy"), false, 450, "send", "RCPT TO: ", false, 0},
		{"RCPT 452", withReply("RCPT", "452 insufficient storage"), false, 452, "send", "RCPT TO: ", false, 0},
		{"DATA 554", withReply("DATA", "554 no valid recipients"), false, 554, "send", "DATA: ", true, 0},
		{"DATA 451", withReply("DATA", "451 try again later"), false, 451, "send", "DATA: ", false, 0},
		// The server has read the whole message, yet the sender reports the
		// failure, so the job sends it again (451) or gives up (554).
		{"after dot 554", func(s *fakeServer) { s.afterDot = "554 message refused" }, false, 554, "send", "DATA: ", true, 1},
		{"after dot 451", func(s *fakeServer) { s.afterDot = "451 try again later" }, false, 451, "send", "DATA: ", false, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			srv := startFake(t, tt.opt)

			cfg := Config{}
			if tt.auth {
				cfg.Username, cfg.Password = "mailer", "change-me"
			}

			s := senderFor(t, srv, cfg, nil)

			err := s.Send(t.Context(), testMessage())
			if err == nil {
				t.Fatal("Send succeeded although the server rejected the mail")
			}

			want := fmt.Sprintf("smtp %s 127.0.0.1:%d: %s", tt.step, srv.port(), tt.detail)
			if !strings.Contains(err.Error(), want) {
				t.Errorf("Send = %v, want an error containing %q", err, want)
			}

			var tpErr *textproto.Error
			if !errors.As(err, &tpErr) || tpErr.Code != tt.code {
				t.Errorf("Send = %v, want it to wrap the server's %d reply", err, tt.code)
			}

			if Permanent(err) != tt.permanent || errors.Is(err, mail.ErrPermanent) != tt.permanent {
				t.Errorf("Permanent(%v) = %v, errors.Is(mail.ErrPermanent) = %v; want both %v",
					err, Permanent(err), errors.Is(err, mail.ErrPermanent), tt.permanent)
			}

			checkRecorded(t, srv, tt.recorded)
		})
	}
}

// TestSendAuthNotOffered pins that a server without AUTH, although a user
// name is configured, is a transient error: the job retries it.
func TestSendAuthNotOffered(t *testing.T) {
	t.Parallel()

	srv := startFake(t, func(s *fakeServer) { s.noAuth = true })
	s := senderFor(t, srv, Config{Username: "mailer", Password: "change-me"}, nil)

	err := s.Send(t.Context(), testMessage())
	if want := "smtp send 127.0.0.1:"; err == nil || !strings.Contains(err.Error(), want) ||
		!strings.Contains(err.Error(), "server does not offer AUTH") {
		t.Fatalf("Send = %v, want a send error about missing AUTH", err)
	}

	checkTransient(t, err)
	checkRecorded(t, srv, 0)
}

func TestSendStartTLSRejected(t *testing.T) {
	t.Parallel()

	pool := x509.NewCertPool()
	srv := startFake(t, withTLS(t, false, pool), withReply("STARTTLS", "454 TLS not available"))
	s := senderFor(t, srv, Config{TLS: TLSStartTLS}, pool)

	err := s.Send(t.Context(), testMessage())
	if err == nil || !strings.Contains(err.Error(), "STARTTLS: ") {
		t.Fatalf("Send = %v, want a STARTTLS error", err)
	}

	var tpErr *textproto.Error
	if !errors.As(err, &tpErr) || tpErr.Code != 454 {
		t.Errorf("Send = %v, want it to wrap the server's 454 reply", err)
	}

	checkTransient(t, err)
	checkRecorded(t, srv, 0)
}

// TestSendServerHangs checks that a server that stops answering mid-dialog
// ends the Send at its timeout, as a transient error.
func TestSendServerHangs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		hangAt   string
		detail   string
		recorded int
	}{
		{"MAIL", "MAIL FROM: ", 0},
		{hangAfterDot, "DATA: ", 1},
	}

	for _, tt := range tests {
		t.Run(tt.hangAt, func(t *testing.T) {
			t.Parallel()

			srv := startFake(t, withHang(tt.hangAt))
			s := senderFor(t, srv, Config{}, nil)
			s.timeout = 150 * time.Millisecond

			err := s.Send(t.Context(), testMessage())
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("Send = %v, want context.DeadlineExceeded", err)
			}

			if !strings.Contains(err.Error(), "smtp send ") || !strings.Contains(err.Error(), tt.detail) {
				t.Errorf("Send = %v, want a send error at %q", err, tt.detail)
			}

			checkTransient(t, err)
			checkRecorded(t, srv, tt.recorded)
		})
	}
}

// TestSendWriteFailsDuringData ends the context while the message is being
// written: the server reads its start and then stops reading, so the write
// of the rest blocks on full socket buffers until the connection is closed.
func TestSendWriteFailsDuringData(t *testing.T) {
	t.Parallel()

	srv := startFake(t, withHang(hangInData))
	s := senderFor(t, srv, Config{}, nil)
	s.timeout = 10 * time.Second // a guard only; the test cancels much earlier

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	go func() {
		select {
		case <-srv.hung:
			cancel()
		case <-ctx.Done():
		}
	}()

	// Far more than the start the server reads plus what the socket buffers
	// of both ends hold.
	m := testMessage()
	m.Attachments = []mail.Attachment{{
		Filename:    "large.pdf",
		ContentType: "application/pdf",
		Content:     bytes.Repeat([]byte("%PDF-1.7 example "), mail.MaxAttachmentsSize/17),
	}}

	err := s.Send(ctx, m)
	if !errors.Is(err, context.Canceled) || !errors.Is(err, net.ErrClosed) {
		t.Fatalf("Send = %v, want context.Canceled wrapping the closed connection", err)
	}

	if !strings.Contains(err.Error(), "smtp send ") || !strings.Contains(err.Error(), "DATA: ") {
		t.Errorf("Send = %v, want a send error in DATA", err)
	}

	checkTransient(t, err)
	checkRecorded(t, srv, 0)
}

var errNoEntropy = errors.New("no entropy")

// failingReader returns n bytes, then errNoEntropy.
type failingReader struct{ n int }

func (r *failingReader) Read(p []byte) (int, error) {
	if r.n == 0 {
		return 0, errNoEntropy
	}

	k := min(len(p), r.n)
	for i := range p[:k] {
		p[i] = 0xab
	}

	r.n -= k

	return k, nil
}

// TestSendRandomFails checks each random value a message needs: the
// Message-ID first, then the boundary of the alternative (HTML) body, then
// the boundary of the attachments. Send fails before it connects.
func TestSendRandomFails(t *testing.T) {
	t.Parallel()

	attachment := []mail.Attachment{{Filename: "SD-2026-0001.pdf", ContentType: "application/pdf", Content: []byte("%PDF")}}

	textOnly := testMessage()
	textOnly.HTML = ""

	withHTMLAndAttachment := testMessage()
	withHTMLAndAttachment.Attachments = attachment

	textAndAttachment := textOnly
	textAndAttachment.Attachments = attachment

	tests := []struct {
		name  string
		bytes int // what the reader returns before it fails
		m     mail.Message
	}{
		{"Message-ID", 0, textOnly},
		{"alternative boundary", 16, testMessage()},
		{"mixed boundary after alternative", 32, withHTMLAndAttachment},
		{"mixed boundary", 16, textAndAttachment},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			srv := startFake(t)
			s := senderFor(t, srv, Config{}, nil)
			s.random = &failingReader{n: tt.bytes}

			err := s.Send(t.Context(), tt.m)
			if !errors.Is(err, errNoEntropy) || !strings.Contains(err.Error(), "smtp: random: ") {
				t.Fatalf("Send = %v, want an smtp: random error wrapping %v", err, errNoEntropy)
			}

			checkTransient(t, err)

			if n := srv.connections(); n != 0 {
				t.Errorf("Send connected %d times without a message", n)
			}
		})
	}
}

// TestFailCountsTheConnectionDeadlineAsTheContexts pins fail's rule for the
// race TestSendTimeout and TestSendServerHangs cannot steer: the connection's
// deadline is the context's, and when it fires first, the error still wraps
// context.DeadlineExceeded (and stays transient).
func TestFailCountsTheConnectionDeadlineAsTheContexts(t *testing.T) {
	t.Parallel()

	s := newTestSender(t, Config{Host: "mail.example.org", TLS: TLSNone})
	readErr := fmt.Errorf("read tcp: %w", os.ErrDeadlineExceeded)

	err := s.fail(context.Background(), "send", readErr)
	if !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("fail = %v, want it to wrap context.DeadlineExceeded and the read error", err)
	}

	if want := "smtp send mail.example.org:25: context deadline exceeded (read tcp: i/o timeout)"; err.Error() != want {
		t.Errorf("fail = %q, want %q", err, want)
	}

	checkTransient(t, err)

	// Any other error of a live context is passed on as it is.
	other := errors.New("connection reset")
	if err := s.fail(context.Background(), "send", other); errors.Is(err, context.DeadlineExceeded) ||
		err.Error() != "smtp send mail.example.org:25: connection reset" {
		t.Errorf("fail = %v, want only the error itself", err)
	}
}
