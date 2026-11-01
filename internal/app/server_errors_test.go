package app_test

import (
	"cmp"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/app"
	"github.com/cwbudde/studio-schedule/internal/auth/password"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/seed"
	"github.com/cwbudde/studio-schedule/internal/store/memory"
)

// faultServer is the app on a faultStore with the demo data, logging JSON
// to logs.
type faultServer struct {
	*authApp

	store *faultStore
	logs  *syncBuffer
}

// faultApp builds the app like newAuthApp, but on a faultStore and with a
// logger that writes to the returned server's logs.
func faultApp(t *testing.T, deps app.Deps) *faultServer {
	t.Helper()

	st := memory.New()
	if err := seed.LoadDemo(t.Context(), st); err != nil {
		t.Fatal(err)
	}

	hasher, err := password.NewWithParams(password.Params{Memory: 8, Time: 1, Threads: 1, SaltLen: 16, KeyLen: 32}, nil)
	if err != nil {
		t.Fatal(err)
	}

	fs := &faultStore{Store: st}
	logs := &syncBuffer{}
	clk := &stepClock{t: time.Date(2026, time.October, 23, 16, 0, 0, 0, time.UTC)}

	deps.Store, deps.Clock, deps.Hasher = fs, clk, hasher
	deps.Logger = slog.New(slog.NewJSONHandler(logs, nil))
	deps.AppURL = "https://school.example.org/"

	if deps.PDF == nil {
		deps.PDF = fakePDF{}
	}

	h, _ := app.New(deps)

	return &faultServer{authApp: &authApp{t: t, h: h, clock: clk, deps: deps}, store: fs, logs: logs}
}

// signIn logs the demo account of role in; nil for "".
func (s *faultServer) signIn(role domain.Role) *http.Cookie {
	s.t.Helper()

	if role == "" {
		return nil
	}

	return s.login(demoLogin(s.t, role).Email)
}

// faultLogLine is a line of the JSON log.
type faultLogLine struct {
	Level     string `json:"level"`
	Msg       string `json:"msg"`
	Err       string `json:"err"`
	RequestID string `json:"requestId"`
}

// lines returns the log lines.
func (s *faultServer) lines() []faultLogLine {
	s.t.Helper()

	var out []faultLogLine

	for line := range strings.Lines(s.logs.String()) {
		var l faultLogLine
		if err := json.Unmarshal([]byte(line), &l); err != nil {
			s.t.Fatalf("log line %q: %v", line, err)
		}

		out = append(out, l)
	}

	return out
}

// expectServerError sends the request and checks the answer is the
// generic 500 problem, carrying the request ID of the response, while the
// injected cause is logged at ERROR as logMsg under the same request ID
// (and once only: maskServerErrors leaves the generic problem alone).
func (s *faultServer) expectServerError(method, path, body string, cookie *http.Cookie, logMsg string) {
	s.t.Helper()

	var cookies []*http.Cookie
	if cookie != nil {
		cookies = append(cookies, cookie)
	}

	res, out := s.call(method, path, body, cookies...)
	if res.StatusCode != http.StatusInternalServerError {
		s.t.Fatalf("%s %s: %d %s, want 500", method, path, res.StatusCode, out)
	}

	if ct := res.Header.Get("Content-Type"); ct != "application/problem+json" {
		s.t.Errorf("Content-Type = %q, want application/problem+json", ct)
	}

	if strings.Contains(string(out), faultMessage) || strings.Contains(string(out), "injected") {
		s.t.Errorf("the response shows the cause: %s", out)
	}

	problem := decode(s.t, out)

	id := res.Header.Get("X-Request-Id")
	if id == "" || problem["requestId"] != id {
		s.t.Errorf("problem requestId = %v, X-Request-Id = %q; want them equal and set", problem["requestId"], id)
	}

	if problem["detail"] != "internal server error" || problem["status"] != float64(http.StatusInternalServerError) {
		s.t.Errorf("problem = %s, want the generic internal server error", out)
	}

	if _, ok := problem["errors"]; ok {
		s.t.Errorf("problem lists errors: %s", out)
	}

	var logged int

	for _, l := range s.lines() {
		switch {
		case l.Msg == "unmapped server error":
			s.t.Errorf("the generic problem was logged again as unmapped: %+v", l)
		case l.Level == slog.LevelError.String():
			logged++

			if l.Msg != logMsg || !strings.Contains(l.Err, faultMessage) || l.RequestID != id {
				s.t.Errorf("error log = %+v, want %q with the cause %q and request ID %q", l, logMsg, faultMessage, id)
			}
		}
	}

	if logged != 1 {
		s.t.Errorf("%d error log lines, want 1:\n%s", logged, s.logs.String())
	}
}

// faultCase is a request that reaches a store-failure branch of a handler
// when the repositories poison are broken.
type faultCase struct {
	name   string
	role   domain.Role // who signs in; "" for nobody
	method string
	path   string
	body   string
	// bodyOf builds the body from the data, signed in as role, before the
	// store breaks; it replaces body.
	bodyOf func(s *faultServer, cookie *http.Cookie) string
	poison []string
	skip   int    // poisoned transactions that pass first
	logMsg string // the message the cause is logged with; default "internal error"
	demo   bool   // Deps.Demo
}

// TestServerErrors breaks the store under a request to every handler and
// checks it answers the generic 500: the problem says nothing of the
// cause, which is logged at ERROR with the request ID of the response.
func TestServerErrors(t *testing.T) {
	t.Parallel()

	const settings = "Settings"

	for _, tc := range []faultCase{
		// Session middleware (auth.go): the session lookup fails.
		{
			name: "authenticate", role: domain.RoleMember, method: http.MethodGet, path: "/api/v1/auth/me",
			poison: []string{faultSessions}, logMsg: "authenticate",
		},

		// auth.go
		{
			name: "register", method: http.MethodPost, path: "/api/v1/auth/register",
			body:   `{"givenName":"Fenna","familyName":"Quist","email":"fenna.quist@example.org","password":"Pirouette-im-Nebel-7"}`,
			poison: []string{"Accounts"},
		},
		{
			name: "login then me", method: http.MethodPost, path: "/api/v1/auth/login",
			bodyOf: func(s *faultServer, _ *http.Cookie) string {
				l := demoLogin(s.t, domain.RoleMember)

				return `{"email":"` + l.Email + `","password":"` + l.Password + `"}`
			},
			poison: []string{"Persons"},
		},
		{
			name: "logout", role: domain.RoleMember, method: http.MethodPost, path: "/api/v1/auth/logout",
			poison: []string{faultSessions}, skip: 1,
		},
		{
			name: "me", role: domain.RoleMember, method: http.MethodGet, path: "/api/v1/auth/me",
			poison: []string{"Persons"},
		},
		{
			name: "locale", role: domain.RoleMember, method: http.MethodPut, path: "/api/v1/auth/locale",
			body: `{"locale":"de"}`, poison: []string{"Accounts"},
		},

		// credentials.go
		{
			name: "password reset", method: http.MethodPost, path: "/api/v1/auth/password-reset",
			body: `{"email":"nobody@example.org"}`, poison: []string{"Accounts"},
		},

		// admin.go
		{
			name: "accounts", role: domain.RoleAdmin, method: http.MethodGet, path: "/api/v1/admin/accounts",
			poison: []string{"Accounts"},
		},
		{
			name: "audit log", role: domain.RoleAdmin, method: http.MethodGet, path: "/api/v1/admin/audit",
			poison: []string{"Audit"},
		},

		// announcements.go
		{
			name: "my announcements", role: domain.RoleMember, method: http.MethodGet, path: "/api/v1/announcements",
			poison: []string{"Announcements"},
		},
		{
			name: "all announcements", role: domain.RoleOffice, method: http.MethodGet, path: "/api/v1/admin/announcements",
			poison: []string{"Announcements"},
		},
		{
			name: "announcement recipients", role: domain.RoleOffice, method: http.MethodPost,
			path: "/api/v1/admin/announcements/recipients", body: `{"audience":"everyone"}`, poison: []string{"Accounts"},
		},

		// calendar_feeds.go
		{
			name: "calendar feeds", role: domain.RoleMember, method: http.MethodGet, path: "/api/v1/my/calendar-feeds",
			poison: []string{"CalendarFeeds"},
		},

		// catalog.go, catalog_admin.go
		{name: "locations", method: http.MethodGet, path: "/api/v1/locations", poison: []string{"Locations"}},
		{name: "staff", method: http.MethodGet, path: "/api/v1/staff", poison: []string{"Staff"}},
		{name: "course types", method: http.MethodGet, path: "/api/v1/course-types", poison: []string{"CourseTypes"}},
		{name: "levels", method: http.MethodGet, path: "/api/v1/levels", poison: []string{"Levels"}},
		{
			name: "all staff", role: domain.RoleOffice, method: http.MethodGet, path: "/api/v1/admin/staff",
			poison: []string{"Staff"},
		},

		// demo.go
		{name: "demo mailbox", method: http.MethodGet, path: "/api/v1/demo/mailbox", poison: []string{"Outbox"}, demo: true},

		// duplicates.go
		{
			name: "duplicates", role: domain.RoleOffice, method: http.MethodGet, path: "/api/v1/admin/duplicates",
			poison: []string{"Persons"},
		},
		{
			name: "dismiss duplicate", role: domain.RoleOffice, method: http.MethodPost,
			path: "/api/v1/admin/duplicates/dismissals", bodyOf: faultPersonPair, poison: []string{"DuplicateDismissals"},
		},

		// family.go
		{
			name: "family", role: domain.RoleMember, method: http.MethodGet, path: "/api/v1/family",
			poison: []string{"Persons"},
		},

		// imports.go
		{
			name: "import runs", role: domain.RoleOffice, method: http.MethodGet, path: "/api/v1/admin/imports",
			poison: []string{"Imports"},
		},

		// mail_log.go
		{
			name: "mail log", role: domain.RoleAdmin, method: http.MethodPost, path: "/api/v1/admin/mails/search",
			body: `{}`, poison: []string{"Outbox"},
		},

		// my_lessons.go
		{
			name: "my lessons", role: domain.RoleMember, method: http.MethodGet, path: "/api/v1/my/lessons",
			poison: []string{"Bookings"},
		},
		{
			name: "my lesson history", role: domain.RoleMember, method: http.MethodGet, path: "/api/v1/my/lessons/history",
			poison: []string{"Bookings"},
		},

		// participants.go
		{
			name: "staff person search", role: domain.RoleInstructor, method: http.MethodPost,
			path: "/api/v1/staff/persons/search", body: `{"query":"an"}`, poison: []string{"Persons"},
		},

		// passes.go
		{
			name: "pass types", role: domain.RoleOffice, method: http.MethodGet, path: "/api/v1/admin/pass-types",
			poison: []string{"PassTypes"},
		},
		{
			name: "pass settings", role: domain.RoleOffice, method: http.MethodGet, path: "/api/v1/admin/settings/passes",
			poison: []string{settings},
		},
		{
			name: "update pass settings", role: domain.RoleAdmin, method: http.MethodPut, path: "/api/v1/admin/settings/passes",
			body: `{"redemptionMode":"attendance"}`, poison: []string{settings},
		},
		{
			name: "my passes", role: domain.RoleMember, method: http.MethodGet, path: "/api/v1/my/passes",
			poison: []string{"Passes"},
		},

		// persons.go
		{
			name: "person search", role: domain.RoleOffice, method: http.MethodPost, path: "/api/v1/admin/persons/search",
			body: `{}`, poison: []string{"Persons"},
		},

		// push.go
		{
			name: "delete push subscription", role: domain.RoleMember, method: http.MethodDelete,
			path: "/api/v1/my/push/subscriptions", body: `{"endpoint":"https://push.example.org/send/abc"}`,
			poison: []string{"PushSubscriptions"},
		},

		// receipts.go
		{
			name: "receipt search", role: domain.RoleOffice, method: http.MethodPost, path: "/api/v1/admin/receipts/search",
			body: `{}`, poison: []string{"Receipts"},
		},
		{
			name: "my receipts", role: domain.RoleMember, method: http.MethodGet, path: "/api/v1/my/receipts",
			poison: []string{"Receipts"},
		},
		{
			name: "receipt settings", role: domain.RoleOffice, method: http.MethodGet, path: "/api/v1/admin/settings/receipts",
			poison: []string{settings},
		},
		{
			name: "receipt logo", role: domain.RoleOffice, method: http.MethodGet,
			path: "/api/v1/admin/settings/receipts/logo", poison: []string{settings},
		},
		{
			name: "delete receipt logo", role: domain.RoleAdmin, method: http.MethodDelete,
			path: "/api/v1/admin/settings/receipts/logo", poison: []string{settings},
		},

		// settings.go
		{
			name: "delete background", role: domain.RoleAdmin, method: http.MethodDelete,
			path: "/api/v1/admin/settings/branding/background", poison: []string{settings},
		},
		{
			name: "school settings", role: domain.RoleOffice, method: http.MethodGet, path: "/api/v1/admin/settings/school",
			poison: []string{settings},
		},
		{
			name: "regional settings", role: domain.RoleOffice, method: http.MethodGet,
			path: "/api/v1/admin/settings/regional", poison: []string{settings},
		},
		{
			name: "booking rules", role: domain.RoleOffice, method: http.MethodGet,
			path: "/api/v1/admin/settings/booking-rules", poison: []string{settings},
		},
		{
			name: "release window preview", role: domain.RoleOffice, method: http.MethodGet,
			path:   "/api/v1/admin/settings/booking-rules/preview?releaseWeekday=1&releaseTime=18:00&releaseWeeksAhead=2",
			poison: []string{settings},
		},

		// timetable.go
		{name: "terms", method: http.MethodGet, path: "/api/v1/terms", poison: []string{"Terms"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			s := faultApp(t, app.Deps{Demo: tc.demo})
			cookie := s.signIn(tc.role)

			body := tc.body
			if tc.bodyOf != nil {
				body = tc.bodyOf(s, cookie)
			}

			s.store.poison(tc.skip, tc.poison...)
			s.expectServerError(tc.method, tc.path, body, cookie, cmp.Or(tc.logMsg, "internal error"))
		})
	}
}

// faultPersonPair is the body of dismissDuplicate: two persons found by
// the office's person search.
func faultPersonPair(s *faultServer, cookie *http.Cookie) string {
	s.t.Helper()

	var found struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	if err := json.Unmarshal(s.expect(http.StatusOK, http.MethodPost, "/api/v1/admin/persons/search", `{}`, cookie), &found); err != nil {
		s.t.Fatal(err)
	}

	if len(found.Items) < 2 {
		s.t.Fatalf("%d persons, want two", len(found.Items))
	}

	return `{"personA":"` + found.Items[0].ID + `","personB":"` + found.Items[1].ID + `"}`
}

// TestServerErrorEndPreviousSession: signing in over a session whose end
// fails still signs in. The failure is only a warning, the new session
// works, and the previous one, whose deletion was rolled back, stays valid.
func TestServerErrorEndPreviousSession(t *testing.T) {
	t.Parallel()

	s := faultApp(t, app.Deps{})
	l := demoLogin(t, domain.RoleMember)
	previous := s.login(l.Email)

	// Authenticating the previous session and creating the new one pass;
	// ending the previous one fails.
	s.store.poison(2, faultSessions)

	res, body := s.call(http.MethodPost, "/api/v1/auth/login",
		`{"email":"`+l.Email+`","password":"`+l.Password+`"}`, previous)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("login: %d %s, want 200", res.StatusCode, body)
	}

	if got := decode(t, body)["email"]; got != l.Email {
		t.Errorf("signed in as %v, want %s", got, l.Email)
	}

	current := sessionCookie(t, res)

	var warned bool

	for _, line := range s.lines() {
		switch {
		case line.Level == slog.LevelError.String():
			t.Errorf("error logged: %+v", line)
		case line.Level == slog.LevelWarn.String() && line.Msg == "end previous session":
			warned = strings.Contains(line.Err, faultMessage)
		}
	}

	if !warned {
		t.Errorf("no warning about the previous session with the cause:\n%s", s.logs.String())
	}

	s.store.poison(0)

	if current.Value == previous.Value {
		t.Fatal("the login kept the previous session")
	}

	for name, c := range map[string]*http.Cookie{"new": current, "previous": previous} {
		if got := decode(t, s.expect(http.StatusOK, http.MethodGet, "/api/v1/auth/me", "", c))["email"]; got != l.Email {
			t.Errorf("%s session: me = %v, want %s", name, got, l.Email)
		}
	}
}
