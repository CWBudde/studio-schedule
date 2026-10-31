package app_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/app"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/seed"
)

// accountIDByEmail returns the account ID of email from the admin's account
// list.
func (a *authApp) accountIDByEmail(email string, admin *http.Cookie) string {
	a.t.Helper()

	var list struct {
		Items []struct{ ID, Email string } `json:"items"`
	}
	if err := json.Unmarshal(a.expect(http.StatusOK, http.MethodGet, "/api/v1/admin/accounts", "", admin), &list); err != nil {
		a.t.Fatal(err)
	}

	for _, it := range list.Items {
		if it.Email == email {
			return it.ID
		}
	}

	a.t.Fatalf("no account %s", email)

	return ""
}

// impersonateByEmail signs the admin in as the account of email and returns the
// impersonation's session cookie.
func (a *authApp) impersonateByEmail(email string, admin *http.Cookie) *http.Cookie {
	a.t.Helper()

	res, body := a.call(http.MethodPost, "/api/v1/admin/accounts/"+a.accountIDByEmail(email, admin)+"/impersonate", "", admin)
	if res.StatusCode != http.StatusOK {
		a.t.Fatalf("impersonate %s: %d %s", email, res.StatusCode, body)
	}

	return sessionCookie(a.t, res)
}

// problemDetail is the detail of a problem.
func problemDetail(t *testing.T, body []byte) string {
	t.Helper()

	d, _ := decode(t, body)["detail"].(string)

	return d
}

// TestImpersonateInactiveAccount: an admin cannot sign in as an account
// that may not sign in itself, unconfirmed or disabled (409), and keeps
// their own session.
func TestImpersonateInactiveAccount(t *testing.T) {
	t.Parallel()

	a := newAuthApp(t, app.Deps{Demo: true})
	admin := a.login(demoLogin(t, domain.RoleAdmin).Email)

	a.want(http.MethodPost, "/api/v1/auth/register", registration, http.StatusAccepted)
	a.expect(http.StatusOK, http.MethodPost, "/api/v1/admin/accounts/"+a.accountIDByEmail("oskar.nettlebury@example.org", admin)+"/disable", "", admin)

	for _, email := range []string{"ada@example.org", "oskar.nettlebury@example.org"} {
		body := a.expect(http.StatusConflict, http.MethodPost,
			"/api/v1/admin/accounts/"+a.accountIDByEmail(email, admin)+"/impersonate", "", admin)
		if d := problemDetail(t, body); d != "the account is disabled or not confirmed" {
			t.Errorf("impersonate %s: detail %q", email, d)
		}
	}

	// The refused impersonation did not end the admin's session.
	if me := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/auth/me", "", admin)); me["role"] != "admin" {
		t.Errorf("admin after the refusal: %v", me)
	}
}

// TestCredentialsRefusedWhileImpersonating: an admin signed in as a member
// can change neither the member's password nor email (403).
func TestCredentialsRefusedWhileImpersonating(t *testing.T) {
	t.Parallel()

	a := newAuthApp(t, app.Deps{Demo: true})
	member := demoMember(t)
	asMember := a.impersonateByEmail(member.Email, a.login(demoLogin(t, domain.RoleAdmin).Email))

	for path, body := range map[string]string{
		"/api/v1/auth/password": `{"currentPassword":"` + seed.DemoPassword + `","newPassword":"a fresh passphrase"}`,
		"/api/v1/auth/email":    `{"password":"` + seed.DemoPassword + `","newEmail":"elsewhere@example.org"}`,
	} {
		if d := problemDetail(t, a.expect(http.StatusForbidden, http.MethodPost, path, body, asMember)); d != "not while signed in as another account" {
			t.Errorf("%s: detail %q", path, d)
		}
	}

	// The member's password is unchanged.
	a.mustLogin(member.Email, seed.DemoPassword)
}

// TestEmailChangeTakenMeanwhile: an address that got an account between
// the request and the confirmation of an email change is a 409; the
// account keeps its address.
func TestEmailChangeTakenMeanwhile(t *testing.T) {
	t.Parallel()

	a := newAuthApp(t, app.Deps{Demo: true})
	kai := a.login("kai.lanternby@example.org")

	a.want(http.MethodPost, "/api/v1/auth/email",
		`{"password":"`+seed.DemoPassword+`","newEmail":"kai.elsewhere@example.org"}`, http.StatusAccepted, kai)
	token := a.linkToken("change-email", "confirm-email-change")

	a.want(http.MethodPost, "/api/v1/auth/register",
		`{"givenName":"Kim","familyName":"Elsewhere","email":"kai.elsewhere@example.org","password":"correct horse"}`,
		http.StatusAccepted)

	body := a.want(http.MethodPost, "/api/v1/auth/email/confirm", `{"token":"`+token+`"}`, http.StatusConflict)
	if d := problemDetail(t, body); d != "the email address belongs to another account" {
		t.Errorf("detail %q", d)
	}

	if me := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/auth/me", "", kai)); me["email"] != "kai.lanternby@example.org" {
		t.Errorf("me = %v, want the old address", me)
	}
}

// TestEmailChangeToOwnAddress: changing the email to the account's own
// address, in any case, is a 422 on body.newEmail. It is checked before the
// password, so a wrong password gets the same answer and does not count as
// a password failure.
func TestEmailChangeToOwnAddress(t *testing.T) {
	t.Parallel()

	a := newAuthApp(t, app.Deps{Demo: true})
	kai := a.login("kai.lanternby@example.org")

	for _, pw := range []string{seed.DemoPassword, "wrong password"} {
		for range 6 {
			body := a.want(http.MethodPost, "/api/v1/auth/email",
				`{"password":"`+pw+`","newEmail":"KAI.Lanternby@example.org"}`, http.StatusUnprocessableEntity, kai)
			if loc := errorLocation(t, body); loc != "body.newEmail" {
				t.Fatalf("location %q, want body.newEmail: %s", loc, body)
			}
		}
	}

	// Twelve refusals, six with a wrong password, and still no 429.
	a.mustLogin("kai.lanternby@example.org", seed.DemoPassword)
}

// TestRegisterRateLimited: a client may register five times an hour; the
// sixth is a 429, and an hour later it may again.
func TestRegisterRateLimited(t *testing.T) {
	t.Parallel()

	a := newAuthApp(t, app.Deps{})
	body := func(i int) string {
		return fmt.Sprintf(`{"givenName":"Rae","familyName":"Example","email":"rae%d@example.org","password":"correct horse"}`, i)
	}

	for i := range 5 {
		a.want(http.MethodPost, "/api/v1/auth/register", body(i), http.StatusAccepted)
	}

	res, out := a.call(http.MethodPost, "/api/v1/auth/register", body(5))
	wantTooMany(t, res, out)

	// The refused registration created no account: the address is free.
	a.clock.advance(time.Hour + time.Minute)
	a.want(http.MethodPost, "/api/v1/auth/register", body(5), http.StatusAccepted)

	admin := a.login(demoLogin(t, domain.RoleAdmin).Email)
	a.accountIDByEmail("rae5@example.org", admin)
}

// TestGetMeSignedOut: getMe without a session is a 401, which the access
// middleware answers (AccessSignedIn) before the handler runs.
func TestGetMeSignedOut(t *testing.T) {
	t.Parallel()

	a := newAuthApp(t, app.Deps{})

	if d := problemDetail(t, a.anonymous(http.StatusUnauthorized, "/api/v1/auth/me")); d != "not signed in" {
		t.Errorf("detail %q", d)
	}
}

// TestDeleteMyAccountBlocked: after five wrong passwords, deleting the
// own account is a 429 even with the right password, which is not checked;
// the account stays.
func TestDeleteMyAccountBlocked(t *testing.T) {
	t.Parallel()

	a := newAuthApp(t, app.Deps{})
	tilo := a.login("tilo.quarrystone@example.org")

	for range 5 {
		a.expect(http.StatusUnprocessableEntity, http.MethodDelete, "/api/v1/my/account", `{"password":"wrong password"}`, tilo)
	}

	res, body := a.call(http.MethodDelete, "/api/v1/my/account", `{"password":"`+seed.DemoPassword+`"}`, tilo)
	wantTooMany(t, res, body)

	if me := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/auth/me", "", tilo)); me["email"] != "tilo.quarrystone@example.org" {
		t.Errorf("me after the 429: %v", me)
	}
}

// TestExportSections: the data export lists the account's calendar
// feeds, its dismissed announcements and the person's waiting-list
// entries, for the member and the office alike.
func TestExportSections(t *testing.T) {
	t.Parallel()

	a := newAuthApp(t, app.Deps{Demo: true})
	office := a.login("olga.ledgerby@example.net")
	clara := a.login("clara.amberly@example.org")
	family := a.family(clara)
	me, ella := family.Me.ID, family.Dependents[0].ID

	feed := decode(t, a.expect(http.StatusCreated, http.MethodPost, "/api/v1/my/calendar-feeds", `{"scope":"bookings"}`, clara))
	feedID, _ := feed["feed"].(map[string]any)["id"].(string)

	ann := decode(t, a.expect(http.StatusCreated, http.MethodPost, "/api/v1/admin/announcements",
		`{"audience":"everyone","title":"Studio repainted","body":"Mind the fresh paint."}`, office))
	annID, _ := ann["id"].(string)
	a.expect(http.StatusNoContent, http.MethodPost, "/api/v1/announcements/"+annID+"/dismiss", "", clara)

	// A full workshop: Ella holds its one place, Clara waits.
	terms := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/terms", "", office))
	termID, _ := terms["items"].([]any)[0].(map[string]any)["id"].(string)
	classes := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/terms/"+termID+"/classes", "", office))
	class, _ := classes["items"].([]any)[0].(map[string]any)
	lesson := decode(t, a.expect(http.StatusCreated, http.MethodPost, "/api/v1/lessons",
		fmt.Sprintf(`{"courseTypeId":%q,"date":"2026-11-03","startTime":"10:00","durationMinutes":60,"roomId":%q,`+
			`"instructorId":%q,"capacity":1,"visibility":"public","kind":"workshop","confirmRoomOverlap":true}`,
			class["courseTypeId"], class["roomId"], class["instructor"].(map[string]any)["id"]), office))
	lessonID, _ := lesson["id"].(string)

	a.expect(http.StatusCreated, http.MethodPost, "/api/v1/lessons/"+lessonID+"/bookings", `{"personId":"`+ella+`"}`, clara)
	entry := decode(t, a.expect(http.StatusCreated, http.MethodPost, "/api/v1/lessons/"+lessonID+"/waitlist", `{"personId":"`+me+`"}`, clara))

	type export struct {
		CalendarFeeds []struct{ ID, Scope, CreatedAt, LastUsedAt string } `json:"calendarFeeds"`
		Dismissed     []struct{ ID, Title, PublishAt string }             `json:"dismissedAnnouncements"`
		Waitlist      []struct {
			ID, Status, JoinedAt, InvitedAt string
			Lesson                          struct{ ID, CourseType, StartsAt string }
		} `json:"waitlistEntries"`
	}

	for name, path := range map[string]string{
		"member": "/api/v1/my/export?person=" + me,
		"office": "/api/v1/admin/persons/" + me + "/export",
	} {
		cookie := clara
		if name == "office" {
			cookie = office
		}

		var x export
		if err := json.Unmarshal(a.expect(http.StatusOK, http.MethodGet, path, "", cookie), &x); err != nil {
			t.Fatal(err)
		}

		if len(x.CalendarFeeds) != 1 || x.CalendarFeeds[0].ID != feedID || x.CalendarFeeds[0].Scope != "bookings" ||
			x.CalendarFeeds[0].CreatedAt != "2026-10-23T16:00:00Z" || x.CalendarFeeds[0].LastUsedAt != "" {
			t.Errorf("%s: calendar feeds %+v", name, x.CalendarFeeds)
		}

		if len(x.Dismissed) != 1 || x.Dismissed[0].ID != annID || x.Dismissed[0].Title != "Studio repainted" ||
			x.Dismissed[0].PublishAt == "" {
			t.Errorf("%s: dismissed %+v", name, x.Dismissed)
		}

		if len(x.Waitlist) != 1 || x.Waitlist[0].ID != entry["id"] || x.Waitlist[0].Status != "waiting" ||
			x.Waitlist[0].Lesson.ID != lessonID || x.Waitlist[0].Lesson.CourseType == "" ||
			x.Waitlist[0].JoinedAt != "2026-10-23T16:00:00Z" || x.Waitlist[0].InvitedAt != "" {
			t.Errorf("%s: waiting list %+v", name, x.Waitlist)
		}
	}

	// Ella's export has her booking but none of Clara's account data.
	var ellaExport export
	if err := json.Unmarshal(a.expect(http.StatusOK, http.MethodGet, "/api/v1/my/export?person="+ella, "", clara), &ellaExport); err != nil {
		t.Fatal(err)
	}

	if len(ellaExport.CalendarFeeds) != 0 || len(ellaExport.Dismissed) != 0 || len(ellaExport.Waitlist) != 0 {
		t.Errorf("Ella's export %+v", ellaExport)
	}
}
