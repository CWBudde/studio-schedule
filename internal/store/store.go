// Package store defines the persistence boundary of the application.
//
// All data access happens inside a transaction started with Store.InTx. The
// repositories obtained from a Tx are only valid while the callback runs.
//
// Rules for implementations and callers:
//
//   - InTx must never be nested: calling InTx from inside a transaction
//     callback deadlocks (memory) or escapes the transaction (Postgres).
//   - If the callback returns an error (or panics), every change made through
//     the Tx is rolled back and the error is returned unchanged.
//   - Writes inside a ReadOnly transaction fail with an error.
//   - Missing records are reported as domain.ErrNotFound (possibly wrapped).
//
// The in-memory implementation (package memory) serialises all transactions
// with a single mutex and restores a snapshot on error. A PostgreSQL
// implementation follows in Phase 1; both are verified by the shared contract
// tests in package storetest.
package store

import (
	"context"
	"errors"

	"github.com/cwbudde/studio-schedule/internal/domain"
)

// ErrReadOnly is returned by write operations inside a ReadOnly transaction.
var ErrReadOnly = errors.New("store: write in read-only transaction")

// TxMode selects the kind of transaction started by Store.InTx.
type TxMode int

const (
	// ReadOnly transactions may only read data.
	ReadOnly TxMode = iota
	// ReadWrite transactions may read and write data.
	ReadWrite
)

// String implements fmt.Stringer.
func (m TxMode) String() string {
	switch m {
	case ReadOnly:
		return "read-only"
	case ReadWrite:
		return "read-write"
	default:
		return "unknown"
	}
}

// Store is the entry point to persistence.
type Store interface {
	// InTx runs fn inside a transaction. The transaction commits if fn
	// returns nil and rolls back otherwise. See the package documentation
	// for the full contract.
	InTx(ctx context.Context, mode TxMode, fn func(ctx context.Context, tx Tx) error) error
}

// Tx gives access to the repositories bound to one transaction.
type Tx interface {
	Settings() SettingsRepo
}

// SettingsRepo stores the single, school-wide settings record.
type SettingsRepo interface {
	// Get returns the settings or domain.ErrNotFound if none were stored.
	Get(ctx context.Context) (domain.Settings, error)
	// Put creates or replaces the settings.
	Put(ctx context.Context, s domain.Settings) error
}
