package seed

import (
	_ "embed"

	"github.com/cwbudde/studio-schedule/internal/domain"
)

// werkstattBackground is the Werkstatt school's background image, a CC0
// photo (assets/README.md).
//
//go:embed assets/werkstatt-background.jpg
var werkstattBackground string

// werkstatt returns the tables of the werkstatt school (SchoolWerkstatt).
// For now (until task 5.1) they are the programme school's tables with its
// own settings, background and places: its own identity (name, imprint,
// receipt issuer and prefix), theme, accent and language, and the
// programme school's rules. Each call returns fresh tables and a fresh copy
// of the background, since the store owns what it is given.
func werkstatt() schoolSpec {
	s := programme()

	// A fictional name; the accent passes the 4.5:1 contrast check, and its
	// dark variant is derived from it (domain.DarkAccent).
	s.settings.SchoolName = "Tanzhaus am Lindenhof"
	s.settings.Theme = domain.ThemeWerkstatt
	s.settings.AccentColor = "#9c215b"
	s.settings.DefaultLocale = "de"
	s.settings.ReceiptPrefix = "TL"
	// A fictional imprint and issuer, like the programme school's: the
	// street, the town, the tax number and the bank exist nowhere.
	s.settings.LegalNotice = "Tanzhaus am Lindenhof · Lindenhofweg 12 · 10999 Beispielstadt\n" +
		"Eine erfundene Schule: Alle Namen, Adressen und Nummern in dieser Demo sind ausgedacht."
	s.settings.Receipts.Issuer = domain.ReceiptIssuer{
		Name: "Tanzhaus am Lindenhof", Address: "Lindenhofweg 12\n10999 Beispielstadt",
		TaxID: "99/999/99999", Email: "buero@tanzhaus-lindenhof.example", Phone: "+49 000 0000000",
	}
	s.settings.Receipts.Footer = "Bankverbindung: Beispielbank · IBAN XX00 0000 0000 0000 0000 00"

	s.background = domain.Background{ContentType: domain.BackgroundJPEG, Data: []byte(werkstattBackground)}

	// One house with two halls, "Oben" and "Unten", and a training room. The
	// halls hold the classes of the programme's two largest rooms; the
	// training room those of the three small ones, whose classes and
	// workshops never meet in time (TestWerkstattPlaces). The address is
	// fictional.
	s.locations = []locationSpec{
		{"Tanzhaus am Lindenhof", "Lindenhofweg 12, 10999 Beispielstadt", []roomSpec{
			{name: "Oben", capacity: 20, holds: []string{"Studio A"}},
			{name: "Unten", capacity: 24, holds: []string{"Hall 1"}},
			{name: "Trainingsraum", capacity: 14, holds: []string{"Studio B", "Hall 2", "Studio C"}},
		}},
	}

	return s
}
