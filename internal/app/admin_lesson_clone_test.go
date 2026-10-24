package app_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/app"
)

// TestAdminLessonClone runs previewLessonClone and cloneLessons over HTTP:
// office only, the copy of a one-off lesson, the skipped lessons of the
// demo's classes, a repeated clone, and the 422s of the request (the rules
// are tested in package service).
func TestAdminLessonClone(t *testing.T) {
	t.Parallel()

	a := newAuthApp(t, app.Deps{Demo: true})
	office := a.login("olga.ledgerby@example.net")
	member := a.login("clara.amberly@example.org")

	// The test clock is Friday 2026-10-23, 16:00 UTC.
	terms := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/terms", "", member))
	termID, _ := terms["items"].([]any)[0].(map[string]any)["id"].(string)
	demo := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/terms/"+termID+"/classes", "", member))
	existing, _ := demo["items"].([]any)[0].(map[string]any)
	courseType, _ := existing["courseTypeId"].(string)
	room, _ := existing["roomId"].(string)
	instructor, _ := existing["instructor"].(map[string]any)["id"].(string)

	// A new Tuesday class generates its lessons from next week on.
	class := decode(t, a.expect(http.StatusCreated, http.MethodPost, "/api/v1/terms/"+termID+"/classes",
		fmt.Sprintf(`{"courseTypeId":%q,"roomId":%q,"instructorId":%q,"level":"open","weekday":2,`+
			`"startTime":"21:15","durationMinutes":45,"capacity":6}`, courseType, room, instructor), office))

	// A workshop late on Tuesday 2026-10-27, after the class.
	workshop := decode(t, a.expect(http.StatusCreated, http.MethodPost, "/api/v1/lessons",
		fmt.Sprintf(`{"courseTypeId":%q,"date":"2026-10-27","startTime":"22:30","durationMinutes":60,"roomId":%q,`+
			`"instructorId":%q,"capacity":5,"visibility":"members","kind":"workshop"}`, courseType, room, instructor),
		office))

	body := `{"unit":"day","source":"2026-10-27","target":"2026-11-03"}`

	a.expect(http.StatusForbidden, http.MethodPost, "/api/v1/admin/lessons/clone/preview", body, member)
	a.expect(http.StatusForbidden, http.MethodPost, "/api/v1/admin/lessons/clone", body, member)

	preview := decode(t, a.expect(http.StatusOK, http.MethodPost, "/api/v1/admin/lessons/clone/preview", body, office))
	cloned := decode(t, a.expect(http.StatusOK, http.MethodPost, "/api/v1/admin/lessons/clone", body, office))

	created, _ := cloned["created"].([]any)
	if len(created) != 1 {
		t.Fatalf("created %v, want the workshop only", cloned["created"])
	}

	cp, _ := created[0].(map[string]any)
	if cp["source"].(map[string]any)["id"] != workshop["id"] || cp["id"] == nil ||
		cp["startsAt"] != "2026-11-03T21:30:00Z" || cp["endsAt"] != "2026-11-03T22:30:00Z" {
		t.Errorf("copy %v", cp)
	}

	if p, _ := preview["created"].([]any); len(p) != 1 || p[0].(map[string]any)["id"] != nil {
		t.Errorf("preview created %v, want one copy without an ID", preview["created"])
	}

	// The class makes its own lesson of the target week.
	skipped, _ := cloned["skipped"].([]any)
	if len(skipped) != 1 || fmt.Sprint(preview["skipped"]) != fmt.Sprint(cloned["skipped"]) {
		t.Fatalf("skipped %v, preview %v", cloned["skipped"], preview["skipped"])
	}

	if m, _ := skipped[0].(map[string]any); m["reason"] != "class-slot" ||
		m["source"].(map[string]any)["classId"] != class["id"] || m["startsAt"] != "2026-11-03T20:15:00Z" ||
		m["lesson"] != nil {
		t.Errorf("skipped %v, want class-slot", m)
	}

	got := decode(t, a.expect(http.StatusOK, http.MethodGet, fmt.Sprintf("/api/v1/lessons/%s", cp["id"]), "", office))
	if got["classId"] != nil || got["kind"] != "workshop" || got["visibility"] != "members" {
		t.Errorf("copy %v", got)
	}

	// Again: the room is taken by the copy.
	again := decode(t, a.expect(http.StatusOK, http.MethodPost, "/api/v1/admin/lessons/clone", body, office))
	if c, _ := again["created"].([]any); len(c) != 0 {
		t.Errorf("second clone created %v", c)
	}

	var taken bool

	for _, s := range again["skipped"].([]any) {
		m, _ := s.(map[string]any)
		if m["source"].(map[string]any)["id"] == workshop["id"] {
			lesson, _ := m["lesson"].(map[string]any)
			taken = m["reason"] == "room-taken" && lesson["id"] == cp["id"] && lesson["copyOf"] == nil
		}
	}

	if !taken {
		t.Errorf("second clone skipped %v, want the workshop as room-taken by %v", again["skipped"], cp["id"])
	}

	// A lesson the class has not generated yet, in January, takes its room.
	overlap := decode(t, a.expect(http.StatusCreated, http.MethodPost, "/api/v1/lessons",
		fmt.Sprintf(`{"courseTypeId":%q,"date":"2026-11-10","startTime":"21:30","durationMinutes":30,"roomId":%q,`+
			`"instructorId":%q,"capacity":5,"visibility":"members","kind":"workshop","confirmRoomOverlap":true}`,
			courseType, room, instructor), office))
	later := decode(t, a.expect(http.StatusOK, http.MethodPost, "/api/v1/admin/lessons/clone/preview",
		`{"unit":"day","source":"2026-11-10","target":"2027-01-12"}`, office))

	var pending map[string]any

	for _, s := range later["skipped"].([]any) {
		if m, _ := s.(map[string]any); m["source"].(map[string]any)["id"] == overlap["id"] && m["reason"] == "room-taken" {
			pending, _ = m["lesson"].(map[string]any)
		}
	}

	if pending == nil || pending["id"] != nil || pending["copyOf"] != nil || pending["classId"] != class["id"] ||
		pending["startsAt"] != "2027-01-12T20:15:00Z" {
		t.Errorf("skipped %v, want the workshop as room-taken by the ungenerated lesson of %v", later["skipped"], class["id"])
	}

	// A lesson of an instructor who left is not copied.
	quinn := decode(t, a.expect(http.StatusCreated, http.MethodPost, "/api/v1/staff",
		`{"displayName":"Quinn Example","kind":"instructor","active":true}`, office))
	early := decode(t, a.expect(http.StatusCreated, http.MethodPost, "/api/v1/lessons",
		fmt.Sprintf(`{"courseTypeId":%q,"date":"2026-11-12","startTime":"07:00","durationMinutes":30,"roomId":%q,`+
			`"instructorId":%q,"capacity":5,"visibility":"members","kind":"workshop"}`, courseType, room, quinn["id"]),
		office))
	a.expect(http.StatusOK, http.MethodPut, fmt.Sprintf("/api/v1/staff/%s", quinn["id"]),
		`{"displayName":"Quinn Example","kind":"instructor","active":false}`, office)

	left := decode(t, a.expect(http.StatusOK, http.MethodPost, "/api/v1/admin/lessons/clone/preview",
		`{"unit":"day","source":"2026-11-12","target":"2026-11-19"}`, office))

	var reason any

	for _, s := range left["skipped"].([]any) {
		if m, _ := s.(map[string]any); m["source"].(map[string]any)["id"] == early["id"] {
			reason = m["reason"]
		}
	}

	if reason != "staff-inactive" {
		t.Errorf("skipped %v, want the lesson of %v as staff-inactive", left["skipped"], quinn["id"])
	}

	for _, tc := range []struct{ body, location string }{
		{`{"unit":"week","source":"2026-12-01","target":"2026-12-07"}`, "body.source"},
		{`{"unit":"week","source":"2026-11-30","target":"2026-12-08"}`, "body.target"},
		{`{"unit":"day","source":"2026-12-01","target":"2026-12-01"}`, "body.target"},
		{`{"unit":"day","source":"2026-10-01","target":"2026-10-22"}`, "body.target"},
		{`{"unit":"month","source":"2026-12-01","target":"2026-12-08"}`, "body.unit"},
	} {
		for _, path := range []string{"/api/v1/admin/lessons/clone/preview", "/api/v1/admin/lessons/clone"} {
			if got := decode(t, a.expect(http.StatusUnprocessableEntity, http.MethodPost, path, tc.body, office)); !strings.Contains(fmt.Sprint(got), tc.location) {
				t.Errorf("%s %s: %v, want 422 on %s", path, tc.body, got, tc.location)
			}
		}
	}
}
