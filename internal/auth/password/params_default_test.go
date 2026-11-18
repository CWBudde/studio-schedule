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

func TestPHPServerHashes(t *testing.T) {
	t.Parallel()

	const plaintext = "fictional Azur 🌙\x00clé"
	vectors := map[string]string{
		"password_hash": "$argon2id$v=19$m=19456,t=2,p=1$LkNDSjZlYXJSbDI0c2ZabA$pcgoob3GuxFAh611TEqHjpccdyl12zoaej5XJX1OMt0",
		"sodium":        "$argon2id$v=19$m=19456,t=2,p=1$8LHKmNyQ5Qtub21CDtZgTw$eJw6rOJEPjtG0ZtvIOSnWIBEFyT2i+lRLvzg+BJE4GU",
	}
	hasher := password.New()
	for provider, encoded := range vectors {
		t.Run(provider, func(t *testing.T) {
			ok, err := hasher.Verify(encoded, plaintext)
			if err != nil || !ok {
				t.Fatalf("Verify() = %v, %v; want true, nil", ok, err)
			}
			if hasher.NeedsRehash(encoded) {
				t.Fatal("PHP hash does not use the Go server's parameters")
			}
		})
	}
}
