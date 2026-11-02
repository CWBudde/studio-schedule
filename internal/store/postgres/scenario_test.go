package postgres

import (
	"testing"

	"github.com/cwbudde/studio-schedule/internal/service/servicetest"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// The merge and import scenarios of ADR 0015, the privacy scenarios of
// ADR 0016, the credential scenarios of ADR 0004, the push scenarios, the
// integrity scenarios of ADR 0020 (db check), the event scenarios and the
// free-room scenarios under PostgreSQL's constraints, locks and queries;
// the memory store's run is in package service.

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

func TestPushScenarios(t *testing.T) {
	t.Parallel()
	servicetest.Push(t, func(t *testing.T) store.Store { return newTestStore(t) })
}

func TestIntegrityScenarios(t *testing.T) {
	t.Parallel()
	servicetest.Integrity(t, func(t *testing.T) store.Store { return newTestStore(t) })
}

func TestEventScenarios(t *testing.T) {
	t.Parallel()
	servicetest.Events(t, func(t *testing.T) store.Store { return newTestStore(t) })
}

func TestFreeRoomsScenarios(t *testing.T) {
	t.Parallel()
	servicetest.FreeRooms(t, func(t *testing.T) store.Store { return newTestStore(t) })
}
