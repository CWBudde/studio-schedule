package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/cwbudde/studio-schedule/cmd/studio-schedule/cli"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/httpapi"
	"github.com/cwbudde/studio-schedule/internal/store"
)

const (
	demoIntensive = "contemporary-weekend-intensive"
	demoTaster    = "open-ballet-taster"
	demoDraft     = "summer-showcase-rehearsal"
)

func eventsRun(t *testing.T, r cliRun, args ...string) result {
	t.Helper()

	return r.exec(t, append([]string{"events", "--as", demoOffice}, args...)...)
}

func eventsOutput(t *testing.T, r cliRun, args ...string) string {
	t.Helper()

	got := eventsRun(t, r, args...)
	if got.code != cli.ExitOK || got.stderr != "" {
		t.Fatalf("events %v: exit %d, stderr %q", args, got.code, got.stderr)
	}

	return got.stdout
}

func TestEventsList(t *testing.T) {
	t.Parallel()

	stores(t, func(t *testing.T, r cliRun) {
		golden(t, "events/list.txt", eventsOutput(t, r, "list"))
		golden(t, "events/list.json", eventsOutput(t, r, "list", "--output", "json"))
	})
}

func TestEventsRegistrations(t *testing.T) {
	t.Parallel()

	stores(t, func(t *testing.T, r cliRun) {
		golden(t, "events/registrations.txt", eventsOutput(t, r, "registrations", demoIntensive))
		golden(t, "events/registrations.json", eventsOutput(t, r, "registrations", demoIntensive, "--output", "json"))
		golden(t, "events/registrations-guests.txt", eventsOutput(t, r, "registrations", demoTaster))

		if got := eventsOutput(t, r, "registrations", demoDraft, "--output", "json"); got != "{\n  \"items\": []\n}\n" {
			t.Errorf("draft registrations: %q, want empty items", got)
		}

		st := storeOf(t, r)
		passesTAnonymize(t, st, passesTPerson(t, st, "Kai Lanternby"))

		if got := eventsOutput(t, r, "registrations", demoIntensive); !strings.Contains(got, deletedPersonName) ||
			strings.Contains(got, "Kai Lanternby") {
			t.Errorf("anonymized participant: %q, want %q and no old name", got, deletedPersonName)
		}

		missing := eventsRun(t, r, "registrations", "unknown-event")
		if missing.code != cli.ExitNotFound || missing.stdout != "" || !strings.Contains(missing.stderr, "not found") {
			t.Errorf("unknown event: %+v, want exit %d and a not-found error", missing, cli.ExitNotFound)
		}
	})
}

func TestEventsMarkPaid(t *testing.T) {
	t.Parallel()

	stores(t, func(t *testing.T, r cliRun) {
		st := storeOf(t, r)
		r.pdf = renderingPDF
		payer := passesTPerson(t, st, "Ali Dunfield")
		reg := intensiveRegistration(t, st, "Kai Lanternby")
		args := []string{
			"mark-paid", reg.ID.String(), "--method", "transfer", "--payer", payer.String(),
			"--recipient-name", "Mira Example", "--recipient-email", "mira@example.org",
			"--recipient-address", "12 Example Lane",
		}

		for _, input := range []cliRun{r, pdWith(r, true, "yes\n")} {
			refused := eventsRun(t, input, args...)
			if refused.code != cli.ExitUsage || refused.stdout != "" || !strings.Contains(refused.stderr, "--yes") {
				t.Fatalf("payment without --yes (terminal=%v): %+v", input.terminal, refused)
			}
		}

		if got := intensiveRegistration(t, st, "Kai Lanternby"); got.Payment != domain.EventUnpaid || !got.ReceiptID.IsZero() {
			t.Fatalf("registration after refusal: %+v, want unpaid without receipt", got)
		}

		out := eventsOutput(t, r, append(args, "--output", "json", "--yes")...)
		golden(t, "events/mark-paid.json", out)

		var body httpapi.EventPayment
		if err := json.Unmarshal([]byte(out), &body); err != nil {
			t.Fatal(err)
		}

		if body.Registration.ID != reg.ID || body.Registration.Payment != string(domain.EventPaid) ||
			body.Receipt.ID.IsZero() || body.Registration.ReceiptID != body.Receipt.ID {
			t.Fatalf("payment body: %+v, want paid registration with its receipt", body)
		}

		got, err := servicesOf(t, st).Receipts.Get(t.Context(), body.Receipt.ID)
		if err != nil {
			t.Fatal(err)
		}

		receipt := got.Receipt
		if receipt.Number != body.Receipt.Number || receipt.Gross != reg.Price || receipt.PayerID != payer ||
			receipt.PaymentMethod != domain.PaymentTransfer || receipt.RecipientName != "Mira Example" ||
			receipt.RecipientEmail != "mira@example.org" || receipt.RecipientAddress != "12 Example Lane" ||
			got.EventID != reg.EventID {
			t.Errorf("stored receipt: %+v for event %v, want the reported event fee with the requested payer and recipient", receipt, got.EventID)
		}

		repeated := eventsRun(t, r, "mark-paid", reg.ID.String(), "--method", "cash", "--yes", "--output", "json")
		var problem struct {
			Status int
			Detail string
		}

		if err := json.Unmarshal([]byte(repeated.stderr), &problem); err != nil || repeated.code != cli.ExitConflict ||
			repeated.stdout != "" || problem.Status != 409 || !strings.Contains(problem.Detail, "paid already") {
			t.Errorf("already paid: %+v, want exit %d and a 409 problem", repeated, cli.ExitConflict)
		}

		other := intensiveRegistration(t, st, "Marta Fernwhistle")
		golden(t, "events/mark-paid.txt", eventsOutput(t, r, "mark-paid", other.ID.String(), "--method", "cash", "--yes"))
	})
}

func TestEventsRefusals(t *testing.T) {
	t.Parallel()

	const unknown = "01900000-0000-7000-8000-00000000abcd"

	stores(t, func(t *testing.T, r cliRun) {
		for _, tc := range []struct {
			name, message string
			args          []string
			code          int
		}{
			{"missing actor", "--as", []string{"list"}, cli.ExitUsage},
			{"member listing", "below office", []string{"--as", demoMember, "list"}, cli.ExitConflict},
			{"member registrations", "below office", []string{"--as", demoMember, "registrations", demoIntensive}, cli.ExitConflict},
			{"member payment", "below office", []string{"--as", demoMember, "mark-paid", unknown, "--method", "cash", "--yes"}, cli.ExitConflict},
			{"invalid registration ID", "registration ID", []string{"--as", demoOffice, "mark-paid", "invalid", "--method", "cash", "--yes"}, cli.ExitUsage},
			{"missing method", "method", []string{"--as", demoOffice, "mark-paid", unknown, "--yes"}, cli.ExitUsage},
			{"invalid method", "--method", []string{"--as", demoOffice, "mark-paid", unknown, "--method", "bitcoin", "--yes"}, cli.ExitUsage},
			{"invalid payer", "--payer", []string{"--as", demoOffice, "mark-paid", unknown, "--method", "cash", "--payer", "invalid", "--yes"}, cli.ExitUsage},
			{"unknown registration", "not found", []string{"--as", demoOffice, "mark-paid", unknown, "--method", "cash", "--yes"}, cli.ExitNotFound},
		} {
			t.Run(tc.name, func(t *testing.T) {
				got := r.exec(t, append([]string{"events"}, tc.args...)...)
				if got.code != tc.code || got.stdout != "" || !strings.Contains(got.stderr, tc.message) {
					t.Errorf("events %v: %+v, want exit %d and stderr containing %q", tc.args, got, tc.code, tc.message)
				}
			})
		}
	})
}

// intensiveRegistration returns the demo intensive's registration for name.
func intensiveRegistration(t *testing.T, st store.Store, name string) domain.EventRegistration {
	t.Helper()

	person := passesTPerson(t, st, name)
	var registration domain.EventRegistration

	err := st.InTx(t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		e, err := tx.Events().BySlug(ctx, demoIntensive)
		if err != nil {
			return err
		}

		regs, err := tx.EventRegistrations().ListByEvent(ctx, e.ID)
		for _, r := range regs {
			if r.PersonID == person {
				registration = r

				break
			}
		}

		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	if registration.ID.IsZero() {
		t.Fatalf("no intensive registration for %s", name)
	}

	return registration
}
