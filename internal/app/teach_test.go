package app_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/app"
)

// TestTeach runs the instructor view over HTTP (ADR 0009, amendment): who
// may list and notify, the lessons and their counts, the notice in the
// mailbox with its Reply-To, the 422 on the message and the 429 with
// Retry-After. The rules at their edges are tested in package service.
func TestTeach(t *testing.T) {
	t.Parallel()

	a := newAuthApp(t, app.Deps{Demo: true})
	office := a.login("olga.ledgerby@example.net")
	alina := a.login("alina.starling@example.net")
	felix := a.login("felix.keysworth@example.net")
	clara := a.login("clara.amberly@example.org")
	me := a.family(clara).Me.ID

	staff := map[string]string{}
	for _, s := range decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/staff", "", clara))["items"].([]any) {
		m, _ := s.(map[string]any)
		staff[m["displayName"].(string)], _ = m["id"].(string)
	}

	terms := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/terms", "", clara))
	termID, _ := terms["items"].([]any)[0].(map[string]any)["id"].(string)
	classes := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/terms/"+termID+"/classes", "", clara))
	existing, _ := classes["items"].([]any)[0].(map[string]any)

	// The test clock is Friday 2026-10-23, 16:00 UTC (18:00 in Berlin).
	lesson := func(instructor, accompanist string) string {
		l := decode(t, a.expect(http.StatusCreated, http.MethodPost, "/api/v1/lessons", fmt.Sprintf(
			`{"courseTypeId":%q,"date":"2026-10-24","startTime":"20:30","durationMinutes":60,"roomId":%q,`+
				`"instructorId":%q,"accompanistId":%q,"capacity":4,"visibility":"hidden","kind":"private",`+
				`"confirmRoomOverlap":true}`,
			existing["courseTypeId"], existing["roomId"], staff[instructor], staff[accompanist]), office))
		id, _ := l["id"].(string)

		return id
	}

	own := lesson("Alina Starling", "Felix Keysworth")
	other := lesson("Sofia Quillon", "Greta Ivorylane")

	participants := "/api/v1/lessons/" + own + "/participants"
	a.expect(http.StatusCreated, http.MethodPost, participants, `{"personId":"`+me+`"}`, alina)
	a.expect(http.StatusCreated, http.MethodPost, participants,
		`{"guest":{"name":"Gina Guest","email":"gina@example.org"},"trial":true}`, alina)
	a.expect(http.StatusCreated, http.MethodPost, participants, `{"guest":{"name":"Walter Walk-in"}}`, felix)

	// Members may not, signed-out visitors neither.
	a.expect(http.StatusForbidden, http.MethodGet, "/api/v1/teach", "", clara)
	if res, _ := a.call(http.MethodGet, "/api/v1/teach", ""); res.StatusCode != http.StatusUnauthorized {
		t.Errorf("signed out: %d", res.StatusCode)
	}
	a.expect(http.StatusForbidden, http.MethodPost, "/api/v1/lessons/"+own+"/notify", `{"message":"Hi"}`, clara)

	find := func(cookie *http.Cookie, query, id string) map[string]any {
		t.Helper()

		body := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/teach"+query, "", cookie))
		for _, l := range body["lessons"].([]any) {
			if m, _ := l.(map[string]any); m["id"] == id {
				return m
			}
		}

		return nil
	}

	l := find(felix, "", own)
	if l == nil || l["role"] != "accompanist" || l["active"] != 3.0 || l["trials"] != 1.0 || l["guests"] != 2.0 ||
		l["freePlaces"] != 1.0 || l["attendanceOpen"] != false || l["attendanceTaken"] != false ||
		l["dayOverAt"] != "2026-10-24T22:00:00Z" {
		t.Fatalf("felix's lesson %v", l)
	}

	if ct, _ := l["courseType"].(map[string]any); ct["name"] == nil || l["instructor"] == nil || l["room"] == nil {
		t.Errorf("summary %v", l)
	}

	if find(felix, "", other) != nil || find(alina, "?staffId="+staff["Sofia Quillon"], own) != nil {
		t.Error("an instructor sees others' lessons")
	}

	if l := find(alina, "?from=2026-10-24&to=2026-10-25", own); l == nil || l["role"] != "instructor" {
		t.Errorf("alina's lesson %v", l)
	}

	if l := find(office, "?staffId="+staff["Sofia Quillon"], other); l == nil || l["role"] != "office" {
		t.Errorf("office's view %v", l)
	}

	window := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/teach", "", alina))
	if window["from"] != "2026-10-16" || window["to"] != "2026-10-31" || window["timezone"] != "Europe/Berlin" {
		t.Errorf("default window %v – %v", window["from"], window["to"])
	}

	a.expect(http.StatusUnprocessableEntity, http.MethodGet, "/api/v1/teach?from=2026-10-01&to=2026-11-02", "", alina)

	// Alina sends a notice: Clara and the guest with an email get it, replies
	// go to Alina.
	notify := "/api/v1/lessons/" + own + "/notify"

	sent := decode(t, a.expect(http.StatusOK, http.MethodPost, notify, `{"message":"Bring water.\nSee you!"}`, alina))
	if sent["recipients"] != 2.0 {
		t.Errorf("sent %v", sent)
	}

	res, body := a.call(http.MethodGet, "/api/v1/demo/mailbox", "")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("mailbox: %d", res.StatusCode)
	}

	var mb struct {
		Mails []struct{ To, Text, Template, ReplyTo string } `json:"mails"`
	}
	if err := json.Unmarshal(body, &mb); err != nil {
		t.Fatal(err)
	}

	to := map[string]bool{}

	for _, m := range mb.Mails {
		if m.Template != "lesson-notice" {
			continue
		}

		to[m.To] = true

		if m.ReplyTo != "alina.starling@example.net" || !strings.Contains(m.Text, "Bring water.\nSee you!") {
			t.Errorf("notice to %s, reply-to %s:\n%s", m.To, m.ReplyTo, m.Text)
		}
	}

	if len(to) != 2 || !to["clara.amberly@example.org"] || !to["gina@example.org"] {
		t.Errorf("notices to %v", to)
	}

	location := func(body []byte) string {
		errs, _ := decode(t, body)["errors"].([]any)
		if len(errs) != 1 {
			return ""
		}

		l, _ := errs[0].(map[string]any)["location"].(string)

		return l
	}

	for _, msg := range []string{
		`""`, `" \n "`, `"` + strings.Repeat("a", 1001) + `"`, `"` + strings.Repeat("a", 4001) + `"`, `"Hi \u202e"`,
	} {
		if got := location(a.expect(http.StatusUnprocessableEntity, http.MethodPost, notify, `{"message":`+msg+`}`,
			alina)); got != "body.message" {
			t.Errorf("message %.20s: 422 at %q", msg, got)
		}
	}

	a.expect(http.StatusNotFound, http.MethodPost, "/api/v1/lessons/"+other+"/notify", `{"message":"Hi"}`, felix)

	// Three notices a lesson a day: the fourth, by anyone, is a 429.
	// 1000 characters once trimmed.
	a.expect(http.StatusOK, http.MethodPost, notify, `{"message":"\r\n  `+strings.Repeat("a", 1000)+`  \r\n"}`, felix)
	a.expect(http.StatusOK, http.MethodPost, notify, `{"message":"Third"}`, office)

	res, body = a.call(http.MethodPost, notify, `{"message":"Fourth"}`, office)
	wantTooMany(t, res, body)

	if p := decode(t, body); p["status"] != 429.0 || !strings.Contains(p["detail"].(string), "too many notices") {
		t.Errorf("problem %v", p)
	}
}
