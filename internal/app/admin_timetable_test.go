package app_test

import (
	"net/http"
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
			`","instructorId":"` + instructor + `","level":"open","weekday":3,"startTime":"20:15",` +
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
// list, add (201, 409 `duplicate`, a 422 on an unknown person) and remove.
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

	persons := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/admin/persons?q=clara", "", office))
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

	a.expect(http.StatusNoContent, http.MethodDelete, add+"/"+personID, "", office)
	a.expect(http.StatusNotFound, http.MethodDelete, add+"/"+personID, "", office)
}
