package store_test

import (
	"testing"

	"github.com/cwbudde/studio-schedule/internal/store"
)

func TestTxModeString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		mode store.TxMode
		want string
	}{
		{store.ReadOnly, "read-only"},
		{store.ReadWrite, "read-write"},
		{store.TxMode(7), "unknown"},
		{store.TxMode(-1), "unknown"},
	}

	for _, tt := range tests {
		if got := tt.mode.String(); got != tt.want {
			t.Errorf("TxMode(%d).String() = %q, want %q", int(tt.mode), got, tt.want)
		}
	}
}
