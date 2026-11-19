package conformance

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"os"
	"slices"
	"strings"
	"testing"
)

func inspectImport(t *testing.T, b *backend) {
	t.Helper()

	office, member := browser(t), browser(t)
	login(t, b, office, "olga.ledgerby@example.net", demoPassword)
	login(t, b, member, demoMember, demoPassword)
	const path = "/api/v1/admin/imports/inspect"
	payload := map[string]string{
		"fileName": "fixture-members.csv",
		"content": base64.StdEncoding.EncodeToString([]byte("Mitgliedsnummer\tVorname\tNachname\tE-Mail Elternteil 1\r\n" +
			"A1\tK\xe4the\tMuster\tguardian@example.invalid\r\n")),
	}
	in := request{method: http.MethodPost, path: path, body: jsonBody(t, payload)}
	b.call(t, browser(t), in, http.StatusUnauthorized)
	b.call(t, member, in, http.StatusForbidden)
	type inspection struct {
		Encoding, Delimiter string
		Headers             []string
		Rows                int
		Sample              [][]string
		Suggested           struct{ Columns map[string]string }
	}
	got := read[inspection](t, b.call(t, office, in, http.StatusOK))
	if got.Encoding != "windows-1252" || got.Delimiter != "tab" || got.Rows != 1 ||
		!slices.Equal(got.Headers, []string{"Mitgliedsnummer", "Vorname", "Nachname", "E-Mail Elternteil 1"}) ||
		len(got.Sample) != 1 || !slices.Equal(got.Sample[0], []string{"A1", "Käthe", "Muster", "guardian@example.invalid"}) ||
		got.Suggested.Columns["guardian1Email"] != "E-Mail Elternteil 1" {
		t.Fatalf("unexpected import inspection: %+v", got)
	}

	const bodyLimit = 1_463_640
	atLimit := in
	atLimit.body += strings.Repeat(" ", bodyLimit-len(atLimit.body))
	b.call(t, office, atLimit, http.StatusRequestEntityTooLarge)

	payload["content"] += "\n"
	wrapped := in
	wrapped.body = jsonBody(t, payload)
	b.call(t, office, wrapped, http.StatusUnprocessableEntity)

	payload["content"] = base64.StdEncoding.EncodeToString([]byte("Name;name\nAda;Bea\n"))
	in.body = jsonBody(t, payload)
	invalid := b.call(t, office, in, http.StatusUnprocessableEntity)
	if !containsJSON(invalid.body, `"location":"body.content"`) {
		t.Fatalf("duplicate headers were not reported on body.content: %s", invalid.body)
	}
}

func importMembers(t *testing.T, b *backend) {
	t.Helper()

	office := browser(t)
	login(t, b, office, "olga.ledgerby@example.net", demoPassword)
	csv, err := os.ReadFile("../service/servicetest/testdata/members-sample.csv")
	if err != nil {
		t.Fatal(err)
	}

	const fileName = "members-sample.csv"
	const path = "/api/v1/admin/imports"
	payload := map[string]any{"fileName": fileName, "content": base64.StdEncoding.EncodeToString(csv)}
	in := request{method: http.MethodPost, path: path + "/inspect", body: jsonBody(t, payload)}
	inspection := read[struct {
		Encoding, Delimiter string
		Suggested           struct{ Columns map[string]string }
	}](t, b.call(t, office, in, http.StatusOK))
	if inspection.Encoding != "utf-8" || inspection.Delimiter != "semicolon" ||
		inspection.Suggested.Columns["memberNo"] != "Mitgliedsnummer" {
		t.Fatalf("unexpected CSV inspection: %+v", inspection)
	}

	payload["mapping"] = map[string]any{"columns": inspection.Suggested.Columns, "notes": []string{"Kurs"}}
	in.path, in.body = path, jsonBody(t, payload)
	preview := b.call(t, office, in, http.StatusOK)
	type counts struct{ Rows, Created, Updated, Unchanged, Failed, GuardiansCreated, GuardiansUpdated, LinksAdded int }
	type run struct {
		Applied bool
		RunID   string
		Counts  counts
	}
	want := counts{Rows: 6, Created: 4, Updated: 1, Failed: 1, GuardiansCreated: 2, LinksAdded: 5}
	if before := read[run](t, preview); before.Applied || before.RunID != "" || before.Counts != want {
		t.Fatalf("unexpected import preview: %+v", before)
	}

	search := request{method: http.MethodPost, path: "/api/v1/admin/persons/search", body: `{"query":"paula amberly"}`}
	if out := b.call(t, office, search, http.StatusOK); !containsJSON(out.body, `"items":[]`) {
		t.Fatal("preview added a person")
	}
	if out := get(t, b, office, path); !containsJSON(out.body, `"items":[]`) {
		t.Fatal("preview added an import history entry")
	}

	payload["apply"] = true
	in.body = jsonBody(t, payload)
	applied := read[run](t, b.call(t, office, in, http.StatusOK))
	if !applied.Applied || applied.RunID == "" || applied.Counts != want {
		t.Fatalf("unexpected applied import: %+v", applied)
	}
	people := read[struct {
		Items []struct{ GivenName, FamilyName string }
	}](t, b.call(t, office, search, http.StatusOK))
	if len(people.Items) != 1 || people.Items[0].GivenName != "Paula" || people.Items[0].FamilyName != "Amberly" {
		t.Fatalf("imported member search: %+v", people)
	}

	history := get(t, b, office, path)
	if !bytes.Contains(history.body, []byte(applied.RunID)) || !bytes.Contains(history.body, []byte(fileName)) ||
		!containsJSON(history.body, `"field":"birthDate"`) {
		t.Fatal("history is missing the import or the invalid birth date")
	}
}

func jsonBody(t *testing.T, value any) string {
	t.Helper()

	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}

	return string(data)
}

func detectDuplicates(t *testing.T, b *backend) {
	t.Helper()

	office, member := browser(t), browser(t)
	login(t, b, office, "olga.ledgerby@example.net", demoPassword)
	login(t, b, member, demoMember, demoPassword)
	const path = "/api/v1/admin/duplicates"
	list := request{method: http.MethodGet, path: path}
	b.call(t, browser(t), list, http.StatusUnauthorized)
	b.call(t, member, list, http.StatusForbidden)

	type candidate struct{ ID, GivenName string }
	type pair struct {
		A, B     candidate
		Strength string
		Reasons  []string
	}
	type response struct{ Items []pair }
	items := read[response](t, b.call(t, office, list, http.StatusOK)).Items
	if len(items) != 2 || items[0].A.GivenName != "Lena" || items[0].Strength != "strong" ||
		!slices.Equal(items[0].Reasons, []string{"email", "name"}) || items[1].A.GivenName != "Jonas" ||
		items[1].Strength != "weak" || !slices.Equal(items[1].Reasons, []string{"name"}) {
		t.Fatalf("unexpected duplicate candidates: %+v", items)
	}

	dismiss := func(client *http.Client, a, bID string, status int) {
		t.Helper()
		b.call(t, client, request{
			method: http.MethodPost, path: path + "/dismissals",
			body: jsonBody(t, map[string]string{"personA": a, "personB": bID}),
		}, status)
	}
	a, other := items[0].A.ID, items[0].B.ID
	dismiss(browser(t), a, other, http.StatusUnauthorized)
	dismiss(member, a, other, http.StatusForbidden)
	dismiss(office, a, a, http.StatusUnprocessableEntity)
	b.call(t, office, request{
		method: http.MethodPost, path: path + "/dismissals",
		body: jsonBody(t, struct {
			Canonical bool   `json:"personA"`
			Alias     string `json:"PERSONA"`
			Other     string `json:"personB"`
		}{false, a, other}),
	}, http.StatusUnprocessableEntity)
	dismiss(office, a, "f20daa9e-941c-44bc-8c4e-c7435c06a5b7", http.StatusNotFound)
	dismiss(office, other, a, http.StatusNoContent)
	dismiss(office, a, other, http.StatusNoContent)
	remaining := read[response](t, b.call(t, office, list, http.StatusOK)).Items
	if len(remaining) != 1 || remaining[0].A.ID != items[1].A.ID || remaining[0].B.ID != items[1].B.ID {
		t.Fatalf("displayed pairs after dismissal: %+v", remaining)
	}
}

func mergeDuplicates(t *testing.T, b *backend) {
	t.Helper()

	office := browser(t)
	login(t, b, office, "olga.ledgerby@example.net", demoPassword)
	type matchPerson struct{ ID, MemberNo, GivenName string }
	type pair struct {
		A, B    matchPerson
		Reasons []string
	}
	const path = "/api/v1/admin/duplicates"
	items := read[struct{ Items []pair }](t, get(t, b, office, path)).Items
	pos := slices.IndexFunc(items, func(p pair) bool { return p.A.GivenName == "Lena" })
	if len(items) != 2 || pos < 0 || !slices.Contains(items[pos].Reasons, "email") {
		t.Fatalf("unexpected duplicate matches: %+v", items)
	}

	keep, drop := items[pos].A, items[pos].B
	if drop.MemberNo == "M-1042" {
		keep, drop = drop, keep
	}
	if keep.MemberNo != "M-1042" {
		t.Fatal("seeded duplicates have no survivor with member number M-1042")
	}

	payload := map[string]any{"survivorId": keep.ID, "mergedId": drop.ID, "take": map[string]bool{"birthDate": true}}
	in := request{method: http.MethodPost, path: "/api/v1/admin/persons/merge", body: jsonBody(t, payload)}
	type result struct {
		Applied  bool
		Survivor struct{ ID, BirthDate string }
	}
	preview := read[result](t, b.call(t, office, in, http.StatusOK))
	if preview.Applied || preview.Survivor.ID != keep.ID || preview.Survivor.BirthDate != "1994-08-17" {
		t.Fatalf("unexpected merge preview: %+v", preview)
	}

	person := request{
		method: http.MethodGet, path: "/api/v1/admin/persons/{personId}",
		params: map[string]string{"personId": drop.ID},
	}
	if before := b.call(t, office, person, http.StatusOK); containsJSON(before.body, `"mergedInto"`) {
		t.Fatal("preview changed the dropped person")
	}

	payload["apply"] = true
	in.body = jsonBody(t, payload)
	if applied := read[result](t, b.call(t, office, in, http.StatusOK)); !applied.Applied || applied.Survivor.ID != keep.ID {
		t.Fatalf("unexpected applied merge: %+v", applied)
	}
	type personLinks struct {
		MergedInto *struct{ ID string }
		Merged     []struct{ ID string }
	}
	dropped := read[personLinks](t, b.call(t, office, person, http.StatusOK))
	person.params["personId"] = keep.ID
	surviving := read[personLinks](t, b.call(t, office, person, http.StatusOK))
	if dropped.MergedInto == nil || dropped.MergedInto.ID != keep.ID || len(surviving.Merged) != 1 ||
		surviving.Merged[0].ID != drop.ID {
		t.Fatal("the applied merge is missing a forward or reverse link")
	}

	b.call(t, office, in, http.StatusNotFound)
	items = read[struct{ Items []pair }](t, get(t, b, office, path)).Items
	if len(items) != 1 || items[0].A.GivenName != "Jonas" {
		t.Fatalf("after merge, duplicate matches: %+v", items)
	}

	b.call(t, office, request{
		method: http.MethodPost, path: path + "/dismissals",
		body: jsonBody(t, map[string]string{"personA": items[0].A.ID, "personB": items[0].B.ID}),
	}, http.StatusNoContent)
	if out := get(t, b, office, path); !containsJSON(out.body, `"items":[]`) {
		t.Error("dismissed pair still appears in duplicate results")
	}
}
