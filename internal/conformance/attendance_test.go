package conformance

import (
	"net/http"
	"slices"
	"testing"
)

func lessonAttendance(t *testing.T, b *backend) {
	t.Helper()

	alina, felix, rosterRequest, trial := checkWorkshopRoster(t, b)
	office, member, visitor := browser(t), browser(t), browser(t)
	login(t, b, office, "olga.ledgerby@example.net", demoPassword)
	login(t, b, member, demoMember, demoPassword)
	rosterResponse := b.call(t, felix, rosterRequest, http.StatusOK)
	roster := read[roster](t, rosterResponse)
	guest := slices.IndexFunc(roster.Bookings, func(p participant) bool { return p.Name == "Mara Gastfield" })
	if guest < 0 {
		t.Fatal("fixture workshop needs a booked guest")
	}

	set := request{
		method: http.MethodPost, path: "/api/v1/bookings/{bookingId}/attendance",
		params: map[string]string{"bookingId": roster.Bookings[guest].BookingID}, body: `{"status":"no_show"}`,
	}
	all := request{method: http.MethodPost, path: "/api/v1/lessons/{lessonId}/attendance/all", params: rosterRequest.params}
	b.call(t, visitor, set, http.StatusUnauthorized)
	b.call(t, member, all, http.StatusForbidden)
	invalid := set
	invalid.body = `{"status":"canceled"}`
	checkLessonField(t, b.call(t, office, invalid, http.StatusUnprocessableEntity), "body.status")
	attendanceInvalid(t, b.call(t, felix, set, http.StatusUnprocessableEntity), "not-started", "path.bookingId")
	attendanceInvalid(t, b.call(t, office, all, http.StatusUnprocessableEntity), "not-started", "path.lessonId")

	lesson := read[struct{ StartsAt string }](t, b.call(t, office, request{
		method: http.MethodGet, path: "/api/v1/lessons/{lessonId}", params: rosterRequest.params,
	}, http.StatusOK))
	b.fixture(t, http.MethodPut, "clock", jsonBody(t, map[string]string{"now": lesson.StartsAt}), http.StatusNoContent)
	login(t, b, alina, "alina.starling@example.net", demoPassword)
	login(t, b, felix, "felix.keysworth@example.net", demoPassword)
	login(t, b, office, "olga.ledgerby@example.net", demoPassword)

	type booking struct {
		ID, Status, GuestName string
		Trial                 bool
	}
	type event struct {
		Status string
		Actor  *struct{ Email string }
	}
	history := request{method: http.MethodGet, path: "/api/v1/admin/bookings/{bookingId}/events", params: set.params}
	before := read[struct{ Items []event }](t, b.call(t, office, history, http.StatusOK)).Items
	for range 2 {
		got := read[booking](t, b.call(t, felix, set, http.StatusOK))
		if got.ID != set.params["bookingId"] || got.Status != "no_show" || got.GuestName != "Mara Gastfield" {
			t.Fatalf("unexpected guest attendance: %+v", got)
		}
	}
	after := read[struct{ Items []event }](t, b.call(t, office, history, http.StatusOK)).Items
	if len(after) != len(before)+1 || after[len(before)].Status != "no_show" || after[len(before)].Actor == nil ||
		after[len(before)].Actor.Email != "felix.keysworth@example.net" {
		t.Fatalf("expected one new attendance event for the accompanist: %+v", after)
	}

	changed := read[struct{ Items []booking }](t, b.call(t, alina, all, http.StatusOK)).Items
	if len(changed) != 2 || !slices.ContainsFunc(changed, func(v booking) bool {
		return v.ID == trial && v.Trial && v.Status == "attended"
	}) || slices.ContainsFunc(changed, func(v booking) bool { return v.ID == set.params["bookingId"] }) {
		t.Fatalf("mark-all touched the wrong bookings: %+v", changed)
	}
	if repeated := read[struct{ Items []booking }](t, b.call(t, alina, all, http.StatusOK)); len(repeated.Items) != 0 {
		t.Fatalf("mark-all changed existing attendance again: %+v", repeated.Items)
	}

	cutoff := read[struct{ DayOverAt string }](t, rosterResponse).DayOverAt
	b.fixture(t, http.MethodPut, "clock", jsonBody(t, map[string]string{"now": cutoff}), http.StatusNoContent)
	set.body = `{"status":"booked"}`
	attendanceInvalid(t, b.call(t, felix, set, http.StatusUnprocessableEntity), "day-over", "path.bookingId")
	if got := read[booking](t, b.call(t, office, set, http.StatusOK)); got.Status != "booked" {
		t.Fatalf("office could not reset attendance after midnight: %+v", got)
	}
}

func attendanceInvalid(t *testing.T, response answer, reason, location string) {
	t.Helper()

	checkLessonField(t, response, location)
	if !containsJSON(response.body, `"reason":"`+reason+`"`) {
		t.Fatalf("expected attendance problem %s: %s", reason, response.body)
	}
}
