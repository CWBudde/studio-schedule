package main

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// rverifyTKit writes receipts into a store for the receipts tests, built
// from the demo's sale SD-2026-0003 (its issuer, payer and tax data), so
// they satisfy PostgreSQL's constraints wherever a test does not break
// one on purpose.
type rverifyTKit struct {
	ctx context.Context
	tx  store.Tx
	tpl domain.Receipt
	src ids.Source
}

// rverifyTWrite runs fn with a kit in one write transaction on the store
// of r; the new IDs come from the run's ID source, so they never collide
// with the demo data's or the commands'.
func rverifyTWrite(t *testing.T, r cliRun, fn func(k rverifyTKit) error) {
	t.Helper()

	err := storeOf(t, r).InTx(t.Context(), store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		tpl, err := tx.Receipts().GetByNumber(ctx, "SD-2026-0003")
		if err != nil {
			return err
		}

		if tpl.Kind != domain.ReceiptSale || tpl.Currency != "EUR" || tpl.TaxMode != domain.TaxStandard {
			return fmt.Errorf("the template SD-2026-0003 is %+v, want a sale in EUR with standard VAT", tpl)
		}

		return fn(rverifyTKit{ctx: ctx, tx: tx, tpl: tpl, src: idsOf(t)})
	})
	if err != nil {
		t.Fatal(err)
	}
}

// rverifyTLine is a line of a fictional pass at 19 % VAT.
func rverifyTLine(position int, description string, unitPrice domain.Money) domain.ReceiptLine {
	return domain.NewReceiptLine(position, description, 1, unitPrice, 1900, domain.TaxStandard)
}

// get returns the receipt with the number.
func (k rverifyTKit) get(number string) (domain.Receipt, error) {
	return k.tx.Receipts().GetByNumber(k.ctx, number)
}

// sale returns a sale with the number, the sequence and the lines, its
// totals their sums, issued like the template.
func (k rverifyTKit) sale(number string, seq int, lines ...domain.ReceiptLine) domain.Receipt {
	r := k.tpl
	r.ID = ids.New[domain.ReceiptID](k.src)
	r.Number, r.Sequence, r.Lines = number, seq, lines
	r.Net, r.VAT, r.Gross = 0, 0, 0

	for _, l := range lines {
		r.Net, r.VAT, r.Gross = r.Net+l.Net, r.VAT+l.VAT, r.Gross+l.Gross
	}

	return r
}

// storno returns the cancellation of orig with the number and sequence:
// its lines negated.
func (k rverifyTKit) storno(number string, seq int, orig domain.Receipt) domain.Receipt {
	lines := make([]domain.ReceiptLine, len(orig.Lines))
	for i, l := range orig.Lines {
		lines[i] = l.Negated()
	}

	r := k.sale(number, seq, lines...)
	r.Kind, r.CancelsReceiptID, r.Reason = domain.ReceiptCancellation, orig.ID, "Fictional test cancellation"

	return r
}

// create stores the receipts.
func (k rverifyTKit) create(receipts ...domain.Receipt) error {
	for _, r := range receipts {
		if err := k.tx.Receipts().Create(k.ctx, r); err != nil {
			return fmt.Errorf("create %s: %w", r.Number, err)
		}
	}

	return nil
}

// rverifyTInYear returns r issued on the day, in its year.
func rverifyTInYear(r domain.Receipt, day domain.Date) domain.Receipt {
	r.Year, r.IssueDate = day.Year, day
	r.IssuedAt = time.Date(day.Year, day.Month, day.Day, 10, 0, 0, 0, time.UTC)

	return r
}

// rverifyTBreak writes the broken receipts of the findings golden files: a
// storno of the storno SD-2026-0002 (6), a number that does not end in its
// sequence (7), a storno of SD-2026-0003 that does not negate it after the
// missing 8 (9), and totals that are not the sum of the lines after the
// missing 10 and 11 (12).
func rverifyTBreak(k rverifyTKit) error {
	storno, err := k.get("SD-2026-0002")
	if err != nil {
		return err
	}

	sale, err := k.get("SD-2026-0003")
	if err != nil {
		return err
	}

	wrong := k.storno("SD-2026-0009", 9, sale)
	wrong.Lines = []domain.ReceiptLine{rverifyTLine(1, "Fictional pass", -1000)}
	wrong.Net, wrong.VAT, wrong.Gross = -840, -160, -1000

	totals := k.sale("SD-2026-0012", 12, rverifyTLine(1, "Fictional pass", 1000))
	totals.Net, totals.VAT, totals.Gross = 1008, 192, 1200

	return k.create(
		k.storno("SD-2026-0006", 6, storno),
		k.sale("SD-2026-0099", 7, rverifyTLine(1, "Fictional pass", 1500)),
		wrong,
		totals,
	)
}

// rverifyTReport parses the JSON report of a run.
func rverifyTReport(t *testing.T, got result) receiptsVerifyReport {
	t.Helper()

	var report receiptsVerifyReport
	if err := json.Unmarshal([]byte(got.stdout), &report); err != nil {
		t.Fatalf("JSON report: %v\n%s", err, got.stdout)
	}

	return report
}

// TestReceiptsVerifyDemo: the demo's receipts have no findings, in text
// and JSON, on both stores; --year 2026 checks the same.
func TestReceiptsVerifyDemo(t *testing.T) {
	t.Parallel()

	stores(t, func(t *testing.T, r cliRun) {
		got := r.exec(t, "receipts", "verify")
		if got.code != 0 || got.stderr != "" {
			t.Fatalf("exit %d, stderr %q", got.code, got.stderr)
		}

		golden(t, "receipts/verify-clean.txt", got.stdout)

		got = r.exec(t, "receipts", "verify", "--output", "json")
		if got.code != 0 || got.stderr != "" {
			t.Fatalf("JSON: exit %d, stderr %q", got.code, got.stderr)
		}

		golden(t, "receipts/verify-clean.json", got.stdout)

		want := receiptsVerifyReport{OK: true, Years: []int{2026}, Receipts: 5, Findings: []receiptsVerifyFinding{}}
		if report := rverifyTReport(t, got); !rverifyTSame(report, want) {
			t.Errorf("report %+v, want %+v", report, want)
		}

		got = r.exec(t, "receipts", "verify", "--year", "2026")
		if got.code != 0 || got.stdout != "5 receipts of 2026 checked: no findings\n" {
			t.Errorf("--year 2026: exit %d, stdout %q", got.code, got.stdout)
		}
	})
}

// TestReceiptsVerifyFindings: a database with a storno of a storno, a
// number that does not end in its sequence, a storno that does not negate
// its original, totals that are not the sum of the lines and gaps of one
// and of two numbers reports each, in text and JSON, and exits with
// status 1; on both stores the same.
func TestReceiptsVerifyFindings(t *testing.T) {
	t.Parallel()

	stores(t, func(t *testing.T, r cliRun) {
		rverifyTWrite(t, r, rverifyTBreak)

		got := r.exec(t, "receipts", "verify")
		if got.code != 1 || got.stderr != "error: the receipts have findings: 6\n" {
			t.Fatalf("exit %d, stderr %q", got.code, got.stderr)
		}

		golden(t, "receipts/verify-findings.txt", got.stdout)

		got = r.exec(t, "receipts", "verify", "--output", "json")
		if got.code != 1 || got.stderr != "" {
			t.Fatalf("JSON: exit %d, stderr %q; want 1 and no problem document", got.code, got.stderr)
		}

		golden(t, "receipts/verify-findings.json", got.stdout)

		report := rverifyTReport(t, got)
		if report.OK || report.Receipts != 9 || !slices.Equal(report.Years, []int{2026}) || len(report.Findings) != 6 {
			t.Errorf("report %+v, want 6 findings in 9 receipts of 2026", report)
		}
	})
}

// rverifyTSame reports whether two reports are equal.
func rverifyTSame(a, b receiptsVerifyReport) bool {
	return a.OK == b.OK && a.Receipts == b.Receipts && slices.Equal(a.Years, b.Years) &&
		slices.Equal(a.Findings, b.Findings)
}

// TestReceiptsVerifyCases builds each broken database on both stores and
// checks the report: the findings (JSON), the summary line (text) and the
// exit status.
func TestReceiptsVerifyCases(t *testing.T) {
	t.Parallel()

	const desc = "Fictional pass"

	for name, tc := range map[string]struct {
		write func(t *testing.T, k rverifyTKit) error
		args  []string
		// want is the JSON report; findings is its Findings, else none.
		want receiptsVerifyReport
		// summary is the last line of the text report.
		summary string
	}{
		"one and several numbers missing": {
			write: func(_ *testing.T, k rverifyTKit) error {
				return k.create(
					k.sale("SD-2026-0007", 7, rverifyTLine(1, desc, 1500)),
					k.sale("SD-2026-0010", 10, rverifyTLine(1, desc, 1500)),
				)
			},
			want: receiptsVerifyReport{Years: []int{2026}, Receipts: 7, Findings: []receiptsVerifyFinding{
				{Year: 2026, Check: "sequence", Message: "the number with sequence 6 is missing"},
				{Year: 2026, Check: "sequence", Message: "the numbers with sequences 8 to 9 are missing"},
			}},
			summary: "7 receipts of 2026 checked: 2 findings",
		},
		"a number not of its sequence": {
			write: func(_ *testing.T, k rverifyTKit) error {
				return k.create(k.sale("SD-2026-0099", 6, rverifyTLine(1, desc, 1500)))
			},
			want: receiptsVerifyReport{Years: []int{2026}, Receipts: 6, Findings: []receiptsVerifyFinding{{
				Year: 2026, Number: "SD-2026-0099", Check: "number",
				Message: "the number does not end in its year 2026 and sequence 6",
			}}},
			summary: "6 receipts of 2026 checked: 1 findings",
		},
		"a number of another year": {
			write: func(_ *testing.T, k rverifyTKit) error {
				return k.create(k.sale("SD-2025-0006", 6, rverifyTLine(1, desc, 1500)))
			},
			want: receiptsVerifyReport{Years: []int{2026}, Receipts: 6, Findings: []receiptsVerifyFinding{{
				Year: 2026, Number: "SD-2025-0006", Check: "number",
				Message: "the number does not end in its year 2026 and sequence 6",
			}}},
			summary: "6 receipts of 2026 checked: 1 findings",
		},
		"a storno of a storno": {
			write: func(_ *testing.T, k rverifyTKit) error {
				storno, err := k.get("SD-2026-0002")
				if err != nil {
					return err
				}

				return k.create(k.storno("SD-2026-0006", 6, storno))
			},
			want: receiptsVerifyReport{Years: []int{2026}, Receipts: 6, Findings: []receiptsVerifyFinding{{
				Year: 2026, Number: "SD-2026-0006", Check: "cancellation",
				Message: "the cancellation cancels no sale receipt",
			}}},
			summary: "6 receipts of 2026 checked: 1 findings",
		},
		"a storno with other amounts": {
			write: func(_ *testing.T, k rverifyTKit) error {
				sale, err := k.get("SD-2026-0003")
				if err != nil {
					return err
				}

				r := k.storno("SD-2026-0006", 6, sale)
				r.Lines = []domain.ReceiptLine{rverifyTLine(1, desc, -1000)}
				r.Net, r.VAT, r.Gross = -840, -160, -1000

				return k.create(r)
			},
			args: []string{"--year", "2026"},
			want: receiptsVerifyReport{Years: []int{2026}, Receipts: 6, Findings: []receiptsVerifyFinding{{
				Year: 2026, Number: "SD-2026-0006", Check: "cancellation",
				Message: "the cancellation does not negate SD-2026-0003: net, VAT and gross -840, -160, -1000 EUR " +
					"against %d, %d, %d EUR",
			}}},
			summary: "6 receipts of 2026 checked: 1 findings",
		},
		"a storno in another currency": {
			write: func(_ *testing.T, k rverifyTKit) error {
				sale, err := k.get("SD-2026-0003")
				if err != nil {
					return err
				}

				r := k.storno("SD-2026-0006", 6, sale)
				r.Currency = "CHF"

				return k.create(r)
			},
			want: receiptsVerifyReport{Years: []int{2026}, Receipts: 6, Findings: []receiptsVerifyFinding{{
				Year: 2026, Number: "SD-2026-0006", Check: "cancellation",
				Message: "the cancellation does not negate SD-2026-0003: net, VAT and gross -%d, -%d, -%d CHF " +
					"against %d, %d, %d EUR",
			}}},
			summary: "6 receipts of 2026 checked: 1 findings",
		},
		"totals not the sum of the lines": {
			write: func(_ *testing.T, k rverifyTKit) error {
				r := k.sale("SD-2026-0006", 6, rverifyTLine(1, desc, 1000))
				r.Net, r.VAT, r.Gross = 1008, 192, 1200

				return k.create(r)
			},
			want: receiptsVerifyReport{Years: []int{2026}, Receipts: 6, Findings: []receiptsVerifyFinding{{
				Year: 2026, Number: "SD-2026-0006", Check: "totals",
				Message: "net, VAT and gross are 1008, 192, 1200; the lines sum to 840, 160, 1000",
			}}},
			summary: "6 receipts of 2026 checked: 1 findings",
		},
		"a storno of a sale of the year before, --year": {
			write:   rverifyTAcrossYears,
			args:    []string{"--year", "2026"},
			want:    receiptsVerifyReport{OK: true, Years: []int{2026}, Receipts: 6},
			summary: "6 receipts of 2026 checked: no findings",
		},
		"a storno of a sale of the year before, every year": {
			write:   rverifyTAcrossYears,
			want:    receiptsVerifyReport{OK: true, Years: []int{2025, 2026}, Receipts: 7},
			summary: "7 receipts of 2025, 2026 checked: no findings",
		},
		"a year without receipts": {
			args:    []string{"--year", "2024"},
			want:    receiptsVerifyReport{OK: true, Years: []int{}, Receipts: 0},
			summary: "no receipts to check",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			stores(t, func(t *testing.T, r cliRun) {
				var sale domain.Receipt

				rverifyTWrite(t, r, func(k rverifyTKit) error {
					var err error
					if sale, err = k.get("SD-2026-0003"); err != nil || tc.write == nil {
						return err
					}

					return tc.write(t, k)
				})

				want := tc.want
				want.Findings = slices.Clone(want.Findings)

				for i, f := range want.Findings {
					if strings.Contains(f.Message, "%d") { // the original's amounts
						if strings.Contains(f.Message, "-%d") {
							want.Findings[i].Message = fmt.Sprintf(f.Message, sale.Net, sale.VAT, sale.Gross,
								sale.Net, sale.VAT, sale.Gross)
						} else {
							want.Findings[i].Message = fmt.Sprintf(f.Message, sale.Net, sale.VAT, sale.Gross)
						}
					}
				}

				if want.Findings == nil {
					want.Findings = []receiptsVerifyFinding{}
				}

				code, stderr := 0, ""
				if !want.OK {
					code, stderr = 1, fmt.Sprintf("error: the receipts have findings: %d\n", len(want.Findings))
				}

				args := append([]string{"receipts", "verify"}, tc.args...)

				got := r.exec(t, append(args, "--output", "json")...)
				if got.code != code || got.stderr != "" {
					t.Fatalf("JSON: exit %d, want %d; stderr %q, want none", got.code, code, got.stderr)
				}

				if report := rverifyTReport(t, got); !rverifyTSame(report, want) {
					t.Errorf("report\n%+v\nwant\n%+v", report, want)
				}

				got = r.exec(t, args...)

				lines := strings.Split(strings.TrimSuffix(got.stdout, "\n"), "\n")
				if got.code != code || got.stderr != stderr || lines[len(lines)-1] != tc.summary {
					t.Errorf("text: exit %d, stderr %q, stdout\n%s\nwant exit %d, last line %q",
						got.code, got.stderr, got.stdout, code, tc.summary)
				}

				// One line per finding, then the summary.
				if len(lines) != len(want.Findings)+1 {
					t.Errorf("text: %d lines, want %d:\n%s", len(lines), len(want.Findings)+1, got.stdout)
				}

				for i, f := range want.Findings {
					number := f.Number
					if number == "" {
						number = "-"
					}

					if fields := strings.Fields(lines[i]); len(fields) < 4 || fields[0] != "2026" ||
						fields[1] != number || fields[2] != f.Check || !strings.HasSuffix(lines[i], f.Message) {
						t.Errorf("text line %d %q, want %d %s %s %s", i, lines[i], f.Year, number, f.Check, f.Message)
					}
				}
			})
		})
	}
}

// rverifyTAcrossYears writes a sale of 2025 and, in 2026, its storno, both
// numbered by the counters as ReceiptService does.
func rverifyTAcrossYears(_ *testing.T, k rverifyTKit) error {
	seq25, err := k.tx.ReceiptCounters().Next(k.ctx, 2025)
	if err != nil {
		return err
	}

	seq26, err := k.tx.ReceiptCounters().Next(k.ctx, 2026)
	if err != nil {
		return err
	}

	if seq25 != 1 || seq26 != 6 {
		return fmt.Errorf("the counters gave %d for 2025 and %d for 2026, want 1 and 6", seq25, seq26)
	}

	sale := rverifyTInYear(k.sale(domain.ReceiptNumber("SD", 2025, seq25), seq25, rverifyTLine(1, "Fictional pass", 1500)),
		domain.Date{Year: 2025, Month: 12, Day: 15})

	return k.create(sale, k.storno(domain.ReceiptNumber("SD", 2026, seq26), seq26, sale))
}

// TestReceiptsVerifyRefusals: verify is an operator command, so --as is a
// usage error, and so is a year outside 0 to 9999.
func TestReceiptsVerifyRefusals(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		args   []string
		stderr string
	}{
		"--as":        {args: []string{"--as", demoOffice}},
		"--year -1":   {args: []string{"--year", "-1"}, stderr: "error: --year: the year must lie between 1 and 9999, or be 0 for every year\n"},
		"--year 1e4":  {args: []string{"--year", "10000"}, stderr: "error: --year: the year must lie between 1 and 9999, or be 0 for every year\n"},
		"an argument": {args: []string{"2026"}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := cliRun{st: demoStore(t)}.exec(t, append([]string{"receipts", "verify"}, tc.args...)...)
			if got.code != 2 || got.stdout != "" || (tc.stderr != "" && got.stderr != tc.stderr) {
				t.Errorf("exit %d, stdout %q, stderr %q; want exit 2, stderr %q", got.code, got.stdout, got.stderr, tc.stderr)
			}
		})
	}
}
