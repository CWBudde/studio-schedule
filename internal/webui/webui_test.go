package webui_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/cwbudde/studio-schedule/internal/webui"
)

func TestHandler(t *testing.T) {
	t.Parallel()

	files := fstest.MapFS{
		"index.html":       {Data: []byte("<html>index</html>")},
		"assets/app-1.js":  {Data: []byte("console.log(1)")},
		"favicon.svg":      {Data: []byte("<svg/>")},
		"assets/nested/xx": {Data: []byte("x")},
	}
	h := webui.NewHandler(files)

	tests := []struct {
		method, path string
		status       int
		bodyContains string
		cache        string
	}{
		{http.MethodGet, "/", http.StatusOK, "index", "no-cache"},
		{http.MethodGet, "/index.html", http.StatusOK, "index", "no-cache"},
		{http.MethodGet, "/schedule/week/3", http.StatusOK, "index", "no-cache"},
		{http.MethodGet, "/assets/app-1.js", http.StatusOK, "console.log", "immutable"},
		{http.MethodGet, "/favicon.svg", http.StatusOK, "<svg/>", ""},
		{http.MethodGet, "/assets/missing.js", http.StatusNotFound, "", ""},
		{http.MethodGet, "/api/v1/unknown", http.StatusNotFound, "", ""},
		{http.MethodGet, "/assets", http.StatusOK, "index", "no-cache"},
		{http.MethodPost, "/", http.StatusMethodNotAllowed, "", ""},
	}

	for _, tc := range tests {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			t.Parallel()

			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, nil))

			res := rec.Result()
			body, _ := io.ReadAll(res.Body)

			if res.StatusCode != tc.status {
				t.Fatalf("status = %d, want %d", res.StatusCode, tc.status)
			}

			if !strings.Contains(string(body), tc.bodyContains) {
				t.Errorf("body %q does not contain %q", body, tc.bodyContains)
			}

			if tc.cache != "" && !strings.Contains(res.Header.Get("Cache-Control"), tc.cache) {
				t.Errorf("Cache-Control = %q, want %q", res.Header.Get("Cache-Control"), tc.cache)
			}
		})
	}
}

func TestEmbeddedPlaceholder(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	webui.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "<html") {
		t.Fatalf("embedded index: status %d, body %q", rec.Code, rec.Body.String())
	}
}
