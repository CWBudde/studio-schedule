package conformance

import (
	"net/http"
	"slices"
	"testing"
)

func classMemberRemoval(t *testing.T, b *backend) {
	t.Helper()

	guest, member, office, admin := browser(t), browser(t), browser(t), browser(t)
	login(t, b, member, linaEmail, demoPassword)
	login(t, b, office, "olga.ledgerby@example.net", demoPassword)
	login(t, b, admin, demoAdmin, demoPassword)
	b.call(t, admin, request{
		method: http.MethodPut, path: "/api/v1/admin/settings/booking-rules",
		body: `{"releaseWeekday":5,"releaseTime":"09:00","releaseWeeksAhead":8,` +
			`"bookingCutoffMinutes":15,"cancellationCutoffMinutes":1440,"waitlistInviteExpiryMinutes":360,"defaultVisibleDays":14}`,
	}, http.StatusOK)
	lessons := upcomingClassBookings(t, b, member)
	class, person := lessons[0].ClassID, lessons[0].BookedFor[0].PersonID
	roster := func() []string {
		t.Helper()
		body := read[struct {
			Items []struct{ Person struct{ ID string } }
		}](t, b.call(t, office, request{
			method: http.MethodGet, path: "/api/v1/admin/classes/{classId}/members",
			params: map[string]string{"classId": class},
		}, http.StatusOK))
		ids := make([]string, 0, len(body.Items))
		for _, item := range body.Items {
			ids = append(ids, item.Person.ID)
		}

		return ids
	}
	before := roster()
	if !slices.Contains(before, person) {
		t.Fatalf("seeded booking owner %s is missing from class roster %v", person, before)
	}
	remove := request{
		method: http.MethodDelete, path: "/api/v1/classes/{classId}/members/{personId}",
		params: map[string]string{"classId": class, "personId": person},
	}
	b.call(t, guest, remove, http.StatusUnauthorized)
	b.call(t, member, remove, http.StatusForbidden)
	b.call(t, office, remove, http.StatusNoContent)
	b.call(t, office, remove, http.StatusNotFound)
	if after := roster(); slices.Contains(after, person) || len(after) != len(before)-1 {
		t.Fatalf("removal changed the expected roster %v into %v", before, after)
	}
	for _, lesson := range lessons {
		doc := read[classBooking](t, b.call(t, member, request{
			method: http.MethodGet, path: "/api/v1/lessons/{lessonId}",
			params: map[string]string{"lessonId": lesson.ID},
		}, http.StatusOK))
		if len(doc.BookedFor) != 0 {
			t.Errorf("lesson %s retains the removed member's booking: %+v", lesson.ID, doc.BookedFor)
		}
	}
	outbox := read[struct{ Mails []struct{ Template string } }](t, b.fixture(t, http.MethodGet, "outbox", "", http.StatusOK))
	for _, mail := range outbox.Mails {
		if mail.Template == "booking-canceled" {
			t.Error("office class removal sent a cancellation mail")
		}
	}
}
