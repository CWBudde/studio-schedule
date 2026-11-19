package conformance

import (
	"net/http"
	"net/url"
	"slices"
	"testing"
)

type familyLessonBooking struct {
	Booking    booking
	PersonName string
	Lesson     scheduledLesson
	CanCancel  bool
	Payment    struct{ Status string }
}

func myLessonsAndHistory(t *testing.T, b *backend) {
	t.Helper()

	visitor, irene, vera := browser(t), browser(t), browser(t)
	current := request{method: http.MethodGet, path: "/api/v1/my/lessons"}
	past := request{method: http.MethodGet, path: "/api/v1/my/lessons/history"}
	b.call(t, visitor, current, http.StatusUnauthorized)
	b.call(t, visitor, past, http.StatusUnauthorized)
	login(t, b, irene, "irene.inkwell@example.com", demoPassword)
	login(t, b, vera, "vera.rushwick@example.com", demoPassword)
	workshop := lessonOn(t, b, "2026-11-02", "2026-11-03", "Tap")

	type familyLessons struct {
		Now, Timezone string
		Upcoming      []familyLessonBooking
		Waitlist      []struct {
			Entry struct {
				Name, Status string
				Position     int
			}
			Lesson scheduledLesson
		}
	}
	upcoming := read[familyLessons](t, b.call(t, irene, current, http.StatusOK))
	if upcoming.Now != demoNow || upcoming.Timezone != "Europe/Berlin" ||
		!slices.ContainsFunc(upcoming.Upcoming, func(row familyLessonBooking) bool {
			return row.Lesson.ID == workshop && row.Booking.LessonID == workshop &&
				row.Booking.Status == "booked" && row.PersonName == "Irene Inkwell" &&
				row.CanCancel && row.Payment.Status == "pending"
		}) {
		t.Fatalf("upcoming list omitted Irene's workshop booking: %+v", upcoming)
	}

	waiting := read[familyLessons](t, b.call(t, vera, current, http.StatusOK))
	if len(waiting.Waitlist) != 1 || waiting.Waitlist[0].Lesson.ID != workshop ||
		waiting.Waitlist[0].Entry.Name != "Vera Rushwick" || waiting.Waitlist[0].Entry.Status != "waiting" ||
		waiting.Waitlist[0].Entry.Position != 1 {
		t.Fatalf("unexpected family waiting list: %+v", waiting.Waitlist)
	}

	type history struct {
		Now, Before, NextBefore, Timezone string
		Items                             []familyLessonBooking
		HasMore                           bool
	}
	stored := read[history](t, b.call(t, irene, past, http.StatusOK))
	if stored.Now != demoNow || stored.Before != demoNow || stored.Timezone != "Europe/Berlin" ||
		stored.NextBefore != "2026-07-23T16:00:00Z" || stored.HasMore || len(stored.Items) == 0 {
		t.Fatalf("unexpected first history page: %+v", stored)
	}
	for _, item := range stored.Items {
		if item.CanCancel || item.PersonName != "Irene Inkwell" || item.Booking.LessonID != item.Lesson.ID {
			t.Fatalf("history exposes an unexpected booking: %+v", item)
		}
	}
	if !slices.IsSortedFunc(stored.Items, func(a, b familyLessonBooking) int {
		return b.Lesson.StartsAt.Compare(a.Lesson.StartsAt)
	}) {
		t.Fatal("history is not newest first")
	}

	past.query = url.Values{"before": {stored.NextBefore}}
	older := read[history](t, b.call(t, irene, past, http.StatusOK))
	if older.Before != stored.NextBefore || len(older.Items) != 0 || older.HasMore {
		t.Fatalf("unexpected older history page: %+v", older)
	}

	past.query.Set("before", "2026-08-01T11:00:00.123456789+02:00")
	fractional := read[history](t, b.call(t, irene, past, http.StatusOK))
	if fractional.Before != "2026-08-01T09:00:00.123456789Z" || fractional.NextBefore != "2026-05-01T09:00:00.123456789Z" {
		t.Fatalf("history lost precision in the cursor: %+v", fractional)
	}

	past.query.Set("before", "not-a-timestamp")
	checkLessonField(t, b.call(t, irene, past, http.StatusUnprocessableEntity), "query.before")
}
