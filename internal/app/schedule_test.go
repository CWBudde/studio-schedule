package app_test

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/app"
	"github.com/cwbudde/studio-schedule/internal/auth/password"
	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/seed"
	"github.com/cwbudde/studio-schedule/internal/store"
	"github.com/cwbudde/studio-schedule/internal/store/memory"
)

// scheduleApp is the demo school with hand-made lessons, served with the
// clock on Wednesday 2026-10-21 12:00 in Berlin (10:00 UTC). The demo
// release rule (Sunday 18:00, one week ahead) has released everything before
// Monday 2026-10-26 00:00.
type scheduleApp struct {
	h       http.Handler
	data    seed.Dataset
	lessons map[string]domain.Lesson
}

func newScheduleApp(t *testing.T) scheduleApp {
	t.Helper()

	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}

	st := memory.New()
	if err := seed.LoadDemo(t.Context(), st); err != nil {
		t.Fatal(err)
	}

	a := scheduleApp{data: seed.Demo(), lessons: map[string]domain.Lesson{}}

	classAt := func(day time.Weekday, hour, minute int) domain.Class {
		for _, c := range a.data.Classes {
			if c.Weekday == day && c.StartTime == (domain.TimeOfDay{Hour: hour, Minute: minute}) {
				return c
			}
		}

		t.Fatalf("no demo class on %v at %02d:%02d", day, hour, minute)

		return domain.Class{}
	}
	fromClass := func(c domain.Class, d domain.Date) domain.Lesson {
		start := d.At(c.StartTime, berlin)

		return domain.Lesson{
			ClassID: c.ID, SlotDate: d, CourseTypeID: c.CourseTypeID, RoomID: c.RoomID,
			InstructorID: c.InstructorID, AccompanistID: c.AccompanistID,
			StartsAt: start, EndsAt: start.Add(c.Duration),
			Status: domain.LessonScheduled, Visibility: domain.VisibilityPublic, Kind: domain.LessonRegular,
			Capacity: c.Capacity,
		}
	}
	add := func(name string, l domain.Lesson) {
		l.ID = scheduleLessonID(t, len(a.lessons)+1)
		a.lessons[name] = l
	}

	monBallet := classAt(time.Monday, 16, 30) // Classical Ballet, beginner, Studio A, with accompanist
	monHipHop := classAt(time.Monday, 18, 0)  // Hip-Hop, beginner, Hall 1 (Studio South)
	wedPointe := classAt(time.Wednesday, 18, 0)

	add("ballet", fromClass(monBallet, domain.Date{Year: 2026, Month: time.October, Day: 19}))
	add("hipHop", fromClass(monHipHop, domain.Date{Year: 2026, Month: time.October, Day: 19}))
	add("unreleased", fromClass(monBallet, domain.Date{Year: 2026, Month: time.October, Day: 26}))

	canceled := fromClass(wedPointe, domain.Date{Year: 2026, Month: time.October, Day: 21})
	canceled.Status = domain.LessonCanceled
	add("canceled", canceled)

	hidden := fromClass(classAt(time.Tuesday, 16, 0), domain.Date{Year: 2026, Month: time.October, Day: 20})
	hidden.Visibility = domain.VisibilityHidden
	add("hidden", hidden)

	members := fromClass(classAt(time.Tuesday, 17, 0), domain.Date{Year: 2026, Month: time.October, Day: 20})
	members.Visibility = domain.VisibilityMembers
	add("members", members)

	workshop := fromClass(monHipHop, domain.Date{Year: 2026, Month: time.November, Day: 7})
	workshop.ClassID, workshop.SlotDate = domain.ClassID{}, domain.Date{}
	workshop.Kind = domain.LessonWorkshop
	add("workshop", workshop)

	err = st.InTx(t.Context(), store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		for _, l := range a.lessons {
			if err := l.Validate(); err != nil {
				return err
			}

			if err := tx.Lessons().Create(ctx, l); err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	hasher, err := password.NewWithParams(password.Params{Memory: 8, Time: 1, Threads: 1, SaltLen: 16, KeyLen: 32}, nil)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, time.October, 21, 12, 0, 0, 0, berlin)
	a.h, _ = app.New(app.Deps{Store: st, Clock: clock.NewFixed(now), Hasher: hasher})

	return a
}

func scheduleLessonID(t *testing.T, n int) domain.LessonID {
	t.Helper()

	id, err := domain.ParseID[domain.LessonID](fmt.Sprintf("00000000-0000-7000-8000-00000000b%03d", n))
	if err != nil {
		t.Fatal(err)
	}

	return id
}

func (a scheduleApp) get(t *testing.T, target string, cookies ...*http.Cookie) (response, []byte) {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, target, nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}

	return do(t, a.h, req)
}

// login signs in as the demo account with role r and returns its session
// cookie.
func (a scheduleApp) login(t *testing.T, r domain.Role) *http.Cookie {
	t.Helper()

	for _, l := range seed.DemoLogins() {
		if l.Role != r {
			continue
		}

		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login",
			strings.NewReader(`{"email":"`+l.Email+`","password":"`+l.Password+`"}`))
		req.Header.Set("Content-Type", "application/json")

		rec := httptest.NewRecorder()
		a.h.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("login as %s: %d %s", r, rec.Code, rec.Body)
		}

		return sessionCookie(t, authResponse{StatusCode: rec.Code, Header: rec.Header()})
	}

	t.Fatalf("no demo login with role %s", r)

	return nil
}

// TestScheduleVisibility checks what each role sees through the session
// cookie (ADR 0003): members also see lessons for members and, with early
// access (demo default 14 days), next week's lessons; staff see everything.
func TestScheduleVisibility(t *testing.T) {
	t.Parallel()

	a := newScheduleApp(t)

	guest := []string{"ballet", "hipHop", "canceled", "workshop"}
	member := []string{"ballet", "hipHop", "members", "canceled", "unreleased", "workshop"}
	staff := []string{"ballet", "hipHop", "hidden", "members", "canceled", "unreleased", "workshop"}

	tests := []struct {
		role         domain.Role
		want         []string
		visibleUntil any
	}{
		{domain.RoleGuest, guest, "2026-10-25T23:00:00Z"},
		{domain.RoleMember, member, "2026-11-04T23:00:00Z"}, // Thursday 00:00 CET
		{domain.RoleInstructor, staff, nil},
		{domain.RoleAdmin, staff, nil},
	}

	for _, tt := range tests {
		t.Run(string(tt.role), func(t *testing.T) {
			t.Parallel()

			var cookies []*http.Cookie
			if tt.role != domain.RoleGuest {
				cookies = append(cookies, a.login(t, tt.role))
			}

			res, body := a.get(t, "/api/v1/schedule?from=2026-10-19&to=2026-11-09", cookies...)
			if res.StatusCode != http.StatusOK {
				t.Fatalf("status = %d, body %s", res.StatusCode, body)
			}

			if names := a.lessonNames(t, body); !slices.Equal(names, tt.want) {
				t.Errorf("lessons = %v, want %v", names, tt.want)
			}

			if got := decode(t, body)["visibleUntil"]; got != tt.visibleUntil {
				t.Errorf("visibleUntil = %v, want %v", got, tt.visibleUntil)
			}

			for name := range a.lessons {
				res, body := a.get(t, "/api/v1/lessons/"+a.id(name), cookies...)
				want := http.StatusNotFound
				if slices.Contains(tt.want, name) {
					want = http.StatusOK
				}

				if res.StatusCode != want {
					t.Errorf("lesson %s: status %d, want %d (%s)", name, res.StatusCode, want, body)
				}
			}
		})
	}
}

// lessonNames maps the lessons of a schedule body back to fixture names.
func (a scheduleApp) lessonNames(t *testing.T, body []byte) []string {
	t.Helper()

	var s struct {
		Lessons []struct {
			ID string `json:"id"`
		} `json:"lessons"`
	}
	if err := json.Unmarshal(body, &s); err != nil {
		t.Fatal(err)
	}

	if s.Lessons == nil {
		t.Fatalf("lessons must be an array: %s", body)
	}

	names := make([]string, 0, len(s.Lessons))

	for _, l := range s.Lessons {
		name := l.ID

		for n, fl := range a.lessons {
			if fl.ID.String() == l.ID {
				name = n
			}
		}

		names = append(names, name)
	}

	return names
}

func (a scheduleApp) id(name string) string { return a.lessons[name].ID.String() }

func (a scheduleApp) entityID(t *testing.T, kind, name string) string {
	t.Helper()

	switch kind {
	case "location":
		for _, l := range a.data.Locations {
			if l.Name == name {
				return l.ID.String()
			}
		}
	case "staff":
		for _, s := range a.data.Staff {
			if s.DisplayName == name {
				return s.ID.String()
			}
		}
	case "courseType":
		for _, c := range a.data.CourseTypes {
			if c.Name == name {
				return c.ID.String()
			}
		}
	}

	t.Fatalf("no demo %s %q", kind, name)

	return ""
}

func sortedJSONKeys(m map[string]any) []string { return slices.Sorted(maps.Keys(m)) }

func TestGetSchedule(t *testing.T) {
	t.Parallel()

	a := newScheduleApp(t)

	res, body := a.get(t, "/api/v1/schedule?from=2026-10-19&to=2026-11-09")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body %s", res.StatusCode, body)
	}

	got := decode(t, body)

	wantKeys := []string{"from", "lessons", "nextReleaseAt", "now", "releasedUntil", "timezone", "to", "visibleUntil"}
	if !slices.Equal(sortedJSONKeys(got), wantKeys) {
		t.Fatalf("keys = %v, want %v", sortedJSONKeys(got), wantKeys)
	}

	header := map[string]any{
		"timezone":      "Europe/Berlin",
		"now":           "2026-10-21T10:00:00Z",
		"from":          "2026-10-19",
		"to":            "2026-11-09",
		"releasedUntil": "2026-10-25T23:00:00Z", // Monday 00:00 CET
		"nextReleaseAt": "2026-10-25T17:00:00Z", // Sunday 18:00 CET
		"visibleUntil":  "2026-10-25T23:00:00Z", // a guest sees what is released
	}
	for k, v := range header {
		if got[k] != v {
			t.Errorf("%s = %v, want %v", k, got[k], v)
		}
	}

	if names, want := a.lessonNames(t, body), []string{"ballet", "hipHop", "canceled", "workshop"}; !slices.Equal(names, want) {
		t.Fatalf("lessons = %v, want %v", names, want)
	}

	lessons, _ := got["lessons"].([]any)

	ballet, _ := lessons[0].(map[string]any)
	wantBallet := []string{
		"accompanist", "capacity", "classId", "courseType", "endsAt", "id", "instructor", "kind",
		"level", "location", "room", "startsAt", "status", "visibility",
	}
	if !slices.Equal(sortedJSONKeys(ballet), wantBallet) {
		t.Fatalf("lesson keys = %v, want %v", sortedJSONKeys(ballet), wantBallet)
	}

	l := a.lessons["ballet"]
	want := map[string]any{
		"id":         l.ID.String(),
		"classId":    l.ClassID.String(),
		"startsAt":   "2026-10-19T14:30:00Z",
		"endsAt":     "2026-10-19T15:30:00Z",
		"status":     "scheduled",
		"kind":       "regular",
		"visibility": "public",
		"level":      "beginner",
		"capacity":   float64(l.Capacity),
	}
	for k, v := range want {
		if ballet[k] != v {
			t.Errorf("lesson %s = %v, want %v", k, ballet[k], v)
		}
	}

	refs := map[string]map[string]any{
		"courseType":  {"id": a.entityID(t, "courseType", "Classical Ballet"), "name": "Classical Ballet"},
		"room":        {"id": l.RoomID.String(), "name": "Studio A"},
		"location":    {"id": a.entityID(t, "location", "Studio North"), "name": "Studio North"},
		"instructor":  {"id": l.InstructorID.String(), "displayName": "Alina Starling"},
		"accompanist": {"id": l.AccompanistID.String(), "displayName": "Felix Keysworth"},
	}
	for field, want := range refs {
		ref, _ := ballet[field].(map[string]any)
		if len(ref) != len(want) {
			t.Errorf("%s = %v, want %v", field, ref, want)

			continue
		}

		for k, v := range want {
			if ref[k] != v {
				t.Errorf("%s.%s = %v, want %v", field, k, ref[k], v)
			}
		}
	}

	if canceled, _ := lessons[2].(map[string]any); canceled["status"] != "canceled" {
		t.Errorf("canceled lesson status = %v", canceled["status"])
	}

	workshop, _ := lessons[3].(map[string]any)
	for _, absent := range []string{"classId", "level", "accompanist"} {
		if _, ok := workshop[absent]; ok {
			t.Errorf("one-off workshop has %s: %v", absent, workshop[absent])
		}
	}

	if workshop["kind"] != "workshop" {
		t.Errorf("workshop kind = %v", workshop["kind"])
	}

	// A guest's lessons are all public, so their visibility reveals nothing.
	for _, leak := range []string{`"members"`, `"hidden"`, a.id("hidden"), a.id("members"), a.id("unreleased")} {
		if strings.Contains(string(body), leak) {
			t.Errorf("response leaks %q", leak)
		}
	}
}

func TestGetScheduleFilters(t *testing.T) {
	t.Parallel()

	a := newScheduleApp(t)
	base := "/api/v1/schedule?from=2026-10-19&to=2026-11-09"

	tests := []struct {
		query string
		want  []string
	}{
		{"&location=" + a.entityID(t, "location", "Studio South"), []string{"hipHop", "workshop"}},
		{"&instructor=" + a.entityID(t, "staff", "Felix Keysworth"), []string{"ballet"}},
		{"&courseType=" + a.entityID(t, "courseType", "Pointe Work"), []string{"canceled"}},
		{"&level=beginner", []string{"ballet", "hipHop"}},
		{"&level=professional", []string{}},
		{"&location=" + a.entityID(t, "location", "Studio North") + "&level=advanced", []string{"canceled"}},
	}

	for _, tt := range tests {
		res, body := a.get(t, base+tt.query)
		if res.StatusCode != http.StatusOK {
			t.Fatalf("%s: status = %d, body %s", tt.query, res.StatusCode, body)
		}

		if got := a.lessonNames(t, body); !slices.Equal(got, tt.want) {
			t.Errorf("%s: lessons = %v, want %v", tt.query, got, tt.want)
		}
	}
}

func TestGetScheduleEmptyDemo(t *testing.T) {
	t.Parallel()

	h, _ := newApp(t, nil) // demo data without lessons

	res, body := do(t, h, httptest.NewRequest(http.MethodGet, "/api/v1/schedule?from=2026-10-19&to=2026-10-26", nil))
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body %s", res.StatusCode, body)
	}

	if !strings.Contains(string(body), `"lessons":[]`) {
		t.Errorf("lessons must be an empty array: %s", body)
	}
}

func TestGetScheduleInvalid(t *testing.T) {
	t.Parallel()

	a := newScheduleApp(t)

	for _, query := range []string{
		"to=2026-10-26",
		"from=2026-10-19",
		"from=2026-13-01&to=2026-10-26",
		"from=19.10.2026&to=2026-10-26",
		"from=2026-10-19&to=2026-10-19",
		"from=2026-10-19&to=2026-10-12",
		"from=2026-10-19&to=2026-12-01", // 43 days
		"from=2026-10-19&to=2026-10-26&level=expert",
		"from=2026-10-19&to=2026-10-26&location=not-an-id",
		"from=2026-10-19&to=2026-10-26&location=00000000-0000-0000-0000-000000000000",
		"from=2026-10-19&to=2026-10-26&instructor={00000000-0000-7000-8000-000000000001}",
		"from=2026-10-19&to=2026-10-26&courseType=12345",
	} {
		res, body := a.get(t, "/api/v1/schedule?"+query)
		if res.StatusCode != http.StatusUnprocessableEntity {
			t.Errorf("%s: status = %d, body %s", query, res.StatusCode, body)

			continue
		}

		if ct := res.Header.Get("Content-Type"); ct != "application/problem+json" {
			t.Errorf("%s: Content-Type = %q", query, ct)
		}
	}

	if res, body := a.get(t, "/api/v1/schedule?from=2026-10-19&to=2026-11-30"); res.StatusCode != http.StatusOK {
		t.Errorf("42 days: status = %d, body %s", res.StatusCode, body)
	}
}

func TestGetLesson(t *testing.T) {
	t.Parallel()

	a := newScheduleApp(t)

	res, body := a.get(t, "/api/v1/lessons/"+a.id("ballet"))
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body %s", res.StatusCode, body)
	}

	got := decode(t, body)
	wantKeys := []string{
		"accompanist", "capacity", "classId", "courseType", "courseTypeDescription", "endsAt", "id",
		"instructor", "kind", "level", "location", "locationAddress", "room", "startsAt", "status",
		"visibility",
	}

	if !slices.Equal(sortedJSONKeys(got), wantKeys) {
		t.Fatalf("keys = %v, want %v", sortedJSONKeys(got), wantKeys)
	}

	if got["id"] != a.id("ballet") || got["locationAddress"] == "" || got["courseTypeDescription"] == "" {
		t.Errorf("body = %v", got)
	}

	for _, name := range []string{"canceled", "workshop"} {
		if res, body := a.get(t, "/api/v1/lessons/"+a.id(name)); res.StatusCode != http.StatusOK {
			t.Errorf("%s: status = %d, body %s", name, res.StatusCode, body)
		}
	}
}

func TestGetLessonNotVisible(t *testing.T) {
	t.Parallel()

	a := newScheduleApp(t)

	notFound := map[string]string{
		"hidden":     a.id("hidden"),
		"members":    a.id("members"),
		"unreleased": a.id("unreleased"),
		"unknown":    scheduleLessonID(t, 999).String(),
	}

	var first []byte

	for name, id := range notFound {
		res, body := a.get(t, "/api/v1/lessons/"+id)
		if res.StatusCode != http.StatusNotFound {
			t.Errorf("%s: status = %d, body %s", name, res.StatusCode, body)

			continue
		}

		if res.Header.Get("Content-Type") != "application/problem+json" {
			t.Errorf("%s: Content-Type = %q", name, res.Header.Get("Content-Type"))
		}

		// An invisible lesson must be indistinguishable from a missing one.
		if first == nil {
			first = body
		} else if string(body) != string(first) {
			t.Errorf("%s: body %s differs from %s", name, body, first)
		}
	}

	for _, id := range []string{"not-an-id", "00000000-0000-0000-0000-000000000000"} {
		if res, body := a.get(t, "/api/v1/lessons/"+id); res.StatusCode != http.StatusUnprocessableEntity {
			t.Errorf("%s: status = %d, body %s", id, res.StatusCode, body)
		}
	}
}

// TestScheduleEnums checks that the enums in the OpenAPI document, which
// come from struct tags, list exactly the domain values.
func TestScheduleEnums(t *testing.T) {
	t.Parallel()

	h, _ := newApp(t, nil)

	_, body := do(t, h, httptest.NewRequest(http.MethodGet, "/api/v1/openapi.json", nil))

	var spec struct {
		Paths map[string]map[string]struct {
			Parameters []struct {
				Name   string `json:"name"`
				Schema struct {
					Enum []string `json:"enum"`
				} `json:"schema"`
			} `json:"parameters"`
		} `json:"paths"`
		Components struct {
			Schemas map[string]struct {
				Properties map[string]struct {
					Enum []string `json:"enum"`
				} `json:"properties"`
			} `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(body, &spec); err != nil {
		t.Fatal(err)
	}

	var levelParam []string

	for _, p := range spec.Paths["/api/v1/schedule"]["get"].Parameters {
		if p.Name == "level" {
			levelParam = p.Schema.Enum
		}
	}

	levels := enumStrings(domain.Levels())
	summary := spec.Components.Schemas["LessonSummary"].Properties

	for name, c := range map[string]struct{ got, want []string }{
		"level parameter":   {levelParam, levels},
		"lesson level":      {summary["level"].Enum, levels},
		"lesson status":     {summary["status"].Enum, enumStrings(domain.LessonStatuses())},
		"lesson kind":       {summary["kind"].Enum, enumStrings(domain.LessonKinds())},
		"lesson visibility": {summary["visibility"].Enum, enumStrings(domain.LessonVisibilities())},
	} {
		if !slices.Equal(c.got, c.want) {
			t.Errorf("%s enum = %v, want %v", name, c.got, c.want)
		}
	}

	for _, id := range []string{`"getSchedule"`, `"getLesson"`} {
		if !strings.Contains(string(body), id) {
			t.Errorf("spec lacks operationId %s", id)
		}
	}
}

func enumStrings[S ~string](values []S) []string {
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = string(v)
	}

	return out
}
