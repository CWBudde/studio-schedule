package app_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/app"
)

// TestParticipants runs the staff actions over HTTP (ADR 0009, amendment):
// who may act on which lesson (403 for members, 404 on others' lessons),
// adding persons and guests with the 409 reasons and the 422 locations,
// trials, removal, attendance with its window, and the booking history for
// the office. The rules at their edges are tested in package service.
func TestParticipants(t *testing.T) {
	t.Parallel()

	a := newAuthApp(t, app.Deps{Demo: true})
	office := a.login("olga.ledgerby@example.net")
	alina := a.login("alina.starling@example.net")
	felix := a.login("felix.keysworth@example.net")
	clara := a.login("clara.amberly@example.org")
	me := a.family(clara).Me.ID

	staff := map[string]string{}
	for _, s := range decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/staff", "", clara))["items"].([]any) {
		m, _ := s.(map[string]any)
		staff[m["displayName"].(string)], _ = m["id"].(string)
	}

	terms := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/terms", "", clara))
	termID, _ := terms["items"].([]any)[0].(map[string]any)["id"].(string)
	classes := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/terms/"+termID+"/classes", "", clara))
	existing, _ := classes["items"].([]any)[0].(map[string]any)

	// The test clock is Friday 2026-10-23, 16:00 UTC (18:00 in Berlin): the
	// lesson starts in half an hour, and its day ends at 22:00 UTC.
	lesson := func(instructor, accompanist string) string {
		l := decode(t, a.expect(http.StatusCreated, http.MethodPost, "/api/v1/lessons", fmt.Sprintf(
			`{"courseTypeId":%q,"date":"2026-10-23","startTime":"18:30","durationMinutes":60,"roomId":%q,`+
				`"instructorId":%q,"accompanistId":%q,"capacity":1,"visibility":"hidden","kind":"private",`+
				`"confirmRoomOverlap":true}`,
			existing["courseTypeId"], existing["roomId"], staff[instructor], staff[accompanist]), office))
		id, _ := l["id"].(string)

		return id
	}

	own := lesson("Alina Starling", "Felix Keysworth")
	other := lesson("Sofia Quillon", "Greta Ivorylane")
	list := func(lesson string) string { return "/api/v1/lessons/" + lesson + "/participants" }

	// Members may not; instructors see their own lessons only.
	a.expect(http.StatusForbidden, http.MethodGet, list(own), "", clara)
	a.expect(http.StatusNotFound, http.MethodGet, list(other), "", alina)
	a.expect(http.StatusNotFound, http.MethodPost, list(other), `{"guest":{"name":"Gina Guest"}}`, felix)
	a.expect(http.StatusOK, http.MethodGet, list(other), "", office)

	// Nobody adds to a lesson the school canceled.
	a.expect(http.StatusOK, http.MethodPost, "/api/v1/lessons/"+other+"/cancel", `{}`, office)

	c := decode(t, a.expect(http.StatusConflict, http.MethodPost, list(other), `{"guest":{"name":"Gina Guest"}}`, office))
	if c["reason"] != "closed" || c["why"] != "canceled" {
		t.Errorf("canceled lesson: %v", c)
	}

	empty := decode(t, a.expect(http.StatusOK, http.MethodGet, list(own), "", felix))
	if empty["capacity"] != 1.0 || empty["freePlaces"] != 1.0 || len(empty["bookings"].([]any)) != 0 ||
		empty["dayOverAt"] != "2026-10-23T22:00:00Z" {
		t.Fatalf("empty list %v", empty)
	}

	reason := func(body []byte) string {
		r, _ := decode(t, body)["reason"].(string)

		return r
	}

	location := func(body []byte) string {
		errs, _ := decode(t, body)["errors"].([]any)
		if len(errs) != 1 {
			return ""
		}

		l, _ := errs[0].(map[string]any)["location"].(string)

		return l
	}

	// Alina books Clara; she gets the confirmation.
	added := decode(t, a.expect(http.StatusCreated, http.MethodPost, list(own), `{"personId":"`+me+`"}`, alina))
	if added["source"] != "staff" || added["personId"] != me || added["status"] != "booked" || added["trial"] != false {
		t.Errorf("added %v", added)
	}

	if to, _ := a.mailTo("booking-confirmed"); to != "clara.amberly@example.org" {
		t.Errorf("confirmation to %s", to)
	}

	if r := reason(a.expect(http.StatusConflict, http.MethodPost, list(own), `{"personId":"`+me+`"}`, felix)); r != "already-booked" {
		t.Errorf("again: %s, want already-booked", r)
	}

	// The lesson is full: guests need the confirmation.
	gina := `{"guest":{"name":"Gina Guest","email":"gina@example.org"},"trial":true,"trialNote":"Danced jazz before"}`
	if r := reason(a.expect(http.StatusConflict, http.MethodPost, list(own), gina, alina)); r != "full" {
		t.Errorf("full: %s", r)
	}

	guest := decode(t, a.expect(http.StatusCreated, http.MethodPost, list(own),
		strings.Replace(gina, "}", `},"confirmOverCapacity":true`, 1), alina))
	if guest["guestName"] != "Gina Guest" || guest["personId"] != nil || guest["trialNote"] != "Danced jazz before" ||
		guest["source"] != "staff" {
		t.Errorf("guest %v", guest)
	}

	if to, text := a.mailTo("booking-confirmed"); to != "gina@example.org" || !strings.Contains(text, "Gina Guest") {
		t.Errorf("guest confirmation to %s:\n%s", to, text)
	}

	walkIn := decode(t, a.expect(http.StatusCreated, http.MethodPost, list(own),
		`{"guest":{"name":"Walter Walk-in"},"confirmOverCapacity":true}`, felix))

	for body, want := range map[string]string{
		`{"personId":"` + me + `","guest":{"name":"Gina"}}`: "body",
		`{}`: "body",
		`{"guest":{"name":"Gina"},"trialNote":"note"}`:        "body.trialNote",
		`{"guest":{"name":"Gina","email":"gina@"}}`:           "body.guest.email",
		`{"guest":{"name":" "}}`:                              "body.guest.name",
		`{"personId":"00000000-0000-7000-8000-00000000f001"}`: "body.personId",
	} {
		if got := location(a.expect(http.StatusUnprocessableEntity, http.MethodPost, list(own), body, alina)); got != want {
			t.Errorf("%s: 422 at %q, want %q", body, got, want)
		}
	}

	participants := decode(t, a.expect(http.StatusOK, http.MethodGet, list(own), "", alina))
	bookings, _ := participants["bookings"].([]any)

	if participants["active"] != 3.0 || participants["freePlaces"] != 0.0 || len(bookings) != 3 {
		t.Fatalf("participants %v", participants)
	}

	if c := bookings[0].(map[string]any); c["name"] != "Clara Amberly" || c["contactEmail"] != "clara.amberly@example.org" ||
		c["contactPhone"] != "+49 000 0000101" || c["guest"] != false {
		t.Errorf("Clara %v", c)
	}

	if g := bookings[1].(map[string]any); g["name"] != "Gina Guest" || g["guest"] != true || g["trial"] != true ||
		g["contactEmail"] != "gina@example.org" {
		t.Errorf("Gina %v", g)
	}

	// Trial flags.
	claraBooking, _ := added["id"].(string)
	booking := func(id, action string) string { return "/api/v1/bookings/" + id + "/" + action }

	trial := decode(t, a.expect(http.StatusOK, http.MethodPut, booking(claraBooking, "participant"),
		`{"trial":true,"trialNote":"Back after a break"}`, felix))
	if trial["trial"] != true || trial["trialNote"] != "Back after a break" {
		t.Errorf("trial %v", trial)
	}

	if got := location(a.expect(http.StatusUnprocessableEntity, http.MethodPut, booking(claraBooking, "participant"),
		`{"trial":false,"trialNote":"note"}`, felix)); got != "body.trialNote" {
		t.Errorf("note without trial at %q", got)
	}

	// Attendance opens at the start: the 422 says why, for the UI.
	early := a.expect(http.StatusUnprocessableEntity, http.MethodPost, booking(claraBooking, "attendance"),
		`{"status":"attended"}`, alina)
	if got := location(early); got != "path.bookingId" || reason(early) != "not-started" {
		t.Errorf("attendance before the start at %q, reason %q", got, reason(early))
	}

	if r := reason(a.expect(http.StatusUnprocessableEntity, http.MethodPost, "/api/v1/lessons/"+own+"/attendance/all",
		"", alina)); r != "not-started" {
		t.Errorf("all attended before the start: reason %q", r)
	}

	// Removing Gina before the start tells her.
	ginaBooking, _ := guest["id"].(string)

	removed := decode(t, a.expect(http.StatusOK, http.MethodPost, booking(ginaBooking, "remove"), "", alina))
	if removed["status"] != "canceled" || removed["late"] != false {
		t.Errorf("removed %v", removed)
	}

	if to, _ := a.mailTo("booking-canceled"); to != "gina@example.org" {
		t.Errorf("cancellation to %s", to)
	}

	if r := reason(a.expect(http.StatusConflict, http.MethodPost, booking(ginaBooking, "remove"), "", alina)); r != "already-canceled" {
		t.Errorf("remove again: %s", r)
	}

	// During the lesson: Felix marks Clara, Alina everyone else.
	a.clock.advance(time.Hour)

	if got := decode(t, a.expect(http.StatusOK, http.MethodPost, booking(claraBooking, "attendance"),
		`{"status":"no_show"}`, felix)); got["status"] != "no_show" {
		t.Errorf("no-show %v", got)
	}

	all := decode(t, a.expect(http.StatusOK, http.MethodPost, "/api/v1/lessons/"+own+"/attendance/all", "", alina))
	if items, _ := all["items"].([]any); len(items) != 1 || items[0].(map[string]any)["id"] != walkIn["id"] {
		t.Errorf("all attended %v", all)
	}

	a.expect(http.StatusUnprocessableEntity, http.MethodPost, booking(claraBooking, "attendance"), `{"status":"maybe"}`, felix)

	// The history is the office's.
	history := func(id string) string { return "/api/v1/admin/bookings/" + id + "/events" }
	events := decode(t, a.expect(http.StatusOK, http.MethodGet, history(claraBooking), "", office))
	items, _ := events["items"].([]any)

	if len(items) != 2 || items[0].(map[string]any)["status"] != "booked" ||
		items[0].(map[string]any)["actor"].(map[string]any)["name"] != "Alina Starling" ||
		items[1].(map[string]any)["status"] != "no_show" ||
		items[1].(map[string]any)["actor"].(map[string]any)["email"] != "felix.keysworth@example.net" {
		t.Errorf("events %v", events)
	}

	a.expect(http.StatusForbidden, http.MethodGet, history(claraBooking), "", alina)
	a.expect(http.StatusNotFound, http.MethodGet, history("00000000-0000-7000-8000-00000000f001"), "", office)
	a.expect(http.StatusNotFound, http.MethodGet, booking(claraBooking, "events"), "", office)

	// After the lesson's day, only the office changes it.
	a.clock.advance(6 * time.Hour)

	late := a.expect(http.StatusUnprocessableEntity, http.MethodPost, booking(claraBooking, "attendance"),
		`{"status":"attended"}`, alina)
	if got := location(late); got != "path.bookingId" || reason(late) != "day-over" {
		t.Errorf("attendance after the day at %q, reason %q", got, reason(late))
	}

	late = a.expect(http.StatusUnprocessableEntity, http.MethodPost, list(own), `{"guest":{"name":"Late Guest"}}`, alina)
	if got := location(late); got != "path.lessonId" || reason(late) != "day-over" {
		t.Errorf("add after the day at %q, reason %q", got, reason(late))
	}

	for _, path := range []string{booking(claraBooking, "remove"), "/api/v1/lessons/" + own + "/attendance/all"} {
		if r := reason(a.expect(http.StatusUnprocessableEntity, http.MethodPost, path, "", felix)); r != "day-over" {
			t.Errorf("%s after the day: reason %q", path, r)
		}
	}

	if r := reason(a.expect(http.StatusUnprocessableEntity, http.MethodPut, booking(claraBooking, "participant"),
		`{"trial":true}`, felix)); r != "day-over" {
		t.Errorf("trial after the day: reason %q", r)
	}

	// A 422 about the request body has no reason.
	if r := reason(a.expect(http.StatusUnprocessableEntity, http.MethodPut, booking(claraBooking, "participant"),
		`{"trial":false,"trialNote":"note"}`, office)); r != "" {
		t.Errorf("body 422 with reason %q", r)
	}

	a.expect(http.StatusOK, http.MethodPost, booking(claraBooking, "attendance"), `{"status":"attended"}`, office)
}

// TestSearchPersonsForStaff: instructors find persons to add by name, with
// the birth year and nothing else; members may not search.
func TestSearchPersonsForStaff(t *testing.T) {
	t.Parallel()

	a := newAuthApp(t, app.Deps{Demo: true})
	alina := a.login("alina.starling@example.net")
	clara := a.login("clara.amberly@example.org")
	search := func(q string) string { return "/api/v1/staff/persons?q=" + url.QueryEscape(q) }

	if res, _ := a.call(http.MethodGet, search("clara"), ""); res.StatusCode != http.StatusUnauthorized {
		t.Errorf("signed out: %d, want 401", res.StatusCode)
	}

	a.expect(http.StatusForbidden, http.MethodGet, search("clara"), "", clara)
	a.expect(http.StatusUnprocessableEntity, http.MethodGet, search("c"), "", alina)

	var found struct {
		Items []map[string]any `json:"items"`
	}

	if err := json.Unmarshal(a.expect(http.StatusOK, http.MethodGet, search("clara amb"), "", alina), &found); err != nil {
		t.Fatal(err)
	}

	if len(found.Items) != 1 || found.Items[0]["name"] != "Clara Amberly" || found.Items[0]["id"] == nil {
		t.Fatalf("found %v", found.Items)
	}

	for key := range found.Items[0] {
		if key != "id" && key != "name" && key != "birthYear" {
			t.Errorf("field %q in %v, want only id, name and birthYear", key, found.Items[0])
		}
	}

	// Email addresses do not match.
	if err := json.Unmarshal(a.expect(http.StatusOK, http.MethodGet, search("example.org"), "", alina), &found); err != nil {
		t.Fatal(err)
	}

	if len(found.Items) != 0 {
		t.Errorf("by email: %v", found.Items)
	}
}
