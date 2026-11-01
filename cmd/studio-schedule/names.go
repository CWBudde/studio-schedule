package main

import (
	"strconv"
	"strings"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/service"
)

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

// audienceText is who a class is for, as the text output shows it (ADR
// 0021), its parts joined by " · ": the levels ("Beginner–Intermediate", one
// level's name, or "all levels"), the stage ("stage 2") and the ages in years
// ("4–6 yrs", "from 3½ yrs", "up to 6 yrs"). Empty when the class states
// nothing.
func audienceText(a service.Audience) string {
	var parts []string

	switch {
	case a.AllLevels:
		parts = append(parts, "all levels")
	case a.Low.ID.IsZero():
	case a.Low.ID == a.High.ID:
		parts = append(parts, a.Low.Name)
	default:
		parts = append(parts, a.Low.Name+"–"+a.High.Name)
	}

	if a.Stage != 0 {
		parts = append(parts, "stage "+strconv.Itoa(a.Stage))
	}

	switch {
	case a.AgeFrom != 0 && a.AgeTo != 0:
		parts = append(parts, years(a.AgeFrom)+"–"+years(a.AgeTo)+" yrs")
	case a.AgeFrom != 0:
		parts = append(parts, "from "+years(a.AgeFrom)+" yrs")
	case a.AgeTo != 0:
		parts = append(parts, "up to "+years(a.AgeTo)+" yrs")
	}

	return strings.Join(parts, " · ")
}

// years is an age in whole and half years, rounded down to the half: 40
// months are "3", 42 and 45 "3½", 48 "4".
func years(m domain.AgeMonths) string {
	whole, half := int(m)/12, int(m)%12 >= 6

	switch {
	case !half:
		return strconv.Itoa(whole)
	case whole == 0:
		return "½"
	default:
		return strconv.Itoa(whole) + "½"
	}
}

// withAudience is name followed by the audience in parentheses, or name
// alone when the audience states nothing.
func withAudience(name string, a service.Audience) string {
	if s := audienceText(a); s != "" {
		return name + " (" + s + ")"
	}

	return name
}
