package main

import (
	"context"
	"errors"

	"github.com/spf13/cobra"

	"github.com/cwbudde/studio-schedule/cmd/studio-schedule/cli"
	"github.com/cwbudde/studio-schedule/internal/config"
	"github.com/cwbudde/studio-schedule/internal/store/postgres"
)

var (
	// errNoDatabaseURL is returned by the commands that need a database.
	errNoDatabaseURL = cli.ErrNoDatabaseURL
	// errSchemaOutdated is returned when the database lacks migrations that
	// the binary embeds.
	errSchemaOutdated = errors.New("the database schema is not up to date: run `studio-schedule migrate up`")
	// errSchemaNewer is returned when the database has migrations that the
	// binary does not embed.
	errSchemaNewer = errors.New("the database schema is newer than this binary: run a newer studio-schedule")
)

// databaseFlag reads the global --database-url (cli.App.Wire) for the
// commands that need a database.
type databaseFlag struct{}

// explicit returns --database-url if it was given.
func (databaseFlag) explicit(cmd *cobra.Command) *string {
	f := cmd.Flags().Lookup(config.FlagDatabaseURL)
	if f == nil || !f.Changed {
		return nil
	}

	v := f.Value.String()

	return &v
}

// load returns the configured database URL (flag > env) or errNoDatabaseURL.
// These commands write no log, so the log settings are not checked.
func (f databaseFlag) load(cmd *cobra.Command, env config.LookupFunc) (string, error) {
	flags := config.Flags{DatabaseURL: f.explicit(cmd)}

	cfg, err := config.LoadWithoutLog(env, flags)
	if err != nil {
		return "", err
	}

	if cfg.DatabaseURL == "" {
		return "", errNoDatabaseURL
	}

	return cfg.DatabaseURL, nil
}

// openPostgres opens the PostgreSQL store after checking, without writing,
// that exactly the embedded migrations are applied, so a server never runs
// against an older or newer schema.
func openPostgres(ctx context.Context, databaseURL string) (*postgres.Store, error) {
	m, err := postgres.NewMigrator(databaseURL)
	if err != nil {
		return nil, err
	}

	defer func() { _ = m.Close() }()

	switch err := m.CheckSchema(ctx); {
	case errors.Is(err, postgres.ErrSchemaPending):
		return nil, errSchemaOutdated
	case errors.Is(err, postgres.ErrSchemaNewer):
		return nil, errSchemaNewer
	case err != nil:
		return nil, err
	}

	return postgres.Open(ctx, databaseURL)
}
