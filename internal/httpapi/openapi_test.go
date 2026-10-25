package httpapi_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/httpapi"
)

// spec returns the OpenAPI document of all registered operations as
// generic JSON. The handlers are never called, so the deps stay empty.
func spec(t *testing.T) map[string]any {
	t.Helper()

	api := humachi.New(chi.NewRouter(), httpapi.NewConfig())
	httpapi.Register(api, httpapi.Deps{})

	raw, err := json.Marshal(api.OpenAPI())
	if err != nil {
		t.Fatal(err)
	}

	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}

	return m
}

// idQueryParams are query parameters whose value is an ID but whose name
// does not end in "Id".
var idQueryParams = map[string]bool{"location": true, "instructor": true, "courseType": true}

func isIDName(name string) bool {
	return name == "id" || strings.HasSuffix(name, "Id")
}

func checkUUIDSchema(t *testing.T, where string, s any) {
	t.Helper()

	m, ok := s.(map[string]any)
	if !ok {
		t.Errorf("%s: schema is %T, want an inline object", where, s)

		return
	}

	if m["type"] != "string" || m["format"] != "uuid" {
		t.Errorf("%s: schema = %v, want {type: string, format: uuid}", where, m)
	}

	if _, isRef := m["$ref"]; isRef {
		t.Errorf("%s: ID schema must be inlined, got $ref %v", where, m["$ref"])
	}
}

// walkProperties visits every object schema below node and checks each
// property that names an ID.
func walkProperties(t *testing.T, path string, node any, checked *int) {
	t.Helper()

	switch n := node.(type) {
	case map[string]any:
		if props, ok := n["properties"].(map[string]any); ok {
			for name, prop := range props {
				if isIDName(name) {
					checkUUIDSchema(t, path+".properties."+name, prop)

					*checked++
				}
			}
		}

		for k, v := range n {
			walkProperties(t, path+"."+k, v, checked)
		}
	case []any:
		for _, v := range n {
			walkProperties(t, path+"[]", v, checked)
		}
	}
}

func TestOpenAPIIDsAreUUIDStrings(t *testing.T) {
	t.Parallel()

	doc := spec(t)

	var props int

	walkProperties(t, "#", doc, &props)

	if props == 0 {
		t.Fatal("no ID properties found; the walk is broken")
	}

	var params int

	paths, _ := doc["paths"].(map[string]any)
	for p, item := range paths {
		for method, op := range item.(map[string]any) {
			opMap, ok := op.(map[string]any)
			if !ok {
				continue
			}

			list, _ := opMap["parameters"].([]any)
			for _, raw := range list {
				param := raw.(map[string]any)
				name, _ := param["name"].(string)

				if isIDName(name) || (param["in"] == "query" && idQueryParams[name]) {
					checkUUIDSchema(t, method+" "+p+" param "+name, param["schema"])

					params++
				}
			}
		}
	}

	// lessonId, termId, rooms?location, schedule?location/instructor/courseType.
	if params < 6 {
		t.Errorf("checked %d ID parameters, want at least 6", params)
	}

	// No component schema per ID type: IDs stay inline, so the generated
	// TypeScript types do not change shape.
	schemas, _ := doc["components"].(map[string]any)["schemas"].(map[string]any)
	for name := range schemas {
		if strings.HasPrefix(name, "ID") || strings.HasSuffix(name, "ID") {
			t.Errorf("unexpected component schema %q for an ID type", name)
		}
	}
}

func TestIDSchemaRegisteredForEveryIDType(t *testing.T) {
	t.Parallel()

	reg := httpapi.NewConfig().Components.Schemas

	for _, typ := range []reflect.Type{
		reflect.TypeFor[domain.PersonID](),
		reflect.TypeFor[domain.AccountID](),
		reflect.TypeFor[domain.LocationID](),
		reflect.TypeFor[domain.RoomID](),
		reflect.TypeFor[domain.StaffID](),
		reflect.TypeFor[domain.CourseTypeID](),
		reflect.TypeFor[domain.TermID](),
		reflect.TypeFor[domain.ClassID](),
		reflect.TypeFor[domain.LessonID](),
		reflect.TypeFor[domain.BookingID](),
		reflect.TypeFor[domain.BookingEventID](),
		reflect.TypeFor[domain.WaitlistEntryID](),
		reflect.TypeFor[domain.PassTypeID](),
		reflect.TypeFor[domain.PassID](),
		reflect.TypeFor[domain.ReceiptID](),
		reflect.TypeFor[domain.AnnouncementID](),
		reflect.TypeFor[domain.CalendarFeedID](),
		reflect.TypeFor[domain.ImportRunID](),
		reflect.TypeFor[*domain.LessonID](),
	} {
		s := reg.Schema(typ, true, "")
		if s == nil || s.Type != huma.TypeString || s.Format != "uuid" || s.Ref != "" {
			t.Errorf("%v: schema = %+v, want inline {type: string, format: uuid}", typ, s)
		}

		// Every call returns a fresh schema: callers set field docs on it.
		if s2 := reg.Schema(typ, true, ""); s2 == s {
			t.Errorf("%v: registry returned a shared schema", typ)
		}
	}
}
