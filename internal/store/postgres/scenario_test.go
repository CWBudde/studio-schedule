package postgres

import (
	"testing"

	"github.com/cwbudde/studio-schedule/internal/service/servicetest"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// The merge and import scenarios of ADR 0015 under PostgreSQL's constraints
// and locks; the memory store's run is in package service.

func TestMergeScenarios(t *testing.T) {
	t.Parallel()
	servicetest.Merge(t, func(t *testing.T) store.Store { return newTestStore(t) })
}

func TestImportScenarios(t *testing.T) {
	t.Parallel()
	servicetest.Import(t, func(t *testing.T) store.Store { return newTestStore(t) })
}
