package conformance

import (
	"bytes"
	"encoding/base64"
	"net/http"
	"testing"
)

func settingsAndBranding(t *testing.T, b *backend) {
	t.Helper()

	guest := browser(t)
	admin := browser(t)
	office := browser(t)
	login(t, b, admin, demoAdmin, demoPassword)
	login(t, b, office, "olga.ledgerby@example.net", demoPassword)
	rename := request{
		method: http.MethodPut, path: "/api/v1/admin/settings/school",
		body: `{"schoolName":" Fixture Dance School "}`,
	}
	b.call(t, office, rename, http.StatusForbidden)
	b.call(t, admin, rename, http.StatusOK)

	const edit = "/api/v1/admin/settings/branding"
	update := request{method: http.MethodPut, path: edit, body: `{"accentColor":"#ff0000"}`}
	b.call(t, admin, update, http.StatusUnprocessableEntity)
	update.body = `{"accentColor":"#1D4ED8","theme":"werkstatt"}`
	b.call(t, admin, update, http.StatusOK)
	type branding struct {
		SchoolName, Theme, AccentColor, AccentColorDark string
		HasBackground                                   bool
	}
	public := func() branding {
		t.Helper()
		return read[branding](t, get(t, b, guest, "/api/v1/settings/public"))
	}
	if view := public(); view.SchoolName != "Fixture Dance School" || view.Theme != "werkstatt" ||
		view.AccentColor != "#1d4ed8" || view.AccentColorDark != "#97b7f8" || view.HasBackground {
		t.Fatalf("unexpected branding on the public app: %+v", view)
	}

	image := request{method: http.MethodGet, path: "/api/v1/settings/background"}
	b.call(t, guest, image, http.StatusNotFound)

	const encoded = "UklGRgAAAABXRUJQVlA4IGJhY2tncm91bmQ="
	upload := request{
		method: http.MethodPut, path: edit + "/background",
		body: `{"contentType":"image/webp","data":"` + encoded + `"}`,
	}
	b.call(t, office, upload, http.StatusForbidden)
	b.call(t, admin, upload, http.StatusNoContent)
	if !public().HasBackground {
		t.Error("the public settings did not expose the uploaded background")
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	asset := b.call(t, guest, image, http.StatusOK)
	etag := asset.header.Get("ETag")
	if etag == "" || !bytes.Equal(asset.body, data) || asset.header.Get("Content-Type") != "image/webp" {
		t.Fatal("background bytes, content type or revision are incorrect")
	}
	image.header = http.Header{"If-None-Match": {etag}}
	b.call(t, guest, image, http.StatusNotModified)

	upload.method, upload.body = http.MethodDelete, ""
	b.call(t, admin, upload, http.StatusNoContent)
	b.call(t, guest, image, http.StatusNotFound)
	if public().HasBackground {
		t.Error("removed background is still visible in settings")
	}

	checkRegionalSettings(t, b, guest, office, admin)
}

func checkRegionalSettings(t *testing.T, b *backend, guest, office, admin *http.Client) {
	t.Helper()

	const path = "/api/v1/admin/settings/regional"
	type regional struct {
		Timezone, DefaultLocale, Currency string
		TimezoneLocked, CurrencyLocked    bool
	}
	want := regional{Timezone: "Europe/Berlin", DefaultLocale: "en", Currency: "EUR", TimezoneLocked: true, CurrencyLocked: true}
	if got := read[regional](t, get(t, b, office, path)); got != want {
		t.Fatalf("seeded regional settings: got %+v, want %+v", got, want)
	}
	update := request{method: http.MethodPut, path: path, body: `{"timezone":"Europe/Berlin","defaultLocale":"de","currency":"EUR"}`}
	b.call(t, office, update, http.StatusForbidden)
	timezone := request{method: http.MethodPut, path: path, body: `{"timezone":"Europe/Vienna","defaultLocale":"de","currency":"EUR"}`}
	expectConflict(t, b, admin, timezone, "timezone-locked")
	currency := request{method: http.MethodPut, path: path, body: `{"timezone":"Europe/Berlin","defaultLocale":"de","currency":"USD"}`}
	expectConflict(t, b, admin, currency, "currency-locked")
	if got := read[regional](t, get(t, b, office, path)); got != want {
		t.Fatalf("regional settings after conflicts: got %+v, want %+v", got, want)
	}
	want.DefaultLocale = "de"
	if got := read[regional](t, b.call(t, admin, update, http.StatusOK)); got != want {
		t.Fatalf("updated regional settings: got %+v, want %+v", got, want)
	}
	public := read[struct{ DefaultLocale string }](t, get(t, b, guest, "/api/v1/settings/public"))
	if public.DefaultLocale != want.DefaultLocale {
		t.Errorf("public locale %q, want %q", public.DefaultLocale, want.DefaultLocale)
	}
}
