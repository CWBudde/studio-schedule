// Package ids generates entity IDs so that ID-dependent code can be tested
// deterministically. It is the only package allowed to create UUIDs.
package ids

import (
	"encoding/binary"
	"sync"
	"uuid"

	"github.com/cwbudde/studio-schedule/internal/domain"
)

// Source generates new UUIDs.
type Source interface {
	NewUUID() uuid.UUID
}

// New returns a fresh ID of type I from src.
func New[I domain.AnyID](src Source) I {
	return I(src.NewUUID())
}

// V7 is a Source of random, time-ordered UUIDv7s, for production use.
type V7 struct{}

// NewUUID returns a new UUIDv7.
func (V7) NewUUID() uuid.UUID { return uuid.NewV7() }

// Sequence is a deterministic Source for tests and seed data. It returns
// UUIDs with version 7 and the RFC 9562 variant set, a zero timestamp and
// a counter starting at 1 in the lowest bits, so they sort in creation
// order. It is safe for concurrent use.
type Sequence struct {
	mu sync.Mutex
	n  uint64
}

// NewSequence returns a Sequence starting at 1.
func NewSequence() *Sequence {
	return &Sequence{}
}

// NewUUID returns the next UUID of the sequence.
func (s *Sequence) NewUUID() uuid.UUID {
	s.mu.Lock()
	s.n++
	n := s.n
	s.mu.Unlock()

	var u uuid.UUID

	// Version 7 in the high nibble of byte 6, the RFC 9562 variant (10xx)
	// in the top bits of byte 8, the counter below it.
	u[6] = 0x70
	binary.BigEndian.PutUint64(u[8:], n|1<<63)

	return u
}
