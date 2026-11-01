package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"unicode"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/cwbudde/studio-schedule/cmd/studio-schedule/cli"
	"github.com/cwbudde/studio-schedule/internal/config"
	"github.com/cwbudde/studio-schedule/internal/httpapi"
)

// cliDoc is the command line reference, generated from the command tree.
const cliDoc = "../../docs/cli.md"

// exitSource holds the exit codes, whose doc comments are their meanings.
const exitSource = "cli/exit.go"

// TestCLIReference renders the command tree of newRootCmd as Markdown and
// fails when docs/cli.md differs; with -update (just cli-docs) it writes
// the file. The reference is generic: every command, flag and annotation
// of the tree appears without a change here.
func TestCLIReference(t *testing.T) {
	t.Parallel()

	got := cliReference(t, newRootCmd(nil))

	if *update {
		if err := os.WriteFile(cliDoc, []byte(got), 0o600); err != nil {
			t.Fatal(err)
		}

		return
	}

	want, err := os.ReadFile(cliDoc)
	if err != nil {
		t.Fatalf("%v (run just cli-docs)", err)
	}

	if got != string(want) {
		gotLines, wantLines := strings.Split(got, "\n"), strings.Split(string(want), "\n")

		line := 0
		for line < len(gotLines) && line < len(wantLines) && gotLines[line] == wantLines[line] {
			line++
		}

		t.Errorf("%s is stale (run just cli-docs); line %d is\n%q\nbut the command tree gives\n%q",
			cliDoc, line+1, lineAt(wantLines, line), lineAt(gotLines, line))
	}
}

// lineAt returns lines[i], or "" past the end.
func lineAt(lines []string, i int) string {
	if i < len(lines) {
		return lines[i]
	}

	return ""
}

// cliReference is the Markdown of docs/cli.md for the tree of root. Its
// shape keeps prettier (treefmt) from changing it: headings, paragraphs,
// "-" lists and fenced code blocks, with text that Markdown would read as
// markup put in code spans (prose).
func cliReference(t *testing.T, root *cobra.Command) string {
	t.Helper()

	r := &reference{values: flagValues(root)}

	var blocks []string

	add := func(b ...string) { blocks = append(blocks, b...) }

	add("# Command line reference",
		"<!-- Generated from the command tree by TestCLIReference (cmd/studio-schedule/clidocs_test.go). "+
			"Do not edit: change the commands and run `just cli-docs`. -->",
		"Every command of the `"+root.Name()+"` binary with its flags, its output and its exit codes. A "+
			"command runs locally on the server's shell: it opens the database of `"+config.EnvDatabaseURL+
			"` or `--"+config.FlagDatabaseURL+"`, checks its schema as `serve` does and calls the same "+
			"services as the HTTP API, so a command and its operation share their rules, validation and "+
			"audit. There is no remote mode ([ADR 0020](decisions/0020-command-line.md)). The server's "+
			"configuration is in [operations.md](operations.md#configuration-reference). Every command "+
			"also explains itself with `--help`.",
		"- [Global flags](#global-flags)\n"+
			"- [Environment variables](#environment-variables)\n"+
			"- [Output](#output)\n"+
			"- [Confirmation](#confirmation)\n"+
			"- [Exit codes](#exit-codes)\n"+
			"- [Commands](#commands)")

	add("## Global flags",
		"They go before or after the command: `"+root.Name()+" --output json <command>` is `"+
			root.Name()+" <command> -o json`. A command that acts as no account refuses `--"+config.FlagAs+
			"`, and one that prints text only refuses `--"+cli.FlagOutput+" json`.",
		r.flagList(root.PersistentFlags()))

	add("## Environment variables", envList(t, root))

	add("## Output",
		"**Text** is the default: one record per line, with a header line only when stdout is a "+
			"terminal, and dates and times in the school's time zone, never the machine's. Color only on "+
			"a terminal and without `"+config.EnvNoColor+"`.",
		"**`--output json`** prints one JSON document on stdout. Where a command mirrors an API "+
			"operation, the document is the operation's response body, with the field names and types of "+
			"its schema in [api/openapi.json](../api/openapi.json). A command without an operation "+
			"documents its schema below, under the command. The schema is stable: fields are only added, "+
			"and a rename or a removal waits for a major release. A command that prints text only refuses "+
			"`--output json`.",
		"**Errors** go to stderr as one line, `error: …`. An invalid value names the flag the way the "+
			"command spells it (`--email: …`, or `key=value` for a setting). With `--output json`, stderr "+
			"gets the error as a problem document like the API's, with `title`, `status`, `detail` and "+
			"`errors`. A command whose report on stdout says what failed (a check with findings, an import "+
			"with failed rows) exits with status "+strconv.Itoa(cli.ExitFailed)+" without one.")

	add("## Confirmation",
		"A command that deletes, anonymizes, mails, cancels or merges asks for confirmation when stdin "+
			"is a terminal (`[y/N]`), and needs `--"+cli.FlagYes+"` otherwise. Without a terminal and "+
			"without `--"+cli.FlagYes+"`, it refuses with exit code "+strconv.Itoa(cli.ExitUsage)+
			" and changes nothing. A secret or a search term is never an argument: the command reads it "+
			"from stdin, after a prompt on a terminal.")

	add("## Exit codes", r.exitList(t))

	add("## Commands", r.commandList(root))

	for _, cmd := range subcommands(root) {
		add(r.commandBlocks(cmd, 3)...)
	}

	return strings.Join(blocks, "\n\n") + "\n"
}

// reference renders the reference of one command tree.
type reference struct {
	// values holds, by name, the usage and default of each flag of the
	// tree that takes a value: prose puts a value it names (--output json)
	// in the flag's code span.
	values map[string]string
}

// flagValues returns, by name, the usages and defaults of the flags of
// the tree of root that take a value. Asking for a command's local flags
// merges its parents' persistent ones into it, which cobra does lazily, so
// every usage line then has its [flags], as --help shows it.
func flagValues(root *cobra.Command) map[string]string {
	values := map[string]string{}

	var walk func(cmd *cobra.Command)

	walk = func(cmd *cobra.Command) {
		visit := func(f *pflag.Flag) {
			if f.NoOptDefVal == "" {
				values[f.Name] += " " + f.Usage + " " + f.DefValue
			}
		}

		cmd.LocalNonPersistentFlags().VisitAll(visit)
		cmd.PersistentFlags().VisitAll(visit)

		for _, sub := range cmd.Commands() {
			walk(sub)
		}
	}

	walk(root)

	return values
}

// subcommands are the documented subcommands of cmd: neither help nor
// hidden ones.
func subcommands(cmd *cobra.Command) []*cobra.Command {
	var out []*cobra.Command

	for _, sub := range cmd.Commands() {
		if sub.Hidden || sub.Name() == "help" {
			continue
		}

		out = append(out, sub)
	}

	return out
}

// commandList links the subcommands of cmd with their Short.
func (r *reference) commandList(cmd *cobra.Command) string {
	var items []string

	for _, sub := range subcommands(cmd) {
		items = append(items, "- [`"+sub.CommandPath()+"`](#"+anchor(sub)+"): "+r.prose(sub.Short))
	}

	return strings.Join(items, "\n")
}

// anchor is the fragment of a command's heading: its path in lower case,
// with hyphens for the spaces (GitHub's heading IDs).
func anchor(cmd *cobra.Command) string {
	return strings.ReplaceAll(strings.ToLower(cmd.CommandPath()), " ", "-")
}

// commandBlocks renders cmd at heading level, then its subcommands one
// level below.
func (r *reference) commandBlocks(cmd *cobra.Command, level int) []string {
	blocks := []string{strings.Repeat("#", min(level, 6)) + " `" + cmd.CommandPath() + "`"}

	if cmd.Short != "" {
		blocks = append(blocks, sentence(r.prose(cmd.Short)))
	}

	usage := cmd.UseLine()
	if len(subcommands(cmd)) > 0 {
		usage = cmd.CommandPath() + " <command> [flags]"
	}

	blocks = append(blocks, fence("text", []string{usage}))
	blocks = append(blocks, r.longBlocks(cmd.Long)...)

	if example := strings.Trim(cmd.Example, "\n"); example != "" {
		blocks = append(blocks, "Examples:", fence("sh", dedent(strings.Split(example, "\n"))))
	}

	if flags := r.flagList(cmd.LocalFlags()); flags != "" {
		blocks = append(blocks, "Flags:", flags)
	}

	blocks = append(blocks, r.commandFacts(cmd)...)

	if len(subcommands(cmd)) > 0 {
		blocks = append(blocks, "Commands:", r.commandList(cmd))
	}

	for _, sub := range subcommands(cmd) {
		blocks = append(blocks, r.commandBlocks(sub, level+1)...)
	}

	return blocks
}

// commandFacts are paragraphs on the account cmd acts as, whether it asks
// for confirmation and what it prints with --output json, from its
// annotations (cli.Spec). A group of commands has none of its own.
func (r *reference) commandFacts(cmd *cobra.Command) []string {
	if len(subcommands(cmd)) > 0 || !cmd.Runnable() {
		return nil
	}

	var facts []string

	if access, ok := cmd.Annotations[cli.AnnotationAccess]; ok {
		role, _ := httpapi.Access(access).MinRole()
		facts = append(facts, "Access: `"+access+"`. Acts as the account of `--"+config.FlagAs+"` (or `"+
			config.EnvAs+"`), which needs the role `"+string(role)+"` or above.")
	} else {
		facts = append(facts, "Access: an operator command. It acts as no account and refuses `--"+
			config.FlagAs+"`.")
	}

	if cmd.Annotations[cli.AnnotationDestructive] == "true" {
		facts = append(facts, "Destructive: asks for confirmation, or needs `--"+cli.FlagYes+"` (see "+
			"[Confirmation](#confirmation)).")
	}

	switch json, ok := cmd.Annotations[cli.AnnotationJSON]; {
	case !ok:
		facts = append(facts, "Output: text only; `--"+cli.FlagOutput+" json` is refused.")
	case json != "":
		facts = append(facts, "Output with `--"+cli.FlagOutput+" json`: "+sentence(r.prose(json)))
	}

	return facts
}

// flagList lists the flags of fs: name, shorthand, type, default and
// usage. The help flag and hidden flags are left out.
func (r *reference) flagList(fs *pflag.FlagSet) string {
	var items []string

	fs.VisitAll(func(f *pflag.Flag) {
		if f.Hidden || f.Name == "help" {
			return
		}

		item := "- `--" + f.Name + "`"
		if f.Shorthand != "" {
			item += ", `-" + f.Shorthand + "`"
		}

		item += " (" + f.Value.Type()

		switch f.DefValue {
		case "", "false", "[]":
		default:
			item += ", default `" + f.DefValue + "`"
		}

		items = append(items, item+"): "+r.prose(f.Usage))
	})

	return strings.Join(items, "\n")
}

// envRef finds "(env NAME)" in a flag's usage.
var envRef = regexp.MustCompile(`\(env ([A-Z][A-Z0-9_]*)\)`)

// envList lists the environment variables of the global flags, named in
// their usage as "(env NAME)", and NO_COLOR, which the command line reads
// without a flag.
func envList(t *testing.T, root *cobra.Command) string {
	t.Helper()

	var items []string

	root.PersistentFlags().VisitAll(func(f *pflag.Flag) {
		if m := envRef.FindStringSubmatch(f.Usage); m != nil {
			items = append(items, "- `"+m[1]+"`: as `--"+f.Name+"`, which wins over it.")
		}
	})

	if len(items) == 0 {
		t.Fatal("no global flag names an environment variable as (env NAME)")
	}

	items = append(items, "- `"+config.EnvNoColor+"`: when set and not empty, the text output has no "+
		"colors ([no-color.org](https://no-color.org)).")

	return strings.Join(items, "\n") + "\n\nAn empty variable counts as unset. Each command's own flags " +
		"name the variable they stand for in the same way."
}

// exitList lists the exit codes with their meanings: the values and doc
// comments of the Exit* constants of cli/exit.go.
func (r *reference) exitList(t *testing.T) string {
	t.Helper()

	f, err := parser.ParseFile(token.NewFileSet(), exitSource, nil, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}

	type code struct {
		value   int
		meaning string
	}

	var codes []code

	for _, decl := range f.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}

		for _, spec := range gen.Specs {
			vs := spec.(*ast.ValueSpec) //nolint:forcetypeassert // const specs are value specs

			name := vs.Names[0].Name
			if !strings.HasPrefix(name, "Exit") || len(vs.Values) != 1 || vs.Doc == nil {
				continue
			}

			lit, ok := vs.Values[0].(*ast.BasicLit)
			if !ok || lit.Kind != token.INT {
				t.Fatalf("%s: %s is not an integer literal", exitSource, name)
			}

			value, err := strconv.Atoi(lit.Value)
			if err != nil {
				t.Fatal(err)
			}

			meaning, ok := strings.CutPrefix(strings.Join(strings.Fields(vs.Doc.Text()), " "), name+": ")
			if !ok {
				t.Fatalf("%s: the comment of %s does not start with %q", exitSource, name, name+": ")
			}

			codes = append(codes, code{value, strings.ToUpper(meaning[:1]) + meaning[1:]})
		}
	}

	// The values the binary returns, so a constant the parse misses fails.
	want := []int{cli.ExitOK, cli.ExitFailed, cli.ExitUsage, cli.ExitNotFound, cli.ExitConflict}

	slices.SortFunc(codes, func(a, b code) int { return a.value - b.value })

	items := make([]string, 0, len(codes))
	values := make([]int, 0, len(codes))

	for _, c := range codes {
		items = append(items, "- `"+strconv.Itoa(c.value)+"`: "+r.prose(c.meaning))
		values = append(values, c.value)
	}

	slices.Sort(want)

	if !slices.Equal(values, want) {
		t.Fatalf("%s: the documented exit codes %v are not the cli.Exit* constants %v", exitSource, values, want)
	}

	return strings.Join(items, "\n")
}

// longBlocks renders a command's Long: paragraphs of prose, "- " lists,
// indented lines as code blocks, and a Markdown heading line (cobra's
// completion help has them) as a paragraph of its own.
func (r *reference) longBlocks(long string) []string {
	var (
		blocks []string
		run    []string
		kind   string
	)

	flush := func() {
		switch kind {
		case "prose":
			blocks = append(blocks, r.prose(strings.Join(run, " ")))
		case "list":
			blocks = append(blocks, strings.Join(run, "\n"))
		case "code":
			blocks = append(blocks, fence("text", dedent(run)))
		}

		run, kind = nil, ""
	}

	for line := range strings.SplitSeq(strings.Trim(long, "\n"), "\n") {
		trimmed := strings.TrimSpace(line)

		var k string

		switch {
		case trimmed == "":
			flush()

			continue
		case strings.HasPrefix(line, "\t") || strings.HasPrefix(line, "  "):
			k = "code"
		case strings.HasPrefix(trimmed, "#"):
			flush()

			blocks = append(blocks, r.prose(strings.TrimSpace(strings.TrimLeft(trimmed, "#"))))

			continue
		case strings.HasPrefix(trimmed, "- "):
			k, line = "list", "- "+r.prose(strings.TrimPrefix(trimmed, "- "))
		default:
			k = "prose"
		}

		if k != kind {
			flush()
		}

		run, kind = append(run, line), k
	}

	flush()

	return blocks
}

// sentence ends text with a full stop unless it ends with punctuation.
func sentence(text string) string {
	if strings.HasSuffix(text, ".") || strings.HasSuffix(text, "!") || strings.HasSuffix(text, "?") {
		return text
	}

	return text + "."
}

// fence is a fenced code block of lines.
func fence(lang string, lines []string) string {
	return "```" + lang + "\n" + strings.Join(lines, "\n") + "\n```"
}

// dedent removes the leading white space that all lines share, and the
// trailing white space of each.
func dedent(lines []string) []string {
	var (
		prefix string
		seen   bool
	)

	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}

		indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
		if !seen {
			prefix, seen = indent, true

			continue
		}

		for !strings.HasPrefix(indent, prefix) {
			prefix = prefix[:len(prefix)-1]
		}
	}

	out := make([]string, len(lines))
	for i, line := range lines {
		out[i] = strings.TrimRight(strings.TrimPrefix(line, prefix), " \t")
	}

	return out
}

// markup are the characters Markdown (or prettier) could read as markup
// within a word.
const markup = "<>*_[]\\|~"

// prose returns text as one Markdown line that renders as written: its
// white space collapsed, and a word with markup characters (STUDIO_AS,
// <email>, [y/N]) or one that would start a list, a quote or a heading put
// in a code span, as is a flag (with its value: --output json) or a Go
// identifier. Code spans of text stay as they are.
func (r *reference) prose(text string) string {
	words := strings.Fields(text)
	out := make([]string, 0, len(words))
	inCode := false

	for i := 0; i < len(words); i++ {
		w := words[i]

		if ticks := strings.Count(w, "`"); inCode || ticks > 0 {
			if ticks%2 == 1 {
				inCode = !inCode
			}

			out = append(out, w)

			continue
		}

		if value, trail, ok := r.flagValue(w, words[i+1:]); ok {
			lead, core, _ := splitWord(w)
			out = append(out, lead+"`"+core+" "+value+"`"+trail)
			i++

			continue
		}

		if strings.ContainsAny(w, markup) || i == 0 && startsBlock(w) || isCode(w) {
			w = codeWord(w)
		}

		out = append(out, w)
	}

	return strings.Join(out, " ")
}

// flagValue returns the value that follows the flag w in rest, with the
// punctuation after it, if w is a flag of the tree that takes a value, is
// not followed by punctuation itself, and the next word looks like its
// value: it has one of "=@/:" in it (--map memberNo=Nr) or is a word of
// the flag's usage or default (--output json, --anchor today).
func (r *reference) flagValue(w string, rest []string) (value, trail string, ok bool) {
	_, core, after := splitWord(w)

	name, isFlag := strings.CutPrefix(core, "--")
	usage, takesValue := r.values[name]

	if !isFlag || !takesValue || after != "" || len(rest) == 0 || strings.Contains(rest[0], "`") {
		return "", "", false
	}

	lead, value, trail := splitWord(rest[0])
	if lead != "" || value == "" || strings.HasPrefix(value, "-") {
		return "", "", false
	}

	if !strings.ContainsAny(value, "=@/:") && !slices.Contains(strings.FieldsFunc(usage, notWordChar), value) {
		return "", "", false
	}

	return value, trail, true
}

// notWordChar separates the words of a usage.
func notWordChar(c rune) bool {
	return !unicode.IsLetter(c) && !unicode.IsDigit(c) && c != '-'
}

// splitWord splits a word into the punctuation before it, its core and
// the punctuation after it.
func splitWord(w string) (lead, core, trail string) {
	core = strings.TrimLeft(w, "(\"'")
	lead = w[:len(w)-len(core)]
	trimmed := strings.TrimRight(core, ".,;:)!?\"'")

	return lead, trimmed, core[len(trimmed):]
}

// codeLike are words that read best as code: a flag (--as) or a
// qualified Go identifier (domain.ErrNotFound).
var codeLike = regexp.MustCompile(`^(--[a-z][a-z0-9-]*|[a-z]+\.[A-Z][A-Za-z0-9]*)$`)

// isCode reports whether w, the punctuation around it left out, is
// codeLike.
func isCode(w string) bool {
	_, core, _ := splitWord(w)

	return codeLike.MatchString(core)
}

// startsBlock reports whether a word at the start of a line would start a
// Markdown block other than a paragraph.
func startsBlock(w string) bool {
	if strings.ContainsAny(w[:1], "#>-+=") {
		return true
	}

	digits := strings.TrimLeft(w, "0123456789")

	return digits != w && (digits == "." || digits == ")")
}

// codeWord puts a word in a code span, the punctuation around it left
// outside.
func codeWord(w string) string {
	lead, core, trail := splitWord(w)
	if core == "" {
		return "`" + w + "`"
	}

	return lead + "`" + core + "`" + trail
}
