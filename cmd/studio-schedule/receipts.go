package main

import (
	"github.com/spf13/cobra"

	"github.com/cwbudde/studio-schedule/cmd/studio-schedule/cli"
	"github.com/cwbudde/studio-schedule/internal/domain"
)

// newReceiptsCmd is the receipts group (ADR 0012, ADR 0020).
func newReceiptsCmd(app *cli.App) *cobra.Command {
	return cli.Group("receipts", "List, download, export and verify receipts",
		newReceiptsListCmd(app), newReceiptsPDFCmd(app), newReceiptsExportCmd(app), newReceiptsVerifyCmd(app))
}

// receiptKind is the kind of a receipt in the commands' words: "receipt"
// for a sale, "storno" for a cancellation.
func receiptKind(k string) string {
	if k == string(domain.ReceiptCancellation) {
		return "storno"
	}

	return "receipt"
}
