package httpapi

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
)

func registerSystem(api huma.API, h *handlers) {
	register(api, AccessPublic, huma.Operation{
		OperationID: "getHealth",
		Method:      "GET",
		Path:        BasePath + "/health",
		Summary:     "Health check",
		Description: "Reports that the service is running. It names no version (ASVS 14.3.3): the office " +
			"finds it in getMe.",
		Tags: []string{"system"},
	}, h.getHealth)
}

// Health is the response body of getHealth.
type Health struct {
	Status string `doc:"Always \"ok\" when the service responds." enum:"ok" json:"status"`
}

// HealthOutput is the response of getHealth.
type HealthOutput struct {
	Body Health
}

func (h *handlers) getHealth(context.Context, *struct{}) (*HealthOutput, error) {
	return &HealthOutput{Body: Health{Status: "ok"}}, nil
}
