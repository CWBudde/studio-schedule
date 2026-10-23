package password_test

import (
	"bytes"
	"encoding/base64"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/auth/password"
)

// cheap keeps the tests fast; it matches the demo (js) parameters.
var cheap = password.Params{Memory: 1024, Time: 1, Threads: 1, SaltLen: 16, KeyLen: 32}

// demoHash is the PHC hash of the fictional demo password "demo-password"
// with the demo parameters and the salt "demo-seed-salt-1". The seed data
// stores it so that the WASM demo never hashes at startup. The key was
// cross-checked with Node's crypto.argon2Sync.
const demoHash = "$argon2id$v=19$m=1024,t=1,p=1$ZGVtby1zZWVkLXNhbHQtMQ$sxSynfRZl9y9AigRtRVAVxaqp24lgJlVioaAAiofZSs"

func newHasher(t *testing.T, p password.Params, salt io.Reader) *password.Argon2id {
	t.Helper()

	h, err := password.NewWithParams(p, salt)
	if err != nil {
		t.Fatalf("NewWithParams(%+v): %v", p, err)
	}

	return h
}

func TestRoundTrip(t *testing.T) {
	t.Parallel()

	h := newHasher(t, cheap, nil)

	for _, pw := range []string{"correct horse battery staple", "", "ünïcødé 🔑", strings.Repeat("x", 1000)} {
		encoded, err := h.Hash(pw)
		if err != nil {
			t.Fatalf("Hash(%q): %v", pw, err)
		}

		if !strings.HasPrefix(encoded, "$argon2id$v=19$m=1024,t=1,p=1$") {
			t.Fatalf("Hash(%q) = %q, unexpected prefix", pw, encoded)
		}

		ok, err := h.Verify(encoded, pw)
		if err != nil || !ok {
			t.Fatalf("Verify(%q, %q) = %v, %v; want true, nil", encoded, pw, ok, err)
		}

		ok, err = h.Verify(encoded, pw+"!")
		if err != nil || ok {
			t.Fatalf("Verify with wrong password = %v, %v; want false, nil", ok, err)
		}
	}
}

func TestSaltDiffersPerCall(t *testing.T) {
	t.Parallel()

	h := newHasher(t, cheap, nil)

	a, err := h.Hash("same")
	if err != nil {
		t.Fatal(err)
	}

	b, err := h.Hash("same")
	if err != nil {
		t.Fatal(err)
	}

	if a == b {
		t.Fatalf("two hashes of the same password are equal: %q", a)
	}
}

func TestDeterministicWithInjectedSalt(t *testing.T) {
	t.Parallel()

	salt := []byte("0123456789abcdef")

	a, err := newHasher(t, cheap, bytes.NewReader(salt)).Hash("secret")
	if err != nil {
		t.Fatal(err)
	}

	b, err := newHasher(t, cheap, bytes.NewReader(salt)).Hash("secret")
	if err != nil {
		t.Fatal(err)
	}

	if a != b {
		t.Fatalf("hashes with the same salt differ:\n%s\n%s", a, b)
	}

	parts := strings.Split(a, "$")
	if got, want := parts[4], base64.RawStdEncoding.EncodeToString(salt); got != want {
		t.Fatalf("salt segment = %q, want %q", got, want)
	}
}

func TestHashSaltReaderError(t *testing.T) {
	t.Parallel()

	// Fewer bytes than SaltLen must fail instead of producing a short salt.
	h := newHasher(t, cheap, bytes.NewReader([]byte("short")))
	if _, err := h.Hash("secret"); err == nil {
		t.Fatal("Hash with an exhausted salt reader succeeded")
	}
}

func TestDemoHashVerifies(t *testing.T) {
	t.Parallel()

	h := newHasher(t, cheap, strings.NewReader("demo-seed-salt-1"))

	got, err := h.Hash("demo-password")
	if err != nil {
		t.Fatal(err)
	}

	if got != demoHash {
		t.Fatalf("demo hash = %q, want %q", got, demoHash)
	}

	// The production hasher reads the cheap parameters from the hash.
	for _, v := range []*password.Argon2id{h, password.New()} {
		ok, err := v.Verify(demoHash, "demo-password")
		if err != nil || !ok {
			t.Fatalf("Verify(demoHash, demo-password) = %v, %v; want true, nil", ok, err)
		}

		ok, err = v.Verify(demoHash, "Demo-password")
		if err != nil || ok {
			t.Fatalf("Verify(demoHash, wrong) = %v, %v; want false, nil", ok, err)
		}
	}
}

func TestVerifyUsesParamsFromHash(t *testing.T) {
	t.Parallel()

	other := password.Params{Memory: 2048, Time: 2, Threads: 2, SaltLen: 8, KeyLen: 16}

	encoded, err := newHasher(t, other, nil).Hash("secret")
	if err != nil {
		t.Fatal(err)
	}

	if !strings.HasPrefix(encoded, "$argon2id$v=19$m=2048,t=2,p=2$") {
		t.Fatalf("Hash = %q, unexpected prefix", encoded)
	}

	ok, err := newHasher(t, cheap, nil).Verify(encoded, "secret")
	if err != nil || !ok {
		t.Fatalf("Verify with different hasher params = %v, %v; want true, nil", ok, err)
	}
}

func TestVerifyMalformed(t *testing.T) {
	t.Parallel()

	const (
		salt = "ZGVtby1zZWVkLXNhbHQtMQ"
		key  = "sxSynfRZl9y9AigRtRVAVxaqp24lgJlVioaAAiofZSs"
	)

	tests := []struct {
		name    string
		encoded string
	}{
		{"empty", ""},
		{"garbage", "not a hash"},
		{"bcrypt", "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy"},
		{"argon2i", "$argon2i$v=19$m=1024,t=1,p=1$" + salt + "$" + key},
		{"argon2d", "$argon2d$v=19$m=1024,t=1,p=1$" + salt + "$" + key},
		{"version 16", "$argon2id$v=16$m=1024,t=1,p=1$" + salt + "$" + key},
		{"missing version", "$argon2id$m=1024,t=1,p=1$" + salt + "$" + key},
		{"missing key", "$argon2id$v=19$m=1024,t=1,p=1$" + salt},
		{"empty key", "$argon2id$v=19$m=1024,t=1,p=1$" + salt + "$"},
		{"extra field", "$argon2id$v=19$m=1024,t=1,p=1$" + salt + "$" + key + "$x"},
		{"no leading dollar", "argon2id$v=19$m=1024,t=1,p=1$" + salt + "$" + key},
		{"missing param", "$argon2id$v=19$m=1024,t=1$" + salt + "$" + key},
		{"params reordered", "$argon2id$v=19$t=1,m=1024,p=1$" + salt + "$" + key},
		{"param not a number", "$argon2id$v=19$m=lots,t=1,p=1$" + salt + "$" + key},
		{"negative param", "$argon2id$v=19$m=1024,t=-1,p=1$" + salt + "$" + key},
		{"param with sign", "$argon2id$v=19$m=+1024,t=1,p=1$" + salt + "$" + key},
		{"param overflow", "$argon2id$v=19$m=99999999999,t=1,p=1$" + salt + "$" + key},
		{"bad salt base64", "$argon2id$v=19$m=1024,t=1,p=1$!!!$" + key},
		{"padded salt", "$argon2id$v=19$m=1024,t=1,p=1$" + salt + "==$" + key},
		{"bad key base64", "$argon2id$v=19$m=1024,t=1,p=1$" + salt + "$***"},
		{"memory too large", "$argon2id$v=19$m=4194304,t=1,p=1$" + salt + "$" + key},
		{"memory too small", "$argon2id$v=19$m=7,t=1,p=1$" + salt + "$" + key},
		{"memory below 8 per thread", "$argon2id$v=19$m=16,t=1,p=4$" + salt + "$" + key},
		{"time zero", "$argon2id$v=19$m=1024,t=0,p=1$" + salt + "$" + key},
		{"time too large", "$argon2id$v=19$m=1024,t=1000,p=1$" + salt + "$" + key},
		{"threads zero", "$argon2id$v=19$m=1024,t=1,p=0$" + salt + "$" + key},
		{"threads too large", "$argon2id$v=19$m=1024,t=1,p=255$" + salt + "$" + key},
		{"salt too short", "$argon2id$v=19$m=1024,t=1,p=1$YWJj$" + key},
		{"key too short", "$argon2id$v=19$m=1024,t=1,p=1$" + salt + "$YWJj"},
		{"key too long", "$argon2id$v=19$m=1024,t=1,p=1$" + salt + "$" + base64.RawStdEncoding.EncodeToString(make([]byte, 1024))},
	}

	h := newHasher(t, cheap, nil)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ok, err := h.Verify(tt.encoded, "demo-password")
			if ok || !errors.Is(err, password.ErrMalformedHash) {
				t.Fatalf("Verify(%q) = %v, %v; want false, ErrMalformedHash", tt.encoded, ok, err)
			}

			if !h.NeedsRehash(tt.encoded) {
				t.Fatalf("NeedsRehash(%q) = false for a malformed hash", tt.encoded)
			}
		})
	}
}

func TestNeedsRehash(t *testing.T) {
	t.Parallel()

	h := newHasher(t, cheap, nil)

	current, err := h.Hash("secret")
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name   string
		params password.Params
		want   bool
	}{
		{"same params", cheap, false},
		{"memory", password.Params{Memory: 2048, Time: 1, Threads: 1, SaltLen: 16, KeyLen: 32}, true},
		{"time", password.Params{Memory: 1024, Time: 2, Threads: 1, SaltLen: 16, KeyLen: 32}, true},
		{"threads", password.Params{Memory: 1024, Time: 1, Threads: 2, SaltLen: 16, KeyLen: 32}, true},
		{"salt length", password.Params{Memory: 1024, Time: 1, Threads: 1, SaltLen: 8, KeyLen: 32}, true},
		{"key length", password.Params{Memory: 1024, Time: 1, Threads: 1, SaltLen: 16, KeyLen: 16}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			encoded, err := newHasher(t, tt.params, nil).Hash("secret")
			if err != nil {
				t.Fatal(err)
			}

			if got := h.NeedsRehash(encoded); got != tt.want {
				t.Fatalf("NeedsRehash(%q) = %v, want %v", encoded, got, tt.want)
			}
		})
	}

	if h.NeedsRehash(current) {
		t.Fatalf("NeedsRehash of own hash %q = true", current)
	}
}

func TestNewWithParamsRejectsInvalid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		params password.Params
	}{
		{"zero", password.Params{}},
		{"memory too large", password.Params{Memory: 1 << 30, Time: 1, Threads: 1, SaltLen: 16, KeyLen: 32}},
		{"time zero", password.Params{Memory: 1024, Time: 0, Threads: 1, SaltLen: 16, KeyLen: 32}},
		{"threads zero", password.Params{Memory: 1024, Time: 1, Threads: 0, SaltLen: 16, KeyLen: 32}},
		{"salt too short", password.Params{Memory: 1024, Time: 1, Threads: 1, SaltLen: 4, KeyLen: 32}},
		{"key too short", password.Params{Memory: 1024, Time: 1, Threads: 1, SaltLen: 16, KeyLen: 4}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if _, err := password.NewWithParams(tt.params, nil); err == nil {
				t.Fatalf("NewWithParams(%+v) succeeded", tt.params)
			}
		})
	}
}

func TestDefaultParamsAreValid(t *testing.T) {
	t.Parallel()

	if _, err := password.NewWithParams(password.DefaultParams(), nil); err != nil {
		t.Fatalf("DefaultParams() = %+v: %v", password.DefaultParams(), err)
	}

	if password.New().NeedsRehash(demoHash) == (password.DefaultParams() == cheap) {
		t.Fatal("NeedsRehash(demoHash) disagrees with whether the defaults are the demo parameters")
	}
}
