package app_test

import (
	"encoding/base64"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestPublicEvents runs the public and the members' event operations over
// HTTP (ADR 0019): drafts stay private, a published event names nobody, a
// member registers their family and cancels before the deadline, a guardian
// downloads a dependent's receipt and Storno, and another family gets 404s.
// The rules are tested in package service.
func TestPublicEvents(t *testing.T) {
	t.Parallel()

	a := newEventsApp(t)
	const admin = "/api/v1/admin/events"

	kai := a.login("kai.lanternby@example.org")
	finn := a.personID("finn amberly", a.office)
	clara := a.personID("clara amberly", a.office)
	kaiID := a.personID("kai lanternby", a.office)

	// public is a request without a cookie, which must answer status.
	public := func(status int, path string) []byte {
		t.Helper()

		res, body := a.call(http.MethodGet, path, "")
		if res.StatusCode != status {
			t.Fatalf("GET %s: %d %s, want %d", path, res.StatusCode, body, status)
		}

		return body
	}

	// Monday 2026-10-26, 07:00 in Berlin; the events share the room.
	newEvent := func(title string, places int, publish bool) (string, string) {
		created := decode(t, a.expect(http.StatusCreated, http.MethodPost, admin, a.event(title, places, ""), a.office))
		id, _ := created["id"].(string)
		slug, _ := created["slug"].(string)
		a.expect(http.StatusCreated, http.MethodPost, admin+"/"+id+"/sessions", a.session("2026-10-26", "07:00", `,"confirmRoomOverlap":true`), a.office)

		if publish {
			a.expect(http.StatusOK, http.MethodPost, admin+"/"+id+"/publish", "", a.office)
		}

		return id, slug
	}

	id, slug := newEvent("Imaginary Intensive", 2, true)
	_, draft := newEvent("Winter Days", 2, false)
	path := "/api/v1/events/" + slug

	// Drafts are private: absent from the list, a 404 by slug, for members too.
	list := decode(t, public(http.StatusOK, "/api/v1/events"))
	items, _ := list["items"].([]any)

	if len(items) != 1 {
		t.Fatalf("events %v", list)
	}

	e, _ := items[0].(map[string]any)
	session, _ := e["sessions"].([]any)[0].(map[string]any)

	if e["slug"] != slug || e["freePlaces"] != 2.0 || e["signupOpen"] != true || e["status"] != "published" ||
		e["hasImage"] != false || e["signupClosesAt"] != "2026-10-26T06:00:00Z" ||
		e["cancelUntil"] != "2026-10-26T06:00:00Z" || e["signupOpensAt"] != nil ||
		e["courseType"].(map[string]any)["id"] != a.courseType || session["eventSlug"] != slug {
		t.Errorf("event %v", e)
	}

	for _, key := range []string{"id", "registered", "registrations", "createdAt"} {
		if _, ok := e[key]; ok {
			t.Errorf("the public event has %s: %v", key, e)
		}
	}

	for _, p := range []string{"/api/v1/events/" + draft, "/api/v1/events/" + draft + "/image"} {
		public(http.StatusNotFound, p)
		a.expect(http.StatusNotFound, http.MethodGet, p, "", a.member)
	}

	a.expect(http.StatusNotFound, http.MethodPost, "/api/v1/events/"+draft+"/registrations",
		`{"personId":"`+clara+`"}`, a.member)
	public(http.StatusNotFound, "/api/v1/events/no-such-event")

	if got := decode(t, public(http.StatusOK, path)); got["title"] != "Imaginary Intensive" {
		t.Errorf("event %v", got)
	}

	// The image, cacheable.
	public(http.StatusNotFound, path+"/image")

	png := base64.StdEncoding.EncodeToString([]byte("\x89PNG\r\n\x1a\nimage"))
	a.expect(http.StatusNoContent, http.MethodPut, admin+"/"+id+"/image", `{"contentType":"image/png","data":"`+png+`"}`,
		a.office)

	res, img := a.call(http.MethodGet, path+"/image", "")
	if res.StatusCode != http.StatusOK || res.Header.Get("Content-Type") != "image/png" ||
		!strings.HasPrefix(string(img), "\x89PNG") {
		t.Fatalf("image %d %v", res.StatusCode, res.Header)
	}

	if got := a.callWith(http.MethodGet, path+"/image", "If-None-Match", res.Header.Get("ETag"), a.member); got != http.StatusNotModified {
		t.Errorf("unchanged image: %d, want 304", got)
	}

	if got := decode(t, public(http.StatusOK, path)); got["hasImage"] != true {
		t.Errorf("hasImage %v", got)
	}

	// Registering: signed in, for the family only.
	if res, _ := a.call(http.MethodPost, path+"/registrations", `{"personId":"`+clara+`"}`); res.StatusCode != http.StatusUnauthorized {
		t.Errorf("signed out: %d, want 401", res.StatusCode)
	}

	a.expect(http.StatusNotFound, http.MethodPost, path+"/registrations", `{"personId":"`+kaiID+`"}`, a.member)

	mine := decode(t, a.expect(http.StatusCreated, http.MethodPost, path+"/registrations",
		`{"personId":"`+finn+`"}`, a.member))
	reg, _ := mine["registration"].(map[string]any)
	regID, _ := reg["id"].(string)

	if person, _ := reg["person"].(map[string]any); person["name"] != "Finn Amberly" || reg["status"] != "registered" ||
		reg["payment"] != "unpaid" || reg["price"] != 4500.0 || reg["cancelable"] != true ||
		mine["event"].(map[string]any)["freePlaces"] != 1.0 {
		t.Errorf("registered %v", mine)
	}

	wantReason(t, a.expect(http.StatusConflict, http.MethodPost, path+"/registrations",
		`{"personId":"`+finn+`"}`, a.member), "duplicate")
	a.expect(http.StatusCreated, http.MethodPost, path+"/registrations", `{"personId":"`+kaiID+`"}`, kai)
	wantReason(t, a.expect(http.StatusConflict, http.MethodPost, path+"/registrations",
		`{"personId":"`+clara+`"}`, a.member), "full")

	// A canceled event says so and takes no registration.
	otherID, other := newEvent("Spring Days", 2, true)
	a.expect(http.StatusOK, http.MethodPost, admin+"/"+otherID+"/cancel", `{}`, a.office)

	if got := decode(t, public(http.StatusOK, "/api/v1/events/"+other)); got["status"] != "canceled" ||
		got["signupOpen"] != false {
		t.Errorf("canceled event %v", got)
	}

	wantReason(t, a.expect(http.StatusConflict, http.MethodPost, "/api/v1/events/"+other+"/registrations",
		`{"personId":"`+clara+`"}`, a.member), "closed")

	if list := decode(t, public(http.StatusOK, "/api/v1/events")); len(list["items"].([]any)) != 1 {
		t.Errorf("events with a canceled one %v", list)
	}

	// My events: the family's own, nobody else's. The demo seed registers some
	// persons for its own events, so only this event's entries are counted.
	for cookie, want := range map[*http.Cookie]string{a.member: "Finn Amberly", kai: "Kai Lanternby"} {
		got := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/my/events", "", cookie))
		items, _ := got["items"].([]any)
		here := 0

		for _, raw := range items {
			item := raw.(map[string]any)
			if item["registration"].(map[string]any)["person"].(map[string]any)["name"] != want {
				t.Errorf("my events %v, want only %s", got, want)
			}

			if item["event"].(map[string]any)["slug"] == slug {
				here++
			}
		}

		if here != 1 {
			t.Errorf("my events %v, want %s once for %s", got, want, slug)
		}
	}

	// The fee is paid at the desk by the dependent: the guardian gets the
	// receipt, another family not.
	paid := decode(t, a.expect(http.StatusOK, http.MethodPost, "/api/v1/admin/event-registrations/"+regID+"/pay",
		`{}`, a.office))
	receiptID, _ := paid["receipt"].(map[string]any)["id"].(string)

	a.expect(http.StatusOK, http.MethodGet, "/api/v1/my/receipts/"+receiptID+"/pdf", "", a.member)
	a.expect(http.StatusNotFound, http.MethodGet, "/api/v1/my/receipts/"+receiptID+"/pdf", "", kai)

	// Canceling: the family only; a paid fee is refunded.
	const cancel = "/api/v1/event-registrations/"

	a.expect(http.StatusNotFound, http.MethodPost, cancel+regID+"/cancel", "", kai)

	canceled := decode(t, a.expect(http.StatusOK, http.MethodPost, cancel+regID+"/cancel", "", a.member))
	reg, _ = canceled["registration"].(map[string]any)

	if reg["status"] != "canceled" || reg["payment"] != "refunded" || reg["cancelable"] != false ||
		canceled["event"].(map[string]any)["freePlaces"] != 1.0 {
		t.Fatalf("canceled %v", canceled)
	}

	stornoID, _ := reg["cancellationReceiptId"].(string)
	a.expect(http.StatusOK, http.MethodGet, "/api/v1/my/receipts/"+stornoID+"/pdf", "", a.member)
	a.expect(http.StatusNotFound, http.MethodGet, "/api/v1/my/receipts/"+stornoID+"/pdf", "", kai)
	wantReason(t, a.expect(http.StatusConflict, http.MethodPost, cancel+regID+"/cancel", "", a.member),
		"already-canceled")
	a.expect(http.StatusNotFound, http.MethodPost, cancel+"00000000-0000-7000-8000-00000000f001/cancel", "", a.member)

	// After the deadline (the first session's start) only the office cancels.
	kais := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/my/events", "", kai))
	kaiReg, _ := kais["items"].([]any)[0].(map[string]any)["registration"].(map[string]any)["id"].(string)

	a.clock.advance(3 * 24 * time.Hour)
	wantReason(t, a.expect(http.StatusConflict, http.MethodPost, cancel+kaiReg+"/cancel", "", kai), "too-late")
}
