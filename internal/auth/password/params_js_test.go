//go:build js

package password_test

import (
	"testing"

	"github.com/cwbudde/studio-schedule/internal/auth/password"
)

func TestDefaultParamsDemo(t *testing.T) {
	t.Parallel()

	if got := password.DefaultParams(); got != cheap {
		t.Fatalf("DefaultParams() = %+v, want the demo parameters %+v", got, cheap)
	}
}
