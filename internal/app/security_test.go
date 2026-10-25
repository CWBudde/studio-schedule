package app_test

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cwbudde/studio-schedule/internal/app"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/httpapi"
	"github.com/cwbudde/studio-schedule/internal/seed"
	"github.com/cwbudde/studio-schedule/internal/store/memory"
	"github.com/cwbudde/studio-schedule/internal/webui"
)

// secretDetail is the text of an unmapped handler error; it must only be
// logged, never sent to the client.
const secretDetail = "secret internal detail"

// newSecurityApp is the demo app with the UI and two test operations: one
// that fails with an unmapped error and one that panics.
func newSecurityApp(t *testing.T, hsts bool) http.Handler {
	t.Helper()

	st := memory.New()
	if err := seed.LoadDemo(t.Context(), st); err != nil {
		t.Fatal(err)
	}

	h, api := app.New(app.Deps{Store: st, UI: webui.Handler(), HSTS: hsts})

	huma.Register(api, httpapi.Declare(huma.Operation{
		OperationID: "testFail", Method: http.MethodGet, Path: "/api/v1/test-fail",
	}, httpapi.AccessPublic), func(context.Context, *struct{}) (*struct{}, error) {
		return nil, errors.New(secretDetail)
	})
	huma.Register(api, httpapi.Declare(huma.Operation{
		OperationID: "testPanic", Method: http.MethodGet, Path: "/api/v1/test-panic",
	}, httpapi.AccessPublic), func(context.Context, *struct{}) (*struct{}, error) {
		panic(secretDetail)
	})

	return h
}

func TestSecurityHeaders(t *testing.T) {
	t.Parallel()

	always := map[string]string{
		"Content-Security-Policy": "default-src 'self'",
		"X-Content-Type-Options":  "nosniff",
		"Referrer-Policy":         "same-origin",
		"X-Frame-Options":         "DENY",
	}

	cases := []struct {
		name, method, target string
		status               int
	}{
		{"API JSON", http.MethodGet, "/api/v1/health", http.StatusOK},
		{"UI fallback", http.MethodGet, "/schedule/week", http.StatusOK},
		{"unknown API route", http.MethodGet, "/api/v1/does-not-exist", http.StatusNotFound},
		{"wrong method", http.MethodDelete, "/api/v1/health", http.StatusMethodNotAllowed},
		{"panic", http.MethodGet, "/api/v1/test-panic", http.StatusInternalServerError},
	}

	for _, hsts := range []bool{false, true} {
		h := newSecurityApp(t, hsts)

		for _, c := range cases {
			res, body := do(t, h, httptest.NewRequest(c.method, c.target, nil))
			if res.StatusCode != c.status {
				t.Fatalf("%s: status = %d, want %d, body %s", c.name, res.StatusCode, c.status, body)
			}

			for name, want := range always {
				if got := res.Header.Get(name); !strings.HasPrefix(got, want) {
					t.Errorf("%s (hsts=%v): %s = %q, want prefix %q", c.name, hsts, name, got, want)
				}
			}

			got := res.Header.Get("Strict-Transport-Security")
			if want := map[bool]string{true: "max-age=31536000"}[hsts]; got != want {
				t.Errorf("%s (hsts=%v): Strict-Transport-Security = %q, want %q", c.name, hsts, got, want)
			}
		}
	}
}

// TestContentSecurityPolicy pins the policy: only the app's own scripts, no
// framing, no plugins.
func TestContentSecurityPolicy(t *testing.T) {
	t.Parallel()

	res, _ := do(t, newSecurityApp(t, false), httptest.NewRequest(http.MethodGet, "/", nil))
	csp := res.Header.Get("Content-Security-Policy")

	directives := map[string]string{}
	for d := range strings.SplitSeq(csp, ";") {
		name, value, _ := strings.Cut(strings.TrimSpace(d), " ")
		directives[name] = value
	}

	for name, want := range map[string]string{
		"default-src":     "'self'",
		"img-src":         "'self' data:",
		"style-src":       "'self' 'unsafe-inline'", // injected <style> elements of sonner and Radix
		"frame-ancestors": "'none'",
		"base-uri":        "'self'",
		"form-action":     "'self'",
		"object-src":      "'none'",
	} {
		if directives[name] != want {
			t.Errorf("%s = %q, want %q (CSP %q)", name, directives[name], want, csp)
		}
	}

	if _, ok := directives["script-src"]; ok {
		t.Errorf("script-src must fall back to default-src 'self' (CSP %q)", csp)
	}

	if strings.Contains(csp, "unsafe-eval") {
		t.Errorf("CSP allows eval: %q", csp)
	}

	// The API docs page keeps the policy Huma sets for its CDN renderer.
	res, _ = do(t, newSecurityApp(t, false), httptest.NewRequest(http.MethodGet, "/api/v1/docs", nil))
	if docs := res.Header.Get("Content-Security-Policy"); docs == csp || !strings.Contains(docs, "https://unpkg.com/") {
		t.Errorf("docs CSP = %q", docs)
	}
}

func TestProblemJSON(t *testing.T) {
	t.Parallel()

	h := newSecurityApp(t, false)
	sched := newScheduleApp(t)
	member := sched.login(t, domain.RoleMember)

	jsonPost := func(target, body string) *http.Request {
		req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")

		return req
	}

	cases := []struct {
		name   string
		h      http.Handler
		req    func() *http.Request
		status int
	}{
		{"401 signed-in operation without session", h, func() *http.Request {
			return httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
		}, http.StatusUnauthorized},
		{"403 admin operation as member", sched.h, func() *http.Request {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/accounts", nil)
			req.AddCookie(member)

			return req
		}, http.StatusForbidden},
		{"404 unknown API route", h, func() *http.Request {
			return httptest.NewRequest(http.MethodGet, "/api/v1/does-not-exist", nil)
		}, http.StatusNotFound},
		{"405 wrong method", h, func() *http.Request {
			return httptest.NewRequest(http.MethodDelete, "/api/v1/health", nil)
		}, http.StatusMethodNotAllowed},
		{"400 malformed JSON", h, func() *http.Request {
			return jsonPost("/api/v1/auth/register", `{"email":`)
		}, http.StatusBadRequest},
		{"422 invalid body", h, func() *http.Request {
			return jsonPost("/api/v1/auth/register", `{"email":42}`)
		}, http.StatusUnprocessableEntity},
		{"500 unmapped handler error", h, func() *http.Request {
			return httptest.NewRequest(http.MethodGet, "/api/v1/test-fail", nil)
		}, http.StatusInternalServerError},
		{"500 panic", h, func() *http.Request {
			return httptest.NewRequest(http.MethodGet, "/api/v1/test-panic", nil)
		}, http.StatusInternalServerError},
		{"403 cross-origin", h, func() *http.Request {
			req := jsonPost("/api/v1/auth/register", `{}`)
			req.Host = "localhost"
			req.Header.Set("Origin", "https://evil.example")

			return req
		}, http.StatusForbidden},
	}

	for _, c := range cases {
		res, body := do(t, c.h, c.req())
		if res.StatusCode != c.status {
			t.Errorf("%s: status = %d, want %d, body %s", c.name, res.StatusCode, c.status, body)

			continue
		}

		if ct := res.Header.Get("Content-Type"); ct != "application/problem+json" {
			t.Errorf("%s: Content-Type = %q", c.name, ct)
		}

		problem := decode(t, body)
		if problem["status"] != float64(c.status) || problem["title"] != http.StatusText(c.status) {
			t.Errorf("%s: problem %s lacks status/title", c.name, body)
		}

		if detail, _ := problem["detail"].(string); detail == "" {
			t.Errorf("%s: problem %s lacks a detail", c.name, body)
		}

		if id := res.Header.Get("X-Request-Id"); id == "" || problem["requestId"] != id {
			t.Errorf("%s: problem %s lacks the request ID %q of the header", c.name, body, id)
		}

		for _, leak := range []string{secretDetail, "goroutine", ".go:", "runtime."} {
			if strings.Contains(string(body), leak) {
				t.Errorf("%s: body leaks %q: %s", c.name, leak, body)
			}
		}

		if c.status >= http.StatusInternalServerError {
			keys := sortedJSONKeys(problem)
			if !slices.Equal(keys, []string{"detail", "requestId", "status", "title"}) ||
				problem["detail"] != "internal server error" {
				t.Errorf("%s: a 500 must say nothing but %q, got %s", c.name, "internal server error", body)
			}
		}
	}
}

// TestServerErrorIsLogged checks that the masked cause of a 500 still reaches
// the log.
func TestServerErrorIsLogged(t *testing.T) {
	t.Parallel()

	var logs strings.Builder

	st := memory.New()
	h, api := app.New(app.Deps{Store: st, Logger: slog.New(slog.NewTextHandler(&logs, nil))})
	huma.Register(api, httpapi.Declare(huma.Operation{
		OperationID: "testFail", Method: http.MethodGet, Path: "/api/v1/test-fail",
	}, httpapi.AccessPublic), func(context.Context, *struct{}) (*struct{}, error) {
		return nil, errors.New(secretDetail)
	})

	if res, _ := do(t, h, httptest.NewRequest(http.MethodGet, "/api/v1/test-fail", nil)); res.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d", res.StatusCode)
	}

	if !strings.Contains(logs.String(), secretDetail) {
		t.Errorf("cause not logged:\n%s", logs.String())
	}
}
