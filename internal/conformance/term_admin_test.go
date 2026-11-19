package conformance

import (
	"fmt"
	"net/http"
	"testing"
)

func termAdministration(t *testing.T, b *backend) {
	t.Helper()

	office := browser(t)
	login(t, b, office, "olga.ledgerby@example.net", demoPassword)
	const path = "/api/v1/terms"
	type term struct{ ID, Name, StartsOn, EndsOn string }
	seeded := read[struct{ Items []term }](t, get(t, b, office, path)).Items
	var inUse term
	for _, item := range seeded {
		if item.StartsOn <= demoDate && item.EndsOn >= demoDate {
			inUse = item
			break
		}
	}
	if inUse.ID == "" {
		t.Fatal("fixture has no term for the current school date")
	}
	create := request{
		method: http.MethodPost, path: path,
		body: `{"name":" Fixture term ","startsOn":"2026-10-23","endsOn":"2026-11-15"}`,
	}
	b.call(t, browser(t), create, http.StatusUnauthorized)
	added := read[term](t, b.call(t, office, create, http.StatusCreated))
	if added.ID == "" || added.Name != "Fixture term" || added.StartsOn != "2026-10-23" || added.EndsOn != "2026-11-15" {
		t.Fatalf("unexpected term: %+v", added)
	}
	update := request{
		method: http.MethodPut, path: path + "/{termId}", params: map[string]string{"termId": added.ID},
		body: `{"name":"Renamed term","startsOn":"2026-10-24","endsOn":"2026-11-16"}`,
	}
	got := read[term](t, b.call(t, office, update, http.StatusOK))
	if got.ID != added.ID || got.Name != "Renamed term" || got.StartsOn != "2026-10-24" || got.EndsOn != "2026-11-16" {
		t.Fatalf("unexpected updated term: %+v", got)
	}
	update.body = `{"name":"Broken dates","startsOn":"2026-11-16","endsOn":"2026-10-24"}`
	if bad := b.call(t, office, update, http.StatusUnprocessableEntity); !containsJSON(bad.body, `"location":"body.endsOn"`) {
		t.Error("a reversed date range was not reported on endsOn")
	}
	update.method, update.body = http.MethodDelete, ""
	b.call(t, office, update, http.StatusNoContent)
	b.call(t, office, update, http.StatusNotFound)
	update.params["termId"] = inUse.ID
	expectConflict(t, b, office, update, "in-use")
	update.method = http.MethodPut
	update.body = fmt.Sprintf(`{"name":%q,"startsOn":%q,"endsOn":%q}`, inUse.Name, demoDate, inUse.EndsOn)
	if bad := b.call(t, office, update, http.StatusUnprocessableEntity); !containsJSON(bad.body, `"location":"body.startsOn"`) {
		t.Error("excluding a lesson that has started was not reported on startsOn")
	}
	for _, item := range read[struct{ Items []term }](t, get(t, b, office, path)).Items {
		if item.ID == inUse.ID && item != inUse {
			t.Errorf("rejected term edit changed the term: %+v", item)
		}
	}
}
