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
		LegalNotice:          "Contract School, Example Street 1, 00000 Example Town",
		WaitlistInviteExpiry: 90 * time.Minute,
		RedemptionMode:       domain.RedeemOnAttendance,
		Receipts: domain.ReceiptSettings{
			Issuer: domain.ReceiptIssuer{
				Name: "Contract School Ltd", Address: "Example Street 1\n00000 Example Town", TaxID: "12/345/67890",
				Email: "office@example.org", Phone: "+49 000 000000",
			},
			TaxMode: domain.TaxExempt, TaxNote: "Exempt under Example Law § 1", Footer: "Bank: Example Bank",
		},
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
			updated.WaitlistInviteExpiry = 30 * 24 * time.Hour
			updated.RedemptionMode = domain.RedeemOnBooking
			updated.Receipts = domain.ReceiptSettings{}
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
		"Logo": func(e *env) {
			logo := func() (domain.Logo, error) {
				return query(e, func(ctx context.Context, tx store.Tx) (domain.Logo, error) { return tx.Settings().Logo(ctx) })
			}
			putLogo := func(l domain.Logo) error {
				return e.write(func(ctx context.Context, tx store.Tx) error { return tx.Settings().PutLogo(ctx, l) })
			}
			png := domain.Logo{ContentType: domain.LogoPNG, Data: []byte("\x89PNG\r\n\x1a\nlogo")}

			_, err := logo()
			wantErr(e.t, err, domain.ErrNotFound)
			wantErr(e.t, putLogo(png), domain.ErrNotFound)

			put(e, want)

			got, err := logo()
			if err != nil || !got.IsZero() {
				e.t.Fatalf("no logo: got %+v, %v", got, err)
			}

			if err := putLogo(png); err != nil {
				e.t.Fatal(err)
			}

			got, err = logo()
			if err != nil || got.ContentType != png.ContentType || string(got.Data) != string(png.Data) {
				e.t.Fatalf("got %+v, %v", got, err)
			}

			// Settings writes keep the logo.
			put(e, want)

			if got, _ = logo(); got.IsZero() {
				e.t.Fatal("the logo is gone after a settings write")
			}

			err = e.read(func(ctx context.Context, tx store.Tx) error { return tx.Settings().PutLogo(ctx, domain.Logo{}) })
			wantErr(e.t, err, store.ErrReadOnly)

			if err := putLogo(domain.Logo{}); err != nil {
				e.t.Fatal(err)
			}

			if got, _ = logo(); !got.IsZero() {
				e.t.Fatalf("logo not removed: %+v", got)
			}
		},
		"ReadOnlyRejectsWrites": func(e *env) {
			err := e.read(func(ctx context.Context, tx store.Tx) error { return tx.Settings().Put(ctx, want) })
			wantErr(e.t, err, store.ErrReadOnly)

			_, err = get(e)
			wantErr(e.t, err, domain.ErrNotFound)
		},
	})
}
