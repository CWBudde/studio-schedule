// Command coverage reports the Go test coverage of a cover profile written by
//
//	go test -coverpkg=./... -coverprofile=coverage.out ./...
//
// With -coverpkg every test binary lists every block of the module, so a block
// appears once per test binary; it counts as covered when any of them ran it.
// Test-support and generated packages are left out (see excluded), so the
// total is the coverage of the code that ships. The report is the total, then
// the packages with the most uncovered statements; -markdown writes it as a
// table for a GitHub job summary.
//
// Usage: go run ./tools/coverage [-markdown] [-top n] coverage.out
package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

const module = "github.com/cwbudde/studio-schedule/"

// excluded matches the package paths (relative to the module) that do not count:
// shared test suites, generated code and this tool.
var excluded = regexp.MustCompile(
	`^(internal/store/storetest|internal/service/servicetest|internal/store/postgres/internal/sqlc|tools/.*)$`,
)

// Package is the coverage of one package.
type Package struct {
	Path                string
	Statements, Covered int
}

// Uncovered returns the number of statements no test ran.
func (p Package) Uncovered() int { return p.Statements - p.Covered }

// Percent returns the covered share in percent (100 for a package without statements).
func (p Package) Percent() float64 {
	if p.Statements == 0 {
		return 100
	}
	return 100 * float64(p.Covered) / float64(p.Statements)
}

// Report is the coverage of the counted packages, the most uncovered first.
type Report struct {
	Total    Package
	Packages []Package
}

func main() {
	markdown := flag.Bool("markdown", false, "write a Markdown table (for $GITHUB_STEP_SUMMARY)")
	top := flag.Int("top", 15, "list the n packages with the most uncovered statements (0: all)")
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: coverage [-markdown] [-top n] coverage.out")
		os.Exit(2)
	}
	if err := run(os.Stdout, flag.Arg(0), *markdown, *top); err != nil {
		fmt.Fprintln(os.Stderr, "coverage:", err)
		os.Exit(1)
	}
}

func run(w io.Writer, profile string, markdown bool, top int) error {
	f, err := os.Open(profile) //nolint:gosec // G304: the profile the developer names on the command line
	if err != nil {
		return err
	}
	defer f.Close()
	r, err := Parse(f)
	if err != nil {
		return fmt.Errorf("%s: %w", profile, err)
	}
	if markdown {
		return r.WriteMarkdown(w, top)
	}
	return r.WriteText(w, top)
}

// Parse reads a cover profile and aggregates it per package.
func Parse(r io.Reader) (Report, error) {
	type block struct {
		pkg        string
		statements int
		covered    bool
	}
	blocks := map[string]*block{}
	sc := bufio.NewScanner(r)
	first := true
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if first {
			first = false
			if !strings.HasPrefix(line, "mode:") {
				return Report{}, errors.New("not a cover profile (no mode line)")
			}
			continue
		}
		if line == "" {
			continue
		}
		// file.go:startLine.startCol,endLine.endCol numStatements count
		fields := strings.Fields(line)
		if len(fields) != 3 {
			return Report{}, fmt.Errorf("malformed line %q", line)
		}
		statements, err1 := strconv.Atoi(fields[1])
		count, err2 := strconv.Atoi(fields[2])
		colon := strings.LastIndexByte(fields[0], ':')
		if err1 != nil || err2 != nil || colon < 0 {
			return Report{}, fmt.Errorf("malformed line %q", line)
		}
		b := blocks[fields[0]]
		if b == nil {
			file := strings.TrimPrefix(fields[0][:colon], module)
			b = &block{pkg: path.Dir(file), statements: statements}
			blocks[fields[0]] = b
		}
		b.covered = b.covered || count > 0
	}
	if err := sc.Err(); err != nil {
		return Report{}, err
	}
	if first {
		return Report{}, errors.New("empty cover profile")
	}

	byPath := map[string]*Package{}
	rep := Report{Total: Package{Path: "total"}}
	for _, b := range blocks {
		if excluded.MatchString(b.pkg) {
			continue
		}
		p := byPath[b.pkg]
		if p == nil {
			p = &Package{Path: b.pkg}
			byPath[b.pkg] = p
		}
		p.Statements += b.statements
		rep.Total.Statements += b.statements
		if b.covered {
			p.Covered += b.statements
			rep.Total.Covered += b.statements
		}
	}
	for _, p := range byPath {
		rep.Packages = append(rep.Packages, *p)
	}
	slices.SortFunc(rep.Packages, func(a, b Package) int {
		if d := b.Uncovered() - a.Uncovered(); d != 0 {
			return d
		}
		return strings.Compare(a.Path, b.Path)
	})
	return rep, nil
}

func (r Report) listed(top int) []Package {
	if top <= 0 || top >= len(r.Packages) {
		return r.Packages
	}
	return r.Packages[:top]
}

// WriteText writes the report for a terminal.
func (r Report) WriteText(w io.Writer, top int) error {
	bw := bufio.NewWriter(w)
	fmt.Fprintf(bw, "Go coverage: %.1f%% of statements (%d of %d)\n",
		r.Total.Percent(), r.Total.Covered, r.Total.Statements)
	fmt.Fprintf(bw, "%8s  %9s  %s\n", "covered", "uncovered", "package")
	for _, p := range r.listed(top) {
		fmt.Fprintf(bw, "%7.1f%%  %9d  %s\n", p.Percent(), p.Uncovered(), p.Path)
	}
	return bw.Flush()
}

// WriteMarkdown writes the report as a GitHub job summary.
func (r Report) WriteMarkdown(w io.Writer, top int) error {
	bw := bufio.NewWriter(w)
	fmt.Fprintf(bw, "## Go coverage: %.1f%% of statements\n\n", r.Total.Percent())
	fmt.Fprintf(bw, "%d of %d statements, all packages' tests counted (`-coverpkg=./...`); "+
		"test suites, generated code and tools left out.\n\n", r.Total.Covered, r.Total.Statements)
	fmt.Fprintln(bw, "| Package | Covered | Uncovered statements |")
	fmt.Fprintln(bw, "| --- | ---: | ---: |")
	for _, p := range r.listed(top) {
		fmt.Fprintf(bw, "| `%s` | %.1f%% | %d |\n", p.Path, p.Percent(), p.Uncovered())
	}
	return bw.Flush()
}
