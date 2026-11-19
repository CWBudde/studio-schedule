package mcpserver

import (
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/mail"
	"github.com/cwbudde/studio-schedule/internal/service"
)

// daOptions are the options of a session with draft_announcement.
var daOptions = Options{AllowWrites: []WriteGroup{WriteAnnouncements}}

// daArgs are the arguments of a draft for everyone, to be emailed once
// the office publishes it.
var daArgs = map[string]any{
	"title": "Studio closed on Monday", "body": "The studio stays closed on Monday.\nSee you on Tuesday.",
	"audience": "everyone", "email": true,
}

// daDraft returns the one announcement of f's store that is not in before.
func daDraft(t *testing.T, f *fixture, before []domain.Announcement) domain.Announcement {
	t.Helper()

	added := slices.DeleteFunc(wtAnnouncements(t, f), func(a domain.Announcement) bool {
		return slices.ContainsFunc(before, func(b domain.Announcement) bool { return b.ID == a.ID })
	})
	if len(added) != 1 {
		t.Fatalf("%d announcements added, want 1: %+v", len(added), added)
	}

	return added[0]
}

// TestDraftAnnouncement: the office saves a draft for everyone with
// email. The result is the draft as the office's list shows it, without
// its author (no --personal-data); the store has it without a publish
// time, the announcements job mails nothing, a member does not see it,
// and the call is audited.
func TestDraftAnnouncement(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	office := f.principal(t, demoOffice)
	cs := f.session(t, demoOffice, daOptions, registry)
	before := wtAnnouncements(t, f)

	text, isError := callTool(t, cs, "draft_announcement", daArgs)
	if isError {
		t.Fatalf("draft_announcement: error result %q", text)
	}

	golden(t, "draft_announcement.json", text)

	var doc map[string]any
	if err := json.Unmarshal([]byte(text), &doc); err != nil {
		t.Fatal(err)
	}

	_, author := doc["author"]
	if _, published := doc["publishAt"]; doc["draft"] != true || published || author || doc["email"] != true {
		t.Errorf("draft %v", doc)
	}

	a := daDraft(t, f, before)
	if !a.Draft() || a.AuthorID != office.AccountID || a.Body != daArgs["body"] || !a.Email || doc["id"] != a.ID.String() {
		t.Fatalf("stored %+v", a)
	}

	if _, err := f.svc.Announcements.SendDue(t.Context()); err != nil {
		t.Fatal(err)
	}

	if mails := bkMails(t, f, mail.TemplateAnnouncement); len(mails) != 0 {
		t.Errorf("the job mailed the draft: %v", mails)
	}

	shown, err := f.svc.Announcements.ForAccount(t.Context(), f.principal(t, demoMember))
	if err != nil || slices.ContainsFunc(shown, func(v service.AnnouncementView) bool { return v.Announcement.ID == a.ID }) {
		t.Errorf("the member sees %+v, %v; want no draft", shown, err)
	}

	bkWantCalls(t, f, office.AccountID, "draft_announcement", "ok")
}

// TestDraftAnnouncementPersonalData: with --personal-data the result
// names the author, as the office's list does.
func TestDraftAnnouncementPersonalData(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	cs := f.session(t, demoOffice, Options{PersonalData: true, AllowWrites: daOptions.AllowWrites}, registry)

	text, isError := callTool(t, cs, "draft_announcement", daArgs)

	var doc map[string]any
	if err := json.Unmarshal([]byte(text), &doc); isError || err != nil {
		t.Fatalf("draft_announcement: %q (error %t), %v", text, isError, err)
	}

	if name, _ := doc["author"].(string); name == "" {
		t.Errorf("draft without author: %v", doc)
	}
}

// TestDraftAnnouncementRefused: arguments the service or the parser
// refuses are error results with the reason; nothing is saved, and
// each call is audited as invalid. A class that does not exist is
// invalid too, and the longest title and body are taken.
func TestDraftAnnouncementRefused(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	office := f.principal(t, demoOffice)
	cs := f.session(t, demoOffice, daOptions, registry)
	before := wtAnnouncements(t, f)
	with := func(k string, v any) map[string]any {
		args := map[string]any{"title": "Note", "body": "Hi.", "audience": "everyone"}
		args[k] = v

		return args
	}
	unknown := "00000000-0000-7000-8000-00000000ffff"

	const create = "create announcement: validation failed: "

	for _, tc := range []struct {
		args map[string]any
		want string
	}{
		{with("title", strings.Repeat("ä", domain.MaxAnnouncementTitleLen+1)), create + "title must not exceed 120 characters"},
		{with("body", strings.Repeat("ä", domain.MaxAnnouncementBodyLen+1)), create + "body must not exceed 4000 characters"},
		{with("audience", "world"), create + "invalid audience"},
		{with("classId", "class one"), "validation failed: classId is not an ID"},
		{with("courseTypeId", "x"), "validation failed: courseTypeId is not an ID"},
		{with("lessonId", "x"), "validation failed: lessonId is not an ID"},
		{with("eventId", "x"), "validation failed: eventId is not an ID"},
		{with("expiresAt", "next week"), "validation failed: expiresAt is not an RFC 3339 instant"},
		{with("expiresAt", "2020-01-01T00:00:00Z"), create + "the expiry must lie in the future"},
		{map[string]any{"title": "Note", "body": "Hi.", "audience": "class", "classId": unknown}, create + "class does not exist"},
	} {
		wantError(t, cs, "draft_announcement", tc.args, tc.want)
	}

	if after := wtAnnouncements(t, f); len(after) != len(before) {
		t.Fatalf("refused calls saved %d announcements", len(after)-len(before))
	}

	args := map[string]any{
		"title":    strings.Repeat("ä", domain.MaxAnnouncementTitleLen),
		"body":     strings.Repeat("ä", domain.MaxAnnouncementBodyLen),
		"audience": "role", "role": "instructor",
	}
	if text, isError := callTool(t, cs, "draft_announcement", args); isError {
		t.Fatalf("the longest draft: %q", text)
	}

	if a := daDraft(t, f, before); !a.Draft() || a.Role != domain.RoleInstructor {
		t.Errorf("stored %+v", a)
	}

	outcomes := make([]string, 0, 11)
	for range 10 {
		outcomes = append(outcomes, "invalid")
	}

	bkWantCalls(t, f, office.AccountID, "draft_announcement", append(outcomes, "ok")...)
}

// TestDraftAnnouncementDescription: the description states the limits of
// domain's title and body, which the input schema repeats.
func TestDraftAnnouncementDescription(t *testing.T) {
	t.Parallel()

	tl := announcementWriteTools()[0]
	for _, n := range []int{domain.MaxAnnouncementTitleLen, domain.MaxAnnouncementBodyLen} {
		if !strings.Contains(tl.description, strconv.Itoa(n)+" ") {
			t.Errorf("the description does not state %d: %s", n, tl.description)
		}
	}
}
