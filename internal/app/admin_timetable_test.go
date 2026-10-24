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
