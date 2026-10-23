package httpapi

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
)

func registerSettings(api huma.API, h *handlers) {
	huma.Register(api, huma.Operation{
		OperationID: "getPublicSettings",
		Method:      "GET",
		Path:        BasePath + "/settings/public",
		Summary:     "Public school settings",
		Description: "Returns the school settings that are visible without signing in.",
		Tags:        []string{"settings"},
	}, h.getPublicSettings)
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
