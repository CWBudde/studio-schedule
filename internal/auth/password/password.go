// Package password hashes and verifies passwords with argon2id.
//
// Hashes use the PHC string format, as the reference argon2 CLI prints it:
//
//	$argon2id$v=19$m=<memory KiB>,t=<passes>,p=<lanes>$<salt>$<key>
//
// where salt and key are standard base64 without padding. Each hash carries
// its own parameters, so Verify does not depend on the hasher's settings, and
// NeedsRehash tells when a stored hash should be replaced after a successful
// login.
//
// The default parameters depend on the build target: the server uses the
// OWASP recommendation (params.go), the WASM demo uses cheap ones because it
// runs single-threaded in the browser (params_js.go).
package password

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"math"
	"runtime"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

// ErrMalformedHash reports a stored hash that is not a supported argon2id PHC
// string, or whose parameters are outside the accepted bounds.
var ErrMalformedHash = errors.New("malformed password hash")

// Bounds for parameters, both configured and read from stored hashes. The
// upper bounds stop a tampered hash from making a single login allocate
// gigabytes or run for minutes.
const (
	maxMemory  = 128 * 1024 // KiB
	maxTime    = 10
	maxThreads = 16
	minSaltLen = 8
	maxSaltLen = 64
	minKeyLen  = 16
	maxKeyLen  = 64
)

// Params are argon2id cost and size parameters.
type Params struct {
	Memory  uint32 // KiB
	Time    uint32 // passes over the memory
	Threads uint8  // lanes
	SaltLen uint32 // bytes
	KeyLen  uint32 // bytes
}

// DefaultParams returns the parameters New uses on this build target.
func DefaultParams() Params { return defaultParams }

func (p Params) validate() error {
	switch {
	case p.Threads < 1 || p.Threads > maxThreads:
		return fmt.Errorf("threads %d not in [1, %d]", p.Threads, maxThreads)
	case p.Memory < 8*uint32(p.Threads) || p.Memory > maxMemory:
		return fmt.Errorf("memory %d KiB not in [%d, %d]", p.Memory, 8*uint32(p.Threads), maxMemory)
	case p.Time < 1 || p.Time > maxTime:
		return fmt.Errorf("time %d not in [1, %d]", p.Time, maxTime)
	case p.SaltLen < minSaltLen || p.SaltLen > maxSaltLen:
		return fmt.Errorf("salt length %d not in [%d, %d]", p.SaltLen, minSaltLen, maxSaltLen)
	case p.KeyLen < minKeyLen || p.KeyLen > maxKeyLen:
		return fmt.Errorf("key length %d not in [%d, %d]", p.KeyLen, minKeyLen, maxKeyLen)
	}

	return nil
}

// slots bounds how many hashes are computed at once in the process. Each
// needs Params.Memory of RAM, so without a bound a burst of logins or
// registrations could exhaust memory; requests beyond it wait.
var slots = make(chan struct{}, max(runtime.GOMAXPROCS(0), 1))

// idKey is argon2.IDKey under the process-wide concurrency bound.
func idKey(password, salt []byte, p Params) []byte {
	slots <- struct{}{}
	defer func() { <-slots }()

	return argon2.IDKey(password, salt, p.Time, p.Memory, p.Threads, p.KeyLen)
}

// Argon2id hashes passwords with argon2id. It is safe for concurrent use if
// its salt source is.
type Argon2id struct {
	params Params
	salt   io.Reader
}

// New returns a hasher with DefaultParams and crypto/rand salts.
func New() *Argon2id {
	return &Argon2id{params: defaultParams, salt: rand.Reader}
}

// NewWithParams returns a hasher with explicit parameters that reads salts
// from salt, or from crypto/rand if salt is nil. Tests and seeds use it.
func NewWithParams(p Params, salt io.Reader) (*Argon2id, error) {
	if err := p.validate(); err != nil {
		return nil, fmt.Errorf("argon2id parameters: %w", err)
	}

	if salt == nil {
		salt = rand.Reader
	}

	return &Argon2id{params: p, salt: salt}, nil
}

// Hash returns the PHC string of password with a fresh salt.
func (h *Argon2id) Hash(password string) (string, error) {
	salt := make([]byte, h.params.SaltLen)
	if _, err := io.ReadFull(h.salt, salt); err != nil {
		return "", fmt.Errorf("read salt: %w", err)
	}

	key := idKey([]byte(password), salt, h.params)

	return encode(h.params, salt, key), nil
}

// Verify reports whether password matches encoded, using the parameters
// stored in encoded. A wrong password is (false, nil); an error, wrapping
// ErrMalformedHash, means encoded could not be parsed.
func (h *Argon2id) Verify(encoded, password string) (bool, error) {
	p, salt, key, err := decode(encoded)
	if err != nil {
		return false, err
	}

	got := idKey([]byte(password), salt, p)

	return subtle.ConstantTimeCompare(got, key) == 1, nil
}

// NeedsRehash reports whether encoded was made with parameters other than the
// hasher's, or cannot be parsed at all.
func (h *Argon2id) NeedsRehash(encoded string) bool {
	p, _, _, err := decode(encoded)

	return err != nil || p != h.params
}

func encode(p Params, salt, key []byte) string {
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, p.Memory, p.Time, p.Threads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key))
}

// decode parses a PHC string strictly: fixed field and parameter order,
// unsigned decimal numbers, unpadded base64, parameters within bounds.
func decode(encoded string) (Params, []byte, []byte, error) {
	fail := func(reason string) (Params, []byte, []byte, error) {
		return Params{}, nil, nil, fmt.Errorf("%w: %s", ErrMalformedHash, reason)
	}

	// "", "argon2id", "v=19", "m=..,t=..,p=..", salt, key
	fields := strings.Split(encoded, "$")
	if len(fields) != 6 || fields[0] != "" {
		return fail("not a PHC string")
	}

	if fields[1] != "argon2id" {
		return fail("unsupported algorithm")
	}

	if fields[2] != "v="+strconv.Itoa(argon2.Version) {
		return fail("unsupported version")
	}

	m, t, threads, ok := parseParams(fields[3])
	if !ok {
		return fail("invalid parameters")
	}

	enc := base64.RawStdEncoding.Strict()

	salt, err := enc.DecodeString(fields[4])
	if err != nil {
		return fail("invalid salt encoding")
	}

	key, err := enc.DecodeString(fields[5])
	if err != nil {
		return fail("invalid key encoding")
	}

	saltLen, keyLen := len(salt), len(key)
	if saltLen > maxSaltLen || keyLen > maxKeyLen {
		return fail("salt or key too long")
	}

	p := Params{
		Memory: m, Time: t, Threads: threads,
		SaltLen: uint32(saltLen), KeyLen: uint32(keyLen),
	}
	if err := p.validate(); err != nil {
		return fail(err.Error())
	}

	return p, salt, key, nil
}

// parseParams parses "m=<n>,t=<n>,p=<n>" in exactly that order.
func parseParams(s string) (memory, passes uint32, threads uint8, ok bool) {
	parts := strings.Split(s, ",")
	if len(parts) != 3 {
		return 0, 0, 0, false
	}

	m, okM := parseUint(parts[0], "m=", 32)
	t, okT := parseUint(parts[1], "t=", 32)
	p, okP := parseUint(parts[2], "p=", 8)

	// ParseUint already enforces the bit sizes; the explicit range checks
	// make the conversions below provably safe for gosec.
	if !okM || !okT || !okP || m > math.MaxUint32 || t > math.MaxUint32 || p > math.MaxUint8 {
		return 0, 0, 0, false
	}

	return uint32(m), uint32(t), uint8(p), true
}

// parseUint parses "<name><decimal digits>" into a value of at most bits bits.
func parseUint(s, name string, bits int) (uint64, bool) {
	digits, found := strings.CutPrefix(s, name)
	if !found || digits == "" || strings.TrimLeft(digits, "0123456789") != "" {
		return 0, false
	}

	v, err := strconv.ParseUint(digits, 10, bits)

	return v, err == nil
}
