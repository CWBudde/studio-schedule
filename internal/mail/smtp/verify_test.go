package smtp

import (
	"bufio"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/textproto"
	"strings"
	"sync"
	"syscall"
	"testing"
)

// verbRecorder is a TCP proxy in front of a fake server that records the
// verbs the client sends, so a test sees what Verify said, not only what
// the fake server kept (it keeps a session only after DATA). It only works
// on a plain-text connection.
type verbRecorder struct {
	ln net.Listener

	mu    sync.Mutex
	verbs []string
}

func recordVerbs(t *testing.T, srv *fakeServer) *verbRecorder {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	r := &verbRecorder{ln: ln}

	var wg sync.WaitGroup

	wg.Go(func() {
		for {
			client, err := ln.Accept()
			if err != nil {
				return
			}

			wg.Go(func() { r.relay(client, srv.ln.Addr().String()) })
		}
	})

	t.Cleanup(func() {
		_ = ln.Close()
		wg.Wait()
	})

	return r
}

// relay copies the server's replies to client and the client's lines to
// the server, recording the first word of each.
func (r *verbRecorder) relay(client net.Conn, addr string) {
	defer func() { _ = client.Close() }()

	server, err := net.Dial("tcp", addr)
	if err != nil {
		return
	}
	defer func() { _ = server.Close() }()

	go func() { _, _ = io.Copy(client, server) }()

	lines := bufio.NewScanner(client)
	for lines.Scan() {
		verb, _, _ := strings.Cut(lines.Text(), " ")

		r.mu.Lock()
		r.verbs = append(r.verbs, strings.ToUpper(verb))
		r.mu.Unlock()

		if _, err := fmt.Fprintf(server, "%s\r\n", lines.Text()); err != nil {
			return
		}
	}
}

func (r *verbRecorder) port() int { return r.ln.Addr().(*net.TCPAddr).Port }

func (r *verbRecorder) recorded() []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	return append([]string(nil), r.verbs...)
}

func TestVerifyLogsInWithoutSending(t *testing.T) {
	t.Parallel()

	srv := startFake(t)
	rec := recordVerbs(t, srv)

	s := newTestSender(t, Config{
		Host: "127.0.0.1", Port: rec.port(), TLS: TLSNone, Username: "mailer", Password: "change-me",
	})

	if err := s.Verify(t.Context()); err != nil {
		t.Fatalf("Verify: %v", err)
	}

	// Verify waits for the reply to QUIT, which the proxy relayed first.
	if got := strings.Join(rec.recorded(), " "); got != "EHLO AUTH QUIT" {
		t.Errorf("Verify sent %s, want EHLO AUTH QUIT", got)
	}

	checkRecorded(t, srv, 0)
}

func TestVerifyStartTLS(t *testing.T) {
	t.Parallel()

	pool := x509.NewCertPool()
	srv := startFake(t, withTLS(t, false, pool))
	s := senderFor(t, srv, Config{TLS: TLSStartTLS, Username: "mailer", Password: "change-me"}, pool)

	if err := s.Verify(t.Context()); err != nil {
		t.Fatalf("Verify: %v", err)
	}

	checkRecorded(t, srv, 0)
}

func TestVerifyAuthRejected(t *testing.T) {
	t.Parallel()

	srv := startFake(t, withReply("AUTH", "535 authentication failed"))
	s := senderFor(t, srv, Config{Username: "mailer", Password: "wrong"}, nil)

	err := s.Verify(t.Context())
	if want := fmt.Sprintf("smtp verify 127.0.0.1:%d: AUTH: ", srv.port()); err == nil ||
		!strings.Contains(err.Error(), want) {
		t.Fatalf("Verify = %v, want an error containing %q", err, want)
	}

	var tpErr *textproto.Error
	if !errors.As(err, &tpErr) || tpErr.Code != 535 {
		t.Errorf("Verify = %v, want it to wrap the server's 535 reply", err)
	}

	if strings.Contains(err.Error(), "wrong") {
		t.Errorf("Verify = %v, which contains the password", err)
	}
}

func TestVerifyStartTLSNotOffered(t *testing.T) {
	t.Parallel()

	srv := startFake(t) // no STARTTLS
	s := senderFor(t, srv, Config{TLS: TLSStartTLS}, nil)

	err := s.Verify(t.Context())
	if err == nil || !strings.Contains(err.Error(), "server does not offer STARTTLS") {
		t.Fatalf("Verify = %v, want an error about missing STARTTLS", err)
	}
}

func TestVerifyConnectionRefused(t *testing.T) {
	t.Parallel()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()

	s := newTestSender(t, Config{Host: "127.0.0.1", Port: port, TLS: TLSNone})

	err = s.Verify(t.Context())
	if want := fmt.Sprintf("smtp connect 127.0.0.1:%d", port); err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("Verify = %v, want an error containing %q", err, want)
	}

	if !errors.Is(err, syscall.ECONNREFUSED) {
		t.Errorf("Verify = %v, want it to wrap ECONNREFUSED", err)
	}
}
