package service

import (
	"strings"

	"github.com/cwbudde/studio-schedule/internal/domain"
)

// feedLabels are the words of a calendar feed in one language: the feed
// is read in calendar apps, outside the web app's translations.
type feedLabels struct {
	bookings, teaching, schedule    string
	instructor, accompanist, room   string
	booked, canceled, canceledNote  string
	workshop, private, rental       string
	renter                          string
	instructorRole, accompanistRole string
	locationSchedule                string // with %s for the location
}

// feedLabelSets are the languages of feeds, by primary language subtag.
var feedLabelSets = map[string]feedLabels{
	"en": {
		bookings: "My lessons", teaching: "Teaching", schedule: "Schedule", locationSchedule: "Schedule (%s)",
		instructor: "Instructor", accompanist: "Accompanist", room: "Room",
		booked: "Booked", canceled: "Canceled", canceledNote: "This lesson is canceled.",
		workshop: "Workshop", private: "Private lesson", rental: "Room rental", renter: "Rented by",
		instructorRole: "instructor", accompanistRole: "accompanist",
	},
	"de": {
		bookings: "Meine Stunden", teaching: "Unterricht", schedule: "Stundenplan", locationSchedule: "Stundenplan (%s)",
		instructor: "Lehrkraft", accompanist: "Korrepetition", room: "Raum",
		booked: "Gebucht", canceled: "Abgesagt", canceledNote: "Diese Stunde fällt aus.",
		workshop: "Workshop", private: "Privatstunde", rental: "Raumvermietung", renter: "Gemietet von",
		instructorRole: "Lehrkraft", accompanistRole: "Korrepetition",
	},
}

// feedLabelsFor returns the labels of the language of the locale (a BCP 47
// tag such as "de-AT"), English for any other.
func feedLabelsFor(locale string) feedLabels {
	lang, _, _ := strings.Cut(strings.ToLower(locale), "-")
	lang, _, _ = strings.Cut(lang, "_")

	if l, ok := feedLabelSets[lang]; ok {
		return l
	}

	return feedLabelSets["en"]
}

// subject names the subject of a feed of scope; location is the name of a
// schedule feed's location, if any.
func (l feedLabels) subject(scope domain.FeedScope, location string) string {
	switch {
	case scope == domain.FeedBookings:
		return l.bookings
	case scope == domain.FeedTeaching:
		return l.teaching
	case location != "":
		return strings.Replace(l.locationSchedule, "%s", location, 1)
	default:
		return l.schedule
	}
}

// role names a part in a lesson.
func (l feedLabels) role(r TeachRole) string {
	if r == TeachAccompanist {
		return l.accompanistRole
	}

	return l.instructorRole
}

// kind names a lesson kind other than regular.
func (l feedLabels) kind(k domain.LessonKind) string {
	switch k {
	case domain.LessonPrivate:
		return l.private
	case domain.LessonRental:
		return l.rental
	default:
		return l.workshop
	}
}
