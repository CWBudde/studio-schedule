package main

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/cwbudde/studio-schedule/cmd/studio-schedule/cli"
	"github.com/cwbudde/studio-schedule/internal/httpapi"
)

func newReceiptsPDFCmd(app *cli.App) *cobra.Command {
	var (
		file  string
		force bool
	)

	return app.Command(cli.Spec{
		Use:   "pdf <number>",
		Short: "Write the PDF of a receipt into a file",
		Long: "Write the PDF of the receipt with the printed number (e.g. SD-2026-0001) into the file of --file, " +
			"created with mode 0600; an existing file is refused unless --force. The PDF is the one issued with " +
			"the receipt; a receipt issued without one (the demo data's) gets it rendered now and stored, as " +
			"the office's download does. An unknown number exits with status 3.",
		Example: "  studio-schedule receipts pdf --as office@example.org SD-2026-0001 --file SD-2026-0001.pdf",
		Args:    cobra.ExactArgs(1),
		Access:  httpapi.AccessOffice,
		JSON:    "nothing: the PDF goes into the file of --file (the operation getReceiptPdf answers with it)",
		Flags: func(cmd *cobra.Command) {
			receiptFileFlags(cmd, &file, &force, "the PDF")
			_ = cmd.MarkFlagRequired("file") // the flag exists
		},
		Run: func(ctx context.Context, env *cli.Env, args []string) error {
			v, err := env.Services.Receipts.ByNumber(ctx, args[0])
			if err != nil {
				return err
			}

			// The services start the renderer only for a receipt issued
			// without a PDF (the demo data's), which it then stores.
			pdf, err := env.Services.Receipts.PDF(ctx, v.Receipt.ID)
			if err != nil {
				return err
			}

			if err := receiptWriteFile(file, pdf.PDF, force); err != nil {
				return err
			}

			if env.Out.JSON() {
				return nil
			}

			return env.Out.Print(nil, func(t *cli.Text) {
				t.Line("wrote %s (%d bytes) to %s", v.Receipt.Number, len(pdf.PDF), file)
			})
		},
	})
}
