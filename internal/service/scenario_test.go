package service_test

import (
	"testing"

	"github.com/cwbudde/studio-schedule/internal/service/servicetest"
	"github.com/cwbudde/studio-schedule/internal/store"
	"github.com/cwbudde/studio-schedule/internal/store/memory"
)

// The merge, import, privacy, credential, push and audit scenarios on the memory store; the PostgreSQL
// store's tests run them too.

func TestMergeScenarios(t *testing.T) {
	t.Parallel()
	servicetest.Merge(t, func(*testing.T) store.Store { return memory.New() })
}

func TestImportScenarios(t *testing.T) {
	t.Parallel()
	servicetest.Import(t, func(*testing.T) store.Store { return memory.New() })
}

func TestPrivacyScenarios(t *testing.T) {
	t.Parallel()
	servicetest.Privacy(t, func(*testing.T) store.Store { return memory.New() })
}

func TestCredentialScenarios(t *testing.T) {
	t.Parallel()
	servicetest.Credentials(t, func(*testing.T) store.Store { return memory.New() })
}

func TestPushScenarios(t *testing.T) {
	t.Parallel()
	servicetest.Push(t, func(*testing.T) store.Store { return memory.New() })
}

func TestAuditScenarios(t *testing.T) {
	t.Parallel()
	servicetest.Audit(t, func(*testing.T) store.Store { return memory.New() })
}
