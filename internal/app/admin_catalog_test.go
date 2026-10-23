package app_test

import (
	"net/http"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/app"
)

// TestAdminLocations runs the office's location and room writes over HTTP
// (ADR 0007): 201/200/204 on success, a located 422, and 409 reasons.
func TestAdminLocations(t *testing.T) {
	t.Parallel()

	a := newAuthApp(t, app.Deps{})
	office := a.login("olga.ledgerby@example.net")
	member := a.login("clara.amberly@example.org")

	a.expect(http.StatusForbidden, http.MethodPost, "/api/v1/locations", `{"name":"Nope"}`, member)

	loc := decode(t, a.expect(http.StatusCreated, http.MethodPost, "/api/v1/locations",
		`{"name":"North Studio","address":"Example Road 2"}`, office))
	locID, _ := loc["id"].(string)

	if loc["name"] != "North Studio" || loc["address"] != "Example Road 2" || locID == "" {
		t.Fatalf("created %v", loc)
	}

	// A blank name passes the schema but not the domain rule; both name the field.
	invalid := decode(t, a.expect(http.StatusUnprocessableEntity, http.MethodPost, "/api/v1/locations", `{"name":" "}`, office))
	if errs, _ := invalid["errors"].([]any); len(errs) != 1 || errs[0].(map[string]any)["location"] != "body.name" {
		t.Errorf("422 errors %v, want body.name", invalid["errors"])
	}

	dup := decode(t, a.expect(http.StatusConflict, http.MethodPost, "/api/v1/locations", `{"name":"north studio"}`, office))
	if dup["reason"] != "duplicate" {
		t.Errorf("409 %v, want reason duplicate", dup)
	}

	a.expect(http.StatusOK, http.MethodPut, "/api/v1/locations/"+locID, `{"name":"North Hall"}`, office)

	room := decode(t, a.expect(http.StatusCreated, http.MethodPost, "/api/v1/rooms",
		`{"locationId":"`+locID+`","name":"Studio 1","capacity":12}`, office))
	roomID, _ := room["id"].(string)

	unknown := decode(t, a.expect(http.StatusUnprocessableEntity, http.MethodPost, "/api/v1/rooms",
		`{"locationId":"ffffffff-ffff-7fff-bfff-ffffffffffff","name":"Studio 2","capacity":5}`, office))
	if errs, _ := unknown["errors"].([]any); len(errs) != 1 || errs[0].(map[string]any)["location"] != "body.locationId" {
		t.Errorf("422 errors %v, want body.locationId", unknown["errors"])
	}

	inUse := decode(t, a.expect(http.StatusConflict, http.MethodDelete, "/api/v1/locations/"+locID, "", office))
	if inUse["reason"] != "in-use" {
		t.Errorf("409 %v, want reason in-use", inUse)
	}

	a.expect(http.StatusOK, http.MethodPut, "/api/v1/rooms/"+roomID,
		`{"locationId":"`+locID+`","name":"Studio 1","capacity":20}`, office)
	a.expect(http.StatusNoContent, http.MethodDelete, "/api/v1/rooms/"+roomID, "", office)
	a.expect(http.StatusNoContent, http.MethodDelete, "/api/v1/locations/"+locID, "", office)
	a.expect(http.StatusNotFound, http.MethodDelete, "/api/v1/locations/"+locID, "", office)

	// A demo room is used by the timetable.
	terms := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/terms", "", office))
	termID, _ := terms["items"].([]any)[0].(map[string]any)["id"].(string)
	classes := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/terms/"+termID+"/classes", "", office))
	usedRoom, _ := classes["items"].([]any)[0].(map[string]any)["roomId"].(string)

	used := decode(t, a.expect(http.StatusConflict, http.MethodDelete, "/api/v1/rooms/"+usedRoom, "", office))
	if used["reason"] != "in-use" || used["detail"] != "the record is still in use" {
		t.Errorf("409 %v, want reason in-use", used)
	}
}
