package conformance

import (
	"net/http"
	"testing"
)

func eventSessionSignupGuards(t *testing.T, b *backend) {
	t.Helper()

	visitor, member, unrelated := browser(t), browser(t), browser(t)
	office, instructor, otherStaff := browser(t), browser(t), browser(t)
	login(t, b, member, "clara.amberly@example.org", demoPassword)
	login(t, b, unrelated, demoMember, demoPassword)
	login(t, b, office, "olga.ledgerby@example.net", demoPassword)
	login(t, b, instructor, "alina.starling@example.net", demoPassword)
	login(t, b, otherStaff, "felix.keysworth@example.net", demoPassword)

	type family struct {
		Me         struct{ ID string }
		Dependents []struct{ ID string }
	}
	kin := read[family](t, get(t, b, member, "/api/v1/family"))
	foreign := read[family](t, get(t, b, unrelated, "/api/v1/family")).Me.ID
	if len(kin.Dependents) == 0 {
		t.Fatal("fixture member needs a dependent")
	}
	people := read[struct {
		Items []struct{ ID, DisplayName string }
	}](t, get(t, b, office, "/api/v1/staff"))
	instructorID := ""
	for _, person := range people.Items {
		if person.DisplayName == "Alina Starling" {
			instructorID = person.ID
		}
	}
	if instructorID == "" {
		t.Fatal("fixture staff list is missing Alina")
	}

	fixture := lessonFixture(t, b, office, "workshop", 2)
	const path = "/api/v1/admin/events"
	event := read[struct{ ID string }](t, b.call(t, office, request{
		method: http.MethodPost, path: path,
		body: jsonBody(t, map[string]any{
			"title": "Session signup guard fixture", "courseTypeId": fixture["courseTypeId"],
			"capacity": 2, "price": 0, "vatRate": 0,
		}),
	}, http.StatusCreated))
	eventRef := request{method: http.MethodPost, path: path + "/{eventId}", params: map[string]string{"eventId": event.ID}}
	addSession := eventRef
	addSession.path += "/sessions"
	addSession.body = jsonBody(t, map[string]any{
		"date": "2026-10-25", "startTime": "08:00", "durationMinutes": 60,
		"roomId": fixture["roomId"], "instructorId": instructorID, "confirmRoomOverlap": true,
	})
	session := read[struct{ ID string }](t, b.call(t, office, addSession, http.StatusCreated))
	lesson := request{method: http.MethodPost, path: "/api/v1/lessons/{lessonId}", params: map[string]string{"lessonId": session.ID}}
	book := lesson
	book.path += "/bookings"
	book.body = jsonBody(t, map[string]any{"personId": kin.Me.ID})
	join := lesson
	join.path += "/waitlist"
	join.body = jsonBody(t, map[string]any{"personId": kin.Dependents[0].ID, "stayOnList": true})
	add := lesson
	add.path += "/participants"
	add.body = `{"guest":{"name":"Fixture walk-in"},"confirmOverCapacity":true}`
	b.call(t, visitor, book, http.StatusUnauthorized)
	b.call(t, visitor, join, http.StatusUnauthorized)
	b.call(t, visitor, add, http.StatusUnauthorized)
	b.call(t, member, add, http.StatusForbidden)
	b.call(t, member, book, http.StatusNotFound)
	b.call(t, member, join, http.StatusNotFound)
	invalid := book
	invalid.params = map[string]string{"lessonId": "invalid"}
	b.call(t, member, invalid, http.StatusUnprocessableEntity)
	invalid.body = `{`
	b.call(t, member, invalid, http.StatusBadRequest)
	b.call(t, otherStaff, add, http.StatusNotFound)
	expectConflict(t, b, office, add, "event-session")
	expectConflict(t, b, instructor, add, "event-session")
	domainInvalid := add
	domainInvalid.body = `{"guest":{"name":"Fixture\u0000 walk-in"},"trialNote":"Fixture\u0000 note"}`
	expectConflict(t, b, instructor, domainInvalid, "event-session")

	publish := eventRef
	publish.path += "/publish"
	b.call(t, office, publish, http.StatusOK)
	expectConflict(t, b, member, book, "event-session")
	expectConflict(t, b, member, join, "event-session")
	book.body = jsonBody(t, map[string]any{"personId": foreign})
	b.call(t, member, book, http.StatusNotFound)
	add.body = `{"personId":"018f0000-0000-7000-8000-000000000001"}`
	if got := b.call(t, office, add, http.StatusUnprocessableEntity); !containsJSON(got.body, `"location":"body.personId"`) {
		t.Fatalf("an unknown person on a managed lesson: %s", got.body)
	}

	cancel := eventRef
	cancel.path += "/cancel"
	cancel.body = `{}`
	b.call(t, office, cancel, http.StatusOK)
	expectConflict(t, b, member, join, "event-session")
}
