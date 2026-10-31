package app_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/app"
)

// firstDemoClass returns the first class of the demo term as the office lists
// it.
func firstDemoClass(t *testing.T, a *authApp, office *http.Cookie) map[string]any {
	t.Helper()

	terms := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/terms", "", office))
	termID, _ := terms["items"].([]any)[0].(map[string]any)["id"].(string)
	classes := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/terms/"+termID+"/classes", "", office))
	class, _ := classes["items"].([]any)[0].(map[string]any)

	return class
}

// createWorkshop creates a public workshop of capacity on Tuesday 2026-11-03,
// 10:00 in Berlin (09:00 UTC), in the course type, room and with the
// instructor of the first demo class, and returns it.
func createWorkshop(t *testing.T, a *authApp, office *http.Cookie, capacity int) map[string]any {
	t.Helper()

	class := firstDemoClass(t, a, office)

	return decode(t, a.expect(http.StatusCreated, http.MethodPost, "/api/v1/lessons",
		fmt.Sprintf(`{"courseTypeId":%q,"date":"2026-11-03","startTime":"10:00","durationMinutes":60,"roomId":%q,`+
			`"instructorId":%q,"capacity":%d,"visibility":"public","kind":"workshop","confirmRoomOverlap":true}`,
			class["courseTypeId"], class["roomId"], class["instructor"].(map[string]any)["id"], capacity), office))
}

// itemOfPerson returns the item of items whose personId (or, nested, whose
// entry's personId) is person.
func itemOfPerson(t *testing.T, items []any, person string) map[string]any {
	t.Helper()

	for _, it := range items {
		m, _ := it.(map[string]any)
		if m["personId"] == person {
			return m
		}

		if e, ok := m["entry"].(map[string]any); ok && e["personId"] == person {
			return m
		}
	}

	t.Fatalf("no item of person %s in %v", person, items)

	return nil
}

// TestWaitlistWithInvite fills a lesson, puts two of Clara's children on
// its waiting list and frees the place: the first is invited, the second
// waits. The staff's participant list, "My lessons" and the in-app accept
// show the invite as the API documents it.
func TestWaitlistWithInvite(t *testing.T) {
	t.Parallel()

	a := newAuthApp(t, app.Deps{Demo: true})
	office := a.login("olga.ledgerby@example.net")
	clara := a.login("clara.amberly@example.org")
	family := a.family(clara)
	me, ella, second := family.Me.ID, family.Dependents[0].ID, family.Dependents[1].ID

	lesson := createWorkshop(t, a, office, 1)
	full, _ := lesson["id"].(string)

	booking := decode(t, a.expect(http.StatusCreated, http.MethodPost, "/api/v1/lessons/"+full+"/bookings",
		`{"personId":"`+me+`"}`, clara))
	ellaEntry := decode(t, a.expect(http.StatusCreated, http.MethodPost, "/api/v1/lessons/"+full+"/waitlist",
		`{"personId":"`+ella+`"}`, clara))
	secondEntry := decode(t, a.expect(http.StatusCreated, http.MethodPost, "/api/v1/lessons/"+full+"/waitlist",
		`{"personId":"`+second+`","stayOnList":true}`, clara))

	// Clara's place frees: Ella, first on the list, is invited for the
	// invite expiry of the demo (12 hours from Friday 16:00 UTC).
	a.expect(http.StatusOK, http.MethodPost, "/api/v1/bookings/"+booking["id"].(string)+"/cancel", "", clara)

	const expires = "2026-10-24T04:00:00Z"

	// The staff's list: the invited entry has its expiry and no position,
	// the waiting one its position and no expiry.
	parts := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/lessons/"+full+"/participants", "", office))
	if parts["held"] != 1.0 || parts["waiting"] != 1.0 || parts["freePlaces"] != 0.0 || parts["active"] != 0.0 {
		t.Errorf("counts %v", parts)
	}

	list, _ := parts["waitlist"].([]any)
	if len(list) != 2 {
		t.Fatalf("waitlist %v", parts["waitlist"])
	}

	invited := itemOfPerson(t, list, ella)
	if invited["entryId"] != ellaEntry["id"] || invited["status"] != "invited" || invited["expiresAt"] != expires ||
		invited["position"] != nil || invited["stayOnList"] != false || invited["name"] != "Ella Amberly" ||
		invited["joinedAt"] != "2026-10-23T16:00:00Z" {
		t.Errorf("invited entry %v", invited)
	}

	waiting := itemOfPerson(t, list, second)
	if waiting["entryId"] != secondEntry["id"] || waiting["status"] != "waiting" || waiting["expiresAt"] != nil ||
		waiting["position"] != 1.0 || waiting["stayOnList"] != true {
		t.Errorf("waiting entry %v", waiting)
	}

	// "My lessons": both entries with their lesson and when booking closes
	// (the demo's booking cutoff is 0: at the start).
	mine := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/my/lessons", "", clara))

	var ofLesson []any

	for _, it := range mine["waitlist"].([]any) {
		if l, _ := it.(map[string]any)["lesson"].(map[string]any); l["id"] == full {
			ofLesson = append(ofLesson, it)
		}
	}

	if len(ofLesson) != 2 {
		t.Fatalf("my waitlist %v", mine["waitlist"])
	}

	for _, it := range ofLesson {
		m, _ := it.(map[string]any)
		l, _ := m["lesson"].(map[string]any)

		if m["bookingClosesAt"] != "2026-11-03T09:00:00Z" || l["startsAt"] != "2026-11-03T09:00:00Z" ||
			l["kind"] != "workshop" {
			t.Errorf("my waitlist item %v", m)
		}
	}

	if e := itemOfPerson(t, ofLesson, ella)["entry"].(map[string]any); e["entryId"] != ellaEntry["id"] ||
		e["status"] != "invited" || e["expiresAt"] != expires || e["position"] != nil || e["name"] != "Ella Amberly" {
		t.Errorf("my invited entry %v", e)
	}

	if e := itemOfPerson(t, ofLesson, second)["entry"].(map[string]any); e["entryId"] != secondEntry["id"] ||
		e["status"] != "waiting" || e["expiresAt"] != nil || e["position"] != 1.0 || e["stayOnList"] != true {
		t.Errorf("my waiting entry %v", e)
	}

	// Clara accepts Ella's invite in the app: the answer is the booking.
	accepted := decode(t, a.expect(http.StatusOK, http.MethodPost, "/api/v1/waitlist/"+ellaEntry["id"].(string)+"/accept", "", clara))
	if accepted["lessonId"] != full || accepted["personId"] != ella || accepted["status"] != "booked" ||
		accepted["source"] != "self" || accepted["bookedAt"] != "2026-10-23T16:00:00Z" || accepted["id"] == "" ||
		accepted["late"] != false || accepted["canceledAt"] != nil {
		t.Errorf("accepted %v", accepted)
	}

	parts = decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/lessons/"+full+"/participants", "", office))
	if parts["held"] != 0.0 || parts["active"] != 1.0 || parts["waiting"] != 1.0 {
		t.Errorf("counts after accepting %v", parts)
	}

	if e := itemOfPerson(t, parts["waitlist"].([]any), ella); e["status"] != "accepted" || e["expiresAt"] != nil {
		t.Errorf("accepted entry %v", e)
	}
}

// TestWaitlistInviteLookupRateLimit: looking at invite links alone runs
// into the limit of the invite links: the 21st look from one client is a
// 429, before the token is even looked up.
func TestWaitlistInviteLookupRateLimit(t *testing.T) {
	t.Parallel()

	a := newAuthApp(t, app.Deps{})

	for i := range 20 {
		if res, body := a.callFrom("198.51.100.4:4321", "", http.MethodGet, fmt.Sprintf("/api/v1/waitlist-invites/b2-%d", i), ""); res.StatusCode != http.StatusNotFound {
			t.Fatalf("look %d: %d %s", i, res.StatusCode, body)
		}
	}

	res, body := a.callFrom("198.51.100.4:4321", "", http.MethodGet, "/api/v1/waitlist-invites/b2-last", "")
	if res.StatusCode != http.StatusTooManyRequests || res.Header.Get("Retry-After") == "" {
		t.Fatalf("look 21: %d %s, want 429 with Retry-After", res.StatusCode, body)
	}

	if p := decode(t, body); p["status"] != float64(http.StatusTooManyRequests) {
		t.Errorf("problem %v", p)
	}
}
