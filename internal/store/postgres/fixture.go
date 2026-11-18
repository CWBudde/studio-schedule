package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/cwbudde/studio-schedule/internal/ids"
)

// Fixture owns a fresh private schema for HTTP conformance tests. All app
// connections use only that schema. The reset operation can only erase the
// schema this instance created.
type Fixture struct {
	Store  *Store
	schema string
	admin  *pgx.ConnConfig
	config *pgxpool.Config
}

// OpenFixture creates and migrates an isolated schema in the named database.
func OpenFixture(ctx context.Context, databaseURL string) (*Fixture, error) {
	cfg, err := poolConfig(databaseURL)
	if err != nil {
		return nil, err
	}

	admin := cfg.ConnConfig.Copy()
	conn, err := pgx.ConnectConfig(ctx, admin)
	if err != nil {
		return nil, fmt.Errorf("connect to fixture database: %w", mapErr(err))
	}

	defer func() { _ = conn.Close(context.WithoutCancel(ctx)) }()

	name := "studio_conformance_" + strings.ReplaceAll(ids.V7{}.NewUUID().String(), "-", "")
	schema := pgx.Identifier{name}.Sanitize()
	if _, err := conn.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		return nil, fmt.Errorf("create fixture schema: %w", mapErr(err))
	}

	f := &Fixture{schema: schema, admin: admin, config: cfg}
	// A connection option must not put a caller's schema back on the
	// fixture connections' search path.
	delete(cfg.ConnConfig.RuntimeParams, "options")
	cfg.ConnConfig.RuntimeParams["search_path"] = name

	if err = f.migrate(ctx); err == nil {
		f.Store, err = openPool(ctx, cfg)
	}
	if err != nil {
		return nil, errors.Join(err, f.Close(context.WithoutCancel(ctx)))
	}

	return f, nil
}

// Reset recreates the fixture schema and applies the migrations again. The
// caller serializes reset with application requests so that no transaction
// uses the schema while it is being replaced.
func (f *Fixture) Reset(ctx context.Context) error {
	conn, err := pgx.ConnectConfig(ctx, f.admin)
	if err != nil {
		return fmt.Errorf("connect to reset fixture schema: %w", mapErr(err))
	}
	defer func() { _ = conn.Close(context.WithoutCancel(ctx)) }()

	err = pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "DROP SCHEMA "+f.schema+" CASCADE"); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, "CREATE SCHEMA "+f.schema)

		return err
	})
	if err != nil {
		return fmt.Errorf("reset fixture schema: %w", mapErr(err))
	}

	return f.migrate(ctx)
}

// A new goose provider checks and creates its version table for this schema.
func (f *Fixture) migrate(ctx context.Context) error {
	m, err := newMigrator(f.config)
	if err != nil {
		return err
	}

	_, err = m.Up(ctx)

	return errors.Join(err, m.Close())
}

// Close closes the app's connections and removes the fixture schema.
func (f *Fixture) Close(ctx context.Context) error {
	if f.Store != nil {
		f.Store.Close()
	}
	conn, err := pgx.ConnectConfig(ctx, f.admin)
	if err != nil {
		return fmt.Errorf("connect to remove fixture schema: %w", mapErr(err))
	}

	_, err = conn.Exec(ctx, "DROP SCHEMA IF EXISTS "+f.schema+" CASCADE")

	return errors.Join(mapErr(err), conn.Close(context.WithoutCancel(ctx)))
}
