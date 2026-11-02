package mcpserver

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/httpapi"
	"github.com/cwbudde/studio-schedule/internal/service"
)

// maxFreeRoomsMinutes is the longest duration of the free_rooms tool, in
// minutes.
const maxFreeRoomsMinutes = int(service.MaxFreeRoomsDuration / time.Minute)

func roomTools() []tool {
	return []tool{{
		name: "free_rooms",
		description: "The rooms that are free on a school date from a start time for a duration, at one " +
			"location or at all of them (CatalogService.FreeRooms; no command): a room is taken while any " +
			"lesson that is not canceled overlaps the time, a room rental and a hidden lesson too; it is for staff, " +
			"as it would otherwise reveal those to members. A lesson that ends when the " +
			"time starts, or starts when it ends, leaves the room free. The date and start are the school's " +
			"wall clock, in its time zone. Each room has its ID, name and location, ordered by location name, " +
			"then as the location orders its rooms. At most " + strconv.Itoa(maxFreeRoomsMinutes) + " minutes (12 hours).",
		access:      httpapi.AccessStaff,
		annotations: readOnly("Free rooms"),
		add:         handler(freeRooms),
	}}
}

// freeRoomsInput are the arguments of the free_rooms tool.
type freeRoomsInput struct {
	Date            string  `json:"date"                 jsonschema:"The school date (YYYY-MM-DD)."`
	Start           string  `json:"start"                jsonschema:"The start, the school's wall clock time (HH:MM, 24 hours)."`
	DurationMinutes minutes `json:"durationMinutes"      jsonschema:"How long the rooms must be free, in whole minutes: more than 0, at most 720."`
	LocationID      string  `json:"locationId,omitempty" jsonschema:"Only the rooms of this location (its ID); default every location."`
}

// minutes is a whole number of minutes. Unlike an int, it refuses a JSON
// value that is no integer with a message that names the field, never the
// value (decode).
type minutes int

// errMinutes is the error of a durationMinutes that is no integer.
var errMinutes = errors.New("durationMinutes is not a whole number of minutes")

// UnmarshalJSON accepts a JSON integer only: no fraction, exponent, string
// or null.
func (m *minutes) UnmarshalJSON(b []byte) error {
	n, err := strconv.Atoi(string(b))
	if err != nil {
		return errMinutes
	}

	*m = minutes(n)

	return nil
}

// freeRoomsBody is the result of the free_rooms tool.
type freeRoomsBody struct {
	Rooms []freeRoom `json:"rooms"`
}

// freeRoom is a room of freeRoomsBody.
type freeRoom struct {
	ID       domain.RoomID       `json:"id"`
	Name     string              `json:"name"`
	Location httpapi.LocationRef `json:"location"`
}

// freeRooms is the free_rooms tool.
func freeRooms(ctx context.Context, s *Server, _ service.Principal, in freeRoomsInput) (any, error) {
	q, err := freeRoomsQuery(in)
	if err != nil {
		return nil, err
	}

	rooms, err := s.svc.Catalog.FreeRooms(ctx, q)
	if err != nil {
		return nil, err
	}

	locations, err := s.svc.Catalog.Locations(ctx)
	if err != nil {
		return nil, err
	}

	names := make(map[domain.LocationID]string, len(locations))
	for _, l := range locations {
		names[l.ID] = l.Name
	}

	body := freeRoomsBody{Rooms: make([]freeRoom, 0, len(rooms))}
	for _, r := range rooms {
		body.Rooms = append(body.Rooms, freeRoom{
			ID: r.ID, Name: r.Name, Location: httpapi.LocationRef{ID: r.LocationID, Name: names[r.LocationID]},
		})
	}

	return body, nil
}

// freeRoomsQuery is the query of in. Date and start are required; the
// service checks the duration.
func freeRoomsQuery(in freeRoomsInput) (service.FreeRoomsQuery, error) {
	var (
		q    service.FreeRoomsQuery
		errs [3]error
	)

	q.Date, errs[0] = parseDate("date", in.Date)
	if in.Date == "" {
		errs[0] = missing("date")
	}

	q.Start, errs[1] = parseTime("start", in.Start)
	if in.Start == "" {
		errs[1] = missing("start")
	}

	q.Location, errs[2] = parseID[domain.LocationID]("locationId", in.LocationID)

	// Clamped before the multiplication, so that a huge count cannot
	// overflow into a duration the service accepts; it refuses both ends.
	q.Duration = time.Duration(min(max(int(in.DurationMinutes), 0), maxFreeRoomsMinutes+1)) * time.Minute

	return q, errors.Join(errs[:]...)
}

// missing is the error of the required argument field left out.
func missing(field string) error {
	return &domain.FieldError{Field: field, Msg: field + " is required"}
}

// parseTime parses the wall clock time argument field (HH:MM). The error
// names the field, not the value.
func parseTime(field, value string) (domain.TimeOfDay, error) {
	t, err := domain.ParseTimeOfDay(value)
	if err != nil {
		return domain.TimeOfDay{}, &domain.FieldError{Field: field, Msg: field + " is not a time of day (HH:MM)"}
	}

	return t, nil
}
