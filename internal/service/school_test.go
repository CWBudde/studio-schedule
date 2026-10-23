package service

import (
	"context"
	"errors"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/seed"
	"github.com/cwbudde/studio-schedule/internal/store"
	"github.com/cwbudde/studio-schedule/internal/store/memory"
)

func TestLoadSchool(t *testing.T) {
	t.Parallel()

	st := memory.New()
	load := func() (domain.Settings, string, error) {
		var (
			s    domain.Settings
			zone string
		)

		err := st.InTx(t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
			settings, loc, err := loadSchool(ctx, tx)
			if err != nil {
				return err
			}

			s, zone = settings, loc.String()

			return nil
		})

		return s, zone, err
	}

	if _, _, err := load(); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("empty store: want ErrNotFound, got %v", err)
	}

	want := seed.DemoSettings()

	err := st.InTx(t.Context(), store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		return tx.Settings().Put(ctx, want)
	})
	if err != nil {
		t.Fatal(err)
	}

	got, zone, err := load()
	if err != nil || got != want || zone != want.Timezone {
		t.Fatalf("loadSchool = %+v, %q, %v; want %+v in %s", got, zone, err, want, want.Timezone)
	}
}
