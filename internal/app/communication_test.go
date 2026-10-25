package app_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/app"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/jobs"
	"github.com/cwbudde/studio-schedule/internal/seed"
)

// runJobs runs the background jobs once, as at startup: the announcements
// job emails what is due.
func (a *authApp) runJobs() {
	a.t.Helper()

	if err := app.Jobs(a.deps).RunOnce(a.t.Context()); err != nil {
		a.t.Fatal(err)
	}
}

// demoLogin returns the demo login of a role.
func demoLogin(t *testing.T, role domain.Role) seed.Login {
	t.Helper()

	for _, l := range seed.DemoLogins() {
		if l.Role == role {
			return l
		}
	}

	t.Fatalf("no demo login of role %s", role)

	return seed.Login{}
}

type mailItem struct {
	ID, To, Template, Status, Body, Text, HTML string
}

// mails searches the mail log with filter, a JSON body of searchMails.
func (a *authApp) mails(filter string, cookie *http.Cookie) []mailItem {
	a.t.Helper()

	var page struct {
		Items []mailItem `json:"items"`
	}
	if err := json.Unmarshal(a.expect(http.StatusOK, http.MethodPost, "/api/v1/admin/mails/search", filter, cookie), &page); err != nil {
		a.t.Fatal(err)
	}

	return page.Items
}

// TestMailLogNeverShowsSecretLinks: the confirmation mail of a new account
// is listed, but its body, which holds the link, leaves the server neither
// in the list nor in the detail, and it is not queued again.
func TestMailLogNeverShowsSecretLinks(t *testing.T) {
	t.Parallel()

	a := newAuthApp(t, app.Deps{})
	if res, body := a.call(http.MethodPost, "/api/v1/auth/register", registration); res.StatusCode != http.StatusAccepted {
		t.Fatalf("register: %d %s", res.StatusCode, body)
	}
	admin := a.login(demoLogin(t, domain.RoleAdmin).Email)

	res, list := a.call(http.MethodPost, "/api/v1/admin/mails/search", `{"template":"confirm-email"}`, admin)
	if res.StatusCode != http.StatusOK || strings.Contains(string(list), "token=") {
		t.Fatalf("list: %d %s", res.StatusCode, list)
	}

	items := a.mails(`{"template":"confirm-email","recipient":"ADA@"}`, admin)
	if len(items) != 1 || items[0].Body != "withheld" || items[0].Status != "pending" {
		t.Fatalf("items = %+v, want the confirmation, withheld", items)
	}

	detail := a.expect(http.StatusOK, http.MethodGet, "/api/v1/admin/mails/"+items[0].ID, "", admin)
	if strings.Contains(string(detail), "token=") || strings.Contains(string(detail), `"text"`) ||
		strings.Contains(string(detail), `"html"`) {
		t.Fatalf("detail leaks the body: %s", detail)
	}

	body := a.expect(http.StatusConflict, http.MethodPost, "/api/v1/admin/mails/"+items[0].ID+"/requeue", "", admin)
	if !strings.Contains(string(body), `"reason":"secret-link"`) {
		t.Errorf("requeue: %s", body)
	}

	// The office does not see the log.
	office := a.login(demoLogin(t, domain.RoleOffice).Email)
	a.expect(http.StatusForbidden, http.MethodPost, "/api/v1/admin/mails/search", "{}", office)
	a.expect(http.StatusForbidden, http.MethodGet, "/api/v1/admin/mails/"+items[0].ID, "", office)
}

// TestAnnouncementFlow: the office writes an emailed announcement for
// everyone; the member sees it, dismisses it and still lists it; the admin
// finds its mails, with their bodies, in the log.
func TestAnnouncementFlow(t *testing.T) {
	t.Parallel()

	a := newAuthApp(t, app.Deps{})
	office := a.login(demoLogin(t, domain.RoleOffice).Email)
	member := a.login(demoLogin(t, domain.RoleMember).Email)

	count := a.expect(http.StatusOK, http.MethodPost, "/api/v1/admin/announcements/recipients",
		`{"audience":"role","role":"admin"}`, office)
	if string(count) != `{"recipients":1}`+"\n" {
		t.Errorf("recipients of the admins: %s", count)
	}

	created := a.expect(http.StatusCreated, http.MethodPost, "/api/v1/admin/announcements",
		`{"audience":"everyone","title":"Studio closed","body":"Closed on Monday.\r\nSee you on Tuesday.","email":true}`,
		office)

	var ann struct {
		ID              string
		Body            string
		EmailedAt       string
		EmailRecipients int
		Author          string
	}
	if err := json.Unmarshal(created, &ann); err != nil {
		t.Fatal(err)
	}

	// Saved; the announcements job sends it.
	if ann.Body != "Closed on Monday.\nSee you on Tuesday." || ann.EmailedAt != "" || ann.Author == "" {
		t.Errorf("created %s", created)
	}

	a.runJobs()

	var written struct {
		Items []struct {
			ID, EmailedAt   string
			EmailRecipients int
		}
	}
	if err := json.Unmarshal(a.expect(http.StatusOK, http.MethodGet, "/api/v1/admin/announcements", "", office), &written); err != nil {
		t.Fatal(err)
	}

	if len(written.Items) == 0 || written.Items[0].ID != ann.ID || written.Items[0].EmailedAt == "" ||
		written.Items[0].EmailRecipients == 0 {
		t.Fatalf("after the job: %+v", written.Items)
	}

	ann.EmailRecipients = written.Items[0].EmailRecipients

	type item struct {
		ID, Title string
		Dismissed bool
	}

	mine := func() []item {
		var list struct{ Items []item }
		if err := json.Unmarshal(a.expect(http.StatusOK, http.MethodGet, "/api/v1/announcements", "", member), &list); err != nil {
			t.Fatal(err)
		}

		return list.Items
	}

	if got := mine(); len(got) == 0 || got[0].ID != ann.ID || got[0].Dismissed {
		t.Fatalf("member sees %+v, want the new one first", got)
	}

	a.expect(http.StatusNoContent, http.MethodPost, "/api/v1/announcements/"+ann.ID+"/dismiss", "", member)

	if got := mine(); got[0].ID != ann.ID || !got[0].Dismissed {
		t.Errorf("after dismissing: %+v", got)
	}

	// The member may not write announcements.
	a.expect(http.StatusForbidden, http.MethodPost, "/api/v1/admin/announcements",
		`{"audience":"everyone","title":"x","body":"y"}`, member)

	// The mails are in the log, with their bodies.
	admin := a.login(demoLogin(t, domain.RoleAdmin).Email)
	items := a.mails(`{"template":"announcement","status":"pending"}`, admin)

	if len(items) != ann.EmailRecipients || items[0].Body != "shown" {
		t.Fatalf("%d announcement mails, want %d: %+v", len(items), ann.EmailRecipients, items)
	}

	var detail mailItem
	if err := json.Unmarshal(a.expect(http.StatusOK, http.MethodGet, "/api/v1/admin/mails/"+items[0].ID, "", admin), &detail); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(detail.Text, "See you on Tuesday.") || detail.HTML == "" {
		t.Errorf("detail %+v", detail)
	}

	// A pending mail is not queued again.
	body := a.expect(http.StatusConflict, http.MethodPost, "/api/v1/admin/mails/"+items[0].ID+"/requeue", "", admin)
	if !strings.Contains(string(body), `"reason":"not-failed"`) {
		t.Errorf("requeue pending: %s", body)
	}

	// The fourth emailed announcement of the day is refused.
	for range 2 {
		a.expect(http.StatusCreated, http.MethodPost, "/api/v1/admin/announcements",
			`{"audience":"role","role":"admin","title":"Note","body":"Hi.","email":true}`, office)
	}

	body = a.expect(http.StatusConflict, http.MethodPost, "/api/v1/admin/announcements",
		`{"audience":"role","role":"admin","title":"Note","body":"Hi.","email":true}`, office)
	if !strings.Contains(string(body), `"reason":"mail-limit"`) {
		t.Errorf("fourth: %s", body)
	}

	// Unknown references are 422s on their fields.
	body = a.expect(http.StatusUnprocessableEntity, http.MethodPost, "/api/v1/admin/announcements",
		`{"audience":"class","classId":"0190a000-0000-7000-8000-000000000999","title":"x","body":"y"}`, office)
	if !strings.Contains(string(body), `"location":"body.classId"`) {
		t.Errorf("unknown class: %s", body)
	}
}

// TestMailLogPages pages through the log with its cursor.
func TestMailLogPages(t *testing.T) {
	t.Parallel()

	a := newAuthApp(t, app.Deps{})
	office := a.login(demoLogin(t, domain.RoleOffice).Email)
	admin := a.login(demoLogin(t, domain.RoleAdmin).Email)

	// Every account gets three mails: more than a page.
	for range 3 {
		a.expect(http.StatusCreated, http.MethodPost, "/api/v1/admin/announcements",
			`{"audience":"everyone","title":"Hello","body":"Hi.","email":true}`, office)
	}

	a.runJobs()

	var page struct {
		Items      []mailItem `json:"items"`
		NextCursor string     `json:"nextCursor"`
	}

	seen := map[string]bool{}
	cursor := ""

	for range 10 {
		page.Items, page.NextCursor = nil, ""
		if err := json.Unmarshal(a.expect(http.StatusOK, http.MethodPost, "/api/v1/admin/mails/search", `{"cursor":"`+cursor+`"}`, admin), &page); err != nil {
			t.Fatal(err)
		}

		for _, m := range page.Items {
			if seen[m.ID] {
				t.Fatalf("mail %s on two pages", m.ID)
			}

			seen[m.ID] = true
		}

		if page.NextCursor == "" {
			break
		}

		cursor = page.NextCursor
	}

	if len(seen) <= 50 || len(seen)%3 != 0 || cursor == "" {
		t.Errorf("%d mails seen, the second page at %q: want three mails each on two pages", len(seen), cursor)
	}

	a.expect(http.StatusUnprocessableEntity, http.MethodPost, "/api/v1/admin/mails/search", `{"cursor":"nonsense"}`, admin)
}

// TestAnnouncementWakesTheJob: an announcement emailed now does not wait
// for the job's next tick; publishing it wakes the job running in the
// background, as in both binaries.
func TestAnnouncementWakesTheJob(t *testing.T) {
	t.Parallel()

	a := newAuthApp(t, app.Deps{Wake: jobs.NewWaker()})
	office := a.login(demoLogin(t, domain.RoleOffice).Email)
	admin := a.login(demoLogin(t, domain.RoleAdmin).Email)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})

	t.Cleanup(func() {
		cancel()
		<-done
	})

	go func() {
		app.Jobs(a.deps).Run(ctx) // ticks a minute apart at the earliest
		close(done)
	}()

	a.expect(http.StatusCreated, http.MethodPost, "/api/v1/admin/announcements",
		`{"audience":"role","role":"admin","title":"Note","body":"Hi.","email":true}`, office)

	timeout := time.After(5 * time.Second)
	for len(a.mails(`{"template":"announcement"}`, admin)) == 0 {
		select {
		case <-timeout:
			t.Fatal("the announcement was not mailed: the job was not woken")
		case <-time.After(10 * time.Millisecond):
		}
	}
}
