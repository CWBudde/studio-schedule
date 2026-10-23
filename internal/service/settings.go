// Package service holds the application use cases. Services run their work
// inside store transactions and return domain errors; they know nothing
// about HTTP.
package service

import (
	"context"
	"fmt"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// SettingsService provides access to the school settings.
type SettingsService struct {
	store store.Store
}

// NewSettingsService returns a SettingsService backed by st.
func NewSettingsService(st store.Store) *SettingsService {
	return &SettingsService{store: st}
}

// Public returns the settings that may be shown to anonymous visitors.
func (s *SettingsService) Public(ctx context.Context) (domain.Settings, error) {
	var settings domain.Settings

	err := s.store.InTx(ctx, store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		var err error
		settings, err = tx.Settings().Get(ctx)

		return err
	})
	if err != nil {
		return domain.Settings{}, fmt.Errorf("public settings: %w", err)
	}

	return settings, nil
}
