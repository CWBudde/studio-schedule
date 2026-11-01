package main

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/service"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// rpdfTRendered is what renderingPDF renders.
const rpdfTRendered = "%PDF-1.7\n%fake\n"

// rpdfTStoredPDF stores pdf as the PDF of the receipt with the number.
func rpdfTStoredPDF(t *testing.T, st store.Store, number string, pdf []byte) {
	t.Helper()

	err := st.InTx(t.Context(), store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		r, err := tx.Receipts().GetByNumber(ctx, number)
		if err != nil {
			return err
		}

		return tx.Receipts().PutPDF(ctx, r.ID, pdf)
	})
	if err != nil {
		t.Fatal(err)
	}
}

// rpdfTPDFOf returns the stored PDF of the receipt with the number, nil
// without one.
func rpdfTPDFOf(t *testing.T, st store.Store, number string) []byte {
	t.Helper()

	var pdf []byte

	err := st.InTx(t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		r, err := tx.Receipts().GetByNumber(ctx, number)
		if err != nil {
			return err
		}

		pdf, err = tx.Receipts().PDF(ctx, r.ID)
		if errors.Is(err, domain.ErrNotFound) {
			return nil
		}

		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	return pdf
}

// rpdfTNoRenderer is a cliRun.pdf that fails the test when the command
// starts the renderer.
func rpdfTNoRenderer(t *testing.T) func() (service.ReceiptRenderer, func()) {
	return func() (service.ReceiptRenderer, func()) {
		t.Error("the renderer was started")

		return fakeRenderer{err: errors.New("not to be started")}, func() {}
	}
}

// rpdfTFile reads the file and its permission bits.
func rpdfTFile(t *testing.T, name string) ([]byte, fs.FileMode) {
	t.Helper()

	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(name)
	if err != nil {
		t.Fatal(err)
	}

	return data, info.Mode().Perm()
}

// rpdfTNoFile fails the test if the file exists.
func rpdfTNoFile(t *testing.T, name string) {
	t.Helper()

	if _, err := os.Stat(name); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("%s: %v, want no file", name, err)
	}
}

// TestReceiptsPDFStored writes a receipt's stored PDF byte for byte with
// mode 0600, without starting the renderer, and names it by the printed
// number in any case; --output json prints nothing.
func TestReceiptsPDFStored(t *testing.T) {
	t.Parallel()

	stores(t, func(t *testing.T, r cliRun) {
		stored := []byte("%PDF-1.7\n%stored\n\x00\xff binary\n")
		rpdfTStoredPDF(t, storeOf(t, r), "SD-2026-0003", stored)

		r.pdf = rpdfTNoRenderer(t)
		file := filepath.Join(t.TempDir(), "receipt.pdf")

		got := r.exec(t, "receipts", "pdf", "--as", demoOffice, "SD-2026-0003", "--file", file)
		if want := "wrote SD-2026-0003 (" + strconv.Itoa(len(stored)) + " bytes) to " + file + "\n"; got.code != 0 ||
			got.stdout != want || got.stderr != "" {
			t.Fatalf("exit %d, stdout %q, stderr %q; want %q", got.code, got.stdout, got.stderr, want)
		}

		if data, mode := rpdfTFile(t, file); !bytes.Equal(data, stored) || mode != outputFileMode {
			t.Errorf("wrote %q with mode %v, want the stored PDF with mode 0600", data, mode)
		}

		// A lower-case number with spaces, and JSON: nothing on stdout.
		file = filepath.Join(t.TempDir(), "receipt.pdf")

		got = r.exec(t, "receipts", "pdf", "--as", demoOffice, " sd-2026-0003 ", "-f", file, "--output", "json")
		if got.code != 0 || got.stdout != "" || got.stderr != "" {
			t.Fatalf("lower case, JSON: exit %d, stdout %q, stderr %q", got.code, got.stdout, got.stderr)
		}

		if data, _ := rpdfTFile(t, file); !bytes.Equal(data, stored) {
			t.Errorf("wrote %q, want the stored PDF", data)
		}
	})
}

// TestReceiptsPDFRendered renders the PDF of a demo receipt without one,
// writes and stores it: the next run reads the stored one, without a
// renderer. Without a renderer at all, or with one that fails, the command
// fails and writes no file.
func TestReceiptsPDFRendered(t *testing.T) {
	t.Parallel()

	stores(t, func(t *testing.T, r cliRun) {
		st := storeOf(t, r)
		dir := t.TempDir()

		// No renderer: the error stays.
		got := r.exec(t, "receipts", "pdf", "--as", demoOffice, "SD-2026-0001", "--file", filepath.Join(dir, "none.pdf"))
		if got.code != 1 || got.stdout != "" || !strings.Contains(got.stderr, service.ErrNoPDFRenderer.Error()) {
			t.Errorf("without a renderer: exit %d, stdout %q, stderr %q", got.code, got.stdout, got.stderr)
		}

		rpdfTNoFile(t, filepath.Join(dir, "none.pdf"))

		// A renderer that fails.
		failing := r
		failing.pdf = pdfOf(fakeRenderer{err: errors.New("the module trapped")})

		got = failing.exec(t, "receipts", "pdf", "--as", demoOffice, "SD-2026-0001", "--file", filepath.Join(dir, "fail.pdf"))
		if got.code != 1 || got.stdout != "" || !strings.Contains(got.stderr, "the module trapped") {
			t.Errorf("a failing renderer: exit %d, stdout %q, stderr %q", got.code, got.stdout, got.stderr)
		}

		rpdfTNoFile(t, filepath.Join(dir, "fail.pdf"))

		if pdf := rpdfTPDFOf(t, st, "SD-2026-0001"); pdf != nil {
			t.Fatalf("stored %q after the failures, want none", pdf)
		}

		// Rendered, written and stored; the renderer stops with the command.
		var starts, stops int

		rendering := r
		rendering.pdf = func() (service.ReceiptRenderer, func()) {
			starts++

			return fakeRenderer{out: []byte(rpdfTRendered)}, func() { stops++ }
		}
		file := filepath.Join(dir, "rendered.pdf")

		got = rendering.exec(t, "receipts", "pdf", "--as", demoOffice, "SD-2026-0001", "--file", file)
		if want := "wrote SD-2026-0001 (" + strconv.Itoa(len(rpdfTRendered)) + " bytes) to " + file + "\n"; got.code != 0 ||
			got.stdout != want || got.stderr != "" {
			t.Fatalf("rendered: exit %d, stdout %q, stderr %q; want %q", got.code, got.stdout, got.stderr, want)
		}

		if data, mode := rpdfTFile(t, file); string(data) != rpdfTRendered || mode != outputFileMode {
			t.Errorf("wrote %q with mode %v, want the rendered PDF with mode 0600", data, mode)
		}

		if pdf := rpdfTPDFOf(t, st, "SD-2026-0001"); string(pdf) != rpdfTRendered {
			t.Errorf("stored %q, want the rendered PDF", pdf)
		}

		if starts != 1 || stops != 1 {
			t.Errorf("the renderer started %d and stopped %d times, want once each", starts, stops)
		}

		// Read again without a renderer: the stored one.
		file = filepath.Join(dir, "again.pdf")

		got = r.exec(t, "receipts", "pdf", "--as", demoOffice, "SD-2026-0001", "--file", file)
		if got.code != 0 || got.stderr != "" {
			t.Fatalf("again: exit %d, stderr %q", got.code, got.stderr)
		}

		if data, _ := rpdfTFile(t, file); string(data) != rpdfTRendered {
			t.Errorf("wrote %q the second time, want the stored PDF", data)
		}
	})
}

// TestReceiptsPDFFile: an existing file is refused unless --force, which
// replaces it and resets its mode to 0600; a file in a missing directory
// fails.
func TestReceiptsPDFFile(t *testing.T) {
	t.Parallel()

	stores(t, func(t *testing.T, r cliRun) {
		stored := []byte("%PDF-1.7\n%stored\n")
		rpdfTStoredPDF(t, storeOf(t, r), "SD-2026-0004", stored)

		file := filepath.Join(t.TempDir(), "existing.pdf")
		if err := os.WriteFile(file, []byte("keep me"), 0o600); err != nil {
			t.Fatal(err)
		}

		if err := os.Chmod(file, 0o644); err != nil {
			t.Fatal(err)
		}

		got := r.exec(t, "receipts", "pdf", "--as", demoOffice, "SD-2026-0004", "--file", file)
		if want := "error: --file " + file + " exists; give --force to overwrite it\n"; got.code != 2 ||
			got.stdout != "" || got.stderr != want {
			t.Errorf("existing file: exit %d, stdout %q, stderr %q; want exit 2, %q", got.code, got.stdout, got.stderr, want)
		}

		if data, mode := rpdfTFile(t, file); string(data) != "keep me" || mode != 0o644 {
			t.Errorf("the refused run left %q with mode %v, want the file unchanged", data, mode)
		}

		got = r.exec(t, "receipts", "pdf", "--as", demoOffice, "SD-2026-0004", "--file", file, "--force")
		if got.code != 0 || got.stderr != "" {
			t.Fatalf("--force: exit %d, stderr %q", got.code, got.stderr)
		}

		if data, mode := rpdfTFile(t, file); !bytes.Equal(data, stored) || mode != outputFileMode {
			t.Errorf("--force wrote %q with mode %v, want the PDF with mode 0600", data, mode)
		}

		missing := filepath.Join(t.TempDir(), "missing", "receipt.pdf")

		got = r.exec(t, "receipts", "pdf", "--as", demoOffice, "SD-2026-0004", "--file", missing)
		if got.code != 1 || got.stdout != "" || !strings.Contains(got.stderr, "no such file or directory") {
			t.Errorf("missing directory: exit %d, stdout %q, stderr %q", got.code, got.stdout, got.stderr)
		}

		got = r.exec(t, "receipts", "pdf", "--as", demoOffice, "SD-2099-0001", "--file", missing)
		if got.code != 3 || got.stdout != "" || !strings.Contains(got.stderr, "SD-2099-0001") {
			t.Errorf("unknown number: exit %d, stdout %q, stderr %q; want exit 3", got.code, got.stdout, got.stderr)
		}
	})
}

// TestReceiptsPDFRefusals: what the command refuses writes no file and
// prints nothing on stdout: the account of --as (none, unknown, a role
// below office), a missing --file or number.
func TestReceiptsPDFRefusals(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		args   []string
		code   int
		stderr string
	}{
		"no --as": {
			args: []string{"SD-2026-0001"}, code: 2,
			stderr: "error: studio-schedule receipts pdf acts as an account: give --as <email> or set STUDIO_AS\n",
		},
		"unknown --as": {
			args: []string{"--as", "nobody@example.org", "SD-2026-0001"}, code: 3,
			stderr: "error: --as nobody@example.org: not found: no account has this email\n",
		},
		"--as a member": {
			args: []string{"--as", demoMember, "SD-2026-0001"}, code: 4,
			stderr: "error: --as " + demoMember + ": forbidden: the account's role member is below office, " +
				"which this command needs\n",
		},
		"--as an instructor": {args: []string{"--as", demoInstructor, "SD-2026-0001"}, code: 4},
		"no --file":          {args: []string{"--as", demoOffice, "SD-2026-0001"}, code: 2, stderr: "error: required flag(s) \"file\" not set\n"},
		"no number":          {args: []string{"--as", demoOffice}, code: 2},
		"two numbers":        {args: []string{"--as", demoOffice, "SD-2026-0001", "SD-2026-0002"}, code: 2},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			file := filepath.Join(t.TempDir(), "receipt.pdf")
			args := append([]string{"receipts", "pdf"}, tc.args...)

			if name != "no --file" {
				args = append(args, "--file", file)
			}

			got := cliRun{st: demoStore(t), pdf: renderingPDF}.exec(t, args...)
			if got.code != tc.code || got.stdout != "" || (tc.stderr != "" && got.stderr != tc.stderr) {
				t.Errorf("receipts pdf %v: exit %d, stdout %q, stderr %q; want exit %d, stderr %q", tc.args, got.code,
					got.stdout, got.stderr, tc.code, tc.stderr)
			}

			rpdfTNoFile(t, file)
		})
	}
}
