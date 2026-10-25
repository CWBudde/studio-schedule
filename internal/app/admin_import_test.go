package app_test

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/app"
	"github.com/cwbudde/studio-schedule/internal/service/servicetest"
)

// TestAdminImport runs the member import over HTTP (ADR 0015) on the demo:
// the sample file is read, tried and imported, and the run listed.
func TestAdminImport(t *testing.T) {
	t.Parallel()

	a := newAuthApp(t, app.Deps{Demo: true})
	office := a.login("olga.ledgerby@example.net")
	file := `"fileName":"members-sample.csv","content":"` + base64.StdEncoding.EncodeToString(servicetest.SampleCSV) + `"`

	insp := decode(t, a.expect(http.StatusOK, http.MethodPost, "/api/v1/admin/imports/inspect", "{"+file+"}", office))
	if insp["encoding"] != "utf-8" || insp["delimiter"] != "semicolon" || insp["rows"] != 6.0 {
		t.Fatalf("inspection %v", insp)
	}

	suggested, _ := insp["suggested"].(map[string]any)
	columns, _ := suggested["columns"].(map[string]any)

	if columns["memberNo"] != "Mitgliedsnummer" || columns["guardian2Email"] != "E-Mail Elternteil 2" {
		t.Errorf("suggested %v", columns)
	}

	mapping, err := json.Marshal(map[string]any{"columns": columns, "notes": []string{"Kurs"}})
	if err != nil {
		t.Fatal(err)
	}

	dry := decode(t, a.expect(http.StatusOK, http.MethodPost, "/api/v1/admin/imports",
		"{"+file+`,"mapping":`+string(mapping)+"}", office))

	want := map[string]any{
		"rows": 6.0, "created": 4.0, "updated": 1.0, "unchanged": 0.0, "failed": 1.0, "guardiansCreated": 2.0,
		"guardiansUpdated": 0.0, "linksAdded": 5.0,
	}
	if dry["applied"] != false || dry["runId"] != nil || !sameJSON(dry["counts"], want) {
		t.Errorf("dry run %v", dry)
	}

	// Nothing was written: Paula is not found, and there is no run yet.
	if found := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/admin/persons?q=paula", "", office)); len(found["items"].([]any)) != 0 {
		t.Errorf("the dry run created %v", found)
	}

	if runs := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/admin/imports", "", office)); len(runs["items"].([]any)) != 0 {
		t.Errorf("runs after the dry run %v", runs)
	}

	applied := decode(t, a.expect(http.StatusOK, http.MethodPost, "/api/v1/admin/imports",
		"{"+file+`,"mapping":`+string(mapping)+`,"apply":true}`, office))
	if applied["applied"] != true || applied["runId"] == nil || !sameJSON(applied["counts"], want) {
		t.Errorf("import %v", applied)
	}

	a.personID("paula amberly", office)

	runs := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/admin/imports", "", office))["items"].([]any)
	run, _ := runs[0].(map[string]any)

	if len(runs) != 1 || run["fileName"] != "members-sample.csv" || run["by"].(map[string]any)["email"] != "olga.ledgerby@example.net" {
		t.Errorf("runs %v", runs)
	}

	if errs, _ := run["errors"].([]any); len(errs) != 1 || errs[0].(map[string]any)["field"] != "birthDate" {
		t.Errorf("run errors %v", run["errors"])
	}

	// A mapping without the member number, and an unreadable file.
	bad := decode(t, a.expect(http.StatusUnprocessableEntity, http.MethodPost, "/api/v1/admin/imports",
		"{"+file+`,"mapping":{"columns":{"givenName":"Vorname","familyName":"Nachname"}}}`, office))
	if errs, _ := bad["errors"].([]any); len(errs) != 1 || errs[0].(map[string]any)["location"] != "body.mapping.columns.memberNo" {
		t.Errorf("422 %v", bad)
	}

	bad = decode(t, a.expect(http.StatusUnprocessableEntity, http.MethodPost, "/api/v1/admin/imports/inspect",
		`{"fileName":"empty.csv","content":""}`, office))
	if errs, _ := bad["errors"].([]any); len(errs) != 1 || errs[0].(map[string]any)["location"] != "body.content" {
		t.Errorf("422 %v", bad)
	}

	member := a.login("clara.amberly@example.org")
	a.expect(http.StatusForbidden, http.MethodGet, "/api/v1/admin/imports", "", member)
}

// TestAdminDuplicates lists the demo's duplicates, previews a merge and
// does it, over HTTP.
func TestAdminDuplicates(t *testing.T) {
	t.Parallel()

	a := newAuthApp(t, app.Deps{Demo: true})
	office := a.login("olga.ledgerby@example.net")

	pairs := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/admin/duplicates", "", office))["items"].([]any)
	if len(pairs) != 2 {
		t.Fatalf("pairs %v", pairs)
	}

	lena, _ := pairs[0].(map[string]any)
	if lena["strength"] != "strong" || lena["a"].(map[string]any)["givenName"] != "Lena" {
		t.Fatalf("first pair %v", lena)
	}

	keep, _ := lena["a"].(map[string]any)["id"].(string)
	drop, _ := lena["b"].(map[string]any)["id"].(string)
	body := `{"survivorId":"` + keep + `","mergedId":"` + drop + `","take":{"birthDate":true}`

	preview := decode(t, a.expect(http.StatusOK, http.MethodPost, "/api/v1/admin/persons/merge", body+"}", office))
	if preview["applied"] != false || preview["survivor"].(map[string]any)["birthDate"] != "1994-08-17" {
		t.Errorf("preview %v", preview)
	}

	done := decode(t, a.expect(http.StatusOK, http.MethodPost, "/api/v1/admin/persons/merge", body+`,"apply":true}`, office))
	if done["applied"] != true {
		t.Errorf("merge %v", done)
	}

	detail := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/admin/persons/"+keep, "", office))
	if merged, _ := detail["merged"].([]any); len(merged) != 1 || detail["memberNo"] != "M-1042" ||
		detail["notes"] != "Registered at the front desk." {
		t.Errorf("survivor %v", detail)
	}

	tomb := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/admin/persons/"+drop, "", office))
	if into, _ := tomb["mergedInto"].(map[string]any); into["id"] != keep {
		t.Errorf("tombstone %v", tomb)
	}

	// The tombstone cannot be merged or changed; the pair is gone.
	a.expect(http.StatusNotFound, http.MethodPost, "/api/v1/admin/persons/merge", body+`,"apply":true}`, office)

	pairs = decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/admin/duplicates", "", office))["items"].([]any)
	if len(pairs) != 1 {
		t.Errorf("pairs after the merge %v", pairs)
	}

	// The merge is in the audit log.
	admin := a.login("arno.keystone@example.net")
	events := decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/admin/audit", "", admin))["items"].([]any)

	if e := events[0].(map[string]any); e["action"] != "person-merged" || e["targetPerson"].(map[string]any)["id"] != keep ||
		e["details"].(map[string]any)["mergedPersonId"] != drop {
		t.Errorf("audit %v", events[0])
	}

	// Two persons with accounts are refused.
	clara, tom := a.personID("clara amberly", office), a.personID("tom amberly", office)
	refused := decode(t, a.expect(http.StatusConflict, http.MethodPost, "/api/v1/admin/persons/merge",
		`{"survivorId":"`+clara+`","mergedId":"`+tom+`","take":{},"apply":true}`, office))
	if refused["reason"] != "two-accounts" {
		t.Errorf("409 %v", refused)
	}

	// The weak pair is dismissed.
	jonas, _ := pairs[0].(map[string]any)
	a.expect(http.StatusNoContent, http.MethodPost, "/api/v1/admin/duplicates/dismissals",
		`{"personA":"`+jonas["a"].(map[string]any)["id"].(string)+`","personB":"`+jonas["b"].(map[string]any)["id"].(string)+`"}`, office)

	pairs = decode(t, a.expect(http.StatusOK, http.MethodGet, "/api/v1/admin/duplicates", "", office))["items"].([]any)
	if len(pairs) != 0 {
		t.Errorf("pairs after the dismissal %v", pairs)
	}
}

func sameJSON(a, b any) bool {
	x, errA := json.Marshal(a)
	y, errB := json.Marshal(b)

	return errA == nil && errB == nil && string(x) == string(y)
}
