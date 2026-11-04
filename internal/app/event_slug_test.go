package app_test

import (
	"fmt"
	"net/http"
	"testing"
)

// TestSessionEventSlug: the schedule, a lesson and the instructor view
// give an event's session its event's slug (ADR 0019), for a member and
// for the office, and a plain one-off lesson on the same day none.
func TestSessionEventSlug(t *testing.T) {
	t.Parallel()

	a := newEventsApp(t)
	const events = "/api/v1/admin/events"

	created := decode(t, a.expect(http.StatusCreated, http.MethodPost, events, a.event("Imaginary Intensive", 4, ""),
		a.office))
	id, _ := created["id"].(string)
	session := decode(t, a.expect(http.StatusCreated, http.MethodPost, events+"/"+id+"/sessions",
		a.session("2026-10-24", "07:00", ""), a.office))
	sessionID, _ := session["id"].(string)
	a.expect(http.StatusOK, http.MethodPost, events+"/"+id+"/publish", "", a.office)

	lesson := decode(t, a.expect(http.StatusCreated, http.MethodPost, "/api/v1/lessons", fmt.Sprintf(
		`{"courseTypeId":%q,"date":"2026-10-24","startTime":"10:00","durationMinutes":60,"roomId":%q,`+
			`"instructorId":%q,"capacity":5,"visibility":"public","kind":"workshop"}`,
		a.courseType, a.room, a.staff), a.office))
	plain, _ := lesson["id"].(string)

	// slugs returns the lessons of the body by ID, each with its eventSlug
	// ("" if absent), and fails unless the session and the plain lesson
	// are among them.
	slugs := func(name string, body []byte) map[string]string {
		t.Helper()

		out := map[string]string{}

		for _, item := range decode(t, body)["lessons"].([]any) {
			l, _ := item.(map[string]any)
			lessonID, _ := l["id"].(string)
			out[lessonID], _ = l["eventSlug"].(string)
		}

		_, hasSession := out[sessionID]
		if _, hasPlain := out[plain]; !hasSession || !hasPlain {
			t.Fatalf("%s: lessons %v, want the session %s and the lesson %s", name, out, sessionID, plain)
		}

		return out
	}

	for name, cookie := range map[string]*http.Cookie{"member": a.member, "office": a.office} {
		got := slugs(name+"'s schedule", a.expect(http.StatusOK, http.MethodGet,
			"/api/v1/schedule?from=2026-10-24&to=2026-10-25", "", cookie))

		if got[sessionID] != "imaginary-intensive" || got[plain] != "" {
			t.Errorf("%s: the schedule's slugs %v", name, got)
		}

		if l := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/lessons/"+sessionID, "", cookie)); l["eventSlug"] != "imaginary-intensive" {
			t.Errorf("%s: the session %v", name, l)
		}

		if l := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/lessons/"+plain, "", cookie)); l["eventSlug"] != nil {
			t.Errorf("%s: the plain lesson %v", name, l)
		}
	}

	got := slugs("the instructor view", a.expect(http.StatusOK, http.MethodGet,
		"/api/v1/teach?from=2026-10-24&to=2026-10-25", "", a.office))
	if got[sessionID] != "imaginary-intensive" || got[plain] != "" {
		t.Errorf("the instructor view's slugs %v", got)
	}
}
