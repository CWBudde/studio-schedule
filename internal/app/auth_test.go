package app_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/app"
	"github.com/cwbudde/studio-schedule/internal/auth/password"
	"github.com/cwbudde/studio-schedule/internal/seed"
	"github.com/cwbudde/studio-schedule/internal/service"
	"github.com/cwbudde/studio-schedule/internal/store/memory"
)

type stepClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *stepClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.t
}

func (c *stepClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.t = c.t.Add(d)
}

type authApp struct {
	t     *testing.T
	h     http.Handler
	clock *stepClock
}

func newAuthApp(t *testing.T, deps app.Deps) *authApp {
	t.Helper()

	st := memory.New()
	if err := seed.LoadDemo(t.Context(), st); err != nil {
		t.Fatal(err)
	}

	hasher, err := password.NewWithParams(password.Params{Memory: 8, Time: 1, Threads: 1, SaltLen: 16, KeyLen: 32}, nil)
	if err != nil {
		t.Fatal(err)
	}

	clk := &stepClock{t: time.Date(2026, time.October, 23, 16, 0, 0, 0, time.UTC)}
	deps.Store, deps.Clock, deps.Hasher = st, clk, hasher
	deps.AppURL = "https://school.example.org/"

	if deps.PDF == nil {
		deps.PDF = fakePDF{}
	}
	h, _ := app.New(deps)

	return &authApp{t: t, h: h, clock: clk}
}

// authResponse is the part of a response the auth tests inspect.
type authResponse struct {
	StatusCode int
	Header     http.Header
}

// Cookies parses the Set-Cookie headers.
func (r authResponse) Cookies() []*http.Cookie {
	var cookies []*http.Cookie

	for _, line := range r.Header.Values("Set-Cookie") {
		if c, err := http.ParseSetCookie(line); err == nil {
			cookies = append(cookies, c)
		}
	}

	return cookies
}

// call sends a JSON request with the given cookies.
func (a *authApp) call(method, path, body string, cookies ...*http.Cookie) (authResponse, []byte) {
	a.t.Helper()

	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}

	for _, c := range cookies {
		req.AddCookie(c)
	}

	rec := httptest.NewRecorder()
	a.h.ServeHTTP(rec, req)

	return authResponse{StatusCode: rec.Code, Header: rec.Header()}, rec.Body.Bytes()
}

func sessionCookie(t *testing.T, res authResponse) *http.Cookie {
	t.Helper()

	for _, c := range res.Cookies() {
		if c.Name == "studio_session" {
			return c
		}
	}

	t.Fatalf("no session cookie in %v", res.Header["Set-Cookie"])

	return nil
}

var tokenInMail = regexp.MustCompile(`confirm\?token=([A-Za-z0-9_-]+)`)

// confirmToken reads the newest confirmation token from the mailbox.
func (a *authApp) confirmToken() string {
	a.t.Helper()

	res, body := a.call(http.MethodGet, "/api/v1/demo/mailbox", "")
	if res.StatusCode != http.StatusOK {
		a.t.Fatalf("mailbox: %d %s", res.StatusCode, body)
	}

	var mb struct {
		Mails []struct {
			To, Text, Status string
		} `json:"mails"`
	}
	if err := json.Unmarshal(body, &mb); err != nil || len(mb.Mails) == 0 {
		a.t.Fatalf("mailbox %s: %v", body, err)
	}

	if mb.Mails[0].Status != "pending" {
		a.t.Fatalf("mail status %q, want pending", mb.Mails[0].Status)
	}

	m := tokenInMail.FindStringSubmatch(mb.Mails[0].Text)
	if m == nil || !strings.Contains(mb.Mails[0].Text, "https://school.example.org/confirm?token=") {
		a.t.Fatalf("no confirmation link in %q", mb.Mails[0].Text)
	}

	return m[1]
}

const registration = `{"givenName":"Ada","familyName":"Lovelace","email":"ada@example.org","password":"correct horse","locale":"de"}`

func TestAuthFlow(t *testing.T) {
	t.Parallel()

	a := newAuthApp(t, app.Deps{Demo: true})

	res, body := a.call(http.MethodPost, "/api/v1/auth/register", registration)
	if res.StatusCode != http.StatusAccepted {
		t.Fatalf("register: %d %s", res.StatusCode, body)
	}

	res, _ = a.call(http.MethodPost, "/api/v1/auth/login", `{"email":"ada@example.org","password":"correct horse"}`)
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("login before confirming: %d, want 403", res.StatusCode)
	}

	token := a.confirmToken()

	res, body = a.call(http.MethodPost, "/api/v1/auth/confirm", `{"token":"`+token+`","password":"correct horse"}`)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("confirm: %d %s", res.StatusCode, body)
	}

	c := sessionCookie(t, res)
	if !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteLaxMode || c.Path != "/" {
		t.Errorf("cookie attributes %+v, want HttpOnly, Secure, SameSite=Lax, Path=/", c)
	}

	if want := a.clock.Now().Add(service.SessionIdleTimeout); !c.Expires.Equal(want) {
		t.Errorf("cookie expires %v, want %v", c.Expires, want)
	}

	res, _ = a.call(http.MethodPost, "/api/v1/auth/confirm", `{"token":"`+token+`","password":"correct horse"}`)
	if res.StatusCode != http.StatusGone {
		t.Fatalf("second confirm: %d, want 410", res.StatusCode)
	}

	res, body = a.call(http.MethodGet, "/api/v1/auth/me", "", c)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("me: %d %s", res.StatusCode, body)
	}

	if me := decode(t, body); me["email"] != "ada@example.org" || me["role"] != "member" || me["givenName"] != "Ada" {
		t.Fatalf("me = %v", me)
	}

	res, _ = a.call(http.MethodPost, "/api/v1/auth/login", `{"email":"ada@example.org","password":"wrong horse"}`)
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong password: %d, want 401", res.StatusCode)
	}

	res, body = a.call(http.MethodPost, "/api/v1/auth/logout", "", c)
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("logout: %d %s", res.StatusCode, body)
	}

	if cleared := sessionCookie(t, res); cleared.Value != "" || cleared.MaxAge >= 0 {
		t.Errorf("logout cookie %+v, want it cleared", cleared)
	}

	res, _ = a.call(http.MethodGet, "/api/v1/auth/me", "", c)
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("me after logout: %d, want 401", res.StatusCode)
	}

	// The stale cookie is cleared, and public endpoints still answer.
	if cleared := sessionCookie(t, res); cleared.MaxAge >= 0 {
		t.Errorf("stale cookie not cleared: %+v", cleared)
	}

	res, _ = a.call(http.MethodGet, "/api/v1/settings/public", "", c)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("public settings with a stale cookie: %d", res.StatusCode)
	}
}

func TestAuthSessionRenewal(t *testing.T) {
	t.Parallel()

	a := newAuthApp(t, app.Deps{Demo: true, InsecureCookies: true})
	a.call(http.MethodPost, "/api/v1/auth/register", registration)

	res, _ := a.call(http.MethodPost, "/api/v1/auth/confirm", `{"token":"`+a.confirmToken()+`","password":"correct horse"}`)
	c := sessionCookie(t, res)

	if c.Secure {
		t.Error("InsecureCookies: the cookie must not be Secure")
	}

	res, _ = a.call(http.MethodGet, "/api/v1/auth/me", "", c)
	if len(res.Cookies()) != 0 {
		t.Fatalf("a fresh session must not renew its cookie: %v", res.Header["Set-Cookie"])
	}

	a.clock.advance(3 * 24 * time.Hour)

	res, _ = a.call(http.MethodGet, "/api/v1/auth/me", "", c)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("me: %d", res.StatusCode)
	}

	renewed := sessionCookie(t, res)
	if renewed.Value != c.Value || !renewed.Expires.Equal(a.clock.Now().Add(service.SessionIdleTimeout)) {
		t.Fatalf("renewed cookie %+v", renewed)
	}
}

func TestDemoEndpointsOnlyInDemo(t *testing.T) {
	t.Parallel()

	a := newAuthApp(t, app.Deps{DemoLogins: seed.DemoLogins()})

	for _, path := range []string{"/api/v1/demo/mailbox", "/api/v1/demo/logins"} {
		res, _ := a.call(http.MethodGet, path, "")
		if res.StatusCode != http.StatusNotFound {
			t.Errorf("%s without Deps.Demo: %d, want 404", path, res.StatusCode)
		}
	}
}

func TestDemoLoginsSignIn(t *testing.T) {
	t.Parallel()

	a := newAuthApp(t, app.Deps{Demo: true, DemoLogins: seed.DemoLogins()})

	res, body := a.call(http.MethodGet, "/api/v1/demo/logins", "")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("demo logins: %d %s", res.StatusCode, body)
	}

	var out struct {
		Logins []struct{ Email, Password, Role string } `json:"logins"`
	}
	if err := json.Unmarshal(body, &out); err != nil || len(out.Logins) != 4 {
		t.Fatalf("demo logins %s: %v", body, err)
	}

	for _, l := range out.Logins {
		res, body := a.call(http.MethodPost, "/api/v1/auth/login",
			`{"email":"`+l.Email+`","password":"`+l.Password+`"}`)
		if res.StatusCode != http.StatusOK {
			t.Fatalf("login %s: %d %s", l.Email, res.StatusCode, body)
		}

		if role := decode(t, body)["role"]; role != l.Role {
			t.Errorf("%s: role %v, want %s", l.Email, role, l.Role)
		}
	}
}
