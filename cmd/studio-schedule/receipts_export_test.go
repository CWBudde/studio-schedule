package main

import (
	"encoding/csv"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/service"
)

// rexportTParse parses the CSV of an export, checks its line ends (CRLF
// only) and its header, and returns the records after the header.
func rexportTParse(t *testing.T, data string) [][]string {
	t.Helper()

	if strings.Count(data, "\n") != strings.Count(data, "\r\n") || !strings.HasSuffix(data, "\r\n") {
		t.Errorf("the CSV has line ends other than CRLF:\n%q", data)
	}

	records, err := csv.NewReader(strings.NewReader(data)).ReadAll()
	if err != nil {
		t.Fatalf("parse the CSV: %v\n%s", err, data)
	}

	if len(records) == 0 || !slices.Equal(records[0], receiptCSVHeader) {
		t.Fatalf("the CSV starts with %q, want the header %q", records, receiptCSVHeader)
	}

	if strings.Count(data, "\r\n") != len(records) {
		t.Errorf("%d lines for %d records: a cell spans lines", strings.Count(data, "\r\n"), len(records))
	}

	for i, rec := range records {
		if len(rec) != len(receiptCSVHeader) {
			t.Errorf("record %d has %d cells, want %d", i, len(rec), len(receiptCSVHeader))
		}
	}

	return records[1:]
}

// rexportTColumn returns the index of the column name.
func rexportTColumn(t *testing.T, name string) int {
	t.Helper()

	i := slices.Index(receiptCSVHeader, name)
	if i < 0 {
		t.Fatalf("no column %q", name)
	}

	return i
}

// TestReceiptsExportDemo exports the demo's receipts of 2026 on both
// stores: the same CSV (golden) to stdout, one record per line in number
// order, the storno with negative amounts and the number it cancels; with
// --file the same bytes into a new file of mode 0600; an existing file is
// refused and left as it was, and replaced with --force.
func TestReceiptsExportDemo(t *testing.T) {
	t.Parallel()

	stores(t, func(t *testing.T, r cliRun) {
		got := r.exec(t, "receipts", "export", "--as", demoOffice, "--year", "2026")
		if got.code != 0 || got.stderr != "" {
			t.Fatalf("exit %d, stderr %q", got.code, got.stderr)
		}

		// The golden file has LF line ends (git, with core.autocrlf=input,
		// and .editorconfig would turn CRLF into LF); rexportTParse checks
		// the CRLF.
		golden(t, "receipts/export-2026.csv", strings.ReplaceAll(got.stdout, "\r\n", "\n"))

		records := rexportTParse(t, got.stdout)
		number, kind, cancels := rexportTColumn(t, "number"), rexportTColumn(t, "kind"), rexportTColumn(t, "cancels")
		gross, grossMinor := rexportTColumn(t, "gross"), rexportTColumn(t, "gross_minor")

		var numbers []string
		for _, rec := range records {
			numbers = append(numbers, rec[number])
		}

		if want := []string{"SD-2026-0001", "SD-2026-0002", "SD-2026-0003", "SD-2026-0004", "SD-2026-0005"}; !slices.Equal(numbers, want) {
			t.Errorf("numbers %q, want %q", numbers, want)
		}

		storno := records[1]
		if storno[kind] != "storno" || storno[cancels] != "SD-2026-0001" || records[0][kind] != "receipt" ||
			records[0][cancels] != "" {
			t.Errorf("kinds and cancels: %q, %q", records[0], storno)
		}

		// The storno's amounts are negative numbers, without the apostrophe
		// of a text cell, and unquoted in the file.
		minor, err := strconv.ParseInt(storno[grossMinor], 10, 64)
		if err != nil || minor >= 0 || storno[gross] != receiptDecimal(domain.Money(minor), "EUR") ||
			!strings.Contains(got.stdout, ","+storno[grossMinor]+",") || !strings.HasPrefix(storno[gross], "-") {
			t.Errorf("storno amounts %q and %q, want negative numbers", storno[grossMinor], storno[gross])
		}

		csvData := got.stdout
		file := filepath.Join(t.TempDir(), "receipts-2026.csv")

		got = r.exec(t, "receipts", "export", "--as", demoOffice, "--year", "2026", "--file", file)
		if got.code != 0 || got.stderr != "" || got.stdout != "wrote 5 lines of 5 receipts of 2026 to "+file+"\n" {
			t.Fatalf("--file: exit %d, stdout %q, stderr %q", got.code, got.stdout, got.stderr)
		}

		rexportTFile(t, file, csvData)

		// An existing file is refused and kept; --force replaces it and
		// resets its mode to 0600.
		if err := os.WriteFile(file, []byte("kept\n"), 0o644); err != nil {
			t.Fatal(err)
		}

		got = r.exec(t, "receipts", "export", "--as", demoOffice, "--year", "2026", "-f", file)
		if got.code != 2 || got.stdout != "" ||
			got.stderr != "error: --file "+file+" exists; give --force to overwrite it\n" {
			t.Errorf("existing file: exit %d, stdout %q, stderr %q", got.code, got.stdout, got.stderr)
		}

		if data, err := os.ReadFile(file); err != nil || string(data) != "kept\n" {
			t.Errorf("the refused export changed the file: %q, %v", data, err)
		}

		got = r.exec(t, "receipts", "export", "--as", demoOffice, "--year", "2026", "--file", file, "--force")
		if got.code != 0 || got.stdout != "wrote 5 lines of 5 receipts of 2026 to "+file+"\n" {
			t.Fatalf("--force: exit %d, stdout %q, stderr %q", got.code, got.stdout, got.stderr)
		}

		rexportTFile(t, file, csvData)
	})
}

// rexportTFile checks that file holds want with mode 0600.
func rexportTFile(t *testing.T, file, want string) {
	t.Helper()

	data, err := os.ReadFile(file)
	if err != nil || string(data) != want {
		t.Errorf("%s holds %q, %v; want the CSV of stdout", file, data, err)
	}

	if info, err := os.Stat(file); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("%s: mode %v, %v; want 0600", file, info.Mode().Perm(), err)
	}
}

// TestReceiptsExportFormulas: a recipient name or an item that starts with
// =, +, - or @ gets a leading apostrophe, so a spreadsheet shows it as
// text; the amount cells stay numbers. On both stores.
func TestReceiptsExportFormulas(t *testing.T) {
	t.Parallel()

	stores(t, func(t *testing.T, r cliRun) {
		const prefixes = "=+-@"

		rverifyTWrite(t, r, func(k rverifyTKit) error {
			for i, p := range prefixes {
				rc := k.sale(domain.ReceiptNumber("SD", 2026, 6+i), 6+i,
					rverifyTLine(1, string(p)+"HYPERLINK(\"https://example.org\")", 1500),
					rverifyTLine(2, "Fictional pass", 1000))
				rc.RecipientName = string(p) + "SUM(A1:A9) Example"

				if err := k.create(rc); err != nil {
					return err
				}
			}

			return nil
		})

		got := r.exec(t, "receipts", "export", "--as", demoOffice, "--year", "2026")
		if got.code != 0 || got.stderr != "" {
			t.Fatalf("exit %d, stderr %q", got.code, got.stderr)
		}

		records := rexportTParse(t, got.stdout)
		if len(records) != 5+2*len(prefixes) {
			t.Fatalf("%d records, want %d", len(records), 5+2*len(prefixes))
		}

		recipient, item := rexportTColumn(t, "recipient"), rexportTColumn(t, "item")
		position, netMinor := rexportTColumn(t, "position"), rexportTColumn(t, "net_minor")

		for i, p := range prefixes {
			for j, wantItem := range []string{"'" + string(p) + "HYPERLINK(\"https://example.org\")", "Fictional pass"} {
				rec := records[5+2*i+j]
				if rec[recipient] != "'"+string(p)+"SUM(A1:A9) Example" || rec[item] != wantItem ||
					rec[position] != strconv.Itoa(j+1) {
					t.Errorf("record %q: want recipient %q, item %q", rec, "'"+string(p)+"SUM(A1:A9) Example", wantItem)
				}
			}
		}

		if net := records[1][netMinor]; !strings.HasPrefix(net, "-") {
			t.Errorf("the storno's net %q lost its sign", net)
		}
	})
}

// TestReceiptsExportEmptyYear: a year without receipts is the header
// alone, to stdout and into a file.
func TestReceiptsExportEmptyYear(t *testing.T) {
	t.Parallel()

	r := cliRun{st: demoStore(t)}

	got := r.exec(t, "receipts", "export", "--as", demoOffice, "--year", "2024")
	if got.code != 0 || got.stderr != "" || len(rexportTParse(t, got.stdout)) != 0 {
		t.Errorf("exit %d, stdout %q, stderr %q; want the header alone", got.code, got.stdout, got.stderr)
	}

	file := filepath.Join(t.TempDir(), "receipts-2024.csv")

	got = r.exec(t, "receipts", "export", "--as", demoOffice, "--year", "2024", "--file", file)
	if got.code != 0 || got.stdout != "wrote 0 lines of 0 receipts of 2024 to "+file+"\n" {
		t.Errorf("--file: exit %d, stdout %q, stderr %q", got.code, got.stdout, got.stderr)
	}

	rexportTFile(t, file, strings.Join(receiptCSVHeader, ",")+"\r\n")
}

// TestReceiptsExportRefusals: what the command refuses prints nothing and
// writes no file: the account of --as (none, unknown, below office), the
// year, JSON and --force without --file.
func TestReceiptsExportRefusals(t *testing.T) {
	t.Parallel()

	const year = "--year=2026"

	for name, tc := range map[string]struct {
		env    map[string]string
		args   []string
		code   int
		stderr string
	}{
		"no --as": {
			args: []string{year}, code: 2,
			stderr: "error: studio-schedule receipts export acts as an account: give --as <email> or set STUDIO_AS\n",
		},
		"unknown --as": {
			args: []string{"--as", "nobody@example.org", year}, code: 3,
			stderr: "error: --as nobody@example.org: not found: no account has this email\n",
		},
		"--as a member": {
			args: []string{"--as", demoMember, year}, code: 4,
			stderr: "error: --as " + demoMember + ": forbidden: the account's role member is below office, " +
				"which this command needs\n",
		},
		"--as an instructor": {args: []string{"--as", demoInstructor, year}, code: 4},
		"STUDIO_AS a member": {env: map[string]string{"STUDIO_AS": demoMember}, args: []string{year}, code: 4},
		"no --year":          {args: []string{"--as", demoOffice}, code: 2},
		"--year 0": {
			args: []string{"--as", demoOffice, "--year", "0"}, code: 2,
			stderr: "error: --year: the year must lie between 1 and 9999\n",
		},
		"--year 10000": {
			args: []string{"--as", demoOffice, "--year", "10000"}, code: 2,
			stderr: "error: --year: the year must lie between 1 and 9999\n",
		},
		"--output json": {
			args: []string{"--as", demoOffice, year, "--output", "json"}, code: 2,
			stderr: "error: --output json: receipts export writes CSV only\n",
		},
		"--force without --file": {
			args: []string{"--as", demoOffice, year, "--force"}, code: 2,
			stderr: "error: --force: give --file too\n",
		},
		"an argument": {args: []string{"--as", demoOffice, year, "2026"}, code: 2},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			// A --file the refusal must not create.
			file := filepath.Join(t.TempDir(), "receipts.csv")
			args := append([]string{"receipts", "export", "--file", file}, tc.args...)

			if name == "--force without --file" {
				args = append([]string{"receipts", "export"}, tc.args...)
			}

			got := cliRun{st: demoStore(t), env: tc.env}.exec(t, args...)
			if got.code != tc.code || got.stdout != "" || (tc.stderr != "" && !rexportTStderr(got.stderr, tc.stderr)) {
				t.Errorf("exit %d, stdout %q, stderr %q; want exit %d, stderr %q",
					got.code, got.stdout, got.stderr, tc.code, tc.stderr)
			}

			if _, err := os.Stat(file); !os.IsNotExist(err) {
				t.Errorf("the refused export wrote %s (%v)", file, err)
			}
		})
	}
}

// rexportTStderr reports whether stderr is want, or, for --output json, a
// problem whose detail is want's message.
func rexportTStderr(stderr, want string) bool {
	msg := strings.TrimSuffix(strings.TrimPrefix(want, "error: "), "\n")

	return stderr == want || strings.Contains(stderr, strconv.Quote(msg))
}

// TestReceiptsExportCSVText: receiptCSVText puts an apostrophe before a
// cell that starts with =, +, -, @, a tab or a carriage return, and leaves
// any other cell, and the empty one, as it is.
func TestReceiptsExportCSVText(t *testing.T) {
	t.Parallel()

	for in, want := range map[string]string{
		"=SUM(A1:A2)":   "'=SUM(A1:A2)",
		"+49 30 000":    "'+49 30 000",
		"-1+2":          "'-1+2",
		"@cmd":          "'@cmd",
		"\tTabbed":      "'\tTabbed",
		"\rReturned":    "'\rReturned",
		"Ida Example":   "Ida Example",
		"Ida =Example":  "Ida =Example",
		"'quoted":       "'quoted",
		"":              "",
		"1800":          "1800",
		" =leading gap": " =leading gap",
	} {
		if got := receiptCSVText(in); got != want {
			t.Errorf("receiptCSVText(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestReceiptsExportCSVAmounts: receiptCSV writes the amounts of a storno
// as plain negative numbers, unquoted and without an apostrophe, the rate
// as a percentage, and an empty cancels cell for a sale.
func TestReceiptsExportCSVAmounts(t *testing.T) {
	t.Parallel()

	line := domain.NewReceiptLine(1, "-Fictional pass", 1, 1800, 700, domain.TaxStandard)
	sale := domain.Receipt{
		Number: "SD-2026-0001", Kind: domain.ReceiptSale, IssueDate: domain.Date{Year: 2026, Month: 9, Day: 2},
		RecipientName: "Ida Example", PaymentMethod: domain.PaymentCash, TaxMode: domain.TaxStandard, Currency: "EUR",
		Lines: []domain.ReceiptLine{line},
	}
	storno := sale
	storno.Number, storno.Kind, storno.Lines = "SD-2026-0002", domain.ReceiptCancellation, []domain.ReceiptLine{line.Negated()}

	data, n := receiptCSV([]service.ReceiptView{{Receipt: sale}, {Receipt: storno, Cancels: &sale}})

	want := strings.Join(receiptCSVHeader, ",") + "\r\n" +
		"SD-2026-0001,2026-09-02,receipt,,Ida Example,cash,standard,1,'-Fictional pass,1,7.00,1682,118,1800,16.82,1.18,18.00,EUR\r\n" +
		"SD-2026-0002,2026-09-02,storno,SD-2026-0001,Ida Example,cash,standard,1,'-Fictional pass,1,7.00,-1682,-118,-1800,-16.82,-1.18,-18.00,EUR\r\n"
	if n != 2 || string(data) != want {
		t.Errorf("receiptCSV = %d lines\n%q\nwant 2\n%q", n, data, want)
	}
}

// TestReceiptsExportPercent: receiptPercent writes basis points as a
// percentage with two decimals.
func TestReceiptsExportPercent(t *testing.T) {
	t.Parallel()

	for bp, want := range map[int]string{0: "0.00", 700: "7.00", 1900: "19.00", 550: "5.50", 5: "0.05", 10000: "100.00"} {
		if got := receiptPercent(bp); got != want {
			t.Errorf("receiptPercent(%d) = %q, want %q", bp, got, want)
		}
	}
}

// TestReceiptsExportUnwritableFile: a --file the command cannot create (its
// directory does not exist) fails the export with the error: status 1.
func TestReceiptsExportUnwritableFile(t *testing.T) {
	t.Parallel()

	file := filepath.Join(t.TempDir(), "missing", "receipts.csv")

	got := cliRun{st: demoStore(t)}.exec(t, "receipts", "export", "--as", demoOffice, "--year", "2026", "--file", file)
	if got.code != 1 || got.stdout != "" || got.stderr != "error: open "+file+": no such file or directory\n" {
		t.Errorf("exit %d, stdout %q, stderr %q; want the error of the file", got.code, got.stdout, got.stderr)
	}
}
