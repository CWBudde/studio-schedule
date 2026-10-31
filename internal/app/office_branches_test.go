package app_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/app"
	"github.com/cwbudde/studio-schedule/internal/mail"
	"github.com/cwbudde/studio-schedule/internal/seed"
	"github.com/cwbudde/studio-schedule/internal/service"
)

// csvUpload is the JSON members of an import request for a file.
func csvUpload(name, content string) string {
	return `"fileName":"` + name + `","content":"` + base64.StdEncoding.EncodeToString([]byte(content)) + `"`
}

// importMappingJSON maps the columns of the files of these tests.
const importMappingJSON = `"mapping":{"columns":{"memberNo":"Mitgliedsnummer","givenName":"Vorname","familyName":"Nachname","email":"E-Mail"}}`

// addPerson creates a person through the office API and returns its
// ID.
func (a *authApp) addPerson(body string, office *http.Cookie) string {
	a.t.Helper()

	id, _ := decode(a.t, a.expect(http.StatusCreated, http.MethodPost, "/api/v1/admin/persons", body, office))["id"].(string)

	return id
}

// TestPersonDetailStaff: the office's person detail lists the staff
// members linked to the person.
func TestPersonDetailStaff(t *testing.T) {
	t.Parallel()

	a := newAuthApp(t, app.Deps{})
	office := a.login("olga.ledgerby@example.net")
	alina := a.personID("alina starling", office)

	var detail struct {
		Staff []struct {
			ID, DisplayName, Kind string
			Active                bool
			Person                struct{ ID, GivenName string }
		} `json:"staff"`
	}
	if err := json.Unmarshal(a.expect(http.StatusOK, http.MethodGet, "/api/v1/admin/persons/"+alina, "", office), &detail); err != nil {
		t.Fatal(err)
	}

	if len(detail.Staff) != 1 {
		t.Fatalf("staff %+v, want Alina's staff record", detail.Staff)
	}

	if s := detail.Staff[0]; s.ID == "" || s.DisplayName == "" || s.Kind != "instructor" || !s.Active ||
		s.Person.ID != alina || s.Person.GivenName != "Alina" {
		t.Errorf("staff %+v", s)
	}
}

// TestPersonDetailDisabledAccount: a disabled account shows as disabled
// in the person detail, and as active again once enabled.
func TestPersonDetailDisabledAccount(t *testing.T) {
	t.Parallel()

	a := newAuthApp(t, app.Deps{})
	office := a.login("olga.ledgerby@example.net")
	admin := a.login("arno.keystone@example.net")
	oskar := a.personID("oskar nettlebury", office)
	accountPath := "/api/v1/admin/accounts/" + a.accountIDByEmail("oskar.nettlebury@example.org", admin)

	status := func() any {
		acc, _ := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/admin/persons/"+oskar, "", office))["account"].(map[string]any)

		return acc["status"]
	}

	a.expect(http.StatusOK, http.MethodPost, accountPath+"/disable", "", admin)

	if got := status(); got != "disabled" {
		t.Errorf("status %v, want disabled", got)
	}

	a.expect(http.StatusOK, http.MethodPost, accountPath+"/enable", "", admin)

	if got := status(); got != "active" {
		t.Errorf("status %v after enabling, want active", got)
	}
}

// TestImportInspectNoRows: a file with a header and no data rows is read;
// its sample is an empty list, not null.
func TestImportInspectNoRows(t *testing.T) {
	t.Parallel()

	a := newAuthApp(t, app.Deps{})
	office := a.login("olga.ledgerby@example.net")

	body := a.expect(http.StatusOK, http.MethodPost, "/api/v1/admin/imports/inspect",
		"{"+csvUpload("header-only.csv", "Mitgliedsnummer;Vorname;Nachname\n")+"}", office)

	insp := decode(t, body)
	if insp["rows"] != 0.0 || !strings.Contains(string(body), `"sample":[]`) {
		t.Errorf("inspection %s", body)
	}

	if headers, _ := insp["headers"].([]any); len(headers) != 3 || headers[0] != "Mitgliedsnummer" {
		t.Errorf("headers %v", insp["headers"])
	}
}

// TestImportRowDuplicates: a new member with the email of a person the
// school has lists that person as a possible duplicate, in the dry run and
// the applied run.
func TestImportRowDuplicates(t *testing.T) {
	t.Parallel()

	a := newAuthApp(t, app.Deps{})
	office := a.login("olga.ledgerby@example.net")
	kai := a.personID("kai lanternby", office)
	file := csvUpload("members.csv", "Mitgliedsnummer;Vorname;Nachname;E-Mail\nM-9001;Kai;Lanternby;kai.lanternby@example.org\n")

	for _, apply := range []string{"false", "true"} {
		var res struct {
			Applied bool
			Rows    []struct {
				Action     string
				Duplicates []struct{ ID, GivenName, FamilyName, Email, MemberNo string }
			}
		}
		if err := json.Unmarshal(a.expect(http.StatusOK, http.MethodPost, "/api/v1/admin/imports",
			"{"+file+","+importMappingJSON+`,"apply":`+apply+"}", office), &res); err != nil {
			t.Fatal(err)
		}

		if len(res.Rows) != 1 || res.Rows[0].Action != "create" || len(res.Rows[0].Duplicates) != 1 {
			t.Fatalf("apply %s: rows %+v", apply, res.Rows)
		}

		if d := res.Rows[0].Duplicates[0]; d.ID != kai || d.GivenName != "Kai" || d.FamilyName != "Lanternby" ||
			d.Email != "kai.lanternby@example.org" || d.MemberNo != "" {
			t.Errorf("apply %s: duplicate %+v", apply, d)
		}
	}
}

// TestImportWhileImpersonating: an import run by an admin signed in as
// an office account is recorded as the admin's.
func TestImportWhileImpersonating(t *testing.T) {
	t.Parallel()

	a := newAuthApp(t, app.Deps{})
	asOffice := a.impersonateByEmail("olga.ledgerby@example.net", a.login("arno.keystone@example.net"))
	file := csvUpload("members.csv", "Mitgliedsnummer;Vorname;Nachname;E-Mail\nM-9002;Nele;Fictory;nele.fictory@example.org\n")

	a.expect(http.StatusOK, http.MethodPost, "/api/v1/admin/imports", "{"+file+","+importMappingJSON+`,"apply":true}`, asOffice)

	runs, _ := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/admin/imports", "", asOffice))["items"].([]any)
	if len(runs) != 1 {
		t.Fatalf("runs %v", runs)
	}

	if by, _ := runs[0].(map[string]any)["by"].(map[string]any); by["email"] != "arno.keystone@example.net" ||
		by["givenName"] != "Arno" {
		t.Errorf("run by %v, want the admin", by)
	}
}

// TestDuplicateWithAccount: a pair one of whose persons has an account
// names the account's email.
func TestDuplicateWithAccount(t *testing.T) {
	t.Parallel()

	a := newAuthApp(t, app.Deps{})
	office := a.login("olga.ledgerby@example.net")
	kai := a.personID("kai lanternby", office)
	twin := a.addPerson(`{"givenName":"Kai","familyName":"Lanternby","email":"Kai.Lanternby@example.org"}`, office)

	var list struct {
		Items []struct {
			A, B     struct{ ID, AccountEmail string }
			Strength string
			Reasons  []string
		}
	}
	if err := json.Unmarshal(a.expect(http.StatusOK, http.MethodGet, "/api/v1/admin/duplicates", "", office), &list); err != nil {
		t.Fatal(err)
	}

	found := false

	for _, p := range list.Items {
		pair := map[string]string{p.A.ID: p.A.AccountEmail, p.B.ID: p.B.AccountEmail}
		if _, ok := pair[twin]; !ok {
			continue
		}

		found = true

		if acc, ok := pair[kai]; !ok || acc != "kai.lanternby@example.org" || pair[twin] != "" || p.Strength != "strong" {
			t.Errorf("pair %+v", p)
		}
	}

	if !found {
		t.Fatalf("no pair of the new person in %+v", list.Items)
	}
}

// TestMergeGuardianCycle: a merge after which a guardian of the survivor
// would be its dependent too is a 409 guardian-cycle, preview or not.
func TestMergeGuardianCycle(t *testing.T) {
	t.Parallel()

	a := newAuthApp(t, app.Deps{})
	office := a.login("olga.ledgerby@example.net")
	survivor := a.addPerson(`{"givenName":"Mila","familyName":"Roundloop"}`, office)
	guardian := a.addPerson(`{"givenName":"Gus","familyName":"Roundloop"}`, office)
	merged := a.addPerson(`{"givenName":"Mila","familyName":"Roundloop","notes":"Second record."}`, office)

	// Gus is Mila's guardian, and the second Mila is Gus's.
	a.expect(http.StatusCreated, http.MethodPost, "/api/v1/admin/persons/"+survivor+"/guardians", `{"guardianId":"`+guardian+`"}`, office)
	a.expect(http.StatusCreated, http.MethodPost, "/api/v1/admin/persons/"+guardian+"/guardians", `{"guardianId":"`+merged+`"}`, office)

	for _, apply := range []string{"false", "true"} {
		body := decode(t, a.expect(http.StatusConflict, http.MethodPost, "/api/v1/admin/persons/merge",
			`{"survivorId":"`+survivor+`","mergedId":"`+merged+`","take":{},"apply":`+apply+`}`, office))
		if body["reason"] != "guardian-cycle" {
			t.Errorf("apply %s: %v", apply, body)
		}
	}

	// Nothing changed: the second Mila is no tombstone.
	if detail := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/admin/persons/"+merged, "", office)); detail["mergedInto"] != nil {
		t.Errorf("merged %v", detail)
	}
}

// TestMergeCancelsAndDropsMemberships: merging two members of the same
// class ends the duplicate's membership and cancels its bookings of the
// lessons that have not started, as the survivor is booked into them.
func TestMergeCancelsAndDropsMemberships(t *testing.T) {
	t.Parallel()

	a := newAuthApp(t, app.Deps{})
	office := a.login("olga.ledgerby@example.net")
	survivor := a.addPerson(`{"givenName":"Tess","familyName":"Doublebar"}`, office)
	merged := a.addPerson(`{"givenName":"Tess","familyName":"Doublebar"}`, office)

	terms := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/terms", "", office))
	term, _ := terms["items"].([]any)[0].(map[string]any)
	termID, _ := term["id"].(string)
	classes := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/terms/"+termID+"/classes", "", office))
	demo, _ := classes["items"].([]any)[0].(map[string]any)
	class := decode(t, a.expect(http.StatusCreated, http.MethodPost, "/api/v1/terms/"+termID+"/classes",
		`{"courseTypeId":"`+demo["courseTypeId"].(string)+`","roomId":"`+demo["roomId"].(string)+
			`","instructorId":"`+demo["instructor"].(map[string]any)["id"].(string)+`","level":"open","weekday":3,`+
			`"startTime":"20:15","durationMinutes":45,"capacity":10,"retired":false}`, office))
	classID, _ := class["id"].(string)

	for _, p := range []string{survivor, merged} {
		a.expect(http.StatusCreated, http.MethodPost, "/api/v1/classes/"+classID+"/members", `{"personId":"`+p+`","since":"2026-09-07"}`, office)
	}

	type summary struct {
		Applied       bool
		BookingsMoved int
		Canceled      []struct{ LessonID, StartsAt, CourseTypeName, Reason string }
		Dropped       []struct {
			ClassID, CourseTypeName, TermName, StartTime string
			Weekday                                      int
		} `json:"membershipsDropped"`
		MembershipsMoved int
	}

	for _, apply := range []bool{false, true} {
		var s summary
		if err := json.Unmarshal(a.expect(http.StatusOK, http.MethodPost, "/api/v1/admin/persons/merge",
			fmt.Sprintf(`{"survivorId":%q,"mergedId":%q,"take":{},"apply":%t}`, survivor, merged, apply), office), &s); err != nil {
			t.Fatal(err)
		}

		if s.Applied != apply || s.MembershipsMoved != 0 || len(s.Dropped) != 1 {
			t.Fatalf("apply %t: summary %+v", apply, s)
		}

		if d := s.Dropped[0]; d.ClassID != classID || d.TermName != term["name"] || d.Weekday != 3 ||
			d.StartTime != "20:15" || d.CourseTypeName == "" {
			t.Errorf("apply %t: dropped %+v", apply, d)
		}

		if len(s.Canceled) == 0 || len(s.Canceled) != s.BookingsMoved {
			t.Fatalf("apply %t: canceled %d of %d moved bookings", apply, len(s.Canceled), s.BookingsMoved)
		}

		for i, c := range s.Canceled {
			if c.Reason != "booked" || c.LessonID == "" || c.CourseTypeName != s.Dropped[0].CourseTypeName ||
				c.StartsAt <= "2026-10-23T16:00:00Z" || (i > 0 && c.StartsAt < s.Canceled[i-1].StartsAt) {
				t.Errorf("apply %t: canceled[%d] %+v", apply, i, c)
			}
		}
	}

	// The class has one member left, the survivor.
	members, _ := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/admin/classes/"+classID+"/members", "", office))["items"].([]any)
	if len(members) != 1 || members[0].(map[string]any)["person"].(map[string]any)["id"] != survivor {
		t.Errorf("members %v", members)
	}
}

// rejectingMailer refuses every mail for good.
type rejectingMailer struct{}

func (rejectingMailer) Send(context.Context, mail.Message) error {
	return fmt.Errorf("%w: recipient rejected", mail.ErrPermanent)
}

// TestRequeueFailedMail: a mail the mailer refused for good is failed; the
// admin queues it again, and it is pending with its attempts reset.
func TestRequeueFailedMail(t *testing.T) {
	t.Parallel()

	a := newAuthApp(t, app.Deps{})
	admin := a.login("arno.keystone@example.net")
	member := a.login(demoMember(t).Email)

	a.expect(http.StatusNoContent, http.MethodPost, "/api/v1/auth/password",
		`{"currentPassword":"`+seed.DemoPassword+`","newPassword":"a fresh passphrase"}`, member)

	// One delivery attempt, as the deliver-mail job makes (it skips the
	// startup run of app.Jobs).
	outbox := service.NewOutboxService(a.deps.Store, a.deps.Clock, rejectingMailer{}, nil)
	if sent, err := outbox.Deliver(t.Context()); err != nil || sent != 0 {
		t.Fatalf("deliver: %d, %v", sent, err)
	}

	failed := a.mails(`{"status":"failed","template":"password-changed"}`, admin)
	if len(failed) != 1 || failed[0].To != demoMember(t).Email {
		t.Fatalf("failed mails %+v", failed)
	}

	var item struct {
		ID, Status, Template, LastError, Body, NextAttemptAt string
		Attempts                                             int
	}
	if err := json.Unmarshal(a.expect(http.StatusOK, http.MethodPost, "/api/v1/admin/mails/"+failed[0].ID+"/requeue", "", admin), &item); err != nil {
		t.Fatal(err)
	}

	if item.ID != failed[0].ID || item.Status != "pending" || item.Attempts != 0 || item.Template != "password-changed" ||
		item.NextAttemptAt != "2026-10-23T16:00:00Z" || !strings.Contains(item.LastError, "recipient rejected") || item.Body != "shown" {
		t.Errorf("requeued %+v", item)
	}

	if pending := a.mails(`{"status":"pending","template":"password-changed"}`, admin); len(pending) != 1 {
		t.Errorf("pending after the requeue %+v", pending)
	}

	// It is audited.
	events, _ := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/admin/audit", "", admin))["items"].([]any)
	if e, _ := events[0].(map[string]any); e["action"] != "mail-requeued" || e["details"].(map[string]any)["mailId"] != item.ID {
		t.Errorf("audit %v", events[0])
	}
}

// TestMailCursorInvalid: a mail log cursor that is not base64url, has a
// bad time or a bad mail ID is a 422 on body.cursor.
func TestMailCursorInvalid(t *testing.T) {
	t.Parallel()

	a := newAuthApp(t, app.Deps{})
	admin := a.login("arno.keystone@example.net")
	enc := base64.RawURLEncoding.EncodeToString
	id := "01890a5d-ac96-774b-bcce-b302099a8057"

	for name, cursor := range map[string]string{
		"not base64":    "*not-base64*",
		"padded base64": base64.URLEncoding.EncodeToString([]byte("1_" + id)),
		"no separator":  enc([]byte("1761235200000000")),
		"time not int":  enc([]byte("yesterday_" + id)),
		"time zero":     enc([]byte("0_" + id)),
		"time negative": enc([]byte("-5_" + id)),
		"bad mail ID":   enc([]byte("1761235200000000_not-an-id")),
		"empty mail ID": enc([]byte("1761235200000000_")),
	} {
		body := a.expect(http.StatusUnprocessableEntity, http.MethodPost, "/api/v1/admin/mails/search",
			`{"cursor":"`+cursor+`"}`, admin)
		if loc := errorLocation(t, body); loc != "body.cursor" {
			t.Errorf("%s: location %q", name, loc)
		}
	}

	// A well-formed cursor past every mail is an empty page.
	page := decode(t, a.expect(http.StatusOK, http.MethodPost, "/api/v1/admin/mails/search",
		`{"cursor":"`+enc([]byte("1_"+id))+`"}`, admin))
	if items, _ := page["items"].([]any); len(items) != 0 || page["nextCursor"] != nil {
		t.Errorf("page %v", page)
	}
}

// TestDemoAttachmentOutsideDemo: the demo mailbox's attachments are a
// 404 when the app is not the demo.
func TestDemoAttachmentOutsideDemo(t *testing.T) {
	t.Parallel()

	a := newAuthApp(t, app.Deps{})

	a.anonymous(http.StatusNotFound, "/api/v1/demo/mailbox/01890a5d-ac96-774b-bcce-b302099a8057/attachment")

	// Not even for a mail that exists, signed in as the admin.
	admin := a.login("arno.keystone@example.net")
	a.expect(http.StatusNoContent, http.MethodPost, "/api/v1/auth/password",
		`{"currentPassword":"`+seed.DemoPassword+`","newPassword":"a fresh passphrase"}`, admin)

	items := a.mails(`{"template":"password-changed"}`, admin)
	if len(items) != 1 {
		t.Fatalf("mails %+v", items)
	}

	a.expect(http.StatusNotFound, http.MethodGet, "/api/v1/demo/mailbox/"+items[0].ID+"/attachment", "", admin)
}
