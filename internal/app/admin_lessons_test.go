package app_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/app"
)

// TestAdminLessons runs the office's lesson writes over HTTP (ADR 0007):
// one-off lessons, 409 `past` and `room-taken` (with the other lesson) and
// their confirmed retries, cancel and restore with the office's details in
// getLesson, the length of a note, 409 `generated`, and the 422 of a started
// lesson (the mails are tested in package service).
func TestAdminLessons(t *testing.T) {
	t.Parallel()

	a := newAuthApp(t, app.Deps{Demo: true})
	office := a.login("olga.ledgerby@example.net")
	member := a.login("clara.amberly@example.org")

	// A new Wednesday class generates its lessons from next week on; the
	// test clock is Friday 2026-10-23, 16:00 UTC.
	terms := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/terms", "", member))
	termID, _ := terms["items"].([]any)[0].(map[string]any)["id"].(string)
	demo := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/terms/"+termID+"/classes", "", member))
	existing, _ := demo["items"].([]any)[0].(map[string]any)
	courseType, _ := existing["courseTypeId"].(string)
	room, _ := existing["roomId"].(string)
	instructor, _ := existing["instructor"].(map[string]any)["id"].(string)

	class := decode(t, a.expect(http.StatusCreated, http.MethodPost, "/api/v1/terms/"+termID+"/classes",
		fmt.Sprintf(`{"courseTypeId":%q,"roomId":%q,"instructorId":%q,"allLevels":true,"weekday":3,`+
			`"startTime":"21:15","durationMinutes":45,"capacity":6}`, courseType, room, instructor), office))

	schedule := decode(t, a.expect(http.StatusOK, http.MethodGet,
		"/api/v1/schedule?from=2026-10-26&to=2026-11-02", "", office))

	var generated map[string]any

	for _, l := range schedule["lessons"].([]any) {
		if m, _ := l.(map[string]any); m["classId"] == class["id"] {
			generated = m
		}
	}

	if generated == nil {
		t.Fatalf("no lesson of class %v next week", class["id"])
	}

	genID, _ := generated["id"].(string)

	body := func(date, start, extra string) string {
		return fmt.Sprintf(`{"courseTypeId":%q,"date":%q,"startTime":%q,"durationMinutes":60,"roomId":%q,`+
			`"instructorId":%q,"capacity":5,"visibility":"public","kind":"workshop"%s}`,
			courseType, date, start, room, instructor, extra)
	}

	a.expect(http.StatusForbidden, http.MethodPost, "/api/v1/lessons", body("2026-12-01", "10:00", ""), member)

	if past := decode(t, a.expect(http.StatusConflict, http.MethodPost, "/api/v1/lessons",
		body("2026-10-01", "10:00", ""), office)); past["reason"] != "past" {
		t.Errorf("409 %v, want past", past)
	}

	pastLesson := decode(t, a.expect(http.StatusCreated, http.MethodPost, "/api/v1/lessons",
		body("2026-10-01", "10:00", `,"confirmPast":true`), office))

	created := decode(t, a.expect(http.StatusCreated, http.MethodPost, "/api/v1/lessons",
		body("2026-12-01", "10:00", ""), office))
	id, _ := created["id"].(string)

	if created["classId"] != nil || created["kind"] != "workshop" || created["admin"] == nil {
		t.Errorf("created %v", created)
	}

	taken := decode(t, a.expect(http.StatusConflict, http.MethodPost, "/api/v1/lessons",
		body("2026-12-01", "10:30", ""), office))
	if other, _ := taken["lesson"].(map[string]any); taken["reason"] != "room-taken" || other["id"] != id ||
		other["startsAt"] != "2026-12-01T09:00:00Z" || other["courseType"].(map[string]any)["id"] != courseType {
		t.Errorf("409 %v, want room-taken naming %s", taken, id)
	}

	overlap := decode(t, a.expect(http.StatusCreated, http.MethodPost, "/api/v1/lessons",
		body("2026-12-01", "10:30", `,"confirmRoomOverlap":true`), office))

	// Cancel with a note; the office sees it, a member only the status.
	a.expect(http.StatusForbidden, http.MethodPost, "/api/v1/lessons/"+id+"/cancel", `{}`, member)

	canceled := decode(t, a.expect(http.StatusOK, http.MethodPost, "/api/v1/lessons/"+id+"/cancel",
		`{"note":"Studio closed"}`, office))
	admin, _ := canceled["admin"].(map[string]any)

	if canceled["status"] != "canceled" || admin["cancelNote"] != "Studio closed" ||
		admin["canceledAt"] != "2026-10-23T16:00:00Z" || admin["canceledBy"] == nil {
		t.Errorf("canceled %v", canceled)
	}

	if got := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/lessons/"+id, "", office)); got["admin"] == nil {
		t.Errorf("office's lesson lacks admin: %v", got)
	}

	if got := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/lessons/"+id, "", member)); got["admin"] != nil ||
		got["status"] != "canceled" {
		t.Errorf("member's lesson %v", got)
	}

	// The overlapping lesson took the room meanwhile.
	if got := decode(t, a.expect(http.StatusConflict, http.MethodPost, "/api/v1/lessons/"+id+"/restore", "", office)); got["reason"] != "room-taken" ||
		got["lesson"].(map[string]any)["id"] != overlap["id"] {
		t.Errorf("409 %v, want room-taken naming %v", got, overlap["id"])
	}

	restored := decode(t, a.expect(http.StatusOK, http.MethodPost, "/api/v1/lessons/"+id+"/restore",
		`{"confirmRoomOverlap":true}`, office))
	if admin, _ := restored["admin"].(map[string]any); restored["status"] != "scheduled" || admin["cancelNote"] != nil ||
		admin["canceledBy"] != nil {
		t.Errorf("restored %v", restored)
	}

	// The note counts characters after trimming: 500 of them and a trailing
	// newline are fine, 501 are not.
	noted := decode(t, a.expect(http.StatusCreated, http.MethodPost, "/api/v1/lessons",
		body("2026-12-02", "10:00", ""), office))
	notedPath := fmt.Sprintf("/api/v1/lessons/%s/cancel", noted["id"])

	if got := decode(t, a.expect(http.StatusUnprocessableEntity, http.MethodPost, notedPath,
		fmt.Sprintf(`{"note":%q}`, strings.Repeat("ä", 501)), office)); !strings.Contains(fmt.Sprint(got), "body.note") {
		t.Errorf("422 %v, want body.note", got)
	}

	if got := decode(t, a.expect(http.StatusOK, http.MethodPost, notedPath,
		fmt.Sprintf(`{"note":%q}`, strings.Repeat("ä", 500)+"\n"), office)); got["status"] != "canceled" {
		t.Errorf("canceled %v", got)
	}

	// Hide the generated lesson: it is detached and stays at its time.
	hide := fmt.Sprintf(`{"date":"%s","startTime":"%s","durationMinutes":%d,"roomId":%q,"instructorId":%q,`+
		`"capacity":%v,"visibility":"hidden","kind":"regular"}`,
		schoolDate(t, generated["startsAt"]), schoolClock(t, generated["startsAt"]),
		minutes(t, generated["startsAt"], generated["endsAt"]), room, instructor, generated["capacity"])

	if acc, ok := generated["accompanist"].(map[string]any); ok {
		hide = strings.Replace(hide, `"capacity"`, fmt.Sprintf(`"accompanistId":%q,"capacity"`, acc["id"]), 1)
	}

	hidden := decode(t, a.expect(http.StatusOK, http.MethodPut, "/api/v1/lessons/"+genID, hide, office))
	if admin, _ := hidden["admin"].(map[string]any); hidden["visibility"] != "hidden" || admin["detached"] != true ||
		hidden["startsAt"] != generated["startsAt"] {
		t.Errorf("hidden %v", hidden)
	}

	if a.expect(http.StatusNotFound, http.MethodGet, "/api/v1/lessons/"+genID, "", member) == nil {
		t.Error("member sees the hidden lesson")
	}

	if got := decode(t, a.expect(http.StatusConflict, http.MethodDelete, "/api/v1/lessons/"+genID, "", office)); got["reason"] != "generated" {
		t.Errorf("409 %v, want generated", got)
	}

	// A lesson of the past week has started.
	lastWeek := decode(t, a.expect(http.StatusOK, http.MethodGet,
		"/api/v1/schedule?from=2026-09-14&to=2026-10-23", "", office))
	old, _ := lastWeek["lessons"].([]any)[0].(map[string]any)["id"].(string)

	// So has the one-off lesson created in the past.
	for _, started := range []any{old, pastLesson["id"]} {
		got := decode(t, a.expect(http.StatusUnprocessableEntity, http.MethodPost,
			fmt.Sprintf("/api/v1/lessons/%s/cancel", started), `{}`, office))
		if errs, _ := got["errors"].([]any); len(errs) != 1 || errs[0].(map[string]any)["location"] != "path.lessonId" {
			t.Errorf("422 %v, want path.lessonId", got)
		}
	}

	if got := decode(t, a.expect(http.StatusUnprocessableEntity, http.MethodDelete,
		fmt.Sprintf("/api/v1/lessons/%s", pastLesson["id"]), "", office)); !strings.Contains(fmt.Sprint(got), "path.lessonId") {
		t.Errorf("delete a started lesson: %v, want 422 on path.lessonId", got)
	}

	a.expect(http.StatusNoContent, http.MethodDelete, "/api/v1/lessons/"+overlap["id"].(string), "", office)
	a.expect(http.StatusNotFound, http.MethodDelete, "/api/v1/lessons/"+overlap["id"].(string), "", office)
}

func schoolDate(t *testing.T, v any) string {
	t.Helper()

	return berlinTime(t, v).Format("2006-01-02")
}

func schoolClock(t *testing.T, v any) string {
	t.Helper()

	return berlinTime(t, v).Format("15:04")
}

func minutes(t *testing.T, start, end any) int {
	t.Helper()

	return int(berlinTime(t, end).Sub(berlinTime(t, start)).Minutes())
}

// berlinTime parses an instant of a response in the demo's time zone.
func berlinTime(t *testing.T, v any) time.Time {
	t.Helper()

	s, _ := v.(string)

	ts, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}

	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}

	return ts.In(loc)
}

// TestAdminRentalWithoutCourseType runs a room rental without a course type
// over HTTP (ADR 0007, amendment 2026-11-01): created without courseTypeId,
// shown and named in a room-taken conflict without courseType, and kept a
// rental; every other kind still needs one.
func TestAdminRentalWithoutCourseType(t *testing.T) {
	t.Parallel()

	a := newAuthApp(t, app.Deps{Demo: true})
	office := a.login("olga.ledgerby@example.net")

	rooms := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/rooms", "", office))
	room, _ := rooms["items"].([]any)[0].(map[string]any)["id"].(string)

	rental := `{"date":"2026-12-02","startTime":%q,"durationMinutes":60,"roomId":%q,"capacity":0,` +
		`"visibility":"hidden","kind":%q%s}`
	workshop := `{"date":"2026-12-02","startTime":"10:00","durationMinutes":60,"roomId":%q,"capacity":5,` +
		`"visibility":"public","kind":"workshop","confirmRoomOverlap":true}`

	created := decode(t, a.expect(http.StatusCreated, http.MethodPost, "/api/v1/lessons",
		fmt.Sprintf(rental, "10:00", room, "rental", `,"renter":"Juniper Players"`), office))
	id, _ := created["id"].(string)

	if _, ok := created["courseType"]; ok || created["renter"] != "Juniper Players" {
		t.Errorf("created %v, want a rental without courseType", created)
	}

	if got := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/lessons/"+id, "", office)); got["courseType"] != nil {
		t.Errorf("getLesson %v, want no courseType", got)
	}

	// Another rental in the room at that time: the conflict names the first.
	taken := decode(t, a.expect(http.StatusConflict, http.MethodPost, "/api/v1/lessons",
		fmt.Sprintf(rental, "10:30", room, "rental", `,"renter":"Maplewood Choir"`), office))
	if other, _ := taken["lesson"].(map[string]any); taken["reason"] != "room-taken" || other["id"] != id ||
		other["courseType"] != nil {
		t.Errorf("409 %v, want room-taken naming %s without courseType", taken, id)
	}

	// Every other kind needs a course type, and the rental cannot become one.
	for path, method := range map[string]string{"/api/v1/lessons": http.MethodPost, "/api/v1/lessons/" + id: http.MethodPut} {
		got := decode(t, a.expect(http.StatusUnprocessableEntity, method, path,
			fmt.Sprintf(workshop, room), office))

		want := "body.courseTypeId"
		if method == http.MethodPut {
			want = "body.kind"
		}

		if errs, _ := got["errors"].([]any); len(errs) == 0 || errs[0].(map[string]any)["location"] != want {
			t.Errorf("%s %s: 422 %v, want %s", method, path, got, want)
		}
	}
}
