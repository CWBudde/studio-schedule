package conformance

import (
	"bytes"
	"net/http"
	"strings"
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
	const resetPath = "/api/v1/auth/password-reset"
	const unknownEmail = "unknown-reset@example.test"
	reset := request{method: http.MethodPost, path: resetPath, body: `{"email":""}`}
	b.call(t, visitor, reset, http.StatusUnprocessableEntity)
	reset.body = `{"Email":"` + unknownEmail + `","Locale":null}`
	unknown := b.call(t, visitor, reset, http.StatusAccepted)
	outbox := read[struct{ Mails []struct{ To string } }](t, b.fixture(t, http.MethodGet, "outbox", "", http.StatusOK))
	for _, mail := range outbox.Mails {
		if mail.To == unknownEmail {
			t.Fatal("password recovery queued a mail for an unknown address")
		}
	}

	reset.body = `{"email":" ` + strings.ToUpper(demoMember) + ` ","locale":"de"}`
	known := b.call(t, visitor, reset, http.StatusAccepted)
	if !bytes.Equal(unknown.body, known.body) {
		t.Fatal("the password-reset response depends on whether the account exists")
	}
	staleToken := mailToken(t, b, demoMember, "reset-password", "/reset-password")

	b.fixture(t, http.MethodPut, "clock", `{"now":"2026-10-23T16:00:01Z"}`, http.StatusNoContent)
	b.call(t, visitor, reset, http.StatusAccepted)
	token := mailToken(t, b, demoMember, "reset-password", "/reset-password")
	if token == staleToken {
		t.Fatal("a new reset request must issue a different token")
	}

	reset.path += "/confirm"
	reset.body = `{"token":"` + staleToken + `","password":"a discarded fixture password"}`
	b.call(t, visitor, reset, http.StatusGone)
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

	reset.path = resetPath
	reset.body = `{"email":"` + unknownEmail + `"}`
	for range 2 {
		b.call(t, visitor, reset, http.StatusAccepted)
	}
	limited := b.call(t, visitor, reset, http.StatusTooManyRequests)
	if limited.header.Get("Retry-After") == "" {
		t.Error("rate-limited password recovery lacked Retry-After")
	}
}

func changeCredentials(t *testing.T, b *backend) {
	t.Helper()

	const newPassword = "different fixture credentials"
	const abandoned = "abandoned-change@example.test"
	const newEmail = "changed-address@example.test"
	current, other, guest := browser(t), browser(t), browser(t)
	login(t, b, current, demoMember, demoPassword)
	login(t, b, other, demoMember, demoPassword)
	me := request{method: http.MethodGet, path: "/api/v1/auth/me"}
	locale := request{method: http.MethodPut, path: "/api/v1/auth/locale", body: `{"locale":"fr"}`}
	b.call(t, guest, locale, http.StatusUnauthorized)
	b.call(t, current, locale, http.StatusUnprocessableEntity)
	locale.body = `{"locale":"en"}`
	b.call(t, current, locale, http.StatusNoContent)
	if profile := read[struct{ Locale string }](t, b.call(t, current, me, http.StatusOK)); profile.Locale != "en" {
		t.Errorf("set account mail locale to %q, want en", profile.Locale)
	}
	locale.body = `{}`
	b.call(t, current, locale, http.StatusNoContent)
	if profile := read[struct{ Locale string }](t, b.call(t, current, me, http.StatusOK)); profile.Locale != "" {
		t.Errorf("reset account mail locale to %q, want the school default", profile.Locale)
	}

	email := request{
		method: http.MethodPost, path: "/api/v1/auth/email",
		body: `{"newEmail":"` + abandoned + `","password":"` + demoPassword + `"}`,
	}
	b.call(t, guest, email, http.StatusUnauthorized)
	b.call(t, current, email, http.StatusAccepted)
	abandonedToken := mailToken(t, b, abandoned, "change-email", "/confirm-email-change")

	password := request{
		method: http.MethodPost, path: "/api/v1/auth/password",
		body: `{"currentPassword":"wrong fixture password","newPassword":"` + newPassword + `"}`,
	}
	b.call(t, guest, password, http.StatusUnauthorized)
	wrong := b.call(t, current, password, http.StatusUnprocessableEntity)
	if !bytes.Contains(wrong.body, []byte("body.currentPassword")) {
		t.Error("wrong password error must identify currentPassword")
	}
	password.body = `{"currentPassword":"` + demoPassword + `","newPassword":"` + newPassword + `"}`
	b.call(t, current, password, http.StatusNoContent)
	b.call(t, current, me, http.StatusOK)
	b.call(t, other, me, http.StatusUnauthorized)
	confirm := request{
		method: http.MethodPost, path: "/api/v1/auth/email/confirm",
		body: `{"token":"` + abandonedToken + `"}`,
	}
	b.call(t, guest, confirm, http.StatusGone)

	email.body = `{"newEmail":"` + demoAdmin + `","password":"` + newPassword + `"}`
	taken := b.call(t, current, email, http.StatusAccepted)
	email.body = `{"newEmail":" ` + newEmail + ` ","password":"` + newPassword + `","locale":null}`
	free := b.call(t, current, email, http.StatusAccepted)
	if !bytes.Equal(taken.body, free.body) {
		t.Error("email change response depends on whether the address is already taken")
	}
	if profile := read[struct{ Email string }](t, b.call(t, current, me, http.StatusOK)); profile.Email != demoMember {
		t.Fatalf("email before confirmation: %q, want %q", profile.Email, demoMember)
	}
	token := mailToken(t, b, newEmail, "change-email", "/confirm-email-change")
	confirm.body = `{"token":"` + token + `"}`
	b.call(t, guest, confirm, http.StatusNoContent)
	b.call(t, guest, confirm, http.StatusGone)
	if profile := read[struct{ Email string }](t, b.call(t, current, me, http.StatusOK)); profile.Email != newEmail {
		t.Fatalf("email after confirmation: %q, want %q", profile.Email, newEmail)
	}
	login(t, b, guest, newEmail, newPassword)

	outbox := read[struct {
		Mails []struct{ To, Template string }
	}](t, b.fixture(t, http.MethodGet, "outbox", "", http.StatusOK))
	var passwordNotices, emailNotices int
	for _, mail := range outbox.Mails {
		switch {
		case mail.Template == "change-email" && mail.To == demoAdmin:
			t.Error("email change queued a link to a taken address")
		case mail.Template == "password-changed" && mail.To == demoMember:
			passwordNotices++
		case mail.Template == "email-changed" && mail.To == demoMember:
			emailNotices++
		}
	}
	if passwordNotices != 1 || emailNotices != 1 {
		t.Errorf("old address received %d password and %d email change notices, want one of each", passwordNotices, emailNotices)
	}
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

	audit := request{method: http.MethodGet, path: "/api/v1/admin/audit"}
	b.call(t, browser(t), audit, http.StatusUnauthorized)
	b.call(t, member, audit, http.StatusForbidden)
	b.call(t, office, audit, http.StatusForbidden)

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

	b.call(t, admin, audit, http.StatusForbidden)
	b.fixture(t, http.MethodPut, "clock", `{"now":"2026-10-23T16:00:01Z"}`, http.StatusNoContent)
	b.call(t, admin, request{method: http.MethodPost, path: "/api/v1/auth/impersonation/stop"}, http.StatusOK)
	restored := read[account](t, get(t, b, admin, "/api/v1/auth/me"))
	if restored.Email != demoAdmin || restored.Role != "admin" || restored.Impersonator != nil {
		t.Fatal("ending impersonation did not restore the admin session")
	}

	assertImpersonationAudit(t, b, admin, restored.AccountID, subject.AccountID)
}

func assertImpersonationAudit(t *testing.T, b *backend, admin *http.Client, actorID, subjectID string) {
	t.Helper()

	type auditAccount struct{ ID, Email string }
	type auditEvent struct {
		Action, At    string
		Actor, Target *auditAccount
	}
	page := read[struct{ Items []auditEvent }](t, get(t, b, admin, "/api/v1/admin/audit"))
	if len(page.Items) < 2 {
		t.Fatalf("expected two impersonation audit records, got %d", len(page.Items))
	}
	for index, want := range []auditEvent{
		{Action: "impersonation-ended", At: "2026-10-23T16:00:01Z"},
		{Action: "impersonation-started", At: demoNow},
	} {
		event := page.Items[index]
		if event.Action != want.Action || event.At != want.At ||
			event.Actor == nil || *event.Actor != (auditAccount{ID: actorID, Email: demoAdmin}) ||
			event.Target == nil || *event.Target != (auditAccount{ID: subjectID, Email: demoMember}) {
			t.Errorf("unexpected audit event at position %d: %+v", index, event)
		}
	}
}
