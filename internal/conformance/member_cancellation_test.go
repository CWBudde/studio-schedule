package conformance

import (
	"net/http"
	"slices"
	"strings"
	"testing"
)

func memberCancellation(t *testing.T, b *backend) {
	t.Helper()

	const email = "irene.inkwell@example.com"

	office, member, visitor := browser(t), browser(t), browser(t)
	login(t, b, office, "olga.ledgerby@example.net", demoPassword)
	login(t, b, member, email, demoPassword)
	lesson := lessonOn(t, b, "2026-11-02", "2026-11-03", "Tap")
	list := request{
		method: http.MethodGet, path: "/api/v1/lessons/{lessonId}/participants",
		params: map[string]string{"lessonId": lesson},
	}
	before := read[roster](t, b.call(t, office, list, http.StatusOK))
	i := slices.IndexFunc(before.Bookings, func(v participant) bool { return v.Name == "Irene Inkwell" })
	if i == -1 {
		t.Fatal("Irene has no seeded booking")
	}

	target := before.Bookings[i]
	cancel := request{
		method: http.MethodPost, path: "/api/v1/bookings/{bookingId}/cancel",
		params: map[string]string{"bookingId": target.BookingID},
	}
	b.call(t, visitor, cancel, http.StatusUnauthorized)
	b.call(t, office, cancel, http.StatusNotFound)
	got := read[struct {
		booking
		Source, CanceledAt string
	}](t, b.call(t, member, cancel, http.StatusOK))
	if got.ID != target.BookingID || got.PersonID != target.PersonID || got.LessonID != lesson ||
		got.Status != "canceled" || got.Source != target.Source || got.Late || got.CanceledAt != demoNow {
		t.Fatalf("unexpected cancellation: %+v", got)
	}
	expectConflict(t, b, member, cancel, "already-canceled")

	after := read[roster](t, b.call(t, office, list, http.StatusOK))
	if after.Active != before.Active-1 || after.Held != 1 || after.FreePlaces != 0 ||
		len(after.Waitlist) != 2 || after.Waitlist[0].Name != "Vera Rushwick" || after.Waitlist[0].Status != "invited" {
		t.Fatalf("the first person should hold the freed place: %+v", after)
	}
	outbox := read[struct {
		Mails []struct{ To, Text, Template string }
	}](t, b.fixture(t, http.MethodGet, "outbox", "", http.StatusOK))
	count := 0
	for _, message := range outbox.Mails {
		if message.Template == "booking-canceled" && message.To == email && strings.Contains(message.Text, "Irene Inkwell") {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected one cancellation notice to Irene, got %d", count)
	}
	mailToken(t, b, "vera.rushwick@example.com", "waitlist-invite", "/waitlist/accept")
}
