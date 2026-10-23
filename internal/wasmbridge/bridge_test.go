package wasmbridge_test

import (
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/app"
	"github.com/cwbudde/studio-schedule/internal/seed"
	"github.com/cwbudde/studio-schedule/internal/store/memory"
	"github.com/cwbudde/studio-schedule/internal/wasmbridge"
)

func TestNewHTTPRequest(t *testing.T) {
	t.Parallel()

	req, err := wasmbridge.NewHTTPRequest(t.Context(), wasmbridge.Request{
		Method:  "post",
		URL:     "/api/v1/things?x=1&y=a%20b",
		Headers: []wasmbridge.Header{{"Content-Type", "application/json"}, {"X-Multi", "a"}, {"x-multi", "b"}, {"Host", "evil.example"}},
		Body:    []byte(`{"a":1}`),
	})
	if err != nil {
		t.Fatal(err)
	}

	if req.Method != http.MethodPost {
		t.Errorf("Method = %q", req.Method)
	}

	if req.Host != wasmbridge.Host || req.URL.Host != wasmbridge.Host {
		t.Errorf("Host = %q / URL.Host = %q, want %q", req.Host, req.URL.Host, wasmbridge.Host)
	}

	if req.URL.Path != "/api/v1/things" || req.URL.Query().Get("y") != "a b" {
		t.Errorf("URL = %v", req.URL)
	}

	if req.RequestURI != "/api/v1/things?x=1&y=a%20b" {
		t.Errorf("RequestURI = %q", req.RequestURI)
	}

	if got := req.Header.Values("X-Multi"); !slices.Equal(got, []string{"a", "b"}) {
		t.Errorf("X-Multi = %v", got)
	}

	if req.Header.Get("Origin") != "" || req.Header.Get("Sec-Fetch-Site") != "" {
		t.Error("bridge must not invent Origin/Sec-Fetch-Site headers")
	}

	body, _ := io.ReadAll(req.Body)
	if string(body) != `{"a":1}` || req.ContentLength != 7 {
		t.Errorf("body = %q, ContentLength = %d", body, req.ContentLength)
	}
}

func TestNewHTTPRequestDefaults(t *testing.T) {
	t.Parallel()

	req, err := wasmbridge.NewHTTPRequest(t.Context(), wasmbridge.Request{URL: "http://other.example/api/v1/health"})
	if err != nil {
		t.Fatal(err)
	}

	if req.Method != http.MethodGet || req.Host != wasmbridge.Host || req.URL.Path != "/api/v1/health" {
		t.Errorf("got %s %s %s", req.Method, req.Host, req.URL)
	}

	if req.Body != http.NoBody || req.ContentLength != 0 {
		t.Errorf("nil body should map to http.NoBody")
	}
}

func TestNewHTTPRequestInvalidURL(t *testing.T) {
	t.Parallel()

	for _, u := range []string{"", "api/v1/health", "%zz"} {
		if _, err := wasmbridge.NewHTTPRequest(t.Context(), wasmbridge.Request{URL: u}); err == nil {
			t.Errorf("URL %q: want error", u)
		}
	}
}

func TestResponseWriter(t *testing.T) {
	t.Parallel()

	t.Run("explicit status and header snapshot", func(t *testing.T) {
		t.Parallel()

		w := wasmbridge.NewResponseWriter()
		w.Header().Set("Content-Type", "application/json")
		w.Header().Add("Set-Cookie", "a=1")
		w.Header().Add("Set-Cookie", "b=2")
		w.WriteHeader(http.StatusTeapot)
		w.WriteHeader(http.StatusOK) // ignored
		w.Header().Set("X-Late", "ignored")
		_, _ = w.Write([]byte("hello "))
		_, _ = io.WriteString(w, "world")
		w.Flush()

		res := w.Result()
		if res.Status != http.StatusTeapot {
			t.Errorf("Status = %d", res.Status)
		}

		want := []wasmbridge.Header{{"Content-Type", "application/json"}, {"Set-Cookie", "a=1"}, {"Set-Cookie", "b=2"}}
		if !slices.Equal(res.Headers, want) {
			t.Errorf("Headers = %v, want %v", res.Headers, want)
		}

		if string(res.Body) != "hello world" {
			t.Errorf("Body = %q", res.Body)
		}
	})

	t.Run("implicit 200 and sniffing", func(t *testing.T) {
		t.Parallel()

		w := wasmbridge.NewResponseWriter()
		w.WriteHeader(http.StatusContinue) // informational, ignored
		_, _ = w.Write([]byte("<html><body>x</body></html>"))

		res := w.Result()
		if res.Status != http.StatusOK {
			t.Errorf("Status = %d", res.Status)
		}

		if !slices.Contains(res.Headers, wasmbridge.Header{"Content-Type", "text/html; charset=utf-8"}) {
			t.Errorf("Headers = %v", res.Headers)
		}
	})

	t.Run("no write", func(t *testing.T) {
		t.Parallel()

		res := wasmbridge.NewResponseWriter().Result()
		if res.Status != http.StatusOK || len(res.Body) != 0 {
			t.Errorf("got %+v", res)
		}
	})

	t.Run("response controller", func(t *testing.T) {
		t.Parallel()

		rc := http.NewResponseController(wasmbridge.NewResponseWriter())
		if err := rc.Flush(); err != nil {
			t.Errorf("Flush: %v", err)
		}
	})
}

func newApp(t *testing.T) http.Handler {
	t.Helper()

	st := memory.New()
	if err := seed.LoadDemo(t.Context(), st); err != nil {
		t.Fatal(err)
	}

	h, _ := app.New(app.Deps{Store: st, Version: "wasm-test"})

	return h
}

func TestServeApp(t *testing.T) {
	t.Parallel()

	h := newApp(t)

	res, err := wasmbridge.Serve(t.Context(), h, wasmbridge.Request{Method: "GET", URL: "/api/v1/health"})
	if err != nil {
		t.Fatal(err)
	}

	if res.Status != http.StatusOK {
		t.Fatalf("Status = %d, body %s", res.Status, res.Body)
	}

	var body map[string]string
	if err := json.Unmarshal(res.Body, &body); err != nil {
		t.Fatal(err)
	}

	if body["status"] != "ok" || body["version"] != "wasm-test" {
		t.Errorf("body = %v", body)
	}

	res, err = wasmbridge.Serve(t.Context(), h, wasmbridge.Request{Method: "HEAD", URL: "/api/v1/health"})
	if err != nil {
		t.Fatal(err)
	}

	if len(res.Body) != 0 {
		t.Errorf("HEAD body = %q", res.Body)
	}
}

// Bridged requests carry no Origin header, so http.CrossOriginProtection in
// the app must let unsafe methods through. POST to a GET-only route proves
// the request got past the middleware: the router answers 405, not 403.
func TestServeUnsafeMethodPassesCrossOriginProtection(t *testing.T) {
	t.Parallel()

	res, err := wasmbridge.Serve(t.Context(), newApp(t), wasmbridge.Request{
		Method:  "POST",
		URL:     "/api/v1/health",
		Headers: []wasmbridge.Header{{"Content-Type", "application/json"}},
		Body:    []byte(`{}`),
	})
	if err != nil {
		t.Fatal(err)
	}

	if res.Status != http.StatusMethodNotAllowed {
		t.Fatalf("Status = %d, want 405 (not 403); body %s", res.Status, res.Body)
	}
}

func TestServePanic(t *testing.T) {
	t.Parallel()

	h := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("kaputt") })

	_, err := wasmbridge.Serve(t.Context(), h, wasmbridge.Request{URL: "/"})
	if err == nil || !strings.Contains(err.Error(), "kaputt") {
		t.Fatalf("err = %v, want panic error", err)
	}
}
