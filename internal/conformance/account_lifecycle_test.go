package conformance

import (
	"bytes"
	"net/http"
	"testing"
)

type lifecycleAccount struct {
	AccountID, PersonID, Role string
}

const (
	lifecycleMe     = "/api/v1/auth/me"
	lifecycleTarget = "/api/v1/admin/accounts/{accountId}"
	lifecycleOwn    = "/api/v1/my/account"
)

func accountLifecycle(t *testing.T, b *backend) {
	t.Helper()

	admin, staff, member := browser(t), browser(t), browser(t)
	owner := read[lifecycleAccount](t, login(t, b, admin, demoAdmin, demoPassword))
	office := read[lifecycleAccount](t, login(t, b, staff, "olga.ledgerby@example.net", demoPassword))
	target := read[lifecycleAccount](t, login(t, b, member, demoMember, demoPassword))
	disable := lifecycleRequest(http.MethodPost, "/disable", target.AccountID)
	b.call(t, browser(t), disable, http.StatusUnauthorized)
	b.call(t, staff, disable, http.StatusForbidden)
	expectConflict(t, b, admin, lifecycleRequest(http.MethodPost, "/disable", owner.AccountID), "own-account")
	expectConflict(t, b, admin, lifecycleRequest(http.MethodDelete, "", owner.AccountID), "own-account")

	role := lifecycleRequest(http.MethodPut, "/role", target.AccountID)
	role.body = `{"role":"guest"}`
	b.call(t, admin, role, http.StatusUnprocessableEntity)
	role.body = `{"role":"instructor"}`
	b.call(t, admin, role, http.StatusOK)
	b.call(t, admin, role, http.StatusOK)
	if got := read[lifecycleAccount](t, get(t, b, member, lifecycleMe)); got.Role != "instructor" {
		t.Errorf("the existing session sees role %q, want instructor", got.Role)
	}

	acting := browser(t)
	login(t, b, acting, demoAdmin, demoPassword)
	b.call(t, acting, lifecycleRequest(http.MethodPost, "/impersonate", target.AccountID), http.StatusOK)
	own := request{method: http.MethodDelete, path: lifecycleOwn, body: `{"password":"` + demoPassword + `"}`}
	b.call(t, acting, own, http.StatusForbidden)
	for range 2 {
		response := read[struct{ Disabled bool }](t, b.call(t, admin, disable, http.StatusOK))
		if !response.Disabled {
			t.Error("disabled account was reported as enabled")
		}
	}
	me := request{method: http.MethodGet, path: lifecycleMe}
	b.call(t, member, me, http.StatusUnauthorized)
	b.call(t, acting, me, http.StatusUnauthorized)
	disabledLogin := request{method: http.MethodPost, path: "/api/v1/auth/login", body: `{"email":"` + demoMember + `","password":"` + demoPassword + `"}`}
	b.call(t, member, disabledLogin, http.StatusUnauthorized)
	b.call(t, admin, lifecycleRequest(http.MethodPost, "/enable", target.AccountID), http.StatusOK)
	login(t, b, member, demoMember, demoPassword)
	b.call(t, admin, lifecycleRequest(http.MethodDelete, "", target.AccountID), http.StatusNoContent)
	b.call(t, member, me, http.StatusUnauthorized)
	b.call(t, member, disabledLogin, http.StatusUnauthorized)
	assertRetargetedAudit(t, b, admin, target.PersonID)

	expectConflict(t, b, admin, own, "last-admin")
	promote := lifecycleRequest(http.MethodPut, "/role", office.AccountID)
	promote.body = `{"role":"admin"}`
	b.call(t, admin, promote, http.StatusOK)
	expectConflict(t, b, staff, lifecycleRequest(http.MethodDelete, "", owner.AccountID), "in-use")
	b.call(t, admin, me, http.StatusOK)

	deleteOwnFixture(t, b, admin)
}

func lifecycleRequest(method, suffix, id string) request {
	return request{method: method, path: lifecycleTarget + suffix, params: map[string]string{"accountId": id}}
}

func assertRetargetedAudit(t *testing.T, b *backend, admin *http.Client, personID string) {
	t.Helper()

	type auditEvent struct {
		Action       string
		TargetPerson *struct{ ID string }
	}
	page := read[struct{ Items []auditEvent }](t, get(t, b, admin, "/api/v1/admin/audit"))
	counts := map[string]int{}
	for _, event := range page.Items {
		if event.TargetPerson != nil && event.TargetPerson.ID == personID {
			counts[event.Action]++
		}
	}
	for _, action := range []string{"account-role-changed", "account-disabled", "account-enabled", "account-deleted", "impersonation-started", "impersonation-ended"} {
		if counts[action] != 1 {
			t.Errorf("audit has %d %s events about the retained person, want 1", counts[action], action)
		}
	}
}

func deleteOwnFixture(t *testing.T, b *backend, admin *http.Client) {
	t.Helper()

	const email = "clara.amberly@example.org"
	holder := browser(t)
	account := read[lifecycleAccount](t, login(t, b, holder, email, demoPassword))
	own := request{method: http.MethodDelete, path: lifecycleOwn, body: `{"password":"fictional wrong password"}`}
	wrong := b.call(t, holder, own, http.StatusUnprocessableEntity)
	if !bytes.Contains(wrong.body, []byte("body.password")) {
		t.Fatal("wrong password did not produce a field error")
	}
	own.body = `{"password":"` + demoPassword + `","locale":"de"}`
	result := b.call(t, holder, own, http.StatusNoContent)
	if result.header.Get("Set-Cookie") == "" {
		t.Error("self-deletion did not clear the session cookie")
	}
	b.call(t, holder, request{method: http.MethodGet, path: lifecycleMe}, http.StatusUnauthorized)

	outbox := read[struct {
		Mails []struct{ To, Template string }
	}](t, b.fixture(t, http.MethodGet, "outbox", "", http.StatusOK))
	notifications := 0
	for _, mail := range outbox.Mails {
		if mail.To == email && mail.Template == "account-deleted" {
			notifications++
		}
	}
	if notifications != 1 {
		t.Errorf("self-deletion queued %d confirmations, want 1", notifications)
	}
	type auditEvent struct {
		Action       string
		Actor        *struct{ ID string }
		TargetPerson *struct{ ID string }
	}
	page := read[struct{ Items []auditEvent }](t, get(t, b, admin, "/api/v1/admin/audit"))
	for _, event := range page.Items {
		if event.Action == "account-deleted" && event.TargetPerson != nil && event.TargetPerson.ID == account.PersonID && event.Actor == nil {
			return
		}
	}
	t.Error("self-deletion is missing an actorless audit event about the retained person")
}
