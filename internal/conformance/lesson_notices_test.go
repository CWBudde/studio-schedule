package conformance

import (
	"net/http"
	"strings"
	"testing"
)

func lessonNotices(t *testing.T, b *backend) {
	t.Helper()

	office, instructor, otherStaff, member, visitor := browser(t), browser(t), browser(t), browser(t), browser(t)
	login(t, b, office, "olga.ledgerby@example.net", demoPassword)
	login(t, b, instructor, "alina.starling@example.net", demoPassword)
	login(t, b, otherStaff, "felix.keysworth@example.net", demoPassword)
	const memberEmail = "clara.amberly@example.org"
	login(t, b, member, memberEmail, demoPassword)

	event, sessions := noticeFixture(t, b, office)
	body := jsonBody(t, map[string]any{"message": "  Doors open early.\r\nUse the side entrance.\u2028Thank you.  "})
	notice := request{
		method: http.MethodPost, path: "/api/v1/lessons/{lessonId}/notify",
		params: map[string]string{"lessonId": sessions[0]}, body: body,
	}
	b.call(t, visitor, notice, http.StatusUnauthorized)
	b.call(t, member, notice, http.StatusForbidden)
	b.call(t, otherStaff, notice, http.StatusNotFound)
	invalid := notice
	invalid.body = `{"message":"Unsafe \u202e text"}`
	checkLessonField(t, b.call(t, office, invalid, http.StatusUnprocessableEntity), "body.message")
	notify := func(client *http.Client, in request, expected int) {
		t.Helper()
		result := read[struct{ Recipients int }](t, b.call(t, client, in, http.StatusOK))
		if result.Recipients != expected {
			t.Fatalf("notice reached %d recipients, want %d", result.Recipients, expected)
		}
	}
	notify(office, notice, 0)

	publish := event
	publish.path += "/publish"
	b.call(t, office, publish, http.StatusOK)
	desk := event
	desk.path += "/registrations"
	person := read[struct{ Me struct{ ID string } }](t, get(t, b, member, "/api/v1/family")).Me.ID
	desk.body = jsonBody(t, map[string]any{"personId": person})
	b.call(t, office, desk, http.StatusCreated)
	for _, guest := range []struct{ Name, Email string }{
		{"Shared Address", strings.ToUpper(memberEmail)},
		{"Fixture Guest", "notice-guest@example.test"},
	} {
		desk.body = jsonBody(t, map[string]any{"guest": guest})
		b.call(t, office, desk, http.StatusCreated)
	}

	notify(instructor, notice, 2)
	cancel := notice
	cancel.path, cancel.body = "/api/v1/lessons/{lessonId}/cancel", `{"note":"Venue repairs"}`
	b.call(t, office, cancel, http.StatusOK)
	notify(office, notice, 2)
	notify(office, notice, 2)
	checkNoticeLimit(t, b.call(t, instructor, notice, http.StatusTooManyRequests), "21600")
	second := notice
	second.params = map[string]string{"lessonId": sessions[1]}
	for range 3 {
		notify(office, second, 2)
	}
	third := notice
	third.params = map[string]string{"lessonId": sessions[2]}
	checkNoticeLimit(t, b.call(t, office, third, http.StatusTooManyRequests), "3600")
	checkNoticeLimit(t, b.call(t, office, notice, http.StatusTooManyRequests), "21600")

	outbox := read[struct {
		Mails []struct{ To, Text, Template string }
	}](t, b.fixture(t, http.MethodGet, "outbox", "", http.StatusOK))
	var notices, shared, instructorMails, canceledMails int
	for _, mail := range outbox.Mails {
		if mail.Template != "lesson-notice" {
			continue
		}
		notices++
		if !strings.Contains(mail.Text, "Doors open early.\nUse the side entrance.\nThank you.") {
			t.Errorf("notice message was not normalized: %s", mail.Text)
		}
		if mail.To == memberEmail && strings.Contains(mail.Text, "Shared Address") {
			shared++
		}
		if strings.Contains(mail.Text, "Alina Starling") {
			instructorMails++
		}
		if strings.Contains(mail.Text, "The lesson is canceled.") || strings.Contains(mail.Text, "Die Stunde fällt aus.") {
			canceledMails++
		}
	}
	if notices != 12 || shared != 6 || instructorMails != 2 || canceledMails != 4 {
		t.Fatalf("unexpected notice counts: %d total, %d deduplicated, %d from instructor, %d canceled", notices, shared, instructorMails, canceledMails)
	}
}

func checkNoticeLimit(t *testing.T, response answer, retry string) {
	t.Helper()

	if after := response.header.Get("Retry-After"); after != retry {
		t.Errorf("Retry-After = %q, want %q (%s)", after, retry, response.body)
	}
}

func noticeFixture(t *testing.T, b *backend, office *http.Client) (request, []string) {
	t.Helper()

	data := lessonFixture(t, b, office, "workshop", 5)
	staff := read[struct {
		Items []struct{ ID, DisplayName string }
	}](t, get(t, b, office, "/api/v1/staff"))
	instructor := ""
	for _, item := range staff.Items {
		if item.DisplayName == "Alina Starling" {
			instructor = item.ID
			break
		}
	}
	if instructor == "" {
		t.Fatal("fixture instructor is missing")
	}
	const events = "/api/v1/admin/events"
	created := read[struct{ ID string }](t, b.call(t, office, request{
		method: http.MethodPost, path: events, body: jsonBody(t, map[string]any{
			"title": "Lesson notice fixture", "courseTypeId": data["courseTypeId"], "capacity": 5, "price": 0, "vatRate": 0,
		}),
	}, http.StatusCreated))
	event := request{method: http.MethodPost, path: events + "/{eventId}", params: map[string]string{"eventId": created.ID}}
	add := event
	add.path += "/sessions"
	sessions := make([]string, 0, 3)
	for _, date := range []string{"2026-10-24", "2026-10-25", "2026-10-26"} {
		add.body = jsonBody(t, map[string]any{
			"date": date, "startTime": "08:00", "durationMinutes": 60, "roomId": data["roomId"],
			"instructorId": instructor, "confirmRoomOverlap": true,
		})
		sessions = append(sessions, read[struct{ ID string }](t, b.call(t, office, add, http.StatusCreated)).ID)
	}

	return event, sessions
}
