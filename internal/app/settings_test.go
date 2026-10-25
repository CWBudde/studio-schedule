package app_test

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/app"
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
	if branding["accentColor"] != nil || branding["hasLogo"] != false || branding["minContrast"] != 4.5 {
		t.Fatalf("branding %v", branding)
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

	public := decode(t, a.anonymous(http.StatusOK, "/api/v1/settings/public"))
	if public["schoolName"] != "Fictional Ballet Academy" || public["accentColor"] != "#1d4ed8" ||
		public["accentColorDark"] != "#97b7f8" || public["legalNotice"] != nil {
		t.Fatalf("public %v", public)
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
		etag == "" || res.Header().Get("Cache-Control") != "public, no-cache" {
		t.Fatalf("logo: %d %v", res.Code, res.Header())
	}

	if res = get(etag); res.Code != http.StatusNotModified || res.Body.Len() != 0 {
		t.Fatalf("revalidation: %d, %d bytes", res.Code, res.Body.Len())
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
