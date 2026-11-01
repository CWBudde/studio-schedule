package main

import (
	"errors"
	"io"
	"io/fs"
	"os"

	"github.com/spf13/cobra"

	"github.com/cwbudde/studio-schedule/cmd/studio-schedule/cli"
)

// outputFileMode is the mode of the files the commands write: exports,
// receipts and settings name persons, payments and the school.
const outputFileMode = 0o600

// outputFile is the --file (-f) and --force of a command that writes a
// file (ADR 0020, amendment): -o is the global --output, so no command
// names its file with it. The file is created with outputFileMode; an
// existing one is refused unless --force, which replaces it and resets its
// mode.
type outputFile struct {
	name  string
	force bool
}

// bind adds --file and --force to cmd; what is what goes into the file,
// e.g. "the CSV", and usage ends the help of --file.
func (f *outputFile) bind(cmd *cobra.Command, what, usage string) {
	cmd.Flags().StringVarP(&f.name, "file", "f", "", "write "+what+" into this file (mode 0600)"+usage)
	cmd.Flags().BoolVar(&f.force, "force", false, "overwrite the file of --file if it exists")
}

// check refuses --force without --file, before the command does anything.
func (f *outputFile) check() error {
	if f.force && f.name == "" {
		return cli.Usagef("--force: give --file too")
	}

	return nil
}

// write writes the file of --file with write. An existing file is a usage
// error unless --force.
func (f *outputFile) write(write func(w io.Writer) error) error {
	flags := os.O_WRONLY | os.O_CREATE | os.O_EXCL
	if f.force {
		flags = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	}

	file, err := os.OpenFile(f.name, flags, outputFileMode)
	if errors.Is(err, fs.ErrExist) {
		return cli.Usagef("--file %s exists; give --force to overwrite it", f.name)
	}

	if err != nil {
		return err
	}

	return errors.Join(write(file), file.Chmod(outputFileMode), file.Close())
}

// writeBytes writes data into the file of --file (write).
func (f *outputFile) writeBytes(data []byte) error {
	return f.write(func(w io.Writer) error {
		_, err := w.Write(data)

		return err
	})
}
