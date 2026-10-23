package main

import (
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/spf13/cobra"
)

type logOptions struct {
	format string
	level  string
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "studio-schedule",
		Short:         "Schedule and booking system for dance and ballet schools",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.AddCommand(newServeCmd(), newOpenAPICmd(), newVersionCmd())

	return root
}

func (o *logOptions) register(cmd *cobra.Command) {
	cmd.Flags().StringVar(&o.format, "log-format", "text", "log format: text or json")
	cmd.Flags().StringVar(&o.level, "log-level", "info", "log level: debug, info, warn or error")
}

func (o *logOptions) logger(w io.Writer) (*slog.Logger, error) {
	var level slog.Level
	if err := level.UnmarshalText([]byte(o.level)); err != nil {
		return nil, fmt.Errorf("invalid --log-level %q: %w", o.level, err)
	}

	opts := &slog.HandlerOptions{Level: level}

	switch strings.ToLower(o.format) {
	case "text":
		return slog.New(slog.NewTextHandler(w, opts)), nil
	case "json":
		return slog.New(slog.NewJSONHandler(w, opts)), nil
	default:
		return nil, fmt.Errorf("invalid --log-format %q: want text or json", o.format)
	}
}
