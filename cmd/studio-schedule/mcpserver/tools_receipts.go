package mcpserver

import (
	"context"

	"github.com/cwbudde/studio-schedule/internal/httpapi"
	"github.com/cwbudde/studio-schedule/internal/service"
)

// receiptTools are the office's receipt tools.
func receiptTools() []tool {
	return []tool{{
		name: "receipts",
		description: "The receipts issued on the school dates from from through to, both inclusive, newest " +
			"first, as `receipts list` lists them (the body of searchReceipts): number, issue date, kind " +
			"(sale, or cancellation for a storno), the recipient printed on it, the amounts, the payment " +
			"method, and the receipt a storno cancels or the storno that canceled a receipt; total is the " +
			"number of receipts, and years lists the years that have receipts. Without to the range ends " +
			"today in the school's time zone, without from it starts on January 1 of the year of to.",
		access:       httpapi.AccessOffice,
		personalData: true,
		annotations:  readOnly("Receipts"),
		add:          handler(receipts),
	}}
}

// receiptsInput are the arguments of the receipts tool.
type receiptsInput struct {
	From string `json:"from,omitempty" jsonschema:"The first issue date (YYYY-MM-DD); default January 1 of the year of to."`
	To   string `json:"to,omitempty"   jsonschema:"The last issue date, inclusive (YYYY-MM-DD); default today in the school's time zone."`
}

// receipts is the receipts tool: what `receipts list` prints.
func receipts(ctx context.Context, s *Server, _ service.Principal, in receiptsInput) (any, error) {
	from, err := parseDate("from", in.From)
	if err != nil {
		return nil, err
	}

	to, err := parseDate("to", in.To)
	if err != nil {
		return nil, err
	}

	page, err := s.svc.Receipts.Issued(ctx, from, to)
	if err != nil {
		return nil, err
	}

	return httpapi.NewReceiptList(page), nil
}
