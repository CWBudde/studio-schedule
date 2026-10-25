package app_test

import (
	"bytes"
	"encoding/base64"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/app"
)

// receiptBody is the part of a Receipt the tests inspect.
type receiptBody struct {
	ID, Number, Kind, PaymentMethod, RecipientName, RecipientEmail, Reason, PassID string
	Gross, Net, VAT                                                                int64
	Cancelable                                                                     bool
	Payer                                                                          *struct{ ID, Name string }
	Cancels, CanceledBy                                                            *struct{ ID, Number string }
	Lines                                                                          []struct {
		Description string
		Gross       int64
		VATRate     int
	}
}

// TestReceipts runs the receipts over HTTP on the demo (ADR 0012): a sale
// issues one, the office lists, downloads and cancels it, the payer sees
// and downloads hers, the mail attaches it, and the settings guard sales.
// The rules are tested in package service.
func TestReceipts(t *testing.T) {
	t.Parallel()

	a := newAuthApp(t, app.Deps{Demo: true})
	olga := a.login("olga.ledgerby@example.net")
	arno := a.login("arno.keystone@example.net")
	clara := a.login("clara.amberly@example.org")

	// The seed has five: four sales and the cancellation of one.
	list := unmarshal[struct {
		Items []receiptBody
		Total int
		Years []int
	}](t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/admin/receipts?limit=2", "", olga))
	if list.Total != 5 || len(list.Items) != 2 || list.Items[0].Number != "SD-2026-0005" || len(list.Years) != 1 ||
		list.Years[0] != 2026 {
		t.Fatalf("receipts %+v", list)
	}

	a.expect(http.StatusForbidden, http.MethodGet, "/api/v1/admin/receipts", "", clara)

	find := func(q string) string {
		t.Helper()

		found := unmarshal[struct{ Items []struct{ ID string } }](t,
			a.expect(http.StatusOK, http.MethodGet, "/api/v1/admin/persons?q="+url.QueryEscape(q), "", olga))
		if len(found.Items) != 1 {
			t.Fatalf("persons %q: %+v", q, found)
		}

		return found.Items[0].ID
	}
	finn, claraID := find("finn amberly"), find("clara amberly")
	types := unmarshal[struct{ Items []struct{ ID, Name string } }](t,
		a.expect(http.StatusOK, http.MethodGet, "/api/v1/admin/pass-types", "", olga))
	single := types.Items[2].ID

	// Clara pays her son's single visit by card.
	sold := unmarshal[struct{ ReceiptID string }](t, a.expect(http.StatusCreated, http.MethodPost,
		"/api/v1/admin/persons/"+finn+"/passes/sell",
		`{"passTypeId":"`+single+`","payerId":"`+claraID+`","paymentMethod":"card","recipientAddress":"Amber Row 3"}`, olga))

	r := unmarshal[receiptBody](t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/admin/receipts/"+sold.ReceiptID, "", olga))

	if r.Number != "SD-2026-0006" || r.Kind != "sale" || r.PaymentMethod != "card" || r.Gross != 1800 || r.Net != 1513 ||
		r.VAT != 287 || r.Payer == nil || r.Payer.Name != "Clara Amberly" || !r.Cancelable || r.PassID == "" ||
		len(r.Lines) != 1 || r.Lines[0].VATRate != 1900 || !strings.HasPrefix(r.Lines[0].Description, "Single visit for Finn Amberly") {
		t.Fatalf("receipt %+v", r)
	}

	res, pdf := a.call(http.MethodGet, "/api/v1/admin/receipts/"+r.ID+"/pdf", "", olga)
	if res.StatusCode != http.StatusOK || res.Header.Get("Content-Type") != "application/pdf" ||
		res.Header.Get("Content-Disposition") != `attachment; filename="SD-2026-0006.pdf"` || !bytes.HasPrefix(pdf, []byte("%PDF")) {
		t.Fatalf("PDF: %d %v", res.StatusCode, res.Header)
	}

	// Clara sees her receipts (her own card, her daughter's and Finn's),
	// not Kai's.
	mine := unmarshal[struct{ Items []receiptBody }](t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/my/receipts", "", clara))
	if len(mine.Items) != 3 || mine.Items[0].Number != "SD-2026-0006" {
		t.Fatalf("Clara's receipts %+v", mine)
	}

	a.expect(http.StatusOK, http.MethodGet, "/api/v1/my/receipts/"+r.ID+"/pdf", "", clara)

	kais := unmarshal[struct {
		Items []receiptBody
		Total int
	}](t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/admin/receipts?q=lanternby", "", olga))
	if kais.Total != 1 {
		t.Fatalf("Kai's receipts %+v", kais)
	}

	a.expect(http.StatusNotFound, http.MethodGet, "/api/v1/my/receipts/"+kais.Items[0].ID+"/pdf", "", clara)

	// The demo mailbox shows the receipt mail with its attachment.
	box := unmarshal[struct {
		Mails []struct {
			ID, To     string
			Attachment *struct{ Filename, ContentType string }
		}
	}](t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/demo/mailbox", "", olga))
	if len(box.Mails) == 0 || box.Mails[0].Attachment == nil || box.Mails[0].Attachment.Filename != "SD-2026-0006.pdf" ||
		box.Mails[0].To != "clara.amberly@example.org" {
		t.Fatalf("mailbox %+v", box)
	}

	res, att := a.call(http.MethodGet, "/api/v1/demo/mailbox/"+box.Mails[0].ID+"/attachment", "")
	if res.StatusCode != http.StatusOK || !bytes.Equal(att, pdf) {
		t.Fatalf("attachment: %d", res.StatusCode)
	}

	// The Storno, once.
	c := unmarshal[receiptBody](t, a.expect(http.StatusCreated, http.MethodPost, "/api/v1/admin/receipts/"+r.ID+"/cancel",
		`{"reason":"Wrong pass"}`, olga))
	if c.Kind != "cancellation" || c.Number != "SD-2026-0007" || c.Gross != -1800 || c.Cancels == nil ||
		c.Cancels.Number != r.Number || c.Reason != "Wrong pass" || c.Cancelable {
		t.Fatalf("cancellation %+v", c)
	}

	again := decode(t, a.expect(http.StatusConflict, http.MethodPost, "/api/v1/admin/receipts/"+r.ID+"/cancel", `{}`, olga))
	if again["reason"] != "already-canceled" {
		t.Errorf("second Storno: %v", again)
	}

	if body := decode(t, a.expect(http.StatusConflict, http.MethodPost, "/api/v1/admin/receipts/"+c.ID+"/cancel", `{}`, olga)); body["reason"] != "cancellation" {
		t.Errorf("Storno of a Storno: %v", body)
	}

	// Clara's own card paid for drop-ins: no Storno.
	used := ""

	for _, it := range mine.Items {
		if it.Number != "SD-2026-0006" && strings.Contains(it.Lines[0].Description, "for Clara Amberly") {
			used = it.ID
		}
	}

	if body := decode(t, a.expect(http.StatusConflict, http.MethodPost, "/api/v1/admin/receipts/"+used+"/cancel", `{}`, olga)); body["reason"] != "pass-used" {
		t.Errorf("Storno of a used pass: %v", body)
	}

	// Settings: the office reads them, the admin writes them; incomplete
	// ones block sales.
	settings := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/admin/settings/receipts", "", olga))
	if settings["complete"] != true || settings["taxMode"] != "standard" || settings["receiptPrefix"] != "SD" ||
		settings["logoMaxBytes"] != float64(512*1024) || settings["addressRequiredAbove"] != float64(25000) {
		t.Fatalf("settings %v", settings)
	}

	a.expect(http.StatusForbidden, http.MethodPut, "/api/v1/admin/settings/receipts", `{"receiptPrefix":"SD"}`, olga)

	bad := decode(t, a.expect(http.StatusUnprocessableEntity, http.MethodPut, "/api/v1/admin/settings/receipts",
		`{"receiptPrefix":"SD","issuerName":"X","email":"no-address"}`, arno))
	if errs, _ := bad["errors"].([]any); len(errs) != 1 || errs[0].(map[string]any)["location"] != "body.email" {
		t.Errorf("invalid email: %v", bad)
	}

	updated := decode(t, a.expect(http.StatusOK, http.MethodPut, "/api/v1/admin/settings/receipts",
		`{"receiptPrefix":"SD","issuerName":"Studio Demo","issuerAddress":"Lane 1","taxNumber":"1","taxMode":"exempt"}`, arno))
	if updated["complete"] != false || updated["missing"] != "taxNote" || updated["addressRequiredAbove"] != nil {
		t.Fatalf("updated %v", updated)
	}

	blocked := decode(t, a.expect(http.StatusConflict, http.MethodPost, "/api/v1/admin/persons/"+finn+"/passes/sell",
		`{"passTypeId":"`+single+`"}`, olga))
	if blocked["reason"] != "receipt-settings-incomplete" || !strings.Contains(blocked["detail"].(string), "legal basis") {
		t.Errorf("sale with incomplete settings: %v", blocked)
	}

	// The logo.
	png := "\x89PNG\r\n\x1a\nlogo"
	logo := `{"contentType":"image/png","data":"` + base64.StdEncoding.EncodeToString([]byte(png)) + `"}`
	a.expect(http.StatusNoContent, http.MethodPut, "/api/v1/admin/settings/receipts/logo", logo, arno)

	res, got := a.call(http.MethodGet, "/api/v1/admin/settings/receipts/logo", "", olga)
	if res.StatusCode != http.StatusOK || res.Header.Get("Content-Type") != "image/png" || string(got) != png {
		t.Fatalf("logo: %d %v", res.StatusCode, res.Header)
	}

	mismatch := decode(t, a.expect(http.StatusUnprocessableEntity, http.MethodPut, "/api/v1/admin/settings/receipts/logo",
		`{"contentType":"image/jpeg","data":"`+base64.StdEncoding.EncodeToString([]byte(png))+`"}`, arno))
	if errs, _ := mismatch["errors"].([]any); len(errs) != 1 || errs[0].(map[string]any)["location"] != "body.data" {
		t.Errorf("JPEG with PNG data: %v", mismatch)
	}

	a.expect(http.StatusUnprocessableEntity, http.MethodPut, "/api/v1/admin/settings/receipts/logo",
		`{"contentType":"image/gif","data":"R0lGODlh"}`, arno)
	a.expect(http.StatusNoContent, http.MethodDelete, "/api/v1/admin/settings/receipts/logo", "", arno)
	a.expect(http.StatusNotFound, http.MethodGet, "/api/v1/admin/settings/receipts/logo", "", olga)

	// Members learn when their bookings use a pass.
	passes := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/my/passes", "", clara))
	if passes["redemptionMode"] != "attendance" {
		t.Errorf("my passes: redemptionMode %v", passes["redemptionMode"])
	}
}

// TestRetirePassType retires a pass type, idempotently, and leaves the rest
// of it alone.
func TestRetirePassType(t *testing.T) {
	t.Parallel()

	a := newAuthApp(t, app.Deps{})
	olga := a.login("olga.ledgerby@example.net")

	types := unmarshal[struct {
		Items []struct {
			ID, Name string
			Price    int64
			Active   bool
		}
	}](t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/admin/pass-types", "", olga))
	pt := types.Items[0]

	for range 2 {
		got := decode(t, a.expect(http.StatusOK, http.MethodPost, "/api/v1/admin/pass-types/"+pt.ID+"/retire", "", olga))
		if got["active"] != false || got["name"] != pt.Name || got["price"] != float64(pt.Price) {
			t.Fatalf("retired %v", got)
		}
	}

	a.expect(http.StatusNotFound, http.MethodPost, "/api/v1/admin/pass-types/00000000-0000-7000-8000-00000000f001/retire", "", olga)
	a.expect(http.StatusForbidden, http.MethodPost, "/api/v1/admin/pass-types/"+pt.ID+"/retire", "",
		a.login("clara.amberly@example.org"))
}
