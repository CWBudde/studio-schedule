package conformance

import (
	"net/http"
	"os"
	"strings"
	"testing"
)

func TestResponseContract(t *testing.T) {
	t.Parallel()

	spec, err := os.Open("../../api/openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = spec.Close() }()

	contract, err := readContract(spec)
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name    string
		method  string
		path    string
		status  int
		media   string
		body    string
		wantErr string
	}{
		{"health", "GET", "/api/v1/health", 200, "application/json; charset=utf-8", `{"status":"ok"}`, ""},
		{"missing field", "GET", "/api/v1/health", 200, "application/json", `{}`, "status"},
		{"wrong media", "GET", "/api/v1/health", 200, "text/html", "ok", "text/html"},
		{"unknown operation", "POST", "/api/v1/health", 200, "application/json", "{}", "POST /api/v1/health"},
		{"default error", "GET", "/api/v1/auth/me", 401, "application/problem+json", `{"status":401,"title":"Unauthorized"}`, ""},
		{"fractional status", "GET", "/api/v1/auth/me", 401, "application/problem+json", `{"status":401.00000000000000001}`, "integer"},
		{"out of range integer", "GET", "/api/v1/auth/me", 401, "application/problem+json", `{"status":9223372036854775808}`, "int64"},
		{
			"invalid UUID", "GET", "/api/v1/auth/me", 200, "application/json",
			`{"accountId":"bad-id","personId":"00000000-0000-7000-8000-000000000001","email":"example@example.test","role":"member","givenName":"Fixture","familyName":"Member","teaches":false}`, "uuid",
		},
		{"bad JSON", "GET", "/api/v1/health", 200, "application/json", `{"status":"ok"}{}`, "JSON"},
		{"empty response", "POST", "/api/v1/auth/logout", 204, "", "", ""},
		{"body with no schema", "POST", "/api/v1/auth/logout", 204, "", "unexpected", "empty"},
		{"non-JSON response", "GET", "/ical/{token}.ics", 200, "text/calendar; charset=utf-8", "BEGIN:VCALENDAR\r\nEND:VCALENDAR\r\n", ""},
		{"missing media", "GET", "/ical/{token}.ics", 200, "", "BEGIN:VCALENDAR\r\nEND:VCALENDAR\r\n", "Content-Type"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h := make(http.Header)
			if tc.media != "" {
				h.Set("Content-Type", tc.media)
			}

			err := contract.validate(tc.method, tc.path, tc.status, h, []byte(tc.body))
			if tc.wantErr == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("validation error = %v, want %q", err, tc.wantErr)
			}
		})
	}
}
