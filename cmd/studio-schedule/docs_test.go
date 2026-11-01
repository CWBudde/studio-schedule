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
// constants) must be in the table "Environment variables", with the flag
// that config.Load pairs it with (or none) in its column "Flag", every
// global flag (the root's persistent flags) in the table "Global flags",
// with that variable (or none) in its second column, every flag of every
// command in the table "Commands and flags", and no table may name a
// variable or flag that the code does not have.
func TestConfigurationReference(t *testing.T) {
	t.Parallel()

	doc, err := os.ReadFile(operationsDoc)
	if err != nil {
		t.Fatal(err)
	}

	src := parseConfigSource(t)
	envVars, pairs := configEnvVars(t, src), configFlagPairs(t, src)

	// Environment variables: the first column, as `NAME`, and the flag in
	// the second, as `--name` or a dash.
	documented := map[string]string{}
	for _, row := range tableRows(t, string(doc), "Environment variables") {
		if len(row) < 2 {
			t.Fatalf("%s: row %q has no flag column", operationsDoc, row)
		}

		documented[codeSpan(t, row[0])] = optionalCell(t, row[1])
	}

	for _, name := range append(envVars, documentedOnly...) {
		flag, ok := documented[name]
		if !ok {
			t.Errorf("%s: the table \"Environment variables\" lacks %s", operationsDoc, name)
		}

		if want := pairs[name]; ok && flag != want {
			t.Errorf("%s: the table \"Environment variables\" gives %s the flag %q, but the code pairs it with %q",
				operationsDoc, name, flag, want)
		}

		delete(documented, name)
	}

	for name := range documented {
		t.Errorf("%s: the table \"Environment variables\" names %s, which the code does not read", operationsDoc, name)
	}

	root := newRootCmd(nil)

	// Global flags: the flag, as `--name`, and the variable that config.Load
	// pairs it with, as `NAME` or a dash.
	envOf := map[string]string{}
	for env, flag := range pairs {
		envOf[flag] = env
	}

	globals := map[string]string{}
	for _, row := range tableRows(t, string(doc), "Global flags") {
		if len(row) < 2 {
			t.Fatalf("%s: row %q has no variable column", operationsDoc, row)
		}

		globals[codeSpan(t, row[0])] = optionalCell(t, row[1])
	}

	root.PersistentFlags().VisitAll(func(f *pflag.Flag) {
		name := "--" + f.Name

		env, ok := globals[name]
		if !ok {
			t.Errorf("%s: the table \"Global flags\" lacks %s", operationsDoc, name)
		}

		if want := envOf[name]; ok && env != want {
			t.Errorf("%s: the table \"Global flags\" gives %s the variable %q, but the code pairs it with %q",
				operationsDoc, name, env, want)
		}

		delete(globals, name)
	})

	for name := range globals {
		t.Errorf("%s: the table \"Global flags\" names %s, which is no global flag", operationsDoc, name)
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

	for _, f := range commandFlags(root) {
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
// after the heading named heading. Lines in fenced code blocks are no
// headings (a shell comment starts with # too).
func tableRows(t *testing.T, doc, heading string) [][]string {
	t.Helper()

	var (
		rows                       [][]string
		inSection, inTable, inCode bool
		fence                      string
	)

	for line := range strings.Lines(doc) {
		line = strings.TrimSpace(line)

		if marker := codeFence(line); marker != "" && (!inCode || strings.HasPrefix(line, fence)) {
			if inTable {
				return rows
			}

			inCode, fence = !inCode, marker

			continue
		}

		switch {
		case inCode:
			continue
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

// codeFence returns the fence (``` or ~~~, possibly longer) that line
// opens or closes a fenced code block with, or "".
func codeFence(line string) string {
	for _, c := range []string{"`", "~"} {
		n := len(line) - len(strings.TrimLeft(line, c))
		if n >= 3 {
			return strings.Repeat(c, n)
		}
	}

	return ""
}

// optionalCell returns the text of the code span of a cell that names a
// flag or variable, or "" for a dash (none).
func optionalCell(t *testing.T, cell string) string {
	t.Helper()

	switch cell {
	case "", "-", "–", "—":
		return ""
	}

	return codeSpan(t, cell)
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

// configSource is the parsed source of internal/config.
const configSource = "../../internal/config/config.go"

func parseConfigSource(t *testing.T) *ast.File {
	t.Helper()

	f, err := parser.ParseFile(token.NewFileSet(), configSource, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}

	return f
}

// configEnvVars returns the values of the Env* constants of
// internal/config, read from its source f.
func configEnvVars(t *testing.T, f *ast.File) []string {
	t.Helper()

	var names []string

	for name, v := range configStrings(t, f) {
		if strings.HasPrefix(name, "Env") {
			names = append(names, v)
		}
	}

	if len(names) == 0 {
		t.Fatal("no Env* constants in internal/config")
	}

	slices.Sort(names)

	return names
}

// configFlagPairs returns, by environment variable, the flag ("--name")
// that config.Load reads before it: the calls pick(flags.X, FlagY, EnvZ,
// default) in the source f.
func configFlagPairs(t *testing.T, f *ast.File) map[string]string {
	t.Helper()

	consts := configStrings(t, f)
	pairs := map[string]string{}

	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) != 4 {
			return true
		}

		if fn, ok := call.Fun.(*ast.Ident); !ok || fn.Name != "pick" {
			return true
		}

		flag, okFlag := call.Args[1].(*ast.Ident)
		env, okEnv := call.Args[2].(*ast.Ident)

		if okFlag && okEnv && strings.HasPrefix(flag.Name, "Flag") && strings.HasPrefix(env.Name, "Env") {
			pairs[consts[env.Name]] = "--" + consts[flag.Name]
		}

		return true
	})

	if len(pairs) == 0 {
		t.Fatal("no pick(flag, FlagX, EnvY, …) calls in internal/config")
	}

	return pairs
}

// configStrings returns the string constants of the source f by name.
func configStrings(t *testing.T, f *ast.File) map[string]string {
	t.Helper()

	out := map[string]string{}

	for _, decl := range f.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}

		for _, spec := range gen.Specs {
			vs := spec.(*ast.ValueSpec) //nolint:forcetypeassert // const specs are value specs

			for i, name := range vs.Names {
				if i >= len(vs.Values) {
					continue
				}

				lit, ok := vs.Values[i].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					if strings.HasPrefix(name.Name, "Env") || strings.HasPrefix(name.Name, "Flag") {
						t.Fatalf("config.%s is not a string literal", name.Name)
					}

					continue
				}

				v, err := strconv.Unquote(lit.Value)
				if err != nil {
					t.Fatal(err)
				}

				out[name.Name] = v
			}
		}
	}

	return out
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
