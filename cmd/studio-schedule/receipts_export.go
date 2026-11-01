package main

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cwbudde/studio-schedule/cmd/studio-schedule/cli"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/httpapi"
	"github.com/cwbudde/studio-schedule/internal/service"
)

// receiptCSVHeader are the columns of receipts export.
var receiptCSVHeader = []string{
	"number", "issue_date", "kind", "cancels", "recipient", "payment_method", "tax_mode", "position", "item",
	"quantity", "vat_rate", "net_minor", "vat_minor", "gross_minor", "net", "vat", "gross", "currency",
}

// receiptCSVColumns documents receiptCSVHeader for the help.
const receiptCSVColumns = "number (the printed number), issue_date (the school date, YYYY-MM-DD), kind (receipt, " +
	"or storno for a cancellation), cancels (the number of the receipt a storno cancels, else empty), " +
	"recipient (the name printed as the payer), payment_method (cash, transfer or card), tax_mode " +
	"(small_business, exempt or standard), position (of the line, from 1), item (what was paid), quantity, " +
	"vat_rate (the VAT rate in percent, e.g. 19.00), net_minor, vat_minor and gross_minor (the line's amounts " +
	"in minor units, e.g. cents; negative on a storno), net, vat and gross (the same as decimal numbers with a " +
	"point, e.g. -15.00) and currency (ISO 4217)"

func newReceiptsExportCmd(app *cli.App) *cobra.Command {
	var (
		year int
		file outputFile
	)

	return app.Command(cli.Spec{
		Use:   "export",
		Short: "Export the receipts of a year as CSV, for the tax adviser",
		Long: "Export every receipt of --year as CSV (RFC 4180: comma-separated, CRLF line ends, UTF-8, a header " +
			"row), one line per line item of a receipt, in the order of the numbers. The columns are " +
			receiptCSVColumns + ". A text cell that starts with =, +, -, @, a tab or a carriage return gets a " +
			"leading apostrophe, so a spreadsheet does not run it as a formula. The CSV goes to stdout, or with " +
			"--file into that file, created with mode 0600; an existing file is refused unless --force. CSV is " +
			"the only format: --output json is refused.",
		Example: "  studio-schedule receipts export --as office@example.org --year 2026 --file receipts-2026.csv",
		Access:  httpapi.AccessOffice,
		JSON:    "none: the command writes CSV and refuses --output json",
		Flags: func(cmd *cobra.Command) {
			cmd.Flags().IntVar(&year, "year", 0, "the year of the receipts")
			_ = cmd.MarkFlagRequired("year") // the flag exists
			file.bind(cmd, "the CSV", " instead of stdout")
		},
		Fields: map[string]string{"year": "--year"},
		Run: func(ctx context.Context, env *cli.Env, _ []string) error {
			if env.Out.JSON() {
				return cli.Usagef("--output json: receipts export writes CSV only")
			}

			if err := file.check(); err != nil {
				return err
			}

			views, err := env.Services.Receipts.OfYear(ctx, year)
			if err != nil {
				return err
			}

			data, lines := receiptCSV(views)

			if file.name == "" {
				return env.Out.Print(nil, func(t *cli.Text) { t.Line("%s", strings.TrimSuffix(string(data), "\n")) })
			}

			if err := file.writeBytes(data); err != nil {
				return err
			}

			return env.Out.Print(nil, func(t *cli.Text) {
				t.Line("wrote %d lines of %d receipts of %d to %s", lines, len(views), year, file.name)
			})
		},
	})
}

// receiptCSV returns the receipts of views as CSV, one record per line
// item after the header, and the number of those records.
func receiptCSV(views []service.ReceiptView) ([]byte, int) {
	var buf bytes.Buffer

	out := csv.NewWriter(&buf)
	out.UseCRLF = true

	// A bytes.Buffer does not fail and the comma is valid: the writes cannot
	// fail.
	_ = out.Write(receiptCSVHeader)

	n := 0

	for _, v := range views {
		r := v.Receipt
		cancels := ""

		if v.Cancels != nil {
			cancels = v.Cancels.Number
		}

		money := func(m domain.Money) []string {
			return []string{strconv.FormatInt(int64(m), 10), cli.Decimal(m, r.Currency)}
		}

		for _, l := range r.Lines {
			net, vat, gross := money(l.Net), money(l.VAT), money(l.Gross)
			_ = out.Write([]string{
				receiptCSVText(r.Number), r.IssueDate.String(), receiptKind(string(r.Kind)), receiptCSVText(cancels),
				receiptCSVText(r.RecipientName), string(r.PaymentMethod), string(r.TaxMode), strconv.Itoa(l.Position),
				receiptCSVText(l.Description), strconv.Itoa(l.Quantity), receiptPercent(l.VATRate),
				net[0], vat[0], gross[0], net[1], vat[1], gross[1], receiptCSVText(r.Currency),
			})
			n++
		}
	}

	out.Flush()

	return buf.Bytes(), n
}

// receiptPercent is a rate in basis points as a percentage with two
// decimals, e.g. "19.00".
func receiptPercent(bp int) string { return fmt.Sprintf("%d.%02d", bp/100, bp%100) }

// receiptCSVText neutralizes a text cell that a spreadsheet would run as a
// formula (CSV injection): one that starts with =, +, -, @, a tab or a
// carriage return gets a leading apostrophe. Amount cells are numbers the
// command formats and stay as they are.
func receiptCSVText(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}

	return s
}
