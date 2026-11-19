package conformance

import (
	"bytes"
	"encoding/base64"
	"net/http"
	"net/url"
	"strings"
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
	checkBookingRules(t, b, office, admin)
}

func checkBookingRules(t *testing.T, b *backend, office, admin *http.Client) {
	t.Helper()

	const path = "/api/v1/admin/settings/booking-rules"
	type window struct {
		Now, LastReleaseAt, NextReleaseAt, ReleasedUntil, RuleReleasedUntil string
	}
	type rules struct {
		ReleaseTime, ReleasedThrough                                                       string
		ReleaseWeekday, ReleaseWeeksAhead, BookingCutoffMinutes, CancellationCutoffMinutes int
		WaitlistInviteExpiryMinutes, DefaultVisibleDays, GeneratedLessons                  int
		Window                                                                             window
	}
	want := rules{
		ReleaseTime: "18:00", ReleaseWeeksAhead: 1, CancellationCutoffMinutes: 120, WaitlistInviteExpiryMinutes: 720,
		DefaultVisibleDays: 14, Window: window{
			Now: demoNow, LastReleaseAt: "2026-10-18T16:00:00Z", NextReleaseAt: "2026-10-25T17:00:00Z",
			ReleasedUntil: "2026-10-25T23:00:00Z", RuleReleasedUntil: "2026-10-25T23:00:00Z",
		},
	}
	if got := read[rules](t, get(t, b, office, path)); got != want {
		t.Fatalf("seeded booking rules: got %+v, want %+v", got, want)
	}
	preview := request{method: http.MethodGet, path: path + "/preview", query: url.Values{
		"releaseWeekday": {"5"}, "releaseTime": {"09:00"}, "releaseWeeksAhead": {"2"}, "releasedThrough": {"2026-11-20"},
	}}
	wantPreview := window{
		Now: demoNow, LastReleaseAt: "2026-10-23T07:00:00Z", NextReleaseAt: "2026-10-30T08:00:00Z",
		ReleasedUntil: "2026-11-20T23:00:00Z", RuleReleasedUntil: "2026-11-06T23:00:00Z",
	}
	if got := read[window](t, b.call(t, office, preview, http.StatusOK)); got != wantPreview {
		t.Fatalf("release preview: got %+v, want %+v", got, wantPreview)
	}
	missing := request{method: http.MethodGet, path: preview.path}
	problem := read[struct{ Errors []struct{ Location string } }](t, b.call(t, office, missing, http.StatusUnprocessableEntity))
	locations := []string{"query.releaseWeekday", "query.releaseTime", "query.releaseWeeksAhead"}
	if len(problem.Errors) != len(locations) {
		t.Fatalf("missing preview query: got %+v, want errors at %v", problem.Errors, locations)
	}
	for i, location := range locations {
		if problem.Errors[i].Location != location {
			t.Errorf("preview error %d: got %q, want %q", i, problem.Errors[i].Location, location)
		}
	}
	preview.query.Set("releasedThrough", "2027-10-23")
	bad := b.call(t, office, preview, http.StatusUnprocessableEntity)
	if !containsJSON(bad.body, `"location":"query.releasedThrough"`) {
		t.Error("the overlong preview was not reported at query.releasedThrough")
	}
	if got := read[rules](t, get(t, b, office, path)); got != want {
		t.Errorf("booking rules changed after preview: got %+v, want %+v", got, want)
	}

	update := request{method: http.MethodPut, path: path, body: `{"releaseWeekday":5,"releaseTime":"09:00","releaseWeeksAhead":8,` +
		`"bookingCutoffMinutes":15,"cancellationCutoffMinutes":1440,"waitlistInviteExpiryMinutes":360,"defaultVisibleDays":14}`}
	b.call(t, office, update, http.StatusForbidden)
	if got := read[rules](t, b.call(t, admin, update, http.StatusOK)); got.ReleaseWeeksAhead != 8 || got.GeneratedLessons <= 0 {
		t.Fatalf("widening the booking rules did not generate lessons: %+v", got)
	}
	if got := read[rules](t, b.call(t, admin, update, http.StatusOK)); got.GeneratedLessons != 0 {
		t.Fatalf("repeating the booking rules update generated %d lessons", got.GeneratedLessons)
	}
	update.body = strings.Replace(update.body, `"releaseWeeksAhead":8`, `"releaseWeeksAhead":53`, 1)
	b.call(t, admin, update, http.StatusUnprocessableEntity)
	if got := read[rules](t, get(t, b, office, path)); got.ReleaseWeeksAhead != 8 || got.BookingCutoffMinutes != 15 || got.GeneratedLessons != 0 {
		t.Fatalf("stored booking rules: %+v", got)
	}
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

func receiptSettingsAndLogo(t *testing.T, b *backend) {
	t.Helper()

	const path = "/api/v1/admin/settings/receipts"
	const logo = path + "/logo"
	admin, office, guest := browser(t), browser(t), browser(t)
	login(t, b, admin, demoAdmin, demoPassword)
	login(t, b, office, "olga.ledgerby@example.net", demoPassword)
	b.call(t, guest, request{method: http.MethodGet, path: path}, http.StatusUnauthorized)
	b.call(t, guest, request{method: http.MethodGet, path: logo}, http.StatusUnauthorized)

	search := request{method: http.MethodPost, path: "/api/v1/admin/receipts/search", body: `{"limit":1}`}
	page := read[struct{ Items []struct{ ID string } }](t, b.call(t, office, search, http.StatusOK))
	if len(page.Items) != 1 {
		t.Fatal("expected an issued receipt in the demo seed")
	}
	detail := request{
		method: http.MethodGet, path: "/api/v1/admin/receipts/{receiptId}",
		params: map[string]string{"receiptId": page.Items[0].ID},
	}
	issued := b.call(t, office, detail, http.StatusOK)

	type settings struct {
		IssuerName, IssuerAddress, TaxNumber, TaxMode, ReceiptPrefix, LogoType, Missing string
		Complete                                                                        bool
		LogoMaxBytes, AddressRequiredAbove                                              int
	}
	update := request{
		method: http.MethodPut, path: path,
		body: `{"issuerName":" Fixture Studio ","issuerAddress":" Road 1\r\n\r\n  Example Town ",` +
			`"taxNumber":" 123 ","taxMode":"standard","receiptPrefix":" DEMO2 "}`,
	}
	b.call(t, office, update, http.StatusForbidden)
	want := settings{
		IssuerName: "Fixture Studio", IssuerAddress: "Road 1\nExample Town", TaxNumber: "123",
		TaxMode: "standard", ReceiptPrefix: "DEMO2", Complete: true, LogoMaxBytes: 524288, AddressRequiredAbove: 25000,
	}
	if got := read[settings](t, b.call(t, admin, update, http.StatusOK)); got != want {
		t.Fatalf("receipt settings after normalization: got %+v, want %+v", got, want)
	}

	invalid := update
	invalid.body = strings.Replace(invalid.body, "DEMO2", "demo2", 1)
	b.call(t, admin, invalid, http.StatusUnprocessableEntity)
	if got := read[settings](t, get(t, b, office, path)); got != want {
		t.Fatalf("receipt settings changed after an invalid prefix: got %+v, want %+v", got, want)
	}

	const encoded = "iVBORw0KGgo="
	upload := request{method: http.MethodPut, path: logo, body: `{"contentType":"image/png","data":"` + encoded + `"}`}
	b.call(t, office, upload, http.StatusForbidden)
	wrongType := upload
	wrongType.body = strings.Replace(wrongType.body, "image/png", "image/jpeg", 1)
	b.call(t, admin, wrongType, http.StatusUnprocessableEntity)
	b.call(t, admin, upload, http.StatusNoContent)

	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	asset := get(t, b, office, logo)
	if !bytes.Equal(asset.body, data) || asset.header.Get("Content-Type") != "image/png" ||
		asset.header.Get("Cache-Control") != "no-cache" || asset.header.Get("Content-Disposition") != "inline" {
		t.Fatal("receipt logo bytes or headers do not match")
	}
	want.LogoType = "image/png"
	if got := read[settings](t, get(t, b, office, path)); got != want {
		t.Fatalf("receipt settings with logo: got %+v, want %+v", got, want)
	}

	update.body = strings.Replace(update.body, "standard", "exempt", 1)
	want.TaxMode, want.Complete, want.Missing, want.AddressRequiredAbove = "exempt", false, "taxNote", 0
	if got := read[settings](t, b.call(t, admin, update, http.StatusOK)); got != want {
		t.Fatalf("incomplete exempt receipt settings: got %+v, want %+v", got, want)
	}
	upload.method, upload.body = http.MethodDelete, ""
	b.call(t, office, upload, http.StatusForbidden)
	b.call(t, admin, upload, http.StatusNoContent)
	b.call(t, office, request{method: http.MethodGet, path: logo}, http.StatusNotFound)
	if got := read[settings](t, get(t, b, office, path)); got.LogoType != "" {
		t.Fatal("the deleted logo still appears in receipt settings")
	}
	if got := b.call(t, office, detail, http.StatusOK); !bytes.Equal(issued.body, got.body) {
		t.Fatal("changing receipt settings modified an issued receipt")
	}
}
