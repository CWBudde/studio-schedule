package conformance

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func catalogAdministration(t *testing.T, b *backend) {
	t.Helper()

	office := browser(t)
	login(t, b, office, "olga.ledgerby@example.net", demoPassword)

	create := request{method: http.MethodPost, path: "/api/v1/locations", body: `{"name":" Conformance Studio ","address":" Fixture road "}`}
	b.call(t, browser(t), create, http.StatusUnauthorized)
	location := read[struct{ ID, Name, Address string }](t, b.call(t, office, create, http.StatusCreated))
	if location.ID == "" || location.Name != "Conformance Studio" || location.Address != "Fixture road" {
		t.Fatalf("unexpected location: %+v", location)
	}
	create.body = `{"name":"conformance studio"}`
	expectConflict(t, b, office, create, "duplicate")

	checkCatalogRooms(t, b, office, location.ID)
	checkCatalogLevels(t, b, office)
	checkCatalogStaff(t, b, office)

	create = request{method: http.MethodPost, path: "/api/v1/course-types", body: `{"name":" Fixture technique ","description":" Description "}`}
	type course struct{ ID, Name, Description string }
	style := read[course](t, b.call(t, office, create, http.StatusCreated))
	if style.ID == "" || style.Name != "Fixture technique" || style.Description != "Description" {
		t.Fatalf("unexpected course type: %+v", style)
	}
	create.body = `{"name":"fixture technique"}`
	expectConflict(t, b, office, create, "duplicate")
	update := request{method: http.MethodPut, path: create.path + "/{courseTypeId}", params: map[string]string{"courseTypeId": style.ID}, body: `{"name":"New technique"}`}
	if got := read[course](t, b.call(t, office, update, http.StatusOK)); got.ID != style.ID || got.Name != "New technique" || got.Description != "" {
		t.Fatalf("unexpected updated course type: %+v", got)
	}
	update.method, update.body = http.MethodDelete, ""
	b.call(t, office, update, http.StatusNoContent)
	b.call(t, office, update, http.StatusNotFound)
}

func checkCatalogRooms(t *testing.T, b *backend, office *http.Client, location string) {
	t.Helper()

	type room struct {
		ID, LocationID, Name string
		Capacity, Position   int
	}
	create := request{method: http.MethodPost, path: "/api/v1/rooms"}
	create.body = fmt.Sprintf(`{"locationId":%q,"name":"First room","capacity":5}`, location)
	first := read[room](t, b.call(t, office, create, http.StatusCreated))
	create.body = strings.Replace(create.body, "First room", "Second room", 1)
	second := read[room](t, b.call(t, office, create, http.StatusCreated))
	if first.Position != 1 || second.Position != 2 || first.LocationID != location || second.LocationID != location {
		t.Fatalf("rooms not appended to their location: %+v, %+v", first, second)
	}
	removeLocation := request{method: http.MethodDelete, path: "/api/v1/locations/{locationId}", params: map[string]string{"locationId": location}}
	expectConflict(t, b, office, removeLocation, "in-use")
	order := request{
		method: http.MethodPut, path: "/api/v1/locations/{locationId}/room-order", params: removeLocation.params,
		body: fmt.Sprintf(`{"roomIds":[%q,%q]}`, first.ID, first.ID),
	}
	b.call(t, office, order, http.StatusUnprocessableEntity)
	order.body = fmt.Sprintf(`{"roomIds":[%q,%q]}`, second.ID, first.ID)
	b.call(t, office, order, http.StatusNoContent)
	list := request{method: http.MethodGet, path: create.path, query: url.Values{"location": {location}}}
	got := read[struct{ Items []room }](t, b.call(t, office, list, http.StatusOK)).Items
	if len(got) != 2 || got[0].ID != second.ID || got[0].Position != 1 || got[1].ID != first.ID || got[1].Position != 2 {
		t.Fatalf("unexpected room order: %+v", got)
	}
	for _, id := range []string{first.ID, second.ID} {
		b.call(t, office, request{method: http.MethodDelete, path: create.path + "/{roomId}", params: map[string]string{"roomId": id}}, http.StatusNoContent)
	}
	b.call(t, office, removeLocation, http.StatusNoContent)
}

func checkCatalogLevels(t *testing.T, b *backend, office *http.Client) {
	t.Helper()

	type level struct {
		ID, Name, Code string
		Position       int
	}
	const path = "/api/v1/levels"
	previous := read[struct{ Items []level }](t, get(t, b, office, path)).Items
	create := request{method: http.MethodPost, path: path, body: `{"name":" Fixture Level ","code":" FX "}`}
	added := read[level](t, b.call(t, office, create, http.StatusCreated))
	if added.Name != "Fixture Level" || added.Code != "FX" || (len(previous) > 0 && added.Position <= previous[len(previous)-1].Position) {
		t.Fatalf("unexpected appended level: %+v", added)
	}
	create.body = `{"name":"Another level","code":"fx"}`
	expectConflict(t, b, office, create, "duplicate")
	ids := []string{fmt.Sprintf("%q", added.ID)}
	for _, old := range previous {
		ids = append(ids, fmt.Sprintf("%q", old.ID))
	}
	b.call(t, office, request{method: http.MethodPut, path: "/api/v1/level-order", body: `{"levelIds":[` + strings.Join(ids, ",") + `]}`}, http.StatusNoContent)
	got := read[struct{ Items []level }](t, get(t, b, office, path)).Items
	if len(got) != len(previous)+1 || got[0].ID != added.ID || got[0].Position != 1 {
		t.Fatalf("unexpected updated scale: %+v", got)
	}
	b.call(t, office, request{method: http.MethodDelete, path: path + "/{levelId}", params: map[string]string{"levelId": added.ID}}, http.StatusNoContent)
}

func checkCatalogStaff(t *testing.T, b *backend, office *http.Client) {
	t.Helper()

	const path = "/api/v1/staff"
	type person struct{ ID, GivenName, FamilyName string }
	me := read[struct{ Me person }](t, get(t, b, office, "/api/v1/family")).Me
	type staff struct {
		ID, DisplayName, Kind string
		Person                *person
		Active                bool
	}
	create := request{
		method: http.MethodPost, path: path,
		body: fmt.Sprintf(`{"displayName":" Fixture Accompanist ","kind":"accompanist","active":true,"personId":%q}`, me.ID),
	}
	added := read[staff](t, b.call(t, office, create, http.StatusCreated))
	if added.DisplayName != "Fixture Accompanist" || !added.Active || added.Person == nil || *added.Person != me {
		t.Fatalf("unexpected linked staff member: %+v", added)
	}
	update := request{
		method: http.MethodPut, path: path + "/{staffId}", params: map[string]string{"staffId": added.ID},
		body: `{"displayName":"Former Accompanist","kind":"accompanist","active":false}`,
	}
	if got := read[staff](t, b.call(t, office, update, http.StatusOK)); got.Active || got.Person != nil {
		t.Fatalf("unexpected inactive staff member: %+v", got)
	}
	if containsJSON(get(t, b, office, path).body, added.ID) || !containsJSON(get(t, b, office, "/api/v1/admin/staff").body, added.ID) {
		t.Fatal("inactive staff must remain in the office list and leave the public list")
	}
	update.method, update.body = http.MethodDelete, ""
	b.call(t, office, update, http.StatusNoContent)
}
