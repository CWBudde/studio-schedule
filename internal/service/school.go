package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// ErrNoSchool reports that no school settings exist yet (e.g. a new
// database before `studio-schedule seed`). It wraps domain.ErrNotFound.
var ErrNoSchool = fmt.Errorf("%w: no school settings", domain.ErrNotFound)

// loadSchool returns the settings and the time zone of the school. It is
// the one place that resolves the school time zone: school dates and wall
// clock times are interpreted in it, never in time.Local.
func loadSchool(ctx context.Context, tx store.Tx) (domain.Settings, *time.Location, error) {
	s, err := tx.Settings().Get(ctx)

	switch {
	case errors.Is(err, domain.ErrNotFound):
		return domain.Settings{}, nil, ErrNoSchool
	case err != nil:
		return domain.Settings{}, nil, fmt.Errorf("school settings: %w", err)
	}

	loc, err := time.LoadLocation(s.Timezone)
	if err != nil {
		// Settings.Validate rejects unknown zones, so the data is broken.
		return domain.Settings{}, nil, fmt.Errorf("school time zone %q: %w", s.Timezone, err)
	}

	return s, loc, nil
}
