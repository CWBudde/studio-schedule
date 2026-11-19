package conformance

import (
	"net/http"
	"testing"
)

type officePersonRecord struct {
	ID, GivenName, FamilyName, Email, Phone, BirthDate, Notes, MemberNo string
}

func officePersonWrites(t *testing.T, b *backend) {
	t.Helper()

	office, member, visitor := browser(t), browser(t), browser(t)
	login(t, b, office, "olga.ledgerby@example.net", demoPassword)
	login(t, b, member, linaEmail, demoPassword)
	create := request{
		method: http.MethodPost, path: "/api/v1/admin/persons",
		body: `{"givenName":" Aster ","familyName":" Sample ","email":" aster@example.test ","phone":" 0123 ",` +
			`"birthDate":"2008-03-12","notes":" internal note ","memberNo":" PH13-A "}`,
	}
	b.call(t, visitor, create, http.StatusUnauthorized)
	b.call(t, member, create, http.StatusForbidden)
	bad := create
	bad.body = `{"givenName":"Aster","familyName":"Sample","email":"invalid"}`
	checkLessonField(t, b.call(t, office, bad, http.StatusUnprocessableEntity), "body.email")
	bad.body = `{"givenName":"Aster","familyName":"Sample","birthDate":"2027-01-01"}`
	checkLessonField(t, b.call(t, office, bad, http.StatusUnprocessableEntity), "body.birthDate")
	person := read[officePersonRecord](t, b.call(t, office, create, http.StatusCreated))
	want := officePersonRecord{
		ID: person.ID, GivenName: "Aster", FamilyName: "Sample", Email: "aster@example.test",
		Phone: "0123", BirthDate: "2008-03-12", Notes: "internal note", MemberNo: "PH13-A",
	}
	if person != want || person.ID == "" {
		t.Fatalf("unexpected created person: %+v", person)
	}
	expectConflict(t, b, office, create, "duplicate")

	update := request{
		method: http.MethodPut, path: "/api/v1/admin/persons/{personId}",
		params: map[string]string{"personId": person.ID},
		body:   `{"givenName":"Aster","familyName":"New Name"}`,
	}
	replaced := read[officePersonRecord](t, b.call(t, office, update, http.StatusOK))
	if want := (officePersonRecord{ID: person.ID, GivenName: "Aster", FamilyName: "New Name"}); replaced != want {
		t.Fatalf("person replaced with %+v, want %+v", replaced, want)
	}
	family := read[struct{ Me struct{ ID string } }](t, get(t, b, member, "/api/v1/family"))
	update.params = map[string]string{"personId": family.Me.ID}
	update.body = `{"givenName":"Lina","familyName":"Mistlewood","email":"contact-only@example.test"}`
	b.call(t, office, update, http.StatusOK)
	login(t, b, browser(t), linaEmail, demoPassword)

	checkGuardianWrites(t, b, office, person.ID)
}

func checkGuardianWrites(t *testing.T, b *backend, office *http.Client, first string) {
	t.Helper()

	addPerson := func(name string) string {
		t.Helper()

		return read[officePersonRecord](t, b.call(t, office, request{
			method: http.MethodPost, path: "/api/v1/admin/persons",
			body: jsonBody(t, map[string]string{"givenName": name, "familyName": "Guardian Fixture"}),
		}, http.StatusCreated)).ID
	}
	second, third := addPerson("Briar"), addPerson("Cedar")
	link := func(dependent, guardian string) request {
		return request{
			method: http.MethodPost, path: "/api/v1/admin/persons/{personId}/guardians",
			params: map[string]string{"personId": dependent},
			body:   jsonBody(t, map[string]string{"guardianId": guardian}),
		}
	}
	if got := read[officePersonRecord](t, b.call(t, office, link(first, second), http.StatusCreated)); got.ID != second {
		t.Fatalf("link returned the wrong guardian: %+v", got)
	}
	expectConflict(t, b, office, link(first, second), "duplicate")
	b.call(t, office, link(second, third), http.StatusCreated)
	checkLessonField(t, b.call(t, office, link(third, first), http.StatusUnprocessableEntity), "body.guardianId")
	checkLessonField(t, b.call(t, office, link(first, first), http.StatusUnprocessableEntity), "body.guardianId")
	checkLessonField(t, b.call(t, office, link(first, "018f0000-0000-7000-8000-000000000999"), http.StatusUnprocessableEntity),
		"body.guardianId")

	remove := request{
		method: http.MethodDelete, path: "/api/v1/admin/persons/{personId}/guardians/{guardianId}",
		params: map[string]string{"personId": second, "guardianId": third},
	}
	b.call(t, office, remove, http.StatusNoContent)
	b.call(t, office, remove, http.StatusNotFound)
	detail := read[struct {
		ID        string
		Guardians []officePersonRecord
	}](t, b.call(t, office, request{
		method: http.MethodGet, path: "/api/v1/admin/persons/{personId}",
		params: map[string]string{"personId": second},
	}, http.StatusOK))
	if detail.ID != second || len(detail.Guardians) != 0 {
		t.Fatalf("unlinking should keep the person without guardians: %+v", detail)
	}
	b.call(t, office, link(third, first), http.StatusCreated)
}
