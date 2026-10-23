package httpapi

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/service"
)

// registerCatalogAdmin registers the office's writes to the catalog
// (ADR 0007). The reads stay public (catalog.go).
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
		Description: "Adds a room to a location. A name another room of the location has (ignoring case) is a " +
			"409 `duplicate`; an unknown location is a 422 on `locationId`.",
		DefaultStatus: http.StatusCreated,
	}), h.createRoom)

	register(api, AccessOffice, write(huma.Operation{
		OperationID: "updateRoom",
		Method:      http.MethodPut,
		Path:        BasePath + "/rooms/{roomId}",
		Summary:     "Update a room",
		Description: "Replaces the location, name and capacity of a room. Existing lessons keep their " +
			"capacity. A name another room of the location has is a 409 `duplicate`.",
	}), h.updateRoom)

	register(api, AccessOffice, write(huma.Operation{
		OperationID:   "deleteRoom",
		Method:        http.MethodDelete,
		Path:          BasePath + "/rooms/{roomId}",
		Summary:       "Delete a room",
		Description:   "Deletes a room that no class or lesson uses; a used one is a 409 `in-use`.",
		DefaultStatus: http.StatusNoContent,
	}), h.deleteRoom)
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

// RoomOutput is the response of createRoom and updateRoom.
type RoomOutput struct {
	Body Room
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

func (h *handlers) deleteRoom(ctx context.Context, in *RoomRefInput) (*struct{}, error) {
	if err := h.deps.Catalog.DeleteRoom(ctx, in.RoomID); err != nil {
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

		return &RoomOutput{Body: Room{ID: r.ID, LocationID: r.LocationID, Name: r.Name, Capacity: r.Capacity}}, nil
	}
}

func (d LocationData) service() service.LocationData {
	return service.LocationData{Name: d.Name, Address: d.Address}
}

func (d RoomData) service() service.RoomData {
	return service.RoomData{LocationID: d.LocationID, Name: d.Name, Capacity: d.Capacity}
}
