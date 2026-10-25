package app_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/app"
)

// passBody is the part of a Pass the tests inspect.
type passBody struct {
	ID       string `json:"id"`
	PassType struct {
		ID, Name string
	} `json:"passType"`
	Owner           struct{ ID, Name string }  `json:"owner"`
	Kind            string                     `json:"kind"`
	VisitsTotal     int                        `json:"visitsTotal"`
	VisitsRemaining int                        `json:"visitsRemaining"`
	ValidFrom       string                     `json:"validFrom"`
	ValidUntil      string                     `json:"validUntil"`
	Status          string                     `json:"status"`
	Usable          bool                       `json:"usable"`
	Origin          string                     `json:"origin"`
	Payer           *struct{ ID, Name string } `json:"payer"`
	PricePaid       int64                      `json:"pricePaid"`
	VoidedAt        string                     `json:"voidedAt"`
	Note            string                     `json:"note"`
	UseCount        int                        `json:"useCount"`
	Uses            []struct {
		BookingID     string `json:"bookingId"`
		PersonName    string `json:"personName"`
		BookingStatus string `json:"bookingStatus"`
		Lesson        struct {
			ID         string                `json:"id"`
			CourseType struct{ Name string } `json:"courseType"`
		} `json:"lesson"`
	} `json:"uses"`
}

func unmarshal[T any](t *testing.T, body []byte) T {
	t.Helper()

	var v T
	if err := json.Unmarshal(body, &v); err != nil {
		t.Fatalf("invalid JSON %s: %v", body, err)
	}

	return v
}

// TestPasses runs the passes over HTTP on the demo (ADR 0012): the
// catalog, a sale paid by a parent, the family's view, voiding, giving a
// pass away, the redemption mode and the payment of bookings. The rules
// are tested in package service.
func TestPasses(t *testing.T) {
	t.Parallel()

	a := newAuthApp(t, app.Deps{})
	olga := a.login("olga.ledgerby@example.net")
	arno := a.login("arno.keystone@example.net")
	clara := a.login("clara.amberly@example.org")

	types := unmarshal[struct {
		Items []struct {
			ID, Name, Kind string
			Visits         int
		}
	}](t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/admin/pass-types", "", olga))
	if len(types.Items) != 3 || types.Items[0].Name != "10-visit pass" || types.Items[2].Name != "Single visit" {
		t.Fatalf("pass types %+v", types)
	}

	a.expect(http.StatusForbidden, http.MethodGet, "/api/v1/admin/pass-types", "", clara)

	created := decode(t, a.expect(http.StatusCreated, http.MethodPost, "/api/v1/admin/pass-types",
		`{"name":"5-visit pass","kind":"visits","visits":5,"validityDays":60,"price":8000,"vatRate":1900,"active":true}`, olga))
	if created["name"] != "5-visit pass" || created["visits"] != 5.0 || created["createdAt"] != "2026-10-23T16:00:00Z" {
		t.Fatalf("created %v", created)
	}

	conflict := decode(t, a.expect(http.StatusConflict, http.MethodPost, "/api/v1/admin/pass-types",
		`{"name":"5-VISIT PASS","kind":"time","validityDays":7,"price":0,"vatRate":0,"active":true}`, olga))
	if conflict["reason"] != "duplicate" {
		t.Errorf("duplicate: %v", conflict)
	}

	invalid := decode(t, a.expect(http.StatusUnprocessableEntity, http.MethodPost, "/api/v1/admin/pass-types",
		`{"name":"Odd","kind":"time","visits":3,"validityDays":7,"price":0,"vatRate":0,"active":true}`, olga))
	if errs, _ := invalid["errors"].([]any); len(errs) != 1 || errs[0].(map[string]any)["location"] != "body.visits" {
		t.Errorf("time pass with visits: %v", invalid)
	}

	persons := func(q string) string {
		t.Helper()

		found := unmarshal[struct{ Items []struct{ ID string } }](t,
			a.expect(http.StatusOK, http.MethodPost, "/api/v1/admin/persons/search", searchBody(q), olga))
		if len(found.Items) != 1 {
			t.Fatalf("persons %q: %+v", q, found)
		}

		return found.Items[0].ID
	}
	finn, claraID := persons("finn amberly"), persons("clara amberly")

	// Clara pays for her son's pass; it starts today.
	sold := unmarshal[passBody](t, a.expect(http.StatusCreated, http.MethodPost,
		"/api/v1/admin/persons/"+finn+"/passes/sell",
		`{"passTypeId":"`+created["id"].(string)+`","payerId":"`+claraID+`","note":"Paid in cash"}`, olga))
	if sold.Kind != "visits" || sold.VisitsTotal != 5 || sold.VisitsRemaining != 5 || sold.ValidFrom != "2026-10-23" ||
		sold.ValidUntil != "2026-12-21" || sold.Status != "active" || !sold.Usable || sold.Origin != "sale" ||
		sold.PricePaid != 8000 || sold.Payer == nil || sold.Payer.Name != "Clara Amberly" ||
		sold.Owner.Name != "Finn Amberly" || sold.Note != "Paid in cash" || sold.Uses == nil || len(sold.Uses) != 0 {
		t.Fatalf("sold %+v", sold)
	}

	a.expect(http.StatusForbidden, http.MethodPost, "/api/v1/admin/persons/"+finn+"/passes/sell",
		`{"passTypeId":"`+created["id"].(string)+`"}`, clara)
	a.expect(http.StatusNotFound, http.MethodPost, "/api/v1/admin/persons/00000000-0000-7000-8000-00000000f001/passes/sell",
		`{"passTypeId":"`+created["id"].(string)+`"}`, olga)

	// The family sees it, Clara's own card with its drop-ins, and her
	// daughter's; the office's note stays with the office.
	mine := unmarshal[struct {
		Timezone, Today string
		Items           []passBody
	}](t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/my/passes", "", clara))
	if mine.Timezone != "Europe/Berlin" || mine.Today != "2026-10-23" || len(mine.Items) != 3 {
		t.Fatalf("my passes %+v", mine)
	}

	owners := map[string]passBody{}
	for _, p := range mine.Items {
		owners[p.Owner.Name] = p

		if p.Note != "" {
			t.Errorf("a member sees the note of %+v", p)
		}
	}

	if own := owners["Clara Amberly"]; own.VisitsRemaining != 8 || len(own.Uses) != 2 || own.UseCount != 2 ||
		own.Uses[0].Lesson.CourseType.Name != "Contemporary" || own.Uses[0].BookingStatus != "attended" {
		t.Errorf("Clara's pass %+v", own)
	}

	if ella := owners["Ella Amberly"]; ella.Payer == nil || ella.Payer.Name != "Clara Amberly" || len(ella.Uses) != 1 {
		t.Errorf("Ella's pass %+v", ella)
	}

	// Voided once, then a conflict.
	voided := unmarshal[passBody](t, a.expect(http.StatusOK, http.MethodPost, "/api/v1/admin/passes/"+sold.ID+"/void", "", olga))
	if voided.Status != "voided" || voided.VoidedAt != "2026-10-23T16:00:00Z" || voided.Usable {
		t.Errorf("voided %+v", voided)
	}

	again := decode(t, a.expect(http.StatusConflict, http.MethodPost, "/api/v1/admin/passes/"+sold.ID+"/void", "", olga))
	if again["reason"] != "already-voided" {
		t.Errorf("void again: %v", again)
	}

	given := unmarshal[passBody](t, a.expect(http.StatusCreated, http.MethodPost,
		"/api/v1/admin/persons/"+finn+"/passes/assign", `{"passTypeId":"`+types.Items[2].ID+`","note":"Goodwill"}`, olga))
	if given.Origin != "assignment" || given.PricePaid != 0 || given.Payer != nil || given.Note != "Goodwill" {
		t.Errorf("given %+v", given)
	}

	finns := unmarshal[struct{ Items []passBody }](t,
		a.expect(http.StatusOK, http.MethodGet, "/api/v1/admin/persons/"+finn+"/passes", "", olga))
	if len(finns.Items) != 2 || finns.Items[0].ID != given.ID || finns.Items[1].Status != "voided" {
		t.Errorf("Finn's passes %+v", finns)
	}

	// The redemption mode: the office reads it, an admin sets it.
	settings := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/admin/settings/passes", "", olga))
	if settings["redemptionMode"] != "attendance" {
		t.Errorf("settings %v", settings)
	}

	a.expect(http.StatusForbidden, http.MethodPut, "/api/v1/admin/settings/passes", `{"redemptionMode":"booking"}`, olga)
	a.expect(http.StatusUnprocessableEntity, http.MethodPut, "/api/v1/admin/settings/passes", `{"redemptionMode":"never"}`, arno)

	settings = decode(t, a.expect(http.StatusOK, http.MethodPut, "/api/v1/admin/settings/passes",
		`{"redemptionMode":"booking"}`, arno))
	if settings["redemptionMode"] != "booking" {
		t.Errorf("updated settings %v", settings)
	}

	// Clara's drop-ins in her history are paid by her card; in the lesson's
	// participant list the class members' bookings are free.
	history := unmarshal[struct {
		Items []struct {
			PersonName string
			Booking    struct{ ID, Status, Source string }
			Lesson     struct{ ID string }
			Payment    struct {
				Status string
				Pass   *struct {
					Name            string
					VisitsRemaining int
				}
			}
		}
	}](t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/my/lessons/history", "", clara))

	var lesson string

	for _, it := range history.Items {
		if it.Booking.Source != "self" {
			continue
		}

		left := map[string]int{"Clara Amberly": 8, "Ella Amberly": 9}[it.PersonName]
		if it.Payment.Status != "paid" || it.Payment.Pass == nil || it.Payment.Pass.Name != "10-visit pass" ||
			it.Payment.Pass.VisitsRemaining != left {
			t.Errorf("drop-in %+v", it)
		}

		lesson = it.Lesson.ID
	}

	if lesson == "" {
		t.Fatalf("no drop-in in Clara's history: %+v", history)
	}

	participants := unmarshal[struct {
		Bookings []struct {
			Name, Source string
			Payment      struct{ Status string }
		}
	}](t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/lessons/"+lesson+"/participants", "", olga))

	for _, b := range participants.Bookings {
		want := "free"
		if b.Source == "self" {
			want = "paid"
		}

		if b.Payment.Status != want {
			t.Errorf("participant %+v, want %s", b, want)
		}
	}
}
