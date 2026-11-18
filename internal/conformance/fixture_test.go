package conformance

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestPHPFixtureControls(t *testing.T) {
	if !*runBackend {
		t.Skip("enable the HTTP suite with scripts/conformance-php.sh")
	}

	b := openBackend(t)
	client := browser(t)
	for _, credential := range []string{"", "Bearer wrong-token"} {
		got, status := b.send(t, client, request{
			method: http.MethodPost, path: "/api/test/reset",
			header: http.Header{"Authorization": {credential}},
		})
		if status != http.StatusUnauthorized || got.header.Get("WWW-Authenticate") != "Bearer" ||
			got.header.Get("Cache-Control") != "no-store" {
			t.Fatalf("untrusted reset: got %d with %v (%s)", status, got.header, got.body)
		}
	}

	b.fixture(t, http.MethodGet, "outbox", "", http.StatusServiceUnavailable)
	b.fixture(t, http.MethodPost, "reset", "", http.StatusNoContent)
	outbox := b.fixture(t, http.MethodGet, "outbox", "", http.StatusOK)
	mails := read[struct{ Mails []json.RawMessage }](t, outbox).Mails
	if mails == nil || len(mails) != 0 {
		t.Fatalf("empty fixture outbox must be an empty JSON array: %s", outbox.body)
	}

	validClock := `{"now":"` + demoNow + `"}`
	oversize := validClock + strings.Repeat(" ", 4097-len(validClock))
	for _, body := range []string{`{"now":"2026-02-30T10:00:00Z"}`, `{"now":"` + demoNow + `","unknown":true}`, oversize} {
		b.fixture(t, http.MethodPut, "clock", body, http.StatusBadRequest)
	}
	b.fixture(t, http.MethodPut, "clock", `{"now":"2026-10-23T18:00:00+02:00"}`, http.StatusNoContent)
	b.fixture(t, http.MethodPost, "seed", `{"anchor":"garbage","school":"programme"}`, http.StatusBadRequest)
	b.fixture(t, http.MethodPost, "seed", `{"anchor":"`+demoDate+`","school":"unknown"}`, http.StatusBadRequest)
	seed := `{"anchor":"` + demoDate + `","school":"` + demoSchool + `"}`
	b.fixture(t, http.MethodPost, "seed", seed, http.StatusNoContent)
	b.fixture(t, http.MethodPost, "seed", seed, http.StatusConflict)

	settings := read[struct{ SchoolName string }](t, get(t, b, client, "/api/v1/settings/public"))
	if settings.SchoolName != "Studio Demo — Ballet & Dance" {
		t.Fatalf("programme fixture school: %q", settings.SchoolName)
	}
	schedule := read[scheduledRange](t, b.call(t, client, request{
		method: http.MethodGet, path: "/api/v1/schedule",
		query: url.Values{"from": {"2026-10-19"}, "to": {"2026-11-09"}},
	}, http.StatusOK))
	if len(schedule.Lessons) == 0 || schedule.Now.UTC().Format("2006-01-02T15:04:05Z") != demoNow {
		t.Fatalf("frozen programme schedule: now %s, lessons %d", schedule.Now, len(schedule.Lessons))
	}

	b.fixture(t, http.MethodPost, "reset", "", http.StatusNoContent)
	b.fixture(t, http.MethodPost, "seed", `{"anchor":"2027-02-05","school":"werkstatt"}`, http.StatusNoContent)
	changed := read[struct{ SchoolName, DefaultLocale string }](t, get(t, b, client, "/api/v1/settings/public"))
	if changed.SchoolName == "" || changed.SchoolName == settings.SchoolName || changed.DefaultLocale != "de" {
		t.Fatalf("werkstatt fixture settings after reset: %+v", changed)
	}
}

func TestPHPFixtureOutbox(t *testing.T) {
	if !*runBackend {
		t.Skip("enable the HTTP suite with scripts/conformance-php.sh")
	}

	b := openBackend(t)
	outbox := b.fixture(t, http.MethodGet, "outbox", "", http.StatusOK)
	type mail struct {
		To, Subject, Text, HTML, CreatedAt string
		Template                           *string
	}
	for _, entry := range read[struct{ Mails []mail }](t, outbox).Mails {
		if entry.To != "fixture-outbox@example.test" {
			continue
		}
		if entry.Subject != "Fixture mail" || entry.Text != "Test message" || entry.HTML != "<p>Test message</p>" ||
			entry.CreatedAt != "2027-02-05T12:00:00.123456Z" || entry.Template == nil || *entry.Template != "" {
			t.Fatalf("outbox mail does not match Go: %+v", entry)
		}
		return
	}
	t.Fatal("the queued fixture mail was missing")
}
