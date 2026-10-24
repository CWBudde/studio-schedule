package app_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/app"
)

// TestMyLessons runs "My lessons" over HTTP (ADR 0009, amendment): the 401
// for signed-out visitors, a booking in the upcoming list with its lesson,
// and the paging fields of the history. The rules are tested in package
// service.
func TestMyLessons(t *testing.T) {
	t.Parallel()

	a := newAuthApp(t, app.Deps{Demo: true})
	office := a.login("olga.ledgerby@example.net")
	clara := a.login("clara.amberly@example.org")
	ella := a.family(clara).Dependents[0].ID

	for _, path := range []string{"/api/v1/my/lessons", "/api/v1/my/lessons/history"} {
		if res, _ := a.call(http.MethodGet, path, ""); res.StatusCode != http.StatusUnauthorized {
			t.Errorf("signed out %s: %d", path, res.StatusCode)
		}
	}

	// A workshop next week (published early); the test clock is Friday
	// 2026-10-23, 16:00 UTC.
	terms := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/terms", "", clara))
	termID, _ := terms["items"].([]any)[0].(map[string]any)["id"].(string)
	classes := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/terms/"+termID+"/classes", "", clara))
	existing, _ := classes["items"].([]any)[0].(map[string]any)
	instructor, _ := existing["instructor"].(map[string]any)["id"].(string)

	lesson := decode(t, a.expect(http.StatusCreated, http.MethodPost, "/api/v1/lessons", fmt.Sprintf(
		`{"courseTypeId":%q,"date":"2026-10-30","startTime":"21:30","durationMinutes":60,"roomId":%q,`+
			`"instructorId":%q,"capacity":4,"visibility":"public","kind":"workshop","confirmRoomOverlap":true}`,
		existing["courseTypeId"], existing["roomId"], instructor), office))
	lessonID, _ := lesson["id"].(string)

	booked := decode(t, a.expect(http.StatusCreated, http.MethodPost, "/api/v1/lessons/"+lessonID+"/bookings",
		`{"personId":"`+ella+`"}`, clara))

	mine := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/my/lessons", "", clara))
	if mine["timezone"] != "Europe/Berlin" || mine["now"] != "2026-10-23T16:00:00Z" {
		t.Errorf("my lessons: %v", mine)
	}

	if _, ok := mine["waitlist"].([]any); !ok {
		t.Errorf("waitlist %v", mine["waitlist"])
	}

	var item map[string]any

	for _, it := range mine["upcoming"].([]any) {
		m, _ := it.(map[string]any)
		if b, _ := m["booking"].(map[string]any); b["id"] == booked["id"] {
			item = m
		}
	}

	if item == nil {
		t.Fatalf("booking %v not in %v", booked["id"], mine["upcoming"])
	}

	b, _ := item["booking"].(map[string]any)
	l, _ := item["lesson"].(map[string]any)
	ct, _ := l["courseType"].(map[string]any)

	if b["status"] != "booked" || b["personId"] != ella || item["canCancel"] != true ||
		!strings.HasPrefix(item["personName"].(string), "Ella ") || l["id"] != lessonID || l["kind"] != "workshop" ||
		ct["id"] != existing["courseTypeId"] || ct["name"] == "" || l["room"] == nil || l["location"] == nil ||
		item["lateCancelFrom"] == nil {
		t.Errorf("item %v", item)
	}

	// The history: the default page ends now, the next three months earlier
	// in the school time zone.
	hist := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/my/lessons/history", "", clara))
	if hist["before"] != "2026-10-23T16:00:00Z" || hist["nextBefore"] != "2026-07-23T16:00:00Z" {
		t.Errorf("history page %v – %v", hist["nextBefore"], hist["before"])
	}

	if _, ok := hist["items"].([]any); !ok {
		t.Errorf("items %v", hist["items"])
	}

	if _, ok := hist["hasMore"].(bool); !ok {
		t.Errorf("hasMore %v", hist["hasMore"])
	}

	hist = decode(t, a.expect(http.StatusOK, http.MethodGet,
		"/api/v1/my/lessons/history?before=2026-01-31T23:00:00Z", "", clara))
	if hist["before"] != "2026-01-31T23:00:00Z" || hist["nextBefore"] != "2025-10-31T23:00:00Z" {
		t.Errorf("older page %v – %v", hist["nextBefore"], hist["before"])
	}

	a.expect(http.StatusUnprocessableEntity, http.MethodGet, "/api/v1/my/lessons/history?before=yesterday", "", clara)
}
