package app_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/app"
	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/seed"
	"github.com/cwbudde/studio-schedule/internal/store"
	"github.com/cwbudde/studio-schedule/internal/store/memory"
)

// TestNewServices: the command line's services (ADR 0020) are every
// service of Services, on the store of the deps.
func TestNewServices(t *testing.T) {
	t.Parallel()

	st := memory.New()
	if err := seed.LoadDemo(t.Context(), st); err != nil {
		t.Fatal(err)
	}

	svc := app.NewServices(app.Deps{
		Store: st, Clock: clock.NewFixed(time.Date(2026, 9, 22, 8, 0, 0, 0, time.UTC)), IDs: ids.NewSequence(),
		AppURL: "https://schedule.example.org/",
	})

	// Every field, a service added later included.
	v := reflect.ValueOf(svc)
	for i := range v.NumField() {
		if f := v.Field(i); f.IsNil() {
			t.Errorf("Services.%s is nil", v.Type().Field(i).Name)
		}
	}

	// The services read the given store.
	var want domain.Settings

	err := st.InTx(t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		var err error
		want, err = tx.Settings().Get(ctx)

		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	got, err := svc.Settings.Public(t.Context())
	if err != nil || got.SchoolName == "" || got.SchoolName != want.SchoolName || got.Timezone != want.Timezone {
		t.Errorf("Settings.Public = %+v, %v; want the store's %q", got.Settings, err, want.SchoolName)
	}

	// On an empty store, the same service finds nothing.
	empty := app.NewServices(app.Deps{Store: memory.New()})
	if _, err := empty.Settings.Public(t.Context()); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("an empty store: %v, want domain.ErrNotFound", err)
	}
}

func TestNewServicesNeedsStore(t *testing.T) {
	t.Parallel()

	defer func() {
		if r, ok := recover().(string); !ok || !strings.Contains(r, "Deps.Store is required") {
			t.Errorf("recovered %v, want the missing store", r)
		}
	}()

	app.NewServices(app.Deps{})
}
