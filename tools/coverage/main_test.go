package main

import (
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	const profile = `mode: atomic
github.com/cwbudde/studio-schedule/internal/a/a.go:1.1,2.2 3 0
github.com/cwbudde/studio-schedule/internal/a/a.go:3.1,4.2 1 0
github.com/cwbudde/studio-schedule/internal/b/b.go:1.1,2.2 2 0
github.com/cwbudde/studio-schedule/internal/a/a.go:1.1,2.2 3 5
github.com/cwbudde/studio-schedule/internal/store/storetest/s.go:1.1,2.2 9 0
github.com/cwbudde/studio-schedule/internal/store/postgres/internal/sqlc/q.go:1.1,2.2 9 0
github.com/cwbudde/studio-schedule/tools/coverage/main.go:1.1,2.2 9 0
`
	r, err := Parse(strings.NewReader(profile))
	if err != nil {
		t.Fatal(err)
	}
	// a.go's first block is listed twice (two test binaries); one run covers it.
	if r.Total.Statements != 6 || r.Total.Covered != 3 {
		t.Errorf("total = %d of %d, want 3 of 6", r.Total.Covered, r.Total.Statements)
	}
	if got := r.Total.Percent(); got != 50 {
		t.Errorf("percent = %v, want 50", got)
	}
	if len(r.Packages) != 2 {
		t.Fatalf("packages = %+v, want internal/a and internal/b only", r.Packages)
	}
	// Most uncovered first: a has 1 uncovered statement, b 2.
	if r.Packages[0].Path != "internal/b" || r.Packages[1].Path != "internal/a" {
		t.Errorf("order = %s, %s; want internal/b, internal/a", r.Packages[0].Path, r.Packages[1].Path)
	}

	var text, md strings.Builder
	if err := r.WriteText(&text, 1); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(text.String(), "Go coverage: 50.0% of statements (3 of 6)\n") ||
		strings.Contains(text.String(), "internal/a") {
		t.Errorf("text report:\n%s", text.String())
	}
	if err := r.WriteMarkdown(&md, 0); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(md.String(), "| `internal/a` | 75.0% | 1 |") {
		t.Errorf("markdown report:\n%s", md.String())
	}
}

func TestParseRejectsOtherInput(t *testing.T) {
	for _, in := range []string{"", "not a profile\n", "mode: set\nfile.go 1 x\n"} {
		if _, err := Parse(strings.NewReader(in)); err == nil {
			t.Errorf("Parse(%q) succeeded", in)
		}
	}
}
