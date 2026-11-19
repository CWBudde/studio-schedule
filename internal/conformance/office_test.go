package conformance

import (
	"bytes"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

type eventSummary struct {
	Slug       string
	Status     string
	FreePlaces int
	SignupOpen bool
	Sessions   []scheduledLesson
}

func publicEventDetails(t *testing.T, b *backend) {
	t.Helper()

	const path = "/api/v1/events/{slug}"
	client := browser(t)
	event := request{
		method: http.MethodGet, path: path, params: map[string]string{"slug": "open-ballet-taster"},
	}
	upcoming := read[eventSummary](t, b.call(t, client, event, http.StatusOK))
	if upcoming.Slug != event.params["slug"] || upcoming.Status != "published" || !upcoming.SignupOpen ||
		upcoming.FreePlaces != 17 || len(upcoming.Sessions) != 1 {
		t.Fatalf("published event details: %+v", upcoming)
	}
	for _, slug := range []string{"summer-showcase-rehearsal", "missing-workshop"} {
		hidden := request{method: http.MethodGet, path: path, params: map[string]string{"slug": slug}}
		b.call(t, client, hidden, http.StatusNotFound)
		hidden.path += "/image"
		b.call(t, client, hidden, http.StatusNotFound)
	}
	image := event
	image.path += "/image"
	b.call(t, client, image, http.StatusNotFound)

	b.fixture(t, http.MethodPut, "clock", `{"now":"2027-02-05T12:00:00Z"}`, http.StatusNoContent)
	past := read[eventSummary](t, b.call(t, client, event, http.StatusOK))
	if past.Slug != upcoming.Slug || past.Status != "published" || past.SignupOpen || len(past.Sessions) != 1 {
		t.Fatalf("past event details: %+v", past)
	}
}

func eventRegistration(t *testing.T, b *backend) {
	t.Helper()

	guest := browser(t)
	office := browser(t)
	login(t, b, office, "olga.ledgerby@example.net", "demo-password")
	out := read[struct{ Items []eventSummary }](t, get(t, b, office, "/api/v1/admin/events"))
	var draft string
	for _, event := range out.Items {
		if event.Status == "draft" {
			draft = event.Slug

			break
		}
	}
	if draft == "" {
		t.Fatal("fixture has no draft event")
	}

	event := request{method: http.MethodGet, path: "/api/v1/events/{slug}", params: map[string]string{"slug": draft}}
	b.call(t, guest, event, http.StatusNotFound)
	event.params["slug"] = "open-ballet-taster"

	before := read[eventSummary](t, b.call(t, guest, event, http.StatusOK))
	if !before.SignupOpen || before.FreePlaces < 1 || len(before.Sessions) != 1 {
		t.Fatalf("free seeded event has unexpected availability or sessions: %+v", before)
	}

	member := browser(t)
	login(t, b, member, "clara.amberly@example.org", "demo-password")
	family := read[struct{ Dependents []struct{ ID string } }](t, get(t, b, member, "/api/v1/family"))
	if len(family.Dependents) == 0 {
		t.Fatal("fixture has no dependent to register")
	}

	person := family.Dependents[0].ID
	register := request{
		method: http.MethodPost, path: event.path + "/registrations",
		params: event.params, body: `{"personId":"` + person + `"}`,
	}
	b.call(t, guest, register, http.StatusUnauthorized)
	type myEvent struct {
		Event        eventSummary
		Registration struct {
			ID     string
			Status string
			Person struct{ ID string }
		}
	}
	registered := read[myEvent](t, b.call(t, member, register, http.StatusCreated))
	if registered.Registration.Status != "registered" || registered.Registration.Person.ID != person ||
		registered.Event.FreePlaces != before.FreePlaces-1 {
		t.Fatalf("unexpected family registration: %+v", registered)
	}
	b.call(t, member, register, http.StatusConflict)

	b.call(t, member, request{
		method: http.MethodPost, path: "/api/v1/event-registrations/{registrationId}/cancel",
		params: map[string]string{"registrationId": registered.Registration.ID},
	}, http.StatusOK)
	after := read[eventSummary](t, b.call(t, guest, event, http.StatusOK))
	if after.FreePlaces != before.FreePlaces {
		t.Errorf("canceling registration did not release the place: before %d, after %d", before.FreePlaces, after.FreePlaces)
	}
}

func passReceipt(t *testing.T, b *backend) {
	t.Helper()

	member := browser(t)
	office := browser(t)
	login(t, b, member, "lina.mistlewood@example.com", "demo-password")
	login(t, b, office, "olga.ledgerby@example.net", "demo-password")
	search := request{method: http.MethodPost, path: "/api/v1/admin/persons/search", body: `{"query":"kai lanternby"}`}
	b.call(t, member, search, http.StatusForbidden)
	match := read[struct{ Items []struct{ ID string } }](t, b.call(t, office, search, http.StatusOK))
	if len(match.Items) != 1 {
		t.Fatalf("unexpected person search results: %+v", match.Items)
	}

	types := read[struct {
		Items []struct {
			ID, Name string
			Active   bool
			Price    int64
		}
	}](t, get(t, b, office, "/api/v1/admin/pass-types"))
	for _, pass := range types.Items {
		if pass.Active && pass.Name == "30-day pass" && pass.Price > 0 {
			sellAndCancelReceipt(t, b, office, match.Items[0].ID, pass.ID, pass.Price)

			return
		}
	}
	t.Fatal("fixture has no 30-day pass on sale")
}

func sellAndCancelReceipt(t *testing.T, b *backend, office *http.Client, person, passType string, price int64) {
	t.Helper()

	sale := request{
		method: http.MethodPost, path: "/api/v1/admin/persons/{personId}/passes/sell",
		params: map[string]string{"personId": person},
		body:   `{"passTypeId":"` + passType + `","paymentMethod":"cash"}`,
	}
	pass := read[struct {
		ReceiptID string
		PricePaid int64
	}](t, b.call(t, office, sale, http.StatusCreated))
	if pass.ReceiptID == "" || pass.PricePaid != price {
		t.Fatalf("paid pass was not issued at the catalog price: %+v", pass)
	}

	type receipt struct {
		ID, Number, PaymentMethod, Currency string
		Cancels, CanceledBy                 struct{ ID string }
		Net, VAT, Gross                     int64
		Cancelable                          bool
	}
	getReceipt := request{
		method: http.MethodGet, path: "/api/v1/admin/receipts/{receiptId}",
		params: map[string]string{"receiptId": pass.ReceiptID},
	}
	original := read[receipt](t, b.call(t, office, getReceipt, http.StatusOK))
	if original.ID != pass.ReceiptID || !strings.HasPrefix(original.Number, "SD-2026-") ||
		original.PaymentMethod != "cash" || original.Currency != "EUR" ||
		original.Gross != price || original.Net+original.VAT != original.Gross || !original.Cancelable {
		t.Fatalf("unexpected original receipt: %+v", original)
	}

	pdf := getReceipt
	pdf.path += "/pdf"
	doc := b.call(t, office, pdf, http.StatusOK)
	if !bytes.HasPrefix(doc.body, []byte("%PDF-")) ||
		!strings.Contains(doc.header.Get("Content-Disposition"), original.Number+".pdf") {
		t.Error("receipt PDF or its filename is invalid")
	}

	cancel := getReceipt
	cancel.method, cancel.path, cancel.body = http.MethodPost, getReceipt.path+"/cancel", `{"reason":"Fixture correction"}`
	storno := read[receipt](t, b.call(t, office, cancel, http.StatusCreated))
	if storno.Cancels.ID != original.ID || storno.Gross != -original.Gross ||
		storno.Net != -original.Net || storno.VAT != -original.VAT || storno.Number == original.Number {
		t.Fatalf("storno does not reverse the original receipt: %+v", storno)
	}
	b.call(t, office, cancel, http.StatusConflict)

	stored := read[receipt](t, b.call(t, office, getReceipt, http.StatusOK))
	if stored.CanceledBy.ID != storno.ID || stored.Cancelable ||
		stored.Gross != original.Gross || stored.Number != original.Number {
		t.Errorf("original receipt was altered or is missing the storno link: %+v", stored)
	}
}

// Create a one-off lesson in a seeded room on a released school date.
func createLesson(t *testing.T, b *backend, office *http.Client, kind string, capacity int) string {
	t.Helper()

	terms := read[struct{ Items []struct{ ID string } }](t, get(t, b, office, "/api/v1/terms"))
	for _, term := range terms.Items {
		classes := read[struct {
			Items []struct {
				CourseTypeID, RoomID string
				Instructor           struct{ ID string }
			}
		}](t, b.call(t, office, request{
			method: http.MethodGet, path: "/api/v1/terms/{termId}/classes",
			params: map[string]string{"termId": term.ID},
		}, http.StatusOK))
		for _, class := range classes.Items {
			if class.CourseTypeID == "" || class.RoomID == "" || class.Instructor.ID == "" {
				continue
			}
			in := request{method: http.MethodPost, path: "/api/v1/lessons", body: fmt.Sprintf(
				`{"courseTypeId":%q,"date":"2026-10-24","startTime":"08:00","durationMinutes":60,"roomId":%q,`+
					`"instructorId":%q,"capacity":%d,"visibility":"public","kind":%q,"confirmRoomOverlap":true}`,
				class.CourseTypeID, class.RoomID, class.Instructor.ID, capacity, kind,
			)}
			return read[struct{ ID string }](t, b.call(t, office, in, http.StatusCreated)).ID
		}
	}

	t.Fatal("the fixture has no class with a complete catalog and room assignment")
	return ""
}
