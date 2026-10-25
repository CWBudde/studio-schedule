package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// operationsDoc holds the configuration reference.
const operationsDoc = "../../docs/operations.md"

// documentedOnly are variables the reference explains without the app
// reading them itself: the cache directory comes from os.UserCacheDir.
var documentedOnly = []string{"XDG_CACHE_HOME"}

// TestConfigurationReference fails when docs/operations.md and the code
// disagree: every environment variable of internal/config (its Env*
// constants) must be in the table "Environment variables", every flag of
// every command in the table "Commands and flags", and neither table may
// name a variable or flag that the code does not have.
func TestConfigurationReference(t *testing.T) {
	t.Parallel()

	doc, err := os.ReadFile(operationsDoc)
	if err != nil {
		t.Fatal(err)
	}

	// Environment variables: the first column, as `NAME`.
	documented := map[string]bool{}
	for _, row := range tableRows(t, string(doc), "Environment variables") {
		documented[codeSpan(t, row[0])] = true
	}

	for _, name := range append(configEnvVars(t), documentedOnly...) {
		if !documented[name] {
			t.Errorf("%s: the table \"Environment variables\" lacks %s", operationsDoc, name)
		}

		delete(documented, name)
	}

	for name := range documented {
		t.Errorf("%s: the table \"Environment variables\" names %s, which the code does not read", operationsDoc, name)
	}

	// Flags: the command (its first word) and the flag, as `--name`.
	flags := map[string]bool{}
	for _, row := range tableRows(t, string(doc), "Commands and flags") {
		if len(row) < 2 {
			t.Fatalf("%s: row %q has no flag column", operationsDoc, row)
		}

		command, _, _ := strings.Cut(codeSpan(t, row[0]), " ")
		flags[command+" "+codeSpan(t, row[1])] = true
	}

	for _, f := range commandFlags(newRootCmd(nil)) {
		if !flags[f] {
			t.Errorf("%s: the table \"Commands and flags\" lacks %s", operationsDoc, f)
		}

		delete(flags, f)
	}

	for f := range flags {
		t.Errorf("%s: the table \"Commands and flags\" names %s, which no command has", operationsDoc, f)
	}
}

// tableRows returns the cells of the body rows of the first Markdown table
// after the heading named heading.
func tableRows(t *testing.T, doc, heading string) [][]string {
	t.Helper()

	var (
		rows               [][]string
		inSection, inTable bool
	)

	for line := range strings.Lines(doc) {
		line = strings.TrimSpace(line)

		switch {
		case strings.HasPrefix(line, "#"):
			if inTable {
				return rows
			}

			inSection = strings.TrimSpace(strings.TrimLeft(line, "#")) == heading
		case inSection && strings.HasPrefix(line, "|"):
			inTable = true

			cells := strings.Split(strings.Trim(line, "|"), "|")
			for i := range cells {
				cells[i] = strings.TrimSpace(cells[i])
			}

			if strings.Trim(cells[0], "-: ") == "" || !strings.HasPrefix(cells[0], "`") {
				continue // the separator, or the header row
			}

			rows = append(rows, cells)
		case inTable:
			return rows
		}
	}

	if len(rows) == 0 {
		t.Fatalf("%s: no table under the heading %q", operationsDoc, heading)
	}

	return rows
}

// codeSpan returns the text of the code span that cell starts with.
func codeSpan(t *testing.T, cell string) string {
	t.Helper()

	parts := strings.SplitN(cell, "`", 3)
	if len(parts) < 3 || parts[0] != "" {
		t.Fatalf("%s: cell %q does not start with a code span", operationsDoc, cell)
	}

	return parts[1]
}

// configEnvVars returns the values of the Env* constants of
// internal/config, read from its source.
func configEnvVars(t *testing.T) []string {
	t.Helper()

	f, err := parser.ParseFile(token.NewFileSet(), "../../internal/config/config.go", nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}

	var names []string

	for _, decl := range f.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}

		for _, spec := range gen.Specs {
			vs := spec.(*ast.ValueSpec) //nolint:forcetypeassert // const specs are value specs

			for i, name := range vs.Names {
				if !strings.HasPrefix(name.Name, "Env") || i >= len(vs.Values) {
					continue
				}

				lit, ok := vs.Values[i].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					t.Fatalf("config.%s is not a string literal", name.Name)
				}

				v, err := strconv.Unquote(lit.Value)
				if err != nil {
					t.Fatal(err)
				}

				names = append(names, v)
			}
		}
	}

	if len(names) == 0 {
		t.Fatal("no Env* constants in internal/config")
	}

	return names
}

// commandFlags returns "command --flag" for every flag that a command
// (or its subcommands, under the top-level command's name) defines, the
// help flags left out.
func commandFlags(root *cobra.Command) []string {
	var out []string

	var walk func(top string, cmd *cobra.Command)

	walk = func(top string, cmd *cobra.Command) {
		cmd.LocalFlags().VisitAll(func(f *pflag.Flag) {
			if f.Name != "help" {
				out = append(out, top+" --"+f.Name)
			}
		})

		for _, sub := range cmd.Commands() {
			walk(top, sub)
		}
	}

	for _, cmd := range root.Commands() {
		walk(cmd.Name(), cmd)
	}

	slices.Sort(out)

	return slices.Compact(out)
}
