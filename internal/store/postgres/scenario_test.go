package postgres

import (
	"testing"

	"github.com/cwbudde/studio-schedule/internal/service/servicetest"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// The merge and import scenarios of ADR 0015 and the privacy scenarios of
// ADR 0016 and the credential scenarios of ADR 0004 under PostgreSQL's constraints
// and locks; the memory store's run is in package service.

func TestMergeScenarios(t *testing.T) {
	t.Parallel()
	servicetest.Merge(t, func(t *testing.T) store.Store { return newTestStore(t) })
}

func TestImportScenarios(t *testing.T) {
	t.Parallel()
	servicetest.Import(t, func(t *testing.T) store.Store { return newTestStore(t) })
}

func TestPrivacyScenarios(t *testing.T) {
	t.Parallel()
	servicetest.Privacy(t, func(t *testing.T) store.Store { return newTestStore(t) })
}

func TestCredentialScenarios(t *testing.T) {
	t.Parallel()
	servicetest.Credentials(t, func(t *testing.T) store.Store { return newTestStore(t) })
}
