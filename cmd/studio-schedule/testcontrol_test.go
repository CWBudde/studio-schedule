package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/app"
	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/config"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/seed"
	"github.com/cwbudde/studio-schedule/internal/store"
	"github.com/cwbudde/studio-schedule/internal/store/memory"
)

const testControlSecret = "test-control-credential-with-32-characters"

func TestServeRefusesTestControlOverHTTPS(t *testing.T) {
	t.Parallel()

	out, err := serveRefused(t, map[string]string{
		"TEST_CONTROL":       "true",
		"TEST_CONTROL_TOKEN": testControlSecret,
		"BASE_URL":           "https://school.example.test/schedule",
	})
	if err == nil || !strings.Contains(err.Error(), "TEST_CONTROL") || !strings.Contains(err.Error(), "http://") {
		t.Errorf("serve error = %v, want refusal to enable test control with an HTTPS BASE_URL", err)
	}
	if strings.Contains(out, testControlSecret) || err != nil && strings.Contains(err.Error(), testControlSecret) {
		t.Error("serve disclosed the test control credential")
	}
}

func TestServeTestControl(t *testing.T) {
	t.Parallel()

	const prefix = "/dance/schedule"
	databaseURL := demoDatabase(t)
	addr := serveFreeAddr(t)
	base := "http://" + addr + prefix
	p := serveStart(t, map[string]string{
		"DATABASE_URL":       databaseURL,
		"HTTP_ADDR":          addr,
		"BASE_URL":           base,
		"TEST_CONTROL":       "true",
		"TEST_CONTROL_TOKEN": testControlSecret,
	})
	p.poll(t, serveStartTimeout, "prefixed readiness", func() bool {
		resp, err := serveClient.Get(base + "/readyz")
		if err != nil {
			return false
		}

		_ = resp.Body.Close()

		return resp.StatusCode == http.StatusOK
	})

	request := func(method, path, body, token string, want int) []byte {
		t.Helper()

		req, err := http.NewRequestWithContext(t.Context(), method, base+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}

		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}

		resp, err := serveClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()

		data, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != want {
			t.Fatalf("%s %s: got %d (%s), want %d", method, path, resp.StatusCode, data, want)
		}

		return data
	}

	request(http.MethodPost, "/api/test/reset", "", "", http.StatusUnauthorized)
	request(http.MethodPost, "/api/test/reset", "", "wrong-token", http.StatusUnauthorized)
	request(http.MethodGet, "/api/v1/settings/public", "", "", http.StatusNotFound)

	clock := `{"now":"2026-10-23T16:00:00Z"}`
	request(http.MethodPut, "/api/test/clock", clock, testControlSecret, http.StatusNoContent)
	request(http.MethodPut, "/api/test/clock", `{"now":"bad"}`, testControlSecret, http.StatusBadRequest)
	fixtures := `{"school":"werkstatt","anchor":"2026-10-23"}`
	request(http.MethodPost, "/api/test/seed", fixtures, testControlSecret, http.StatusNoContent)
	request(http.MethodPost, "/api/test/seed", fixtures, testControlSecret, http.StatusConflict)
	request(http.MethodGet, "/api/v1/settings/public", "", "", http.StatusOK)

	const newAccount = `{"email":"conformance@example.test","givenName":"Case","familyName":"Example","password":"conformance-test-password","locale":"en"}`
	request(http.MethodPost, "/api/v1/auth/register", newAccount, "", http.StatusAccepted)
	data := request(http.MethodGet, "/api/test/outbox", "", testControlSecret, http.StatusOK)

	var outbox struct {
		Mails []struct {
			To        string
			Text      string
			CreatedAt time.Time
		}
	}
	if err := json.Unmarshal(data, &outbox); err != nil {
		t.Fatal(err)
	}
	if len(outbox.Mails) != 1 || outbox.Mails[0].To != "conformance@example.test" ||
		outbox.Mails[0].CreatedAt.Format(time.RFC3339) != "2026-10-23T16:00:00Z" ||
		!strings.Contains(outbox.Mails[0].Text, base+"/confirm?token=") {
		t.Fatalf("test mail was not recorded with the test clock and mounted link: %s", data)
	}

	doc := request(http.MethodGet, "/api/v1/openapi.json", "", "", http.StatusOK)
	if strings.Contains(string(doc), "/api/test/") {
		t.Error("the generated API contract contains a fixture control operation")
	}

	request(http.MethodPost, "/api/test/reset", "", testControlSecret, http.StatusNoContent)
	request(http.MethodGet, "/api/v1/settings/public", "", "", http.StatusNotFound)
	if data := request(http.MethodGet, "/api/test/outbox", "", testControlSecret, http.StatusOK); !strings.Contains(string(data), `"mails":[]`) {
		t.Fatalf("outbox after reset: %s", data)
	}
	request(http.MethodPost, "/api/test/seed", fixtures, testControlSecret, http.StatusNoContent)

	ordinary := openTestStore(t, databaseURL)
	if err := ordinary.InTx(t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		settings, err := tx.Settings().Get(ctx)
		if err == nil && settings.SchoolName != seed.DemoSettings().SchoolName {
			t.Errorf("the fixture reset changed the regular school: %s", settings.SchoolName)
		}

		_, accountErr := tx.Accounts().GetByEmail(ctx, "conformance@example.test")
		if !errors.Is(accountErr, domain.ErrNotFound) {
			t.Errorf("test account leaked into the regular schema: %v", accountErr)
		}

		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func TestTestControlConfiguration(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		baseURL, database, addr, credential, want string
	}{
		{"", "test", "127.0.0.1:8093", testControlSecret, "BASE_URL"},
		{"http://localhost", "", "127.0.0.1:8093", testControlSecret, "DATABASE_URL"},
		{"http://localhost", "test", ":8093", testControlSecret, "HTTP_ADDR"},
		{"http://localhost", "test", "localhost:8093", "", "TEST_CONTROL_TOKEN"},
		{"http://localhost", "test", "[::1]:8093", testControlSecret, ""},
	} {
		err := checkTestControl(config.Config{
			TestControl: true, BaseURL: tc.baseURL, DatabaseURL: tc.database,
			HTTPAddr: tc.addr, TestControlToken: tc.credential,
		})
		switch {
		case tc.want == "" && err != nil:
			t.Error(err)
		case tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)):
			t.Errorf("configuration error %v, want %s", err, tc.want)
		}
	}
}

func TestTestControlCanRetryFailedReset(t *testing.T) {
	t.Parallel()

	fail := true
	handler := newTestControl(app.Deps{
		Store: memory.New(), Clock: clock.NewFixed(time.Date(2026, 10, 23, 16, 0, 0, 0, time.UTC)),
		Logger: slog.New(slog.DiscardHandler),
	}, testControlSecret, func(context.Context) error {
		if fail {
			return errors.New("fixture migration unavailable")
		}

		return nil
	})

	for _, tc := range []struct {
		method, path string
		want         int
	}{
		{http.MethodGet, "/readyz", http.StatusOK},
		{http.MethodPost, "/api/test/reset", http.StatusInternalServerError},
		{http.MethodGet, "/readyz", http.StatusServiceUnavailable},
		{http.MethodGet, "/api/test/outbox", http.StatusServiceUnavailable},
		{http.MethodPost, "/api/test/reset", http.StatusNoContent},
		{http.MethodGet, "/readyz", http.StatusOK},
	} {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		req.Header.Set("Authorization", "Bearer "+testControlSecret)
		out := httptest.NewRecorder()
		handler.ServeHTTP(out, req)
		if out.Code != tc.want {
			t.Fatalf("%s %s: got %d, want %d", tc.method, tc.path, out.Code, tc.want)
		}
		if tc.want == http.StatusInternalServerError {
			fail = false
		}
	}
}
