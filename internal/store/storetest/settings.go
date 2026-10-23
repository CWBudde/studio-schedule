package storetest

import (
	"context"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/store"
)

func testSettings(t *testing.T, newStore func(*testing.T) store.Store) {
	t.Helper()

	want := domain.Settings{
		SchoolName: "Contract School", Timezone: "Europe/Berlin", DefaultLocale: "en",
		Currency: "EUR", ReceiptPrefix: "CS1",
		ReleaseWeekday: time.Saturday, ReleaseTime: domain.TimeOfDay{Hour: 9, Minute: 30}, ReleaseWeeksAhead: 2,
		BookingCutoff: 30 * time.Minute, CancellationCutoff: 12 * time.Hour, DefaultVisibleDays: 21,
		LegalNotice: "Contract School, Example Street 1, 00000 Example Town",
	}
	get := func(e *env) (domain.Settings, error) {
		return query(e, func(ctx context.Context, tx store.Tx) (domain.Settings, error) { return tx.Settings().Get(ctx) })
	}
	put := func(e *env, s domain.Settings) {
		e.t.Helper()
		e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Settings().Put(ctx, s) })
	}

	cases(t, newStore, map[string]func(e *env){
		"GetEmptyIsNotFound": func(e *env) {
			_, err := get(e)
			wantErr(e.t, err, domain.ErrNotFound)
		},
		"PutThenGet": func(e *env) {
			valid(e.t, want)
			put(e, want)

			got, err := get(e)
			if err != nil {
				e.t.Fatalf("Get: %v", err)
			}

			wantEqual(e.t, got, want)
		},
		"PutReplacesAndClearsOptionalFields": func(e *env) {
			put(e, want)

			updated := want
			updated.SchoolName = "Renamed School"
			updated.BookingCutoff = 0
			updated.LegalNotice = ""
			put(e, updated)

			got, err := get(e)
			if err != nil {
				e.t.Fatalf("Get: %v", err)
			}

			wantEqual(e.t, got, updated)
		},
		"RollbackOnError": func(e *env) {
			put(e, want)

			err := e.write(func(ctx context.Context, tx store.Tx) error {
				changed := want
				changed.SchoolName = "Must Not Persist"

				if err := tx.Settings().Put(ctx, changed); err != nil {
					return err
				}

				return errBoom
			})
			wantErr(e.t, err, errBoom)

			got, err := get(e)
			if err != nil {
				e.t.Fatalf("Get: %v", err)
			}

			wantEqual(e.t, got, want)
		},
		"ReadOnlyRejectsWrites": func(e *env) {
			err := e.read(func(ctx context.Context, tx store.Tx) error { return tx.Settings().Put(ctx, want) })
			wantErr(e.t, err, store.ErrReadOnly)

			_, err = get(e)
			wantErr(e.t, err, domain.ErrNotFound)
		},
	})
}
