package main

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cwbudde/studio-schedule/cmd/studio-schedule/cli"
	"github.com/cwbudde/studio-schedule/internal/service"
)

// errReceiptFindings makes receipts verify exit with status 1 after its
// report.
var errReceiptFindings = errors.New("the receipts have findings")

// receiptsVerifyJSON documents what `receipts verify --output json` prints.
const receiptsVerifyJSON = "one object: ok (bool, false when there is a finding), years (the years checked), " +
	"receipts (how many were checked) and findings, an array of {year, number, check, message}, where check " +
	"is sequence, number, cancellation or totals, and number is absent for missing numbers"

// receiptsVerifyReport is the JSON of receipts verify.
type receiptsVerifyReport struct {
	OK       bool                    `json:"ok"`
	Years    []int                   `json:"years"`
	Receipts int                     `json:"receipts"`
	Findings []receiptsVerifyFinding `json:"findings"`
}

type receiptsVerifyFinding struct {
	Year    int    `json:"year"`
	Number  string `json:"number,omitempty"`
	Check   string `json:"check"`
	Message string `json:"message"`
}

func newReceiptsVerifyCmd(app *cli.App) *cobra.Command {
	var year int

	return app.Command(cli.Spec{
		Use:   "verify",
		Short: "Check that the receipts are complete and consistent",
		Long: "Check the receipts of --year, or of every year without it, and print one line per finding: " +
			"sequence (the numbers of a year do not run from 1 without a gap), number (a printed number does " +
			"not end in its year and sequence), cancellation (a storno cancels no sale receipt, or does not " +
			"negate its amounts) and totals (the net, VAT or gross total of a receipt is not the sum of its " +
			"lines). Duplicate numbers and a receipt canceled twice are refused by the database itself. It " +
			"changes nothing and exits with status 1 when there is a finding. An operator command: it acts as " +
			"no account.",
		Example: "  studio-schedule receipts verify\n  studio-schedule receipts verify --year 2026 --output json",
		Store:   true,
		JSON:    receiptsVerifyJSON,
		Flags: func(cmd *cobra.Command) {
			cmd.Flags().IntVar(&year, "year", 0, "check only this year (default: every year)")
		},
		Fields: map[string]string{"year": "--year"},
		Run: func(ctx context.Context, env *cli.Env, _ []string) error {
			v, err := env.Services.Receipts.Verify(ctx, year)
			if err != nil {
				return err
			}

			report := receiptsVerifyReport{
				OK: len(v.Findings) == 0, Years: v.Years, Receipts: v.Receipts,
				Findings: make([]receiptsVerifyFinding, len(v.Findings)),
			}
			for i, f := range v.Findings {
				report.Findings[i] = receiptsVerifyFinding{
					Year: f.Year, Number: f.Number, Check: string(f.Check), Message: f.Message,
				}
			}

			err = env.Out.Print(report, func(t *cli.Text) { receiptsVerifyText(t, v) })
			if err != nil || report.OK {
				return err
			}

			return fmt.Errorf("%w: %d", errReceiptFindings, len(v.Findings))
		},
	})
}

// receiptsVerifyText writes the findings of v, then what was checked.
func receiptsVerifyText(t *cli.Text, v service.ReceiptVerification) {
	if len(v.Findings) > 0 {
		t.Table("Year", "Receipt", "Check", "Finding")

		for _, f := range v.Findings {
			t.Row(strconv.Itoa(f.Year), f.Number, t.Paint(cli.Red, string(f.Check)), f.Message)
		}
	}

	years := make([]string, len(v.Years))
	for i, y := range v.Years {
		years[i] = strconv.Itoa(y)
	}

	switch {
	case v.Receipts == 0:
		t.Line("no receipts to check")
	case len(v.Findings) == 0:
		t.Line("%d receipts of %s checked: no findings", v.Receipts, strings.Join(years, ", "))
	default:
		t.Line("%d receipts of %s checked: %d findings", v.Receipts, strings.Join(years, ", "), len(v.Findings))
	}
}
