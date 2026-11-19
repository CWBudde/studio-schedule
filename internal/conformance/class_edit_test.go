package conformance

import (
	"net/http"
	"net/url"
	"slices"
	"testing"
)

type editedClassLesson struct {
	ID, ClassID, StartsAt, EndsAt, Status string
	Capacity                              int
}

func classEditingAndRetirement(t *testing.T, b *backend) {
	t.Helper()

	guest, office, member := browser(t), browser(t), browser(t)
	timetable := readSeededTimetable(t, b, guest)
	source := timetable.classes[slices.IndexFunc(timetable.classes, func(c listedClass) bool { return c.ID == timetable.balletID })]
	login(t, b, office, "olga.ledgerby@example.net", demoPassword)
	login(t, b, member, demoMember, demoPassword)
	data := map[string]any{
		"courseTypeId": source.CourseTypeID, "roomId": source.RoomID, "instructorId": source.Instructor.ID,
		"weekday": 1, "startTime": "14:15", "durationMinutes": 50, "capacity": 8,
	}
	created := read[listedClass](t, b.call(t, office, request{
		method: http.MethodPost, path: "/api/v1/terms/{termId}/classes",
		params: map[string]string{"termId": timetable.termID}, body: jsonBody(t, data),
	}, http.StatusCreated))
	original := classEditLesson(t, b, office, created.ID, "2026-10-26", "2026-10-27")
	detached := classEditLesson(t, b, office, created.ID, "2026-11-02", "2026-11-03")
	const lessonPath = "/api/v1/lessons/{lessonId}"
	b.call(t, office, request{
		method: http.MethodPost, path: lessonPath + "/cancel", body: `{"note":"Instructor away"}`,
		params: map[string]string{"lessonId": detached.ID},
	}, http.StatusOK)

	data["weekday"], data["startTime"], data["durationMinutes"], data["capacity"] = 2, "15:30", 60, 10
	edit := request{
		method: http.MethodPut, path: "/api/v1/classes/{classId}",
		params: map[string]string{"classId": created.ID}, body: jsonBody(t, data),
	}
	b.call(t, guest, edit, http.StatusUnauthorized)
	b.call(t, member, edit, http.StatusForbidden)
	updated := read[listedClass](t, b.call(t, office, edit, http.StatusOK))
	if updated.ID != created.ID || updated.TermID != created.TermID || updated.Weekday != 2 || updated.StartTime != "15:30" {
		t.Fatalf("class fields not updated: %+v", updated)
	}
	moved := read[editedClassLesson](t, b.call(t, office, request{
		method: http.MethodGet, path: lessonPath, params: map[string]string{"lessonId": original.ID},
	}, http.StatusOK))
	if moved.StartsAt != "2026-10-27T14:30:00Z" || moved.EndsAt != "2026-10-27T15:30:00Z" || moved.Capacity != 10 {
		t.Fatalf("generated lesson did not follow its class: %+v", moved)
	}

	data["retired"] = true
	edit.body = jsonBody(t, data)
	retired := read[listedClass](t, b.call(t, office, edit, http.StatusOK))
	if retired.Retired == nil || !*retired.Retired {
		t.Fatalf("class did not retire: %+v", retired)
	}
	b.call(t, office, request{
		method: http.MethodGet, path: lessonPath, params: map[string]string{"lessonId": original.ID},
	}, http.StatusNotFound)
	kept := classEditLesson(t, b, office, created.ID, "2026-11-02", "2026-11-04")
	if kept.ID != detached.ID || kept.StartsAt != detached.StartsAt || kept.Status != "canceled" {
		t.Fatalf("retirement changed the detached lesson: %+v", kept)
	}
}

func classEditLesson(t *testing.T, b *backend, office *http.Client, class, from, to string) editedClassLesson {
	t.Helper()

	lessons := read[struct{ Lessons []editedClassLesson }](t, b.call(t, office, request{
		method: http.MethodGet, path: "/api/v1/schedule", query: url.Values{"from": {from}, "to": {to}},
	}, http.StatusOK)).Lessons
	lessons = slices.DeleteFunc(lessons, func(lesson editedClassLesson) bool { return lesson.ClassID != class })
	if len(lessons) != 1 {
		t.Fatalf("wanted one lesson for the class from %s to %s, got %d", from, to, len(lessons))
	}

	return lessons[0]
}
