package httpapi

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/service"
)

// registerCatalogAdmin registers the office's writes to the catalog
// (ADR 0007), and the office's list of all staff. The public reads are in
// catalog.go.
func registerCatalogAdmin(api huma.API, h *handlers) {
	write := func(op huma.Operation) huma.Operation {
		op.Tags = []string{"catalog"}

		return withConflict(api, op)
	}

	register(api, AccessOffice, write(huma.Operation{
		OperationID:   "createLocation",
		Method:        http.MethodPost,
		Path:          BasePath + "/locations",
		Summary:       "Create a location",
		Description:   "Adds a location. A name another location has (ignoring case) is a 409 `duplicate`.",
		DefaultStatus: http.StatusCreated,
	}), h.createLocation)

	register(api, AccessOffice, write(huma.Operation{
		OperationID: "updateLocation",
		Method:      http.MethodPut,
		Path:        BasePath + "/locations/{locationId}",
		Summary:     "Update a location",
		Description: "Replaces the name and address of a location. A name another location has is a 409 `duplicate`.",
	}), h.updateLocation)

	register(api, AccessOffice, write(huma.Operation{
		OperationID:   "deleteLocation",
		Method:        http.MethodDelete,
		Path:          BasePath + "/locations/{locationId}",
		Summary:       "Delete a location",
		Description:   "Deletes a location without rooms; one with rooms is a 409 `in-use`.",
		DefaultStatus: http.StatusNoContent,
	}), h.deleteLocation)

	register(api, AccessOffice, write(huma.Operation{
		OperationID: "createRoom",
		Method:      http.MethodPost,
		Path:        BasePath + "/rooms",
		Summary:     "Create a room",
		Description: "Adds a room to a location, after its last room. A name another room of the location has " +
			"(ignoring case) is a 409 `duplicate`; an unknown location is a 422 on `locationId`.",
		DefaultStatus: http.StatusCreated,
	}), h.createRoom)

	register(api, AccessOffice, write(huma.Operation{
		OperationID: "updateRoom",
		Method:      http.MethodPut,
		Path:        BasePath + "/rooms/{roomId}",
		Summary:     "Update a room",
		Description: "Replaces the location, name and capacity of a room. The room keeps its place, or goes " +
			"after the last room of its new location. Existing lessons keep their capacity. A name another " +
			"room of the location has is a 409 `duplicate`.",
	}), h.updateRoom)

	register(api, AccessOffice, write(huma.Operation{
		OperationID: "setRoomOrder",
		Method:      http.MethodPut,
		Path:        BasePath + "/locations/{locationId}/room-order",
		Summary:     "Set the order of a location's rooms",
		Description: "Numbers the rooms of a location from 1 in the order given; lists and the schedule show " +
			"them in this order. The list must name every room of the location exactly once and no other " +
			"room, else it is a 422 on `roomIds`, also when a room was added or removed meanwhile. A new " +
			"room goes after the last one, as does a room moved to the location.",
		DefaultStatus: http.StatusNoContent,
	}), h.setRoomOrder)

	register(api, AccessOffice, write(huma.Operation{
		OperationID:   "deleteRoom",
		Method:        http.MethodDelete,
		Path:          BasePath + "/rooms/{roomId}",
		Summary:       "Delete a room",
		Description:   "Deletes a room that no class or lesson uses; a used one is a 409 `in-use`.",
		DefaultStatus: http.StatusNoContent,
	}), h.deleteRoom)

	register(api, AccessOffice, write(huma.Operation{
		OperationID:   "createCourseType",
		Method:        http.MethodPost,
		Path:          BasePath + "/course-types",
		Summary:       "Create a course type",
		Description:   "Adds a course type. A name another course type has (ignoring case) is a 409 `duplicate`.",
		DefaultStatus: http.StatusCreated,
	}), h.createCourseType)

	register(api, AccessOffice, write(huma.Operation{
		OperationID: "updateCourseType",
		Method:      http.MethodPut,
		Path:        BasePath + "/course-types/{courseTypeId}",
		Summary:     "Update a course type",
		Description: "Replaces the name and description of a course type; its classes and lessons show the " +
			"new name. A name another course type has is a 409 `duplicate`.",
	}), h.updateCourseType)

	register(api, AccessOffice, write(huma.Operation{
		OperationID:   "deleteCourseType",
		Method:        http.MethodDelete,
		Path:          BasePath + "/course-types/{courseTypeId}",
		Summary:       "Delete a course type",
		Description:   "Deletes a course type that no class or lesson uses; a used one is a 409 `in-use`.",
		DefaultStatus: http.StatusNoContent,
	}), h.deleteCourseType)

	register(api, AccessOffice, huma.Operation{
		OperationID: "listAllStaff",
		Method:      http.MethodGet,
		Path:        BasePath + "/admin/staff",
		Summary:     "List all staff",
		Description: "Returns every staff member ordered by display name, including former (inactive) staff " +
			"and the link to a person, which the public `listStaff` leaves out.",
		Tags: []string{"catalog"},
	}, h.listAllStaff)

	register(api, AccessOffice, write(huma.Operation{
		OperationID: "createStaff",
		Method:      http.MethodPost,
		Path:        BasePath + "/staff",
		Summary:     "Create a staff member",
		Description: "Adds an instructor or accompanist, optionally linked to a person. An unknown person is " +
			"a 422 on `personId`.",
		DefaultStatus: http.StatusCreated,
	}), h.createStaff)

	register(api, AccessOffice, write(huma.Operation{
		OperationID: "updateStaff",
		Method:      http.MethodPut,
		Path:        BasePath + "/staff/{staffId}",
		Summary:     "Update a staff member",
		Description: "Replaces the name, kind, person link and active flag of a staff member. Inactive staff " +
			"leave the public staff list and the schedule filters but stay on their lessons.",
	}), h.updateStaff)

	register(api, AccessOffice, write(huma.Operation{
		OperationID: "deleteStaff",
		Method:      http.MethodDelete,
		Path:        BasePath + "/staff/{staffId}",
		Summary:     "Delete a staff member",
		Description: "Deletes a staff member that no class or lesson refers to; a referenced one is a 409 " +
			"`in-use` and can be deactivated instead.",
		DefaultStatus: http.StatusNoContent,
	}), h.deleteStaff)
}

// LocationData is the editable part of a location.
type LocationData struct {
	Name    string `doc:"Display name, unique within the school (ignoring case)." json:"name"              maxLength:"100" minLength:"1"`
	Address string `doc:"Postal address, if known."                               json:"address,omitempty" maxLength:"200"`
}

// RoomData is the editable part of a room.
type RoomData struct {
	LocationID domain.LocationID `doc:"Location the room belongs to."                            json:"locationId"`
	Name       string            `doc:"Display name, unique within its location (ignoring case)." json:"name"       maxLength:"100" minLength:"1"`
	Capacity   int               `doc:"Default capacity of its lessons."                         json:"capacity"   maximum:"1000"  minimum:"1"`
}

// CourseTypeData is the editable part of a course type.
type CourseTypeData struct {
	Name        string `doc:"Display name, unique (ignoring case)." json:"name"                  maxLength:"100"  minLength:"1"`
	Description string `doc:"Description, if any."                  json:"description,omitempty" maxLength:"2000"`
}

// StaffData is the editable part of a staff member.
type StaffData struct {
	DisplayName string           `doc:"Name shown in the timetable." json:"displayName" maxLength:"100" minLength:"1"`
	Kind        domain.StaffKind `doc:"Role of the staff member."    enum:"instructor,accompanist" json:"kind"`
	PersonID    domain.PersonID  `doc:"Person record of the staff member, if any." json:"personId,omitzero" required:"false"`
	Active      bool             `doc:"Whether the staff member is listed publicly; former staff are inactive." json:"active"`
}

// StaffEntry is a staff member in the office's list, with what the public
// StaffMember leaves out: the linked person and whether they are active.
type StaffEntry struct {
	ID          domain.StaffID   `doc:"Staff ID."                    json:"id"`
	DisplayName string           `doc:"Name shown in the timetable." json:"displayName"`
	Kind        domain.StaffKind `doc:"Role of the staff member."    enum:"instructor,accompanist" json:"kind"`
	Person      *PersonMatch     `doc:"Person record of the staff member, if any." json:"person,omitempty"`
	Active      bool             `doc:"Whether the staff member is listed publicly." json:"active"`
}

// StaffEntryList is the response body of listAllStaff.
type StaffEntryList struct {
	Items []StaffEntry `doc:"All staff ordered by display name." json:"items" nullable:"false"`
}

// StaffEntryListOutput is the response of listAllStaff.
type StaffEntryListOutput struct {
	Body StaffEntryList
}

// LocationDataInput is the request of createLocation.
type LocationDataInput struct {
	Body LocationData
}

// LocationUpdateInput is the request of updateLocation.
type LocationUpdateInput struct {
	LocationID domain.LocationID `doc:"The location." path:"locationId"`
	Body       LocationData
}

// LocationRefInput is the request of deleteLocation.
type LocationRefInput struct {
	LocationID domain.LocationID `doc:"The location." path:"locationId"`
}

// LocationOutput is the response of createLocation and updateLocation.
type LocationOutput struct {
	Body Location
}

// RoomDataInput is the request of createRoom.
type RoomDataInput struct {
	Body RoomData
}

// RoomUpdateInput is the request of updateRoom.
type RoomUpdateInput struct {
	RoomID domain.RoomID `doc:"The room." path:"roomId"`
	Body   RoomData
}

// RoomRefInput is the request of deleteRoom.
type RoomRefInput struct {
	RoomID domain.RoomID `doc:"The room." path:"roomId"`
}

// RoomOrder is the request body of setRoomOrder.
type RoomOrder struct {
	RoomIDs []domain.RoomID `doc:"Every room of the location, each once, in their new order." json:"roomIds" maxItems:"1000" nullable:"false"`
}

// RoomOrderInput is the request of setRoomOrder.
type RoomOrderInput struct {
	LocationID domain.LocationID `doc:"The location." path:"locationId"`
	Body       RoomOrder
}

// RoomOutput is the response of createRoom and updateRoom.
type RoomOutput struct {
	Body Room
}

// CourseTypeDataInput is the request of createCourseType.
type CourseTypeDataInput struct {
	Body CourseTypeData
}

// CourseTypeUpdateInput is the request of updateCourseType.
type CourseTypeUpdateInput struct {
	CourseTypeID domain.CourseTypeID `doc:"The course type." path:"courseTypeId"`
	Body         CourseTypeData
}

// CourseTypeRefInput is the request of deleteCourseType.
type CourseTypeRefInput struct {
	CourseTypeID domain.CourseTypeID `doc:"The course type." path:"courseTypeId"`
}

// CourseTypeOutput is the response of createCourseType and updateCourseType.
type CourseTypeOutput struct {
	Body CourseType
}

// StaffDataInput is the request of createStaff.
type StaffDataInput struct {
	Body StaffData
}

// StaffUpdateInput is the request of updateStaff.
type StaffUpdateInput struct {
	StaffID domain.StaffID `doc:"The staff member." path:"staffId"`
	Body    StaffData
}

// StaffRefInput is the request of deleteStaff.
type StaffRefInput struct {
	StaffID domain.StaffID `doc:"The staff member." path:"staffId"`
}

// StaffOutput is the response of createStaff and updateStaff.
type StaffOutput struct {
	Body StaffEntry
}

func (h *handlers) createLocation(ctx context.Context, in *LocationDataInput) (*LocationOutput, error) {
	return h.locationOutput(ctx)(h.deps.Catalog.CreateLocation(ctx, in.Body.service()))
}

func (h *handlers) updateLocation(ctx context.Context, in *LocationUpdateInput) (*LocationOutput, error) {
	return h.locationOutput(ctx)(h.deps.Catalog.UpdateLocation(ctx, in.LocationID, in.Body.service()))
}

func (h *handlers) deleteLocation(ctx context.Context, in *LocationRefInput) (*struct{}, error) {
	if err := h.deps.Catalog.DeleteLocation(ctx, in.LocationID); err != nil {
		return nil, h.toHTTPError(ctx, err)
	}

	return &struct{}{}, nil
}

func (h *handlers) createRoom(ctx context.Context, in *RoomDataInput) (*RoomOutput, error) {
	return h.roomOutput(ctx)(h.deps.Catalog.CreateRoom(ctx, in.Body.service()))
}

func (h *handlers) updateRoom(ctx context.Context, in *RoomUpdateInput) (*RoomOutput, error) {
	return h.roomOutput(ctx)(h.deps.Catalog.UpdateRoom(ctx, in.RoomID, in.Body.service()))
}

func (h *handlers) setRoomOrder(ctx context.Context, in *RoomOrderInput) (*struct{}, error) {
	if err := h.deps.Catalog.SetRoomOrder(ctx, in.LocationID, in.Body.RoomIDs); err != nil {
		return nil, h.toHTTPError(ctx, err)
	}

	return &struct{}{}, nil
}

func (h *handlers) deleteRoom(ctx context.Context, in *RoomRefInput) (*struct{}, error) {
	if err := h.deps.Catalog.DeleteRoom(ctx, in.RoomID); err != nil {
		return nil, h.toHTTPError(ctx, err)
	}

	return &struct{}{}, nil
}

func (h *handlers) createCourseType(ctx context.Context, in *CourseTypeDataInput) (*CourseTypeOutput, error) {
	return h.courseTypeOutput(ctx)(h.deps.Catalog.CreateCourseType(ctx, in.Body.service()))
}

func (h *handlers) updateCourseType(ctx context.Context, in *CourseTypeUpdateInput) (*CourseTypeOutput, error) {
	return h.courseTypeOutput(ctx)(h.deps.Catalog.UpdateCourseType(ctx, in.CourseTypeID, in.Body.service()))
}

func (h *handlers) deleteCourseType(ctx context.Context, in *CourseTypeRefInput) (*struct{}, error) {
	if err := h.deps.Catalog.DeleteCourseType(ctx, in.CourseTypeID); err != nil {
		return nil, h.toHTTPError(ctx, err)
	}

	return &struct{}{}, nil
}

func (h *handlers) listAllStaff(ctx context.Context, _ *struct{}) (*StaffEntryListOutput, error) {
	staff, err := h.deps.Catalog.AllStaff(ctx)
	if err != nil {
		return nil, h.toHTTPError(ctx, err)
	}

	items := make([]StaffEntry, 0, len(staff))
	for _, s := range staff {
		items = append(items, staffEntry(s))
	}

	return &StaffEntryListOutput{Body: StaffEntryList{Items: items}}, nil
}

func (h *handlers) createStaff(ctx context.Context, in *StaffDataInput) (*StaffOutput, error) {
	return h.staffOutput(ctx)(h.deps.Catalog.CreateStaff(ctx, in.Body.service()))
}

func (h *handlers) updateStaff(ctx context.Context, in *StaffUpdateInput) (*StaffOutput, error) {
	return h.staffOutput(ctx)(h.deps.Catalog.UpdateStaff(ctx, in.StaffID, in.Body.service()))
}

func (h *handlers) deleteStaff(ctx context.Context, in *StaffRefInput) (*struct{}, error) {
	if err := h.deps.Catalog.DeleteStaff(ctx, in.StaffID); err != nil {
		return nil, h.toHTTPError(ctx, err)
	}

	return &struct{}{}, nil
}

func (h *handlers) locationOutput(ctx context.Context) func(domain.Location, error) (*LocationOutput, error) {
	return func(l domain.Location, err error) (*LocationOutput, error) {
		if err != nil {
			return nil, h.toHTTPError(ctx, err)
		}

		return &LocationOutput{Body: Location{ID: l.ID, Name: l.Name, Address: l.Address}}, nil
	}
}

func (h *handlers) roomOutput(ctx context.Context) func(domain.Room, error) (*RoomOutput, error) {
	return func(r domain.Room, err error) (*RoomOutput, error) {
		if err != nil {
			return nil, h.toHTTPError(ctx, err)
		}

		return &RoomOutput{Body: newRoom(r)}, nil
	}
}

func (d LocationData) service() service.LocationData {
	return service.LocationData{Name: d.Name, Address: d.Address}
}

func (d RoomData) service() service.RoomData {
	return service.RoomData{LocationID: d.LocationID, Name: d.Name, Capacity: d.Capacity}
}

func (h *handlers) courseTypeOutput(ctx context.Context) func(domain.CourseType, error) (*CourseTypeOutput, error) {
	return func(c domain.CourseType, err error) (*CourseTypeOutput, error) {
		if err != nil {
			return nil, h.toHTTPError(ctx, err)
		}

		return &CourseTypeOutput{Body: CourseType{ID: c.ID, Name: c.Name, Description: c.Description}}, nil
	}
}

func (h *handlers) staffOutput(ctx context.Context) func(service.StaffEntry, error) (*StaffOutput, error) {
	return func(s service.StaffEntry, err error) (*StaffOutput, error) {
		if err != nil {
			return nil, h.toHTTPError(ctx, err)
		}

		return &StaffOutput{Body: staffEntry(s)}, nil
	}
}

func staffEntry(e service.StaffEntry) StaffEntry {
	out := StaffEntry{ID: e.ID, DisplayName: e.DisplayName, Kind: e.Kind, Active: e.Active}
	if !e.PersonID.IsZero() {
		p := personMatch(e.Person)
		out.Person = &p
	}

	return out
}

func (d CourseTypeData) service() service.CourseTypeData {
	return service.CourseTypeData{Name: d.Name, Description: d.Description}
}

func (d StaffData) service() service.StaffData {
	return service.StaffData{DisplayName: d.DisplayName, Kind: d.Kind, PersonID: d.PersonID, Active: d.Active}
}
