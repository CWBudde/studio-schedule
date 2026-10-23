// Package memory provides an in-memory store.Store, used for tests, the demo
// server and the WebAssembly build.
//
// All transactions are serialised by a single mutex, so there is no
// concurrency inside the store. Before each transaction the state is
// snapshotted; if the callback fails or panics the snapshot is restored.
// Nested InTx calls deadlock and are forbidden by the store contract.
package memory

import (
	"context"
	"fmt"
	"sync"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// Store is an in-memory implementation of store.Store. The zero value is not
// usable; create one with New.
type Store struct {
	mu    sync.Mutex
	state state
}

// state is the complete data set. clone must deep-copy every field that is a
// pointer, slice or map so that a snapshot is unaffected by later writes.
type state struct {
	settings *domain.Settings
}

func (s state) clone() state {
	c := state{}

	if s.settings != nil {
		v := *s.settings
		c.settings = &v
	}

	return c
}

var _ store.Store = (*Store)(nil)

// New returns an empty store.
func New() *Store {
	return &Store{}
}

// InTx implements store.Store.
func (s *Store) InTx(ctx context.Context, mode store.TxMode, fn func(ctx context.Context, tx store.Tx) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	snapshot := s.state.clone()
	committed := false

	defer func() {
		if !committed {
			s.state = snapshot
		}
	}()

	if err := fn(ctx, &tx{state: &s.state, mode: mode}); err != nil {
		return err
	}

	committed = true

	return nil
}

type tx struct {
	state *state
	mode  store.TxMode
}

func (t *tx) Settings() store.SettingsRepo { return settingsRepo{t} }

func (t *tx) checkWritable() error {
	if t.mode != store.ReadWrite {
		return store.ErrReadOnly
	}

	return nil
}

type settingsRepo struct{ tx *tx }

func (r settingsRepo) Get(context.Context) (domain.Settings, error) {
	if r.tx.state.settings == nil {
		return domain.Settings{}, fmt.Errorf("settings: %w", domain.ErrNotFound)
	}

	return *r.tx.state.settings, nil
}

func (r settingsRepo) Put(_ context.Context, s domain.Settings) error {
	if err := r.tx.checkWritable(); err != nil {
		return err
	}

	r.tx.state.settings = &s

	return nil
}
