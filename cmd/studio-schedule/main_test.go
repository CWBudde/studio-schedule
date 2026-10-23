package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func run(t *testing.T, args ...string) string {
	t.Helper()

	var out bytes.Buffer

	cmd := newRootCmd()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("%v: %v\n%s", args, err, out.String())
	}

	return out.String()
}

func TestOpenAPICommand(t *testing.T) {
	first := run(t, "openapi")
	if second := run(t, "openapi"); first != second {
		t.Fatal("openapi output is not deterministic")
	}

	var spec struct {
		OpenAPI string                               `json:"openapi"`
		Paths   map[string]map[string]map[string]any `json:"paths"`
	}
	if err := json.Unmarshal([]byte(first), &spec); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if spec.OpenAPI != "3.1.0" {
		t.Errorf("openapi = %q", spec.OpenAPI)
	}

	for path, id := range map[string]string{
		"/api/v1/health":          "getHealth",
		"/api/v1/settings/public": "getPublicSettings",
	} {
		if got := spec.Paths[path]["get"]["operationId"]; got != id {
			t.Errorf("%s operationId = %v, want %s", path, got, id)
		}
	}
}

func TestVersionCommand(t *testing.T) {
	if got := strings.TrimSpace(run(t, "version")); got != version {
		t.Fatalf("version = %q, want %q", got, version)
	}
}

func TestLogOptions(t *testing.T) {
	for _, o := range []logOptions{{"text", "info"}, {"json", "debug"}, {"JSON", "warn"}} {
		if _, err := o.logger(&bytes.Buffer{}); err != nil {
			t.Errorf("%+v: %v", o, err)
		}
	}

	for _, o := range []logOptions{{"xml", "info"}, {"text", "loud"}} {
		if _, err := o.logger(&bytes.Buffer{}); err == nil {
			t.Errorf("%+v: want error", o)
		}
	}
}
