package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

// routerWith returns a router with recordRoute and the routes patterns, each
// answering 204.
func routerWith(patterns ...string) chi.Router {
	r := chi.NewRouter()
	r.Use(recordRoute)

	for _, p := range patterns {
		r.Get(p, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	}

	return r
}

// serveInfo serves a GET of path on h with a requestInfo, as accessLog does,
// and returns what the router recorded in it.
func serveInfo(t *testing.T, h http.Handler, path string) *requestInfo {
	t.Helper()

	info := &requestInfo{}
	req := httptest.NewRequest(http.MethodGet, path, nil)
	h.ServeHTTP(httptest.NewRecorder(), req.WithContext(context.WithValue(req.Context(), requestInfoKey{}, info)))

	return info
}

// TestMatchedPathWildcard: a route with a trailing wildcard logs the path
// the wildcard matched, after a redacted secret parameter.
func TestMatchedPathWildcard(t *testing.T) {
	t.Parallel()

	r := routerWith("/files/{fileToken}/*", "/open/{id}/*")

	if info := serveInfo(t, r, "/files/c3secret/report/2026.pdf"); info.route != "/files/{fileToken}/*" ||
		info.path != "/files/<redacted>/report/2026.pdf" {
		t.Errorf("secret route: %+v", info)
	}

	if info := serveInfo(t, r, "/open/7/a/b"); info.route != "/open/{id}/*" || info.path != "/open/7/a/b" {
		t.Errorf("open route: %+v", info)
	}

	// The wildcard is no parameter of the redactor: only the secret one is.
	p := newPathRedactor(r)
	if got := p.redact("/files/c3secret"); got != "/files/<redacted>" {
		t.Errorf("redact = %q", got)
	}

	if got := p.redact("/open/7"); got != "/open/7" {
		t.Errorf("redact of a route without a secret = %q", got)
	}
}

// TestRedactSuffixBeatsLongerPrefix: of the secret routes whose prefix an
// unmatched path has, one whose suffix the rest of the path ends in (in
// its last segment) wins over a longer prefix without a suffix; otherwise
// the longest prefix redacts.
func TestRedactSuffixBeatsLongerPrefix(t *testing.T) {
	t.Parallel()

	p := newPathRedactor(routerWith("/feeds/p-{token}", "/feeds/{token}.ics", "/feeds/{token}/{id}"))

	cases := map[string]string{
		// Both match; the suffix route wins although its prefix is shorter.
		"/feeds/p-x1.secret.ics": "/feeds/<redacted>.ics",
		// No suffix in the last segment: the longest prefix.
		"/feeds/p-c3secret/more": "/feeds/p-<redacted>",
		"/feeds/c3secret/x.ics":  "/feeds/<redacted>",
		"/feeds/x1.secret.ics":   "/feeds/<redacted>.ics",
		"/other/c3secret":        "/other/c3secret",
	}

	for path, want := range cases {
		if got := p.redact(path); got != want {
			t.Errorf("redact(%q) = %q, want %q", path, got, want)
		}
	}
}

// TestPathOfUnmatchedRequest: the log path of a request that matched no
// route is its URL path, redacted; of a matched one, the path the router
// recorded.
func TestPathOfUnmatchedRequest(t *testing.T) {
	t.Parallel()

	p := newPathRedactor(routerWith("/invites/{token}"))

	unmatched := httptest.NewRequest(http.MethodGet, "/invites/c3secret/accept", nil)
	if got := p.of(unmatched); got != "/invites/<redacted>" {
		t.Errorf("of(unmatched) = %q", got)
	}

	withInfo := unmatched.WithContext(context.WithValue(unmatched.Context(), requestInfoKey{}, &requestInfo{}))
	if got := p.of(withInfo); got != "/invites/<redacted>" {
		t.Errorf("of(unmatched, recorded nothing) = %q", got)
	}

	recorded := unmatched.WithContext(context.WithValue(unmatched.Context(), requestInfoKey{},
		&requestInfo{route: "/invites/{token}", path: "/invites/<redacted>"}))
	if got := p.of(recorded); got != "/invites/<redacted>" {
		t.Errorf("of(matched) = %q", got)
	}
}

// TestRecordRouteWithoutInfo: recordRoute records nothing, and does not
// fail, for a request without a requestInfo (not passed through accessLog)
// or outside a chi router. app.New always wraps its router in accessLog,
// and chi always sets the route context before its middlewares run, so
// only a router used on its own gets here.
func TestRecordRouteWithoutInfo(t *testing.T) {
	t.Parallel()

	served := 0
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		served++

		w.WriteHeader(http.StatusNoContent)
	})

	// In a router, without a requestInfo.
	r := chi.NewRouter()
	r.Use(recordRoute)
	r.Get("/rooms/{id}", next)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/rooms/7", nil))

	if rec.Code != http.StatusNoContent {
		t.Errorf("in a router: status %d", rec.Code)
	}

	// Outside a router, with a requestInfo: no route context to read.
	if info := serveInfo(t, recordRoute(next), "/rooms/7"); *info != (requestInfo{}) {
		t.Errorf("outside a router: recorded %+v", info)
	}

	if served != 2 {
		t.Errorf("the handler ran %d times, want 2", served)
	}
}
