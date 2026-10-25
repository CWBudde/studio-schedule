package app_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/app"
	"github.com/cwbudde/studio-schedule/internal/seed"
)

// TestPrivacyExport downloads the data of a person over HTTP (ADR 0016): a
// member exports their own and their dependents', the office anyone's.
func TestPrivacyExport(t *testing.T) {
	t.Parallel()

	a := newAuthApp(t, app.Deps{Demo: true})
	office := a.login("olga.ledgerby@example.net")
	clara := a.login("clara.amberly@example.org")
	claraID := a.personID("clara amberly", office)
	ellaID := a.personID("ella amberly", office)
	kaiID := a.personID("kai lanternby", office)

	own := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/my/export?person="+claraID, "", clara))
	if own["format"] != "studio-schedule.person-export" || own["version"] != float64(1) {
		t.Errorf("format %v %v", own["format"], own["version"])
	}

	if p, _ := own["person"].(map[string]any); p["givenName"] != "Clara" {
		t.Errorf("person %v", own["person"])
	}

	if acc, _ := own["account"].(map[string]any); acc["email"] != "clara.amberly@example.org" || acc["passwordHash"] != nil {
		t.Errorf("account %v", own["account"])
	}

	if deps, _ := own["dependents"].([]any); len(deps) != 2 {
		t.Errorf("dependents %v", own["dependents"])
	}

	if ella := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/my/export?person="+ellaID, "", clara)); ella["account"] != nil {
		t.Errorf("Ella's account %v", ella["account"])
	}

	a.expect(http.StatusNotFound, http.MethodGet, "/api/v1/my/export?person="+kaiID, "", clara)
	a.expect(http.StatusForbidden, http.MethodGet, "/api/v1/admin/persons/"+claraID+"/export", "", clara)

	body := string(a.expect(http.StatusOK, http.MethodGet, "/api/v1/admin/persons/"+claraID+"/export", "", office))
	for _, secret := range []string{"argon2", "token", "hash"} {
		if strings.Contains(strings.ToLower(body), secret) {
			t.Errorf("the export contains %q", secret)
		}
	}
}

// TestAnonymizeOverHTTP anonymizes a person and checks the 409 reasons.
func TestAnonymizeOverHTTP(t *testing.T) {
	t.Parallel()

	a := newAuthApp(t, app.Deps{Demo: true})
	office := a.login("olga.ledgerby@example.net")
	kai := a.login("kai.lanternby@example.org")
	kaiID := a.personID("kai lanternby", office)
	hannaID := a.personID("hanna birchmoor", office)
	path := func(id string) string { return "/api/v1/admin/persons/" + id + "/anonymize" }

	a.expect(http.StatusForbidden, http.MethodPost, path(kaiID), "", kai)

	if c := decode(t, a.expect(http.StatusConflict, http.MethodPost, path(hannaID), "", office)); c["reason"] != "sole-guardian" {
		t.Errorf("Hanna: %v", c)
	}

	res := decode(t, a.expect(http.StatusOK, http.MethodPost, path(kaiID), "", office))
	if p, _ := res["person"].(map[string]any); p["givenName"] != "Deleted" || p["familyName"] != "person" ||
		p["email"] != nil || res["accountDeleted"] != true {
		t.Errorf("result %v", res)
	}

	// Kai is signed out and gone from the search; his page reads.
	a.expect(http.StatusUnauthorized, http.MethodGet, "/api/v1/auth/me", "", kai)

	detail := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/admin/persons/"+kaiID, "", office))
	if detail["anonymizedAt"] == nil || detail["account"] != nil {
		t.Errorf("detail %v", detail)
	}

	if c := decode(t, a.expect(http.StatusConflict, http.MethodPost, path(kaiID), "", office)); c["reason"] != "anonymized" {
		t.Errorf("twice: %v", c)
	}

	if items, _ := decode(t, a.expect(http.StatusOK, http.MethodPost, "/api/v1/admin/persons/search", `{"query": "deleted"}`, office))["items"].([]any); len(items) != 0 {
		t.Errorf("search %v", items)
	}

	a.expect(http.StatusNotFound, http.MethodPut, "/api/v1/admin/persons/"+kaiID,
		`{"givenName":"Kai","familyName":"Lanternby"}`, office)

	audit := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/admin/audit", "", a.login("arno.keystone@example.net")))
	items, _ := audit["items"].([]any)
	if len(items) == 0 || items[0].(map[string]any)["action"] != "person-anonymized" {
		t.Fatalf("audit %v", audit["items"])
	}

	// The app translates the fixed name of an anonymized person.
	if target, _ := items[0].(map[string]any)["targetPerson"].(map[string]any); target["anonymized"] != true {
		t.Errorf("audit target %v", target)
	}
}

// TestAccountAdminOverHTTP disables, enables, re-roles and deletes an
// account as the admin.
func TestAccountAdminOverHTTP(t *testing.T) {
	t.Parallel()

	a := newAuthApp(t, app.Deps{Demo: true})
	admin := a.login("arno.keystone@example.net")
	office := a.login("olga.ledgerby@example.net")
	oskar := a.login("oskar.nettlebury@example.org")

	ids := map[string]string{}

	for _, item := range decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/admin/accounts", "", admin))["items"].([]any) {
		acc, _ := item.(map[string]any)
		ids[acc["email"].(string)], _ = acc["id"].(string)
	}

	oskarPath := "/api/v1/admin/accounts/" + ids["oskar.nettlebury@example.org"]
	adminPath := "/api/v1/admin/accounts/" + ids["arno.keystone@example.net"]

	a.expect(http.StatusForbidden, http.MethodPost, oskarPath+"/disable", "", office)

	if e := decode(t, a.expect(http.StatusOK, http.MethodPost, oskarPath+"/disable", "", admin)); e["disabled"] != true {
		t.Errorf("disabled %v", e)
	}

	a.expect(http.StatusUnauthorized, http.MethodGet, "/api/v1/auth/me", "", oskar)

	if e := decode(t, a.expect(http.StatusOK, http.MethodPost, oskarPath+"/enable", "", admin)); e["disabled"] != false {
		t.Errorf("enabled %v", e)
	}

	if e := decode(t, a.expect(http.StatusOK, http.MethodPut, oskarPath+"/role", `{"role":"instructor"}`, admin)); e["role"] != "instructor" {
		t.Errorf("role %v", e)
	}

	a.expect(http.StatusUnprocessableEntity, http.MethodPut, oskarPath+"/role", `{"role":"guest"}`, admin)

	for _, call := range []struct{ method, path, body string }{
		{http.MethodPost, adminPath + "/disable", ""},
		{http.MethodPut, adminPath + "/role", `{"role":"member"}`},
		{http.MethodDelete, adminPath, ""},
	} {
		if c := decode(t, a.expect(http.StatusConflict, call.method, call.path, call.body, admin)); c["reason"] != "own-account" {
			t.Errorf("%s %s: %v", call.method, call.path, c)
		}
	}

	// The office issued the demo's receipts: it is kept.
	if c := decode(t, a.expect(http.StatusConflict, http.MethodDelete, "/api/v1/admin/accounts/"+ids["olga.ledgerby@example.net"], "", admin)); c["reason"] != "in-use" {
		t.Errorf("delete the office: %v", c)
	}

	a.expect(http.StatusNoContent, http.MethodDelete, oskarPath, "", admin)
	a.expect(http.StatusNotFound, http.MethodDelete, oskarPath, "", admin)

	res, _ := a.call(http.MethodPost, "/api/v1/auth/login", loginBody("oskar.nettlebury@example.org", seed.DemoPassword))
	if res.StatusCode != http.StatusUnauthorized {
		t.Errorf("login after the deletion: %d", res.StatusCode)
	}

	audit := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/admin/audit", "", admin))
	if items, _ := audit["items"].([]any); len(items) < 4 ||
		items[0].(map[string]any)["action"] != "account-deleted" ||
		items[1].(map[string]any)["details"].(map[string]any)["role"] != "instructor" {
		t.Errorf("audit %v", audit["items"])
	}
}

// TestDeleteMyAccountOverHTTP deletes the signed-in account.
func TestDeleteMyAccountOverHTTP(t *testing.T) {
	t.Parallel()

	a := newAuthApp(t, app.Deps{Demo: true})
	tilo := a.login("tilo.quarrystone@example.org")

	wrong := decode(t, a.expect(http.StatusUnprocessableEntity, http.MethodDelete, "/api/v1/my/account",
		`{"password":"not-it"}`, tilo))
	if errs, _ := wrong["errors"].([]any); len(errs) != 1 || errs[0].(map[string]any)["location"] != "body.password" {
		t.Errorf("422 %v", wrong)
	}

	res, body := a.call(http.MethodDelete, "/api/v1/my/account", `{"password":"`+seed.DemoPassword+`","locale":"en"}`, tilo)
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("delete: %d %s", res.StatusCode, body)
	}

	if c := res.Cookies(); len(c) != 1 || c[0].MaxAge >= 0 {
		t.Errorf("cookies %v; want the session cookie cleared", c)
	}

	a.expect(http.StatusUnauthorized, http.MethodGet, "/api/v1/auth/me", "", tilo)

	if to, text := a.mailTo("account-deleted"); to != "tilo.quarrystone@example.org" || !strings.Contains(text, "you deleted your account") {
		t.Errorf("mail to %s: %s", to, text)
	}

	// The admin is the demo's only one.
	admin := a.login("arno.keystone@example.net")

	if c := decode(t, a.expect(http.StatusConflict, http.MethodDelete, "/api/v1/my/account",
		`{"password":"`+seed.DemoPassword+`"}`, admin)); c["reason"] != "last-admin" {
		t.Errorf("the last admin: %v", c)
	}
}
