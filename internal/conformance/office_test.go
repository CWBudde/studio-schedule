package conformance

import (
	"bytes"
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

func officeEventReads(t *testing.T, b *backend) {
	t.Helper()

	const events = "/api/v1/admin/events"
	guest, member, office := browser(t), browser(t), browser(t)
	login(t, b, member, "clara.amberly@example.org", "demo-password")
	login(t, b, office, "olga.ledgerby@example.net", "demo-password")

	type adminEvent struct {
		eventSummary
		ID           string
		CourseTypeID string
		Capacity     int
		Registered   int
		Invited      int
		Waiting      int
	}

	list := request{method: http.MethodGet, path: events}
	b.call(t, guest, list, http.StatusUnauthorized)
	b.call(t, member, list, http.StatusForbidden)
	all := read[struct{ Items []adminEvent }](t, b.call(t, office, list, http.StatusOK))
	bySlug := make(map[string]adminEvent, len(all.Items))
	for _, item := range all.Items {
		bySlug[item.Slug] = item
	}
	public := bySlug["open-ballet-taster"]
	if public.Status != "published" || public.Capacity-public.Registered-public.Invited != 17 ||
		public.Registered == 0 || public.CourseTypeID == "" || len(public.Sessions) != 1 {
		t.Fatalf("published workshop in office list: %+v", public)
	}

	draft := bySlug["summer-showcase-rehearsal"]
	if draft.Status != "draft" || draft.ID == "" {
		t.Fatalf("draft workshop in office list: %+v", draft)
	}
	detail := request{
		method: http.MethodGet, path: events + "/{eventId}", params: map[string]string{"eventId": draft.ID},
	}
	b.call(t, guest, detail, http.StatusUnauthorized)
	b.call(t, member, detail, http.StatusForbidden)
	found := read[adminEvent](t, b.call(t, office, detail, http.StatusOK))
	if found.ID != draft.ID || found.Slug != draft.Slug || found.Status != "draft" ||
		found.Registered != draft.Registered || found.Invited != draft.Invited || found.Waiting != draft.Waiting {
		t.Fatalf("office workshop detail: got %+v, listed %+v", found, draft)
	}

	invalid := detail
	invalid.params = map[string]string{"eventId": draft.ID + "/image"}
	problem := b.call(t, office, invalid, http.StatusUnprocessableEntity)
	if !containsJSON(problem.body, `"location":"path.eventId"`) {
		t.Fatalf("invalid event reference: %s", problem.body)
	}

	detail.path += "/image"
	b.call(t, guest, detail, http.StatusUnauthorized)
	b.call(t, member, detail, http.StatusForbidden)
	b.call(t, office, detail, http.StatusNotFound)
}

func eventRegistration(t *testing.T, b *backend) {
	t.Helper()

	guest := browser(t)
	office := browser(t)
	login(t, b, office, "olga.ledgerby@example.net", "demo-password")
	const slug = "contemporary-weekend-intensive"
	out := read[struct {
		Items []struct{ ID, Slug, Status string }
	}](t, get(t, b, office, "/api/v1/admin/events"))
	var draft, expensiveID string
	for _, event := range out.Items {
		if event.Status == "draft" {
			draft = event.Slug
		}
		if event.Slug == slug {
			expensiveID = event.ID
		}
	}
	if draft == "" || expensiveID == "" {
		t.Fatal("fixture needs a draft and a priced event")
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
	if len(family.Dependents) < 2 {
		t.Fatal("fixture needs two dependents to register")
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

	register.params = map[string]string{"slug": slug}
	priced := read[myEvent](t, b.call(t, member, register, http.StatusCreated))
	wait := register
	wait.path = event.path + "/waitlist"
	wait.body = `{"personId":"` + family.Dependents[1].ID + `"}`
	waiting := read[myEvent](t, b.call(t, member, wait, http.StatusCreated))
	const own = "/api/v1/event-registrations/{registrationId}"
	const admin = "/api/v1/admin/event-registrations/{registrationId}"
	pay := request{
		method: http.MethodPost, path: admin + "/pay", params: map[string]string{"registrationId": priced.Registration.ID},
		body: `{"recipientEmail":"event-refund@example.test"}`,
	}
	type registration struct {
		Status, Payment, ReceiptID, CancellationReceiptID, CanceledAt string
	}
	original := read[struct{ Registration registration }](t, b.call(t, office, pay, http.StatusOK)).Registration
	leave := request{method: http.MethodPost, path: own + "/cancel", params: pay.params}
	b.call(t, guest, leave, http.StatusUnauthorized)
	b.call(t, office, leave, http.StatusNotFound)
	waitCancel := leave
	waitCancel.params = map[string]string{"registrationId": waiting.Registration.ID}
	expectConflict(t, b, member, waitCancel, "not-registered")
	outcome := read[struct {
		Registration registration
		Event        eventSummary
	}](t, b.call(t, member, leave, http.StatusOK))
	refunded := outcome.Registration
	if refunded.Status != "canceled" || refunded.Payment != "refunded" || refunded.CanceledAt != demoNow ||
		refunded.ReceiptID != original.ReceiptID || refunded.CancellationReceiptID == "" ||
		refunded.CancellationReceiptID == original.ReceiptID || outcome.Event.FreePlaces != 0 {
		t.Fatalf("member cancellation and replacement offer: %+v", outcome)
	}
	expectConflict(t, b, member, leave, "already-canceled")
	offer := mailToken(t, b, "clara.amberly@example.org", "event-invite", "/events/invite")
	b.call(t, guest, request{method: http.MethodGet, path: "/api/v1/event-invites/{token}", params: map[string]string{"token": offer}}, http.StatusOK)

	receipt := request{
		method: http.MethodGet, path: "/api/v1/admin/receipts/{receiptId}",
		params: map[string]string{"receiptId": refunded.CancellationReceiptID},
	}
	storno := read[struct {
		Kind, Number string
		Gross        int
	}](t, b.call(t, office, receipt, http.StatusOK))
	if storno.Kind != "cancellation" || storno.Gross != -12000 {
		t.Fatalf("cancellation receipt: %+v", storno)
	}
	receipt.path = "/api/v1/my/receipts/{receiptId}/pdf"
	b.call(t, member, receipt, http.StatusOK)
	outbox := read[struct {
		Mails []struct{ To, Text, Template string }
	}](t, b.fixture(t, http.MethodGet, "outbox", "", http.StatusOK))
	var memberNotice, refundNotice bool
	for _, mail := range outbox.Mails {
		memberNotice = memberNotice || mail.To == "clara.amberly@example.org" &&
			mail.Template == "event-registration-canceled" && strings.Contains(mail.Text, "Contemporary Weekend Intensive")
		refundNotice = refundNotice || mail.To == "event-refund@example.test" &&
			mail.Template == "receipt" && strings.Contains(mail.Text, storno.Number)
	}
	if !memberNotice || !refundNotice {
		t.Errorf("missing cancellation or refund notice: member notice=%t, refund notice=%t", memberNotice, refundNotice)
	}

	waitCancel.path = own + "/accept"
	b.call(t, member, waitCancel, http.StatusOK)
	pay.params = waitCancel.params
	b.call(t, office, pay, http.StatusOK)
	b.fixture(t, http.MethodPut, "clock", `{"now":"2026-10-25T09:30:00Z"}`, http.StatusNoContent)
	waitCancel.path = own + "/cancel"
	expectConflict(t, b, member, waitCancel, "too-late")
	waitCancel.path, waitCancel.body = admin+"/cancel", `{}`
	b.call(t, member, waitCancel, http.StatusForbidden)
	if kept := read[registration](t, b.call(t, office, waitCancel, http.StatusOK)); kept.Status != "canceled" ||
		kept.Payment != "paid" || kept.ReceiptID == "" || kept.CancellationReceiptID != "" {
		t.Fatalf("office cancellation without a refund: %+v", kept)
	}

	officeCancelEvent(t, b, office, member, guest, expensiveID, person, waiting.Registration.ID)
}

func officeCancelEvent(t *testing.T, b *backend, office, member, guest *http.Client, eventID, personID, retainedID string) {
	t.Helper()

	const events = "/api/v1/admin/events/{eventId}"
	roster := request{
		method: http.MethodPost, path: events + "/registrations", params: map[string]string{"eventId": eventID},
		body: `{"personId":"` + personID + `"}`,
	}
	b.call(t, office, roster, http.StatusCreated)
	roster.body = `{"guest":{"name":"Guest Example","email":"guest-cancellation@example.test"},"confirmOverCapacity":true}`
	b.call(t, office, roster, http.StatusCreated)

	cancel := request{
		method: http.MethodPost, path: events + "/cancel", params: roster.params,
		body: `{"note":"  Fixture venue closed  "}`,
	}
	b.call(t, guest, cancel, http.StatusUnauthorized)
	b.call(t, member, cancel, http.StatusForbidden)
	bad := cancel
	bad.body = jsonBody(t, map[string]any{"note": strings.Repeat("ä", 501)})
	if answer := b.call(t, office, bad, http.StatusUnprocessableEntity); !containsJSON(answer.body, `"location":"body.note"`) {
		t.Fatalf("invalid cancellation note: %s", answer.body)
	}
	type event struct {
		Status                       string
		Registered, Invited, Waiting int
		Sessions                     []struct{ Status string }
	}
	canceled := read[event](t, b.call(t, office, cancel, http.StatusOK))
	if canceled.Status != "canceled" || canceled.Registered != 0 || canceled.Invited != 0 || canceled.Waiting != 0 ||
		len(canceled.Sessions) != 3 || canceled.Sessions[0].Status != "scheduled" || canceled.Sessions[1].Status != "scheduled" ||
		canceled.Sessions[2].Status != "canceled" {
		t.Fatalf("event or session cancellation: %+v", canceled)
	}
	expectConflict(t, b, office, cancel, "closed")

	roster.method, roster.body = http.MethodGet, ""
	items := read[struct {
		Items []struct{ ID, Status, Payment, CancellationReceiptID string }
	}](t, b.call(t, office, roster, http.StatusOK)).Items
	retainedRefunded := false
	for _, item := range items {
		if item.Status != "canceled" {
			t.Errorf("registration was left open: %+v", item)
		}
		if item.ID == retainedID {
			retainedRefunded = item.Payment == "refunded" && item.CancellationReceiptID != ""
		}
	}
	if !retainedRefunded {
		t.Error("the event did not refund a previously canceled registration with a retained payment")
	}
	outbox := read[struct {
		Mails []struct{ To, Text, Template string }
	}](t, b.fixture(t, http.MethodGet, "outbox", "", http.StatusOK))
	var memberNotices, guestNotices int
	for _, mail := range outbox.Mails {
		if mail.Template != "event-canceled" {
			continue
		}
		if mail.To == "clara.amberly@example.org" {
			memberNotices++
			if !strings.Contains(mail.Text, "Fixture venue closed") || !strings.Contains(mail.Text, "refunded") {
				t.Errorf("family cancellation mail did not describe the note and refund: %q", mail.Text)
			}
		}
		if mail.To == "guest-cancellation@example.test" {
			guestNotices++
		}
	}
	if memberNotices != 1 || guestNotices != 1 {
		t.Fatalf("wanted one event notice per recipient, got %d family and %d guest notices", memberNotices, guestNotices)
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
		ID, Number, PaymentMethod, Currency, Reason string
		Cancels, CanceledBy                         struct{ ID string }
		Net, VAT, Gross                             int64
		Cancelable                                  bool
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
		storno.Net != -original.Net || storno.VAT != -original.VAT || storno.Number == original.Number ||
		storno.Reason != "Fixture correction" || storno.Cancelable {
		t.Fatalf("storno does not reverse the original receipt: %+v", storno)
	}
	if repeat := read[struct{ Reason string }](t, b.call(t, office, cancel, http.StatusConflict)); repeat.Reason != "already-canceled" {
		t.Fatalf("unexpected repeat cancellation: %+v", repeat)
	}
	stornoPDF := pdf
	stornoPDF.params = map[string]string{"receiptId": storno.ID}
	if doc := b.call(t, office, stornoPDF, http.StatusOK); !bytes.HasPrefix(doc.body, []byte("%PDF-")) {
		t.Fatal("expected the stored cancellation PDF")
	}
	cancelStorno := cancel
	cancelStorno.params = stornoPDF.params
	if invalid := read[struct{ Reason string }](t, b.call(t, office, cancelStorno, http.StatusConflict)); invalid.Reason != "cancellation" {
		t.Fatalf("unexpected cancellation of a cancellation: %+v", invalid)
	}

	stored := read[receipt](t, b.call(t, office, getReceipt, http.StatusOK))
	if stored.CanceledBy.ID != storno.ID || stored.Cancelable ||
		stored.Gross != original.Gross || stored.Number != original.Number {
		t.Errorf("original receipt was altered or is missing the storno link: %+v", stored)
	}
}

// Create a one-off lesson in a seeded room on a released school date.
func createLesson(t *testing.T, b *backend, office *http.Client, kind string, capacity int) string {
	t.Helper()

	data := lessonFixture(t, b, office, kind, capacity)
	in := request{method: http.MethodPost, path: "/api/v1/lessons", body: jsonBody(t, data)}

	return read[struct{ ID string }](t, b.call(t, office, in, http.StatusCreated)).ID
}

func lessonFixture(t *testing.T, b *backend, office *http.Client, kind string, capacity int) map[string]any {
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
			return map[string]any{
				"courseTypeId": class.CourseTypeID, "date": "2026-10-24", "startTime": "08:00",
				"durationMinutes": 60, "roomId": class.RoomID, "instructorId": class.Instructor.ID,
				"capacity": capacity, "visibility": "public", "kind": kind, "confirmRoomOverlap": true,
			}
		}
	}

	t.Fatal("the fixture has no class with a complete catalog and room assignment")

	return nil
}

func oneOffLessonCreation(t *testing.T, b *backend) {
	t.Helper()

	office, member := browser(t), browser(t)
	login(t, b, office, "olga.ledgerby@example.net", demoPassword)
	login(t, b, member, demoMember, demoPassword)
	data := lessonFixture(t, b, office, "regular", 2)
	delete(data, "confirmRoomOverlap")
	data["startTime"] = "02:00"
	const path = "/api/v1/lessons"
	call := func(client *http.Client, status int) answer {
		t.Helper()
		return b.call(t, client, request{method: http.MethodPost, path: path, body: jsonBody(t, data)}, status)
	}
	call(browser(t), http.StatusUnauthorized)
	call(member, http.StatusForbidden)

	type lesson struct {
		ID, StartsAt, EndsAt, Kind, Renter, Visibility, ClassID, Status string
		Capacity, FreePlaces                                            int
		Admin                                                           *struct {
			ActiveBookings int
			Detached       bool
		}
	}
	created := read[lesson](t, call(office, http.StatusCreated))
	if created.ID == "" || created.ClassID != "" || created.Kind != "regular" || created.Status != "scheduled" ||
		created.StartsAt != "2026-10-24T00:00:00Z" || created.EndsAt != "2026-10-24T01:00:00Z" ||
		created.FreePlaces != 2 || created.Admin == nil || created.Admin.ActiveBookings != 0 || created.Admin.Detached {
		t.Fatalf("unexpected one-off lesson: %+v", created)
	}
	stored := request{method: http.MethodGet, path: path + "/{lessonId}", params: map[string]string{"lessonId": created.ID}}
	if saved := read[lesson](t, b.call(t, office, stored, http.StatusOK)); saved.ID != created.ID || saved.StartsAt != created.StartsAt {
		t.Fatalf("created lesson was not stored: %+v", saved)
	}
	taken := read[struct {
		Reason string
		Lesson *struct{ ID, StartsAt string }
	}](t, call(office, http.StatusConflict))
	if taken.Reason != "room-taken" || taken.Lesson == nil || taken.Lesson.ID != created.ID ||
		taken.Lesson.StartsAt != created.StartsAt {
		t.Fatalf("unexpected room conflict: %+v", taken)
	}
	data["confirmRoomOverlap"] = true
	call(office, http.StatusCreated)
	delete(data, "confirmRoomOverlap")
	data["startTime"] = "03:00"
	call(office, http.StatusCreated)
	data["startTime"] = "23:30"
	if failed := call(office, http.StatusUnprocessableEntity); !containsJSON(failed.body, `"location":"body.durationMinutes"`) {
		t.Fatalf("crossing midnight did not identify the duration: %s", failed.body)
	}
	data["startTime"] = "03:00"
	data["date"] = "2026-10-01"
	if failed := call(office, http.StatusConflict); !containsJSON(failed.body, `"reason":"past"`) {
		t.Fatalf("past lesson did not request confirmation: %s", failed.body)
	}
	data["confirmPast"] = true
	call(office, http.StatusCreated)
	delete(data, "confirmPast")
	data["date"] = "2026-10-25"
	data["startTime"] = "02:30"
	data["capacity"], data["kind"], data["visibility"], data["renter"] = 0, "rental", "hidden", " Fixture renter "
	delete(data, "instructorId")
	delete(data, "courseTypeId")
	rental := read[lesson](t, call(office, http.StatusCreated))
	if rental.Kind != "rental" || rental.Visibility != "hidden" || rental.Renter != "Fixture renter" || rental.Capacity != 0 ||
		rental.StartsAt != "2026-10-25T00:30:00Z" || rental.EndsAt != "2026-10-25T01:30:00Z" {
		t.Fatalf("unexpected rental on the repeated school-local hour: %+v", rental)
	}
	stored.params["lessonId"] = rental.ID
	b.call(t, member, stored, http.StatusNotFound)
}

func passTypeAdministration(t *testing.T, b *backend) {
	t.Helper()

	const catalog = "/api/v1/admin/pass-types"
	type passType struct {
		ID, Name, Kind, CreatedAt            string
		Visits, ValidityDays, Price, VATRate int
		Active                               bool
	}
	admin, member := browser(t), browser(t)
	login(t, b, admin, "olga.ledgerby@example.net", demoPassword)
	list := request{method: http.MethodGet, path: catalog}
	b.call(t, member, list, http.StatusUnauthorized)
	login(t, b, member, demoMember, demoPassword)
	b.call(t, member, list, http.StatusForbidden)

	types := func() []passType {
		t.Helper()

		return read[struct{ Items []passType }](t, b.call(t, admin, list, http.StatusOK)).Items
	}
	var used passType
	for _, item := range types() {
		if item.Name == "30-day pass" {
			used = item
			break
		}
	}
	if used.ID == "" || !used.Active {
		t.Fatal("expected an active, previously sold 30-day pass type")
	}

	create := request{
		method: http.MethodPost, path: catalog,
		body: `{"name":" Fixture yearly pass ","kind":"time","validityDays":365,"price":8000,"vatRate":1900,"active":true}`,
	}
	b.call(t, member, create, http.StatusForbidden)
	created := read[passType](t, b.call(t, admin, create, http.StatusCreated))
	if created.ID == "" || created.Name != "Fixture yearly pass" || created.Kind != "time" ||
		created.CreatedAt != demoNow || created.Visits != 0 || created.Price != 8000 || !created.Active {
		t.Fatalf("created pass type: %+v", created)
	}
	duplicate := create
	duplicate.body = strings.Replace(duplicate.body, "Fixture", "fixture", 1)
	expectConflict(t, b, admin, duplicate, "duplicate")

	update := request{
		method: http.MethodPut, path: catalog + "/{passTypeId}",
		params: map[string]string{"passTypeId": created.ID},
		body:   `{"name":"Fixture 12 visits","kind":"visits","visits":12,"validityDays":90,"price":6000,"vatRate":0,"active":true}`,
	}
	replaced := read[passType](t, b.call(t, admin, update, http.StatusOK))
	if replaced.ID != created.ID || replaced.CreatedAt != created.CreatedAt || replaced.Name != "Fixture 12 visits" ||
		replaced.Kind != "visits" || replaced.Visits != 12 || replaced.ValidityDays != 90 || replaced.Price != 6000 || !replaced.Active {
		t.Fatalf("updated pass type: %+v", replaced)
	}

	remove := request{method: http.MethodDelete, path: update.path, params: map[string]string{"passTypeId": used.ID}}
	expectConflict(t, b, admin, remove, "in-use")
	seenUsed := false
	for _, item := range types() {
		if item.ID == used.ID {
			seenUsed = true
			if item != used {
				t.Fatalf("in-use deletion changed the existing pass type: %+v", item)
			}
		}
	}
	if !seenUsed {
		t.Fatal("the previously sold pass type disappeared after the in-use conflict")
	}

	retire := request{method: http.MethodPost, path: update.path + "/retire", params: update.params}
	replaced.Active = false
	for range 2 {
		if got := read[passType](t, b.call(t, admin, retire, http.StatusOK)); got != replaced {
			t.Fatalf("retiring the pass type changed unexpected fields: got %+v, want %+v", got, replaced)
		}
	}
	remove.params = update.params
	b.call(t, admin, remove, http.StatusNoContent)
	b.call(t, admin, retire, http.StatusNotFound)
}

func passLists(t *testing.T, b *backend) {
	t.Helper()

	type person struct{ ID, Name string }
	type passUse struct {
		BookingID, BookingStatus, RedeemedAt string
		Lesson                               struct{ ID, StartsAt string }
	}
	type pass struct {
		ID, ReceiptID, SoldAt, Kind      string
		Owner                            person
		Payer                            *person
		PassType                         struct{ ID, Name string }
		Usable                           bool
		UseCount, VisitsTotal, PricePaid int
		Uses                             []passUse
	}
	type wallet struct {
		Timezone, Today, RedemptionMode string
		Items                           []pass
	}

	member := browser(t)
	mine := request{method: http.MethodGet, path: "/api/v1/my/passes"}
	b.call(t, member, mine, http.StatusUnauthorized)
	login(t, b, member, "clara.amberly@example.org", demoPassword)
	view := read[wallet](t, b.call(t, member, mine, http.StatusOK))
	if view.Timezone != "Europe/Berlin" || view.Today != demoDate || view.RedemptionMode != "attendance" || len(view.Items) != 2 {
		t.Fatalf("unexpected family pass wallet: %+v", view)
	}

	child, parent := view.Items[0], view.Items[1]
	if child.Owner.Name != "Ella Amberly" || parent.Owner.Name != "Clara Amberly" || child.Payer == nil ||
		*child.Payer != parent.Owner || parent.Payer != nil || !child.Usable || !parent.Usable {
		t.Fatalf("family pass ownership, payer or ordering is wrong: child %+v, parent %+v", child, parent)
	}
	for _, item := range view.Items {
		if item.ID == "" || item.ReceiptID == "" || item.PricePaid <= 0 || item.Kind != "visits" ||
			item.VisitsTotal != 10 || item.PassType.Name != "10-visit pass" || item.UseCount != len(item.Uses) || item.UseCount == 0 {
			t.Fatalf("pass is missing its purchase or redemption history: %+v", item)
		}
		for i, use := range item.Uses {
			if use.BookingID == "" || use.BookingStatus != "attended" || use.RedeemedAt == "" ||
				use.Lesson.ID == "" || use.Lesson.StartsAt == "" || (i > 0 && use.Lesson.StartsAt > item.Uses[i-1].Lesson.StartsAt) {
				t.Fatalf("unexpected pass use or lesson order: %+v", use)
			}
		}
	}

	ref := request{
		method: http.MethodGet, path: "/api/v1/admin/persons/{personId}/passes",
		params: map[string]string{"personId": child.Owner.ID},
	}
	b.call(t, member, ref, http.StatusForbidden)
	office := browser(t)
	login(t, b, office, "olga.ledgerby@example.net", demoPassword)
	owned := read[struct{ Items []pass }](t, b.call(t, office, ref, http.StatusOK)).Items
	if len(owned) != 1 || owned[0].ID != child.ID || owned[0].Owner != child.Owner || owned[0].UseCount != child.UseCount {
		t.Fatalf("office pass list did not select Ella's pass: %+v", owned)
	}
	ref.params["personId"] = "a961f110-a017-7241-8123-123456789abc"
	b.call(t, office, ref, http.StatusNotFound)
}
