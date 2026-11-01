package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// passesTPerson returns the ID of the demo person with the full name in st.
func passesTPerson(t *testing.T, st store.Store, name string) domain.PersonID {
	t.Helper()

	var id domain.PersonID

	err := st.InTx(t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		persons, err := tx.Persons().List(ctx)
		for _, p := range persons {
			if p.GivenName+" "+p.FamilyName == name {
				id = p.ID
			}
		}

		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	if id.IsZero() {
		t.Fatalf("no person %s in the demo data", name)
	}

	return id
}

// passesTAnonymize anonymizes the person in st, as PersonService.Anonymize
// leaves it.
func passesTAnonymize(t *testing.T, st store.Store, id domain.PersonID) {
	t.Helper()

	err := st.InTx(t.Context(), store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		p, err := tx.Persons().Get(ctx, id)
		if err != nil {
			return err
		}

		return tx.Persons().Update(ctx, p.Anonymize(testNow))
	})
	if err != nil {
		t.Fatal(err)
	}
}

// passesTList runs passes list for the person as the office and fails the
// test unless it succeeds.
func passesTList(t *testing.T, r cliRun, person domain.PersonID, extra ...string) string {
	t.Helper()

	got := r.exec(t, append([]string{"passes", "list", "--as", demoOffice, "--person", person.String()}, extra...)...)
	if got.code != 0 || got.stderr != "" {
		t.Fatalf("passes list --person %s %v: exit %d, stderr %q", person, extra, got.code, got.stderr)
	}

	return got.stdout
}

// TestPassesList lists the passes of the demo's persons: Clara's own
// 10-visit pass (text and JSON), her daughter's that she paid for, a time
// pass, an assigned pass, a voided one, and a person without passes; then
// Clara is anonymized, and her daughter's pass shows its payer as
// "Deleted person". An unknown person exits with status 3.
func TestPassesList(t *testing.T) {
	t.Parallel()

	stores(t, func(t *testing.T, r cliRun) {
		st := storeOf(t, r)
		clara := passesTPerson(t, st, "Clara Amberly")
		ella := passesTPerson(t, st, "Ella Amberly")

		golden(t, "passes/clara.txt", passesTList(t, r, clara))

		// On a terminal, the table has its header line.
		tr := r.with("NO_COLOR", "1")
		tr.terminal = true
		golden(t, "passes/clara-terminal.txt", passesTList(t, tr, clara))

		js := passesTList(t, r, clara, "--output", "json")
		golden(t, "passes/clara.json", js)

		var body struct {
			Items []struct {
				Kind            string `json:"kind"`
				VisitsTotal     int    `json:"visitsTotal"`
				VisitsRemaining int    `json:"visitsRemaining"`
				Payer           *struct {
					Name string `json:"name"`
				} `json:"payer"`
			} `json:"items"`
		}
		if err := json.Unmarshal([]byte(js), &body); err != nil || len(body.Items) != 1 ||
			body.Items[0].VisitsTotal != 10 || body.Items[0].VisitsRemaining != 8 || body.Items[0].Payer != nil {
			t.Errorf("Clara's passes as JSON: %+v, %v; want one 10-visit pass with 8 left, paid by herself", body, err)
		}

		golden(t, "passes/ella.txt", passesTList(t, r, ella))
		golden(t, "passes/kai.txt", passesTList(t, r, passesTPerson(t, st, "Kai Lanternby")))
		golden(t, "passes/irene.txt", passesTList(t, r, passesTPerson(t, st, "Irene Inkwell")))
		golden(t, "passes/vera.txt", passesTList(t, r, passesTPerson(t, st, "Vera Rushwick")))

		if got := passesTList(t, r, passesTPerson(t, st, "Olga Ledgerby")); got != "no passes\n" {
			t.Errorf("a person without passes: %q, want \"no passes\"", got)
		}

		if got := passesTList(t, r, passesTPerson(t, st, "Olga Ledgerby"), "--output", "json"); got != "{\n  \"items\": []\n}\n" {
			t.Errorf("a person without passes as JSON: %q, want an empty list", got)
		}

		got := r.exec(t, "passes", "list", "--as", demoOffice, "--person", "01900000-0000-7000-8000-00000000abcd")
		if got.code != 3 || got.stdout != "" || !strings.Contains(got.stderr, "not found") {
			t.Errorf("an unknown person: exit %d, stdout %q, stderr %q; want exit 3", got.code, got.stdout, got.stderr)
		}

		passesTAnonymize(t, st, clara)

		if got := passesTList(t, r, ella); !strings.Contains(got, "Deleted person") || strings.Contains(got, "Clara") {
			t.Errorf("Ella's pass after her payer was anonymized:\n%s\nwant the payer as Deleted person", got)
		}
	})
}

// TestPassesListRefusals: what the command refuses prints nothing on
// stdout: the account of --as (none, unknown, a role below office), and a
// missing or invalid --person.
func TestPassesListRefusals(t *testing.T) {
	t.Parallel()

	clara := "00000000-0000-7000-8000-000000000001"

	for name, tc := range map[string]struct {
		args   []string
		code   int
		stderr string
	}{
		"no --as": {
			args: []string{"--person", clara}, code: 2,
			stderr: "error: studio-schedule passes list acts as an account: give --as <email> or set STUDIO_AS\n",
		},
		"unknown --as": {
			args: []string{"--as", "nobody@example.org", "--person", clara}, code: 3,
			stderr: "error: --as nobody@example.org: not found: no account has this email\n",
		},
		"--as a member": {
			args: []string{"--as", demoMember, "--person", clara}, code: 4,
			stderr: "error: --as " + demoMember + ": forbidden: the account's role member is below office, " +
				"which this command needs\n",
		},
		"--as an instructor": {args: []string{"--as", demoInstructor, "--person", clara}, code: 4},
		"no --person":        {args: []string{"--as", demoOffice}, code: 2},
		"invalid --person": {
			args: []string{"--as", demoOffice, "--person", "clara"}, code: 2,
			stderr: "error: --person: \"clara\" is no person ID\n",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := cliRun{st: demoStore(t)}.exec(t, append([]string{"passes", "list"}, tc.args...)...)
			if got.code != tc.code || got.stdout != "" || (tc.stderr != "" && got.stderr != tc.stderr) {
				t.Errorf("passes list %v: exit %d, stdout %q, stderr %q; want exit %d, stderr %q", tc.args, got.code,
					got.stdout, got.stderr, tc.code, tc.stderr)
			}
		})
	}
}
