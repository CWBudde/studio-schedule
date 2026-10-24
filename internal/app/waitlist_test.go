package app_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/app"
)

// TestWaitlist runs the waiting list over HTTP (ADR 0010): joining a full
// lesson, the conflicts, the family's entries in the lesson, the invite
// mailed when a place frees, and its link, which works without a sign-in
// and only once. The rules at their edges are tested in package service.
func TestWaitlist(t *testing.T) {
	t.Parallel()

	a := newAuthApp(t, app.Deps{Demo: true})
	office := a.login("olga.ledgerby@example.net")
	clara := a.login("clara.amberly@example.org")
	family := a.family(clara)
	me, ella := family.Me.ID, family.Dependents[0].ID

	terms := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/terms", "", office))
	termID, _ := terms["items"].([]any)[0].(map[string]any)["id"].(string)
	demo := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/terms/"+termID+"/classes", "", office))
	existing, _ := demo["items"].([]any)[0].(map[string]any)

	workshop := func(capacity int) string {
		l := decode(t, a.expect(http.StatusCreated, http.MethodPost, "/api/v1/lessons",
			fmt.Sprintf(`{"courseTypeId":%q,"date":"2026-11-03","startTime":"10:00","durationMinutes":60,"roomId":%q,`+
				`"instructorId":%q,"capacity":%d,"visibility":"public","kind":"workshop","confirmRoomOverlap":true}`,
				existing["courseTypeId"], existing["roomId"], existing["instructor"].(map[string]any)["id"], capacity), office))
		id, _ := l["id"].(string)

		return id
	}
	person := func(id string) string { return `{"personId":"` + id + `"}` }
	reason := func(body []byte) string {
		r, _ := decode(t, body)["reason"].(string)

		return r
	}

	full := workshop(1)
	booking := decode(t, a.expect(http.StatusCreated, http.MethodPost, "/api/v1/lessons/"+full+"/bookings", person(me), clara))

	joinPath := "/api/v1/lessons/" + full + "/waitlist"
	entry := decode(t, a.expect(http.StatusCreated, http.MethodPost, joinPath,
		`{"personId":"`+ella+`","stayOnList":true}`, clara))

	if entry["lessonId"] != full || entry["personId"] != ella || entry["status"] != "waiting" ||
		entry["stayOnList"] != true || entry["position"] != 1.0 || entry["expiresAt"] != nil || entry["id"] == "" {
		t.Fatalf("entry %v", entry)
	}

	entryID, _ := entry["id"].(string)

	for name, tt := range map[string]struct {
		path, body, want string
	}{
		"twice":    {joinPath, person(ella), "already-waiting"},
		"booked":   {joinPath, person(me), "already-booked"},
		"not full": {"/api/v1/lessons/" + workshop(5) + "/waitlist", person(ella), "not-full"},
	} {
		if got := reason(a.expect(http.StatusConflict, http.MethodPost, tt.path, tt.body, clara)); got != tt.want {
			t.Errorf("%s: %s, want %s", name, got, tt.want)
		}
	}

	a.expect(http.StatusNotFound, http.MethodPost, joinPath, person("00000000-0000-7000-8000-00000000f001"), clara)
	a.want(http.MethodPost, joinPath, person(ella), http.StatusUnauthorized)

	// The family sees its entries; the lesson has no free place.
	lesson := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/lessons/"+full, "", clara))
	waitlist, _ := lesson["waitlist"].([]any)

	if lesson["freePlaces"] != 0.0 || len(waitlist) != 1 {
		t.Fatalf("lesson for clara: %v", lesson)
	}

	if w := waitlist[0].(map[string]any); w["personId"] != ella || w["entryId"] != entryID || w["status"] != "waiting" ||
		w["position"] != 1.0 || w["name"] != "Ella Amberly" || w["stayOnList"] != true {
		t.Errorf("waitlist entry %v", w)
	}

	// Nobody else touches the entry; a waiting entry has no invite.
	acceptPath := "/api/v1/waitlist/" + entryID + "/accept"
	a.expect(http.StatusNotFound, http.MethodPost, acceptPath, "", office)
	a.expect(http.StatusNotFound, http.MethodDelete, "/api/v1/waitlist/"+entryID, "", office)

	if got := reason(a.expect(http.StatusConflict, http.MethodPost, acceptPath, "", clara)); got != "not-invited" {
		t.Errorf("accept a waiting entry: %s, want not-invited", got)
	}

	// Clara cancels her booking: Ella is invited, through her guardians.
	a.expect(http.StatusOK, http.MethodPost, "/api/v1/bookings/"+booking["id"].(string)+"/cancel", "", clara)

	to, text := a.mailTo("waitlist-invite")
	if (to != "clara.amberly@example.org" && to != "tom.amberly@example.org") || !strings.Contains(text, "Ella Amberly") {
		t.Errorf("invite to %s:\n%s", to, text)
	}

	token := a.linkToken("waitlist-invite", "waitlist/accept")
	invitePath := "/api/v1/waitlist-invites/" + token

	lesson = decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/lessons/"+full, "", clara))
	if w := lesson["waitlist"].([]any)[0].(map[string]any); lesson["freePlaces"] != 0.0 || w["status"] != "invited" ||
		w["position"] != nil || w["expiresAt"] == nil {
		t.Errorf("lesson while invited: %v", lesson)
	}

	// The link works without a session: a look changes nothing.
	view := decode(t, a.want(http.MethodGet, invitePath, "", http.StatusOK))
	if view["state"] != "invited" || view["givenName"] != "Ella" || view["expiresAt"] == nil || view["closed"] != nil ||
		view["lesson"].(map[string]any)["id"] != full {
		t.Fatalf("invite %v", view)
	}

	accepted := decode(t, a.want(http.MethodPost, invitePath, "", http.StatusOK))
	if accepted["state"] != "accepted" {
		t.Fatalf("accepted %v", accepted)
	}

	if got := reason(a.want(http.MethodPost, invitePath, "", http.StatusConflict)); got != "invite-used" {
		t.Errorf("accept twice: %s, want invite-used", got)
	}

	a.want(http.MethodGet, "/api/v1/waitlist-invites/no-such-token", "", http.StatusNotFound)

	lesson = decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/lessons/"+full, "", clara))
	if bookedFor, _ := lesson["bookedFor"].([]any); lesson["waitlist"] != nil || len(bookedFor) != 1 ||
		bookedFor[0].(map[string]any)["personId"] != ella {
		t.Errorf("lesson after accepting: %v", lesson)
	}

	if got := reason(a.expect(http.StatusConflict, http.MethodDelete, "/api/v1/waitlist/"+entryID, "", clara)); got != "not-waiting" {
		t.Errorf("leave after accepting: %s, want not-waiting", got)
	}

	// Leaving and declining in the app.
	other := workshop(1)
	a.expect(http.StatusCreated, http.MethodPost, "/api/v1/lessons/"+other+"/bookings", person(ella), clara)
	left := decode(t, a.expect(http.StatusCreated, http.MethodPost, "/api/v1/lessons/"+other+"/waitlist", person(me), clara))
	a.expect(http.StatusNoContent, http.MethodDelete, "/api/v1/waitlist/"+left["id"].(string), "", clara)

	if got := reason(a.expect(http.StatusConflict, http.MethodPost, "/api/v1/waitlist/"+left["id"].(string)+"/decline", "", clara)); got != "invite-used" {
		t.Errorf("decline a withdrawn entry: %s, want invite-used", got)
	}

	// Declined but staying on the list: the link says so, and takes
	// nothing.
	third := workshop(1)
	mine := decode(t, a.expect(http.StatusCreated, http.MethodPost, "/api/v1/lessons/"+third+"/bookings", person(me), clara))
	staying := decode(t, a.expect(http.StatusCreated, http.MethodPost, "/api/v1/lessons/"+third+"/waitlist",
		`{"personId":"`+ella+`","stayOnList":true}`, clara))
	a.expect(http.StatusOK, http.MethodPost, "/api/v1/bookings/"+mine["id"].(string)+"/cancel", "", clara)
	token = a.linkToken("waitlist-invite", "waitlist/accept")

	declined := decode(t, a.expect(http.StatusOK, http.MethodPost, "/api/v1/waitlist/"+staying["id"].(string)+"/decline", "", clara))
	if declined["status"] != "waiting" || declined["position"] != 1.0 {
		t.Fatalf("declined %v", declined)
	}

	view = decode(t, a.want(http.MethodGet, "/api/v1/waitlist-invites/"+token, "", http.StatusOK))
	if view["state"] != "waiting" || view["position"] != 1.0 || view["expiresAt"] != nil {
		t.Errorf("link after declining: %v", view)
	}

	if got := reason(a.want(http.MethodPost, "/api/v1/waitlist-invites/"+token, "", http.StatusConflict)); got != "invite-expired" {
		t.Errorf("accept after declining: %s, want invite-expired", got)
	}
}

// TestWaitlistInviteRateLimit limits the invite links per client: they need
// no sign-in.
func TestWaitlistInviteRateLimit(t *testing.T) {
	t.Parallel()

	a := newAuthApp(t, app.Deps{})

	for i := range 20 {
		if res, body := a.callFrom("192.0.2.7:1234", "", http.MethodGet, "/api/v1/waitlist-invites/guess"+fmt.Sprint(i), ""); res.StatusCode != http.StatusNotFound {
			t.Fatalf("attempt %d: %d %s", i, res.StatusCode, body)
		}
	}

	res, _ := a.callFrom("192.0.2.7:1234", "", http.MethodPost, "/api/v1/waitlist-invites/guess", "")
	if res.StatusCode != http.StatusTooManyRequests || res.Header.Get("Retry-After") == "" {
		t.Fatalf("attempt 21: %d, want 429 with Retry-After", res.StatusCode)
	}

	// Another client is not affected.
	if res, _ := a.callFrom("192.0.2.8:1234", "", http.MethodGet, "/api/v1/waitlist-invites/guess", ""); res.StatusCode != http.StatusNotFound {
		t.Fatalf("other client: %d", res.StatusCode)
	}
}
