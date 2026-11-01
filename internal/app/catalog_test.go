package app_test

import (
	"cmp"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/app"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/seed"
	"github.com/cwbudde/studio-schedule/internal/store"
	"github.com/cwbudde/studio-schedule/internal/store/memory"
)

// catalogUnknownID is a well-formed ID that the demo data set does not use.
const catalogUnknownID = "ffffffff-ffff-7fff-bfff-ffffffffffff"

// getItems requests path, expects 200 and returns the "items" array of a
// list response, which must be the only field.
func getItems(t *testing.T, h http.Handler, path string) []any {
	t.Helper()

	res, body := do(t, h, httptest.NewRequest(http.MethodGet, path, nil))
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: status = %d, body %s", path, res.StatusCode, body)
	}

	got := decode(t, body)

	items, ok := got["items"].([]any)
	if !ok || len(got) != 1 {
		t.Fatalf("GET %s: body = %s, want exactly an items array", path, body)
	}

	return items
}

// jsonValue round-trips v through JSON, so it compares with decoded
// response bodies.
func jsonValue(t *testing.T, v any) any {
	t.Helper()

	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}

	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}

	return out
}

// sameItems compares the items of a list response with want, after a JSON
// round trip of want.
func sameItems(t *testing.T, path string, got []any, want []map[string]any) {
	t.Helper()

	if w := jsonValue(t, want); !reflect.DeepEqual(any(got), w) {
		gotJSON, _ := json.Marshal(got)
		wantJSON, _ := json.Marshal(w)
		t.Fatalf("GET %s:\n got  %s\n want %s", path, gotJSON, wantJSON)
	}
}

// byNameThenID orders items like the store lists: by name, then by the
// bytes of the ID.
func byNameThenID[T any](items []T, name func(T) string, id func(T) [16]byte) []T {
	return slices.SortedFunc(slices.Values(items), func(a, b T) int {
		ia, ib := id(a), id(b)

		return cmp.Or(cmp.Compare(name(a), name(b)), slices.Compare(ia[:], ib[:]))
	})
}

func TestListLocations(t *testing.T) {
	t.Parallel()

	h, _ := newApp(t, nil)
	got := getItems(t, h, "/api/v1/locations")

	var want []map[string]any

	for _, l := range byNameThenID(seed.Demo().Locations,
		func(l domain.Location) string { return l.Name },
		func(l domain.Location) [16]byte { return [16]byte(l.ID) }) {
		item := map[string]any{"id": l.ID, "name": l.Name}
		if l.Address != "" {
			item["address"] = l.Address
		}

		want = append(want, item)
	}

	sameItems(t, "/api/v1/locations", got, want)
}

func TestListLocationsWithoutAddress(t *testing.T) {
	t.Parallel()

	st := memory.New()
	loc := domain.Location{ID: domain.LocationID{0x01}, Name: "Pop-up Studio"}

	if err := createLocation(t, st, loc); err != nil {
		t.Fatal(err)
	}

	h, _ := app.New(app.Deps{Store: st})
	got := getItems(t, h, "/api/v1/locations")

	sameItems(t, "/api/v1/locations", got, []map[string]any{{"id": loc.ID, "name": loc.Name}})
}

func TestListRooms(t *testing.T) {
	t.Parallel()

	h, _ := newApp(t, nil)
	demo := seed.Demo()
	locationName := map[domain.LocationID]string{}
	for _, l := range demo.Locations {
		locationName[l.ID] = l.Name
	}
	// The store's order: by location name and ID, then position, name, ID.
	inListOrder := slices.SortedFunc(slices.Values(demo.Rooms), func(a, b domain.Room) int {
		return cmp.Or(
			cmp.Compare(locationName[a.LocationID], locationName[b.LocationID]),
			slices.Compare(a.LocationID[:], b.LocationID[:]),
			cmp.Compare(a.Position, b.Position),
			cmp.Compare(a.Name, b.Name),
			slices.Compare(a.ID[:], b.ID[:]),
		)
	})
	want := func(location domain.LocationID) []map[string]any {
		var items []map[string]any

		for _, r := range inListOrder {
			if !location.IsZero() && r.LocationID != location {
				continue
			}

			items = append(items, map[string]any{
				"id": r.ID, "locationId": r.LocationID, "name": r.Name, "capacity": r.Capacity, "position": r.Position,
			})
		}

		return items
	}

	t.Run("all", func(t *testing.T) {
		t.Parallel()

		for _, path := range []string{"/api/v1/rooms", "/api/v1/rooms?location="} {
			sameItems(t, path, getItems(t, h, path), want(domain.LocationID{}))
		}
	})

	t.Run("one location", func(t *testing.T) {
		t.Parallel()

		for _, l := range demo.Locations {
			path := "/api/v1/rooms?location=" + l.ID.String()
			sameItems(t, path, getItems(t, h, path), want(l.ID))
		}
	})

	t.Run("unknown location", func(t *testing.T) {
		t.Parallel()

		expectProblem(t, h, "/api/v1/rooms?location="+catalogUnknownID, http.StatusNotFound)
	})

	t.Run("invalid location", func(t *testing.T) {
		t.Parallel()

		for _, id := range []string{"studio-north", "00000000-0000-0000-0000-000000000000", strings.ToUpper(catalogUnknownID) + "0"} {
			expectProblem(t, h, "/api/v1/rooms?location="+id, http.StatusUnprocessableEntity)
		}
	})
}

func TestListStaff(t *testing.T) {
	t.Parallel()

	h, _ := newApp(t, nil)
	got := getItems(t, h, "/api/v1/staff")

	var want []map[string]any

	for _, s := range byNameThenID(seed.Demo().Staff,
		func(s domain.Staff) string { return s.DisplayName },
		func(s domain.Staff) [16]byte { return [16]byte(s.ID) }) {
		if s.Active {
			// No personId: the link to the person record is private.
			want = append(want, map[string]any{"id": s.ID, "displayName": s.DisplayName, "kind": s.Kind})
		}
	}

	sameItems(t, "/api/v1/staff", got, want)
}

func TestListCourseTypes(t *testing.T) {
	t.Parallel()

	h, _ := newApp(t, nil)
	got := getItems(t, h, "/api/v1/course-types")

	var want []map[string]any

	for _, c := range byNameThenID(seed.Demo().CourseTypes,
		func(c domain.CourseType) string { return c.Name },
		func(c domain.CourseType) [16]byte { return [16]byte(c.ID) }) {
		item := map[string]any{"id": c.ID, "name": c.Name}
		if c.Description != "" {
			item["description"] = c.Description
		}

		want = append(want, item)
	}

	sameItems(t, "/api/v1/course-types", got, want)
}

func TestListTerms(t *testing.T) {
	t.Parallel()

	h, _ := newApp(t, nil)
	got := getItems(t, h, "/api/v1/terms")

	var want []map[string]any
	for _, term := range seed.Demo().Terms {
		want = append(want, map[string]any{
			"id": term.ID, "name": term.Name, "startsOn": term.StartsOn.String(), "endsOn": term.EndsOn.String(),
		})
	}

	sameItems(t, "/api/v1/terms", got, want)
}

func TestListClasses(t *testing.T) {
	t.Parallel()

	h, _ := newApp(t, nil)
	demo := seed.Demo()
	path := "/api/v1/terms/" + demo.Terms[0].ID.String() + "/classes"
	got := getItems(t, h, path)

	classes := slices.SortedFunc(slices.Values(demo.Classes), func(a, b domain.Class) int {
		return cmp.Or(cmp.Compare(a.Weekday, b.Weekday), a.StartTime.Compare(b.StartTime),
			slices.Compare(a.ID[:], b.ID[:]))
	})

	staff := map[domain.StaffID]map[string]any{}
	for _, m := range demo.Staff {
		staff[m.ID] = map[string]any{"id": m.ID, "displayName": m.DisplayName}
	}

	var want []map[string]any

	for _, c := range classes {
		// No members: who attends a class is private.
		item := map[string]any{
			"id":              c.ID,
			"termId":          c.TermID,
			"courseTypeId":    c.CourseTypeID,
			"roomId":          c.RoomID,
			"instructor":      staff[c.InstructorID],
			"level":           c.Level,
			"weekday":         int(c.Weekday),
			"startTime":       c.StartTime.String(),
			"durationMinutes": int(c.Duration / time.Minute),
			"capacity":        c.Capacity,
		}
		if !c.AccompanistID.IsZero() {
			item["accompanist"] = staff[c.AccompanistID]
		}

		want = append(want, item)
	}

	sameItems(t, path, got, want)

	// The demo data must exercise both shapes of the optional field.
	var with, without bool
	for _, item := range want {
		_, ok := item["accompanist"]
		with, without = with || ok, without || !ok
	}

	if !with || !without {
		t.Error("the demo classes need some with and some without an accompanist")
	}
}

func TestListClassesErrors(t *testing.T) {
	t.Parallel()

	h, _ := newApp(t, nil)

	expectProblem(t, h, "/api/v1/terms/"+catalogUnknownID+"/classes", http.StatusNotFound)
	expectProblem(t, h, "/api/v1/terms/school-year/classes", http.StatusUnprocessableEntity)
	expectProblem(t, h, "/api/v1/terms/00000000-0000-0000-0000-000000000000/classes", http.StatusUnprocessableEntity)
}

func TestCatalogEmptyStore(t *testing.T) {
	t.Parallel()

	h, _ := app.New(app.Deps{Store: memory.New()})

	for _, path := range []string{
		"/api/v1/locations",
		"/api/v1/rooms",
		"/api/v1/staff",
		"/api/v1/course-types",
		"/api/v1/levels",
		"/api/v1/terms",
	} {
		res, body := do(t, h, httptest.NewRequest(http.MethodGet, path, nil))
		if res.StatusCode != http.StatusOK || strings.TrimSpace(string(body)) != `{"items":[]}` {
			t.Errorf("GET %s: status %d, body %s; want 200 with an empty items array", path, res.StatusCode, body)
		}
	}

	expectProblem(t, h, "/api/v1/rooms?location="+catalogUnknownID, http.StatusNotFound)
	expectProblem(t, h, "/api/v1/terms/"+catalogUnknownID+"/classes", http.StatusNotFound)
}

func TestCatalogOpenAPI(t *testing.T) {
	t.Parallel()

	_, api := newApp(t, nil)
	spec := api.OpenAPI()

	ops := map[string]string{
		"listLocations":   "/api/v1/locations",
		"listRooms":       "/api/v1/rooms",
		"listStaff":       "/api/v1/staff",
		"listCourseTypes": "/api/v1/course-types",
		"listLevels":      "/api/v1/levels",
		"listTerms":       "/api/v1/terms",
		"listClasses":     "/api/v1/terms/{termId}/classes",
	}

	for id, path := range ops {
		item := spec.Paths[path]
		if item == nil || item.Get == nil || item.Get.OperationID != id {
			t.Errorf("GET %s: want operation %s", path, id)
		}
	}

	schemas := spec.Components.Schemas.Map()

	enum := func(schema, field string) []any {
		s := schemas[schema]
		if s == nil || s.Properties[field] == nil {
			t.Fatalf("schema %s.%s missing", schema, field)
		}

		return s.Properties[field].Enum
	}

	if got, want := enum("StaffMember", "kind"), jsonValue(t, domain.StaffKinds()); !reflect.DeepEqual(any(got), want) {
		t.Errorf("StaffMember.kind enum = %v, want %v", got, want)
	}

	if got, want := enum("Class", "level"), jsonValue(t, domain.SkillLevels()); !reflect.DeepEqual(any(got), want) {
		t.Errorf("Class.level enum = %v, want %v", got, want)
	}

	if _, ok := schemas["StaffMember"].Properties["personId"]; ok {
		t.Error("StaffMember must not expose personId")
	}
}

func createLocation(t *testing.T, st store.Store, l domain.Location) error {
	t.Helper()

	return st.InTx(t.Context(), store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		return tx.Locations().Create(ctx, l)
	})
}

// expectProblem requests path and expects a problem+json response with the
// given status.
func expectProblem(t *testing.T, h http.Handler, path string, status int) {
	t.Helper()

	res, body := do(t, h, httptest.NewRequest(http.MethodGet, path, nil))
	if res.StatusCode != status {
		t.Fatalf("GET %s: status = %d, want %d; body %s", path, res.StatusCode, status, body)
	}

	if ct := res.Header.Get("Content-Type"); ct != "application/problem+json" {
		t.Errorf("GET %s: Content-Type = %q", path, ct)
	}
}
