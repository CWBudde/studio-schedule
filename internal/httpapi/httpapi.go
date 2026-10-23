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
	Settings *service.SettingsService
	Version  string
	Logger   *slog.Logger
}

// NewConfig returns the Huma configuration for the API: OpenAPI document at
// BasePath/openapi.json, docs at BasePath/docs, and no `$schema` response
// field or `Link` header (keeps generated client types clean).
func NewConfig() huma.Config {
	cfg := huma.DefaultConfig(Title, SpecVersion)
	cfg.Info.Description = "Schedule and booking API for dance and ballet schools."
	cfg.OpenAPIPath = BasePath + "/openapi"
	cfg.DocsPath = BasePath + "/docs"
	cfg.SchemasPath = ""
	cfg.CreateHooks = nil

	return cfg
}

// Register adds all API operations to api.
func Register(api huma.API, deps Deps) {
	h := &handlers{deps: deps}

	huma.Register(api, huma.Operation{
		OperationID: "getHealth",
		Method:      "GET",
		Path:        BasePath + "/health",
		Summary:     "Health check",
		Description: "Reports that the service is running and which version is deployed.",
		Tags:        []string{"system"},
	}, h.getHealth)

	huma.Register(api, huma.Operation{
		OperationID: "getPublicSettings",
		Method:      "GET",
		Path:        BasePath + "/settings/public",
		Summary:     "Public school settings",
		Description: "Returns the school settings that are visible without signing in.",
		Tags:        []string{"settings"},
	}, h.getPublicSettings)
}

type handlers struct {
	deps Deps
}

// Health is the response body of getHealth.
type Health struct {
	Status  string `doc:"Always \"ok\" when the service responds." enum:"ok" json:"status"`
	Version string `doc:"Build version of the running service."    json:"version"`
}

// HealthOutput is the response of getHealth.
type HealthOutput struct {
	Body Health
}

func (h *handlers) getHealth(context.Context, *struct{}) (*HealthOutput, error) {
	return &HealthOutput{Body: Health{Status: "ok", Version: h.deps.Version}}, nil
}

// PublicSettings is the response body of getPublicSettings.
type PublicSettings struct {
	SchoolName    string `doc:"Display name of the school."                      json:"schoolName"`
	Timezone      string `doc:"IANA time zone of the school, e.g. Europe/Berlin." json:"timezone"`
	DefaultLocale string `doc:"Default UI locale (BCP 47 tag), e.g. en."         json:"defaultLocale"`
}

// PublicSettingsOutput is the response of getPublicSettings.
type PublicSettingsOutput struct {
	Body PublicSettings
}

func (h *handlers) getPublicSettings(ctx context.Context, _ *struct{}) (*PublicSettingsOutput, error) {
	s, err := h.deps.Settings.Public(ctx)
	if err != nil {
		return nil, h.toHTTPError(ctx, err)
	}

	return &PublicSettingsOutput{Body: PublicSettings{
		SchoolName:    s.SchoolName,
		Timezone:      s.Timezone,
		DefaultLocale: s.DefaultLocale,
	}}, nil
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
