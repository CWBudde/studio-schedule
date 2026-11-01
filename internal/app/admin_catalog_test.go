package app_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
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

// TestAdminRoomOrder runs the office's room order over HTTP: new rooms go
// to the end, a reorder is a 204 that listRooms shows, a list that is not
// exactly the location's rooms is a 422 on body.roomIds, an unknown
// location a 404, and a member gets a 403.
func TestAdminRoomOrder(t *testing.T) {
	t.Parallel()

	a := newAuthApp(t, app.Deps{})
	office := a.login("olga.ledgerby@example.net")
	member := a.login("clara.amberly@example.org")

	loc := decode(t, a.expect(http.StatusCreated, http.MethodPost, "/api/v1/locations", `{"name":"West Studio"}`, office))
	locID, _ := loc["id"].(string)
	path := "/api/v1/locations/" + locID + "/room-order"

	var ids []string

	for i, name := range []string{"Zeta", "Alpha"} {
		room := decode(t, a.expect(http.StatusCreated, http.MethodPost, "/api/v1/rooms",
			`{"locationId":"`+locID+`","name":"`+name+`","capacity":12}`, office))
		if room["position"] != float64(i+1) {
			t.Fatalf("created %v, want position %d", room, i+1)
		}

		id, _ := room["id"].(string)
		ids = append(ids, id)
	}

	order := func(ids ...string) string {
		body, err := json.Marshal(map[string][]string{"roomIds": ids})
		if err != nil {
			t.Fatal(err)
		}

		return string(body)
	}
	listed := func() []string {
		t.Helper()

		var got []string

		for _, item := range decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/rooms?location="+locID, "", office))["items"].([]any) {
			r, _ := item.(map[string]any)
			got = append(got, fmt.Sprintf("%v %v", r["position"], r["name"]))
		}

		return got
	}

	if got := listed(); !slices.Equal(got, []string{"1 Zeta", "2 Alpha"}) {
		t.Fatalf("rooms %q before the reorder", got)
	}

	a.expect(http.StatusForbidden, http.MethodPut, path, order(ids[1], ids[0]), member)
	a.expect(http.StatusNoContent, http.MethodPut, path, order(ids[1], ids[0]), office)

	if got := listed(); !slices.Equal(got, []string{"1 Alpha", "2 Zeta"}) {
		t.Fatalf("rooms %q after the reorder, want Alpha first", got)
	}

	for name, body := range map[string]string{
		"missing room":  order(ids[1]),
		"repeated room": order(ids[1], ids[0], ids[0]),
		"null":          `{"roomIds":null}`,
	} {
		invalid := decode(t, a.expect(http.StatusUnprocessableEntity, http.MethodPut, path, body, office))
		if errs, _ := invalid["errors"].([]any); len(errs) != 1 || errs[0].(map[string]any)["location"] != "body.roomIds" {
			t.Errorf("%s: 422 errors %v, want body.roomIds", name, invalid["errors"])
		}
	}

	// Without the list at all.
	a.expect(http.StatusUnprocessableEntity, http.MethodPut, path, `{}`, office)

	a.expect(http.StatusNotFound, http.MethodPut, "/api/v1/locations/ffffffff-ffff-7fff-bfff-ffffffffffff/room-order",
		`{"roomIds":[]}`, office)

	if got := listed(); !slices.Equal(got, []string{"1 Alpha", "2 Zeta"}) {
		t.Fatalf("rooms %q after the refused orders", got)
	}
}

// TestAdminCourseTypes runs the office's course type writes over HTTP.
func TestAdminCourseTypes(t *testing.T) {
	t.Parallel()

	a := newAuthApp(t, app.Deps{})
	office := a.login("olga.ledgerby@example.net")
	member := a.login("clara.amberly@example.org")

	a.expect(http.StatusForbidden, http.MethodPost, "/api/v1/course-types", `{"name":"Nope"}`, member)

	created := decode(t, a.expect(http.StatusCreated, http.MethodPost, "/api/v1/course-types",
		`{"name":"Salsa","description":"Partner work."}`, office))
	id, _ := created["id"].(string)

	if created["name"] != "Salsa" || created["description"] != "Partner work." || id == "" {
		t.Fatalf("created %v", created)
	}

	dup := decode(t, a.expect(http.StatusConflict, http.MethodPost, "/api/v1/course-types", `{"name":"TAP"}`, office))
	if dup["reason"] != "duplicate" {
		t.Errorf("409 %v, want reason duplicate", dup)
	}

	invalid := decode(t, a.expect(http.StatusUnprocessableEntity, http.MethodPut, "/api/v1/course-types/"+id, `{"name":" "}`, office))
	if errs, _ := invalid["errors"].([]any); len(errs) != 1 || errs[0].(map[string]any)["location"] != "body.name" {
		t.Errorf("422 errors %v, want body.name", invalid["errors"])
	}

	a.expect(http.StatusOK, http.MethodPut, "/api/v1/course-types/"+id, `{"name":"Salsa Cubana"}`, office)

	list := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/course-types", "", office))
	if !hasItem(list, "name", "Salsa Cubana") {
		t.Errorf("public list %v lacks the renamed course type", list)
	}

	a.expect(http.StatusNoContent, http.MethodDelete, "/api/v1/course-types/"+id, "", office)
	a.expect(http.StatusNotFound, http.MethodDelete, "/api/v1/course-types/"+id, "", office)

	used := decode(t, a.expect(http.StatusConflict, http.MethodDelete, "/api/v1/course-types/"+demoClassField(t, a, office, "courseTypeId"), "", office))
	if used["reason"] != "in-use" {
		t.Errorf("409 %v, want reason in-use", used)
	}
}

// TestAdminStaff runs the office's staff list and writes over HTTP: a
// deactivated instructor leaves the public list but stays in the office's.
func TestAdminStaff(t *testing.T) {
	t.Parallel()

	a := newAuthApp(t, app.Deps{})
	office := a.login("olga.ledgerby@example.net")
	member := a.login("clara.amberly@example.org")

	a.expect(http.StatusForbidden, http.MethodGet, "/api/v1/admin/staff", "", member)
	a.expect(http.StatusForbidden, http.MethodPost, "/api/v1/admin/persons/search", `{"query": "clara"}`, member)

	persons := decode(t, a.expect(http.StatusOK, http.MethodPost, "/api/v1/admin/persons/search", `{"query": "clara amberly"}`, office))
	items, _ := persons["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("person search %v, want one match", persons)
	}

	clara, _ := items[0].(map[string]any)["id"].(string)
	if _, ok := items[0].(map[string]any)["phone"]; ok {
		t.Error("person search exposes the phone number")
	}

	unknown := decode(t, a.expect(http.StatusUnprocessableEntity, http.MethodPost, "/api/v1/staff",
		`{"displayName":"Ivy Lanthorn","kind":"instructor","personId":"ffffffff-ffff-7fff-bfff-ffffffffffff","active":true}`, office))
	if errs, _ := unknown["errors"].([]any); len(errs) != 1 || errs[0].(map[string]any)["location"] != "body.personId" {
		t.Errorf("422 errors %v, want body.personId", unknown["errors"])
	}

	created := decode(t, a.expect(http.StatusCreated, http.MethodPost, "/api/v1/staff",
		`{"displayName":"Ivy Lanthorn","kind":"instructor","active":true}`, office))
	id, _ := created["id"].(string)

	if _, ok := created["person"]; ok || created["active"] != true {
		t.Errorf("created %v, want no person and active", created)
	}

	linked := decode(t, a.expect(http.StatusOK, http.MethodPut, "/api/v1/staff/"+id,
		`{"displayName":"Ivy Lanthorn","kind":"accompanist","personId":"`+clara+`","active":true}`, office))
	if person, _ := linked["person"].(map[string]any); person["id"] != clara || person["familyName"] != "Amberly" ||
		linked["kind"] != "accompanist" {
		t.Errorf("updated %v", linked)
	}

	a.expect(http.StatusNoContent, http.MethodDelete, "/api/v1/staff/"+id, "", office)

	// A demo instructor with classes can be deactivated, not deleted.
	instructor := demoClassInstructor(t, a, office)

	used := decode(t, a.expect(http.StatusConflict, http.MethodDelete, "/api/v1/staff/"+instructor, "", office))
	if used["reason"] != "in-use" {
		t.Errorf("409 %v, want reason in-use", used)
	}

	if !hasItem(decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/staff", "", office)), "id", instructor) {
		t.Fatal("public staff list lacks the active instructor")
	}

	a.expect(http.StatusOK, http.MethodPut, "/api/v1/staff/"+instructor,
		`{"displayName":"Former Instructor","kind":"instructor","active":false}`, office)

	if hasItem(decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/staff", "", office)), "id", instructor) {
		t.Error("public staff list shows the deactivated instructor")
	}

	// The timetable still names them on their classes.
	terms := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/terms", "", office))
	termID, _ := terms["items"].([]any)[0].(map[string]any)["id"].(string)
	classes := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/terms/"+termID+"/classes", "", office))

	if first, _ := classes["items"].([]any)[0].(map[string]any)["instructor"].(map[string]any); first["displayName"] != "Former Instructor" {
		t.Errorf("class instructor %v, want the deactivated instructor's new name", first)
	}

	all := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/admin/staff", "", office))
	if !hasItem(all, "id", instructor) {
		t.Error("office staff list lacks the deactivated instructor")
	}
}

// demoClassField returns a field of the first class of the first demo term.
func demoClassField(t *testing.T, a *authApp, cookie *http.Cookie, field string) string {
	t.Helper()

	terms := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/terms", "", cookie))
	termID, _ := terms["items"].([]any)[0].(map[string]any)["id"].(string)
	classes := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/terms/"+termID+"/classes", "", cookie))
	value, _ := classes["items"].([]any)[0].(map[string]any)[field].(string)

	return value
}

// demoClassInstructor returns the instructor ID of the first class of the
// first demo term.
func demoClassInstructor(t *testing.T, a *authApp, cookie *http.Cookie) string {
	t.Helper()

	terms := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/terms", "", cookie))
	termID, _ := terms["items"].([]any)[0].(map[string]any)["id"].(string)
	classes := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/terms/"+termID+"/classes", "", cookie))
	instructor, _ := classes["items"].([]any)[0].(map[string]any)["instructor"].(map[string]any)
	id, _ := instructor["id"].(string)

	return id
}

// hasItem reports whether a list response has an item whose field is value.
func hasItem(list map[string]any, field, value string) bool {
	items, _ := list["items"].([]any)
	for _, item := range items {
		if m, _ := item.(map[string]any); m[field] == value {
			return true
		}
	}

	return false
}

// TestAdminLevels runs the office's level writes and the level order over
// HTTP (ADR 0021). It finds the demo's levels, if any, through listLevels,
// so it holds whether the seed has levels or not.
func TestAdminLevels(t *testing.T) {
	t.Parallel()

	a := newAuthApp(t, app.Deps{})
	office := a.login("olga.ledgerby@example.net")
	member := a.login("clara.amberly@example.org")

	// listed returns the levels as "position name" and their IDs, in the
	// order of listLevels, read signed out.
	listed := func() ([]string, []string) {
		t.Helper()

		res, body := a.call(http.MethodGet, "/api/v1/levels", "")
		if res.StatusCode != http.StatusOK {
			t.Fatalf("listLevels signed out: %d %s", res.StatusCode, body)
		}

		var names, levelIDs []string

		for _, item := range decode(t, body)["items"].([]any) {
			l, _ := item.(map[string]any)
			names = append(names, fmt.Sprintf("%v %v", l["position"], l["name"]))
			levelIDs = append(levelIDs, l["id"].(string))
		}

		return names, levelIDs
	}
	order := func(ids ...string) string {
		body, err := json.Marshal(map[string][]string{"levelIds": ids})
		if err != nil {
			t.Fatal(err)
		}

		return string(body)
	}
	wantField := func(what string, body []byte, field string) {
		t.Helper()

		invalid := decode(t, body)
		if errs, _ := invalid["errors"].([]any); len(errs) != 1 || errs[0].(map[string]any)["location"] != field {
			t.Errorf("%s: 422 errors %v, want %s", what, invalid["errors"], field)
		}
	}

	_, before := listed()

	a.expect(http.StatusForbidden, http.MethodPost, "/api/v1/levels", `{"name":"Nope"}`, member)

	created := decode(t, a.expect(http.StatusCreated, http.MethodPost, "/api/v1/levels",
		`{"name":" Masterclass ","code":"MC"}`, office))
	id, _ := created["id"].(string)

	if created["name"] != "Masterclass" || created["code"] != "MC" || created["position"] != float64(len(before)+1) || id == "" {
		t.Fatalf("created %v, want Masterclass, MC after the %d levels", created, len(before))
	}

	plain := decode(t, a.expect(http.StatusCreated, http.MethodPost, "/api/v1/levels", `{"name":"Showcase"}`, office))
	if _, ok := plain["code"]; ok {
		t.Errorf("created %v, want no code", plain)
	}

	dup := decode(t, a.expect(http.StatusConflict, http.MethodPost, "/api/v1/levels", `{"name":"MASTERCLASS"}`, office))
	if dup["reason"] != "duplicate" {
		t.Errorf("409 %v, want reason duplicate", dup)
	}

	wantField("long name", a.expect(http.StatusUnprocessableEntity, http.MethodPost, "/api/v1/levels",
		`{"name":"`+strings.Repeat("x", 61)+`"}`, office), "body.name")
	wantField("blank name", a.expect(http.StatusUnprocessableEntity, http.MethodPut, "/api/v1/levels/"+id,
		`{"name":" "}`, office), "body.name")

	updated := decode(t, a.expect(http.StatusOK, http.MethodPut, "/api/v1/levels/"+id, `{"name":"Master Class","code":"MC"}`, office))
	if updated["name"] != "Master Class" || updated["position"] != created["position"] {
		t.Errorf("updated %v, want the new name at position %v", updated, created["position"])
	}

	a.expect(http.StatusNotFound, http.MethodPut, "/api/v1/levels/ffffffff-ffff-7fff-bfff-ffffffffffff", `{"name":"Gone"}`, office)

	// Move the new level to the front.
	names, all := listed()
	if got := names[len(names)-2:]; !slices.Equal(got, []string{
		fmt.Sprintf("%d Master Class", len(before)+1), fmt.Sprintf("%d Showcase", len(before)+2),
	}) {
		t.Fatalf("levels %q, want Master Class and Showcase last", names)
	}

	moved := append([]string{id}, slices.DeleteFunc(slices.Clone(all), func(s string) bool { return s == id })...)
	a.expect(http.StatusForbidden, http.MethodPut, "/api/v1/level-order", order(moved...), member)
	a.expect(http.StatusNoContent, http.MethodPut, "/api/v1/level-order", order(moved...), office)

	names, after := listed()
	if !slices.Equal(after, moved) || names[0] != "1 Master Class" {
		t.Fatalf("levels %q after the reorder, want Master Class first", names)
	}

	for name, body := range map[string]string{
		"missing level":  order(moved[1:]...),
		"repeated level": order(append(slices.Clone(moved), id)...),
		"null":           `{"levelIds":null}`,
	} {
		wantField(name, a.expect(http.StatusUnprocessableEntity, http.MethodPut, "/api/v1/level-order", body, office), "body.levelIds")
	}

	if names, _ := listed(); names[0] != "1 Master Class" {
		t.Fatalf("levels %q after the refused orders", names)
	}

	a.expect(http.StatusForbidden, http.MethodDelete, "/api/v1/levels/"+id, "", member)
	a.expect(http.StatusNoContent, http.MethodDelete, "/api/v1/levels/"+id, "", office)
	a.expect(http.StatusNotFound, http.MethodDelete, "/api/v1/levels/"+id, "", office)

	if _, left := listed(); slices.Contains(left, id) || len(left) != len(before)+1 {
		t.Fatalf("levels %v after the delete", left)
	}
}
