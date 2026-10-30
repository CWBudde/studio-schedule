package app_test

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/app"
	"github.com/cwbudde/studio-schedule/internal/domain"
)

// anonymous sends a GET without a session and fails unless it answers
// status.
func (a *authApp) anonymous(status int, path string) []byte {
	a.t.Helper()

	res, out := a.call(http.MethodGet, path, "")
	if res.StatusCode != status {
		a.t.Fatalf("GET %s: %d %s, want %d", path, res.StatusCode, out, status)
	}

	return out
}

// errorLocation is the location of the only error of a 422 problem.
func errorLocation(t *testing.T, body []byte) string {
	t.Helper()

	errs, _ := decode(t, body)["errors"].([]any)
	if len(errs) != 1 {
		t.Fatalf("want one error, got %s", body)
	}

	loc, _ := errs[0].(map[string]any)["location"].(string)

	return loc
}

func TestSettingsCards(t *testing.T) {
	t.Parallel()

	a := newAuthApp(t, app.Deps{Demo: true})
	olga := a.login("olga.ledgerby@example.net")  // office
	arno := a.login("arno.keystone@example.net")  // admin
	clara := a.login("clara.amberly@example.org") // member

	// The school profile.
	school := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/admin/settings/school", "", olga))
	if school["schoolName"] != "Studio Demo — Ballet & Dance" || school["legalNotice"] == nil {
		t.Fatalf("school %v", school)
	}

	a.expect(http.StatusForbidden, http.MethodGet, "/api/v1/admin/settings/school", "", clara)
	a.expect(http.StatusForbidden, http.MethodPut, "/api/v1/admin/settings/school", `{"schoolName":"X"}`, olga)

	school = decode(t, a.expect(http.StatusOK, http.MethodPut, "/api/v1/admin/settings/school",
		`{"schoolName":" Fictional Ballet Academy "}`, arno))
	if school["schoolName"] != "Fictional Ballet Academy" || school["legalNotice"] != nil {
		t.Fatalf("updated school %v", school)
	}

	// Branding: an accent that the light text cannot stand on is refused.
	branding := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/admin/settings/branding", "", olga))
	if branding["accentColor"] != nil || branding["hasLogo"] != false || branding["minContrast"] != 4.5 ||
		branding["theme"] != "programme" || branding["hasBackground"] != false {
		t.Fatalf("branding %v", branding)
	}

	public := decode(t, a.anonymous(http.StatusOK, "/api/v1/settings/public"))
	if public["theme"] != "programme" || public["hasBackground"] != false {
		t.Fatalf("fresh public %v", public)
	}

	bad := a.expect(http.StatusUnprocessableEntity, http.MethodPut, "/api/v1/admin/settings/branding",
		`{"accentColor":"#ff0000"}`, arno)
	if loc := errorLocation(t, bad); loc != "body.accentColor" {
		t.Errorf("light accent: %s", bad)
	}

	a.expect(http.StatusUnprocessableEntity, http.MethodPut, "/api/v1/admin/settings/branding", `{"accentColor":"red"}`, arno)

	branding = decode(t, a.expect(http.StatusOK, http.MethodPut, "/api/v1/admin/settings/branding",
		`{"accentColor":"#1D4ED8"}`, arno))
	if branding["accentColor"] != "#1d4ed8" || branding["accentColorDark"] != "#97b7f8" {
		t.Fatalf("branding %v", branding)
	}

	public = decode(t, a.anonymous(http.StatusOK, "/api/v1/settings/public"))
	if public["schoolName"] != "Fictional Ballet Academy" || public["accentColor"] != "#1d4ed8" ||
		public["accentColorDark"] != "#97b7f8" || public["legalNotice"] != nil {
		t.Fatalf("public %v", public)
	}

	// The theme: an unknown one is refused, one is public, and an update
	// without one keeps it.
	bad = a.expect(http.StatusUnprocessableEntity, http.MethodPut, "/api/v1/admin/settings/branding",
		`{"accentColor":"#1d4ed8","theme":"neon"}`, arno)
	if loc := errorLocation(t, bad); loc != "body.theme" {
		t.Errorf("unknown theme: %s", bad)
	}

	branding = decode(t, a.expect(http.StatusOK, http.MethodPut, "/api/v1/admin/settings/branding",
		`{"accentColor":"#1d4ed8","theme":"werkstatt"}`, arno))
	if branding["theme"] != "werkstatt" {
		t.Fatalf("branding with a theme %v", branding)
	}

	if public = decode(t, a.anonymous(http.StatusOK, "/api/v1/settings/public")); public["theme"] != "werkstatt" {
		t.Fatalf("public theme %v", public)
	}

	branding = decode(t, a.expect(http.StatusOK, http.MethodPut, "/api/v1/admin/settings/branding",
		`{"accentColor":"#1d4ed8"}`, arno))
	if branding["theme"] != "werkstatt" {
		t.Fatalf("branding without a theme %v", branding)
	}

	// Time and language: the demo has lessons and prices.
	regional := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/admin/settings/regional", "", olga))
	if regional["timezone"] != "Europe/Berlin" || regional["timezoneLocked"] != true || regional["currencyLocked"] != true {
		t.Fatalf("regional %v", regional)
	}

	locked := decode(t, a.expect(http.StatusConflict, http.MethodPut, "/api/v1/admin/settings/regional",
		`{"timezone":"Europe/Vienna","defaultLocale":"en","currency":"EUR"}`, arno))
	if locked["reason"] != "timezone-locked" {
		t.Errorf("new zone: %v", locked)
	}

	locked = decode(t, a.expect(http.StatusConflict, http.MethodPut, "/api/v1/admin/settings/regional",
		`{"timezone":"Europe/Berlin","defaultLocale":"en","currency":"USD"}`, arno))
	if locked["reason"] != "currency-locked" {
		t.Errorf("new currency: %v", locked)
	}

	a.expect(http.StatusUnprocessableEntity, http.MethodPut, "/api/v1/admin/settings/regional",
		`{"timezone":"Europe/Berlin","defaultLocale":"fr","currency":"EUR"}`, arno)

	regional = decode(t, a.expect(http.StatusOK, http.MethodPut, "/api/v1/admin/settings/regional",
		`{"timezone":"Europe/Berlin","defaultLocale":"de","currency":"EUR"}`, arno))
	if regional["defaultLocale"] != "de" {
		t.Fatalf("regional %v", regional)
	}

	// Booking rules, with the window.
	rules := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/admin/settings/booking-rules", "", olga))
	if rules["releaseWeekday"] != float64(0) || rules["releaseTime"] != "18:00" || rules["releaseWeeksAhead"] != float64(1) ||
		rules["cancellationCutoffMinutes"] != float64(120) || rules["waitlistInviteExpiryMinutes"] != float64(720) {
		t.Fatalf("rules %v", rules)
	}

	// Friday, October 23rd, 2026, 18:00 in Berlin: the release of Sunday the
	// 18th opened everything before Monday the 26th.
	if w, _ := rules["window"].(map[string]any); w["lastReleaseAt"] != "2026-10-18T16:00:00Z" ||
		w["releasedUntil"] != "2026-10-25T23:00:00Z" || w["nextReleaseAt"] != "2026-10-25T17:00:00Z" {
		t.Fatalf("window %v", rules["window"])
	}

	preview := decode(t, a.expect(http.StatusOK, http.MethodGet,
		"/api/v1/admin/settings/booking-rules/preview?releaseWeekday=5&releaseTime=09:00&releaseWeeksAhead=2", "", olga))
	if preview["lastReleaseAt"] != "2026-10-23T07:00:00Z" || preview["releasedUntil"] != "2026-11-06T23:00:00Z" {
		t.Fatalf("preview %v", preview)
	}

	a.expect(http.StatusUnprocessableEntity, http.MethodGet,
		"/api/v1/admin/settings/booking-rules/preview?releaseWeekday=5&releaseTime=25:00&releaseWeeksAhead=2", "", olga)

	body := `{"releaseWeekday":5,"releaseTime":"09:00","releaseWeeksAhead":8,"bookingCutoffMinutes":0,` +
		`"cancellationCutoffMinutes":1440,"waitlistInviteExpiryMinutes":360,"defaultVisibleDays":14}`

	rules = decode(t, a.expect(http.StatusOK, http.MethodPut, "/api/v1/admin/settings/booking-rules", body, arno))
	if rules["releaseWeeksAhead"] != float64(8) || rules["generatedLessons"].(float64) <= 0 {
		t.Fatalf("rules %v", rules)
	}

	// A domain rule on a duration points at the request's field.
	bad = a.expect(http.StatusUnprocessableEntity, http.MethodPut, "/api/v1/admin/settings/booking-rules",
		`{"releaseWeekday":5,"releaseTime":"09:00","releaseWeeksAhead":8,"bookingCutoffMinutes":43201,`+
			`"cancellationCutoffMinutes":0,"waitlistInviteExpiryMinutes":60,"defaultVisibleDays":14}`, arno)
	if loc := errorLocation(t, bad); loc != "body.bookingCutoffMinutes" {
		t.Errorf("cutoff: %s", bad)
	}
}

func TestSchoolLogo(t *testing.T) {
	t.Parallel()

	a := newAuthApp(t, app.Deps{Demo: true})
	arno := a.login("arno.keystone@example.net")

	a.anonymous(http.StatusNotFound, "/api/v1/settings/logo")

	png := "\x89PNG\r\n\x1a\nlogo"
	a.expect(http.StatusNoContent, http.MethodPut, "/api/v1/admin/settings/receipts/logo",
		`{"contentType":"image/png","data":"`+base64.StdEncoding.EncodeToString([]byte(png))+`"}`, arno)

	if public := decode(t, a.anonymous(http.StatusOK, "/api/v1/settings/public")); public["hasLogo"] != true {
		t.Fatalf("public %v", public)
	}

	get := func(etag string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/settings/logo", nil)
		if etag != "" {
			req.Header.Set("If-None-Match", etag)
		}

		rec := httptest.NewRecorder()
		a.h.ServeHTTP(rec, req)

		return rec
	}

	res := get("")
	etag := res.Header().Get("ETag")

	if res.Code != http.StatusOK || res.Body.String() != png || res.Header().Get("Content-Type") != "image/png" ||
		etag == "" || res.Header().Get("Cache-Control") != "public, no-cache" ||
		res.Header().Get("Content-Disposition") != "inline" { // shown in the header, not saved as api.json
		t.Fatalf("logo: %d %v", res.Code, res.Header())
	}

	if res = get(etag); res.Code != http.StatusNotModified || res.Body.Len() != 0 ||
		res.Header().Get("Content-Disposition") != "inline" {
		t.Fatalf("revalidation: %d, %d bytes, %v", res.Code, res.Body.Len(), res.Header())
	}

	if res = get(`"other"`); res.Code != http.StatusOK {
		t.Fatalf("other etag: %d", res.Code)
	}

	// A new logo, a new ETag.
	a.expect(http.StatusNoContent, http.MethodPut, "/api/v1/admin/settings/receipts/logo",
		`{"contentType":"image/png","data":"`+base64.StdEncoding.EncodeToString([]byte(png+"2"))+`"}`, arno)

	if res = get(etag); res.Code != http.StatusOK || res.Header().Get("ETag") == etag {
		t.Fatalf("after a new logo: %d %v", res.Code, res.Header())
	}
}

func TestSchoolBackground(t *testing.T) {
	t.Parallel()

	a := newAuthApp(t, app.Deps{Demo: true})
	olga := a.login("olga.ledgerby@example.net") // office
	arno := a.login("arno.keystone@example.net") // admin

	const path = "/api/v1/admin/settings/branding/background"

	upload := func(contentType string, data []byte) string {
		return `{"contentType":"` + contentType + `","data":"` + base64.StdEncoding.EncodeToString(data) + `"}`
	}

	a.anonymous(http.StatusNotFound, "/api/v1/settings/background")

	webp := []byte("RIFF\x00\x00\x00\x00WEBPVP8 background")

	// Only an admin changes it.
	a.expect(http.StatusForbidden, http.MethodPut, path, upload("image/webp", webp), olga)
	a.expect(http.StatusForbidden, http.MethodDelete, path, "", olga)

	// Invalid images, located on their field.
	for _, c := range []struct {
		name, body, location string
	}{
		{"gif", upload("image/gif", []byte("GIF89a")), "body.contentType"},
		{"empty", upload("image/webp", nil), "body.data"},
		{"not base64", `{"contentType":"image/webp","data":"%%%"}`, "body.data"},
		{"not a png", upload("image/png", []byte("not a png")), "body.data"},
		{"too large", upload("image/jpeg",
			append([]byte("\xff\xd8\xff"), make([]byte, domain.MaxBackgroundBytes-2)...)), "body.data"},
	} {
		bad := a.expect(http.StatusUnprocessableEntity, http.MethodPut, path, c.body, arno)
		if loc := errorLocation(t, bad); loc != c.location {
			t.Errorf("%s: %s, want an error on %s", c.name, bad, c.location)
		}
	}

	// The largest image passes the body limit.
	large := append([]byte("\xff\xd8\xff"), make([]byte, domain.MaxBackgroundBytes-3)...)
	a.expect(http.StatusNoContent, http.MethodPut, path, upload("image/jpeg", large), arno)

	get := func(etag string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/settings/background", nil)
		if etag != "" {
			req.Header.Set("If-None-Match", etag)
		}

		rec := httptest.NewRecorder()
		a.h.ServeHTTP(rec, req)

		return rec
	}

	if res := get(""); res.Code != http.StatusOK || res.Body.Len() != len(large) ||
		res.Header().Get("Content-Type") != "image/jpeg" {
		t.Fatalf("large background: %d, %d bytes, %v", res.Code, res.Body.Len(), res.Header())
	}

	a.expect(http.StatusNoContent, http.MethodPut, path, upload("image/webp", webp), arno)

	public := decode(t, a.anonymous(http.StatusOK, "/api/v1/settings/public"))
	if public["hasBackground"] != true {
		t.Fatalf("public %v", public)
	}

	branding := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/admin/settings/branding", "", olga))
	if branding["hasBackground"] != true {
		t.Fatalf("branding %v", branding)
	}

	res := get("")
	etag := res.Header().Get("ETag")
	want := domain.Background{ContentType: "image/webp", Data: webp}

	if res.Code != http.StatusOK || res.Body.String() != string(webp) || res.Header().Get("Content-Type") != "image/webp" ||
		etag != `"`+want.Digest()+`"` ||
		res.Header().Get("Cache-Control") != "public, no-cache" || res.Header().Get("Content-Disposition") != "inline" {
		t.Fatalf("background: %d %v", res.Code, res.Header())
	}

	if res = get(etag); res.Code != http.StatusNotModified || res.Body.Len() != 0 ||
		res.Header().Get("ETag") != etag {
		t.Fatalf("revalidation: %d, %d bytes, %v", res.Code, res.Body.Len(), res.Header())
	}

	if res = get(`"other"`); res.Code != http.StatusOK {
		t.Fatalf("other etag: %d", res.Code)
	}

	// Removing it: a 404, and no background in the public settings.
	a.expect(http.StatusNoContent, http.MethodDelete, path, "", arno)
	a.anonymous(http.StatusNotFound, "/api/v1/settings/background")

	if public = decode(t, a.anonymous(http.StatusOK, "/api/v1/settings/public")); public["hasBackground"] != false {
		t.Fatalf("public after removing %v", public)
	}

	// Removing none is fine.
	a.expect(http.StatusNoContent, http.MethodDelete, path, "", arno)
}
