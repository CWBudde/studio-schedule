package conformance

import (
	"net/http"
	"strings"
	"testing"
)

func lessonEditingAndRestoration(t *testing.T, b *backend) {
	t.Helper()

	office, member, visitor := browser(t), browser(t), browser(t)
	login(t, b, office, "olga.ledgerby@example.net", demoPassword)
	login(t, b, member, demoMember, demoPassword)
	data := lessonFixture(t, b, office, "workshop", 1200)
	const events = "/api/v1/admin/events"
	newEvent := read[struct{ ID, Slug string }](t, b.call(t, office, request{
		method: http.MethodPost, path: events,
		body: jsonBody(t, map[string]any{
			"title": "Lesson editing fixture", "courseTypeId": data["courseTypeId"],
			"capacity": 1200, "price": 0, "vatRate": 0,
		}),
	}, http.StatusCreated))
	event := request{method: http.MethodPost, path: events + "/{eventId}", params: map[string]string{"eventId": newEvent.ID}}
	session := event
	session.path += "/sessions"
	session.body = jsonBody(t, map[string]any{
		"date": data["date"], "startTime": data["startTime"], "durationMinutes": data["durationMinutes"],
		"roomId": data["roomId"], "instructorId": data["instructorId"], "confirmRoomOverlap": true,
	})
	created := read[struct{ ID string }](t, b.call(t, office, session, http.StatusCreated))
	publish := event
	publish.path += "/publish"
	b.call(t, office, publish, http.StatusOK)
	desk := event
	desk.path += "/registrations"
	desk.body = `{"guest":{"name":"Restoration Guest","email":"restoration-guest@example.test"}}`
	b.call(t, office, desk, http.StatusCreated)

	delete(data, "courseTypeId")
	delete(data, "confirmRoomOverlap")
	edit := request{method: http.MethodPut, path: "/api/v1/lessons/{lessonId}", params: map[string]string{"lessonId": created.ID}}
	edit.body = jsonBody(t, data)
	restore := edit
	restore.method, restore.path, restore.body = http.MethodPost, edit.path+"/restore", ""
	b.call(t, visitor, edit, http.StatusUnauthorized)
	b.call(t, member, restore, http.StatusForbidden)

	update := func(status int) answer {
		t.Helper()
		edit.body = jsonBody(t, data)

		return b.call(t, office, edit, status)
	}
	data["capacity"] = 1199
	checkLessonField(t, update(http.StatusUnprocessableEntity), "body.capacity")
	data["capacity"], data["visibility"] = 1200, "members"
	checkLessonField(t, update(http.StatusUnprocessableEntity), "body.visibility")
	data["visibility"], data["kind"] = "public", "private"
	checkLessonField(t, update(http.StatusUnprocessableEntity), "body.kind")
	data["kind"], data["date"] = "workshop", "2026-10-22"
	edit.body = jsonBody(t, data)
	expectConflict(t, b, office, edit, "past")

	blocker := lessonFixture(t, b, office, "workshop", 4)
	blocker["date"] = "2026-10-26"
	blocking := read[struct{ ID string }](t, b.call(t, office, request{
		method: http.MethodPost, path: "/api/v1/lessons", body: jsonBody(t, blocker),
	}, http.StatusCreated))
	data["date"] = blocker["date"]
	if failed := update(http.StatusConflict); !containsJSON(failed.body, `"reason":"room-taken"`) ||
		!containsJSON(failed.body, `"id":"`+blocking.ID+`"`) {
		t.Fatalf("overlapping move did not name the blocker: %s", failed.body)
	}
	data["confirmRoomOverlap"] = true
	type lesson struct {
		Status, EventID, StartsAt string
		Capacity                  int
	}
	moved := read[lesson](t, update(http.StatusOK))
	if moved.Capacity != 1200 || moved.EventID != newEvent.ID || moved.StartsAt != "2026-10-26T07:00:00Z" {
		t.Fatalf("event session after move: %+v", moved)
	}
	cancel := restore
	cancel.path, cancel.body = edit.path+"/cancel", `{"note":"Fixture closure"}`
	b.call(t, office, cancel, http.StatusOK)
	expectConflict(t, b, office, restore, "room-taken")
	restore.body = `{"confirmRoomOverlap":true}`
	result := b.call(t, office, restore, http.StatusOK)
	if got := read[lesson](t, result); got.Status != "scheduled" || got.StartsAt != moved.StartsAt ||
		got.EventID != newEvent.ID || got.Capacity != 1200 || containsJSON(result.body, `"cancelNote":`) ||
		containsJSON(result.body, `"canceledAt":`) || containsJSON(result.body, `"canceledBy":`) {
		t.Fatalf("restored session: %s", result.body)
	}
	restore.body = "null"
	b.call(t, office, restore, http.StatusUnprocessableEntity)
	restore.body = ""
	b.call(t, office, restore, http.StatusOK)

	cancelEvent := event
	cancelEvent.path, cancelEvent.body = event.path+"/cancel", `{}`
	b.call(t, office, cancelEvent, http.StatusOK)
	expectConflict(t, b, office, restore, "closed")
	data["date"] = "2026-10-27"
	if got := read[lesson](t, update(http.StatusOK)); got.Status != "canceled" {
		t.Fatalf("editing a canceled session changed its status: %+v", got)
	}

	outbox := read[struct {
		Mails []struct{ To, Text, Template string }
	}](t, b.fixture(t, http.MethodGet, "outbox", "", http.StatusOK))
	notices := map[string]int{}
	for _, mail := range outbox.Mails {
		if mail.To == "restoration-guest@example.test" && (mail.Template == "lesson-changed" || mail.Template == "lesson-restored") {
			if !strings.Contains(mail.Text, "Restoration Guest") {
				t.Errorf("the guest's name is missing from %s", mail.Template)
			}
			notices[mail.Template]++
		}
	}
	if notices["lesson-changed"] != 1 || notices["lesson-restored"] != 1 {
		t.Errorf("expected one change and one restoration notice: %v", notices)
	}
}

func checkLessonField(t *testing.T, response answer, location string) {
	t.Helper()

	if !containsJSON(response.body, `"location":"`+location+`"`) {
		t.Fatalf("expected field %s in %s", location, response.body)
	}
}
