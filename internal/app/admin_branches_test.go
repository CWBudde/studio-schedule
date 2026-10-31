package app_test

import (
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/cwbudde/studio-schedule/internal/app"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
)

// conflictReasonOf is the reason of a 409 problem.
func conflictReasonOf(t *testing.T, body []byte) string {
	t.Helper()

	r, _ := decode(t, body)["reason"].(string)

	return r
}

// TestAnnouncementAudiences publishes announcements for a class and for
// a lesson: the answer names the class (with its course type and term) or
// the lesson. An edit changes the fields until the announcement expires,
// then it is a 409 expired; a delete answers 204 and the list loses it.
func TestAnnouncementAudiences(t *testing.T) {
	t.Parallel()

	a := newAuthApp(t, app.Deps{})
	office := a.login(demoLogin(t, domain.RoleOffice).Email)

	class := firstDemoClass(t, a, office)
	term, _ := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/terms", "", office))["items"].([]any)[0].(map[string]any)

	created := decode(t, a.expect(http.StatusCreated, http.MethodPost, "/api/v1/admin/announcements",
		`{"audience":"class","classId":"`+class["id"].(string)+`","title":"Class note","body":"Bring water.",`+
			`"expiresAt":"2026-10-23T18:00:00Z"}`, office))
	id, _ := created["id"].(string)

	c, _ := created["class"].(map[string]any)
	ct, _ := c["courseType"].(map[string]any)

	if created["audience"] != "class" || created["classId"] != class["id"] || created["lesson"] != nil ||
		created["courseType"] != nil || c["id"] != class["id"] || c["weekday"] != class["weekday"] ||
		c["startTime"] != class["startTime"] || c["termId"] != term["id"] || c["termName"] != term["name"] ||
		ct["id"] != class["courseTypeId"] || ct["name"] == "" {
		t.Fatalf("class announcement %v", created)
	}

	lesson := createWorkshop(t, a, office, 4)
	forLesson := decode(t, a.expect(http.StatusCreated, http.MethodPost, "/api/v1/admin/announcements",
		`{"audience":"lesson","lessonId":"`+lesson["id"].(string)+`","title":"Workshop note","body":"Room changed."}`, office))

	l, _ := forLesson["lesson"].(map[string]any)
	lct, _ := l["courseType"].(map[string]any)

	if forLesson["audience"] != "lesson" || forLesson["lessonId"] != lesson["id"] || forLesson["class"] != nil ||
		l["id"] != lesson["id"] || l["startsAt"] != "2026-11-03T09:00:00Z" || l["endsAt"] != "2026-11-03T10:00:00Z" ||
		lct["id"] != class["courseTypeId"] || lct["name"] != ct["name"] {
		t.Fatalf("lesson announcement %v", forLesson)
	}

	// An edit a minute later: the new fields, the class still named.
	a.clock.advance(time.Minute)

	updated := decode(t, a.expect(http.StatusOK, http.MethodPut, "/api/v1/admin/announcements/"+id,
		`{"audience":"class","classId":"`+class["id"].(string)+`","title":"Class note, updated","body":"Bring water and a towel.",`+
			`"expiresAt":"2026-10-23T17:00:00Z"}`, office))
	if updated["id"] != id || updated["title"] != "Class note, updated" || updated["body"] != "Bring water and a towel." ||
		updated["expiresAt"] != "2026-10-23T17:00:00Z" || updated["createdAt"] != created["createdAt"] ||
		updated["updatedAt"] != "2026-10-23T16:01:00Z" || updated["class"].(map[string]any)["id"] != class["id"] {
		t.Errorf("updated %v", updated)
	}

	// Once it has expired, it stays as it is.
	a.clock.advance(time.Hour)

	if r := conflictReasonOf(t, a.expect(http.StatusConflict, http.MethodPut, "/api/v1/admin/announcements/"+id,
		`{"audience":"everyone","title":"Too late","body":"x"}`, office)); r != "expired" {
		t.Errorf("edit an expired announcement: %s, want expired", r)
	}

	// Deleting works, expired or not.
	a.expect(http.StatusNoContent, http.MethodDelete, "/api/v1/admin/announcements/"+id, "", office)
	a.expect(http.StatusNoContent, http.MethodDelete, "/api/v1/admin/announcements/"+forLesson["id"].(string), "", office)
	a.expect(http.StatusNotFound, http.MethodDelete, "/api/v1/admin/announcements/"+id, "", office)

	for _, it := range decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/admin/announcements", "", office))["items"].([]any) {
		if got := it.(map[string]any)["id"]; got == id || got == forLesson["id"] {
			t.Errorf("deleted announcement %v still listed", got)
		}
	}
}

// TestPassTypeWrites updates and deletes pass types and sells a pass for
// a price other than its type's: the pass and its receipt carry the price
// paid.
func TestPassTypeWrites(t *testing.T) {
	t.Parallel()

	a := newAuthApp(t, app.Deps{})
	olga := a.login("olga.ledgerby@example.net")

	created := decode(t, a.expect(http.StatusCreated, http.MethodPost, "/api/v1/admin/pass-types",
		`{"name":"B2 trial pass","kind":"visits","visits":3,"validityDays":30,"price":6000,"vatRate":0,"active":true}`, olga))
	id, _ := created["id"].(string)

	updated := decode(t, a.expect(http.StatusOK, http.MethodPut, "/api/v1/admin/pass-types/"+id,
		`{"name":"B2 starter pass","kind":"visits","visits":4,"validityDays":45,"price":7000,"vatRate":0,"active":true}`, olga))
	if updated["id"] != id || updated["name"] != "B2 starter pass" || updated["visits"] != 4.0 ||
		updated["validityDays"] != 45.0 || updated["price"] != 7000.0 || updated["createdAt"] != created["createdAt"] {
		t.Errorf("updated %v", updated)
	}

	// Sold for less than the type's price.
	finn := unmarshal[struct{ Items []struct{ ID string } }](t,
		a.expect(http.StatusOK, http.MethodPost, "/api/v1/admin/persons/search", searchBody("finn amberly"), olga)).Items[0].ID

	sold := decode(t, a.expect(http.StatusCreated, http.MethodPost, "/api/v1/admin/persons/"+finn+"/passes/sell",
		`{"passTypeId":"`+id+`","price":4500}`, olga))
	if sold["pricePaid"] != 4500.0 || sold["visitsTotal"] != 4.0 || sold["origin"] != "sale" || sold["receiptId"] == nil {
		t.Fatalf("sold %v", sold)
	}

	receipt := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/admin/receipts/"+sold["receiptId"].(string), "", olga))
	lines, _ := receipt["lines"].([]any)

	if receipt["gross"] != 4500.0 || receipt["passId"] != sold["id"] || len(lines) != 1 ||
		lines[0].(map[string]any)["unitPrice"] != 4500.0 || lines[0].(map[string]any)["gross"] != 4500.0 {
		t.Errorf("receipt %v", receipt)
	}

	// The type with a sold pass stays; an unused one goes.
	if r := conflictReasonOf(t, a.expect(http.StatusConflict, http.MethodDelete, "/api/v1/admin/pass-types/"+id, "", olga)); r != "in-use" {
		t.Errorf("delete a sold type: %s, want in-use", r)
	}

	unused := decode(t, a.expect(http.StatusCreated, http.MethodPost, "/api/v1/admin/pass-types",
		`{"name":"B2 unused pass","kind":"time","validityDays":7,"price":1500,"vatRate":0,"active":true}`, olga))
	a.expect(http.StatusNoContent, http.MethodDelete, "/api/v1/admin/pass-types/"+unused["id"].(string), "", olga)
	a.expect(http.StatusNotFound, http.MethodDelete, "/api/v1/admin/pass-types/"+unused["id"].(string), "", olga)

	for _, it := range decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/admin/pass-types", "", olga))["items"].([]any) {
		if it.(map[string]any)["id"] == unused["id"] {
			t.Errorf("deleted type still listed")
		}
	}
}

// TestEmptyReceiptLogo: a logo without data is a 422 on body.data, and
// no logo is stored.
func TestEmptyReceiptLogo(t *testing.T) {
	t.Parallel()

	a := newAuthApp(t, app.Deps{})
	arno := a.login("arno.keystone@example.net")

	body := a.expect(http.StatusUnprocessableEntity, http.MethodPut, "/api/v1/admin/settings/receipts/logo",
		`{"contentType":"image/png","data":""}`, arno)
	if loc := errorLocation(t, body); loc != "body.data" || !strings.Contains(string(body), "the logo is empty") {
		t.Errorf("empty logo: %s", body)
	}

	a.expect(http.StatusNotFound, http.MethodGet, "/api/v1/admin/settings/receipts/logo", "", arno)
}

// TestReleasedThroughErrors: a released-through date more than 52 weeks
// ahead is a domain field error, located in the body of the update and in
// the query of the preview.
func TestReleasedThroughErrors(t *testing.T) {
	t.Parallel()

	a := newAuthApp(t, app.Deps{})
	arno := a.login("arno.keystone@example.net")

	before := a.expect(http.StatusOK, http.MethodGet, "/api/v1/admin/settings/booking-rules", "", arno)

	body := a.expect(http.StatusUnprocessableEntity, http.MethodPut, "/api/v1/admin/settings/booking-rules",
		`{"releaseWeekday":0,"releaseTime":"18:00","releaseWeeksAhead":1,"bookingCutoffMinutes":0,`+
			`"cancellationCutoffMinutes":120,"waitlistInviteExpiryMinutes":720,"defaultVisibleDays":14,`+
			`"releasedThrough":"2028-01-01"}`, arno)
	if loc := errorLocation(t, body); loc != "body.releasedThrough" ||
		!strings.Contains(string(body), "released-through date must be at most 52 weeks after today") {
		t.Errorf("update: %s", body)
	}

	if after := a.expect(http.StatusOK, http.MethodGet, "/api/v1/admin/settings/booking-rules", "", arno); string(after) != string(before) {
		t.Errorf("rules changed by a refused update: %s, was %s", after, before)
	}

	body = a.expect(http.StatusUnprocessableEntity, http.MethodGet,
		"/api/v1/admin/settings/booking-rules/preview?releaseWeekday=0&releaseTime=18:00&releaseWeeksAhead=1"+
			"&releasedThrough=2028-01-01", "", arno)
	if loc := errorLocation(t, body); loc != "query.releasedThrough" ||
		!strings.Contains(string(body), "released-through date must be at most 52 weeks after today") {
		t.Errorf("preview: %s", body)
	}

	// 52 weeks ahead is still fine.
	preview := decode(t, a.expect(http.StatusOK, http.MethodGet,
		"/api/v1/admin/settings/booking-rules/preview?releaseWeekday=0&releaseTime=18:00&releaseWeeksAhead=1"+
			"&releasedThrough=2027-10-22", "", arno))
	if preview["releasedUntil"] != "2027-10-22T22:00:00Z" {
		t.Errorf("preview %v", preview)
	}
}

// repeatingIDs is an ids.Source that, once stuck, returns the same UUID
// again and again, so that the next entity created twice collides.
type repeatingIDs struct {
	mu    sync.Mutex
	stuck bool
	id    uuid.UUID
}

func (s *repeatingIDs) NewUUID() uuid.UUID {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.stuck {
		return ids.V7{}.NewUUID()
	}

	return s.id
}

func (s *repeatingIDs) stick() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.stuck, s.id = true, ids.V7{}.NewUUID()
}

// TestWiderWindowGenerationFails: when the lessons of a wider release
// window cannot be generated at once (here: the ID source repeats itself,
// so the second new lesson collides), the rules are stored anyway, the
// answer reports no generated lessons, and the failure is logged as a
// warning for the hourly job to make up.
func TestWiderWindowGenerationFails(t *testing.T) {
	t.Parallel()

	var logs syncBuffer

	src := &repeatingIDs{}
	a := newAuthApp(t, app.Deps{IDs: src, Logger: slog.New(slog.NewTextHandler(&logs, nil))})
	arno := a.login("arno.keystone@example.net")

	src.stick()

	rules := decode(t, a.expect(http.StatusOK, http.MethodPut, "/api/v1/admin/settings/booking-rules",
		`{"releaseWeekday":0,"releaseTime":"18:00","releaseWeeksAhead":10,"bookingCutoffMinutes":0,`+
			`"cancellationCutoffMinutes":120,"waitlistInviteExpiryMinutes":720,"defaultVisibleDays":14}`, arno))
	if rules["releaseWeeksAhead"] != 10.0 || rules["generatedLessons"] != 0.0 {
		t.Errorf("rules %v", rules)
	}

	if got := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/admin/settings/booking-rules", "", arno)); got["releaseWeeksAhead"] != 10.0 {
		t.Errorf("stored rules %v", got)
	}

	line := grepLines(logs.String(), "generate the lessons of a wider release window")
	if !strings.Contains(line, "level=WARN") || !strings.Contains(line, "exists") {
		t.Errorf("log %q in\n%s", line, logs.String())
	}
}

// TestStatisticsWithoutTerm: once the demo term has ended, the term
// range has no term to count and is a 404; the other ranges still answer.
func TestStatisticsWithoutTerm(t *testing.T) {
	t.Parallel()

	a := newAuthApp(t, app.Deps{})
	a.clock.advance(400 * 24 * time.Hour) // past the demo term's end
	olga := a.login("olga.ledgerby@example.net")

	body := a.expect(http.StatusNotFound, http.MethodGet, "/api/v1/admin/statistics?range=term", "", olga)
	if p := decode(t, body); p["detail"] != "no term includes today" {
		t.Errorf("term range: %v", p)
	}

	if st := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/admin/statistics?range=last4Weeks", "", olga)); st["range"] != "last4Weeks" || st["term"] != nil {
		t.Errorf("last 4 weeks: %v", st)
	}
}
