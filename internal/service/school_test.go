package service

import (
	"context"
	"errors"
	"strings"
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

	if _, _, err := load(); !errors.Is(err, ErrNoSchool) || !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("empty store: want ErrNoSchool (an ErrNotFound), got %v", err)
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

// TestLoadSchoolUnknownZone: settings naming a zone this installation
// cannot load (Settings.Validate refuses unknown ones, but zone data may be
// missing or older where the binary runs) fail as broken data naming the
// zone, not as a missing school.
func TestLoadSchoolUnknownZone(t *testing.T) {
	t.Parallel()

	st := memory.New()
	s := seed.DemoSettings()
	s.Timezone = "Mars/Olympus_Mons"

	err := st.InTx(t.Context(), store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		return tx.Settings().Put(ctx, s)
	})
	if err != nil {
		t.Fatal(err)
	}

	err = st.InTx(t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		_, _, err := loadSchool(ctx, tx)

		return err
	})
	if err == nil || errors.Is(err, domain.ErrNotFound) || !strings.Contains(err.Error(), "Mars/Olympus_Mons") {
		t.Fatalf("loadSchool = %v, want an internal error naming the zone", err)
	}
}
