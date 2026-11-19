package conformance

import (
	"cmp"
	"net/http"
	"slices"
	"testing"
)

type listedClass struct {
	ID, TermID, CourseTypeID, RoomID, StartTime string
	Weekday                                     int
	Retired                                     *bool
	Instructor                                  struct{ ID, DisplayName string }
	LevelFrom                                   struct{ Name string }
}

type seededTimetable struct {
	termID, startsOn, balletID string
	classes                    []listedClass
}

func readSeededTimetable(t *testing.T, b *backend, guest *http.Client) seededTimetable {
	t.Helper()

	type term struct{ ID, StartsOn, EndsOn string }
	terms := read[struct{ Items []term }](t, get(t, b, guest, "/api/v1/terms")).Items
	index := slices.IndexFunc(terms, func(term term) bool { return term.StartsOn <= demoDate && term.EndsOn >= demoDate })
	if index < 0 {
		t.Fatal("fixture has no current term")
	}
	current := terms[index]
	query := request{
		method: http.MethodGet, path: "/api/v1/terms/{termId}/classes",
		params: map[string]string{"termId": current.ID},
	}
	public := read[struct{ Items []listedClass }](t, b.call(t, guest, query, http.StatusOK)).Items
	if !slices.IsSortedFunc(public, func(a, b listedClass) int {
		return cmp.Or(cmp.Compare(a.Weekday, b.Weekday), cmp.Compare(a.StartTime, b.StartTime), cmp.Compare(a.ID, b.ID))
	}) {
		t.Fatal("public classes not in timetable order")
	}
	var ballet string
	for _, c := range public {
		if c.TermID != current.ID || c.Retired != nil {
			t.Errorf("unexpected public class: %+v", c)
		}
		if c.Weekday == 1 && c.StartTime == "16:30" && c.Instructor.DisplayName == "Alina Starling" && c.LevelFrom.Name == "Beginner" {
			ballet = c.ID
		}
	}
	if ballet == "" {
		t.Fatal("fixture ballet class is missing or lacks its staff or audience")
	}

	return seededTimetable{termID: current.ID, startsOn: current.StartsOn, balletID: ballet, classes: public}
}

func classListsAndRoster(t *testing.T, b *backend) {
	t.Helper()

	guest, office, member := browser(t), browser(t), browser(t)
	timetable := readSeededTimetable(t, b, guest)
	query := request{
		method: http.MethodGet, path: "/api/v1/admin/terms/{termId}/classes",
		params: map[string]string{"termId": timetable.termID},
	}
	b.call(t, guest, query, http.StatusUnauthorized)
	login(t, b, member, demoMember, demoPassword)
	b.call(t, member, query, http.StatusForbidden)
	login(t, b, office, "olga.ledgerby@example.net", demoPassword)
	all := read[struct{ Items []listedClass }](t, b.call(t, office, query, http.StatusOK)).Items
	active := make([]string, 0, len(all))
	for _, c := range all {
		switch {
		case c.Retired == nil:
			t.Error("office class is missing retirement state")
		case !*c.Retired:
			active = append(active, c.ID)
		}
	}
	if !slices.EqualFunc(timetable.classes, active, func(c listedClass, id string) bool { return c.ID == id }) {
		t.Error("office and public lists disagree about active classes")
	}

	roster := request{
		method: http.MethodGet, path: "/api/v1/admin/classes/{classId}/members",
		params: map[string]string{"classId": timetable.balletID},
	}
	b.call(t, guest, roster, http.StatusUnauthorized)
	b.call(t, member, roster, http.StatusForbidden)
	type enrollment struct {
		Person struct{ ID, GivenName, FamilyName string }
		Since  string
	}
	members := read[struct{ Items []enrollment }](t, b.call(t, office, roster, http.StatusOK)).Items
	names := make([]string, 0, len(members))
	for _, m := range members {
		if m.Person.ID == "" || m.Since != timetable.startsOn {
			t.Errorf("unexpected roster entry: %+v", m)
		}
		names = append(names, m.Person.GivenName+" "+m.Person.FamilyName)
	}
	if want := []string{"Ella Amberly", "Leni Dunfield", "Rosa Elderwick"}; !slices.Equal(names, want) {
		t.Errorf("roster = %v, want %v", names, want)
	}
}
