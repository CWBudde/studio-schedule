package conformance

import (
	"net/http"
	"strings"
	"testing"
)

type invitedAccount struct{ ID, Email, Role, Status string }

func officeAccountInvitation(t *testing.T, b *backend) {
	t.Helper()

	const email = "dependent-invite@example.test"
	const expiredEmail = "admin-invite@example.test"
	visitor, parent, office, admin := browser(t), browser(t), browser(t), browser(t)
	login(t, b, parent, "clara.amberly@example.org", demoPassword)
	login(t, b, office, "olga.ledgerby@example.net", demoPassword)
	login(t, b, admin, demoAdmin, demoPassword)
	child := read[struct{ ID string }](t, b.call(t, parent, request{
		method: http.MethodPost, path: "/api/v1/family/dependents",
		body: `{"givenName":"Nia","familyName":"Fixture"}`,
	}, http.StatusCreated))
	invite := func(person, address, role string) request {
		return request{
			method: http.MethodPost, path: "/api/v1/admin/persons/{personId}/account",
			params: map[string]string{"personId": person},
			body:   jsonBody(t, map[string]string{"email": address, "role": role}),
		}
	}
	b.call(t, visitor, invite(child.ID, email, "member"), http.StatusUnauthorized)
	b.call(t, parent, invite(child.ID, email, "member"), http.StatusForbidden)
	b.call(t, office, invite(child.ID, email, "admin"), http.StatusForbidden)
	b.call(t, office, invite(child.ID, email, "guest"), http.StatusUnprocessableEntity)
	b.call(t, office, invite(child.ID, "bad address", "member"), http.StatusUnprocessableEntity)
	checkLessonField(t, b.call(t, office, invite(child.ID, "\n"+email, "member"), http.StatusUnprocessableEntity), "body.email")
	b.call(t, office, invite("018f0000-0000-7000-8000-000000000999", email, "member"), http.StatusNotFound)
	expectConflict(t, b, office, invite(child.ID, strings.ToUpper(demoMember), "member"), "duplicate")
	created := read[invitedAccount](t, b.call(t, office, invite(child.ID, email, "member"), http.StatusCreated))
	if want := (invitedAccount{ID: created.ID, Email: email, Role: "member", Status: "pending"}); created.ID == "" || created != want {
		t.Fatalf("unexpected invited account: %+v", created)
	}
	expectConflict(t, b, office, invite(child.ID, "another-invite@example.test", "member"), "has-account")
	expectConflict(t, b, parent, request{
		method: http.MethodPut, path: "/api/v1/family/dependents/{personId}",
		params: map[string]string{"personId": child.ID}, body: `{"givenName":"Changed","familyName":"Fixture"}`,
	}, "managed")

	person := read[struct{ ID string }](t, b.call(t, office, request{
		method: http.MethodPost, path: "/api/v1/admin/persons", body: `{"givenName":"Robin","familyName":"Fixture"}`,
	}, http.StatusCreated))
	adminInvite := read[invitedAccount](t, b.call(t, admin, invite(person.ID, expiredEmail, "admin"), http.StatusCreated))
	if adminInvite.Role != "admin" || adminInvite.Status != "pending" {
		t.Fatalf("unexpected invited admin: %+v", adminInvite)
	}
	checkInvitationRedemption(t, b, visitor, child.ID, created.ID, email, expiredEmail)
}

func checkInvitationRedemption(t *testing.T, b *backend, visitor *http.Client, personID, accountID, email, expiredEmail string) {
	t.Helper()

	const password = "fixture invitation password"
	const otherPassword = "fixture registration password"
	register := request{
		method: http.MethodPost, path: "/api/v1/auth/register",
		body: jsonBody(t, map[string]string{
			"givenName": "Replacement", "familyName": "Fixture", "email": email, "password": otherPassword,
		}),
	}
	b.call(t, visitor, register, http.StatusAccepted)
	b.call(t, visitor, request{
		method: http.MethodPost, path: "/api/v1/auth/login",
		body: jsonBody(t, map[string]string{"email": email, "password": otherPassword}),
	}, http.StatusUnauthorized)
	for _, mail := range read[struct {
		Mails []struct{ To, Template string }
	}](t, b.fixture(t, http.MethodGet, "outbox", "", http.StatusOK)).Mails {
		if mail.To == email && mail.Template == "confirm-email" {
			t.Fatal("registration queued a confirmation for the invited account")
		}
	}
	token := mailToken(t, b, email, "invite", "/reset-password")
	expired := mailToken(t, b, expiredEmail, "invite", "/reset-password")
	confirm := func(token string) request {
		return request{
			method: http.MethodPost, path: "/api/v1/auth/password-reset/confirm",
			body: jsonBody(t, map[string]string{"token": token, "password": password}),
		}
	}
	b.fixture(t, http.MethodPut, "clock", `{"now":"2026-10-26T15:59:59Z"}`, http.StatusNoContent)
	b.call(t, visitor, confirm(token), http.StatusNoContent)
	b.call(t, visitor, confirm(token), http.StatusGone)
	if account := read[struct{ AccountID, PersonID, GivenName string }](t, login(t, b, visitor, email, password)); account.AccountID != accountID || account.PersonID != personID || account.GivenName != "Nia" {
		t.Fatalf("invitation did not preserve the existing person: %+v", account)
	}

	b.fixture(t, http.MethodPut, "clock", `{"now":"2026-10-26T16:00:00Z"}`, http.StatusNoContent)
	b.call(t, visitor, confirm(expired), http.StatusGone)
	b.call(t, visitor, request{
		method: http.MethodPost, path: "/api/v1/auth/password-reset",
		body: jsonBody(t, map[string]string{"email": expiredEmail}),
	}, http.StatusAccepted)
	recovery := mailToken(t, b, expiredEmail, "reset-password", "/reset-password")
	b.call(t, visitor, confirm(recovery), http.StatusNoContent)
}
