package app_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/app"
)

// TestAdminClasses runs the office's class writes over HTTP (ADR 0007): 201,
// 200 and 204 on success, located 422s, 409 `in-use` for a class with
// history, and the retired flag that hides a class from the public list.
func TestAdminClasses(t *testing.T) {
	t.Parallel()

	a := newAuthApp(t, app.Deps{})
	office := a.login("olga.ledgerby@example.net")
	member := a.login("clara.amberly@example.org")

	terms := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/terms", "", member))
	termID, _ := terms["items"].([]any)[0].(map[string]any)["id"].(string)
	adminList := "/api/v1/admin/terms/" + termID + "/classes"
	publicList := "/api/v1/terms/" + termID + "/classes"

	a.expect(http.StatusForbidden, http.MethodGet, adminList, "", member)

	demo := decode(t, a.expect(http.StatusOK, http.MethodGet, publicList, "", member))["items"].([]any)
	existing, _ := demo[0].(map[string]any)
	instructor, _ := existing["instructor"].(map[string]any)["id"].(string)
	body := func(roomID string, retired bool) string {
		r := "false"
		if retired {
			r = "true"
		}

		return `{"courseTypeId":"` + existing["courseTypeId"].(string) + `","roomId":"` + roomID +
			`","instructorId":"` + instructor + `","allLevels":true,"weekday":3,"startTime":"20:15",` +
			`"durationMinutes":45,"capacity":6,"retired":` + r + `}`
	}
	room, _ := existing["roomId"].(string)

	a.expect(http.StatusForbidden, http.MethodPost, publicList, body(room, false), member)
	a.expect(http.StatusNotFound, http.MethodPost, "/api/v1/terms/ffffffff-ffff-7fff-bfff-ffffffffffff/classes",
		body(room, false), office)

	unknown := decode(t, a.expect(http.StatusUnprocessableEntity, http.MethodPost, publicList,
		body("ffffffff-ffff-7fff-bfff-ffffffffffff", false), office))
	if errs, _ := unknown["errors"].([]any); len(errs) != 1 || errs[0].(map[string]any)["location"] != "body.roomId" {
		t.Errorf("422 errors %v, want body.roomId", unknown["errors"])
	}

	created := decode(t, a.expect(http.StatusCreated, http.MethodPost, publicList, body(room, false), office))
	id, _ := created["id"].(string)

	if created["startTime"] != "20:15" || created["retired"] != false || created["termId"] != termID {
		t.Errorf("created %v", created)
	}

	if !hasItem(decode(t, a.expect(http.StatusOK, http.MethodGet, publicList, "", member)), "id", id) {
		t.Fatal("public list lacks the new class")
	}

	retired := decode(t, a.expect(http.StatusOK, http.MethodPut, "/api/v1/classes/"+id, body(room, true), office))
	if retired["retired"] != true {
		t.Errorf("updated %v, want retired", retired)
	}

	if hasItem(decode(t, a.expect(http.StatusOK, http.MethodGet, publicList, "", member)), "id", id) {
		t.Error("public list shows the retired class")
	}

	if !hasItem(decode(t, a.expect(http.StatusOK, http.MethodGet, adminList, "", office)), "id", id) {
		t.Error("office list lacks the retired class")
	}

	a.expect(http.StatusNoContent, http.MethodDelete, "/api/v1/classes/"+id, "", office)
	a.expect(http.StatusNotFound, http.MethodDelete, "/api/v1/classes/"+id, "", office)

	// A demo class has lessons in the past: it can be retired, not deleted.
	used := decode(t, a.expect(http.StatusConflict, http.MethodDelete, "/api/v1/classes/"+existing["id"].(string), "", office))
	if used["reason"] != "in-use" {
		t.Errorf("409 %v, want reason in-use", used)
	}
}

// TestAdminClassAudience writes a class's audience over HTTP (ADR 0021): a
// span named the other way round is answered lower level first, and an
// audience the rules refuse is a 422 on its field.
func TestAdminClassAudience(t *testing.T) {
	t.Parallel()

	a := newAuthApp(t, app.Deps{})
	office := a.login("olga.ledgerby@example.net")
	member := a.login("clara.amberly@example.org")

	terms := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/terms", "", member))
	termID, _ := terms["items"].([]any)[0].(map[string]any)["id"].(string)
	classes := "/api/v1/terms/" + termID + "/classes"

	existing, _ := decode(t, a.expect(http.StatusOK, http.MethodGet, classes, "", member))["items"].([]any)[0].(map[string]any)
	instructor, _ := existing["instructor"].(map[string]any)["id"].(string)

	levels := map[string]map[string]any{}
	for _, l := range decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/levels", "", member))["items"].([]any) {
		m, _ := l.(map[string]any)
		levels[m["name"].(string)] = m
	}

	beginner, advanced := levels["Beginner"]["id"].(string), levels["Advanced"]["id"].(string)
	body := func(audience string) string {
		return `{"courseTypeId":"` + existing["courseTypeId"].(string) + `","roomId":"` + existing["roomId"].(string) +
			`","instructorId":"` + instructor + `",` + audience + `"weekday":3,"startTime":"20:15",` +
			`"durationMinutes":45,"capacity":6}`
	}

	created := decode(t, a.expect(http.StatusCreated, http.MethodPost, classes, body(
		`"levelFromId":"`+advanced+`","levelToId":"`+beginner+`","stage":2,"ageFromMonths":120,"ageToMonths":216,`), office))

	from, _ := created["levelFrom"].(map[string]any)
	to, _ := created["levelTo"].(map[string]any)

	if from["id"] != beginner || from["name"] != "Beginner" || from["code"] != "B" || to["id"] != advanced ||
		created["stage"] != float64(2) || created["ageFromMonths"] != float64(120) || created["ageToMonths"] != float64(216) {
		t.Errorf("created %v, want Beginner to Advanced, stage 2, 120 to 216 months", created)
	}

	if _, ok := created["allLevels"]; ok {
		t.Errorf("created %v: allLevels must be absent", created)
	}

	id, _ := created["id"].(string)

	listed := decode(t, a.expect(http.StatusOK, http.MethodGet, classes, "", member))
	for _, item := range listed["items"].([]any) {
		if c, _ := item.(map[string]any); c["id"] == id && c["levelFrom"].(map[string]any)["id"] != beginner {
			t.Errorf("listed %v, want Beginner first", c)
		}
	}

	// All levels and nothing else: every other audience field is absent.
	updated := decode(t, a.expect(http.StatusOK, http.MethodPut, "/api/v1/classes/"+id, body(`"allLevels":true,`), office))
	for _, absent := range []string{"levelFrom", "levelTo", "stage", "ageFromMonths", "ageToMonths"} {
		if _, ok := updated[absent]; ok || updated["allLevels"] != true {
			t.Errorf("updated %v, want only allLevels", updated)
		}
	}

	for name, tt := range map[string]struct{ audience, location string }{
		"all levels with a span": {
			`"allLevels":true,"levelFromId":"` + beginner + `","levelToId":"` + beginner + `",`, "body.allLevels",
		},
		"one end only":   {`"levelFromId":"` + beginner + `",`, "body.levelToId"},
		"unknown level":  {`"levelFromId":"ffffffff-ffff-7fff-bfff-ffffffffffff","levelToId":"` + beginner + `",`, "body.levelFromId"},
		"stage 10":       {`"stage":10,`, "body.stage"},
		"age 1201":       {`"ageToMonths":1201,`, "body.ageToMonths"},
		"ages backwards": {`"ageFromMonths":72,"ageToMonths":48,`, "body.ageToMonths"},
	} {
		got := decode(t, a.expect(http.StatusUnprocessableEntity, http.MethodPost, classes, body(tt.audience), office))
		if errs, _ := got["errors"].([]any); len(errs) != 1 || errs[0].(map[string]any)["location"] != tt.location {
			t.Errorf("%s: 422 errors %v, want %s", name, got["errors"], tt.location)
		}
	}
}

// TestAdminTerms runs the office's term writes over HTTP (ADR 0007): 201,
// 200 and 204 on success, a 422 on the date that would leave lessons with
// history outside the term, and 409 `in-use` for a term with classes.
func TestAdminTerms(t *testing.T) {
	t.Parallel()

	a := newAuthApp(t, app.Deps{})
	office := a.login("olga.ledgerby@example.net")
	member := a.login("clara.amberly@example.org")

	body := `{"name":"Fictional Summer","startsOn":"2030-07-01","endsOn":"2030-07-31"}`
	a.expect(http.StatusForbidden, http.MethodPost, "/api/v1/terms", body, member)

	created := decode(t, a.expect(http.StatusCreated, http.MethodPost, "/api/v1/terms", body, office))
	id, _ := created["id"].(string)

	if created["name"] != "Fictional Summer" || created["startsOn"] != "2030-07-01" || created["endsOn"] != "2030-07-31" {
		t.Errorf("created %v", created)
	}

	if !hasItem(decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/terms", "", member)), "id", id) {
		t.Fatal("term list lacks the new term")
	}

	updated := decode(t, a.expect(http.StatusOK, http.MethodPut, "/api/v1/terms/"+id,
		`{"name":"Fictional Workshop","startsOn":"2030-07-01","endsOn":"2030-07-14"}`, office))
	if updated["name"] != "Fictional Workshop" || updated["endsOn"] != "2030-07-14" {
		t.Errorf("updated %v", updated)
	}

	backwards := decode(t, a.expect(http.StatusUnprocessableEntity, http.MethodPut, "/api/v1/terms/"+id,
		`{"name":"Backwards","startsOn":"2030-07-14","endsOn":"2030-07-01"}`, office))
	if errs, _ := backwards["errors"].([]any); len(errs) != 1 || errs[0].(map[string]any)["location"] != "body.endsOn" {
		t.Errorf("422 errors %v, want body.endsOn", backwards["errors"])
	}

	a.expect(http.StatusNoContent, http.MethodDelete, "/api/v1/terms/"+id, "", office)
	a.expect(http.StatusNotFound, http.MethodDelete, "/api/v1/terms/"+id, "", office)

	// The demo term has classes with lessons in the past.
	demo, _ := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/terms", "", member))["items"].([]any)[0].(map[string]any)
	demoID, _ := demo["id"].(string)

	used := decode(t, a.expect(http.StatusConflict, http.MethodDelete, "/api/v1/terms/"+demoID, "", office))
	if used["reason"] != "in-use" {
		t.Errorf("409 %v, want reason in-use", used)
	}

	late := decode(t, a.expect(http.StatusUnprocessableEntity, http.MethodPut, "/api/v1/terms/"+demoID,
		`{"name":"Late start","startsOn":"`+demo["endsOn"].(string)+`","endsOn":"`+demo["endsOn"].(string)+`"}`, office))
	if errs, _ := late["errors"].([]any); len(errs) != 1 || errs[0].(map[string]any)["location"] != "body.startsOn" {
		t.Errorf("422 errors %v, want body.startsOn", late["errors"])
	}
}

// TestAdminClassMembers runs the office's class member writes over HTTP:
// list, add (201, 409 `duplicate`, a 422 on an unknown person, 409 `full`
// beyond the class's capacity) and remove, and the 422 on a class capacity
// below its member count.
func TestAdminClassMembers(t *testing.T) {
	t.Parallel()

	a := newAuthApp(t, app.Deps{})
	office := a.login("olga.ledgerby@example.net")
	member := a.login("clara.amberly@example.org")

	terms := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/terms", "", member))
	termID, _ := terms["items"].([]any)[0].(map[string]any)["id"].(string)
	classes := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/terms/"+termID+"/classes", "", member))
	classID, _ := classes["items"].([]any)[0].(map[string]any)["id"].(string)
	list := "/api/v1/admin/classes/" + classID + "/members"
	add := "/api/v1/classes/" + classID + "/members"

	persons := decode(t, a.expect(http.StatusOK, http.MethodPost, "/api/v1/admin/persons/search", `{"query": "clara"}`, office))
	personID, _ := persons["items"].([]any)[0].(map[string]any)["id"].(string)
	body := `{"personId":"` + personID + `","since":"2026-09-07"}`

	a.expect(http.StatusForbidden, http.MethodGet, list, "", member)
	a.expect(http.StatusForbidden, http.MethodPost, add, body, member)
	a.expect(http.StatusNotFound, http.MethodGet, "/api/v1/admin/classes/ffffffff-ffff-7fff-bfff-ffffffffffff/members", "", office)

	created := decode(t, a.expect(http.StatusCreated, http.MethodPost, add, body, office))
	if p, _ := created["person"].(map[string]any); p["id"] != personID || created["since"] != "2026-09-07" {
		t.Errorf("created %v", created)
	}

	members, _ := decode(t, a.expect(http.StatusOK, http.MethodGet, list, "", office))["items"].([]any)
	found := false

	for _, m := range members {
		if p, _ := m.(map[string]any)["person"].(map[string]any); p["id"] == personID {
			found = true
		}
	}

	if !found {
		t.Fatalf("members %v lack the new member", members)
	}

	twice := decode(t, a.expect(http.StatusConflict, http.MethodPost, add, body, office))
	if twice["reason"] != "duplicate" {
		t.Errorf("409 %v, want reason duplicate", twice)
	}

	unknown := decode(t, a.expect(http.StatusUnprocessableEntity, http.MethodPost, add,
		`{"personId":"ffffffff-ffff-7fff-bfff-ffffffffffff"}`, office))
	if errs, _ := unknown["errors"].([]any); len(errs) != 1 || errs[0].(map[string]any)["location"] != "body.personId" {
		t.Errorf("422 errors %v, want body.personId", unknown["errors"])
	}

	// The office pauses the membership and ends the pause.
	a.expect(http.StatusForbidden, http.MethodPut, add+"/"+personID, `{}`, member)

	paused := decode(t, a.expect(http.StatusOK, http.MethodPut, add+"/"+personID,
		`{"since":"2026-09-07","pausedUntil":"2026-12-24"}`, office))
	if paused["pausedUntil"] != "2026-12-24" || paused["since"] != "2026-09-07" {
		t.Errorf("paused %v", paused)
	}

	resumed := decode(t, a.expect(http.StatusOK, http.MethodPut, add+"/"+personID, `{"since":"2026-09-07"}`, office))
	if _, ok := resumed["pausedUntil"]; ok {
		t.Errorf("resumed %v, want no pause", resumed)
	}

	a.expect(http.StatusNoContent, http.MethodDelete, add+"/"+personID, "", office)
	a.expect(http.StatusNotFound, http.MethodDelete, add+"/"+personID, "", office)
	a.expect(http.StatusNotFound, http.MethodPut, add+"/"+personID, `{}`, office)

	// A class refuses members beyond its capacity, and a capacity below its
	// member count (ADR 0009).
	demo, _ := classes["items"].([]any)[0].(map[string]any)
	instructor, _ := demo["instructor"].(map[string]any)["id"].(string)
	classBody := func(capacity string) string {
		return `{"courseTypeId":"` + demo["courseTypeId"].(string) + `","roomId":"` + demo["roomId"].(string) +
			`","instructorId":"` + instructor + `","allLevels":true,"weekday":3,"startTime":"20:15",` +
			`"durationMinutes":45,"capacity":` + capacity + `,"retired":false}`
	}
	small := decode(t, a.expect(http.StatusCreated, http.MethodPost, "/api/v1/terms/"+termID+"/classes", classBody("1"), office))
	smallID, _ := small["id"].(string)
	smallAdd := "/api/v1/classes/" + smallID + "/members"

	a.expect(http.StatusCreated, http.MethodPost, smallAdd, body, office)

	others := decode(t, a.expect(http.StatusOK, http.MethodPost, "/api/v1/admin/persons/search", `{"query": "hanna"}`, office))
	otherID, _ := others["items"].([]any)[0].(map[string]any)["id"].(string)

	full := decode(t, a.expect(http.StatusConflict, http.MethodPost, smallAdd, `{"personId":"`+otherID+`"}`, office))
	if full["reason"] != "full" {
		t.Errorf("409 %v, want reason full", full)
	}

	a.expect(http.StatusOK, http.MethodPut, "/api/v1/classes/"+smallID, classBody("2"), office)
	a.expect(http.StatusCreated, http.MethodPost, smallAdd, `{"personId":"`+otherID+`"}`, office)

	below := decode(t, a.expect(http.StatusUnprocessableEntity, http.MethodPut, "/api/v1/classes/"+smallID, classBody("1"), office))
	if errs, _ := below["errors"].([]any); len(errs) != 1 || errs[0].(map[string]any)["location"] != "body.capacity" {
		t.Errorf("422 errors %v, want body.capacity", below["errors"])
	}

	// A retired class takes no members.
	a.expect(http.StatusOK, http.MethodPut, "/api/v1/classes/"+smallID,
		strings.Replace(classBody("3"), `"retired":false`, `"retired":true`, 1), office)

	retired := decode(t, a.expect(http.StatusUnprocessableEntity, http.MethodPost, smallAdd, `{"personId":"`+personID+`"}`, office))
	if errs, _ := retired["errors"].([]any); len(errs) != 1 || errs[0].(map[string]any)["location"] != "path.classId" {
		t.Errorf("422 errors %v, want path.classId", retired["errors"])
	}

	retired = decode(t, a.expect(http.StatusUnprocessableEntity, http.MethodPut, smallAdd+"/"+personID, `{}`, office))
	if errs, _ := retired["errors"].([]any); len(errs) != 1 || errs[0].(map[string]any)["location"] != "path.classId" {
		t.Errorf("update in a retired class: 422 errors %v, want path.classId", retired["errors"])
	}
}
