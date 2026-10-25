package app_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cwbudde/studio-schedule/internal/app"
	"github.com/cwbudde/studio-schedule/internal/httpapi"
	"github.com/cwbudde/studio-schedule/internal/seed"
	"github.com/cwbudde/studio-schedule/internal/store/memory"
	"github.com/cwbudde/studio-schedule/internal/webui"
)

func newApp(t *testing.T, ui http.Handler) (http.Handler, huma.API) {
	t.Helper()

	st := memory.New()
	if err := seed.LoadDemo(t.Context(), st); err != nil {
		t.Fatal(err)
	}

	return app.New(app.Deps{Store: st, Version: "1.2.3-test", UI: ui})
}

// response is the part of a recorded response the tests inspect.
type response struct {
	StatusCode int
	Header     http.Header
}

func do(t *testing.T, h http.Handler, req *http.Request) (response, []byte) {
	t.Helper()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	return response{StatusCode: rec.Code, Header: rec.Header()}, rec.Body.Bytes()
}

func decode(t *testing.T, body []byte) map[string]any {
	t.Helper()

	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("invalid JSON %q: %v", body, err)
	}

	return m
}

func TestHealth(t *testing.T) {
	t.Parallel()

	h, _ := newApp(t, nil)
	res, body := do(t, h, httptest.NewRequest(http.MethodGet, "/api/v1/health", nil))

	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body %s", res.StatusCode, body)
	}

	got := decode(t, body)
	want := map[string]any{"status": "ok", "version": "1.2.3-test"}

	if len(got) != len(want) || got["status"] != want["status"] || got["version"] != want["version"] {
		t.Fatalf("body = %v, want exactly %v", got, want)
	}

	if link := res.Header.Get("Link"); link != "" {
		t.Errorf("unexpected Link header %q", link)
	}
}

func TestPublicSettings(t *testing.T) {
	t.Parallel()

	h, _ := newApp(t, nil)
	res, body := do(t, h, httptest.NewRequest(http.MethodGet, "/api/v1/settings/public", nil))

	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body %s", res.StatusCode, body)
	}

	demo := seed.DemoSettings()
	got := decode(t, body)
	want := map[string]any{
		"schoolName":    demo.SchoolName,
		"timezone":      demo.Timezone,
		"defaultLocale": demo.DefaultLocale,
		"currency":      demo.Currency,
	}

	if len(got) != len(want) {
		t.Fatalf("body = %v, want exactly %v", got, want)
	}

	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %v, want %v", k, got[k], v)
		}
	}
}

func TestPublicSettingsNotFound(t *testing.T) {
	t.Parallel()

	h, _ := app.New(app.Deps{Store: memory.New()})
	res, body := do(t, h, httptest.NewRequest(http.MethodGet, "/api/v1/settings/public", nil))

	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, body %s", res.StatusCode, body)
	}

	if ct := res.Header.Get("Content-Type"); ct != "application/problem+json" {
		t.Errorf("Content-Type = %q", ct)
	}
}

func TestOpenAPI(t *testing.T) {
	t.Parallel()

	h, _ := newApp(t, nil)
	res, body := do(t, h, httptest.NewRequest(http.MethodGet, "/api/v1/openapi.json", nil))

	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", res.StatusCode)
	}

	spec := decode(t, body)
	if spec["openapi"] != "3.1.0" {
		t.Errorf("openapi = %v", spec["openapi"])
	}

	for _, id := range []string{`"getHealth"`, `"getPublicSettings"`} {
		if !strings.Contains(string(body), id) {
			t.Errorf("spec lacks operationId %s", id)
		}
	}

	if strings.Contains(string(body), "$schema") {
		t.Error("spec must not contain $schema properties")
	}

	res, _ = do(t, h, httptest.NewRequest(http.MethodGet, "/api/v1/docs", nil))
	if res.StatusCode != http.StatusOK {
		t.Errorf("docs status = %d", res.StatusCode)
	}
}

func TestUIFallback(t *testing.T) {
	t.Parallel()

	h, _ := newApp(t, webui.Handler())

	res, body := do(t, h, httptest.NewRequest(http.MethodGet, "/schedule/week", nil))
	if res.StatusCode != http.StatusOK || !strings.Contains(string(body), "<html") {
		t.Fatalf("UI route: status %d body %q", res.StatusCode, body)
	}

	for _, ui := range []http.Handler{nil, webui.Handler()} {
		h, _ := newApp(t, ui)

		res, _ = do(t, h, httptest.NewRequest(http.MethodGet, "/api/v1/does-not-exist", nil))
		if res.StatusCode != http.StatusNotFound || res.Header.Get("Content-Type") != "application/problem+json" {
			t.Fatalf("unknown API route (ui=%v): status %d, Content-Type %q", ui != nil, res.StatusCode, res.Header.Get("Content-Type"))
		}

		res, _ = do(t, h, httptest.NewRequest(http.MethodDelete, "/api/v1/health", nil))
		if res.StatusCode != http.StatusMethodNotAllowed || res.Header.Get("Content-Type") != "application/problem+json" {
			t.Fatalf("wrong method (ui=%v): status %d, Content-Type %q", ui != nil, res.StatusCode, res.Header.Get("Content-Type"))
		}
	}
}

type echoInput struct {
	Body struct {
		Msg string `json:"msg"`
	}
}

type echoOutput struct {
	Body struct {
		Msg string `json:"msg"`
	}
}

func TestCrossOriginProtection(t *testing.T) {
	t.Parallel()

	h, api := newApp(t, nil)
	op := httpapi.Declare(huma.Operation{
		OperationID: "testEcho", Method: http.MethodPost, Path: "/api/v1/test-echo",
	}, httpapi.AccessPublic)
	huma.Register(api, op, func(_ context.Context, in *echoInput) (*echoOutput, error) {
		out := &echoOutput{}
		out.Body.Msg = in.Body.Msg

		return out, nil
	})

	newPost := func() *http.Request {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/test-echo", strings.NewReader(`{"msg":"hi"}`))
		req.Host = "localhost"
		req.Header.Set("Content-Type", "application/json")

		return req
	}

	t.Run("foreign origin rejected", func(t *testing.T) {
		t.Parallel()

		req := newPost()
		req.Header.Set("Origin", "https://evil.example")

		res, body := do(t, h, req)
		if res.StatusCode != http.StatusForbidden {
			t.Fatalf("status = %d, body %s", res.StatusCode, body)
		}

		if ct := res.Header.Get("Content-Type"); ct != "application/problem+json" {
			t.Errorf("Content-Type = %q", ct)
		}

		if decode(t, body)["status"] != float64(http.StatusForbidden) {
			t.Errorf("problem body %s", body)
		}
	})

	t.Run("cross-site fetch metadata rejected", func(t *testing.T) {
		t.Parallel()

		req := newPost()
		req.Header.Set("Sec-Fetch-Site", "cross-site")

		if res, _ := do(t, h, req); res.StatusCode != http.StatusForbidden {
			t.Fatalf("status = %d", res.StatusCode)
		}
	})

	t.Run("same origin allowed", func(t *testing.T) {
		t.Parallel()

		req := newPost()
		req.Header.Set("Origin", "http://localhost")

		if res, body := do(t, h, req); res.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, body %s", res.StatusCode, body)
		}
	})

	t.Run("no origin headers allowed", func(t *testing.T) {
		t.Parallel()

		if res, body := do(t, h, newPost()); res.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, body %s", res.StatusCode, body)
		}
	})
}

func TestRecoverer(t *testing.T) {
	t.Parallel()

	h, api := newApp(t, nil)
	op := httpapi.Declare(huma.Operation{
		OperationID: "testPanic", Method: http.MethodGet, Path: "/api/v1/test-panic",
	}, httpapi.AccessPublic)

	panicked := false
	huma.Register(api, op, func(context.Context, *struct{}) (*struct{}, error) {
		panicked = true

		panic("boom")
	})

	res, body := do(t, h, httptest.NewRequest(http.MethodGet, "/api/v1/test-panic", nil))
	if res.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, body %s", res.StatusCode, body)
	}

	if !panicked {
		t.Fatal("the handler did not run")
	}

	if strings.Contains(string(body), "boom") {
		t.Error("panic value must not leak to the client")
	}
}
