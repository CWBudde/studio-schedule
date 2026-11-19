package conformance

import (
	"bytes"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func passIssuance(t *testing.T, b *backend) {
	t.Helper()

	office, member, guest := browser(t), browser(t), browser(t)
	login(t, b, office, "olga.ledgerby@example.net", demoPassword)
	login(t, b, member, "lina.mistlewood@example.com", demoPassword)
	owner := read[struct{ Me struct{ ID string } }](t, get(t, b, member, "/api/v1/family")).Me.ID

	createType := request{
		method: http.MethodPost, path: "/api/v1/admin/pass-types",
		body: `{"name":"Fixture visits","kind":"visits","visits":3,"validityDays":10,"price":1600,"vatRate":1900,"active":true}`,
	}
	typeID := read[struct{ ID string }](t, b.call(t, office, createType, http.StatusCreated)).ID
	assign := request{
		method: http.MethodPost, path: "/api/v1/admin/persons/{personId}/passes/assign",
		params: map[string]string{"personId": owner},
		body:   fmt.Sprintf(`{"passTypeId":%q,"validFrom":"2026-10-20","note":"  Fixture gift  "}`, typeID),
	}
	b.call(t, guest, assign, http.StatusUnauthorized)
	b.call(t, member, assign, http.StatusForbidden)

	type issuedPass struct {
		ID, Origin, ValidFrom, ValidUntil, Note, Status, ReceiptID string
		VisitsTotal, VisitsRemaining, UseCount                     int
		PricePaid                                                  int64
		Uses                                                       []any
	}
	gift := read[issuedPass](t, b.call(t, office, assign, http.StatusCreated))
	if gift.ID == "" || gift.Origin != "assignment" || gift.Status != "active" || gift.ReceiptID != "" ||
		gift.PricePaid != 0 || gift.Note != "Fixture gift" || gift.ValidFrom != "2026-10-20" ||
		gift.ValidUntil != "2026-10-29" || gift.VisitsTotal != 3 || gift.VisitsRemaining != 3 || gift.UseCount != 0 {
		t.Fatalf("unexpected assigned pass: %+v", gift)
	}

	sell := assign
	sell.path = "/api/v1/admin/persons/{personId}/passes/sell"
	sell.body = fmt.Sprintf(`{"passTypeId":%q,"validFrom":null,"price":1350,"paymentMethod":"transfer","recipientEmail":"ignored@example.test"}`, typeID)
	sale := read[issuedPass](t, b.call(t, office, sell, http.StatusCreated))
	if sale.ID == "" || sale.ID == gift.ID || sale.Origin != "sale" || sale.ReceiptID == "" ||
		sale.PricePaid != 1350 || sale.ValidFrom != "2026-10-23" || sale.ValidUntil != "2026-11-01" ||
		sale.VisitsRemaining != 3 || sale.UseCount != 0 {
		t.Fatalf("unexpected sold pass: %+v", sale)
	}

	receipt := request{
		method: http.MethodGet, path: "/api/v1/admin/receipts/{receiptId}",
		params: map[string]string{"receiptId": sale.ReceiptID},
	}
	issued := read[struct {
		ID, Number, PaymentMethod string
		Gross                     int64
	}](t, b.call(t, office, receipt, http.StatusOK))
	if issued.ID != sale.ReceiptID || issued.Gross != sale.PricePaid || issued.PaymentMethod != "transfer" {
		t.Fatalf("unexpected pass receipt: %+v", issued)
	}
	receipt.path += "/pdf"
	result := b.call(t, office, receipt, http.StatusOK)
	if !bytes.HasPrefix(result.body, []byte("%PDF-")) {
		t.Fatal("expected the original receipt PDF")
	}

	outbox := read[struct {
		Mails []struct{ To, Template, Subject string }
	}](t, b.fixture(t, http.MethodGet, "outbox", "", http.StatusOK))
	matches := 0
	for _, message := range outbox.Mails {
		if message.Template == "receipt" && strings.Contains(message.Subject, issued.Number) {
			matches++
			if message.To != "lina.mistlewood@example.com" {
				t.Errorf("receipt went to %q instead of payer's account", message.To)
			}
		}
	}
	if matches != 1 {
		t.Errorf("expected one mail for receipt %s, saw %d", issued.Number, matches)
	}

	passList := read[struct{ Items []issuedPass }](t, get(t, b, member, "/api/v1/my/passes"))
	for _, item := range passList.Items {
		if item.ID == gift.ID {
			if item.Note != "" {
				t.Fatal("assignment note was disclosed in the family pass list")
			}
			return
		}
	}
	t.Fatal("the family could not see the assigned pass")
}
