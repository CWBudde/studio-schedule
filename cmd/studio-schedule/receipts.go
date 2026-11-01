package main

import (
	"errors"
	"io/fs"
	"os"

	"github.com/spf13/cobra"

	"github.com/cwbudde/studio-schedule/cmd/studio-schedule/cli"
	"github.com/cwbudde/studio-schedule/internal/domain"
)

// newReceiptsCmd is the receipts group (ADR 0012, ADR 0020).
func newReceiptsCmd(app *cli.App) *cobra.Command {
	return cli.Group("receipts", "List, download, export and verify receipts",
		newReceiptsListCmd(app), newReceiptsPDFCmd(app), newReceiptsExportCmd(app), newReceiptsVerifyCmd(app))
}

// receiptFileMode is the mode of the files the receipt commands write:
// receipts name persons and their payments.
const receiptFileMode = 0o600

// receiptDeletedPerson is the name of an anonymized person, as the web
// shows it (web/src/lib/personName.ts).
const receiptDeletedPerson = "Deleted person"

// receiptShownName is a name the API put together, or "Deleted person" for
// an anonymized person, like the web's shownName.
func receiptShownName(name string, anonymized bool) string {
	if anonymized {
		return receiptDeletedPerson
	}

	return name
}

// receiptKind is the kind of a receipt in the commands' words: "receipt"
// for a sale, "storno" for a cancellation.
func receiptKind(k string) string {
	if k == string(domain.ReceiptCancellation) {
		return "storno"
	}

	return "receipt"
}

// receiptWriteFile writes data into the file name with receiptFileMode. An
// existing file is a usage error unless force; then it is replaced, and its
// mode becomes receiptFileMode.
func receiptWriteFile(name string, data []byte, force bool) error {
	flags := os.O_WRONLY | os.O_CREATE | os.O_EXCL
	if force {
		flags = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	}

	f, err := os.OpenFile(name, flags, receiptFileMode) //nolint:gosec // G304: the operator names the file
	if errors.Is(err, fs.ErrExist) {
		return cli.Usagef("--file %s exists; give --force to overwrite it", name)
	}

	if err != nil {
		return err
	}

	_, err = f.Write(data)

	return errors.Join(err, f.Chmod(receiptFileMode), f.Close())
}

// receiptFileFlags binds --file and --force of a command that writes a
// file.
func receiptFileFlags(cmd *cobra.Command, file *string, force *bool, what string) {
	cmd.Flags().StringVarP(file, "file", "f", "", "write "+what+" into this file (mode 0600)")
	cmd.Flags().BoolVar(force, "force", false, "overwrite the file of --file if it exists")
}
