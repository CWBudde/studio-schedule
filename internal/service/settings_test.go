package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/seed"
	"github.com/cwbudde/studio-schedule/internal/service"
	"github.com/cwbudde/studio-schedule/internal/store"
	"github.com/cwbudde/studio-schedule/internal/store/memory"
)

func TestSettingsPublic(t *testing.T) {
	t.Parallel()

	st := memory.New()
	svc := service.NewSettingsService(st)

	if _, err := svc.Public(t.Context()); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("empty store: want ErrNotFound, got %v", err)
	}

	want := seed.DemoSettings()

	err := st.InTx(t.Context(), store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		return tx.Settings().Put(ctx, want)
	})
	if err != nil {
		t.Fatal(err)
	}

	got, err := svc.Public(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	if got != want {
		t.Fatalf("Public() = %+v, want %+v", got, want)
	}
}
