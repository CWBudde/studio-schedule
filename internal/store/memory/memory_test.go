package memory_test

import (
	"testing"

	"github.com/cwbudde/studio-schedule/internal/store"
	"github.com/cwbudde/studio-schedule/internal/store/memory"
	"github.com/cwbudde/studio-schedule/internal/store/storetest"
)

func TestContract(t *testing.T) {
	t.Parallel()

	storetest.Run(t, func(*testing.T) store.Store { return memory.New() })
}
