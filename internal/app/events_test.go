package app_test

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/app"
)

// eventsApp is the demo with the office and a member signed in, and the
// catalog the event tests use.
type eventsApp struct {
	*authApp

	office, member          *http.Cookie
	courseType, room, staff string
}

func newEventsApp(t *testing.T) *eventsApp {
	t.Helper()

	a := &eventsApp{authApp: newAuthApp(t, app.Deps{Demo: true})}
	a.office = a.login("olga.ledgerby@example.net")
	a.member = a.login("clara.amberly@example.org")

	terms := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/terms", "", a.member))
	termID, _ := terms["items"].([]any)[0].(map[string]any)["id"].(string)
	classes := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/terms/"+termID+"/classes", "", a.member))
	class, _ := classes["items"].([]any)[0].(map[string]any)
	a.courseType, _ = class["courseTypeId"].(string)
	a.room, _ = class["roomId"].(string)
	a.staff, _ = class["instructor"].(map[string]any)["id"].(string)

	return a
}

// event is the body of a priced event with places.
func (a *eventsApp) event(title string, places int, extra string) string {
	return fmt.Sprintf(`{"title":%q,"courseTypeId":%q,"capacity":%d,"price":4500,"vatRate":1900%s}`,
		title, a.courseType, places, extra)
}

// session is the body of a session on the date at the time (school time).
func (a *eventsApp) session(date, start, extra string) string {
	return fmt.Sprintf(`{"date":%q,"startTime":%q,"durationMinutes":90,"roomId":%q,"instructorId":%q%s}`,
		date, start, a.room, a.staff, extra)
}

// wantReason fails unless body is a 409 with reason.
func wantReason(t *testing.T, body []byte, reason string) {
	t.Helper()

	if got := decode(t, body); got["reason"] != reason {
		t.Errorf("409 %v, want %s", got, reason)
	}
}

// wantField fails unless body is a 422 at location.
func wantField(t *testing.T, body []byte, location string) {
	t.Helper()

	if !strings.Contains(string(body), `"location":"`+location+`"`) {
		t.Errorf("422 %s, want %s", body, location)
	}
}

// TestAdminEvents runs the office's event administration over HTTP (ADR
// 0019): an event's lifecycle, its sessions, image, registrations, fees
// and refunds, with a 404, a 409 with its reason and a 422 per operation.
// Members get a 403 (TestAccessMatrix); the rules are tested in package
// service.
func TestAdminEvents(t *testing.T) {
	t.Parallel()

	a := newEventsApp(t)
	const events = "/api/v1/admin/events"
	const unknown = "/00000000-0000-7000-8000-00000000f001"

	a.expect(http.StatusForbidden, http.MethodPost, events, a.event("Imaginary Intensive", 1, ""), a.member)

	created := decode(t, a.expect(http.StatusCreated, http.MethodPost, events,
		a.event("Imaginary Intensive", 1, `,"description":"Two mornings."`), a.office))
	id, _ := created["id"].(string)
	path := events + "/" + id

	if created["slug"] != "imaginary-intensive" || created["status"] != "draft" || created["price"] != 4500.0 ||
		created["registered"] != 0.0 || created["createdAt"] != "2026-10-23T16:00:00Z" {
		t.Errorf("created %v", created)
	}

	wantField(t, a.expect(http.StatusUnprocessableEntity, http.MethodPost, events, a.event("!!!", 1, ""), a.office),
		"body.slug")
	wantReason(t, a.expect(http.StatusConflict, http.MethodPost, events,
		a.event("Other", 1, `,"slug":"imaginary-intensive"`), a.office), "duplicate")

	// Reads.
	a.expect(http.StatusNotFound, http.MethodGet, events+unknown, "", a.office)

	if list := decode(t, a.expect(http.StatusOK, http.MethodGet, events, "", a.office)); len(list["items"].([]any)) != 1 {
		t.Errorf("list %v", list)
	}

	// Publishing needs a session.
	wantReason(t, a.expect(http.StatusConflict, http.MethodPost, path+"/publish", "", a.office), "no-sessions")
	a.expect(http.StatusNotFound, http.MethodPost, events+unknown+"/publish", "", a.office)

	session := decode(t, a.expect(http.StatusCreated, http.MethodPost, path+"/sessions",
		a.session("2026-10-24", "07:00", ""), a.office))
	sessionID, _ := session["id"].(string)

	if session["eventId"] != id || session["kind"] != "workshop" || session["capacity"] != 1.0 ||
		session["startsAt"] != "2026-10-24T05:00:00Z" {
		t.Errorf("session %v", session)
	}

	wantReason(t, a.expect(http.StatusConflict, http.MethodPost, path+"/sessions",
		a.session("2026-10-24", "07:30", ""), a.office), "room-taken")
	wantReason(t, a.expect(http.StatusConflict, http.MethodPost, path+"/sessions",
		a.session("2026-10-01", "07:00", ""), a.office), "past")
	wantField(t, a.expect(http.StatusUnprocessableEntity, http.MethodPost, path+"/sessions",
		strings.Replace(a.session("2026-10-25", "07:00", ""), a.room, "00000000-0000-7000-8000-00000000f001", 1),
		a.office), "body.roomId")
	a.expect(http.StatusNotFound, http.MethodPost, events+unknown+"/sessions", a.session("2026-10-25", "07:00", ""),
		a.office)

	// A draft takes no registrations, and its session is the office's.
	finn := a.personID("finn amberly", a.office)
	clara := a.personID("clara amberly", a.office)

	wantReason(t, a.expect(http.StatusConflict, http.MethodPost, path+"/registrations",
		`{"personId":"`+finn+`"}`, a.office), "closed")
	a.expect(http.StatusNotFound, http.MethodGet, "/api/v1/lessons/"+sessionID, "", a.member)

	published := decode(t, a.expect(http.StatusOK, http.MethodPost, path+"/publish", "", a.office))
	if published["status"] != "published" || len(published["sessions"].([]any)) != 1 {
		t.Errorf("published %v", published)
	}

	wantReason(t, a.expect(http.StatusConflict, http.MethodPost, path+"/publish", "", a.office), "not-draft")

	// Members see the session with its event, and book it not.
	if got := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/lessons/"+sessionID, "", a.member)); got["eventId"] != id {
		t.Errorf("member's session %v", got)
	}

	wantReason(t, a.expect(http.StatusConflict, http.MethodPost, "/api/v1/lessons/"+sessionID+"/bookings",
		`{"personId":"`+clara+`"}`, a.member), "event-session")
	wantReason(t, a.expect(http.StatusConflict, http.MethodPost, "/api/v1/lessons/"+sessionID+"/waitlist",
		`{"personId":"`+clara+`"}`, a.member), "event-session")
	wantReason(t, a.expect(http.StatusConflict, http.MethodPost, "/api/v1/lessons/"+sessionID+"/participants",
		`{"personId":"`+clara+`"}`, a.office), "event-session")

	// Registrations.
	wantField(t, a.expect(http.StatusUnprocessableEntity, http.MethodPost, path+"/registrations",
		`{"personId":"`+finn+`","guest":{"name":"Gina Guest","email":"gina@example.org"}}`, a.office), "body")
	wantField(t, a.expect(http.StatusUnprocessableEntity, http.MethodPost, path+"/registrations",
		`{"guest":{"name":"Gina Guest"}}`, a.office), "body.guest.email")
	a.expect(http.StatusNotFound, http.MethodPost, events+unknown+"/registrations", `{"personId":"`+finn+`"}`, a.office)

	reg := decode(t, a.expect(http.StatusCreated, http.MethodPost, path+"/registrations",
		`{"personId":"`+finn+`"}`, a.office))
	regID, _ := reg["id"].(string)

	if person, _ := reg["person"].(map[string]any); person["name"] != "Finn Amberly" || reg["status"] != "registered" ||
		reg["payment"] != "unpaid" || reg["price"] != 4500.0 {
		t.Errorf("registration %v", reg)
	}

	wantReason(t, a.expect(http.StatusConflict, http.MethodPost, path+"/registrations",
		`{"guest":{"name":"Gina Guest","email":"gina@example.org"}}`, a.office), "full")

	guest := decode(t, a.expect(http.StatusCreated, http.MethodPost, path+"/registrations",
		`{"guest":{"name":"Gina Guest","email":"gina@example.org"},"confirmOverCapacity":true}`, a.office))
	wantReason(t, a.expect(http.StatusConflict, http.MethodPost, path+"/registrations",
		`{"personId":"`+finn+`","confirmOverCapacity":true}`, a.office), "duplicate")

	regs := decode(t, a.expect(http.StatusOK, http.MethodGet, path+"/registrations", "", a.office))
	if items, _ := regs["items"].([]any); len(items) != 2 || items[1].(map[string]any)["guest"].(map[string]any)["name"] != "Gina Guest" {
		t.Errorf("registrations %v", regs)
	}

	a.expect(http.StatusNotFound, http.MethodGet, events+unknown+"/registrations", "", a.office)

	// The capacity follows; it is not lowered below the places taken.
	update := a.event("Imaginary Intensive", 3, "")
	if got := decode(t, a.expect(http.StatusOK, http.MethodPut, path, update, a.office)); got["capacity"] != 3.0 ||
		got["sessions"].([]any)[0].(map[string]any)["capacity"] != 3.0 {
		t.Errorf("updated %v", got)
	}

	wantField(t, a.expect(http.StatusUnprocessableEntity, http.MethodPut, path, a.event("Imaginary Intensive", 1, ""),
		a.office), "body.capacity")
	a.expect(http.StatusNotFound, http.MethodPut, events+unknown, update, a.office)

	// Fees at the desk.
	const registrations = "/api/v1/admin/event-registrations/"

	paid := decode(t, a.expect(http.StatusOK, http.MethodPost, registrations+regID+"/pay",
		`{"paymentMethod":"card","payerId":"`+clara+`"}`, a.office))
	receipt, _ := paid["receipt"].(map[string]any)

	if r, _ := paid["registration"].(map[string]any); r["payment"] != "paid" || r["receiptId"] != receipt["id"] ||
		receipt["number"] == "" {
		t.Errorf("paid %v", paid)
	}

	wantReason(t, a.expect(http.StatusConflict, http.MethodPost, registrations+regID+"/pay", `{}`, a.office),
		"already-paid")
	wantField(t, a.expect(http.StatusUnprocessableEntity, http.MethodPost, registrations+guest["id"].(string)+"/pay",
		`{"payerId":"`+clara+`"}`, a.office), "body.payerId")
	a.expect(http.StatusNotFound, http.MethodPost, registrations+unknown[1:]+"/pay", `{}`, a.office)

	receiptPath := "/api/v1/admin/receipts/" + receipt["id"].(string)
	if got := decode(t, a.expect(http.StatusOK, http.MethodGet, receiptPath, "", a.office)); got["eventId"] != id ||
		got["eventTitle"] != "Imaginary Intensive" || got["cancelable"] != false {
		t.Errorf("receipt %v", got)
	}

	// While the registration is open, the office cancels it, not its receipt.
	wantReason(t, a.expect(http.StatusConflict, http.MethodPost, receiptPath+"/cancel", `{}`, a.office),
		"registration-open")

	// A second session ahead, which the event's cancellation cancels.
	ahead := decode(t, a.expect(http.StatusCreated, http.MethodPost, path+"/sessions",
		a.session("2026-10-26", "07:00", ""), a.office))

	// Taking an event back that has registrations.
	wantReason(t, a.expect(http.StatusConflict, http.MethodPost, path+"/unpublish", "", a.office), "in-use")

	// Images.
	png := base64.StdEncoding.EncodeToString([]byte("\x89PNG\r\n\x1a\nimage"))

	a.expect(http.StatusNotFound, http.MethodGet, path+"/image", "", a.office)
	wantField(t, a.expect(http.StatusUnprocessableEntity, http.MethodPut, path+"/image",
		`{"contentType":"image/png","data":"`+base64.StdEncoding.EncodeToString([]byte("GIF89a"))+`"}`, a.office),
		"body.data")
	a.expect(http.StatusNoContent, http.MethodPut, path+"/image", `{"contentType":"image/png","data":"`+png+`"}`,
		a.office)
	a.expect(http.StatusNotFound, http.MethodPut, events+unknown+"/image",
		`{"contentType":"image/png","data":"`+png+`"}`, a.office)

	res, img := a.call(http.MethodGet, path+"/image", "", a.office)
	if res.StatusCode != http.StatusOK || res.Header.Get("Content-Type") != "image/png" ||
		!strings.HasPrefix(string(img), "\x89PNG") {
		t.Fatalf("image %d %v", res.StatusCode, res.Header)
	}

	etag := res.Header.Get("ETag")

	got := decode(t, a.expect(http.StatusOK, http.MethodGet, path, "", a.office))
	if info, _ := got["image"].(map[string]any); `"`+info["digest"].(string)+`"` != etag {
		t.Errorf("image info %v, ETag %s", got["image"], etag)
	}

	if res := a.callWith(http.MethodGet, path+"/image", "If-None-Match", etag, a.office); res != http.StatusNotModified {
		t.Errorf("unchanged image: %d, want 304", res)
	}

	a.expect(http.StatusNoContent, http.MethodDelete, path+"/image", "", a.office)
	a.expect(http.StatusNotFound, http.MethodGet, path+"/image", "", a.office)
	a.expect(http.StatusNotFound, http.MethodDelete, events+unknown+"/image", "", a.office)

	// Once the session has started, a fee kept at the cancellation is
	// refunded only with the registration's cancellation.
	a.clock.advance(14 * time.Hour)

	kept := decode(t, a.expect(http.StatusOK, http.MethodPost, registrations+regID+"/cancel", `{}`, a.office))
	if kept["status"] != "canceled" || kept["payment"] != "paid" {
		t.Errorf("canceled and kept %v", kept)
	}

	wantReason(t, a.expect(http.StatusConflict, http.MethodPost, receiptPath+"/cancel", `{}`, a.office),
		"event-attended")

	guestID, _ := guest["id"].(string)
	a.expect(http.StatusOK, http.MethodPost, registrations+guestID+"/pay", `{}`, a.office)

	canceled := decode(t, a.expect(http.StatusOK, http.MethodPost, registrations+guestID+"/cancel",
		`{"refund":true}`, a.office))
	if canceled["status"] != "canceled" || canceled["payment"] != "refunded" || canceled["cancellationReceiptId"] == nil {
		t.Errorf("canceled %v", canceled)
	}

	wantReason(t, a.expect(http.StatusConflict, http.MethodPost, registrations+regID+"/cancel", `{}`, a.office),
		"already-canceled")
	a.expect(http.StatusNotFound, http.MethodPost, registrations+unknown[1:]+"/cancel", `{}`, a.office)

	// Canceling the event, with a note of at most 500 characters.
	wantField(t, a.expect(http.StatusUnprocessableEntity, http.MethodPost, path+"/cancel",
		fmt.Sprintf(`{"note":%q}`, strings.Repeat("x", 501)), a.office), "body.note")

	if got := decode(t, a.expect(http.StatusOK, http.MethodPost, path+"/cancel", `{"note":"Ill"}`, a.office)); got["status"] != "canceled" {
		t.Errorf("canceled event %v", got)
	}

	wantReason(t, a.expect(http.StatusConflict, http.MethodPost, path+"/cancel", `{}`, a.office), "closed")
	wantReason(t, a.expect(http.StatusConflict, http.MethodPost, "/api/v1/lessons/"+ahead["id"].(string)+"/restore",
		"", a.office), "closed")
	a.expect(http.StatusNotFound, http.MethodPost, events+unknown+"/cancel", `{}`, a.office)

	// Deleting: an event with registrations is canceled instead; a draft
	// goes with its sessions.
	wantReason(t, a.expect(http.StatusConflict, http.MethodDelete, path, "", a.office), "in-use")

	draft := decode(t, a.expect(http.StatusCreated, http.MethodPost, events, a.event("Winter Days", 2, ""), a.office))
	draftPath := events + "/" + draft["id"].(string)
	a.expect(http.StatusCreated, http.MethodPost, draftPath+"/sessions", a.session("2026-12-05", "07:00", ""), a.office)
	wantReason(t, a.expect(http.StatusConflict, http.MethodPost, draftPath+"/unpublish", "", a.office), "not-published")
	a.expect(http.StatusNoContent, http.MethodDelete, draftPath, "", a.office)
	a.expect(http.StatusNotFound, http.MethodDelete, draftPath, "", a.office)

	// A published event without registrations is taken back.
	other := decode(t, a.expect(http.StatusCreated, http.MethodPost, events, a.event("Spring Days", 2, ""), a.office))
	otherPath := events + "/" + other["id"].(string)
	a.expect(http.StatusCreated, http.MethodPost, otherPath+"/sessions", a.session("2026-12-06", "07:00", ""), a.office)
	a.expect(http.StatusOK, http.MethodPost, otherPath+"/publish", "", a.office)

	if got := decode(t, a.expect(http.StatusOK, http.MethodPost, otherPath+"/unpublish", "", a.office)); got["status"] != "draft" {
		t.Errorf("taken back %v", got)
	}

	a.expect(http.StatusNotFound, http.MethodPost, events+unknown+"/unpublish", "", a.office)

	// The audit log names the event, the registration and the refund.
	audit := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/admin/audit", "",
		a.login("arno.keystone@example.net")))
	actions := map[string]map[string]any{}

	for _, item := range audit["items"].([]any) {
		e, _ := item.(map[string]any)
		// Newest first: the latest of each action.
		action, _ := e["action"].(string)
		if d, ok := e["details"].(map[string]any); ok && d["eventId"] != nil && actions[action] == nil {
			actions[action] = d
		}
	}

	if d := actions["event-registration-canceled"]; d["eventId"] != id || d["registrationId"] != guestID || d["refund"] != true {
		t.Errorf("audited cancellation %v", d)
	}

	for _, action := range []string{"event-registration-added", "event-registration-paid", "event-canceled"} {
		if actions[action] == nil {
			t.Errorf("no %s in the audit log: %v", action, audit)
		}
	}
}

// callWith sends a request with one header and returns its status.
func (a *eventsApp) callWith(method, path, header, value string, cookie *http.Cookie) int {
	a.t.Helper()

	req := httptest.NewRequest(method, path, nil)
	req.Header.Set(header, value)
	req.AddCookie(cookie)

	rec := httptest.NewRecorder()
	a.h.ServeHTTP(rec, req)

	return rec.Code
}
