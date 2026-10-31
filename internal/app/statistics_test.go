package app_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/app"
)

// TestStatistics reads the office's statistics of the demo: the week of
// its past lessons with their attendance, the pass sales, and the limits
// of a custom range.
func TestStatistics(t *testing.T) {
	t.Parallel()

	a := newAuthApp(t, app.Deps{})
	office := a.login("olga.ledgerby@example.net")

	var st struct {
		Timezone, Currency, Range, Granularity, From, To string
		Term                                             *struct{ Name string }
		Totals                                           struct {
			Lessons, Bookings, Attended, NoShows, ActiveMembers, PassesSold int
			NoShowRate, Utilization                                         float64
		}
		Periods []struct{ Start, End string }
		Classes []struct {
			ClassID, LessonID, CourseTypeName string
			Weekday                           *int
		}
		Instructors     []struct{ Name string }
		TopParticipants []struct {
			PersonID, GivenName, FamilyName       string
			Bookings, Attended, LateCancellations int
		}
	}

	body := a.expect(http.StatusOK, http.MethodGet,
		"/api/v1/admin/statistics?range=custom&from=2026-09-01&to=2026-09-30", "", office)
	if err := json.Unmarshal(body, &st); err != nil {
		t.Fatal(err)
	}

	if st.Timezone != "Europe/Berlin" || st.Range != "custom" || st.Granularity != "week" || st.From != "2026-09-01" ||
		st.To != "2026-09-30" || len(st.Periods) != 5 || st.Periods[0].End != "2026-09-06" || st.Term != nil {
		t.Errorf("statistics %s", body)
	}

	if st.Totals.Lessons == 0 || st.Totals.Attended == 0 || st.Totals.NoShows == 0 || st.Totals.ActiveMembers == 0 ||
		st.Totals.PassesSold == 0 || st.Totals.NoShowRate <= 0 || st.Totals.Utilization <= 0 {
		t.Errorf("totals %+v", st.Totals)
	}

	if len(st.Classes) == 0 || st.Classes[0].ClassID == "" || st.Classes[0].Weekday == nil || len(st.Instructors) == 0 {
		t.Errorf("classes %s", body)
	}

	// The top participants, by bookings, with their names.
	if len(st.TopParticipants) == 0 || len(st.TopParticipants) > 20 {
		t.Fatalf("top participants %s", body)
	}

	for i, p := range st.TopParticipants {
		if p.PersonID == "" || p.GivenName == "" || p.FamilyName == "" || p.Bookings < 1 || p.Attended > p.Bookings ||
			(i > 0 && p.Bookings > st.TopParticipants[i-1].Bookings) {
			t.Errorf("top participant %d: %+v", i, p)
		}
	}

	var raw struct {
		TopParticipants []map[string]any `json:"topParticipants"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatal(err)
	}

	for _, k := range []string{"personId", "givenName", "familyName", "bookings", "attended", "lateCancellations"} {
		if _, ok := raw.TopParticipants[0][k]; !ok {
			t.Errorf("top participant without %q: %v", k, raw.TopParticipants[0])
		}
	}

	// The default range; the current term names its term.
	a.expect(http.StatusOK, http.MethodGet, "/api/v1/admin/statistics", "", office)

	if err := json.Unmarshal(a.expect(http.StatusOK, http.MethodGet, "/api/v1/admin/statistics?range=term", "", office), &st); err != nil {
		t.Fatal(err)
	}

	if st.Range != "term" || st.Term == nil || st.Term.Name == "" || st.From != "2026-09-01" {
		t.Errorf("term statistics %+v", st)
	}

	// At most 366 days, and from before to.
	a.expect(http.StatusUnprocessableEntity, http.MethodGet,
		"/api/v1/admin/statistics?range=custom&from=2026-01-01&to=2027-01-02", "", office)
	a.expect(http.StatusUnprocessableEntity, http.MethodGet,
		"/api/v1/admin/statistics?range=custom&from=2026-02-01&to=2026-01-31", "", office)
	a.expect(http.StatusUnprocessableEntity, http.MethodGet, "/api/v1/admin/statistics?range=custom", "", office)
	a.expect(http.StatusUnprocessableEntity, http.MethodGet, "/api/v1/admin/statistics?range=lastYear", "", office)

	// Members never see them.
	a.expect(http.StatusForbidden, http.MethodGet, "/api/v1/admin/statistics", "", a.login("clara.amberly@example.org"))
}
