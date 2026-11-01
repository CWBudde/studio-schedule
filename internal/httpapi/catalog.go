package httpapi

import (
	"context"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cwbudde/studio-schedule/internal/domain"
)

func registerCatalog(api huma.API, h *handlers) {
	register(api, AccessPublic, huma.Operation{
		OperationID: "listLocations",
		Method:      "GET",
		Path:        BasePath + "/locations",
		Summary:     "List locations",
		Description: "Returns all locations of the school ordered by name.",
		Tags:        []string{"catalog"},
	}, h.listLocations)

	register(api, AccessPublic, huma.Operation{
		OperationID: "listRooms",
		Method:      "GET",
		Path:        BasePath + "/rooms",
		Summary:     "List rooms",
		Description: "Returns the rooms of one location, or of all locations, ordered by location name, then " +
			"by their position within the location (the office's order), then by name.",
		Tags: []string{"catalog"},
	}, h.listRooms)

	register(api, AccessPublic, huma.Operation{
		OperationID: "listStaff",
		Method:      "GET",
		Path:        BasePath + "/staff",
		Summary:     "List staff",
		Description: "Returns the active instructors and accompanists ordered by display name.",
		Tags:        []string{"catalog"},
	}, h.listStaff)

	register(api, AccessPublic, huma.Operation{
		OperationID: "listCourseTypes",
		Method:      "GET",
		Path:        BasePath + "/course-types",
		Summary:     "List course types",
		Description: "Returns all course types (dance styles) ordered by name.",
		Tags:        []string{"catalog"},
	}, h.listCourseTypes)

	register(api, AccessPublic, huma.Operation{
		OperationID: "listLevels",
		Method:      "GET",
		Path:        BasePath + "/levels",
		Summary:     "List levels",
		Description: "Returns the school's levels ordered by position.",
		Tags:        []string{"catalog"},
	}, h.listLevels)
}

// Location is a site of the school.
type Location struct {
	ID      domain.LocationID `doc:"Location ID."              json:"id"`
	Name    string            `doc:"Display name."             json:"name"`
	Address string            `doc:"Postal address, if known." json:"address,omitempty"`
}

// LocationList is the response body of listLocations.
type LocationList struct {
	Items []Location `doc:"Locations ordered by name." json:"items" nullable:"false"`
}

// LocationListOutput is the response of listLocations.
type LocationListOutput struct {
	Body LocationList
}

func (h *handlers) listLocations(ctx context.Context, _ *struct{}) (*LocationListOutput, error) {
	locations, err := h.deps.Catalog.Locations(ctx)
	if err != nil {
		return nil, h.toHTTPError(ctx, err)
	}

	items := make([]Location, 0, len(locations))
	for _, l := range locations {
		items = append(items, Location{ID: l.ID, Name: l.Name, Address: l.Address})
	}

	return &LocationListOutput{Body: LocationList{Items: items}}, nil
}

// Room is a studio at a location.
type Room struct {
	ID         domain.RoomID     `doc:"Room ID."                                       json:"id"`
	LocationID domain.LocationID `doc:"Location the room belongs to."                  json:"locationId"`
	Name       string            `doc:"Display name."                                  json:"name"`
	Capacity   int               `doc:"Default capacity of its lessons."               json:"capacity" minimum:"1"`
	Position   int               `doc:"Place among the rooms of its location, from 1." json:"position" minimum:"1"`
}

// RoomList is the response body of listRooms.
type RoomList struct {
	Items []Room `doc:"Rooms ordered by location name, then by position and name." json:"items" nullable:"false"`
}

// RoomListInput is the request of listRooms.
type RoomListInput struct {
	Location domain.LocationID `doc:"Only list the rooms of this location." query:"location"`
}

// RoomListOutput is the response of listRooms.
type RoomListOutput struct {
	Body RoomList
}

func (h *handlers) listRooms(ctx context.Context, in *RoomListInput) (*RoomListOutput, error) {
	rooms, err := h.deps.Catalog.Rooms(ctx, in.Location)
	if err != nil {
		return nil, h.toHTTPError(ctx, err)
	}

	items := make([]Room, 0, len(rooms))
	for _, r := range rooms {
		items = append(items, newRoom(r))
	}

	return &RoomListOutput{Body: RoomList{Items: items}}, nil
}

func newRoom(r domain.Room) Room {
	return Room{ID: r.ID, LocationID: r.LocationID, Name: r.Name, Capacity: r.Capacity, Position: r.Position}
}

// StaffMember is an instructor or accompanist. The link to a person record
// is not exposed.
type StaffMember struct {
	ID          domain.StaffID   `doc:"Staff ID."                    json:"id"`
	DisplayName string           `doc:"Name shown in the timetable." json:"displayName"`
	Kind        domain.StaffKind `doc:"Role of the staff member."    enum:"instructor,accompanist" json:"kind"`
}

// StaffList is the response body of listStaff.
type StaffList struct {
	Items []StaffMember `doc:"Active staff ordered by display name." json:"items" nullable:"false"`
}

// StaffListOutput is the response of listStaff.
type StaffListOutput struct {
	Body StaffList
}

func (h *handlers) listStaff(ctx context.Context, _ *struct{}) (*StaffListOutput, error) {
	staff, err := h.deps.Catalog.Staff(ctx)
	if err != nil {
		return nil, h.toHTTPError(ctx, err)
	}

	items := make([]StaffMember, 0, len(staff))
	for _, s := range staff {
		items = append(items, StaffMember{ID: s.ID, DisplayName: s.DisplayName, Kind: s.Kind})
	}

	return &StaffListOutput{Body: StaffList{Items: items}}, nil
}

// CourseType is a catalog entry (dance style).
type CourseType struct {
	ID          domain.CourseTypeID `doc:"Course type ID."      json:"id"`
	Name        string              `doc:"Display name."        json:"name"`
	Description string              `doc:"Description, if any." json:"description,omitempty"`
}

// CourseTypeList is the response body of listCourseTypes.
type CourseTypeList struct {
	Items []CourseType `doc:"Course types ordered by name." json:"items" nullable:"false"`
}

// CourseTypeListOutput is the response of listCourseTypes.
type CourseTypeListOutput struct {
	Body CourseTypeList
}

func (h *handlers) listCourseTypes(ctx context.Context, _ *struct{}) (*CourseTypeListOutput, error) {
	courseTypes, err := h.deps.Catalog.CourseTypes(ctx)
	if err != nil {
		return nil, h.toHTTPError(ctx, err)
	}

	items := make([]CourseType, 0, len(courseTypes))
	for _, c := range courseTypes {
		items = append(items, CourseType{ID: c.ID, Name: c.Name, Description: c.Description})
	}

	return &CourseTypeListOutput{Body: CourseTypeList{Items: items}}, nil
}

// Level is a step on the school's level scale (ADR 0021).
type Level struct {
	ID       domain.LevelID `doc:"Level ID."                                                   json:"id"`
	Name     string         `doc:"Display name."                                               json:"name"`
	Code     string         `doc:"Short label, if any."                                        json:"code,omitempty"`
	Position int            `doc:"Place on the school's level scale, from 1 (least advanced)." json:"position" minimum:"1"`
}

// LevelList is the response body of listLevels.
type LevelList struct {
	Items []Level `doc:"Levels ordered by position." json:"items" nullable:"false"`
}

// LevelListOutput is the response of listLevels.
type LevelListOutput struct {
	Body LevelList
}

func (h *handlers) listLevels(ctx context.Context, _ *struct{}) (*LevelListOutput, error) {
	levels, err := h.deps.Catalog.Levels(ctx)
	if err != nil {
		return nil, h.toHTTPError(ctx, err)
	}

	items := make([]Level, 0, len(levels))
	for _, l := range levels {
		items = append(items, newLevel(l))
	}

	return &LevelListOutput{Body: LevelList{Items: items}}, nil
}

func newLevel(l domain.Level) Level {
	return Level{ID: l.ID, Name: l.Name, Code: l.Code, Position: l.Position}
}
