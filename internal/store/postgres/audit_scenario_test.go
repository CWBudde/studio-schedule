package postgres

import (
	"testing"

	"github.com/cwbudde/studio-schedule/internal/service/servicetest"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// TestAuditScenarios runs the audit scenarios (the channel of an actor,
// ADR 0020) on PostgreSQL, as internal/service runs them on the memory
// store.
func TestAuditScenarios(t *testing.T) {
	t.Parallel()
	servicetest.Audit(t, func(t *testing.T) store.Store { return newTestStore(t) })
}
