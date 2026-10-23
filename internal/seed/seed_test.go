package seed_test

import (
	"testing"

	"github.com/cwbudde/studio-schedule/internal/seed"
	"github.com/cwbudde/studio-schedule/internal/service"
	"github.com/cwbudde/studio-schedule/internal/store/memory"
)

func TestDemoSettingsValid(t *testing.T) {
	t.Parallel()

	if err := seed.DemoSettings().Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestLoadDemo(t *testing.T) {
	t.Parallel()

	st := memory.New()
	if err := seed.LoadDemo(t.Context(), st); err != nil {
		t.Fatal(err)
	}

	got, err := service.NewSettingsService(st).Public(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	if got != seed.DemoSettings() {
		t.Fatalf("got %+v, want demo settings", got)
	}
}
