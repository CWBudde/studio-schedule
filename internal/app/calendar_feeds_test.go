package app_test

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cwbudde/studio-schedule/internal/app"
	"github.com/cwbudde/studio-schedule/internal/httpapi"
	"github.com/cwbudde/studio-schedule/internal/store/memory"
	"github.com/cwbudde/studio-schedule/internal/webui"
)

// TestCalendarFeeds runs the calendar feeds over HTTP (ADR 0011): create,
// list without the token, fetch with the iCalendar headers, the ETag's 304,
// the download, revoke, and the 404s. The rules are tested in package
// service.
func TestCalendarFeeds(t *testing.T) {
	t.Parallel()

	a := newAuthApp(t, app.Deps{})
	clara := a.login("clara.amberly@example.org")
	alina := a.login("alina.starling@example.net")

	if res, _ := a.call(http.MethodGet, "/api/v1/my/calendar-feeds", ""); res.StatusCode != http.StatusUnauthorized {
		t.Errorf("signed out: %d", res.StatusCode)
	}

	created := decode(t, a.expect(http.StatusCreated, http.MethodPost, "/api/v1/my/calendar-feeds",
		`{"scope":"bookings"}`, clara))
	token, _ := created["token"].(string)
	feed, _ := created["feed"].(map[string]any)
	path := "/ical/" + token + ".ics"

	if token == "" || created["path"] != path || created["url"] != "https://school.example.org"+path ||
		feed["scope"] != "bookings" || feed["createdAt"] != "2026-10-23T16:00:00Z" || feed["lastUsedAt"] != nil {
		t.Fatalf("created %v", created)
	}

	// Teaching is for staff with a staff record; a location must exist.
	a.expect(http.StatusForbidden, http.MethodPost, "/api/v1/my/calendar-feeds", `{"scope":"teaching"}`, clara)
	a.expect(http.StatusCreated, http.MethodPost, "/api/v1/my/calendar-feeds", `{"scope":"teaching"}`, alina)
	a.expect(http.StatusUnprocessableEntity, http.MethodPost, "/api/v1/my/calendar-feeds",
		`{"scope":"schedule","locationId":"00000000-0000-7000-8000-00000000f001"}`, clara)
	a.expect(http.StatusUnprocessableEntity, http.MethodPost, "/api/v1/my/calendar-feeds", `{"scope":"all"}`, clara)

	// Fetching needs no sign-in.
	res, body := a.call(http.MethodGet, path, "")
	if res.StatusCode != http.StatusOK || res.Header.Get("Content-Type") != "text/calendar; charset=utf-8" ||
		res.Header.Get("Cache-Control") != "private, max-age=900" || res.Header.Get("Content-Disposition") != "inline" ||
		!strings.HasPrefix(string(body), "BEGIN:VCALENDAR\r\n") || !strings.Contains(string(body), "X-WR-TIMEZONE:Europe/Berlin") {
		t.Fatalf("feed: %d %v\n%s", res.StatusCode, res.Header, body)
	}

	etag := res.Header.Get("ETag")
	if !strings.HasPrefix(etag, `"`) || len(etag) < 10 {
		t.Fatalf("ETag %q", etag)
	}

	notModified := func(ifNoneMatch string) authResponse {
		t.Helper()

		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("If-None-Match", ifNoneMatch)

		rec := httptest.NewRecorder()
		a.h.ServeHTTP(rec, req)

		if rec.Body.Len() != 0 && rec.Code == http.StatusNotModified {
			t.Errorf("a 304 with a body: %q", rec.Body.String())
		}

		return authResponse{StatusCode: rec.Code, Header: rec.Header()}
	}

	if res := notModified(etag); res.StatusCode != http.StatusNotModified || res.Header.Get("ETag") != etag {
		t.Errorf("If-None-Match %s: %d %v", etag, res.StatusCode, res.Header)
	}

	if res := notModified(`"other", W/` + etag); res.StatusCode != http.StatusNotModified {
		t.Errorf("weak match in a list: %d", res.StatusCode)
	}

	if res := notModified(`"other"`); res.StatusCode != http.StatusOK {
		t.Errorf("other ETag: %d", res.StatusCode)
	}

	res, _ = a.call(http.MethodGet, path+"?download=true", "")
	if res.StatusCode != http.StatusOK ||
		res.Header.Get("Content-Disposition") != `attachment; filename="studio-demo-ballet-dance-bookings.ics"` {
		t.Errorf("download: %d %q", res.StatusCode, res.Header.Get("Content-Disposition"))
	}

	// The list shows the feeds, never their tokens, and when they were used.
	list := a.expect(http.StatusOK, http.MethodGet, "/api/v1/my/calendar-feeds", "", clara)
	if strings.Contains(string(list), token) || !strings.Contains(string(list), `"lastUsedAt":"2026-10-23T16:00:00Z"`) {
		t.Errorf("list %s", list)
	}

	items, _ := decode(t, list)["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("items %v", items)
	}

	id, _ := items[0].(map[string]any)["id"].(string)

	// Only the owner revokes; then the link is gone.
	a.expect(http.StatusNotFound, http.MethodDelete, "/api/v1/my/calendar-feeds/"+id, "", alina)
	a.expect(http.StatusNoContent, http.MethodDelete, "/api/v1/my/calendar-feeds/"+id, "", clara)

	for _, p := range []string{path, "/ical/unknown.ics", "/ical/" + token} {
		res, body := a.call(http.MethodGet, p, "")
		if res.StatusCode != http.StatusNotFound || res.Header.Get("Content-Type") != "application/problem+json" {
			t.Errorf("%s: %d %q %s", p, res.StatusCode, res.Header.Get("Content-Type"), body)
		}
	}
}

// TestMeTeaches tells the app whether an account may have a teaching feed.
func TestMeTeaches(t *testing.T) {
	t.Parallel()

	a := newAuthApp(t, app.Deps{})

	for email, want := range map[string]bool{
		"alina.starling@example.net": true,  // instructor with a staff record
		"olga.ledgerby@example.net":  false, // office without one
		"clara.amberly@example.org":  false, // member
	} {
		me := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/auth/me", "", a.login(email)))
		if me["teaches"] != want {
			t.Errorf("%s: teaches %v, want %v", email, me["teaches"], want)
		}
	}
}

// TestCalendarFeedDemo leaves the URL out in a demo installation: no
// calendar reaches the web demo, which downloads the path instead.
func TestCalendarFeedDemo(t *testing.T) {
	t.Parallel()

	a := newAuthApp(t, app.Deps{Demo: true})
	clara := a.login("clara.amberly@example.org")

	created := decode(t, a.expect(http.StatusCreated, http.MethodPost, "/api/v1/my/calendar-feeds",
		`{"scope":"schedule"}`, clara))
	if _, ok := created["url"]; ok || created["path"] == nil {
		t.Fatalf("created %v", created)
	}

	path, _ := created["path"].(string)
	if res, _ := a.call(http.MethodGet, path+"?download=1", ""); res.StatusCode != http.StatusOK ||
		!strings.HasPrefix(res.Header.Get("Content-Disposition"), "attachment") {
		t.Errorf("download: %d %v", res.StatusCode, res.Header)
	}
}

// TestCalendarFeedRateLimit answers too many fetches of one feed with 429,
// whatever the client; other feeds are fetched as before.
func TestCalendarFeedRateLimit(t *testing.T) {
	t.Parallel()

	a := newAuthApp(t, app.Deps{})
	clara := a.login("clara.amberly@example.org")

	path := func(scope string) string {
		t.Helper()

		created := decode(t, a.expect(http.StatusCreated, http.MethodPost, "/api/v1/my/calendar-feeds",
			`{"scope":"`+scope+`"}`, clara))
		p, _ := created["path"].(string)

		return p
	}

	busy, quiet := path("bookings"), path("schedule")

	for i := range 30 {
		if res, _ := a.call(http.MethodGet, busy, ""); res.StatusCode != http.StatusOK {
			t.Fatalf("fetch %d: %d", i, res.StatusCode)
		}
	}

	res, _ := a.call(http.MethodGet, busy, "")
	if res.StatusCode != http.StatusTooManyRequests || res.Header.Get("Retry-After") == "" {
		t.Errorf("fetch 31: %d %v", res.StatusCode, res.Header)
	}

	if res, _ := a.call(http.MethodGet, quiet, ""); res.StatusCode != http.StatusOK {
		t.Errorf("another feed: %d", res.StatusCode)
	}
}

// TestCalendarFeedClientRateLimit answers too many fetches from one client
// with 429, whatever the tokens.
func TestCalendarFeedClientRateLimit(t *testing.T) {
	t.Parallel()

	a := newAuthApp(t, app.Deps{})

	for i := range 2000 {
		if res, _ := a.call(http.MethodGet, fmt.Sprintf("/ical/unknown-%d.ics", i), ""); res.StatusCode != http.StatusNotFound {
			t.Fatalf("fetch %d: %d", i, res.StatusCode)
		}
	}

	res, _ := a.call(http.MethodGet, "/ical/unknown.ics", "")
	if res.StatusCode != http.StatusTooManyRequests || res.Header.Get("Retry-After") == "" {
		t.Errorf("fetch 2001: %d %v", res.StatusCode, res.Header)
	}
}

// TestCalendarFeedTokenRedacted keeps feed tokens out of the logs and error
// details: the token alone grants access.
func TestCalendarFeedTokenRedacted(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer

	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	h, api := app.New(app.Deps{Store: memory.New(), Logger: logger, UI: webui.Handler()})

	huma.Register(api, httpapi.Declare(huma.Operation{
		OperationID: "testFeedPanic", Method: http.MethodGet, Path: "/ical/{token}/panic",
	}, httpapi.AccessPublic), func(context.Context, *struct {
		Token string `path:"token"`
	},
	) (*struct{}, error) {
		panic("boom")
	})

	const secret = "s3cretFeedToken"

	for _, path := range []string{"/ical/" + secret + ".ics", "/ical/" + secret, "/ical/" + secret + "/panic"} {
		res, body := do(t, h, httptest.NewRequest(http.MethodGet, path, nil))
		if res.StatusCode < http.StatusBadRequest || strings.Contains(string(body), secret) {
			t.Errorf("%s: %d %s", path, res.StatusCode, body)
		}
	}

	if strings.Contains(logs.String(), secret) || !strings.Contains(logs.String(), "/ical/<redacted>.ics") ||
		!strings.Contains(logs.String(), "panic in handler") {
		t.Errorf("logs:\n%s", logs.String())
	}
}

// TestCalendarFeedNotUI keeps the feed paths from falling back to the app's
// page.
func TestCalendarFeedNotUI(t *testing.T) {
	t.Parallel()

	h, _ := newApp(t, webui.Handler())

	res, _ := do(t, h, httptest.NewRequest(http.MethodGet, "/ical/something", nil))
	if res.StatusCode != http.StatusNotFound || res.Header.Get("Content-Type") != "application/problem+json" {
		t.Errorf("status %d, Content-Type %q", res.StatusCode, res.Header.Get("Content-Type"))
	}
}
