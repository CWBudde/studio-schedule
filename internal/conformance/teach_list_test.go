package conformance

import (
	"net/http"
	"net/url"
	"slices"
	"testing"
)

type teachLesson struct {
	ID, Role, Kind, Status, EventSlug, DayOverAt string
	Instructor, Accompanist                      *struct{ ID string }
	Active, Held, FreePlaces, Waiting            int
	Trials, Guests                               int
	AttendanceOpen, AttendanceTaken              bool
}

type teachList struct {
	Timezone, Now, From, To string
	Lessons                 []teachLesson
}

func teachingSchedule(t *testing.T, b *backend) {
	t.Helper()

	office, alina, felix, member := browser(t), browser(t), browser(t), browser(t)
	login(t, b, office, "olga.ledgerby@example.net", demoPassword)
	login(t, b, alina, "alina.starling@example.net", demoPassword)
	login(t, b, felix, "felix.keysworth@example.net", demoPassword)
	login(t, b, member, demoMember, demoPassword)

	in := request{method: http.MethodGet, path: "/api/v1/teach"}
	b.call(t, browser(t), in, http.StatusUnauthorized)
	b.call(t, member, in, http.StatusForbidden)
	if got := read[teachList](t, b.call(t, office, in, http.StatusOK)); got.Timezone != "Europe/Berlin" ||
		got.Now != demoNow || got.From != "2026-10-16" || got.To != "2026-10-31" || len(got.Lessons) == 0 {
		t.Fatalf("unexpected default teaching date range: %+v", got)
	}
	bad := in
	bad.query = url.Values{"from": {"invalid"}, "staffId": {"invalid"}}
	b.call(t, office, bad, http.StatusUnprocessableEntity)
	bad.query = url.Values{"from": {"2026-11-17"}, "to": {"2026-11-17"}}
	b.call(t, office, bad, http.StatusUnprocessableEntity)
	bad.query.Set("to", "2027-01-01")
	b.call(t, office, bad, http.StatusUnprocessableEntity)

	lesson := lessonOn(t, b, "2026-11-16", "2026-11-17", "Classical Ballet")
	room := lessonFixture(t, b, office, "workshop", 4)["roomId"]
	rental := read[struct{ ID string }](t, b.call(t, office, request{
		method: http.MethodPost, path: "/api/v1/lessons", body: jsonBody(t, map[string]any{
			"date": "2026-11-16", "startTime": "00:00", "durationMinutes": 60, "roomId": room,
			"kind": "rental", "capacity": 0, "visibility": "hidden", "renter": "Fixture group", "confirmRoomOverlap": true,
		}),
	}, http.StatusCreated))

	in.query = url.Values{"from": {"2026-11-16"}, "to": {"2026-11-17"}}
	lookup := func(client *http.Client, role string) teachLesson {
		t.Helper()
		list := read[teachList](t, b.call(t, client, in, http.StatusOK))
		if slices.ContainsFunc(list.Lessons, func(l teachLesson) bool { return l.ID == rental.ID }) {
			t.Fatal("the teaching schedule includes a room rental")
		}
		at := slices.IndexFunc(list.Lessons, func(l teachLesson) bool { return l.ID == lesson })
		if at < 0 {
			t.Fatalf("workshop missing for %s", role)
		}
		l := list.Lessons[at]
		if l.Role != role || l.Active != 3 || l.FreePlaces != 13 || l.Trials != 1 || l.Guests != 2 ||
			l.Held != 0 || l.Waiting != 0 || l.DayOverAt != "2026-11-16T23:00:00Z" {
			t.Fatalf("unexpected workshop for %s: %+v", role, l)
		}

		return l
	}
	workshop := lookup(alina, "instructor")
	lookup(felix, "accompanist")
	lookup(office, "office")
	if workshop.AttendanceOpen || workshop.AttendanceTaken || workshop.Accompanist == nil {
		t.Fatalf("unexpected attendance state before the workshop: %+v", workshop)
	}
	in.query.Set("staffId", workshop.Accompanist.ID)
	if got := read[teachList](t, b.call(t, alina, in, http.StatusOK)); len(got.Lessons) != 0 {
		t.Fatalf("an instructor read another staff member's lessons: %+v", got.Lessons)
	}
	lookup(office, "office")
	in.query.Del("staffId")

	draft := in
	draft.query = url.Values{"from": {"2026-11-08"}, "to": {"2026-11-22"}}
	list := read[teachList](t, b.call(t, alina, draft, http.StatusOK))
	if !slices.ContainsFunc(list.Lessons, func(l teachLesson) bool { return l.EventSlug == "summer-showcase-rehearsal" }) {
		t.Fatal("the linked instructor cannot see the draft event's session")
	}

	b.fixture(t, http.MethodPut, "clock", `{"now":"2026-11-16T23:00:00Z"}`, http.StatusNoContent)
	login(t, b, alina, "alina.starling@example.net", demoPassword)
	login(t, b, office, "olga.ledgerby@example.net", demoPassword)
	if lookup(alina, "instructor").AttendanceOpen || !lookup(office, "office").AttendanceOpen {
		t.Fatal("after school midnight only the office can take attendance")
	}
}
