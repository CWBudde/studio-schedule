// Package seed provides fictional demo data for the demo server, the
// WebAssembly build and tests. It must never contain real names or data.
package seed

import (
	"context"
	"fmt"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// DemoSettings returns the settings of the fictional demo school.
func DemoSettings() domain.Settings {
	return domain.Settings{
		SchoolName:         "Studio Demo — Ballet & Dance",
		Timezone:           "Europe/Berlin",
		DefaultLocale:      "en",
		Currency:           "EUR",
		ReceiptPrefix:      "SD",
		ReleaseWeekday:     time.Sunday,
		ReleaseTime:        domain.TimeOfDay{Hour: 18},
		ReleaseWeeksAhead:  1,
		BookingCutoff:      0,
		CancellationCutoff: 2 * time.Hour,
		DefaultVisibleDays: 14,
	}
}

// LoadDemo writes the complete demo data set into st in one transaction.
func LoadDemo(ctx context.Context, st store.Store) error {
	err := st.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		return tx.Settings().Put(ctx, DemoSettings())
	})
	if err != nil {
		return fmt.Errorf("seed demo data: %w", err)
	}

	return nil
}
