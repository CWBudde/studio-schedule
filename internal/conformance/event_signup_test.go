package conformance

import (
	"net/http"
	"strings"
	"testing"
)

func eventSignup(t *testing.T, b *backend) {
	t.Helper()

	guest, member, office, outsider := browser(t), browser(t), browser(t), browser(t)
	const memberEmail, officeEmail = "clara.amberly@example.org", "olga.ledgerby@example.net"
	login(t, b, member, memberEmail, demoPassword)
	login(t, b, office, officeEmail, demoPassword)
	login(t, b, outsider, demoMember, demoPassword)
	type family struct {
		Me         struct{ ID string }
		Dependents []struct{ ID string }
	}
	kin := read[family](t, get(t, b, member, "/api/v1/family"))
	other := read[family](t, get(t, b, outsider, "/api/v1/family")).Me.ID

	if len(kin.Dependents) < 2 {
		t.Fatal("fixture needs two dependents")
	}
	child := kin.Dependents[0].ID
	person := func(id string) string { return jsonBody(t, map[string]any{"personId": id}) }

	fixture := lessonFixture(t, b, office, "workshop", 1)
	const admin = "/api/v1/admin/events"
	created := read[struct{ ID, Slug string }](t, b.call(t, office, request{
		method: http.MethodPost, path: admin,
		body: jsonBody(t, map[string]any{
			"title": "Small event signup fixture", "courseTypeId": fixture["courseTypeId"],
			"capacity": 1, "price": 2250, "vatRate": 1900,
		}),
	}, http.StatusCreated))
	edit := request{method: http.MethodPost, path: admin + "/{eventId}", params: map[string]string{"eventId": created.ID}}
	join := request{
		method: http.MethodPost, path: "/api/v1/events/{slug}/waitlist",
		params: map[string]string{"slug": created.Slug}, body: person(child),
	}
	signup := join
	signup.path, signup.body = "/api/v1/events/{slug}/registrations", person(kin.Me.ID)
	b.call(t, guest, signup, http.StatusUnauthorized)
	b.call(t, member, signup, http.StatusNotFound)

	session := edit
	session.path += "/sessions"
	session.body = jsonBody(t, map[string]any{
		"date": "2026-10-25", "startTime": "08:00", "durationMinutes": 60,
		"roomId": fixture["roomId"], "instructorId": fixture["instructorId"], "confirmRoomOverlap": true,
	})
	b.call(t, office, session, http.StatusCreated)
	publish := edit
	publish.path += "/publish"
	b.call(t, office, publish, http.StatusOK)
	expectConflict(t, b, member, join, "not-full")
	foreign := signup
	foreign.body = person(other)
	b.call(t, member, foreign, http.StatusNotFound)

	type result struct {
		Event        eventSummary
		Registration struct {
			ID, Status, Payment string
			Price               int
			Cancelable          bool
			Person              struct{ ID string }
		}
	}
	booked := read[result](t, b.call(t, member, signup, http.StatusCreated))
	if booked.Event.FreePlaces != 0 || booked.Registration.Status != "registered" || booked.Registration.Person.ID != kin.Me.ID ||
		booked.Registration.Price != 2250 || booked.Registration.Payment != "unpaid" || !booked.Registration.Cancelable {
		t.Fatalf("member signup: %+v", booked)
	}
	signup.body = person(child)
	expectConflict(t, b, member, signup, "full")
	enqueued := read[result](t, b.call(t, member, join, http.StatusCreated))
	if enqueued.Registration.Status != "waiting" || enqueued.Registration.Person.ID != child || enqueued.Registration.Cancelable {
		t.Fatalf("waiting registration: %+v", enqueued)
	}
	expectConflict(t, b, member, join, "duplicate")
	change := request{
		method: http.MethodPost, path: "/api/v1/event-registrations/{registrationId}/accept",
		params: map[string]string{"registrationId": enqueued.Registration.ID},
	}
	b.call(t, outsider, change, http.StatusNotFound)
	expectConflict(t, b, member, change, "not-invited")
	change.path = "/api/v1/event-registrations/{registrationId}/leave"
	out := read[result](t, b.call(t, member, change, http.StatusOK))
	if out.Registration.Status != "canceled" || out.Event.FreePlaces != 0 {
		t.Fatalf("left waiting list: %+v", out)
	}
	expectConflict(t, b, member, change, "not-waiting")

	desk := edit
	desk.path += "/registrations"
	desk.body = `{"guest":{"name":" Desk guest ","email":"guest@example.test"}}`
	b.call(t, member, desk, http.StatusForbidden)
	expectConflict(t, b, office, desk, "full")
	desk.body = `{"guest":{"name":" Desk guest ","email":"guest@example.test"},"confirmOverCapacity":true}`
	added := b.call(t, office, desk, http.StatusCreated)
	guestID := read[struct{ ID string }](t, added).ID
	if !containsJSON(added.body, `"name":"Desk guest"`) || !containsJSON(added.body, `"price":2250`) ||
		!containsJSON(added.body, `"payment":"unpaid"`) {
		t.Fatalf("office guest signup: %s", added.body)
	}

	free := request{
		method: http.MethodPost, path: "/api/v1/events/{slug}/registrations",
		params: map[string]string{"slug": "open-ballet-taster"}, body: person(kin.Dependents[1].ID),
	}
	if added := read[result](t, b.call(t, member, free, http.StatusCreated)); added.Registration.Payment != "paid" || added.Registration.Price != 0 {
		t.Fatalf("free event signup: %+v", added)
	}
	expectConflict(t, b, member, free, "duplicate")

	outbox := read[struct {
		Mails []struct{ To, Text, Template string }
	}](t, b.fixture(t, http.MethodGet, "outbox", "", http.StatusOK))
	sent := false
	for _, mail := range outbox.Mails {
		if mail.To == memberEmail && mail.Template == "event-registered" && strings.Contains(mail.Text, "Small event signup fixture") {
			sent = true

			break
		}
	}
	if !sent {
		t.Error("signup did not queue a confirmation for the registering member")
	}

	adminClient := browser(t)
	login(t, b, adminClient, demoAdmin, demoPassword)
	type auditEvent struct {
		Action       string
		Actor        *struct{ Email string }
		TargetPerson *struct{ ID string }
		Details      struct{ EventID, RegistrationID string }
	}
	type auditEntry struct{ id, action, email, person string }
	expected := []auditEntry{
		{booked.Registration.ID, "event-registration-added", memberEmail, kin.Me.ID},
		{enqueued.Registration.ID, "event-registration-added", memberEmail, child},
		{enqueued.Registration.ID, "event-registration-canceled", memberEmail, child},
		{guestID, "event-registration-added", officeEmail, ""},
	}
	page := read[struct{ Items []auditEvent }](t, get(t, b, adminClient, "/api/v1/admin/audit"))
	for _, want := range expected {
		found := false
		for _, item := range page.Items {
			if item.Details.EventID != created.ID || item.Details.RegistrationID != want.id || item.Action != want.action {
				continue
			}
			personID := ""
			if item.TargetPerson != nil {
				personID = item.TargetPerson.ID
			}
			found = item.Actor != nil && item.Actor.Email == want.email && personID == want.person

			break
		}
		if !found {
			t.Errorf("missing audit action %s for registration %s", want.action, want.id)
		}
	}
}
