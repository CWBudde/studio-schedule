package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/service"
	"github.com/cwbudde/studio-schedule/internal/store"
	"github.com/cwbudde/studio-schedule/internal/store/memory"
	"github.com/cwbudde/studio-schedule/internal/webpush"
)

// fakeRenderer renders every input as out, or fails with err.
type fakeRenderer struct {
	out []byte
	err error
}

func (f fakeRenderer) Render(context.Context, []byte) ([]byte, error) { return f.out, f.err }

// pdfOf is a cliRun.pdf that starts r.
func pdfOf(r service.ReceiptRenderer) func() (service.ReceiptRenderer, func()) {
	return func() (service.ReceiptRenderer, func()) { return r, func() {} }
}

// renderingPDF renders a minimal PDF.
var renderingPDF = pdfOf(fakeRenderer{out: []byte("%PDF-1.7\n%fake\n")})

// with returns r with the environment variables kv added.
func (r cliRun) with(kv ...string) cliRun {
	env := maps.Clone(r.env)
	if env == nil {
		env = map[string]string{}
	}

	for i := 0; i < len(kv); i += 2 {
		env[kv[i]] = kv[i+1]
	}

	r.env = env

	return r
}

// doctorResults runs doctor with --output json and returns its report by
// check, and the exit status.
func doctorResults(t *testing.T, r cliRun, args ...string) (map[string]doctorCheck, int) {
	t.Helper()

	res := r.exec(t, append([]string{"doctor", "--output", "json"}, args...)...)

	var report doctorReport
	if err := json.Unmarshal([]byte(res.stdout), &report); err != nil {
		t.Fatalf("doctor printed no report (%v): stdout %q, stderr %q", err, res.stdout, res.stderr)
	}

	if report.OK != (res.code == 0) {
		t.Errorf("ok %v with exit %d", report.OK, res.code)
	}

	// A failed check is in the report: stderr gets no problem document.
	if res.stderr != "" {
		t.Errorf("stderr %q, want none", res.stderr)
	}

	byName := map[string]doctorCheck{}
	for _, c := range report.Checks {
		byName[c.Name] = c
	}

	return byName, res.code
}

// wantResults fails unless every check of want has its result, and the
// detail of the check contains the text after the result ("fail: run init").
func wantResults(t *testing.T, got map[string]doctorCheck, want map[string]string) {
	t.Helper()

	for name, w := range want {
		result, detail, _ := strings.Cut(w, ": ")
		if c := got[name]; c.Result != result || !strings.Contains(c.Detail, detail) {
			t.Errorf("check %s = %s (%s), want %s with %q", name, c.Result, c.Detail, result, detail)
		}
	}
}

// TestDoctorHealthy passes on both stores with the demo data: every check
// ok, except smtp and vapid, which are not configured.
func TestDoctorHealthy(t *testing.T) {
	t.Parallel()

	stores(t, func(t *testing.T, r cliRun) {
		r = r.with("BASE_URL", "https://schedule.example.org")
		r.pdf = renderingPDF

		text := r.exec(t, "doctor")
		if text.code != 0 || text.stderr != "" {
			t.Fatalf("doctor: exit %d, stderr %q", text.code, text.stderr)
		}

		golden(t, "doctor/healthy.txt", text.stdout)

		js := r.exec(t, "doctor", "--output", "json")
		if js.code != 0 {
			t.Fatalf("doctor --output json: exit %d, stderr %q", js.code, js.stderr)
		}

		golden(t, "doctor/healthy.json", js.stdout)
	})
}

// TestDoctorWithoutSettings fails the settings check of a database that
// has none, on both stores.
func TestDoctorWithoutSettings(t *testing.T) {
	t.Parallel()

	for name, r := range map[string]func(t *testing.T) cliRun{
		"memory":   func(*testing.T) cliRun { return cliRun{st: memory.New()} },
		"postgres": func(t *testing.T) cliRun { return cliRun{}.with("DATABASE_URL", newTestDatabase(t)) },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, code := doctorResults(t, r(t))
			if code != 1 {
				t.Errorf("exit %d, want 1", code)
			}

			wantResults(t, got, map[string]string{
				"database": "ok: connected", "schema": "ok: up to date", "settings": "fail: no school settings: run init",
			})
		})
	}
}

// TestDoctorSettingsInvalid fails the settings check of a time zone that
// does not load, and of settings it cannot read.
func TestDoctorSettingsInvalid(t *testing.T) {
	t.Parallel()

	st := demoStore(t)

	err := st.InTx(t.Context(), store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		s, err := tx.Settings().Get(ctx)
		if err != nil {
			return err
		}

		s.Timezone = "Mars/Olympus_Mons"

		return tx.Settings().Put(ctx, s)
	})
	if err != nil {
		t.Fatal(err)
	}

	got, code := doctorResults(t, cliRun{st: st})
	if code != 1 {
		t.Errorf("exit %d, want 1", code)
	}

	wantResults(t, got, map[string]string{"settings": `fail: unknown timezone "Mars/Olympus_Mons"`})

	got, _ = doctorResults(t, cliRun{st: unreadableStore{st}})
	wantResults(t, got, map[string]string{"database": "ok", "settings": "fail: the disk is unreadable"})
}

// unreadableStore fails every transaction.
type unreadableStore struct{ store.Store }

func (unreadableStore) InTx(context.Context, store.TxMode, func(context.Context, store.Tx) error) error {
	return errors.New("the disk is unreadable")
}

// TestDoctorDatabase fails the database check without a database or with
// one it cannot reach, and the schema check of a schema that does not
// match; the checks after them skip.
func TestDoctorDatabase(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		name string
		run  cliRun
		want map[string]string
	}{
		{
			"none",
			cliRun{},
			map[string]string{
				"database": "fail: no database: set DATABASE_URL", "schema": "skip: no database",
				"settings": "skip: no database",
			},
		},
		{
			"unreachable", cliRun{}.with("DATABASE_URL", "postgres://studio@127.0.0.1:1/studio?connect_timeout=1"),
			map[string]string{
				"database": "fail: 127.0.0.1:1", "schema": "skip: the database is unreachable",
				"settings": "skip: the database is unreachable",
			},
		},
		{
			"outdated", cliRun{open: failingOpener(errSchemaOutdated)}.with("DATABASE_URL", "postgres://db.example.test/studio"),
			map[string]string{
				"database": "ok: connected", "schema": "fail: run `studio-schedule migrate up`",
				"settings": "skip: the schema does not match",
			},
		},
		{
			"newer", cliRun{open: failingOpener(errSchemaNewer)}.with("DATABASE_URL", "postgres://db.example.test/studio"),
			map[string]string{"database": "ok: connected", "schema": "fail: newer than this binary"},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			got, code := doctorResults(t, c.run)
			if code != 1 {
				t.Errorf("exit %d, want 1", code)
			}

			wantResults(t, got, c.want)

			if d := got["database"].Detail; strings.Contains(d, "\n") {
				t.Errorf("database detail %q has several lines", d)
			}
		})
	}
}

// failingOpener is an opener that fails with err.
func failingOpener(err error) func(context.Context, string) (store.Store, func(), error) {
	return func(context.Context, string) (store.Store, func(), error) { return nil, nil, err }
}

// TestDoctorInvalidConfig fails the config check with config.Load's error;
// the checks that need the configuration skip, the receipt check runs.
func TestDoctorInvalidConfig(t *testing.T) {
	t.Parallel()

	r := cliRun{pdf: renderingPDF}.with("LOG_LEVEL", "loud")

	got, code := doctorResults(t, r)
	if code != 1 {
		t.Errorf("exit %d, want 1", code)
	}

	want := map[string]string{"config": `fail: LOG_LEVEL: "loud" is not a log level`, "receipts": "ok"}
	for _, name := range []string{"database", "schema", "settings", "smtp", "vapid", "base-url"} {
		want[name] = "skip: the configuration is invalid"
	}

	wantResults(t, got, want)

	got, _ = doctorResults(t, r, "--log-level", "debug") // the flag wins
	wantResults(t, got, map[string]string{"config": "ok"})
}

// TestDoctorSMTP logs in to the mail server without sending, and fails
// when the server refuses the login or the configuration.
func TestDoctorSMTP(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		name     string
		username string
		tls      string
		reject   bool
		want     string
	}{
		{"login", "mailer", "none", false, "ok: logged in to 127.0.0.1 as mailer"},
		{"no login", "", "none", false, "ok: connected to 127.0.0.1 (no login configured)"},
		{"rejected", "mailer", "none", true, "fail: AUTH: 535"},
		{"starttls not offered", "", "starttls", false, "fail: server does not offer STARTTLS"},
		{"invalid", "", "ssl", false, "fail: invalid smtp configuration"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			srv := startSMTP(t, c.reject)
			r := cliRun{st: demoStore(t)}.with("SMTP_HOST", "127.0.0.1", "SMTP_PORT", strconv.Itoa(srv.port()),
				"SMTP_TLS", c.tls, "SMTP_FROM", "noreply@example.org")

			if c.username != "" {
				r = r.with("SMTP_USERNAME", c.username, "SMTP_PASSWORD", "smtp-secret-pw")
			}

			got, _ := doctorResults(t, r)
			wantResults(t, got, map[string]string{"smtp": c.want})

			if d := got["smtp"].Detail; strings.Contains(d, "smtp-secret-pw") {
				t.Errorf("smtp detail %q contains the password", d)
			}

			if verbs := srv.recorded(); strings.Contains(verbs, "MAIL") || strings.Contains(verbs, "DATA") {
				t.Errorf("doctor sent %s, want no mail", verbs)
			}
		})
	}
}

// smtpServer is a minimal SMTP server on 127.0.0.1 that offers AUTH PLAIN
// and accepts or rejects every login (reject), and accepts every mail or
// rejects every recipient (rejectRCPT); it records the verbs it gets and
// the messages it accepts.
type smtpServer struct {
	ln         net.Listener
	reject     bool
	rejectRCPT bool

	mu       sync.Mutex
	verbs    []string
	messages []string
}

func startSMTP(t *testing.T, reject bool) *smtpServer {
	t.Helper()

	return startSMTPServer(t, &smtpServer{reject: reject})
}

// startSMTPServer starts s on a free port of 127.0.0.1.
func startSMTPServer(t *testing.T, s *smtpServer) *smtpServer {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	s.ln = ln

	var wg sync.WaitGroup

	wg.Go(func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}

			wg.Go(func() { s.serve(conn) })
		}
	})

	t.Cleanup(func() {
		_ = ln.Close()
		wg.Wait()
	})

	return s
}

func (s *smtpServer) port() int { return s.ln.Addr().(*net.TCPAddr).Port }

func (s *smtpServer) recorded() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return strings.Join(s.verbs, " ")
}

// received returns the messages the server accepted.
func (s *smtpServer) received() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return slices.Clone(s.messages)
}

// receive reads a message up to the line "." and records it.
func (s *smtpServer) receive(lines *bufio.Scanner) bool {
	var msg strings.Builder

	for lines.Scan() {
		if lines.Text() == "." {
			s.mu.Lock()
			s.messages = append(s.messages, msg.String())
			s.mu.Unlock()

			return true
		}

		msg.WriteString(lines.Text() + "\n")
	}

	return false
}

func (s *smtpServer) serve(conn net.Conn) {
	defer func() { _ = conn.Close() }()

	reply := func(line string) bool {
		_, err := fmt.Fprintf(conn, "%s\r\n", line)

		return err == nil
	}

	if !reply("220 mail.example.test ESMTP") {
		return
	}

	lines := bufio.NewScanner(conn)
	for lines.Scan() {
		verb, _, _ := strings.Cut(lines.Text(), " ")
		verb = strings.ToUpper(verb)

		s.mu.Lock()
		s.verbs = append(s.verbs, verb)
		s.mu.Unlock()

		var ok bool

		switch verb {
		case "EHLO":
			ok = reply("250-mail.example.test") && reply("250 AUTH PLAIN")
		case "AUTH":
			if s.reject {
				ok = reply("535 authentication failed")
			} else {
				ok = reply("235 ok")
			}
		case "MAIL":
			ok = reply("250 ok")
		case "RCPT":
			if s.rejectRCPT {
				ok = reply("550 no such user here")
			} else {
				ok = reply("250 ok")
			}
		case "DATA":
			ok = reply("354 end with .") && s.receive(lines) && reply("250 queued")
		case "QUIT":
			_ = reply("221 bye")

			return
		default:
			ok = reply("502 not implemented")
		}

		if !ok {
			return
		}
	}
}

// TestDoctorVAPID passes with a key pair that parses and matches.
func TestDoctorVAPID(t *testing.T) {
	t.Parallel()

	keys, err := webpush.GenerateKeys()
	if err != nil {
		t.Fatal(err)
	}

	r := cliRun{st: demoStore(t)}.with("VAPID_PUBLIC_KEY", keys.PublicKey(), "VAPID_PRIVATE_KEY", keys.PrivateKey(),
		"VAPID_SUBJECT", "mailto:office@example.org")

	res := r.exec(t, "doctor", "--output", "json")
	if strings.Contains(res.stdout, keys.PrivateKey()) {
		t.Errorf("doctor printed the private key: %s", res.stdout)
	}

	got, _ := doctorResults(t, r)
	wantResults(t, got, map[string]string{"vapid": "ok: keys parse and match"})
}

// TestDoctorBaseURL wants https, warns of plain http on localhost or no
// BASE_URL (local development), and fails plain http elsewhere.
func TestDoctorBaseURL(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		baseURL string
		want    string
	}{
		{"", "warn: links point to http://localhost:8080/; local development only"},
		{"https://schedule.example.org", "ok: https://schedule.example.org"},
		{"http://localhost:8080", "warn: http://localhost:8080 is plain http"},
		{"http://127.0.0.1:8080", "warn: plain http"},
		{"http://[::1]:8080", "warn: plain http"},
		{"http://schedule.example.org", "fail: http://schedule.example.org is plain http: session cookies"},
	} {
		t.Run(c.baseURL, func(t *testing.T) {
			t.Parallel()

			r := cliRun{st: demoStore(t), pdf: renderingPDF}
			if c.baseURL != "" {
				r = r.with("BASE_URL", c.baseURL)
			}

			got, code := doctorResults(t, r)
			wantResults(t, got, map[string]string{"base-url": c.want})

			// warn and skip do not fail
			if wantCode := map[bool]int{true: 1, false: 0}[strings.HasPrefix(c.want, "fail")]; code != wantCode {
				t.Errorf("exit %d, want %d", code, wantCode)
			}
		})
	}
}

// TestDoctorReceipts renders a sample receipt: a failing renderer or one
// that returns no PDF fails, a build without a renderer skips the check.
func TestDoctorReceipts(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		name string
		pdf  func() (service.ReceiptRenderer, func())
		want string
	}{
		{"renders", renderingPDF, "ok: a sample receipt renders as PDF"},
		{"fails", pdfOf(fakeRenderer{err: errors.New("the module trapped")}), "fail: the module trapped"},
		{"no pdf", pdfOf(fakeRenderer{out: []byte("<html>")}), "fail: the renderer returned 6 bytes that are no PDF"},
		{"none", nil, "skip: this build has no receipt renderer"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			got, _ := doctorResults(t, cliRun{st: demoStore(t), pdf: c.pdf})
			wantResults(t, got, map[string]string{"receipts": c.want})
		})
	}
}

// TestDoctorReceiptModule renders the sample with the server's receipt
// renderer (typstwasm), as main sets it up.
func TestDoctorReceiptModule(t *testing.T) {
	t.Parallel()

	got, _ := doctorResults(t, cliRun{st: demoStore(t), pdf: startPDF})
	wantResults(t, got, map[string]string{"receipts": "ok: a sample receipt renders as PDF"})
}

// TestDoctorTerminal colors the results on a color terminal, and reports
// a failure on stderr after the report.
func TestDoctorTerminal(t *testing.T) {
	t.Parallel()

	res := cliRun{terminal: true, st: memory.New()}.exec(t, "doctor")
	if res.code != 1 {
		t.Errorf("exit %d, want 1", res.code)
	}

	for _, want := range []string{
		"\x1b[1mCheck\x1b[0m", "\x1b[32mok\x1b[0m", "\x1b[31mfail\x1b[0m", "\x1b[2mwarn\x1b[0m", "\x1b[2mskip\x1b[0m",
	} {
		if !strings.Contains(res.stdout, want) {
			t.Errorf("stdout lacks %q:\n%s", want, res.stdout)
		}
	}

	if res.stderr != "error: "+errChecksFailed.Error()+"\n" {
		t.Errorf("stderr = %q, want the failure", res.stderr)
	}
}
