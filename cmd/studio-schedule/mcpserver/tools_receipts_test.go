package mcpserver

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/service"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// receiptNumbers are the numbers of a receipts document's items, in order.
func receiptNumbers(t *testing.T, doc map[string]any) []string {
	t.Helper()

	items, ok := doc["items"].([]any)
	if !ok {
		t.Fatalf("no items in %v", doc)
	}

	numbers := make([]string, 0, len(items))
	for _, it := range items {
		n, _ := it.(map[string]any)["number"].(string)
		numbers = append(numbers, n)
	}

	return numbers
}

// TestReceiptsGolden: by default the receipts of this year up to today in
// the school's time zone (the demo's five of September 2026), newest
// first, with the storno and the receipt it cancels.
func TestReceiptsGolden(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	cs := f.session(t, demoOffice, Options{PersonalData: true}, registry)

	text, _ := callTool(t, cs, "receipts", nil)
	golden(t, "receipts.json", text)

	doc := document(t, cs, "receipts", map[string]any{})
	if got := strings.Join(receiptNumbers(t, doc), " "); got !=
		"SD-2026-0005 SD-2026-0004 SD-2026-0003 SD-2026-0002 SD-2026-0001" || doc["total"] != float64(5) {
		t.Errorf("the default range: %s, total %v", got, doc["total"])
	}
}

// TestReceiptsRange: from and to are both inclusive issue dates; without
// from the range starts on January 1 of the year of to, without to it ends
// today; the dates are checked by field, the range by the service.
func TestReceiptsRange(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	cs := f.session(t, demoOffice, Options{PersonalData: true}, registry)

	for _, tc := range []struct {
		args    map[string]any
		numbers string
	}{
		{map[string]any{"from": "2026-09-08", "to": "2026-09-12"}, "SD-2026-0004 SD-2026-0003"},
		{map[string]any{"from": "2026-09-12", "to": "2026-09-12"}, "SD-2026-0004"},
		{map[string]any{"from": "2026-09-13"}, "SD-2026-0005"},
		{map[string]any{"to": "2026-09-07"}, "SD-2026-0002 SD-2026-0001"},
		{map[string]any{"to": "2025-12-31"}, ""},
		{map[string]any{"from": "2026-09-16", "to": "2026-12-31"}, ""},
	} {
		doc := document(t, cs, "receipts", tc.args)
		if got := strings.Join(receiptNumbers(t, doc), " "); got != tc.numbers ||
			doc["total"] != float64(len(receiptNumbers(t, doc))) {
			t.Errorf("%v: %q, total %v; want %q", tc.args, got, doc["total"], tc.numbers)
		}
	}

	for _, tc := range []struct {
		args map[string]any
		text string
	}{
		{
			map[string]any{"from": "2026-09-20", "to": "2026-09-10"},
			"receipts from 2026-09-20 to 2026-09-10: validation failed: the end lies before the start",
		},
		{map[string]any{"from": "2026-10-01"}, // after today, the default to
			"receipts from 2026-10-01 to 2026-09-22: validation failed: the end lies before the start"},
		{map[string]any{"from": "1.9.2026"}, "validation failed: from is not a date (YYYY-MM-DD)"},
		{map[string]any{"from": "2026-09-01", "to": "Zebulon"}, "validation failed: to is not a date (YYYY-MM-DD)"},
		{
			map[string]any{"year": "2026"},
			`validation failed: the arguments do not fit the tool's input schema: json: unknown field "year"`,
		},
	} {
		wantError(t, cs, "receipts", tc.args, tc.text)
	}

	calls := toolCalls(t, f.st)
	if last := calls[len(calls)-1]; last.Details.Tool != "receipts" || last.Details.Outcome != "invalid" {
		t.Errorf("the last call was audited as %+v", last.Details)
	}
}

// TestReceiptsStoreFails: a store that fails is an unexpected error, in
// the log and not in the result.
func TestReceiptsStoreFails(t *testing.T) {
	t.Parallel()

	st := &faultStore{Store: newFixture(t).st}
	f := fixtureOn(st)
	office := f.principal(t, demoOffice)
	cs := f.sessionOf(t, office, func(context.Context) (service.Principal, error) { return office, nil },
		Options{PersonalData: true}, registry)

	st.failing(store.ReadOnly, errors.New("connection reset"))

	wantError(t, cs, "receipts", nil, "internal error")

	if log := f.log.String(); !strings.Contains(log, "connection reset") || !strings.Contains(log, "tool=receipts") {
		t.Errorf("log %q", log)
	}
}
