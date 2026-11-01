package main

import "github.com/cwbudde/studio-schedule/internal/domain"

// The words the commands' text output shares.

// deletedPersonName is the name of an anonymized person, as the web shows
// it (web/src/lib/personName.ts).
const deletedPersonName = "Deleted person"

// shownName is a person's name as the web shows it (its shownName): the
// name, or "Deleted person" for an anonymized person.
func shownName(name string, anonymized bool) string {
	if anonymized {
		return deletedPersonName
	}

	return name
}

// personName is the shownName of a person: "Given Family".
func personName(p domain.Person) string {
	return shownName(p.GivenName+" "+p.FamilyName, p.Anonymized())
}

// yesNo is a flag in a table cell.
func yesNo(b bool) string {
	if b {
		return "yes"
	}

	return "no"
}
