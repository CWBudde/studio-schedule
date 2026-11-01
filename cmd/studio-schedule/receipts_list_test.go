package main

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// rlistTNumbers runs receipts list as the office with args and --output
// json, and returns the numbers of the receipts it lists, in its order.
func rlistTNumbers(t *testing.T, r cliRun, args ...string) []string {
	t.Helper()

	got := r.exec(t, append([]string{"receipts", "list", "--as", demoOffice, "--output", "json"}, args...)...)
	if got.code != 0 || got.stderr != "" {
		t.Fatalf("receipts list %v: exit %d, stderr %q", args, got.code, got.stderr)
	}

	var body struct {
		Items []struct {
			Number string `json:"number"`
		} `json:"items"`
		Total int `json:"total"`
	}
	if err := json.Unmarshal([]byte(got.stdout), &body); err != nil {
		t.Fatalf("receipts list %v: %v in %s", args, err, got.stdout)
	}

	numbers := []string{}
	for _, it := range body.Items {
		numbers = append(numbers, it.Number)
	}

	if body.Total != len(numbers) {
		t.Errorf("receipts list %v: total %d, %d items", args, body.Total, len(numbers))
	}

	return numbers
}

// rlistTOfLastYear stores SD-2025-0001, a copy of the demo's SD-2026-0003
// issued on 2025-12-30, with the counter of 2025 taken so that the year is
// one with receipts.
func rlistTOfLastYear(t *testing.T, r cliRun) {
	t.Helper()

	err := storeOf(t, r).InTx(t.Context(), store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		orig, err := tx.Receipts().GetByNumber(ctx, "SD-2026-0003")
		if err != nil {
			return err
		}

		seq, err := tx.ReceiptCounters().Next(ctx, 2025)
		if err != nil {
			return err
		}

		c := orig
		c.ID, c.Number, c.Year, c.Sequence = ids.New[domain.ReceiptID](idsOf(t)), "SD-2025-0001", 2025, seq
		c.IssueDate = domain.Date{Year: 2025, Month: 12, Day: 30}
		c.IssuedAt = time.Date(2025, 12, 30, 10, 0, 0, 0, time.UTC)

		return tx.Receipts().Create(ctx, c)
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestReceiptsList lists the demo's receipts: by default those of this
// year up to today (text and JSON), an explicit range that selects some,
// one that selects none; and, with a receipt of the year before, a range
// across the turn of the year.
func TestReceiptsList(t *testing.T) {
	t.Parallel()

	stores(t, func(t *testing.T, r cliRun) {
		got := r.exec(t, "receipts", "list", "--as", demoOffice)
		if got.code != 0 || got.stderr != "" {
			t.Fatalf("receipts list: exit %d, stderr %q", got.code, got.stderr)
		}

		golden(t, "receipts/list.txt", got.stdout)

		got = r.exec(t, "receipts", "list", "--as", demoOffice, "--output", "json")
		if got.code != 0 || got.stderr != "" {
			t.Fatalf("receipts list --output json: exit %d, stderr %q", got.code, got.stderr)
		}

		golden(t, "receipts/list.json", got.stdout)

		all := []string{"SD-2026-0005", "SD-2026-0004", "SD-2026-0003", "SD-2026-0002", "SD-2026-0001"}
		if n := rlistTNumbers(t, r); !slices.Equal(n, all) {
			t.Errorf("by default: %v, want %v", n, all)
		}

		// The demo issued them from 2026-09-02 to 2026-09-15.
		for _, c := range []struct {
			args []string
			want []string
		}{
			{[]string{"--from", "2026-09-03", "--to", "2026-09-12"}, []string{"SD-2026-0004", "SD-2026-0003", "SD-2026-0002"}},
			{[]string{"--from", "2026-09-08", "--to", "2026-09-08"}, []string{"SD-2026-0003"}},
			{[]string{"--from", "2026-09-15"}, []string{"SD-2026-0005"}},
			{[]string{"--to", "2026-09-02"}, []string{"SD-2026-0001"}},
			{[]string{"--from", "2026-01-01", "--to", "2026-08-31"}, []string{}},
			{[]string{"--to", "2025-12-31"}, []string{}},
		} {
			if n := rlistTNumbers(t, r, c.args...); !slices.Equal(n, c.want) {
				t.Errorf("receipts list %v: %v, want %v", c.args, n, c.want)
			}
		}

		got = r.exec(t, "receipts", "list", "--as", demoOffice, "--from", "2026-01-01", "--to", "2026-08-31")
		if got.code != 0 || got.stdout != "no receipts\n" {
			t.Errorf("an empty range: exit %d, stdout %q, want \"no receipts\"", got.code, got.stdout)
		}

		rlistTOfLastYear(t, r)

		if n := rlistTNumbers(t, r); !slices.Equal(n, all) {
			t.Errorf("by default, with a receipt of 2025: %v, want this year's %v", n, all)
		}

		if n, want := rlistTNumbers(t, r, "--from", "2025-12-01", "--to", "2026-09-02"),
			[]string{"SD-2026-0001", "SD-2025-0001"}; !slices.Equal(n, want) {
			t.Errorf("across the turn of the year: %v, want %v", n, want)
		}

		if n, want := rlistTNumbers(t, r, "--to", "2025-12-31"), []string{"SD-2025-0001"}; !slices.Equal(n, want) {
			t.Errorf("2025: %v, want %v", n, want)
		}

		if n := rlistTNumbers(t, r, "--from", "2025-12-31", "--to", "2025-12-31"); len(n) != 0 {
			t.Errorf("a day of 2025 without receipts: %v", n)
		}
	})
}

// TestReceiptsListRefusals: what the command refuses prints nothing on
// stdout: the account of --as (none, unknown, a role below office), a bad
// date, and a range that ends before it starts (named by --to).
func TestReceiptsListRefusals(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		args   []string
		code   int
		stderr string
	}{
		"no --as": {
			code:   2,
			stderr: "error: studio-schedule receipts list acts as an account: give --as <email> or set STUDIO_AS\n",
		},
		"unknown --as": {
			args: []string{"--as", "nobody@example.org"}, code: 3,
			stderr: "error: --as nobody@example.org: not found: no account has this email\n",
		},
		"--as a member": {
			args: []string{"--as", demoMember}, code: 4,
			stderr: "error: --as " + demoMember + ": forbidden: the account's role member is below office, " +
				"which this command needs\n",
		},
		"--as an instructor": {args: []string{"--as", demoInstructor}, code: 4},
		"bad --from": {
			args: []string{"--as", demoOffice, "--from", "2026-13-01"}, code: 2,
			stderr: "error: --from: \"2026-13-01\" is no date of the form YYYY-MM-DD\n",
		},
		"bad --to": {
			args: []string{"--as", demoOffice, "--to", "22.09.2026"}, code: 2,
			stderr: "error: --to: \"22.09.2026\" is no date of the form YYYY-MM-DD\n",
		},
		"from after to":    {args: []string{"--as", demoOffice, "--from", "2026-09-23", "--to", "2026-09-22"}, code: 2},
		"from after today": {args: []string{"--as", demoOffice, "--from", "2026-09-23"}, code: 2},
		"too many args":    {args: []string{"--as", demoOffice, "2026"}, code: 2},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := cliRun{st: demoStore(t)}.exec(t, append([]string{"receipts", "list"}, tc.args...)...)
			if got.code != tc.code || got.stdout != "" || (tc.stderr != "" && got.stderr != tc.stderr) {
				t.Errorf("receipts list %v: exit %d, stdout %q, stderr %q; want exit %d, stderr %q", tc.args, got.code,
					got.stdout, got.stderr, tc.code, tc.stderr)
			}

			if strings.HasPrefix(name, "from after") && !strings.HasPrefix(got.stderr, "error: --to: ") {
				t.Errorf("stderr %q, want the error named by --to", got.stderr)
			}
		})
	}
}

// TestReceiptsListDecimal formats amounts with the currency's minor
// digits: none (JPY), two (EUR, and an unknown currency), three (KWD), with
// leading zeros below one unit and the sign before them.
func TestReceiptsListDecimal(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		m        domain.Money
		currency string
		want     string
	}{
		{1800, "EUR", "18.00"},
		{-1800, "EUR", "-18.00"},
		{5, "EUR", "0.05"},
		{-5, "EUR", "-0.05"},
		{0, "EUR", "0.00"},
		{123456789, "EUR", "1234567.89"},
		{1500, "JPY", "1500"},
		{-1500, "JPY", "-1500"},
		{0, "JPY", "0"},
		{1500, "KWD", "1.500"},
		{5, "KWD", "0.005"},
		{-5, "KWD", "-0.005"},
		{1500, "ZZZ", "15.00"},
		{-9223372036854775808, "EUR", "-92233720368547758.08"},
	} {
		if got := receiptDecimal(c.m, c.currency); got != c.want {
			t.Errorf("receiptDecimal(%d, %s) = %q, want %q", c.m, c.currency, got, c.want)
		}
	}
}
