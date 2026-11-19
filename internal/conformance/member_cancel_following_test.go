package conformance

import (
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
)

const linaEmail = "lina.mistlewood@example.com"

type classBooking struct {
	ID, ClassID string
	BookedFor   []struct{ BookingID, PersonID string }
}

func upcomingClassBookings(t *testing.T, b *backend, member *http.Client) []classBooking {
	t.Helper()

	schedule := request{
		method: http.MethodGet, path: "/api/v1/schedule",
		query: url.Values{"from": {"2026-10-27"}, "to": {"2026-11-11"}},
	}
	lessons := read[struct{ Lessons []classBooking }](t, b.call(t, member, schedule, http.StatusOK)).Lessons
	byClass := make(map[string][]classBooking)
	for _, lesson := range lessons {
		if lesson.ClassID != "" && len(lesson.BookedFor) == 1 {
			byClass[lesson.ClassID] = append(byClass[lesson.ClassID], lesson)
			if len(byClass[lesson.ClassID]) == 3 {
				return byClass[lesson.ClassID]
			}
		}
	}

	t.Fatal("fixture needs three upcoming bookings of the same class")
	return nil
}

type followingResult struct {
	Bookings []struct {
		BookingID, LessonID, StartsAt, EndsAt string
		Late                                  bool
	}
	Until, PausedUntil string
}

func memberCancellationFollowing(t *testing.T, b *backend) {
	t.Helper()

	member, admin, visitor := browser(t), browser(t), browser(t)
	login(t, b, member, linaEmail, demoPassword)
	login(t, b, admin, demoAdmin, demoPassword)
	b.call(t, admin, request{
		method: http.MethodPut, path: "/api/v1/admin/settings/booking-rules",
		body: `{"releaseWeekday":5,"releaseTime":"09:00","releaseWeeksAhead":8,` +
			`"bookingCutoffMinutes":15,"cancellationCutoffMinutes":1440,"waitlistInviteExpiryMinutes":360,"defaultVisibleDays":14}`,
	}, http.StatusOK)

	lessons := upcomingClassBookings(t, b, member)
	target := lessons[0].BookedFor[0]
	preview := request{
		method: http.MethodGet, path: "/api/v1/bookings/{bookingId}/cancel-following",
		params: map[string]string{"bookingId": target.BookingID},
	}
	b.call(t, visitor, preview, http.StatusUnauthorized)
	b.call(t, admin, preview, http.StatusNotFound)
	full := read[followingResult](t, b.call(t, member, preview, http.StatusOK))
	if len(full.Bookings) < 3 || full.PausedUntil != full.Until || full.Bookings[0].BookingID != target.BookingID {
		t.Fatalf("unexpected term preview: %+v", full)
	}
	preview.query = url.Values{"until": {"2026-10-01"}}
	checkLessonField(t, b.call(t, member, preview, http.StatusUnprocessableEntity), "query.until")

	// The fixture's weekly classes take place well within one UTC calendar day.
	until := strings.SplitN(full.Bookings[1].StartsAt, "T", 2)[0]
	preview.query.Set("until", until)
	plan := read[followingResult](t, b.call(t, member, preview, http.StatusOK))
	if len(plan.Bookings) != 2 || plan.Until != until || plan.PausedUntil != until ||
		plan.Bookings[1].BookingID != lessons[1].BookedFor[0].BookingID || plan.Bookings[0].Late {
		t.Fatalf("unexpected bounded preview: %+v", plan)
	}
	apply := preview
	apply.method, apply.query, apply.body = http.MethodPost, nil, `{"until":"2026-10-01"}`
	checkLessonField(t, b.call(t, member, apply, http.StatusUnprocessableEntity), "body.until")
	apply.body = jsonBody(t, map[string]string{"until": until})
	got := read[followingResult](t, b.call(t, member, apply, http.StatusOK))
	if !slices.Equal(got.Bookings, plan.Bookings) || got.Until != plan.Until || got.PausedUntil != plan.PausedUntil {
		t.Fatalf("applying the cancellation disagrees with the preview: %+v, want %+v", got, plan)
	}
	expectConflict(t, b, member, apply, "already-canceled")
	checkFollowingPause(t, b, member, admin, lessons, target.PersonID, until)
}

func checkFollowingPause(t *testing.T, b *backend, member, admin *http.Client, lessons []classBooking, person, until string) {
	t.Helper()

	for i, lesson := range lessons {
		doc := read[classBooking](t, b.call(t, member, request{
			method: http.MethodGet, path: "/api/v1/lessons/{lessonId}",
			params: map[string]string{"lessonId": lesson.ID},
		}, http.StatusOK))
		want := 0
		if i == len(lessons)-1 {
			want = 1
		}
		if got := len(doc.BookedFor); got != want {
			t.Errorf("lesson index %d has %d bookings for this family, want %d", i, got, want)
		}
	}
	type membership struct {
		Person      struct{ ID string }
		PausedUntil string
	}
	roster := read[struct{ Items []membership }](t, b.call(t, admin, request{
		method: http.MethodGet, path: "/api/v1/admin/classes/{classId}/members",
		params: map[string]string{"classId": lessons[0].ClassID},
	}, http.StatusOK))
	if !slices.ContainsFunc(roster.Items, func(entry membership) bool {
		return entry.Person.ID == person && entry.PausedUntil == until
	}) {
		t.Fatalf("the office roster lacks the pause through %s: %+v", until, roster)
	}

	outbox := read[struct {
		Mails []struct{ To, Text, Template string }
	}](t, b.fixture(t, http.MethodGet, "outbox", "", http.StatusOK))
	count := 0
	for _, message := range outbox.Mails {
		if message.Template == "booking-canceled" && message.To == linaEmail && strings.Contains(message.Text, "Lina Mistlewood") {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected one combined cancellation email, got %d", count)
	}
}
