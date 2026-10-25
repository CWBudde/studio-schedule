package postgres

import (
	"context"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/seed"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// TestReceiptsImmutable: the database itself refuses to change or delete
// receipts, their lines and their PDFs (ADR 0012), whatever the code does.
func TestReceiptsImmutable(t *testing.T) {
	t.Parallel()

	st := newTestStore(t)
	if err := seed.LoadDemo(t.Context(), st); err != nil {
		t.Fatal(err)
	}

	r := seed.Demo().Receipts[0]

	err := st.InTx(t.Context(), store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		return tx.Receipts().PutPDF(ctx, r.ID, []byte("%PDF-1.7"))
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, stmt := range []string{
		`UPDATE receipts SET recipient_name = 'Someone else' WHERE id = $1`,
		`UPDATE receipts SET gross = 0, net = 0, vat = 0 WHERE id = $1`,
		`DELETE FROM receipts WHERE id = $1`,
		`UPDATE receipt_lines SET description = 'Other' WHERE receipt_id = $1`,
		`DELETE FROM receipt_lines WHERE receipt_id = $1`,
		`UPDATE receipt_pdfs SET pdf = '\x00' WHERE receipt_id = $1`,
		`DELETE FROM receipt_pdfs WHERE receipt_id = $1`,
	} {
		if _, err := st.pool.Exec(t.Context(), stmt, r.ID[:]); err == nil {
			t.Errorf("%s: succeeded", stmt)
		}
	}

	for _, table := range []string{"receipt_pdfs", "receipt_lines", "receipts"} {
		if _, err := st.pool.Exec(t.Context(), `TRUNCATE `+table+` CASCADE`); err == nil {
			t.Errorf("truncating %s succeeded", table)
		}
	}

	var (
		name string
		pdfs int
	)

	err = st.pool.QueryRow(t.Context(), `SELECT recipient_name, (SELECT count(*) FROM receipt_pdfs) FROM receipts WHERE id = $1`,
		r.ID[:]).Scan(&name, &pdfs)
	if err != nil || name != r.RecipientName || pdfs != 1 {
		t.Fatalf("recipient %q, %d PDFs (%v); want %q and the PDF", name, pdfs, err, r.RecipientName)
	}
}
