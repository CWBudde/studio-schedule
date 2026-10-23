package clock_test

import (
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/clock"
)

func TestFixed(t *testing.T) {
	t.Parallel()

	want := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

	var c clock.Clock = clock.NewFixed(want)
	if got := c.Now(); !got.Equal(want) {
		t.Fatalf("Now() = %v, want %v", got, want)
	}
}

func TestSystem(t *testing.T) {
	t.Parallel()

	var c clock.Clock = clock.System{}
	if c.Now().IsZero() {
		t.Fatal("System.Now() returned zero time")
	}
}
