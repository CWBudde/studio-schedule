package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestStats prints the demo school's statistics as the office: the last
// four weeks by default, the term with the top participants, a custom week
// as getStatistics's body with them.
func TestStats(t *testing.T) {
	t.Parallel()

	stores(t, func(t *testing.T, r cliRun) {
		got := r.exec(t, "stats", "--as", demoOffice)
		wantExitCode(t, got, 0, "")
		golden(t, "stats/last4weeks.txt", got.stdout)

		if !strings.HasSuffix(got.stdout, "top participants: left out, as they name persons; --participants lists them\n") ||
			strings.Contains(got.stdout, "Ida Elderwick") {
			t.Error("the text names the top participants without --participants")
		}

		got = r.exec(t, "stats", "--as", demoOffice, "--range", "term", "--participants")
		wantExitCode(t, got, 0, "")
		golden(t, "stats/term.txt", got.stdout)

		got = r.exec(t, "stats", "--as", demoOffice, "--from", "2026-09-14", "--to", "2026-09-20", "--output", "json")
		wantExitCode(t, got, 0, "")
		golden(t, "stats/week.json", got.stdout)

		var body struct {
			Range           string `json:"range"`
			From            string `json:"from"`
			To              string `json:"to"`
			TopParticipants []struct {
				GivenName string `json:"givenName"`
			} `json:"topParticipants"`
		}
		if err := json.Unmarshal([]byte(got.stdout), &body); err != nil || body.Range != "custom" ||
			body.From != "2026-09-14" || body.To != "2026-09-20" || len(body.TopParticipants) == 0 {
			t.Errorf("JSON body %+v, %v", body, err)
		}
	})
}

// TestStatsRanges covers the other ranges: months beyond 26 weeks, a
// range without lessons, --range custom, and the usage errors.
func TestStatsRanges(t *testing.T) {
	t.Parallel()

	r := cliRun{st: demoStore(t)}

	got := r.exec(t, "stats", "--as", demoOffice, "--range", "custom", "--from", "2026-01-01", "--to", "2026-09-22")
	if got.code != 0 || !strings.Contains(got.stdout, "(custom), by month, in Europe/Berlin\n") ||
		!strings.Contains(got.stdout, "\nby month:\n2026-01-01 to 2026-01-31  0") {
		t.Errorf("months: exit %d, stdout %q, stderr %q", got.code, got.stdout, got.stderr)
	}

	// A custom range may reach ahead: this week's one-off workshops are rows
	// of their own, named by their start in the school's time zone.
	got = r.exec(t, "stats", "--as", demoOffice, "--from", "2026-09-21", "--to", "2026-09-27")
	if got.code != 0 || !strings.Contains(got.stdout, "\n2026-09-25 13:30 Contemporary (workshop) ") {
		t.Errorf("one-off lessons: exit %d, stdout %q, stderr %q", got.code, got.stdout, got.stderr)
	}

	got = r.exec(t, "stats", "--as", demoOffice, "--from", "2026-08-01", "--to", "2026-08-02")
	wantExitCode(t, got, 0, "")
	golden(t, "stats/empty.txt", got.stdout)

	for _, c := range []struct {
		name   string
		args   []string
		code   int
		stderr string
	}{
		{"member", []string{"--as", demoMember}, 4, "below office"},
		{"instructor", []string{"--as", demoInstructor}, 4, "below office"},
		{"unknown range", []string{"--as", demoOffice, "--range", "lastYear"}, 2, `unknown range "lastYear"`},
		{"preset with dates", []string{
			"--as", demoOffice, "--range", "term", "--from", "2026-09-01", "--to", "2026-09-22",
		}, 2, "--range term: --from and --to give a custom range"},
		{"from alone", []string{"--as", demoOffice, "--from", "2026-09-01"}, 2, "must all be set"},
		{
			"custom without dates",
			[]string{"--as", demoOffice, "--range", "custom"},
			2,
			"a custom range needs valid from and to dates",
		},
		{
			"bad from",
			[]string{"--as", demoOffice, "--from", "Sept", "--to", "2026-09-22"},
			2,
			`--from: "Sept" is not a date`,
		},
		{"bad to", []string{"--as", demoOffice, "--from", "2026-09-01", "--to", "now"}, 2, `--to: "now" is not a date`},
		{
			"to before from",
			[]string{"--as", demoOffice, "--from", "2026-09-22", "--to", "2026-09-01"},
			2,
			"to must not be before from",
		},
		{
			"too long",
			[]string{"--as", demoOffice, "--from", "2025-09-01", "--to", "2026-09-22"},
			2,
			"must not exceed 366 days",
		},
	} {
		got := r.exec(t, append([]string{"stats"}, c.args...)...)
		if got.code != c.code || !strings.Contains(got.stderr, c.stderr) || got.stdout != "" {
			t.Errorf("%s: exit %d, stdout %q, stderr %q; want exit %d with %q", c.name, got.code, got.stdout,
				got.stderr, c.code, c.stderr)
		}
	}

	// The term range needs a term that includes today.
	wantExitCode(t, cliRun{st: pastDemoStore(t)}.exec(t, "stats", "--as", demoOffice, "--range", "term"), 3,
		"no term includes today")
}
