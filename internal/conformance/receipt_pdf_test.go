package conformance

import (
	"bytes"
	"net/http"
	"slices"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/seed"
)

func receiptPDFDownloads(t *testing.T, b *backend) {
	t.Helper()

	anchor, err := domain.ParseDate(demoDate)
	if err != nil {
		t.Fatal(err)
	}

	// The fixture seed assigns the same IDs on both backends.
	fixture := seed.DemoAt(anchor)
	member := slices.IndexFunc(fixture.Accounts, func(a domain.Account) bool { return a.Email == demoMember })
	if member < 0 {
		t.Fatal("the fixture has no demo member")
	}

	index := slices.IndexFunc(fixture.Receipts, func(r domain.Receipt) bool {
		return r.PayerID == fixture.Accounts[member].PersonID
	})
	if index < 0 {
		t.Fatal("the fixture has no receipt paid by the demo member")
	}

	r := fixture.Receipts[index]
	memberDownload := request{
		method: http.MethodGet, path: "/api/v1/my/receipts/{receiptId}/pdf",
		params: map[string]string{"receiptId": r.ID.String()},
	}
	officeDownload := memberDownload
	officeDownload.path = "/api/v1/admin/receipts/{receiptId}/pdf"

	client := browser(t)
	b.call(t, client, memberDownload, http.StatusUnauthorized)
	b.call(t, client, officeDownload, http.StatusUnauthorized)
	login(t, b, client, demoMember, demoPassword)
	b.call(t, client, officeDownload, http.StatusForbidden)
	doc := b.call(t, client, memberDownload, http.StatusOK)
	if !bytes.HasPrefix(doc.body, []byte("%PDF-")) ||
		doc.header.Get("Content-Disposition") != `attachment; filename="`+r.Number+`.pdf"` ||
		doc.header.Get("Cache-Control") != "private, no-store" {
		t.Fatal("member receipt download did not return a private PDF attachment named after the receipt")
	}

	admin := browser(t)
	login(t, b, admin, demoAdmin, demoPassword)
	b.call(t, admin, memberDownload, http.StatusNotFound)
	original := b.call(t, admin, officeDownload, http.StatusOK)
	if !bytes.Equal(doc.body, original.body) || original.header.Get("Cache-Control") != "private, no-store" {
		t.Fatal("office receipt download did not return the same immutable, private PDF")
	}
}
