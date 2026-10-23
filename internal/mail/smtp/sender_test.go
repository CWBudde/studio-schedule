package smtp

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"errors"
	"math/big"
	"mime"
	"net"
	"net/textproto"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/mail"
)

// session is what the fake server recorded for one mail transaction.
type session struct {
	auth string // decoded AUTH PLAIN credentials
	from string
	rcpt []string
	data []byte // after dot-unstuffing, with LF line endings
	tls  bool
}

// fakeServer is a minimal in-process SMTP server on 127.0.0.1.
type fakeServer struct {
	ln         net.Listener
	tlsConfig  *tls.Config // non-nil: offers STARTTLS, or speaks TLS at once if implicit
	implicit   bool
	silent     bool // accepts connections but never greets
	rejectRcpt bool // answers RCPT with 550

	mu       sync.Mutex
	sessions []session
	conns    int
	done     chan struct{}
}

type fakeOption func(*fakeServer)

func startFake(t *testing.T, opts ...fakeOption) *fakeServer {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	s := &fakeServer{ln: ln, done: make(chan struct{})}
	for _, o := range opts {
		o(s)
	}

	var wg sync.WaitGroup

	wg.Go(func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}

			wg.Go(func() { s.handle(conn) })
		}
	})

	t.Cleanup(func() {
		close(s.done)
		_ = ln.Close()
		wg.Wait()
	})

	return s
}

func (s *fakeServer) port() int { return s.ln.Addr().(*net.TCPAddr).Port }

func (s *fakeServer) recorded() []session {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]session(nil), s.sessions...)
}

func (s *fakeServer) connections() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.conns
}

func (s *fakeServer) handle(conn net.Conn) {
	s.mu.Lock()
	s.conns++
	s.mu.Unlock()

	defer func() { _ = conn.Close() }()

	go func() {
		<-s.done
		_ = conn.Close()
	}()

	if s.silent {
		<-s.done

		return
	}

	isTLS := false

	if s.implicit {
		tc := tls.Server(conn, s.tlsConfig)
		if err := tc.Handshake(); err != nil {
			return
		}

		conn, isTLS = tc, true
	}

	tp := textproto.NewConn(conn)
	reply := func(format string, args ...any) { _ = tp.PrintfLine(format, args...) }

	reply("220 fake.example.org ESMTP")

	var sess session

	for {
		line, err := tp.ReadLine()
		if err != nil {
			return
		}

		verb, arg, _ := strings.Cut(line, " ")

		switch strings.ToUpper(verb) {
		case "EHLO", "HELO":
			ext := []string{"fake.example.org", "8BITMIME", "AUTH PLAIN"}
			if s.tlsConfig != nil && !isTLS {
				ext = append(ext, "STARTTLS")
			}

			for i, e := range ext {
				sep := "-"
				if i == len(ext)-1 {
					sep = " "
				}

				reply("250%s%s", sep, e)
			}
		case "STARTTLS":
			reply("220 ready")

			tc := tls.Server(conn, s.tlsConfig)
			if err := tc.Handshake(); err != nil {
				return
			}

			conn, isTLS = tc, true
			tp = textproto.NewConn(conn)
		case "AUTH":
			mech, payload, _ := strings.Cut(arg, " ")
			if mech != "PLAIN" {
				reply("504 unsupported")

				continue
			}

			b, err := base64.StdEncoding.DecodeString(payload)
			if err != nil {
				reply("501 bad base64")

				continue
			}

			sess.auth = string(b)

			reply("235 ok")
		case "MAIL":
			sess.from = arg

			reply("250 ok")
		case "RCPT":
			if s.rejectRcpt {
				reply("550 no such user")

				continue
			}

			sess.rcpt = append(sess.rcpt, arg)

			reply("250 ok")
		case "DATA":
			reply("354 go ahead")

			data, err := tp.ReadDotBytes()
			if err != nil {
				return
			}

			sess.data, sess.tls = data, isTLS

			s.mu.Lock()
			s.sessions = append(s.sessions, sess)
			s.mu.Unlock()

			sess = session{auth: sess.auth}

			reply("250 queued")
		case "RSET", "NOOP":
			reply("250 ok")
		case "QUIT":
			reply("221 bye")

			return
		default:
			reply("502 not implemented")
		}
	}
}

// withTLS makes the server offer STARTTLS (or implicit TLS) with a fresh
// self-signed certificate for 127.0.0.1; pool receives the certificate.
func withTLS(t *testing.T, implicit bool, pool *x509.CertPool) fakeOption {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "fake smtp"},
		NotBefore:    time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC),
		NotAfter:     time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC),
		IPAddresses:  []net.IP{net.IPv4(127, 0, 0, 1)},
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}

	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}

	pool.AddCert(cert)

	return func(s *fakeServer) {
		s.tlsConfig = &tls.Config{
			Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}},
			MinVersion:   tls.VersionTLS12,
		}
		s.implicit = implicit
	}
}

func senderFor(t *testing.T, srv *fakeServer, cfg Config, pool *x509.CertPool) *Sender {
	t.Helper()

	cfg.Host = "127.0.0.1"
	cfg.Port = srv.port()

	if cfg.TLS == "" {
		cfg.TLS = TLSNone
	}

	s := newTestSender(t, cfg)
	s.rootCAs = pool

	return s
}

func checkDelivered(t *testing.T, sess session) {
	t.Helper()

	m := testMessage()

	if sess.from != "FROM:<noreply@example.org>" && !strings.HasPrefix(sess.from, "FROM:<noreply@example.org> ") {
		t.Errorf("MAIL %s, want FROM:<noreply@example.org>", sess.from)
	}

	if len(sess.rcpt) != 1 || sess.rcpt[0] != "TO:<anna@example.org>" {
		t.Errorf("RCPT %v, want [TO:<anna@example.org>]", sess.rcpt)
	}

	msg := parse(t, bytes.ReplaceAll(sess.data, []byte("\n"), []byte("\r\n")))

	subject, err := new(mime.WordDecoder).DecodeHeader(msg.Header.Get("Subject"))
	if err != nil || subject != m.Subject {
		t.Errorf("Subject = %q (%v), want %q", subject, err, m.Subject)
	}

	// The lone "." line survives dot-stuffing.
	if !bytes.Contains(sess.data, []byte("\n.\n")) {
		t.Errorf("DATA lost the lone dot line:\n%s", sess.data)
	}
}

func TestSendPlain(t *testing.T) {
	t.Parallel()

	srv := startFake(t)
	s := senderFor(t, srv, Config{}, nil)

	if err := s.Send(t.Context(), testMessage()); err != nil {
		t.Fatalf("Send: %v", err)
	}

	got := srv.recorded()
	if len(got) != 1 {
		t.Fatalf("server recorded %d mails, want 1", len(got))
	}

	checkDelivered(t, got[0])

	if got[0].auth != "" || got[0].tls {
		t.Errorf("auth %q, tls %v; want neither", got[0].auth, got[0].tls)
	}
}

func TestSendAuthOnLocalhostWithoutTLS(t *testing.T) {
	t.Parallel()

	srv := startFake(t)
	s := senderFor(t, srv, Config{Username: "mailer", Password: "change-me"}, nil)

	if err := s.Send(t.Context(), testMessage()); err != nil {
		t.Fatalf("Send: %v", err)
	}

	got := srv.recorded()
	if len(got) != 1 || got[0].auth != "\x00mailer\x00change-me" {
		t.Fatalf("sessions = %+v, want one with PLAIN auth", got)
	}
}

func TestSendStartTLS(t *testing.T) {
	t.Parallel()

	pool := x509.NewCertPool()
	srv := startFake(t, withTLS(t, false, pool))
	s := senderFor(t, srv, Config{TLS: TLSStartTLS, Username: "mailer", Password: "change-me"}, pool)

	if err := s.Send(t.Context(), testMessage()); err != nil {
		t.Fatalf("Send: %v", err)
	}

	got := srv.recorded()
	if len(got) != 1 {
		t.Fatalf("server recorded %d mails, want 1", len(got))
	}

	checkDelivered(t, got[0])

	if !got[0].tls || got[0].auth != "\x00mailer\x00change-me" {
		t.Errorf("tls %v, auth %q; want TLS and PLAIN auth", got[0].tls, got[0].auth)
	}
}

func TestSendImplicitTLS(t *testing.T) {
	t.Parallel()

	pool := x509.NewCertPool()
	srv := startFake(t, withTLS(t, true, pool))
	s := senderFor(t, srv, Config{TLS: TLSImplicit}, pool)

	if err := s.Send(t.Context(), testMessage()); err != nil {
		t.Fatalf("Send: %v", err)
	}

	got := srv.recorded()
	if len(got) != 1 || !got[0].tls {
		t.Fatalf("sessions = %+v, want one over TLS", got)
	}

	checkDelivered(t, got[0])
}

func TestSendRejectsUntrustedCertificate(t *testing.T) {
	t.Parallel()

	srv := startFake(t, withTLS(t, false, x509.NewCertPool()))
	s := senderFor(t, srv, Config{TLS: TLSStartTLS}, x509.NewCertPool())

	if err := s.Send(t.Context(), testMessage()); err == nil {
		t.Fatal("Send trusted a self-signed certificate")
	}

	if n := len(srv.recorded()); n != 0 {
		t.Fatalf("server recorded %d mails, want 0", n)
	}
}

func TestSendStartTLSNotOffered(t *testing.T) {
	t.Parallel()

	srv := startFake(t) // no STARTTLS
	s := senderFor(t, srv, Config{TLS: TLSStartTLS}, nil)

	err := s.Send(t.Context(), testMessage())
	if err == nil || !strings.Contains(err.Error(), "STARTTLS") {
		t.Fatalf("Send = %v, want an error about missing STARTTLS", err)
	}

	if n := len(srv.recorded()); n != 0 {
		t.Fatalf("server recorded %d mails without TLS, want 0", n)
	}
}

func TestSendRecipientRejected(t *testing.T) {
	t.Parallel()

	srv := startFake(t, func(s *fakeServer) { s.rejectRcpt = true })
	s := senderFor(t, srv, Config{}, nil)

	err := s.Send(t.Context(), testMessage())
	if err == nil {
		t.Fatal("Send succeeded although the server rejected the recipient")
	}

	if !Permanent(err) || !errors.Is(err, mail.ErrPermanent) {
		t.Errorf("Permanent(%v) = false or no mail.ErrPermanent, want both for a 550 reply", err)
	}
}

func TestSendInvalidMessage(t *testing.T) {
	t.Parallel()

	srv := startFake(t)
	s := senderFor(t, srv, Config{}, nil)

	m := testMessage()
	m.To = "not an address"

	err := s.Send(t.Context(), m)
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("Send = %v, want ErrValidation", err)
	}

	if !Permanent(err) {
		t.Errorf("Permanent(%v) = false, want true for an invalid message", err)
	}

	if n := srv.connections(); n != 0 {
		t.Fatalf("Send connected %d times for an invalid message", n)
	}
}

func TestSendContextCanceled(t *testing.T) {
	t.Parallel()

	srv := startFake(t, func(s *fakeServer) { s.silent = true })
	s := senderFor(t, srv, Config{}, nil)

	ctx, cancel := context.WithCancel(t.Context())

	errc := make(chan error, 1)

	go func() { errc <- s.Send(ctx, testMessage()) }()

	time.Sleep(50 * time.Millisecond) // let Send block on the greeting
	cancel()

	select {
	case err := <-errc:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Send = %v, want context.Canceled", err)
		}

		if Permanent(err) || errors.Is(err, mail.ErrPermanent) {
			t.Errorf("Permanent(%v) = true or mail.ErrPermanent, want neither", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Send did not return after the context was canceled")
	}
}

func TestSendTimeout(t *testing.T) {
	t.Parallel()

	srv := startFake(t, func(s *fakeServer) { s.silent = true })
	s := senderFor(t, srv, Config{}, nil)
	s.timeout = 100 * time.Millisecond

	err := s.Send(t.Context(), testMessage())
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Send = %v, want context.DeadlineExceeded", err)
	}
}

func TestSendContextAlreadyCanceled(t *testing.T) {
	t.Parallel()

	srv := startFake(t)
	s := senderFor(t, srv, Config{}, nil)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if err := s.Send(ctx, testMessage()); !errors.Is(err, context.Canceled) {
		t.Fatalf("Send = %v, want context.Canceled", err)
	}

	if n := len(srv.recorded()); n != 0 {
		t.Fatalf("server recorded %d mails, want 0", n)
	}
}
