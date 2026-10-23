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
		Description: "Reports that the service is running and which version is deployed.",
		Tags:        []string{"system"},
	}, h.getHealth)
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
