package conformance

import (
	"bytes"
	"net/http"
	"testing"
)

const (
	demoPassword = "demo-password"
	demoMember   = "lina.mistlewood@example.com"
	demoAdmin    = "arno.keystone@example.net"
)

func recoverPassword(t *testing.T, b *backend) {
	t.Helper()

	original := browser(t)
	visitor := browser(t)
	login(t, b, original, demoMember, demoPassword)
	reset := request{
		method: http.MethodPost, path: "/api/v1/auth/password-reset",
		body: `{"email":"` + demoMember + `","locale":"de"}`,
	}
	b.call(t, visitor, reset, http.StatusAccepted)
	token := mailToken(t, b, demoMember, "reset-password", "/reset-password")

	reset.path += "/confirm"
	reset.body = `{"token":"` + token + `","password":"Sunshine"}`
	weak := b.call(t, visitor, reset, http.StatusUnprocessableEntity)
	if !bytes.Contains(weak.body, []byte("body.password")) {
		t.Error("the rejected password must be named in the problem")
	}

	const newPassword = "a different fixture password"
	reset.body = `{"token":"` + token + `","password":"` + newPassword + `"}`
	b.call(t, visitor, reset, http.StatusNoContent)
	b.call(t, visitor, reset, http.StatusGone)
	b.call(t, original, request{method: http.MethodGet, path: "/api/v1/auth/me"}, http.StatusUnauthorized)
	b.call(t, visitor, request{
		method: http.MethodPost, path: "/api/v1/auth/login",
		body: `{"email":"` + demoMember + `","password":"` + demoPassword + `"}`,
	}, http.StatusUnauthorized)
	login(t, b, visitor, demoMember, newPassword)
}

func loginRateLimit(t *testing.T, b *backend) {
	t.Helper()

	client := browser(t)
	in := request{
		method: http.MethodPost, path: "/api/v1/auth/login",
		body: `{"email":"` + demoMember + `","password":"incorrect-password"}`,
	}
	for range 5 {
		b.call(t, client, in, http.StatusUnauthorized)
	}
	rate := b.call(t, client, in, http.StatusTooManyRequests)
	if rate.header.Get("Retry-After") == "" {
		t.Error("rate limited login lacked Retry-After")
	}

	b.fixture(t, http.MethodPut, "clock", `{"now":"2026-10-23T16:16:00Z"}`, http.StatusNoContent)
	login(t, b, client, demoMember, demoPassword)
}

func adminImpersonation(t *testing.T, b *backend) {
	t.Helper()

	admin := browser(t)
	member := browser(t)
	office := browser(t)
	type account struct {
		AccountID, Email, Role string
		Impersonator           *struct{ Email string }
	}
	login(t, b, admin, demoAdmin, demoPassword)
	subject := read[account](t, login(t, b, member, demoMember, demoPassword))
	login(t, b, office, "olga.ledgerby@example.net", demoPassword)

	impersonate := request{
		method: http.MethodPost, path: "/api/v1/admin/accounts/{accountId}/impersonate",
		params: map[string]string{"accountId": subject.AccountID},
	}
	b.call(t, office, impersonate, http.StatusForbidden)
	b.call(t, admin, impersonate, http.StatusOK)

	effective := read[account](t, get(t, b, admin, "/api/v1/auth/me"))
	if effective.Email != demoMember || effective.Role != "member" || effective.Impersonator == nil ||
		effective.Impersonator.Email != demoAdmin {
		t.Fatal("admin session did not assume the member role and record the impersonator")
	}
	b.call(t, admin, request{method: http.MethodGet, path: "/api/v1/admin/accounts"}, http.StatusForbidden)
	b.call(t, admin, request{method: http.MethodPost, path: "/api/v1/auth/impersonation/stop"}, http.StatusOK)
	restored := read[account](t, get(t, b, admin, "/api/v1/auth/me"))
	if restored.Email != demoAdmin || restored.Role != "admin" || restored.Impersonator != nil {
		t.Fatal("ending impersonation did not restore the admin session")
	}
}
