package main

import (
	"context"
	"errors"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/cwbudde/studio-schedule/internal/config"
	"github.com/cwbudde/studio-schedule/internal/store/postgres"
)

var (
	// errNoDatabaseURL is returned by the commands that need a database.
	errNoDatabaseURL = errors.New("no database URL: set " + config.EnvDatabaseURL + " or --database-url")
	// errSchemaOutdated is returned when the database lacks migrations that
	// the binary embeds.
	errSchemaOutdated = errors.New("the database schema is not up to date: run `studio-schedule migrate up`")
	// errSchemaNewer is returned when the database has migrations that the
	// binary does not embed.
	errSchemaNewer = errors.New("the database schema is newer than this binary: run a newer studio-schedule")
)

const databaseURLUsage = "PostgreSQL connection URL or key=value DSN; prefer the env var, flags are " +
	"visible in the process list (env " + config.EnvDatabaseURL + ")"

// databaseFlag binds --database-url for the commands that need a database.
type databaseFlag struct{ url string }

func (f *databaseFlag) register(flags *pflag.FlagSet) {
	flags.StringVar(&f.url, "database-url", "", databaseURLUsage)
}

// load returns the configured database URL (flag > env) or errNoDatabaseURL.
func (f *databaseFlag) load(cmd *cobra.Command, env config.LookupFunc) (string, error) {
	var flags config.Flags
	if cmd.Flags().Changed("database-url") {
		flags.DatabaseURL = &f.url
	}

	cfg, err := config.Load(env, flags)
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
