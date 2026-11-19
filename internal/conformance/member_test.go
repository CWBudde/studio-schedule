package conformance

import (
	"bytes"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
)

type booking struct {
	ID       string
	PersonID string
	LessonID string
	Status   string
	Late     bool
}

func memberBooking(t *testing.T, b *backend) {
	t.Helper()

	member := browser(t)
	login(t, b, member, "lina.mistlewood@example.com", "demo-password")
	family := read[struct{ Me struct{ ID string } }](t, get(t, b, member, "/api/v1/family"))
	type lessonState struct {
		scheduledLesson
		Status     string
		FreePlaces int
		BookedFor  []struct{ BookingID string }
	}
	office := browser(t)
	login(t, b, office, "olga.ledgerby@example.net", demoPassword)
	lessonID := createLesson(t, b, office, "regular", 2)

	params := map[string]string{"lessonId": lessonID}
	detail := request{method: http.MethodGet, path: "/api/v1/lessons/{lessonId}", params: params}
	target := read[lessonState](t, b.call(t, member, detail, http.StatusOK))
	book := request{
		method: http.MethodPost, path: "/api/v1/lessons/{lessonId}/bookings",
		params: params, body: `{"personId":"` + family.Me.ID + `"}`,
	}
	original := read[booking](t, b.call(t, member, book, http.StatusCreated))
	b.call(t, member, book, http.StatusConflict)

	before := read[lessonState](t, b.call(t, member, detail, http.StatusOK))
	if before.FreePlaces != target.FreePlaces-1 || len(before.BookedFor) != 1 {
		t.Errorf("booking did not reserve the member's place: before %+v, after %+v", target, before)
	}
	cancel := request{
		method: http.MethodPost, path: "/api/v1/bookings/{bookingId}/cancel",
		params: map[string]string{"bookingId": original.ID},
	}
	other := browser(t)
	login(t, b, other, "clara.amberly@example.org", "demo-password")
	b.call(t, other, cancel, http.StatusNotFound)
	canceled := read[booking](t, b.call(t, member, cancel, http.StatusOK))
	if canceled.ID != original.ID || canceled.Status != "canceled" || canceled.Late {
		t.Fatalf("unexpected canceled booking: %+v", canceled)
	}
	after := read[lessonState](t, b.call(t, member, detail, http.StatusOK))
	if after.FreePlaces != before.FreePlaces+1 || len(after.BookedFor) != 0 {
		t.Errorf("canceling did not release the member's place: before %+v, after %+v", before, after)
	}

	rebooked := read[booking](t, b.call(t, member, book, http.StatusCreated))
	if rebooked.Status != "booked" || rebooked.LessonID != original.LessonID || rebooked.PersonID != original.PersonID {
		t.Fatalf("unexpected rebooking: %+v", rebooked)
	}
	restored := read[lessonState](t, b.call(t, member, detail, http.StatusOK))
	if restored.FreePlaces != before.FreePlaces || len(restored.BookedFor) != 1 ||
		restored.BookedFor[0].BookingID != rebooked.ID {
		t.Errorf("rebooking did not restore availability and ownership: %+v", restored)
	}
}

func calendarSubscription(t *testing.T, b *backend) {
	t.Helper()

	member := browser(t)
	guest := browser(t)
	login(t, b, member, "lina.mistlewood@example.com", "demo-password")
	const endpoint = "/api/v1/my/calendar-feeds"
	result := read[struct {
		Feed      struct{ ID string }
		Token     string
		Path, URL string
	}](t, b.call(t, member, request{method: http.MethodPost, path: endpoint, body: `{"scope":"bookings"}`}, http.StatusCreated))
	base, _ := url.Parse(b.base)
	wantPath := "/ical/" + result.Token + ".ics"
	if result.Token == "" || result.Path != base.Path+wantPath || result.URL != b.base+wantPath {
		t.Fatal("calendar feed URL or path did not use the mount point")
	}

	list := get(t, b, member, endpoint)
	if !bytes.Contains(list.body, []byte(result.Feed.ID)) || bytes.Contains(list.body, []byte(result.Token)) {
		t.Fatal("the list must show the active feed and omit its secret")
	}

	in := request{method: http.MethodGet, path: "/ical/{token}.ics", params: map[string]string{"token": result.Token}}
	document := b.call(t, guest, in, http.StatusOK)
	etag := document.header.Get("ETag")
	if etag == "" || !bytes.HasPrefix(document.body, []byte("BEGIN:VCALENDAR\r\n")) ||
		!bytes.Contains(document.body, []byte("BEGIN:VEVENT")) ||
		!bytes.Contains(document.body, []byte(b.base+"/schedule")) {
		t.Fatal("calendar document lacks its revision, events or mounted lesson links")
	}
	in.header = http.Header{"If-None-Match": {etag}}
	b.call(t, guest, in, http.StatusNotModified)

	b.call(t, member, request{
		method: http.MethodDelete, path: endpoint + "/{feedId}",
		params: map[string]string{"feedId": result.Feed.ID},
	}, http.StatusNoContent)
	b.call(t, guest, in, http.StatusNotFound)
}

func familyProfiles(t *testing.T, b *backend) {
	t.Helper()

	const path = "/api/v1/family"
	parent, outsider := browser(t), browser(t)
	b.call(t, parent, request{method: http.MethodGet, path: path}, http.StatusUnauthorized)
	login(t, b, parent, "clara.amberly@example.org", demoPassword)
	login(t, b, outsider, "lina.mistlewood@example.com", demoPassword)

	type profile struct{ ID, GivenName, FamilyName, Phone, BirthDate string }
	type family struct {
		Me         profile
		Dependents []struct {
			profile
			Guardians []struct{ GivenName, FamilyName string }
			Managed   bool
		}
	}
	initial := read[family](t, get(t, b, parent, path))
	if initial.Me.GivenName != "Clara" || len(initial.Dependents) != 2 || len(initial.Dependents[0].Guardians) == 0 {
		t.Fatalf("unexpected fixture family: %+v", initial)
	}
	me := request{
		method: http.MethodPut, path: path + "/me",
		body: `{"givenName":"Clara","familyName":"Amberly","phone":" 0123 "}`,
	}
	bad := me
	bad.body = `{"givenName":"Clara","familyName":"Amberly","birthDate":""}`
	if res := b.call(t, parent, bad, http.StatusUnprocessableEntity); !containsJSON(res.body, `"location":"body.birthDate"`) {
		t.Fatal("the empty birth date was not reported at body.birthDate")
	}
	if updated := read[profile](t, b.call(t, parent, me, http.StatusOK)); updated.ID != initial.Me.ID || updated.Phone != "0123" {
		t.Fatalf("unexpected saved profile: %+v", updated)
	}

	child := request{
		method: http.MethodPost, path: path + "/dependents",
		body: `{"givenName":"New","familyName":"Fixture","birthDate":"2010-02-03"}`,
	}
	created := read[profile](t, b.call(t, parent, child, http.StatusCreated))
	if created.ID == "" || created.GivenName != "New" || created.BirthDate != "2010-02-03" {
		t.Fatalf("unexpected saved dependent: %+v", created)
	}
	edit := request{
		method: http.MethodPut, path: path + "/dependents/{personId}",
		params: map[string]string{"personId": created.ID}, body: `{"givenName":"Edited","familyName":"Fixture"}`,
	}
	b.call(t, outsider, edit, http.StatusNotFound)
	if updated := read[profile](t, b.call(t, parent, edit, http.StatusOK)); updated.ID != created.ID || updated.GivenName != "Edited" || updated.BirthDate != "" {
		t.Fatalf("unexpected edited dependent: %+v", updated)
	}
	after := read[family](t, get(t, b, parent, path))
	if len(after.Dependents) != 3 || after.Me.Phone != "0123" {
		t.Fatalf("profile changes were not stored: %+v", after)
	}
	for _, dependent := range after.Dependents {
		if dependent.ID == created.ID && dependent.GivenName == "Edited" && !dependent.Managed && len(dependent.Guardians) == 0 {
			return
		}
	}
	t.Fatal("updated dependent is missing from the guardian's family")
}

func personDetail(t *testing.T, b *backend) {
	t.Helper()

	parent, office := browser(t), browser(t)
	login(t, b, parent, "clara.amberly@example.org", demoPassword)
	login(t, b, office, "olga.ledgerby@example.net", demoPassword)
	type person struct {
		ID, GivenName, FamilyName string
	}
	family := read[struct {
		Me         person
		Dependents []person
	}](t, get(t, b, parent, "/api/v1/family"))
	if len(family.Dependents) != 2 {
		t.Fatalf("unexpected family fixture: %+v", family)
	}
	in := request{method: http.MethodGet, path: "/api/v1/admin/persons/{personId}", params: map[string]string{"personId": family.Me.ID}}
	b.call(t, browser(t), in, http.StatusUnauthorized)
	b.call(t, parent, in, http.StatusForbidden)

	type detail struct {
		person
		Account               *struct{ ID, Email, Role, Status string }
		Dependents, Guardians []person
		Classes               []struct{ ClassID, TermName, CourseTypeName, StartTime string }
		Staff                 []any
		Merged                []person
		MergedInto            *person
	}
	response := b.call(t, office, in, http.StatusOK)
	got := read[detail](t, response)
	if got.person != family.Me || got.Account == nil || got.Account.Email != "clara.amberly@example.org" ||
		got.Account.Role != "member" || got.Account.Status != "active" || len(got.Dependents) != 2 ||
		len(got.Classes) == 0 || got.Guardians == nil || got.Staff == nil || got.Merged == nil || got.MergedInto != nil ||
		strings.Contains(string(response.body), "passwordHash") {
		t.Fatalf("unexpected office person detail: %+v", got)
	}
	in.params["personId"] = family.Dependents[0].ID
	child := read[detail](t, b.call(t, office, in, http.StatusOK))
	if child.person != family.Dependents[0] || !slices.Contains(child.Guardians, family.Me) {
		t.Fatalf("office detail omitted the guardian link: %+v", child)
	}
	in.params["personId"] = "f20daa9e-941c-44bc-8c4e-c7435c06a5b7"
	b.call(t, office, in, http.StatusNotFound)
}

func familyPrivacy(t *testing.T, b *backend) {
	t.Helper()

	parent := browser(t)
	login(t, b, parent, "clara.amberly@example.org", "demo-password")
	family := read[struct {
		Me         struct{ ID, GivenName string }
		Dependents []struct{ ID, GivenName string }
	}](t, get(t, b, parent, "/api/v1/family"))
	if family.Me.GivenName != "Clara" || len(family.Dependents) != 2 {
		t.Fatalf("unexpected family: %+v", family)
	}
	export := request{
		method: http.MethodGet, path: "/api/v1/my/export",
		query: url.Values{"person": {family.Me.ID}},
	}
	signed := b.call(t, parent, export, http.StatusOK)
	body := read[struct {
		Format, ExportedAt string
		Version            int
		Account            struct{ Email string }
		ClassMemberships   []any
		Bookings           []any
		Passes             []any
		Receipts           []any
	}](t, signed)
	if body.Format != "studio-schedule.person-export" || body.Version != 1 ||
		body.Account.Email != "clara.amberly@example.org" || body.ExportedAt != demoNow ||
		strings.Contains(string(signed.body), "passwordHash") {
		t.Fatal("person export contains an unexpected account, export version, or secret")
	}

	if len(body.ClassMemberships) == 0 || len(body.Bookings) == 0 || len(body.Passes) == 0 || len(body.Receipts) == 0 {
		t.Fatal("export omitted the demo person's history")
	}
	office := browser(t)
	adminExport := request{
		method: http.MethodGet, path: "/api/v1/admin/persons/{personId}/export",
		params: map[string]string{"personId": family.Me.ID},
	}
	b.call(t, parent, adminExport, http.StatusForbidden)
	login(t, b, office, "olga.ledgerby@example.net", demoPassword)
	if officeExport := b.call(t, office, adminExport, http.StatusOK); !bytes.Equal(officeExport.body, signed.body) {
		t.Error("office and self exports of the same person differ")
	}
	export.query.Set("person", family.Dependents[0].ID)
	child := b.call(t, parent, export, http.StatusOK)
	if containsJSON(child.body, `"account":`) {
		t.Error("dependent export disclosed an account")
	}

	outsider := browser(t)
	login(t, b, outsider, "lina.mistlewood@example.com", "demo-password")
	b.call(t, outsider, export, http.StatusNotFound)
}
