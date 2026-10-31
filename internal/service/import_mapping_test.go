package service_test

import (
	"maps"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/service"
)

// TestSuggestMappingGuardianHeaders: a guardian's header needs the
// guardian's number (unless the word implies it, as mother and father do)
// and names one of the guardian's fields: names, email or phone. Other
// headers stay unmapped, and of two headers for one field the first wins.
func TestSuggestMappingGuardianHeaders(t *testing.T) {
	t.Parallel()

	got := service.SuggestMapping([]string{
		"Guardian email",         // no number: whose?
		"Parent 1 shoe size",     // not a field
		"Parent 1 date of birth", // a field, but not a guardian's
		"Erziehungsberechtigte 2 Telefon",
		"Mother first name",
		"Vater Nachname",
		"Elternteil 1 E-Mail",
		"Guardian 1 email", // the field is taken
		"Shoe size",
	})

	want := map[service.ImportField]string{
		service.FieldGuardian2Phone:      "Erziehungsberechtigte 2 Telefon",
		service.FieldGuardian1GivenName:  "Mother first name",
		service.FieldGuardian2FamilyName: "Vater Nachname",
		service.FieldGuardian1Email:      "Elternteil 1 E-Mail",
	}
	if !maps.Equal(got.Columns, want) {
		t.Errorf("suggested %v, want %v", got.Columns, want)
	}
}

// TestInspectTabs: a tab-separated file is read and named so.
func TestInspectTabs(t *testing.T) {
	t.Parallel()

	svc := service.NewImportService(nil, nil, nil) // Inspect touches no store

	insp, err := svc.Inspect(service.ImportFile{Name: "members.tsv", Data: []byte(
		"Member No\tFirst name\tLast name\nT-1\tTeo\tExample\n")})
	if err != nil {
		t.Fatal(err)
	}

	if insp.Delimiter != "tab" || insp.Rows != 1 || insp.Sample[0][1] != "Teo" ||
		insp.Suggested.Columns[service.FieldFamilyName] != "Last name" {
		t.Errorf("inspection %+v", insp)
	}
}
