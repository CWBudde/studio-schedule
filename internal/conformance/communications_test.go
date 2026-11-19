package conformance

import (
	"bytes"
	"net/http"
	"net/url"
	"slices"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/seed"
)

func announcements(t *testing.T, b *backend) {
	t.Helper()

	office := browser(t)
	member := browser(t)
	login(t, b, office, "olga.ledgerby@example.net", demoPassword)
	login(t, b, member, demoMember, demoPassword)
	const path = "/api/v1/admin/announcements"
	const public = "/api/v1/announcements"
	b.call(t, browser(t), request{method: http.MethodGet, path: public}, http.StatusUnauthorized)

	newNote := request{
		method: http.MethodPost, path: path,
		body: `{"audience":"everyone","title":"Studio closed","body":"Closed on Monday.\r\nSee you on Tuesday.","expiresAt":"2026-10-23T18:00:00Z"}`,
	}
	b.call(t, member, newNote, http.StatusForbidden)
	type note struct{ ID, Body, PublishAt, CreatedAt, UpdatedAt string }
	n := read[note](t, b.call(t, office, newNote, http.StatusCreated))
	if n.ID == "" || n.Body != "Closed on Monday.\nSee you on Tuesday." ||
		n.PublishAt != demoNow || n.CreatedAt != demoNow {
		t.Fatalf("unexpected published announcement: %+v", n)
	}

	type item struct {
		ID        string
		Dismissed bool
	}
	items := func(client *http.Client) []item {
		t.Helper()
		return read[struct{ Items []item }](t, get(t, b, client, public)).Items
	}
	first := items(member)
	if i := slices.IndexFunc(first, func(v item) bool { return v.ID == n.ID }); i < 0 || first[i].Dismissed {
		t.Fatal("published announcement is missing or dismissed for a member")
	}
	dismiss := request{
		method: http.MethodPost, path: public + "/{announcementId}/dismiss",
		params: map[string]string{"announcementId": n.ID},
	}
	b.call(t, member, dismiss, http.StatusNoContent)
	b.call(t, member, dismiss, http.StatusNoContent)
	after := items(member)
	if i := slices.IndexFunc(after, func(v item) bool { return v.ID == n.ID }); i < 0 || !after[i].Dismissed {
		t.Fatal("member dismissal was not stored")
	}
	if slices.ContainsFunc(items(office), func(v item) bool { return v.ID == n.ID && v.Dismissed }) {
		t.Error("dismissing changed another account's announcement")
	}

	draft := request{
		method: http.MethodPost, path: path,
		body: `{"audience":"role","role":"member","title":"Draft","body":"Draft body","Draft":true,"email":true}`,
	}
	typo := draft
	typo.body = `{"audience":"everyone","title":"Typo","body":"Must stay private","drfat":true,"email":true}`
	b.call(t, office, typo, http.StatusUnprocessableEntity)

	d := read[struct {
		ID, PublishAt string
		Draft         bool
	}](t, b.call(t, office, draft, http.StatusCreated))
	if !d.Draft || d.PublishAt != "" || slices.ContainsFunc(items(member), func(v item) bool { return v.ID == d.ID }) {
		t.Fatal("draft became visible or acquired a publication time")
	}
	ref := map[string]string{"announcementId": d.ID}
	b.call(t, member, request{method: http.MethodPost, path: public + "/{announcementId}/dismiss", params: ref}, http.StatusNotFound)
	preview := request{method: http.MethodPost, path: path + "/recipients", body: `{"audience":"role","role":"member"}`}
	if got := read[struct{ Recipients int }](t, b.call(t, office, preview, http.StatusOK)).Recipients; got < 1 {
		t.Error("member audience had no email recipients")
	}
	edit := request{
		method: http.MethodPut, path: path + "/{announcementId}", params: ref,
		body: `{"audience":"role","role":"member","title":"Member notice","body":"For members."}`,
	}
	published := read[note](t, b.call(t, office, edit, http.StatusOK))
	if published.PublishAt != demoNow || !slices.ContainsFunc(items(member), func(v item) bool { return v.ID == d.ID }) ||
		slices.ContainsFunc(items(office), func(v item) bool { return v.ID == d.ID }) {
		t.Fatal("publishing the draft did not target members")
	}
	edit.body = draft.body
	b.call(t, office, edit, http.StatusUnprocessableEntity)

	timetable := readSeededTimetable(t, b, browser(t))
	ballet := timetable.classes[slices.IndexFunc(timetable.classes, func(c listedClass) bool { return c.ID == timetable.balletID })]
	guardian := browser(t)
	login(t, b, guardian, "clara.amberly@example.org", demoPassword)
	classBody := `{"audience":"class","classId":"` + ballet.ID + `","title":"Class notice","body":"Class updates."}`
	classNotice := read[struct {
		ID    string
		Class struct {
			ID, TermID, StartTime string
		}
	}](t, b.call(t, office, request{method: http.MethodPost, path: path, body: classBody}, http.StatusCreated))
	if classNotice.Class.ID != ballet.ID || classNotice.Class.TermID != timetable.termID || classNotice.Class.StartTime != ballet.StartTime ||
		!slices.ContainsFunc(items(guardian), func(v item) bool { return v.ID == classNotice.ID }) ||
		slices.ContainsFunc(items(member), func(v item) bool { return v.ID == classNotice.ID }) {
		t.Fatal("class announcement did not reach the enrolled child's guardian")
	}
	b.call(t, member, request{
		method: http.MethodPost, path: public + "/{announcementId}/dismiss",
		params: map[string]string{"announcementId": classNotice.ID},
	}, http.StatusNotFound)
	course := read[struct {
		CourseType struct{ ID string }
	}](t, b.call(t, office, request{
		method: http.MethodPost, path: path,
		body: `{"audience":"course-type","courseTypeId":"` + ballet.CourseTypeID + `","title":"Course notice","body":"Course updates."}`,
	}, http.StatusCreated))
	if course.CourseType.ID != ballet.CourseTypeID {
		t.Fatalf("course type description = %+v", course.CourseType)
	}
	event := read[struct {
		Title    string
		Sessions []scheduledLesson
	}](t, b.call(t, browser(t), request{
		method: http.MethodGet, path: "/api/v1/events/{slug}",
		params: map[string]string{"slug": "open-ballet-taster"},
	}, http.StatusOK))
	anchor, err := domain.ParseDate(demoDate)
	if err != nil {
		t.Fatal(err)
	}
	fixture := seed.DemoAt(anchor)
	eventIndex := slices.IndexFunc(fixture.Events, func(e domain.Event) bool { return e.Slug == "open-ballet-taster" })
	if eventIndex < 0 {
		t.Fatal("fixture event ID missing")
	}
	eventID := fixture.Events[eventIndex].ID.String()

	eventGuardian := browser(t)
	login(t, b, eventGuardian, "yara.gildenmoor@example.org", demoPassword)
	if len(event.Sessions) != 1 {
		t.Fatal("fixture event needs one session")
	}
	for _, audience := range []string{
		`"audience":"event","eventId":"` + eventID + `"`,
		`"audience":"lesson","lessonId":"` + event.Sessions[0].ID + `"`,
	} {
		recipientCount := read[struct{ Recipients int }](t, b.call(t, office, request{
			method: http.MethodPost, path: path + "/recipients", body: "{" + audience + "}",
		}, http.StatusOK))
		if recipientCount.Recipients != 3 {
			t.Fatalf("expected the two registered contacts and the guest, got %d", recipientCount.Recipients)
		}
		eventNotice := read[struct {
			ID, Audience, EventTitle string
			Lesson                   *struct{ ID string }
		}](t, b.call(t, office, request{method: http.MethodPost, path: path, body: "{" + audience +
			`,"title":"Event notice","body":"Event updates."}`}, http.StatusCreated))
		if (eventNotice.Audience == "event" && eventNotice.EventTitle != event.Title) ||
			(eventNotice.Audience == "lesson" && (eventNotice.Lesson == nil || eventNotice.Lesson.ID != event.Sessions[0].ID)) ||
			!slices.ContainsFunc(items(eventGuardian), func(v item) bool { return v.ID == eventNotice.ID }) ||
			slices.ContainsFunc(items(member), func(v item) bool { return v.ID == eventNotice.ID }) {
			t.Fatal("event or session announcement did not reach the registered child's guardian")
		}
	}

	memberIndex := slices.IndexFunc(fixture.Accounts, func(a domain.Account) bool { return a.Email == demoMember })
	if memberIndex < 0 {
		t.Fatal("fixture member is missing")
	}
	person := fixture.Accounts[memberIndex].PersonID
	attendance := slices.IndexFunc(fixture.Bookings, func(b domain.Booking) bool {
		return b.PersonID == person && (b.Status == domain.BookingAttended || b.Status == domain.BookingNoShow)
	})
	if attendance < 0 {
		t.Fatal("fixture needs a recorded attendance result")
	}
	audience := `"audience":"lesson","lessonId":"` + fixture.Bookings[attendance].LessonID.String() + `"`
	attendedNote := read[note](t, b.call(t, office, request{
		method: http.MethodPost, path: path,
		body: "{" + audience + `,"title":"Lesson follow-up","body":"Follow-up after attendance."}`,
	}, http.StatusCreated))
	if !slices.ContainsFunc(items(member), func(v item) bool { return v.ID == attendedNote.ID }) {
		t.Fatal("recorded attendance hid the lesson announcement")
	}
	if got := read[struct{ Recipients int }](t, b.call(t, office, request{
		method: http.MethodPost, path: path + "/recipients", body: "{" + audience + "}",
	}, http.StatusOK)).Recipients; got < 1 {
		t.Fatal("recorded attendance removed every lesson recipient")
	}

	b.fixture(t, http.MethodPut, "clock", `{"now":"2026-10-23T18:01:00Z"}`, http.StatusNoContent)
	if slices.ContainsFunc(items(member), func(v item) bool { return v.ID == n.ID }) {
		t.Error("expired announcement is still visible")
	}
	remove := request{
		method: http.MethodDelete, path: path + "/{announcementId}",
		params: map[string]string{"announcementId": n.ID},
	}
	b.call(t, office, remove, http.StatusNoContent)
	b.call(t, office, remove, http.StatusNotFound)
}

func mailLog(t *testing.T, b *backend) {
	t.Helper()

	admin := browser(t)
	office := browser(t)
	login(t, b, admin, demoAdmin, demoPassword)
	login(t, b, office, "olga.ledgerby@example.net", demoPassword)
	const recipient = "fixture-mail@example.test"
	b.call(t, browser(t), request{
		method: http.MethodPost, path: "/api/v1/auth/register",
		body: `{"givenName":"Test","familyName":"Mailbox","email":"` + recipient + `","password":"a fixture mail password","locale":"de"}`,
	},
		http.StatusAccepted)
	token := mailToken(t, b, recipient, "confirm-email", "/confirm")

	search := request{
		method: http.MethodPost, path: "/api/v1/admin/mails/search",
		body: `{"template":"confirm-email","recipient":"Fixture-Mail@"}`,
	}
	b.call(t, office, search, http.StatusForbidden)
	type mailEntry struct {
		ID, To, Status, Body string
		Attempts             int
	}
	result := b.call(t, admin, search, http.StatusOK)
	page := read[struct{ Items []mailEntry }](t, result)
	if len(page.Items) != 1 || page.Items[0].To != recipient || page.Items[0].Status != "pending" ||
		page.Items[0].Attempts != 0 || page.Items[0].Body != "withheld" ||
		bytes.Contains(result.body, []byte(token)) {
		t.Fatal("mail search did not report the pending mail with its body withheld")
	}
	detail := request{
		method: http.MethodGet, path: "/api/v1/admin/mails/{mailId}",
		params: map[string]string{"mailId": page.Items[0].ID},
	}
	b.call(t, office, detail, http.StatusForbidden)
	full := b.call(t, admin, detail, http.StatusOK)
	if read[mailEntry](t, full).Body != "withheld" || bytes.Contains(full.body, []byte(token)) {
		t.Error("secret link was disclosed in the mail log")
	}
	detail.method, detail.path = http.MethodPost, detail.path+"/requeue"
	if retry := b.call(t, admin, detail, http.StatusConflict); !containsJSON(retry.body, `"reason":"secret-link"`) {
		t.Error("secret-link mail was not refused by the requeue rule")
	}
}

func statistics(t *testing.T, b *backend) {
	t.Helper()

	office := browser(t)
	member := browser(t)
	login(t, b, office, "olga.ledgerby@example.net", demoPassword)
	login(t, b, member, demoMember, demoPassword)
	in := request{
		method: http.MethodGet, path: "/api/v1/admin/statistics",
		query: url.Values{"range": {"custom"}, "from": {"2026-09-01"}, "to": {demoDate}},
	}
	b.call(t, member, in, http.StatusForbidden)
	res := read[struct {
		Timezone, Currency, Range, From, To string
		Periods                             []struct{}
		Totals                              struct{ Lessons, Attended, ActiveMembers int }
	}](t, b.call(t, office, in, http.StatusOK))
	if res.Timezone != "Europe/Berlin" || res.Currency != "EUR" || res.Range != "custom" ||
		res.From != "2026-09-01" || res.To != demoDate || len(res.Periods) == 0 ||
		res.Totals.Lessons == 0 || res.Totals.Attended == 0 || res.Totals.ActiveMembers == 0 {
		t.Fatalf("unexpected custom statistics: %+v", res)
	}
	in.query.Set("from", "2026-10-24")
	b.call(t, office, in, http.StatusUnprocessableEntity)
}
