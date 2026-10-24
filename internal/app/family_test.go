package app_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/app"
	"github.com/cwbudde/studio-schedule/internal/seed"
)

// login signs in a demo account and returns its session cookie.
func (a *authApp) login(email string) *http.Cookie {
	a.t.Helper()

	res, body := a.call(http.MethodPost, "/api/v1/auth/login", `{"email":"`+email+`","password":"`+seed.DemoPassword+`"}`)
	if res.StatusCode != http.StatusOK {
		a.t.Fatalf("login %s: %d %s", email, res.StatusCode, body)
	}

	return sessionCookie(a.t, res)
}

// expect sends a request and fails unless it answers status.
func (a *authApp) expect(status int, method, path, body string, cookie *http.Cookie) []byte {
	a.t.Helper()

	res, out := a.call(method, path, body, cookie)
	if res.StatusCode != status {
		a.t.Fatalf("%s %s: %d %s, want %d", method, path, res.StatusCode, out, status)
	}

	return out
}

type familyBody struct {
	Me struct {
		ID, GivenName, BirthDate string
	} `json:"me"`
	Dependents []struct {
		ID, GivenName string
		Guardians     []struct{ GivenName, FamilyName string }
	} `json:"dependents"`
}

func (a *authApp) family(cookie *http.Cookie) familyBody {
	a.t.Helper()

	var f familyBody
	if err := json.Unmarshal(a.expect(http.StatusOK, http.MethodGet, "/api/v1/family", "", cookie), &f); err != nil {
		a.t.Fatal(err)
	}

	return f
}

func TestFamilyFlow(t *testing.T) {
	t.Parallel()

	a := newAuthApp(t, app.Deps{})
	clara := a.login("clara.amberly@example.org")

	f := a.family(clara)
	if f.Me.GivenName != "Clara" || len(f.Dependents) != 2 || f.Dependents[0].GivenName != "Ella" ||
		len(f.Dependents[0].Guardians) != 1 || f.Dependents[0].Guardians[0].GivenName != "Tom" {
		t.Fatalf("family = %+v", f)
	}

	// Own person data: the header's name comes from it.
	a.expect(http.StatusOK, http.MethodPut, "/api/v1/family/me",
		`{"givenName":"Clara Maria","familyName":"Amberly","birthDate":"1985-02-03"}`, clara)

	if me := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/auth/me", "", clara)); me["givenName"] != "Clara Maria" {
		t.Errorf("me after the update: %v", me)
	}

	// A birth date in the future names the field, so the form can mark it.
	future := decode(t, a.expect(http.StatusUnprocessableEntity, http.MethodPut, "/api/v1/family/me",
		`{"givenName":"Clara","familyName":"Amberly","birthDate":"2099-01-01"}`, clara))
	if errs, _ := future["errors"].([]any); len(errs) != 1 || errs[0].(map[string]any)["location"] != "body.birthDate" {
		t.Errorf("422 errors %v, want body.birthDate", future["errors"])
	}

	// A new child, edited and removed again.
	added := decode(t, a.expect(http.StatusCreated, http.MethodPost, "/api/v1/family/dependents",
		`{"givenName":"Paula","familyName":"Amberly"}`, clara))
	paula, _ := added["id"].(string)

	a.expect(http.StatusOK, http.MethodPut, "/api/v1/family/dependents/"+paula,
		`{"givenName":"Paula","familyName":"Amberly","birthDate":"2021-05-04"}`, clara)

	// Another family sees none of it: 404, not 403.
	hanna := a.login("hanna.birchmoor@example.com")
	a.expect(http.StatusNotFound, http.MethodPut, "/api/v1/family/dependents/"+paula,
		`{"givenName":"X","familyName":"Y"}`, hanna)
	a.expect(http.StatusNotFound, http.MethodDelete, "/api/v1/family/dependents/"+paula, "", hanna)

	// Hanna is Mia's only guardian, and Mia is a class member.
	mia := ""
	for _, d := range a.family(hanna).Dependents {
		if d.GivenName == "Mia" {
			mia = d.ID
		}
	}

	problem := decode(t, a.expect(http.StatusConflict, http.MethodDelete, "/api/v1/family/dependents/"+mia, "", hanna))
	if problem["detail"] != "the dependent is still in use, e.g. by bookings or class memberships" || problem["reason"] != "in-use" {
		t.Errorf("409 %v, want reason in-use", problem)
	}

	a.expect(http.StatusNoContent, http.MethodDelete, "/api/v1/family/dependents/"+paula, "", clara)

	if f := a.family(clara); len(f.Dependents) != 2 {
		t.Errorf("dependents after removing Paula: %+v", f.Dependents)
	}
}

func TestUpdateLocale(t *testing.T) {
	t.Parallel()

	a := newAuthApp(t, app.Deps{})
	clara := a.login("clara.amberly@example.org")

	locale := func() any {
		return decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/auth/me", "", clara))["locale"]
	}

	a.expect(http.StatusNoContent, http.MethodPut, "/api/v1/auth/locale", `{"locale":"de"}`, clara)

	if got := locale(); got != "de" {
		t.Errorf("locale %v, want de", got)
	}

	a.expect(http.StatusUnprocessableEntity, http.MethodPut, "/api/v1/auth/locale", `{"locale":"fr"}`, clara)
	a.expect(http.StatusNoContent, http.MethodPut, "/api/v1/auth/locale", `{}`, clara)

	if got := locale(); got != nil {
		t.Errorf("locale %v after clearing it, want none", got)
	}
}
