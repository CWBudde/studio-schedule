package conformance

import (
	"bytes"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/seed"
)

func eventRegistrationReads(t *testing.T, b *backend) {
	t.Helper()

	anchor, err := domain.ParseDate(demoDate)
	if err != nil {
		t.Fatal(err)
	}
	fixture := seed.DemoAt(anchor)
	eventIDs := make(map[string]string)
	for _, event := range fixture.Events {
		eventIDs[event.Slug] = event.ID.String()
	}
	guest, office, member, guardian := browser(t), browser(t), browser(t), browser(t)
	login(t, b, office, "olga.ledgerby@example.net", demoPassword)
	login(t, b, member, demoMember, demoPassword)
	login(t, b, guardian, "yara.gildenmoor@example.org", demoPassword)

	const ballet = "open-ballet-taster"
	const intensive = "contemporary-weekend-intensive"
	roster := request{
		method: http.MethodGet, path: "/api/v1/admin/events/{eventId}/registrations",
		params: map[string]string{"eventId": eventIDs[ballet]},
	}
	b.call(t, guest, roster, http.StatusUnauthorized)
	b.call(t, member, roster, http.StatusForbidden)
	type registration struct {
		ID, EventID, Status, Payment, ReceiptID, CreatedAt, UpdatedAt string
		Price, VATRate                                                int
		Guest                                                         *struct{ Name, Email string }
		Person                                                        *struct{ ID, Name string }
	}
	balletRoster := read[struct{ Items []registration }](t, b.call(t, office, roster, http.StatusOK))
	if len(balletRoster.Items) != 3 {
		t.Fatalf("ballet registrations: %+v", balletRoster)
	}
	names := make([]string, 0, len(balletRoster.Items))
	for _, row := range balletRoster.Items {
		if row.EventID != eventIDs[ballet] || row.Status != "registered" || row.Payment != "paid" ||
			row.Price != 0 || row.VATRate != 1900 || row.CreatedAt == "" || row.UpdatedAt == "" {
			t.Fatalf("unexpected free event registration: %+v", row)
		}
		if row.Person != nil {
			names = append(names, row.Person.Name)
		} else if row.Guest != nil && row.Guest.Email == "juna.firstlight@example.org" {
			names = append(names, row.Guest.Name)
		}
	}
	if !slices.Equal(names, []string{"Kira Gildenmoor", "Tilo Quarrystone", "Juna Firstlight"}) {
		t.Fatalf("ballet registration order and participants: %v", names)
	}
	roster.params["eventId"] = eventIDs["summer-showcase-rehearsal"]
	if draft := b.call(t, office, roster, http.StatusOK); !containsJSON(draft.body, `"items":[]`) {
		t.Fatalf("draft registration list: %s", draft.body)
	}
	roster.params["eventId"] = "00000000-0000-7000-8000-ffffffffffff"
	b.call(t, office, roster, http.StatusNotFound)

	mine := request{method: http.MethodGet, path: "/api/v1/my/events"}
	b.call(t, guest, mine, http.StatusUnauthorized)
	type item struct {
		Registration struct {
			registration
			Cancelable bool
		}
		Event eventSummary
	}
	family := b.call(t, guardian, mine, http.StatusOK)
	if strings.Contains(string(family.body), "Juna Firstlight") || strings.Contains(string(family.body), "Tilo Quarrystone") {
		t.Fatalf("family document exposed another participant: %s", family.body)
	}
	items := read[struct{ Items []item }](t, family).Items
	if len(items) != 2 || items[0].Event.Slug != intensive || items[1].Event.Slug != ballet ||
		items[0].Registration.Person == nil || items[0].Registration.Person.Name != "Yara Gildenmoor" ||
		items[1].Registration.Person == nil || items[1].Registration.Person.Name != "Kira Gildenmoor" ||
		!items[0].Registration.Cancelable || !items[1].Registration.Cancelable {
		t.Fatalf("unexpected guardian events: %+v", items)
	}
	paid := read[struct{ Items []item }](t, b.call(t, member, mine, http.StatusOK)).Items
	if len(paid) != 1 || paid[0].Event.Slug != intensive || paid[0].Registration.Price != 12000 ||
		paid[0].Registration.Payment != "paid" || paid[0].Registration.ReceiptID == "" {
		t.Fatalf("member's paid event registration: %+v", paid)
	}
	roster.params["eventId"] = eventIDs[intensive]
	desk := read[struct{ Items []registration }](t, b.call(t, office, roster, http.StatusOK)).Items
	if !slices.ContainsFunc(desk, func(row registration) bool {
		return row.ID == paid[0].Registration.ID && row.ReceiptID == paid[0].Registration.ReceiptID
	}) {
		t.Fatal("receipt references differ between the roster and family registrations")
	}
	receipt := request{
		method: http.MethodGet, path: "/api/v1/my/receipts/{receiptId}/pdf",
		params: map[string]string{"receiptId": paid[0].Registration.ReceiptID},
	}
	pdf := b.call(t, member, receipt, http.StatusOK)
	if !bytes.HasPrefix(pdf.body, []byte("%PDF-")) || pdf.header.Get("Cache-Control") != "private, no-store" {
		t.Fatal("the event receipt link did not return a private PDF")
	}
	b.call(t, guardian, receipt, http.StatusNotFound)
}
