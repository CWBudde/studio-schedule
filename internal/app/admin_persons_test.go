package app_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/app"
)

// personID finds the ID of the single person that q matches.
func (a *authApp) personID(q string, cookie *http.Cookie) string {
	a.t.Helper()

	items, _ := decode(a.t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/admin/persons?q="+url.QueryEscape(q), "", cookie))["items"].([]any)
	if len(items) != 1 {
		a.t.Fatalf("persons matching %q: %v", q, items)
	}

	id, _ := items[0].(map[string]any)["id"].(string)

	return id
}

// names returns the given names of a list of PersonMatch.
func names(items any) []string {
	list, _ := items.([]any)
	out := make([]string, 0, len(list))

	for _, p := range list {
		given, _ := p.(map[string]any)["givenName"].(string)
		out = append(out, given)
	}

	return out
}

var inviteLink = regexp.MustCompile(`https://school\.example\.org/reset-password\?token=([A-Za-z0-9_-]+)`)

// TestAdminPersons runs the office's persons view over HTTP (ADR 0008):
// person data, guardian links, and an account with an invitation.
func TestAdminPersons(t *testing.T) {
	t.Parallel()

	a := newAuthApp(t, app.Deps{Demo: true})
	office := a.login("olga.ledgerby@example.net")
	clara := a.login("clara.amberly@example.org")
	claraID := a.personID("clara amberly", office)

	a.expect(http.StatusForbidden, http.MethodGet, "/api/v1/admin/persons/"+claraID, "", clara)
	a.expect(http.StatusNotFound, http.MethodGet, "/api/v1/admin/persons/ffffffff-ffff-7fff-bfff-ffffffffffff", "", office)

	detail := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/admin/persons/"+claraID, "", office))
	if acc, _ := detail["account"].(map[string]any); acc["email"] != "clara.amberly@example.org" ||
		acc["role"] != "member" || acc["status"] != "active" {
		t.Errorf("Clara's account %v", detail["account"])
	}

	if got := names(detail["dependents"]); !slices.Equal(got, []string{"Ella", "Finn"}) {
		t.Errorf("Clara's dependents %v", got)
	}

	// A new person, edited with every field.
	invalid := decode(t, a.expect(http.StatusUnprocessableEntity, http.MethodPost, "/api/v1/admin/persons",
		`{"givenName":"Ida","familyName":"Amberly","email":"ida at example"}`, office))
	if errs, _ := invalid["errors"].([]any); len(errs) != 1 || errs[0].(map[string]any)["location"] != "body.email" {
		t.Errorf("422 errors %v, want body.email", invalid["errors"])
	}

	created := decode(t, a.expect(http.StatusCreated, http.MethodPost, "/api/v1/admin/persons",
		`{"givenName":"Ida","familyName":"Amberly","birthDate":"2008-04-01"}`, office))
	idaID, _ := created["id"].(string)

	updated := decode(t, a.expect(http.StatusOK, http.MethodPut, "/api/v1/admin/persons/"+idaID,
		`{"givenName":"Ida","familyName":"Amberly","birthDate":"2008-04-01","phone":"+49 000 0000177","notes":"Grown up."}`, office))
	if updated["notes"] != "Grown up." || updated["phone"] != "+49 000 0000177" || updated["email"] != nil {
		t.Errorf("updated %v", updated)
	}

	// Clara becomes Ida's guardian.
	guardians := "/api/v1/admin/persons/" + idaID + "/guardians"
	link := `{"guardianId":"` + claraID + `"}`

	if g := decode(t, a.expect(http.StatusCreated, http.MethodPost, guardians, link, office)); g["id"] != claraID {
		t.Errorf("added guardian %v", g)
	}

	if twice := decode(t, a.expect(http.StatusConflict, http.MethodPost, guardians, link, office)); twice["reason"] != "duplicate" {
		t.Errorf("409 %v, want reason duplicate", twice)
	}

	self := decode(t, a.expect(http.StatusUnprocessableEntity, http.MethodPost, guardians, `{"guardianId":"`+idaID+`"}`, office))
	if errs, _ := self["errors"].([]any); len(errs) != 1 || errs[0].(map[string]any)["location"] != "body.guardianId" {
		t.Errorf("422 errors %v, want body.guardianId", self["errors"])
	}

	if got := names(decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/admin/persons/"+idaID, "", office))["guardians"]); !slices.Equal(got, []string{"Clara"}) {
		t.Errorf("Ida's guardians %v", got)
	}

	// Ida gets an account, created by the office with an invitation.
	account := "/api/v1/admin/persons/" + idaID + "/account"

	a.expect(http.StatusForbidden, http.MethodPost, account, `{"email":"ida@example.org","role":"admin"}`, office)

	acc := decode(t, a.expect(http.StatusCreated, http.MethodPost, account, `{"email":"ida@example.org","role":"member"}`, office))
	if acc["status"] != "pending" || acc["role"] != "member" || acc["email"] != "ida@example.org" {
		t.Errorf("account %v", acc)
	}

	again := decode(t, a.expect(http.StatusConflict, http.MethodPost, account, `{"email":"ida2@example.org","role":"member"}`, office))
	taken := decode(t, a.expect(http.StatusConflict, http.MethodPost,
		"/api/v1/admin/persons/"+a.personID("finn amberly", office)+"/account",
		`{"email":"CLARA.amberly@example.org","role":"member"}`, office))

	if again["reason"] != "has-account" {
		t.Errorf("409 %v, want reason has-account", again)
	}

	if taken["reason"] != "duplicate" {
		t.Errorf("409 %v, want reason duplicate", taken)
	}

	// Ida is now a managed dependent: Clara sees her read-only, and removing
	// her only ends the guardianship.
	var family struct {
		Dependents []struct {
			ID      string
			Managed bool
		}
	}
	if err := json.Unmarshal(a.expect(http.StatusOK, http.MethodGet, "/api/v1/family", "", clara), &family); err != nil {
		t.Fatal(err)
	}

	for _, d := range family.Dependents {
		if d.Managed != (d.ID == idaID) {
			t.Errorf("dependent %s managed = %v", d.ID, d.Managed)
		}
	}

	managed := decode(t, a.expect(http.StatusConflict, http.MethodPut, "/api/v1/family/dependents/"+idaID,
		`{"givenName":"I","familyName":"A"}`, clara))
	if managed["reason"] != "managed" {
		t.Errorf("409 %v, want reason managed", managed)
	}

	a.expect(http.StatusNoContent, http.MethodDelete, "/api/v1/family/dependents/"+idaID, "", clara)

	if d := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/admin/persons/"+idaID, "", office)); len(names(d["guardians"])) != 0 {
		t.Errorf("Ida after Clara removed her: %v", d)
	}

	// The invitation sets the password and activates the account.
	var mb struct {
		Mails []struct{ To, Template, Text string }
	}
	res, body := a.call(http.MethodGet, "/api/v1/demo/mailbox", "")
	if err := json.Unmarshal(body, &mb); res.StatusCode != http.StatusOK || err != nil {
		t.Fatalf("mailbox: %d %s", res.StatusCode, body)
	}

	if len(mb.Mails) == 0 || mb.Mails[0].To != "ida@example.org" || mb.Mails[0].Template != "invite" {
		t.Fatalf("newest mail %+v, want the invitation", mb.Mails)
	}

	token := inviteLink.FindStringSubmatch(mb.Mails[0].Text)
	if token == nil {
		t.Fatalf("no reset-password link in %q", mb.Mails[0].Text)
	}

	if res, body := a.call(http.MethodPost, "/api/v1/auth/password-reset/confirm",
		`{"token":"`+token[1]+`","password":"battery staple"}`); res.StatusCode != http.StatusNoContent {
		t.Fatalf("reset with the invitation: %d %s", res.StatusCode, body)
	}

	if res, body := a.call(http.MethodPost, "/api/v1/auth/login",
		`{"email":"ida@example.org","password":"battery staple"}`); res.StatusCode != http.StatusOK {
		t.Fatalf("login after the invitation: %d %s", res.StatusCode, body)
	}

	// The office removing the last guardian keeps the person.
	a.expect(http.StatusCreated, http.MethodPost, guardians, link, office)
	a.expect(http.StatusNoContent, http.MethodDelete, guardians+"/"+claraID, "", office)
	a.expect(http.StatusNotFound, http.MethodDelete, guardians+"/"+claraID, "", office)
	a.expect(http.StatusOK, http.MethodGet, "/api/v1/admin/persons/"+idaID, "", office)
}

// TestAdminPersonLinksAndDeletion covers guardian cycles and deleting
// persons over HTTP.
func TestAdminPersonLinksAndDeletion(t *testing.T) {
	t.Parallel()

	a := newAuthApp(t, app.Deps{Demo: true})
	office := a.login("olga.ledgerby@example.net")
	claraID, ellaID := a.personID("clara amberly", office), a.personID("ella amberly", office)

	// Ella is Clara's dependent: she cannot become Clara's guardian.
	cycle := decode(t, a.expect(http.StatusUnprocessableEntity, http.MethodPost,
		"/api/v1/admin/persons/"+claraID+"/guardians", `{"guardianId":"`+ellaID+`"}`, office))
	if errs, _ := cycle["errors"].([]any); len(errs) != 1 || errs[0].(map[string]any)["location"] != "body.guardianId" {
		t.Errorf("422 errors %v, want body.guardianId", cycle["errors"])
	}

	twice := decode(t, a.expect(http.StatusConflict, http.MethodPost,
		"/api/v1/admin/persons/"+ellaID+"/guardians", `{"guardianId":"`+claraID+`"}`, office))
	if twice["reason"] != "duplicate" || twice["detail"] != "the guardian is linked already" {
		t.Errorf("409 %v, want reason duplicate with a fitting detail", twice)
	}

	// A person nothing refers to can be deleted; Clara, with an account and
	// dependents, cannot.
	created := decode(t, a.expect(http.StatusCreated, http.MethodPost, "/api/v1/admin/persons",
		`{"givenName":"Ida","familyName":"Example"}`, office))
	idaID, _ := created["id"].(string)

	a.expect(http.StatusForbidden, http.MethodDelete, "/api/v1/admin/persons/"+idaID, "", a.login("clara.amberly@example.org"))
	a.expect(http.StatusNoContent, http.MethodDelete, "/api/v1/admin/persons/"+idaID, "", office)
	a.expect(http.StatusNotFound, http.MethodGet, "/api/v1/admin/persons/"+idaID, "", office)
	a.expect(http.StatusNotFound, http.MethodDelete, "/api/v1/admin/persons/"+idaID, "", office)

	used := decode(t, a.expect(http.StatusConflict, http.MethodDelete, "/api/v1/admin/persons/"+claraID, "", office))
	if used["reason"] != "in-use" {
		t.Errorf("409 %v, want reason in-use", used)
	}

	a.expect(http.StatusOK, http.MethodGet, "/api/v1/admin/persons/"+claraID, "", office)
}
