package main

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/cwbudde/studio-schedule/internal/app"
	"github.com/cwbudde/studio-schedule/internal/store/memory"
)

func newOpenAPICmd() *cobra.Command {
	return &cobra.Command{
		Use:   "openapi",
		Short: "Print the OpenAPI 3.1 specification as JSON",
		Long: "Print the OpenAPI 3.1 specification of the HTTP API to stdout. " +
			"The output is deterministic; it is committed as api/openapi.json and " +
			"used to generate the TypeScript client.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return writeOpenAPI(cmd.OutOrStdout())
		},
	}
}

func writeOpenAPI(w io.Writer) error {
	// The store is never touched; it only satisfies app.New.
	_, api := app.New(app.Deps{Store: memory.New(), Version: version})

	spec, err := json.MarshalIndent(api.OpenAPI(), "", "  ")
	if err != nil {
		return fmt.Errorf("marshal OpenAPI: %w", err)
	}

	if _, err := fmt.Fprintf(w, "%s\n", spec); err != nil {
		return fmt.Errorf("write OpenAPI: %w", err)
	}

	return nil
}
