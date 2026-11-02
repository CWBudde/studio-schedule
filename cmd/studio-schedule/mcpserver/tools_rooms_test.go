package mcpserver

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/service"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// The demo's locations.
const (
	studioNorth = "00000000-0000-7000-8000-000000000041"
	studioSouth = "00000000-0000-7000-8000-000000000045"
)

// roomNames lists the names of the rooms of a free_rooms document, in its
// order.
func roomNames(t *testing.T, doc map[string]any) []string {
	t.Helper()

	rooms, ok := doc["rooms"].([]any)
	if !ok {
		t.Fatalf("no rooms in %v", doc)
	}

	names := make([]string, 0, len(rooms))
	for _, r := range rooms {
		names = append(names, r.(map[string]any)["name"].(string))
	}

	return names
}

// scheduledRooms lists, sorted, the rooms that a lesson not canceled of
// the office's schedule of date overlaps from start (Europe/Berlin, the
// demo's zone) for minutes; a lesson that ends at start or starts at the
// end does not.
func scheduledRooms(t *testing.T, f *fixture, date, start string, minutes int) []string {
	t.Helper()

	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}

	from, err := time.ParseInLocation("2006-01-02 15:04", date+" "+start, berlin)
	if err != nil {
		t.Fatal(err)
	}

	to := from.Add(time.Duration(minutes) * time.Minute)

	doc := document(t, f.session(t, demoOffice, Options{}, registry), "schedule", map[string]any{"from": date, "to": date})

	var rooms []string

	for _, l := range doc["lessons"].([]any) {
		lesson := l.(map[string]any)

		startsAt, _ := time.Parse(time.RFC3339, lesson["startsAt"].(string))
		endsAt, _ := time.Parse(time.RFC3339, lesson["endsAt"].(string))

		if lesson["status"] != "canceled" && startsAt.Before(to) && endsAt.After(from) {
			rooms = append(rooms, lesson["room"].(map[string]any)["name"].(string))
		}
	}

	slices.Sort(rooms)

	return slices.Compact(rooms)
}

// allRooms are the demo's rooms in the order of free_rooms: by location
// name, then position.
var allRooms = []string{"Studio A", "Studio B", "Studio C", "Hall 1", "Hall 2"}

// TestFreeRoomsGolden: on the demo's Tuesday from 18:00 for an hour,
// Contemporary (18:30–19:45) takes Studio A; Jazz Dance in Hall 1 ends at
// 18:00 and Barre Fitness in Hall 2 starts at 19:00, so both halls are
// free. The rooms missing are exactly those the schedule has a lesson in.
func TestFreeRoomsGolden(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	args := map[string]any{"date": "2026-09-22", "start": "18:00", "durationMinutes": 60}

	text, isError := callTool(t, f.session(t, demoInstructor, Options{}, registry), "free_rooms", args)
	if isError {
		t.Fatalf("error result %q", text)
	}

	golden(t, "free_rooms.json", text)

	if taken := scheduledRooms(t, f, "2026-09-22", "18:00", 60); !slices.Equal(taken, []string{"Studio A"}) {
		t.Errorf("the schedule has lessons in %v", taken)
	}
}

// TestFreeRooms: a room is free unless a lesson overlaps the time, in the
// school's time zone; back to back is free. Each case agrees with the
// office's schedule of the day.
func TestFreeRooms(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	cs := f.session(t, demoInstructor, Options{}, registry)

	for _, tc := range []struct {
		start   string
		minutes int
		free    []string
	}{
		{"03:00", 60, allRooms},
		{"18:00", 60, []string{"Studio B", "Studio C", "Hall 1", "Hall 2"}},
		{"17:59", 61, []string{"Studio B", "Studio C", "Hall 2"}},           // Jazz Dance until 18:00
		{"18:00", 61, []string{"Studio B", "Studio C", "Hall 1"}},           // Barre Fitness from 19:00
		{"16:00", 45, []string{"Studio A", "Studio C", "Hall 1", "Hall 2"}}, // Pre-Ballet
		{"19:00", 60, []string{"Studio B", "Studio C", "Hall 1"}},           // 17:00 UTC
		{"08:00", 720, []string{"Studio C"}},                                // the whole evening
		{"23:59", 1, allRooms},                                              // the day's last minute
	} {
		doc := document(t, cs, "free_rooms",
			map[string]any{"date": "2026-09-22", "start": tc.start, "durationMinutes": tc.minutes})

		if got := roomNames(t, doc); !slices.Equal(got, tc.free) {
			t.Errorf("%s for %d min: %v, want %v", tc.start, tc.minutes, got, tc.free)
		}

		var taken []string

		for _, r := range allRooms {
			if !slices.Contains(tc.free, r) {
				taken = append(taken, r)
			}
		}

		slices.Sort(taken)

		if scheduled := scheduledRooms(t, f, "2026-09-22", tc.start, tc.minutes); !slices.Equal(scheduled, taken) {
			t.Errorf("%s for %d min: the schedule has lessons in %v, the tool took %v", tc.start, tc.minutes,
				scheduled, taken)
		}
	}
}

// TestFreeRoomsHidden: a hidden room rental takes its room.
func TestFreeRoomsHidden(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	cs := f.session(t, demoInstructor, Options{}, registry)

	// The rental of Studio B on Saturday, 13:30–15:30.
	doc := document(t, cs, "free_rooms",
		map[string]any{"date": "2026-09-26", "start": "14:00", "durationMinutes": 60, "locationId": studioNorth})
	if got := roomNames(t, doc); !slices.Equal(got, []string{"Studio A", "Studio C"}) {
		t.Errorf("free rooms %v", got)
	}

	if taken := scheduledRooms(t, f, "2026-09-26", "14:00", 60); !slices.Equal(taken, []string{"Studio B"}) {
		t.Errorf("the office's schedule has lessons in %v", taken)
	}
}

// TestFreeRoomsLocation: locationId keeps the rooms of one location; an
// unknown one is not found, not an empty list.
func TestFreeRoomsLocation(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	cs := f.session(t, demoInstructor, Options{}, registry)
	args := func(location string) map[string]any {
		return map[string]any{"date": "2026-09-22", "start": "18:00", "durationMinutes": 60, "locationId": location}
	}

	doc := document(t, cs, "free_rooms", args(studioSouth))
	if got := roomNames(t, doc); !slices.Equal(got, []string{"Hall 1", "Hall 2"}) {
		t.Errorf("Studio South: %v", got)
	}

	for _, r := range doc["rooms"].([]any) {
		if loc := r.(map[string]any)["location"]; loc.(map[string]any)["id"] != studioSouth ||
			loc.(map[string]any)["name"] != "Studio South" {
			t.Errorf("room %v", r)
		}
	}

	if got := roomNames(t, document(t, cs, "free_rooms", args(studioNorth))); !slices.Equal(got,
		[]string{"Studio B", "Studio C"}) {
		t.Errorf("Studio North: %v", got)
	}

	wantError(t, cs, "free_rooms", args("01890a5d-ac96-774b-bcce-b302099a8057"), "free rooms: location: not found")

	// Nothing free is an empty list, not null.
	text, _ := callTool(t, cs, "free_rooms",
		map[string]any{"date": "2026-09-22", "start": "08:00", "durationMinutes": 720, "locationId": studioSouth})
	if text != `{"rooms":[]}` {
		t.Errorf("no room free: %s", text)
	}

	calls := toolCalls(t, f.st)
	if outcomes := []string{calls[2].Details.Outcome, calls[3].Details.Outcome}; calls[2].Details.Tool != "free_rooms" ||
		!slices.Equal(outcomes, []string{"not-found", "ok"}) {
		t.Errorf("audit events %+v", calls)
	}
}

// TestFreeRoomsInput: date and start are required and checked by field,
// without their values; durationMinutes must be an integer, which the
// service checks for its range; locationId must be an ID.
func TestFreeRoomsInput(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	cs := f.session(t, demoInstructor, Options{}, registry)

	const (
		duration = "validation failed: the duration must be more than 0 and at most 12h0m0s"
		notWhole = "validation failed: the arguments do not fit the tool's input schema: " +
			"durationMinutes is not a whole number of minutes"
	)

	args := func(set map[string]any) map[string]any {
		a := map[string]any{"date": "2026-09-22", "start": "18:00", "durationMinutes": 60}
		for k, v := range set {
			if v == nil {
				delete(a, k)
			} else {
				a[k] = v
			}
		}

		return a
	}

	for _, tc := range []struct {
		args map[string]any
		text string
	}{
		{args(map[string]any{"date": "22.09.2026"}), "validation failed: date is not a date (YYYY-MM-DD)"},
		{args(map[string]any{"date": "2026-02-30"}), "validation failed: date is not a date (YYYY-MM-DD)"},
		{args(map[string]any{"start": "25:00"}), "validation failed: start is not a time of day (HH:MM)"},
		{args(map[string]any{"start": "6pm"}), "validation failed: start is not a time of day (HH:MM)"},
		{args(map[string]any{"start": "6:00"}), "validation failed: start is not a time of day (HH:MM)"},
		{args(map[string]any{"locationId": "Zebulon"}), "validation failed: locationId is not an ID"},
		{args(map[string]any{"date": nil}), "validation failed: date is required"},
		{args(map[string]any{"start": nil}), "validation failed: start is required"},
		{
			map[string]any{"locationId": "x"},
			"validation failed: date is required\nvalidation failed: start is required\n" +
				"validation failed: locationId is not an ID",
		},
		{args(map[string]any{"durationMinutes": nil}), duration},
		{args(map[string]any{"durationMinutes": 0}), duration},
		{args(map[string]any{"durationMinutes": -30}), duration},
		{args(map[string]any{"durationMinutes": 721}), duration},
		// (2^53 + 60) minutes in nanoseconds overflow to exactly an hour.
		{args(map[string]any{"durationMinutes": int64(1)<<53 + 60}), duration},
		{args(map[string]any{"durationMinutes": "60"}), notWhole},
		{args(map[string]any{"durationMinutes": 1.5}), notWhole},
		{args(map[string]any{"durationMinutes": 1e30}), notWhole},
		{map[string]any{"date": "2026-09-22", "start": "18:00", "durationMinutes": nil}, notWhole}, // null
	} {
		wantError(t, cs, "free_rooms", tc.args, tc.text)
	}

	// The longest duration is accepted.
	if got := roomNames(t, document(t, cs, "free_rooms", args(map[string]any{"start": "08:00", "durationMinutes": 720}))); !slices.Equal(got, []string{"Studio C"}) {
		t.Errorf("720 minutes: %v", got)
	}
}

// TestFreeRoomsSchema: the input schema lists the three required
// arguments, the duration as an integer, and locationId as optional.
func TestFreeRoomsSchema(t *testing.T) {
	t.Parallel()

	f := newFixture(t)

	res, err := f.session(t, demoInstructor, Options{}, registry).ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}

	i := slices.IndexFunc(res.Tools, func(tl *mcp.Tool) bool { return tl.Name == "free_rooms" })
	if i < 0 {
		t.Fatal("no free_rooms tool")
	}

	schema := res.Tools[i].InputSchema.(map[string]any)
	props := schema["properties"].(map[string]any)

	required := []string{}
	for _, r := range schema["required"].([]any) {
		required = append(required, r.(string))
	}

	if !slices.Equal(required, []string{"date", "start", "durationMinutes"}) ||
		props["durationMinutes"].(map[string]any)["type"] != "integer" ||
		props["locationId"].(map[string]any)["type"] != "string" || len(props) != 4 {
		t.Errorf("input schema %v", schema)
	}

	for name, p := range props {
		if p.(map[string]any)["description"] == "" {
			t.Errorf("%s has no description", name)
		}
	}
}

// TestFreeRoomsAudit: a call writes one mcp-tool-called event with the
// tool's name and the outcome, by the instructor's account.
func TestFreeRoomsAudit(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	instructor := f.principal(t, demoInstructor)
	cs := f.session(t, demoInstructor, Options{}, registry)

	document(t, cs, "free_rooms", map[string]any{"date": "2026-09-22", "start": "18:00", "durationMinutes": 60})

	calls := toolCalls(t, f.st)
	if len(calls) != 1 || calls[0].ActorID != instructor.AccountID ||
		calls[0].Details != (domain.AuditDetails{Tool: "free_rooms", Outcome: "ok", Via: via}) {
		t.Errorf("audit events %+v", calls)
	}
}

// TestFreeRoomsStaffOnly: a member, who would learn of hidden lessons and
// rentals from a room's occupancy, gets no free_rooms: it is not listed,
// a call by name is an unknown tool, and nothing is audited.
func TestFreeRoomsStaffOnly(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	cs := f.session(t, demoMember, Options{}, registry)

	if slices.Contains(toolNames(t, cs), "free_rooms") {
		t.Error("free_rooms is listed for a member")
	}

	_, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: "free_rooms", Arguments: map[string]any{"date": "2026-09-22", "start": "18:00", "durationMinutes": 60},
	})
	if err == nil || !strings.Contains(err.Error(), `unknown tool "free_rooms"`) {
		t.Errorf("call as a member: %v", err)
	}

	if calls := toolCalls(t, f.st); len(calls) != 0 {
		t.Errorf("audit events %+v", calls)
	}
}

// TestFreeRoomsSchool: without school settings, whose time zone the date
// needs, the call is not found; a store that fails, in the rooms or in
// their locations' names, is an unexpected error.
func TestFreeRoomsSchool(t *testing.T) {
	t.Parallel()

	args := map[string]any{"date": "2026-09-22", "start": "18:00", "durationMinutes": 60}

	t.Run("no settings", func(t *testing.T) {
		t.Parallel()

		f := fixtureOn(noSchool{newFixture(t).st})
		wantError(t, f.session(t, demoInstructor, Options{}, registry), "free_rooms", args,
			"free rooms: not found: no school settings")
	})

	t.Run("store fails", func(t *testing.T) {
		t.Parallel()

		st := &faultStore{Store: newFixture(t).st}
		f := fixtureOn(st)
		instructor := f.principal(t, demoInstructor)
		cs := f.sessionOf(t, instructor, func(context.Context) (service.Principal, error) { return instructor, nil },
			Options{}, registry)

		st.failing(store.ReadOnly, errors.New("connection reset"))

		wantError(t, cs, "free_rooms", args, "internal error")

		if log := f.log.String(); !strings.Contains(log, "connection reset") || !strings.Contains(log, "tool=free_rooms") {
			t.Errorf("log %q", log)
		}
	})

	t.Run("locations fail", func(t *testing.T) {
		t.Parallel()

		f := fixtureOn(noLocations{newFixture(t).st})
		wantError(t, f.session(t, demoInstructor, Options{}, registry), "free_rooms", args, "internal error")

		if log := f.log.String(); !strings.Contains(log, "list locations: connection reset") {
			t.Errorf("log %q", log)
		}
	})
}

// noLocations is a store whose list of locations fails; a location's Get
// and the rooms still work.
type noLocations struct{ store.Store }

func (s noLocations) InTx(ctx context.Context, mode store.TxMode, fn func(ctx context.Context, tx store.Tx) error) error {
	return s.Store.InTx(ctx, mode, func(ctx context.Context, tx store.Tx) error { return fn(ctx, noLocationsTx{tx}) })
}

type noLocationsTx struct{ store.Tx }

func (tx noLocationsTx) Locations() store.LocationRepo { return failingLocations{tx.Tx.Locations()} }

type failingLocations struct{ store.LocationRepo }

func (failingLocations) List(context.Context) ([]domain.Location, error) {
	return nil, errors.New("connection reset")
}
