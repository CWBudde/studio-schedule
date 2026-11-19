package conformance

import (
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"testing"
)

func classCreationAndDeletion(t *testing.T, b *backend) {
	t.Helper()

	guest, office, member := browser(t), browser(t), browser(t)
	timetable := readSeededTimetable(t, b, guest)
	source := timetable.classes[slices.IndexFunc(timetable.classes, func(c listedClass) bool { return c.ID == timetable.balletID })]
	const path = "/api/v1/terms/{termId}/classes"
	data := fmt.Sprintf(`{"courseTypeId":%q,"roomId":%q,"instructorId":%q,"weekday":1,"startTime":"14:15","durationMinutes":50,"capacity":8,"allLevels":true,"stage":2}`,
		source.CourseTypeID, source.RoomID, source.Instructor.ID)
	create := request{method: http.MethodPost, path: path, params: map[string]string{"termId": timetable.termID}, body: data}
	b.call(t, guest, create, http.StatusUnauthorized)
	login(t, b, member, demoMember, demoPassword)
	b.call(t, member, create, http.StatusForbidden)
	login(t, b, office, "olga.ledgerby@example.net", demoPassword)
	invalid := create
	invalid.body = fmt.Sprintf(`{"courseTypeId":%q,"roomId":"00000000-0000-0000-0000-000000000001","instructorId":%q,"weekday":1,"startTime":"14:15","durationMinutes":50,"capacity":8}`,
		source.CourseTypeID, source.Instructor.ID)
	if body := b.call(t, office, invalid, http.StatusUnprocessableEntity).body; !containsJSON(body, `"location":"body.roomId"`) {
		t.Fatal("missing room was not reported on roomId")
	}

	type createdClass struct {
		listedClass
		AllLevels                        bool
		Stage, DurationMinutes, Capacity int
	}
	class := read[createdClass](t, b.call(t, office, create, http.StatusCreated))
	if class.ID == "" || class.TermID != timetable.termID || class.StartTime != "14:15" || class.Weekday != 1 ||
		class.Instructor != source.Instructor || class.Retired == nil || *class.Retired ||
		!class.AllLevels || class.Stage != 2 || class.DurationMinutes != 50 || class.Capacity != 8 {
		t.Fatalf("unexpected class: %+v", class)
	}

	schedule := func() bool {
		t.Helper()
		lessons := read[struct{ Lessons []struct{ ClassID string } }](t, b.call(t, office, request{
			method: http.MethodGet, path: "/api/v1/schedule", query: url.Values{"from": {"2026-10-26"}, "to": {"2026-10-27"}},
		}, http.StatusOK)).Lessons
		return slices.ContainsFunc(lessons, func(lesson struct{ ClassID string }) bool { return lesson.ClassID == class.ID })
	}
	if !schedule() {
		t.Fatal("the class was created without a lesson in the schedule")
	}

	remove := request{method: http.MethodDelete, path: "/api/v1/classes/{classId}", params: map[string]string{"classId": timetable.balletID}}
	expectConflict(t, b, office, remove, "in-use")
	remove.params["classId"] = class.ID
	b.call(t, guest, remove, http.StatusUnauthorized)
	b.call(t, member, remove, http.StatusForbidden)
	b.call(t, office, remove, http.StatusNoContent)
	b.call(t, office, remove, http.StatusNotFound)
	if schedule() {
		t.Error("deleting the class left its lesson in the schedule")
	}
}
