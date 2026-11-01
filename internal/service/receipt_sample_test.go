package service

import (
	"encoding/binary"
	"encoding/json"
	"testing"
)

// TestSampleReceiptInput is the module's input format (pdf.Input) around
// the document of a fictional sale, without a logo.
func TestSampleReceiptInput(t *testing.T) {
	t.Parallel()

	in := SampleReceiptInput()
	if len(in) < 4 {
		t.Fatalf("input has %d bytes, want the length prefix and the data", len(in))
	}

	n := binary.LittleEndian.Uint32(in)
	if int(n) != len(in)-4 {
		t.Fatalf("length prefix %d, want %d: the sample has no logo", n, len(in)-4)
	}

	var doc receiptDoc
	if err := json.Unmarshal(in[4:], &doc); err != nil {
		t.Fatal(err)
	}

	if doc.Title != "Receipt SAMPLE-2026-0001" || doc.Issuer.Name != "Example Dance School" || doc.Logo != "" {
		t.Errorf("title %q, issuer %q, logo %q; want the fictional sample", doc.Title, doc.Issuer.Name, doc.Logo)
	}

	if len(doc.Lines) != 1 || doc.Total[1] != "€100.00" || len(doc.Summary) != 2 {
		t.Errorf("lines %v, total %v, summary %v; want one line of €100.00 with its VAT", doc.Lines, doc.Total, doc.Summary)
	}
}
