package app_test

import (
	"encoding/json"
	"maps"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/app"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/seed"
)

// accessLevels pins the access level of every operation (ADR 0005). A new
// operation must be added here, and changing a level is a visible diff.
var accessLevels = map[string]string{
	"getHealth":              "public",
	"getPublicSettings":      "public",
	"listLocations":          "public",
	"listRooms":              "public",
	"listStaff":              "public",
	"listCourseTypes":        "public",
	"createLocation":         "office",
	"updateLocation":         "office",
	"deleteLocation":         "office",
	"createRoom":             "office",
	"updateRoom":             "office",
	"deleteRoom":             "office",
	"createCourseType":       "office",
	"updateCourseType":       "office",
	"deleteCourseType":       "office",
	"listAllStaff":           "office",
	"createStaff":            "office",
	"updateStaff":            "office",
	"deleteStaff":            "office",
	"listPersons":            "office",
	"getPerson":              "office",
	"createPerson":           "office",
	"updatePerson":           "office",
	"deletePerson":           "office",
	"addGuardian":            "office",
	"removeGuardian":         "office",
	"createPersonAccount":    "office",
	"listTerms":              "public",
	"createTerm":             "office",
	"updateTerm":             "office",
	"deleteTerm":             "office",
	"listClasses":            "public",
	"listAllClasses":         "office",
	"createClass":            "office",
	"updateClass":            "office",
	"deleteClass":            "office",
	"listClassMembers":       "office",
	"addClassMember":         "office",
	"removeClassMember":      "office",
	"getSchedule":            "public",
	"getLesson":              "public",
	"createLesson":           "office",
	"updateLesson":           "office",
	"cancelLesson":           "office",
	"restoreLesson":          "office",
	"deleteLesson":           "office",
	"previewLessonClone":     "office",
	"cloneLessons":           "office",
	"bookLesson":             "signedIn",
	"cancelBooking":          "signedIn",
	"previewCancelFollowing": "signedIn",
	"cancelFollowing":        "signedIn",
	"register":               "public",
	"confirmEmail":           "public",
	"login":                  "public",
	"logout":                 "public",
	"getMe":                  "signedIn",
	"stopImpersonation":      "signedIn",
	"requestPasswordReset":   "public",
	"resetPassword":          "public",
	"changePassword":         "signedIn",
	"requestEmailChange":     "signedIn",
	"confirmEmailChange":     "public",
	"updateLocale":           "signedIn",
	"getMyFamily":            "signedIn",
	"updateMyPerson":         "signedIn",
	"addDependent":           "signedIn",
	"updateDependent":        "signedIn",
	"removeDependent":        "signedIn",
	"listDemoMailbox":        "public",
	"listDemoLogins":         "public",
	"listAccounts":           "admin",
	"impersonateAccount":     "admin",
	"listAuditEvents":        "admin",
}

// minRoles is the least role each access level admits.
var minRoles = map[string]domain.Role{
	"public":   domain.RoleGuest,
	"signedIn": domain.RoleMember,
	"staff":    domain.RoleInstructor,
	"office":   domain.RoleOffice,
	"admin":    domain.RoleAdmin,
}

type specOperation struct {
	id, method, path, access string
}

// specOperations reads the operations and their x-access level from the
// served OpenAPI document.
func specOperations(t *testing.T, a *authApp) []specOperation {
	t.Helper()

	res, body := a.call(http.MethodGet, "/api/v1/openapi.json", "")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("openapi: %d", res.StatusCode)
	}

	var spec struct {
		Paths map[string]map[string]struct {
			OperationID string `json:"operationId"`
			Access      string `json:"x-access"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(body, &spec); err != nil {
		t.Fatal(err)
	}

	var ops []specOperation

	for path, methods := range spec.Paths {
		for method, op := range methods {
			ops = append(ops, specOperation{op.OperationID, strings.ToUpper(method), path, op.Access})
		}
	}

	slices.SortFunc(ops, func(a, b specOperation) int { return strings.Compare(a.id, b.id) })

	return ops
}

func TestAccessLevelsPinned(t *testing.T) {
	t.Parallel()

	a := newAuthApp(t, app.Deps{Demo: true, DemoLogins: seed.DemoLogins()})
	ops := specOperations(t, a)

	seen := map[string]bool{}

	for _, op := range ops {
		seen[op.id] = true

		want, ok := accessLevels[op.id]
		switch {
		case !ok:
			t.Errorf("operation %s (x-access %q) is missing from accessLevels", op.id, op.access)
		case op.access != want:
			t.Errorf("operation %s: x-access %q, want %q", op.id, op.access, want)
		}
	}

	for id := range maps.Keys(accessLevels) {
		if !seen[id] {
			t.Errorf("accessLevels lists %s, which the API does not have", id)
		}
	}
}

// TestAccessMatrix calls every operation as a guest and as every role. A
// caller below the operation's level gets 401 (guest) or 403; everyone else
// gets past the access check, whatever the operation then answers to the
// placeholder input.
func TestAccessMatrix(t *testing.T) {
	t.Parallel()

	a := newAuthApp(t, app.Deps{Demo: true, DemoLogins: seed.DemoLogins()})

	logins := map[domain.Role]seed.Login{}
	for _, l := range seed.DemoLogins() {
		logins[l.Role] = l
	}

	// One session per role, so the matrix stays below the login rate
	// limit; calling logout ends it, and the next call signs in again.
	sessions := map[domain.Role]*http.Cookie{}
	session := func(role domain.Role) *http.Cookie {
		if c, ok := sessions[role]; ok {
			return c
		}

		l := logins[role]

		res, out := a.call(http.MethodPost, "/api/v1/auth/login",
			`{"email":"`+l.Email+`","password":"`+l.Password+`"}`)
		if res.StatusCode != http.StatusOK {
			t.Fatalf("login as %s: %d %s", role, res.StatusCode, out)
		}

		sessions[role] = sessionCookie(t, res)

		return sessions[role]
	}

	const placeholder = "00000000-0000-7000-8000-00000000f001"

	for _, op := range specOperations(t, a) {
		path := op.path
		for strings.Contains(path, "{") {
			start, end := strings.Index(path, "{"), strings.Index(path, "}")
			path = path[:start] + placeholder + path[end+1:]
		}

		body := ""
		if op.method != http.MethodGet {
			body = "{}"
		}

		minimum := minRoles[accessLevels[op.id]]

		for _, role := range domain.Roles() {
			var cookies []*http.Cookie

			if role != domain.RoleGuest {
				cookies = append(cookies, session(role))
			}

			res, out := a.call(op.method, path, body, cookies...)
			if op.id == "logout" {
				delete(sessions, role)
			}

			switch {
			case !role.AtLeast(minimum) && role == domain.RoleGuest:
				if res.StatusCode != http.StatusUnauthorized {
					t.Errorf("%s as %s: status %d, want 401 (%s)", op.id, role, res.StatusCode, out)
				}
			case !role.AtLeast(minimum):
				if res.StatusCode != http.StatusForbidden {
					t.Errorf("%s as %s: status %d, want 403 (%s)", op.id, role, res.StatusCode, out)
				}
			case res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden:
				t.Errorf("%s as %s: status %d, want access (%s)", op.id, role, res.StatusCode, out)
			}
		}
	}
}

// TestImpersonationFlow signs in as the demo admin, impersonates the demo
// member, sees the impersonator in getMe, and returns.
func TestImpersonationFlow(t *testing.T) {
	t.Parallel()

	a := newAuthApp(t, app.Deps{Demo: true, DemoLogins: seed.DemoLogins()})

	var admin, member seed.Login

	for _, l := range seed.DemoLogins() {
		switch l.Role {
		case domain.RoleAdmin:
			admin = l
		case domain.RoleMember:
			member = l
		}
	}

	res, body := a.call(http.MethodPost, "/api/v1/auth/login", `{"email":"`+admin.Email+`","password":"`+admin.Password+`"}`)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("login: %d %s", res.StatusCode, body)
	}

	adminCookie := sessionCookie(t, res)

	res, body = a.call(http.MethodGet, "/api/v1/admin/accounts", "", adminCookie)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("listAccounts: %d %s", res.StatusCode, body)
	}

	var list struct {
		Items []struct {
			ID, Email, Role string
			Confirmed       bool
		} `json:"items"`
	}
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatal(err)
	}

	memberID := ""

	for _, it := range list.Items {
		if it.Email == member.Email {
			memberID = it.ID
		}
	}

	if memberID == "" || len(list.Items) < 4 {
		t.Fatalf("accounts %s lack the demo member", body)
	}

	res, body = a.call(http.MethodPost, "/api/v1/admin/accounts/"+memberID+"/impersonate", "", adminCookie)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("impersonate: %d %s", res.StatusCode, body)
	}

	asMember := sessionCookie(t, res)
	if me := decode(t, body); me["email"] != member.Email || me["role"] != "member" {
		t.Fatalf("impersonate answered %s", body)
	}

	// The admin cookie no longer works; the member session names the admin.
	if res, _ := a.call(http.MethodGet, "/api/v1/auth/me", "", adminCookie); res.StatusCode != http.StatusUnauthorized {
		t.Errorf("old admin cookie: status %d, want 401", res.StatusCode)
	}

	res, body = a.call(http.MethodGet, "/api/v1/auth/me", "", asMember)

	imp, _ := decode(t, body)["impersonator"].(map[string]any)
	if res.StatusCode != http.StatusOK || imp["email"] != admin.Email {
		t.Fatalf("getMe while impersonating: %d %s", res.StatusCode, body)
	}

	// As the member, admin operations are forbidden.
	if res, _ := a.call(http.MethodGet, "/api/v1/admin/accounts", "", asMember); res.StatusCode != http.StatusForbidden {
		t.Errorf("listAccounts as the impersonated member: status %d, want 403", res.StatusCode)
	}

	res, body = a.call(http.MethodPost, "/api/v1/auth/impersonation/stop", "", asMember)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("stop: %d %s", res.StatusCode, body)
	}

	back := sessionCookie(t, res)
	if me := decode(t, body); me["email"] != admin.Email || me["impersonator"] != nil {
		t.Fatalf("stop answered %s", body)
	}

	if res, _ := a.call(http.MethodGet, "/api/v1/auth/me", "", asMember); res.StatusCode != http.StatusUnauthorized {
		t.Errorf("member cookie after return: status %d, want 401", res.StatusCode)
	}

	if res, body := a.call(http.MethodPost, "/api/v1/auth/impersonation/stop", "", back); res.StatusCode != http.StatusConflict {
		t.Errorf("stop without impersonation: status %d, want 409 (%s)", res.StatusCode, body)
	}

	// Another admin cannot be impersonated.
	adminID := ""

	for _, it := range list.Items {
		if it.Role == "admin" {
			adminID = it.ID
		}
	}

	if res, _ := a.call(http.MethodPost, "/api/v1/admin/accounts/"+adminID+"/impersonate", "", back); res.StatusCode != http.StatusForbidden {
		t.Errorf("impersonating an admin: status %d, want 403", res.StatusCode)
	}
}

// TestAuditLogListsImpersonation: after an admin impersonated the demo
// member and returned, the audit log names both events, newest first, with
// the admin as actor and the member as target.
func TestAuditLogListsImpersonation(t *testing.T) {
	t.Parallel()

	a := newAuthApp(t, app.Deps{Demo: true, DemoLogins: seed.DemoLogins()})

	var admin, member seed.Login

	for _, l := range seed.DemoLogins() {
		switch l.Role {
		case domain.RoleAdmin:
			admin = l
		case domain.RoleMember:
			member = l
		}
	}

	res, body := a.call(http.MethodPost, "/api/v1/auth/login", `{"email":"`+admin.Email+`","password":"`+admin.Password+`"}`)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("login: %d %s", res.StatusCode, body)
	}

	adminCookie := sessionCookie(t, res)

	res, body = a.call(http.MethodGet, "/api/v1/admin/audit", "", adminCookie)
	if res.StatusCode != http.StatusOK || string(body) != `{"items":[]}`+"\n" {
		t.Fatalf("empty audit log: %d %q", res.StatusCode, body)
	}

	res, body = a.call(http.MethodGet, "/api/v1/admin/accounts", "", adminCookie)

	var accounts struct {
		Items []struct{ ID, Email string } `json:"items"`
	}
	if err := json.Unmarshal(body, &accounts); err != nil || res.StatusCode != http.StatusOK {
		t.Fatalf("listAccounts: %d %s", res.StatusCode, body)
	}

	memberID := ""

	for _, it := range accounts.Items {
		if it.Email == member.Email {
			memberID = it.ID
		}
	}

	res, body = a.call(http.MethodPost, "/api/v1/admin/accounts/"+memberID+"/impersonate", "", adminCookie)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("impersonate: %d %s", res.StatusCode, body)
	}

	res, body = a.call(http.MethodPost, "/api/v1/auth/impersonation/stop", "", sessionCookie(t, res))
	if res.StatusCode != http.StatusOK {
		t.Fatalf("stop: %d %s", res.StatusCode, body)
	}

	res, body = a.call(http.MethodGet, "/api/v1/admin/audit", "", sessionCookie(t, res))
	if res.StatusCode != http.StatusOK {
		t.Fatalf("listAuditEvents: %d %s", res.StatusCode, body)
	}

	type account struct{ ID, Email, GivenName, FamilyName string }

	var log struct {
		Items []struct {
			ID, Action, At string
			Actor, Target  account
		} `json:"items"`
	}
	if err := json.Unmarshal(body, &log); err != nil {
		t.Fatal(err)
	}

	want := []string{"impersonation-ended", "impersonation-started"}
	if len(log.Items) != len(want) {
		t.Fatalf("audit log %s, want two events", body)
	}

	for i, e := range log.Items {
		if e.ID == "" || e.At == "" || e.Action != want[i] || e.Actor.Email != admin.Email ||
			e.Actor.GivenName == "" || e.Target.Email != member.Email || e.Target.ID != memberID {
			t.Errorf("event %d = %+v", i, e)
		}
	}
}

// TestSignInEndsThePreviousSession signs in while signed in: the previous
// session ends, so its cookie stops working. (For an impersonation that is
// also audited; see the service's Logout.)
func TestSignInEndsThePreviousSession(t *testing.T) {
	t.Parallel()

	a := newAuthApp(t, app.Deps{Demo: true, DemoLogins: seed.DemoLogins()})
	login := func(role domain.Role, cookies ...*http.Cookie) *http.Cookie {
		t.Helper()

		for _, l := range seed.DemoLogins() {
			if l.Role == role {
				res, body := a.call(http.MethodPost, "/api/v1/auth/login",
					`{"email":"`+l.Email+`","password":"`+l.Password+`"}`, cookies...)
				if res.StatusCode != http.StatusOK {
					t.Fatalf("login as %s: %d %s", role, res.StatusCode, body)
				}

				return sessionCookie(t, res)
			}
		}

		t.Fatalf("no demo login for %s", role)

		return nil
	}

	first := login(domain.RoleMember)
	second := login(domain.RoleInstructor, first)

	if res, _ := a.call(http.MethodGet, "/api/v1/auth/me", "", first); res.StatusCode != http.StatusUnauthorized {
		t.Errorf("first session after signing in again: status %d, want 401", res.StatusCode)
	}

	if res, _ := a.call(http.MethodGet, "/api/v1/auth/me", "", second); res.StatusCode != http.StatusOK {
		t.Errorf("second session: status %d, want 200", res.StatusCode)
	}
}
