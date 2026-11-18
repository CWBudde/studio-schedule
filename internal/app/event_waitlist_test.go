package app_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/app"
)

func TestEventInviteLink(t *testing.T) {
	t.Parallel()

	a := newEventsApp(t)
	const admin = "/api/v1/admin/events"
	created := decode(t, a.expect(http.StatusCreated, http.MethodPost, admin,
		a.event("Imaginary Intensive", 1, ""), a.office))
	id := created["id"].(string)
	a.expect(http.StatusCreated, http.MethodPost, admin+"/"+id+"/sessions",
		a.session("2026-10-26", "07:00", `,"confirmRoomOverlap":true`), a.office)
	a.expect(http.StatusOK, http.MethodPost, admin+"/"+id+"/publish", "", a.office)
	path := "/api/v1/events/" + created["slug"].(string)
	person := func(name string) string {
		return `{"personId":"` + a.personID(name, a.office) + `"}`
	}
	finn, clara := person("finn amberly"), person("clara amberly")

	wantReason(t, a.expect(http.StatusConflict, http.MethodPost, path+"/waitlist", finn, a.member), "not-full")
	registered := decode(t, a.expect(http.StatusCreated, http.MethodPost, path+"/registrations", clara, a.member))
	waiting := decode(t, a.expect(http.StatusCreated, http.MethodPost, path+"/waitlist", finn, a.member))
	if waiting["registration"].(map[string]any)["status"] != "waiting" {
		t.Fatalf("waiting: %v", waiting)
	}
	reg := "/api/v1/event-registrations/"
	a.expect(http.StatusOK, http.MethodPost, reg+registered["registration"].(map[string]any)["id"].(string)+"/cancel", "", a.member)

	token := a.linkToken("event-invite", "events/invite")
	link := "/api/v1/event-invites/" + token
	public := func(status int, method, path string) []byte {
		t.Helper()

		res, body := a.call(method, path, "")
		if res.StatusCode != status {
			t.Fatalf("%s %s: %d %s, want %d", method, path, res.StatusCode, body, status)
		}

		return body
	}
	view := decode(t, public(http.StatusOK, http.MethodGet, link))
	if view["givenName"] != "Finn" || view["expiresAt"] == nil ||
		view["event"].(map[string]any)["freePlaces"] != 0.0 {
		t.Fatalf("offer: %v", view)
	}

	public(http.StatusNoContent, http.MethodPost, link+"/accept")
	public(http.StatusNotFound, http.MethodPost, link+"/accept")
	public(http.StatusNotFound, http.MethodGet, link)

	mine := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/my/events", "", a.member))
	first := mine["items"].([]any)[0].(map[string]any)["registration"].(map[string]any)
	if first["id"] != waiting["registration"].(map[string]any)["id"] || first["status"] != "registered" {
		t.Fatalf("accepted registration: %v", mine)
	}
}

func TestJobsExpireEventInvite(t *testing.T) {
	t.Parallel()

	a := newEventsApp(t)
	const admin = "/api/v1/admin/events"
	created := decode(t, a.expect(http.StatusCreated, http.MethodPost, admin, a.event("Imaginary Intensive", 1, ""), a.office))
	id := created["id"].(string)
	a.expect(http.StatusCreated, http.MethodPost, admin+"/"+id+"/sessions", a.session("2026-10-26", "07:00", `,"confirmRoomOverlap":true`), a.office)
	a.expect(http.StatusOK, http.MethodPost, admin+"/"+id+"/publish", "", a.office)
	path := "/api/v1/events/" + created["slug"].(string)

	occupant := decode(t, a.expect(http.StatusCreated, http.MethodPost, path+"/registrations",
		`{"personId":"`+a.personID("clara amberly", a.office)+`"}`, a.member))
	regID := occupant["registration"].(map[string]any)["id"].(string)
	a.expect(http.StatusCreated, http.MethodPost, path+"/waitlist",
		`{"personId":"`+a.personID("finn amberly", a.office)+`"}`, a.member)
	a.expect(http.StatusOK, http.MethodPost, "/api/v1/event-registrations/"+regID+"/cancel", "", a.member)

	_, text := a.mailTo("event-invite")
	if text == "" {
		t.Fatal("event invite mail is missing")
	}

	a.clock.advance(24 * time.Hour)
	runner := app.Jobs(a.deps)
	for range 2 {
		if _, err := runner.RunNow(t.Context(), "waitlist-expiry"); err != nil {
			t.Fatal(err)
		}
	}
	_, text = a.mailTo("event-invite-expired")
	if text == "" {
		t.Fatal("event invite expiry mail is missing")
	}
	after := decode(t, a.expect(http.StatusOK, http.MethodGet, path, "", a.member))
	if after["freePlaces"] != 1.0 {
		t.Fatalf("event after expiry: %v", after)
	}
}
