package app_test

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/app"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/seed"
)

// mailTo returns the newest mailbox mail of template.
func (a *authApp) mailTo(template string) (to, text string) {
	a.t.Helper()

	res, body := a.call(http.MethodGet, "/api/v1/demo/mailbox", "")
	if res.StatusCode != http.StatusOK {
		a.t.Fatalf("mailbox: %d %s", res.StatusCode, body)
	}

	var mb struct {
		Mails []struct{ To, Text, Template string } `json:"mails"`
	}
	if err := json.Unmarshal(body, &mb); err != nil {
		a.t.Fatal(err)
	}

	for _, m := range mb.Mails {
		if m.Template == template {
			return m.To, m.Text
		}
	}

	a.t.Fatalf("no %s mail in the mailbox", template)

	return "", ""
}

// linkToken returns the token of the newest mail of template, whose link
// opens route.
func (a *authApp) linkToken(template, route string) string {
	a.t.Helper()

	_, text := a.mailTo(template)

	m := regexp.MustCompile(regexp.QuoteMeta("https://school.example.org/"+route) + `\?token=([A-Za-z0-9_-]+)`).
		FindStringSubmatch(text)
	if m == nil {
		a.t.Fatalf("no %s link in %q", route, text)
	}

	return m[1]
}

// demoMember returns the demo member's login.
func demoMember(t *testing.T) seed.Login {
	t.Helper()

	for _, l := range seed.DemoLogins() {
		if l.Role == domain.RoleMember {
			return l
		}
	}

	t.Fatal("no demo member")

	return seed.Login{}
}

func loginBody(email, pw string) string {
	return `{"email":"` + email + `","password":"` + pw + `"}`
}

func (a *authApp) mustLogin(email, pw string) *http.Cookie {
	a.t.Helper()

	res, body := a.call(http.MethodPost, "/api/v1/auth/login", loginBody(email, pw))
	if res.StatusCode != http.StatusOK {
		a.t.Fatalf("login %s: %d %s", email, res.StatusCode, body)
	}

	return sessionCookie(a.t, res)
}

func (a *authApp) want(method, path, body string, status int, cookies ...*http.Cookie) []byte {
	a.t.Helper()

	res, out := a.call(method, path, body, cookies...)
	if res.StatusCode != status {
		a.t.Fatalf("%s %s: %d %s, want %d", method, path, res.StatusCode, out, status)
	}

	return out
}

func TestPasswordResetFlow(t *testing.T) {
	t.Parallel()

	a := newAuthApp(t, app.Deps{Demo: true, DemoLogins: seed.DemoLogins()})
	m := demoMember(t)
	old := a.mustLogin(m.Email, m.Password)

	a.want(http.MethodPost, "/api/v1/auth/password-reset", `{"email":"nobody@example.org"}`, http.StatusAccepted)
	a.want(http.MethodPost, "/api/v1/auth/password-reset", `{"email":"`+m.Email+`","locale":"de"}`, http.StatusAccepted)

	if to, _ := a.mailTo("reset-password"); to != m.Email {
		t.Errorf("reset mail to %q, want %q", to, m.Email)
	}

	token := a.linkToken("reset-password", "reset-password")
	a.want(http.MethodPost, "/api/v1/auth/password-reset/confirm", `{"token":"`+token+`","password":"short"}`,
		http.StatusUnprocessableEntity)
	out := a.want(http.MethodPost, "/api/v1/auth/password-reset/confirm",
		`{"token":"`+token+`","password":"Sunshine"}`, http.StatusUnprocessableEntity)
	if !strings.Contains(string(out), `"location":"body.password"`) {
		t.Errorf("common password: %s, want an error at body.password", out)
	}

	a.want(http.MethodPost, "/api/v1/auth/password-reset/confirm", `{"token":"`+token+`","password":"battery staple"}`,
		http.StatusNoContent)
	a.want(http.MethodPost, "/api/v1/auth/password-reset/confirm", `{"token":"`+token+`","password":"battery staple"}`,
		http.StatusGone)

	a.want(http.MethodGet, "/api/v1/auth/me", "", http.StatusUnauthorized, old)
	a.want(http.MethodPost, "/api/v1/auth/login", loginBody(m.Email, m.Password), http.StatusUnauthorized)
	a.mustLogin(m.Email, "battery staple")

	if to, text := a.mailTo("password-changed"); to != m.Email || strings.Contains(text, "http") {
		t.Errorf("password-changed mail to %q: %q", to, text)
	}
}

// TestPasswordLengthInCharacters: the API and the service both count a
// password's length in characters, so a long password of multi-byte
// characters is accepted, and one too long is a 422 on the field.
func TestPasswordLengthInCharacters(t *testing.T) {
	t.Parallel()

	a := newAuthApp(t, app.Deps{Demo: true, DemoLogins: seed.DemoLogins()})
	m := demoMember(t)
	c := a.mustLogin(m.Email, m.Password)

	long := strings.Repeat("€", 100) // 300 bytes
	a.want(http.MethodPost, "/api/v1/auth/password",
		`{"currentPassword":"`+m.Password+`","newPassword":"`+long+`"}`, http.StatusNoContent, c)
	c = a.mustLogin(m.Email, long)

	out := a.want(http.MethodPost, "/api/v1/auth/password",
		`{"currentPassword":"`+long+`","newPassword":"`+strings.Repeat("€", 257)+`"}`,
		http.StatusUnprocessableEntity, c)
	if !strings.Contains(string(out), `"location":"body.newPassword"`) {
		t.Errorf("too long: %s, want an error at body.newPassword", out)
	}

	out = a.want(http.MethodPost, "/api/v1/auth/password",
		`{"currentPassword":"`+long+`","newPassword":"QWERTYUIOP"}`, http.StatusUnprocessableEntity, c)
	if !strings.Contains(string(out), `"location":"body.newPassword"`) {
		t.Errorf("common password: %s, want an error at body.newPassword", out)
	}
}

func TestChangePasswordAndEmailFlow(t *testing.T) {
	t.Parallel()

	a := newAuthApp(t, app.Deps{Demo: true, DemoLogins: seed.DemoLogins()})
	m := demoMember(t)
	c := a.mustLogin(m.Email, m.Password)
	other := a.mustLogin(m.Email, m.Password)

	out := a.want(http.MethodPost, "/api/v1/auth/password",
		`{"currentPassword":"wrong password","newPassword":"battery staple"}`, http.StatusUnprocessableEntity, c)
	if !strings.Contains(string(out), `"location":"body.currentPassword"`) {
		t.Errorf("wrong current password: %s, want an error at body.currentPassword", out)
	}

	a.want(http.MethodPost, "/api/v1/auth/password",
		`{"currentPassword":"`+m.Password+`","newPassword":"battery staple"}`, http.StatusNoContent, c)
	a.want(http.MethodGet, "/api/v1/auth/me", "", http.StatusOK, c)
	a.want(http.MethodGet, "/api/v1/auth/me", "", http.StatusUnauthorized, other)

	out = a.want(http.MethodPost, "/api/v1/auth/email",
		`{"password":"`+m.Password+`","newEmail":"new.address@example.org"}`, http.StatusUnprocessableEntity, c)
	if !strings.Contains(string(out), `"location":"body.password"`) {
		t.Errorf("wrong password: %s, want an error at body.password", out)
	}

	// An address with a display name passes the schema but is no login name.
	out = a.want(http.MethodPost, "/api/v1/auth/email",
		`{"password":"battery staple","newEmail":"New <new.address@example.org>"}`, http.StatusUnprocessableEntity, c)
	if !strings.Contains(string(out), `"location":"body.newEmail"`) {
		t.Errorf("invalid new email: %s, want an error at body.newEmail", out)
	}

	a.want(http.MethodPost, "/api/v1/auth/email",
		`{"password":"battery staple","newEmail":"new.address@example.org"}`, http.StatusAccepted, c)

	token := a.linkToken("change-email", "confirm-email-change")
	a.want(http.MethodPost, "/api/v1/auth/email/confirm", `{"token":"`+token+`"}`, http.StatusNoContent)
	a.want(http.MethodPost, "/api/v1/auth/email/confirm", `{"token":"`+token+`"}`, http.StatusGone)

	if to, text := a.mailTo("email-changed"); to != m.Email || !strings.Contains(text, "new.address@example.org") {
		t.Errorf("notice to %q: %q", to, text)
	}

	if me := decode(t, a.want(http.MethodGet, "/api/v1/auth/me", "", http.StatusOK, c)); me["email"] != "new.address@example.org" {
		t.Errorf("me = %v, want the new address", me)
	}
}

// wantTooMany checks a 429: problem+json with Retry-After and the security
// headers.
func wantTooMany(t *testing.T, res authResponse, body []byte) {
	t.Helper()

	if res.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("status %d %s, want 429", res.StatusCode, body)
	}

	if ct := res.Header.Get("Content-Type"); ct != "application/problem+json" {
		t.Errorf("Content-Type %q", ct)
	}

	if secs, err := strconv.Atoi(res.Header.Get("Retry-After")); err != nil || secs < 1 {
		t.Errorf("Retry-After %q, want positive seconds", res.Header.Get("Retry-After"))
	}

	if res.Header.Get("Content-Security-Policy") == "" {
		t.Error("429 without security headers")
	}
}

func TestLoginRateLimitPerEmail(t *testing.T) {
	t.Parallel()

	a := newAuthApp(t, app.Deps{Demo: true, DemoLogins: seed.DemoLogins()})
	m := demoMember(t)

	for range 5 {
		a.want(http.MethodPost, "/api/v1/auth/login", loginBody(m.Email, "wrong password"), http.StatusUnauthorized)
	}

	// Even the right password waits now, for this email only; any spelling
	// of it counts.
	res, body := a.call(http.MethodPost, "/api/v1/auth/login", loginBody(" "+strings.ToUpper(m.Email), m.Password))
	wantTooMany(t, res, body)

	if got := res.Header.Get("Retry-After"); got != "900" {
		t.Errorf("Retry-After %q, want 900 (15 minutes)", got)
	}

	a.want(http.MethodPost, "/api/v1/auth/login", loginBody("nobody@example.org", "wrong password"), http.StatusUnauthorized)

	a.clock.advance(15 * time.Minute)
	a.mustLogin(m.Email, m.Password)
}

func TestLoginSuccessClearsFailures(t *testing.T) {
	t.Parallel()

	a := newAuthApp(t, app.Deps{Demo: true, DemoLogins: seed.DemoLogins()})
	m := demoMember(t)

	for range 4 {
		a.want(http.MethodPost, "/api/v1/auth/login", loginBody(m.Email, "wrong password"), http.StatusUnauthorized)
	}

	a.mustLogin(m.Email, m.Password)

	for range 4 {
		a.want(http.MethodPost, "/api/v1/auth/login", loginBody(m.Email, "wrong password"), http.StatusUnauthorized)
	}

	a.mustLogin(m.Email, m.Password)
}

// callFrom sends a request from the client remote with the given
// X-Forwarded-For header (if any).
func (a *authApp) callFrom(remote, forwardedFor, method, path, body string) (authResponse, []byte) {
	a.t.Helper()

	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.RemoteAddr = remote
	req.Header.Set("Content-Type", "application/json")

	if forwardedFor != "" {
		req.Header.Set("X-Forwarded-For", forwardedFor)
	}

	rec := httptest.NewRecorder()
	a.h.ServeHTTP(rec, req)

	return authResponse{StatusCode: rec.Code, Header: rec.Header()}, rec.Body.Bytes()
}

func TestLoginRateLimitPerClient(t *testing.T) {
	t.Parallel()

	proxy := netip.MustParsePrefix("10.0.0.0/8")
	a := newAuthApp(t, app.Deps{TrustedProxies: []netip.Prefix{proxy}})
	attempt := func(remote, xff string, i int) (authResponse, []byte) {
		return a.callFrom(remote, xff, http.MethodPost, "/api/v1/auth/login",
			loginBody("guess"+strconv.Itoa(i)+"@example.org", "wrong password"))
	}

	// A client sending its own X-Forwarded-For directly is limited by its
	// address: the header is ignored.
	for i := range 20 {
		if res, body := attempt("198.51.100.7:4000", "203.0.113."+strconv.Itoa(i), i); res.StatusCode != http.StatusUnauthorized {
			t.Fatalf("attempt %d: %d %s", i, res.StatusCode, body)
		}
	}

	res, body := attempt("198.51.100.7:4000", "203.0.113.99", 20)
	wantTooMany(t, res, body)

	// Behind the trusted proxy, each client has its own limit.
	for i := range 20 {
		if res, body := attempt("10.0.0.1:4000", "203.0.113.5", i); res.StatusCode != http.StatusUnauthorized {
			t.Fatalf("proxied attempt %d: %d %s", i, res.StatusCode, body)
		}
	}

	res, body = attempt("10.0.0.1:4000", "203.0.113.5", 20)
	wantTooMany(t, res, body)

	if res, body := attempt("10.0.0.1:4000", "203.0.113.6", 21); res.StatusCode != http.StatusUnauthorized {
		t.Errorf("another client behind the proxy: %d %s, want 401", res.StatusCode, body)
	}
}

func TestWarnsOfAnUntrustedProxy(t *testing.T) {
	t.Parallel()

	var logs strings.Builder

	a := newAuthApp(t, app.Deps{Logger: slog.New(slog.NewTextHandler(&logs, nil))})

	// Behind a proxy that nobody listed, every client shares its address.
	for i := range 2 {
		a.callFrom("127.0.0.1:4000", "203.0.113.5", http.MethodPost, "/api/v1/auth/login",
			loginBody("guess"+strconv.Itoa(i)+"@example.org", "wrong password"))
	}

	if got := strings.Count(logs.String(), "TRUSTED_PROXIES"); got != 1 {
		t.Errorf("warnings = %d, want 1:\n%s", got, logs.String())
	}
}

func TestMailRateLimit(t *testing.T) {
	t.Parallel()

	a := newAuthApp(t, app.Deps{Demo: true, DemoLogins: seed.DemoLogins()})

	// Register and reset requests share the per-client budget of mails.
	a.want(http.MethodPost, "/api/v1/auth/register", registration, http.StatusAccepted)

	for range 4 {
		a.want(http.MethodPost, "/api/v1/auth/password-reset", `{"email":"someone@example.org"}`, http.StatusAccepted)
	}

	res, body := a.call(http.MethodPost, "/api/v1/auth/password-reset", `{"email":"someone@example.org"}`)
	wantTooMany(t, res, body)

	a.clock.advance(time.Hour)
	a.want(http.MethodPost, "/api/v1/auth/password-reset", `{"email":"someone@example.org"}`, http.StatusAccepted)
}

func TestEmailChangeCountsOnlySentMails(t *testing.T) {
	t.Parallel()

	a := newAuthApp(t, app.Deps{Demo: true, DemoLogins: seed.DemoLogins()})
	m := demoMember(t)
	c := a.mustLogin(m.Email, m.Password)

	// Mistyped passwords send no mail and leave the client's budget alone.
	for range 4 {
		a.want(http.MethodPost, "/api/v1/auth/email",
			`{"password":"wrong password","newEmail":"elsewhere@example.org"}`, http.StatusUnprocessableEntity, c)
	}

	a.want(http.MethodPost, "/api/v1/auth/email",
		`{"password":"`+m.Password+`","newEmail":"elsewhere@example.org"}`, http.StatusAccepted, c)

	for range 4 {
		a.want(http.MethodPost, "/api/v1/auth/password-reset", `{"email":"someone@example.org"}`, http.StatusAccepted)
	}

	res, body := a.call(http.MethodPost, "/api/v1/auth/email",
		`{"password":"`+m.Password+`","newEmail":"elsewhere@example.org"}`, c)
	wantTooMany(t, res, body)
}

func TestChangePasswordRateLimit(t *testing.T) {
	t.Parallel()

	a := newAuthApp(t, app.Deps{Demo: true, DemoLogins: seed.DemoLogins()})
	m := demoMember(t)
	c := a.mustLogin(m.Email, m.Password)
	wrong := `{"currentPassword":"wrong password","newPassword":"battery staple"}`

	for range 5 {
		a.want(http.MethodPost, "/api/v1/auth/password", wrong, http.StatusUnprocessableEntity, c)
	}

	res, body := a.call(http.MethodPost, "/api/v1/auth/password",
		`{"currentPassword":"`+m.Password+`","newPassword":"battery staple"}`, c)
	wantTooMany(t, res, body)
}
