package conformance

import (
	"net/http"
	"testing"
)

func passVoidingAndRedemptionSettings(t *testing.T, b *backend) {
	t.Helper()

	office, admin, member := browser(t), browser(t), browser(t)
	login(t, b, office, "olga.ledgerby@example.net", demoPassword)
	login(t, b, admin, demoAdmin, demoPassword)
	login(t, b, member, "clara.amberly@example.org", demoPassword)
	const settingsPath = "/api/v1/admin/settings/passes"

	type settings struct{ RedemptionMode string }
	before := read[settings](t, get(t, b, office, settingsPath))
	if before.RedemptionMode != "attendance" {
		t.Fatalf("unexpected initial mode: %+v", before)
	}
	setMode := request{method: http.MethodPut, path: settingsPath, body: `{"redemptionMode":"booking"}`}
	b.call(t, office, setMode, http.StatusForbidden)
	badMode := setMode
	badMode.body = `{"redemptionMode":"unknown"}`
	b.call(t, admin, badMode, http.StatusUnprocessableEntity)
	if updated := read[settings](t, b.call(t, admin, setMode, http.StatusOK)); updated.RedemptionMode != "booking" {
		t.Fatalf("pass settings did not change: %+v", updated)
	}

	type pass struct {
		ID, Status, VoidedAt, ReceiptID string
		Usable                          bool
		UseCount, VisitsRemaining       int
		Uses                            []struct{ BookingID string }
	}
	wallet := read[struct {
		RedemptionMode string
		Items          []pass
	}](t, get(t, b, member, "/api/v1/my/passes"))
	if wallet.RedemptionMode != "booking" || len(wallet.Items) == 0 ||
		wallet.Items[0].UseCount == 0 || len(wallet.Items[0].Uses) == 0 {
		t.Fatalf("the new mode or previously paid visits are missing: %+v", wallet)
	}
	used := wallet.Items[0]
	usedReceipt := request{
		method: http.MethodPost, path: "/api/v1/admin/receipts/{receiptId}/cancel",
		params: map[string]string{"receiptId": used.ReceiptID}, body: `{}`,
	}
	if res := read[struct{ Reason string }](t, b.call(t, office, usedReceipt, http.StatusConflict)); res.Reason != "pass-used" {
		t.Fatalf("expected pass-used when canceling a used pass's receipt: %+v", res)
	}

	void := request{
		method: http.MethodPost, path: "/api/v1/admin/passes/{passId}/void",
		params: map[string]string{"passId": used.ID},
	}
	b.call(t, member, void, http.StatusForbidden)
	result := read[pass](t, b.call(t, office, void, http.StatusOK))
	if result.ID != used.ID || result.Status != "voided" || result.Usable || result.VoidedAt == "" ||
		result.ReceiptID != used.ReceiptID || result.UseCount != used.UseCount ||
		result.VisitsRemaining != used.VisitsRemaining || len(result.Uses) != len(used.Uses) ||
		result.Uses[0].BookingID != used.Uses[0].BookingID {
		t.Fatalf("voiding changed a past payment or returned the wrong pass: before %+v, after %+v", used, result)
	}
	if retry := read[struct{ Reason string }](t, b.call(t, office, void, http.StatusConflict)); retry.Reason != "already-voided" {
		t.Fatalf("unexpected repeated void result: %+v", retry)
	}
}
