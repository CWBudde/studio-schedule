package conformance

import (
	"net/http"
	"net/url"
	"slices"
	"testing"
)

type participant struct {
	BookingID, PersonID, Name, Status, Source, ContactEmail, TrialNote string
	Guest, Trial                                                       bool
}

type roster struct {
	Capacity, Active, Held, Waiting, FreePlaces int
	Bookings                                    []participant
	Waitlist                                    []struct {
		Name, Status, ExpiresAt string
		Position                int
	}
}

func lessonOn(t *testing.T, b *backend, date, nextDate, course string) string {
	t.Helper()

	schedule := read[struct {
		Lessons []struct {
			ID, Kind   string
			CourseType struct{ Name string }
		}
	}](t, b.call(t, browser(t), request{
		method: http.MethodGet, path: "/api/v1/schedule",
		query: url.Values{"from": {date}, "to": {nextDate}},
	}, http.StatusOK))
	for _, lesson := range schedule.Lessons {
		if lesson.Kind == "workshop" && lesson.CourseType.Name == course {
			return lesson.ID
		}
	}

	t.Fatalf("no %s workshop on %s", course, date)
	return ""
}

func lessonWaitlist(t *testing.T, b *backend) {
	t.Helper()

	const vera = "Vera Rushwick"
	const wanda = "Wanda Sparrowhill"
	office := browser(t)
	irene := browser(t)
	clara := browser(t)
	login(t, b, office, "olga.ledgerby@example.net", demoPassword)
	login(t, b, irene, "irene.inkwell@example.com", demoPassword)
	login(t, b, clara, "clara.amberly@example.org", demoPassword)

	lesson := lessonOn(t, b, "2026-11-02", "2026-11-03", "Tap")
	params := map[string]string{"lessonId": lesson}
	list := request{method: http.MethodGet, path: "/api/v1/lessons/{lessonId}/participants", params: params}
	before := read[roster](t, b.call(t, office, list, http.StatusOK))
	if before.Capacity != 4 || before.Active != 4 || before.Held != 0 || before.Waiting != 2 || before.FreePlaces != 0 ||
		len(before.Waitlist) != 2 || before.Waitlist[0].Name != vera || before.Waitlist[1].Name != wanda {
		t.Fatalf("unexpected seeded workshop roster: %+v", before)
	}
	i := slices.IndexFunc(before.Bookings, func(v participant) bool { return v.Name == "Irene Inkwell" })
	if i < 0 {
		t.Fatal("Irene has no seeded booking")
	}
	booking := before.Bookings[i]

	family := read[struct {
		Dependents []struct{ ID, GivenName string }
	}](t, get(t, b, clara, "/api/v1/family"))
	ella := ""
	for _, child := range family.Dependents {
		if child.GivenName == "Ella" {
			ella = child.ID
		}
	}
	if ella == "" {
		t.Fatal("Ella is missing from the seeded family")
	}
	join := request{
		method: http.MethodPost, path: "/api/v1/lessons/{lessonId}/waitlist", params: params,
		body: `{"personId":"` + ella + `","stayOnList":true}`,
	}
	entry := read[struct {
		LessonID, PersonID, Status string
		Position                   int
		StayOnList                 bool
	}](t, b.call(t, clara, join, http.StatusCreated))
	if entry.LessonID != lesson || entry.PersonID != ella || entry.Status != "waiting" || entry.Position != 3 || !entry.StayOnList {
		t.Fatalf("unexpected waiting-list entry: %+v", entry)
	}
	expectConflict(t, b, clara, join, "already-waiting")

	b.call(t, irene, request{
		method: http.MethodPost, path: "/api/v1/bookings/{bookingId}/cancel",
		params: map[string]string{"bookingId": booking.BookingID},
	}, http.StatusOK)
	held := read[roster](t, b.call(t, office, list, http.StatusOK))
	if held.Active != 3 || held.Held != 1 || held.Waiting != 2 || held.FreePlaces != 0 ||
		len(held.Waitlist) != 3 || held.Waitlist[0].Name != vera || held.Waitlist[0].Status != "invited" ||
		held.Waitlist[0].ExpiresAt != "2026-10-24T04:00:00Z" || held.Waitlist[1].Name != wanda ||
		held.Waitlist[1].Position != 1 || held.Waitlist[2].Position != 2 {
		t.Fatalf("a cancellation did not invite the first waiting person: %+v", held)
	}
	expectConflict(t, b, irene, request{
		method: http.MethodPost, path: "/api/v1/lessons/{lessonId}/bookings",
		params: params, body: `{"personId":"` + booking.PersonID + `"}`,
	}, "full")

	token := mailToken(t, b, "vera.rushwick@example.com", "waitlist-invite", "/waitlist/accept")
	invite := request{
		method: http.MethodGet, path: "/api/v1/waitlist-invites/{token}",
		params: map[string]string{"token": token},
	}
	guest := browser(t)
	type inviteData struct {
		State, GivenName string
		Lesson           struct{ ID string }
	}
	if r := read[inviteData](t, b.call(t, guest, invite, http.StatusOK)); r.State != "invited" ||
		r.GivenName != "Vera" || r.Lesson.ID != lesson {
		t.Fatalf("unexpected invitation: %+v", r)
	}
	invite.method = http.MethodPost
	if accepted := read[inviteData](t, b.call(t, guest, invite, http.StatusOK)); accepted.State != "accepted" {
		t.Fatalf("unexpected invitation acceptance: %+v", accepted)
	}
	expectConflict(t, b, guest, invite, "invite-used")
	final := read[roster](t, b.call(t, office, list, http.StatusOK))
	if final.Active != 4 || final.Held != 0 || final.Waiting != 2 || final.FreePlaces != 0 {
		t.Errorf("accepting did not settle the held place: %+v", final)
	}
}

func expectConflict(t *testing.T, b *backend, client *http.Client, in request, reason string) {
	t.Helper()

	if problem := read[struct{ Reason string }](t, b.call(t, client, in, http.StatusConflict)); problem.Reason != reason {
		t.Errorf("conflict reason %q, want %q", problem.Reason, reason)
	}
}

func participantRoles(t *testing.T, b *backend) {
	t.Helper()

	alina, felix, in, ute := checkWorkshopRoster(t, b)
	b.call(t, felix, request{
		method: http.MethodPut, path: "/api/v1/bookings/{bookingId}/participant",
		params: map[string]string{"bookingId": ute}, body: `{"trial":true,"trialNote":"Back after a break"}`,
	}, http.StatusOK)
	refreshed := read[roster](t, b.call(t, alina, in, http.StatusOK))
	for _, person := range refreshed.Bookings {
		if person.BookingID == ute && person.Trial && person.TrialNote == "Back after a break" {
			return
		}
	}
	t.Error("the instructor did not see the updated trial note")
}

func checkWorkshopRoster(t *testing.T, b *backend) (instructor, accompanist *http.Client, in request, trialBooking string) {
	t.Helper()

	alina := browser(t)
	felix := browser(t)
	clara := browser(t)
	login(t, b, alina, "alina.starling@example.net", demoPassword)
	login(t, b, felix, "felix.keysworth@example.net", demoPassword)
	login(t, b, clara, "clara.amberly@example.org", demoPassword)
	lesson := lessonOn(t, b, "2026-11-16", "2026-11-17", "Classical Ballet")
	in = request{
		method: http.MethodGet, path: "/api/v1/lessons/{lessonId}/participants",
		params: map[string]string{"lessonId": lesson},
	}
	b.call(t, browser(t), in, http.StatusUnauthorized)
	b.call(t, clara, in, http.StatusForbidden)
	b.call(t, alina, in, http.StatusOK)
	list := read[roster](t, b.call(t, felix, in, http.StatusOK))
	mara, pia, ute := false, false, ""
	for _, person := range list.Bookings {
		switch person.Name {
		case "Mara Gastfield":
			mara = person.Guest && person.ContactEmail == "mara.gastfield@example.com"
		case "Pia Walkby":
			pia = person.Guest && person.ContactEmail == ""
		case "Ute Tallowfield":
			if person.Trial {
				ute = person.BookingID
			}
		}
	}
	if list.Capacity != 16 || list.Active != 3 || list.FreePlaces != 13 || !mara || !pia || ute == "" {
		t.Fatalf("unexpected workshop participants: %+v", list)
	}

	return alina, felix, in, ute
}

func participantRosterAndHistory(t *testing.T, b *backend) {
	t.Helper()

	_, felix, _, booking := checkWorkshopRoster(t, b)
	guest, office := browser(t), browser(t)
	login(t, b, office, "olga.ledgerby@example.net", demoPassword)
	history := request{
		method: http.MethodGet, path: "/api/v1/admin/bookings/{bookingId}/events",
		params: map[string]string{"bookingId": booking},
	}
	b.call(t, guest, history, http.StatusUnauthorized)
	b.call(t, felix, history, http.StatusForbidden)
	items := read[struct {
		Items []struct {
			ID, Status, At string
			Actor          *struct{ AccountID, Name, Email string }
		}
	}](t, b.call(t, office, history, http.StatusOK)).Items
	if len(items) == 0 || items[0].Status != "booked" || items[0].Actor == nil || items[0].Actor.Name == "" || items[0].Actor.Email == "" {
		t.Fatalf("workshop booking is missing its initial history: %+v", items)
	}

	event := read[eventSummary](t, b.call(t, guest, request{
		method: http.MethodGet, path: "/api/v1/events/{slug}", params: map[string]string{"slug": "open-ballet-taster"},
	}, http.StatusOK))
	if len(event.Sessions) != 1 {
		t.Fatal("fixture event must have one session")
	}
	session := request{
		method: http.MethodGet, path: "/api/v1/lessons/{lessonId}/participants",
		params: map[string]string{"lessonId": event.Sessions[0].ID},
	}
	view := read[struct {
		roster
		Registrants []struct{ Name string }
	}](t, b.call(t, office, session, http.StatusOK))
	if view.Registrants == nil || len(view.Bookings) != 0 || len(view.Waitlist) != 0 || view.Active != len(view.Registrants) ||
		view.Capacity != 20 || view.FreePlaces != event.FreePlaces {
		t.Fatalf("session roster does not use the event registrations and capacity: %+v", view)
	}
}
