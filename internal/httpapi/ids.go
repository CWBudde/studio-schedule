package httpapi

import (
	"reflect"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cwbudde/studio-schedule/internal/domain"
)

// uuidSchema stands in for every domain ID type in the OpenAPI document.
// The domain package stays free of Huma, so the ID types cannot provide
// their own schema; instead registerIDSchemas makes each of them an alias
// of this type in the schema registry. It is a struct implementing
// huma.SchemaProvider, which Huma inlines instead of adding a component
// schema, so IDs appear as plain `{type: string, format: uuid}` (a `string`
// in the generated TypeScript types).
type uuidSchema struct{}

// Schema implements huma.SchemaProvider. Every call returns a new schema,
// because Huma sets per-field details (doc, ...) on the returned value.
func (uuidSchema) Schema(huma.Registry) *huma.Schema {
	return &huma.Schema{Type: huma.TypeString, Format: "uuid"}
}

// idTypes lists every ID type of internal/domain/id.go. A new ID type must
// be added here; TestIDSchemaRegisteredForEveryIDType and the OpenAPI walk
// in TestOpenAPIIDsAreUUIDStrings catch a missing one.
var idTypes = []reflect.Type{
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
	reflect.TypeFor[domain.MessageID](),
	reflect.TypeFor[domain.CalendarFeedID](),
	reflect.TypeFor[domain.ImportRunID](),
}

// registerIDSchemas makes every domain ID type a `{type: string, format:
// uuid}` schema in r. It only affects the documented schema (and Huma's
// validation against it); parsing still uses the ID types' UnmarshalText.
func registerIDSchemas(r huma.Registry) {
	target := reflect.TypeFor[uuidSchema]()
	for _, t := range idTypes {
		r.RegisterTypeAlias(t, target)
	}
}
