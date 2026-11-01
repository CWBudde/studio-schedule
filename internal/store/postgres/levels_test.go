package postgres

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/service"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// TestCreateLevelsConcurrently adds levels from many goroutines: under
// LockScale each one takes its own place after the last (ADR 0021).
func TestCreateLevelsConcurrently(t *testing.T) {
	t.Parallel()

	const n = 8

	ctx := t.Context()
	st := newTestStore(t)
	svc := service.NewCatalogService(st, ids.V7{})

	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			if _, err := svc.CreateLevel(ctx, service.LevelData{Name: fmt.Sprintf("Level %d", i)}); err != nil {
				t.Error(err)
			}
		})
	}

	wg.Wait()

	levels, err := svc.Levels(ctx)
	if err != nil {
		t.Fatal(err)
	}

	positions := make([]int, 0, len(levels))
	for _, l := range levels {
		positions = append(positions, l.Position)
	}

	if want := []int{1, 2, 3, 4, 5, 6, 7, 8}; !slices.Equal(positions, want) {
		t.Errorf("positions = %v, want %v", positions, want)
	}
}

// TestCreateLevelWaitsForReorder adds a level while a reorder holds the
// scale: the new level waits and lands after the reordered ones, instead of
// slipping past an order that misses it.
func TestCreateLevelWaitsForReorder(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	st := newTestStore(t)
	svc := service.NewCatalogService(st, ids.V7{})

	a, err := svc.CreateLevel(ctx, service.LevelData{Name: "Advanced"})
	if err != nil {
		t.Fatal(err)
	}

	b, err := svc.CreateLevel(ctx, service.LevelData{Name: "Beginner"})
	if err != nil {
		t.Fatal(err)
	}

	locked, release := make(chan struct{}), make(chan struct{})
	reordered := make(chan error, 1)

	go func() {
		reordered <- st.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
			if err := tx.Levels().LockScale(ctx); err != nil {
				return err
			}

			close(locked)
			<-release

			return tx.Levels().SetOrder(ctx, []domain.LevelID{b.ID, a.ID})
		})
	}()

	<-locked

	created := make(chan error, 1)

	go func() {
		_, err := svc.CreateLevel(ctx, service.LevelData{Name: "Professional"})
		created <- err
	}()

	var early bool

	select {
	case err := <-created:
		// Not Fatal: the reorder must still be released and end.
		t.Errorf("create finished while the scale was locked: %v", err)

		early = true
	case <-time.After(200 * time.Millisecond):
	}

	close(release)

	if err := <-reordered; err != nil {
		t.Fatal(err)
	}

	if !early {
		if err := <-created; err != nil {
			t.Fatal(err)
		}
	}

	levels, err := svc.Levels(ctx)
	if err != nil {
		t.Fatal(err)
	}

	names := make([]string, 0, len(levels))
	for _, l := range levels {
		names = append(names, fmt.Sprintf("%d %s", l.Position, l.Name))
	}

	if want := []string{"1 Beginner", "2 Advanced", "3 Professional"}; !slices.Equal(names, want) {
		t.Errorf("levels = %v, want %v", names, want)
	}
}
