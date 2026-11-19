package conformance

import (
	"net/http"
	"strings"
	"testing"
)

func lessonCancellationAndDeletion(t *testing.T, b *backend) {
	t.Helper()

	office, member, guest := browser(t), browser(t), browser(t)
	login(t, b, office, "olga.ledgerby@example.net", demoPassword)
	login(t, b, member, demoMember, demoPassword)
	data := lessonFixture(t, b, office, "regular", 4)
	created := read[struct{ ID string }](t, b.call(t, office, request{
		method: http.MethodPost, path: "/api/v1/lessons", body: jsonBody(t, data),
	}, http.StatusCreated))
	ref := request{
		method: http.MethodGet, path: "/api/v1/lessons/{lessonId}",
		params: map[string]string{"lessonId": created.ID},
	}
	cancel := ref
	cancel.method, cancel.path, cancel.body = http.MethodPost, ref.path+"/cancel", `{"note":"  Fixture instructor unavailable  "}`
	remove := ref
	remove.method = http.MethodDelete
	b.call(t, guest, cancel, http.StatusUnauthorized)
	b.call(t, member, cancel, http.StatusForbidden)
	b.call(t, guest, remove, http.StatusUnauthorized)
	b.call(t, member, remove, http.StatusForbidden)

	type lesson struct {
		Status, EventID string
		Admin           struct {
			ActiveBookings         int
			Detached               bool
			CancelNote, CanceledAt string
			CanceledBy             *struct{ ID, Name string }
		}
	}
	got := read[lesson](t, b.call(t, office, cancel, http.StatusOK))
	if got.Status != "canceled" || got.Admin.CancelNote != "Fixture instructor unavailable" ||
		got.Admin.CanceledAt != demoNow || got.Admin.CanceledBy == nil || got.Admin.CanceledBy.ID == "" ||
		got.Admin.ActiveBookings != 0 || got.Admin.Detached {
		t.Fatalf("canceled one-off lesson: %+v", got)
	}
	cancel.body = `{"note":"Another note"}`
	again := read[lesson](t, b.call(t, office, cancel, http.StatusOK))
	if again.Admin.CancelNote != got.Admin.CancelNote || again.Admin.CanceledAt != got.Admin.CanceledAt {
		t.Fatalf("repeated cancel changed the original cancellation: %+v", again)
	}
	b.call(t, office, remove, http.StatusNoContent)
	b.call(t, office, ref, http.StatusNotFound)

	event := request{method: http.MethodGet, path: "/api/v1/events/{slug}", params: map[string]string{"slug": "open-ballet-taster"}}
	workshop := read[eventSummary](t, b.call(t, office, event, http.StatusOK))
	if len(workshop.Sessions) != 1 {
		t.Fatalf("fixture needs a single event session: %+v", workshop)
	}
	remove.params = map[string]string{"lessonId": workshop.Sessions[0].ID}
	expectConflict(t, b, office, remove, "in-use")
	cancel.params = remove.params
	cancel.body = `{"note":"Part of the event was canceled"}`
	session := b.call(t, office, cancel, http.StatusOK)
	if got := read[lesson](t, session); got.Status != "canceled" || got.Admin.CanceledBy == nil ||
		got.EventID == "" || containsJSON(session.body, `"eventSlug":`) {
		t.Fatalf("canceled event session: %s", session.body)
	}
	expectConflict(t, b, office, remove, "in-use")
	if updated := read[eventSummary](t, b.call(t, guest, event, http.StatusOK)); updated.Status != "published" || updated.Sessions[0].ID != workshop.Sessions[0].ID {
		t.Fatalf("individual cancellation changed the event: %+v", updated)
	}
	outbox := read[struct {
		Mails []struct{ To, Text, Template string }
	}](t, b.fixture(t, http.MethodGet, "outbox", "", http.StatusOK))
	mails := 0
	for _, mail := range outbox.Mails {
		if mail.To == "juna.firstlight@example.org" && mail.Template == "lesson-canceled" && strings.Contains(mail.Text, "Part of the event was canceled") {
			mails++
		}
	}
	if mails != 1 {
		t.Errorf("wanted one cancellation mail to the event's guest, got %d", mails)
	}
}
