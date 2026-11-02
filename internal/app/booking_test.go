package app_test

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/app"
)

// TestBookings runs booking and cancellation over HTTP (ADR 0009): 201 and
// the booking, the 409 reasons with why, 404 for strangers, availability in
// the schedule and the lesson, the 422 of a started lesson, and canceling
// the following weeks. The rules at their edges are tested in package
// service.
func TestBookings(t *testing.T) {
	t.Parallel()

	a := newAuthApp(t, app.Deps{Demo: true})
	office := a.login("olga.ledgerby@example.net")
	clara := a.login("clara.amberly@example.org")

	family := a.family(clara)
	me, ella := family.Me.ID, family.Dependents[0].ID

	// A Wednesday class for 2 at 21:15 generates its lessons from next week
	// on; the test clock is Friday 2026-10-23, 16:00 UTC.
	terms := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/terms", "", clara))
	termID, _ := terms["items"].([]any)[0].(map[string]any)["id"].(string)
	demo := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/terms/"+termID+"/classes", "", clara))
	existing, _ := demo["items"].([]any)[0].(map[string]any)
	courseType, _ := existing["courseTypeId"].(string)
	room, _ := existing["roomId"].(string)
	instructor, _ := existing["instructor"].(map[string]any)["id"].(string)

	class := decode(t, a.expect(http.StatusCreated, http.MethodPost, "/api/v1/terms/"+termID+"/classes",
		fmt.Sprintf(`{"courseTypeId":%q,"roomId":%q,"instructorId":%q,"allLevels":true,"weekday":3,`+
			`"startTime":"21:15","durationMinutes":45,"capacity":2}`, courseType, room, instructor), office))

	schedule := func(cookie *http.Cookie) []map[string]any {
		t.Helper()

		res, body := a.call(http.MethodGet, "/api/v1/schedule?from=2026-10-26&to=2026-11-09", "", cookie)
		if res.StatusCode != http.StatusOK {
			t.Fatalf("schedule: %d %s", res.StatusCode, body)
		}

		var out []map[string]any

		for _, l := range decode(t, body)["lessons"].([]any) {
			if m, _ := l.(map[string]any); m["classId"] == class["id"] {
				out = append(out, m)
			}
		}

		return out
	}

	lessons := schedule(clara)
	if len(lessons) != 2 || lessons[0]["freePlaces"] != 2.0 || lessons[0]["bookedFor"] != nil {
		t.Fatalf("lessons of the class: %v", lessons)
	}

	first, _ := lessons[0]["id"].(string)
	second, _ := lessons[1]["id"].(string)
	bookPath := func(lesson string) string { return "/api/v1/lessons/" + lesson + "/bookings" }
	person := func(id string) string { return `{"personId":"` + id + `"}` }

	booked := decode(t, a.expect(http.StatusCreated, http.MethodPost, bookPath(first), person(ella), clara))
	if booked["lessonId"] != first || booked["personId"] != ella || booked["status"] != "booked" ||
		booked["source"] != "self" || booked["late"] != false || booked["canceledAt"] != nil || booked["id"] == "" {
		t.Fatalf("booked %v", booked)
	}

	if to, text := a.mailTo("booking-confirmed"); to != "clara.amberly@example.org" || !strings.Contains(text, "Ella") {
		t.Errorf("confirmation to %s:\n%s", to, text)
	}

	conflictReason := func(body []byte) (string, string) {
		c := decode(t, body)
		reason, _ := c["reason"].(string)
		why, _ := c["why"].(string)

		return reason, why
	}

	if reason, why := conflictReason(a.expect(http.StatusConflict, http.MethodPost, bookPath(first), person(ella),
		clara)); reason != "already-booked" || why != "" {
		t.Errorf("again: %s %s, want already-booked", reason, why)
	}

	a.expect(http.StatusCreated, http.MethodPost, bookPath(first), person(me), clara)

	if reason, _ := conflictReason(a.expect(http.StatusConflict, http.MethodPost, bookPath(first),
		person(family.Dependents[1].ID), clara)); reason != "full" {
		t.Errorf("third: %s, want full", reason)
	}

	// Nobody books outside their family, or a lesson that does not exist.
	a.expect(http.StatusNotFound, http.MethodPost, bookPath(first), person("00000000-0000-7000-8000-00000000f001"), clara)
	a.expect(http.StatusNotFound, http.MethodPost, bookPath("00000000-0000-7000-8000-00000000f001"), person(ella), clara)
	a.expect(http.StatusUnprocessableEntity, http.MethodPost, bookPath(first), `{}`, clara)

	// Availability: the family sees whom it booked, a guest only the
	// free places.
	lessons = schedule(clara)
	bookedFor, _ := lessons[0]["bookedFor"].([]any)

	var names []string

	for _, b := range bookedFor {
		m, _ := b.(map[string]any)
		names = append(names, m["name"].(string))

		if m["personId"] == ella && m["bookingId"] != booked["id"] {
			t.Errorf("booked for %v, want booking %v", m, booked["id"])
		}
	}

	if lessons[0]["freePlaces"] != 0.0 || !slices.Equal(names, []string{"Clara Amberly", "Ella Amberly"}) {
		t.Errorf("schedule for clara: %v", lessons[0])
	}

	detail := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/lessons/"+first, "", clara))
	if detail["freePlaces"] != 0.0 || len(detail["bookedFor"].([]any)) != 2 {
		t.Errorf("lesson for clara: %v", detail)
	}

	// Cancel: the place is free again; a second cancel is a conflict, and
	// strangers get a 404.
	cancelPath := "/api/v1/bookings/" + booked["id"].(string) + "/cancel"
	a.expect(http.StatusNotFound, http.MethodPost, cancelPath, "", office)

	canceled := decode(t, a.expect(http.StatusOK, http.MethodPost, cancelPath, "", clara))
	if canceled["status"] != "canceled" || canceled["late"] != false || canceled["canceledAt"] == nil {
		t.Errorf("canceled %v", canceled)
	}

	if reason, _ := conflictReason(a.expect(http.StatusConflict, http.MethodPost, cancelPath, "", clara)); reason != "already-canceled" {
		t.Errorf("cancel again: %s, want already-canceled", reason)
	}

	if to, _ := a.mailTo("booking-canceled"); to != "clara.amberly@example.org" {
		t.Errorf("cancellation mail to %s", to)
	}

	if lessons = schedule(clara); lessons[0]["freePlaces"] != 1.0 {
		t.Errorf("after the cancellation: %v", lessons[0])
	}

	// Following weeks: Ella in both lessons of the class.
	ellaFirst := decode(t, a.expect(http.StatusCreated, http.MethodPost, bookPath(first), person(ella), clara))
	a.expect(http.StatusCreated, http.MethodPost, bookPath(second), person(ella), clara)

	followingPath := "/api/v1/bookings/" + ellaFirst["id"].(string) + "/cancel-following"
	a.expect(http.StatusNotFound, http.MethodGet, followingPath, "", office)

	// The preview is a GET on the path the cancellation POSTs to.
	preview := decode(t, a.expect(http.StatusOK, http.MethodGet, followingPath, "", clara))
	if items, _ := preview["bookings"].([]any); len(items) != 2 ||
		items[0].(map[string]any)["lessonId"] != first || items[1].(map[string]any)["lessonId"] != second ||
		items[0].(map[string]any)["late"] != false || preview["until"] == nil {
		t.Fatalf("preview %v", preview)
	}

	// An end date before the lesson is refused where it was given.
	early := decode(t, a.expect(http.StatusUnprocessableEntity, http.MethodGet, followingPath+"?until=2000-01-01", "", clara))
	if errs, _ := early["errors"].([]any); len(errs) != 1 || errs[0].(map[string]any)["location"] != "query.until" {
		t.Errorf("preview until 2000-01-01: %v, want 422 on query.until", early)
	}

	early = decode(t, a.expect(http.StatusUnprocessableEntity, http.MethodPost, followingPath, `{"until":"2000-01-01"}`, clara))
	if errs, _ := early["errors"].([]any); len(errs) != 1 || errs[0].(map[string]any)["location"] != "body.until" {
		t.Errorf("cancel until 2000-01-01: %v, want 422 on body.until", early)
	}

	done := decode(t, a.expect(http.StatusOK, http.MethodPost, followingPath, "", clara))
	if items, _ := done["bookings"].([]any); len(items) != 2 {
		t.Fatalf("canceled following %v", done)
	}

	if lessons = schedule(clara); lessons[0]["freePlaces"] != 1.0 || lessons[1]["freePlaces"] != 2.0 {
		t.Errorf("after canceling the following weeks: %v", lessons)
	}

	if reason, _ := conflictReason(a.expect(http.StatusConflict, http.MethodPost, followingPath, "", clara)); reason != "already-canceled" {
		t.Errorf("again: %s, want already-canceled", reason)
	}

	// A one-off workshop has no following weeks; closed lessons say why.
	oneOff := func(date, start, extra string) string {
		l := decode(t, a.expect(http.StatusCreated, http.MethodPost, "/api/v1/lessons",
			fmt.Sprintf(`{"courseTypeId":%q,"date":%q,"startTime":%q,"durationMinutes":60,"roomId":%q,`+
				`"instructorId":%q,"capacity":5,"visibility":"public","kind":"workshop"%s}`,
				courseType, date, start, room, instructor, extra), office))
		id, _ := l["id"].(string)

		return id
	}

	workshop := oneOff("2026-12-01", "10:00", "")
	ws := decode(t, a.expect(http.StatusCreated, http.MethodPost, bookPath(workshop), person(ella), clara))

	// A guest sees the free places of a workshop (published early), not
	// who booked it.
	if res, body := a.call(http.MethodGet, "/api/v1/lessons/"+workshop, ""); res.StatusCode != http.StatusOK {
		t.Errorf("lesson for a guest: %d %s", res.StatusCode, body)
	} else if guest := decode(t, body); guest["freePlaces"] != 4.0 || guest["bookedFor"] != nil ||
		guest["bookingClosesAt"] != "2026-12-01T09:00:00Z" || guest["lateCancelFrom"] != "2026-12-01T07:00:00Z" {
		// The demo's booking cutoff is 0, its cancellation cutoff 2 hours.
		t.Errorf("lesson for a guest: %v", guest)
	}

	notClass := decode(t, a.expect(http.StatusUnprocessableEntity, http.MethodGet,
		"/api/v1/bookings/"+ws["id"].(string)+"/cancel-following", "", clara))
	if errs, _ := notClass["errors"].([]any); len(errs) != 1 || errs[0].(map[string]any)["location"] != "path.bookingId" {
		t.Errorf("one-off: %v, want 422 on path.bookingId", notClass)
	}

	past := oneOff("2026-10-01", "10:00", `,"confirmPast":true`)
	if reason, why := conflictReason(a.expect(http.StatusConflict, http.MethodPost, bookPath(past), person(ella),
		clara)); reason != "closed" || why != "started" {
		t.Errorf("past: %s %s, want closed started", reason, why)
	}

	// A lesson the school canceled is never canceled late.
	if c := decode(t, a.expect(http.StatusOK, http.MethodPost, "/api/v1/lessons/"+workshop+"/cancel", `{}`, office)); c["bookingClosesAt"] != "2026-12-01T09:00:00Z" || c["lateCancelFrom"] != nil {
		t.Errorf("canceled lesson: %v", c)
	}

	if reason, why := conflictReason(a.expect(http.StatusConflict, http.MethodPost, bookPath(workshop), person(me),
		clara)); reason != "closed" || why != "canceled" {
		t.Errorf("canceled lesson: %s %s, want closed canceled", reason, why)
	}

	// After its start, nobody cancels a booking.
	tomorrow := oneOff("2026-10-24", "10:00", "")
	soon := decode(t, a.expect(http.StatusCreated, http.MethodPost, bookPath(tomorrow), person(ella), clara))

	a.clock.advance(24 * time.Hour)

	started := decode(t, a.expect(http.StatusUnprocessableEntity, http.MethodPost,
		"/api/v1/bookings/"+soon["id"].(string)+"/cancel", "", clara))
	if errs, _ := started["errors"].([]any); len(errs) != 1 || errs[0].(map[string]any)["location"] != "path.bookingId" {
		t.Errorf("started: %v, want 422 on path.bookingId", started)
	}
}

// TestBookingClosedForStaff books under member rules: the office sees a
// hidden lesson, but may not book it.
func TestBookingClosedForStaff(t *testing.T) {
	t.Parallel()

	a := newAuthApp(t, app.Deps{})
	office := a.login("olga.ledgerby@example.net")
	me := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/family", "", office))["me"].(map[string]any)["id"].(string)

	terms := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/terms", "", office))
	termID, _ := terms["items"].([]any)[0].(map[string]any)["id"].(string)
	demo := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/terms/"+termID+"/classes", "", office))
	existing, _ := demo["items"].([]any)[0].(map[string]any)

	hidden := decode(t, a.expect(http.StatusCreated, http.MethodPost, "/api/v1/lessons",
		fmt.Sprintf(`{"courseTypeId":%q,"date":"2026-10-27","startTime":"10:00","durationMinutes":60,"roomId":%q,`+
			`"instructorId":%q,"capacity":5,"visibility":"hidden","kind":"regular"}`,
			existing["courseTypeId"], existing["roomId"], existing["instructor"].(map[string]any)["id"]), office))

	c := decode(t, a.expect(http.StatusConflict, http.MethodPost, "/api/v1/lessons/"+hidden["id"].(string)+"/bookings",
		`{"personId":"`+me+`"}`, office))
	if c["reason"] != "closed" || c["why"] != "release" {
		t.Errorf("hidden lesson for the office: %v, want closed release", c)
	}

	clara := a.login("clara.amberly@example.org")
	claraID := a.family(clara).Me.ID
	a.expect(http.StatusNotFound, http.MethodPost, "/api/v1/lessons/"+hidden["id"].(string)+"/bookings",
		`{"personId":"`+claraID+`"}`, clara)
}
