//go:build !js

package password_test

import (
	"testing"

	"github.com/cwbudde/studio-schedule/internal/auth/password"
)

func TestDefaultParamsServer(t *testing.T) {
	t.Parallel()

	want := password.Params{Memory: 19456, Time: 2, Threads: 1, SaltLen: 16, KeyLen: 32}
	if got := password.DefaultParams(); got != want {
		t.Fatalf("DefaultParams() = %+v, want %+v", got, want)
	}
}
