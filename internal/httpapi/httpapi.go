// Package httpapi defines the HTTP/JSON API (Huma operations, request and
// response types) on top of the service layer. All operations live below
// BasePath; the OpenAPI 3.1 document is generated from the registered
// operations.
package httpapi

import (
	"context"
	"errors"
	"log/slog"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/service"
)

const (
	// BasePath is the path prefix of every API route.
	BasePath = "/api/v1"
	// Title is the API title in the OpenAPI document.
	Title = "Studio Schedule API"
	// SpecVersion is the version of the API contract reported in the OpenAPI
	// document. It is independent of the build version so that the
	// generated api/openapi.json stays stable across builds.
	SpecVersion = "0.1.0"
)

// Deps are the dependencies of the API handlers.
type Deps struct {
	Settings  *service.SettingsService
	Catalog   *service.CatalogService
	Timetable *service.TimetableService
	Schedule  *service.ScheduleService
	Version   string
	Logger    *slog.Logger
}

// NewConfig returns the Huma configuration for the API: OpenAPI document at
// BasePath/openapi.json, docs at BasePath/docs, and no `$schema` response
// field or `Link` header (keeps generated client types clean). Domain IDs
// are documented as `{type: string, format: uuid}` (see registerIDSchemas).
func NewConfig() huma.Config {
	cfg := huma.DefaultConfig(Title, SpecVersion)
	registerIDSchemas(cfg.Components.Schemas)
	cfg.Info.Description = "Schedule and booking API for dance and ballet schools."
	cfg.OpenAPIPath = BasePath + "/openapi"
	cfg.DocsPath = BasePath + "/docs"
	cfg.SchemasPath = ""
	cfg.CreateHooks = nil

	return cfg
}

// Register adds all API operations to api. Each area registers its
// operations in its own file.
func Register(api huma.API, deps Deps) {
	h := &handlers{deps: deps}

	registerSystem(api, h)
	registerSettings(api, h)
	registerCatalog(api, h)
	registerTimetable(api, h)
	registerSchedule(api, h)
}

type handlers struct {
	deps Deps
}

// toHTTPError maps domain errors to RFC 9457 problem responses. Unknown
// errors are logged and reported as 500 without leaking details.
func (h *handlers) toHTTPError(ctx context.Context, err error) error {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return huma.Error404NotFound("resource not found")
	case errors.Is(err, domain.ErrValidation):
		return huma.Error422UnprocessableEntity(err.Error())
	case errors.Is(err, domain.ErrConflict):
		return huma.Error409Conflict("conflict")
	case errors.Is(err, domain.ErrForbidden):
		return huma.Error403Forbidden("forbidden")
	default:
		h.deps.Logger.ErrorContext(ctx, "internal error", slog.Any("err", err))

		return huma.Error500InternalServerError("internal server error")
	}
}
