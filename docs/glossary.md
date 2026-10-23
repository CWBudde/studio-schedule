# Glossary

Domain terms used in code and UI, with the German UI term and the name used in the previous system
where it differs.

| Term (code)        | English UI          | German UI             | Notes / previous name                                                   |
| ------------------ | ------------------- | --------------------- | ----------------------------------------------------------------------- |
| `Lesson`           | Lesson              | Stunde                | A concrete, dated occurrence (formerly "Unterrichtsstunde")             |
| `Class`            | Class               | Kurs                  | A recurring weekly template that generates lessons (formerly "Klasse")  |
| `ClassMember`      | Class member        | Kursmitglied          | Fixed enrolment in a class; auto-booked into its lessons                |
| `Term`             | Term                | Zeitraum / Saison     | Date range for a timetable, e.g. a summer timetable ("Sommertanzplan")  |
| `CourseType`       | Course type / style | Stilrichtung          | Catalogue entry with description (formerly "Unterrichtsbeschreibung")   |
| `Level`            | Level               | Niveau                | e.g. beginner, intermediate, advanced, professional, open               |
| `Location`, `Room` | Location, room      | Standort, Raum        | Rooms have a capacity                                                   |
| `Staff`            | Instructor          | Lehrkraft             | Kind `instructor` or `accompanist` (musician, "Korrepetition")          |
| `Person`           | Person              | Person                | Any human: member, child, guest                                         |
| `Guardian`         | Parent / guardian   | Erziehungsberechtigte | Can manage and book for dependents ("Kinder")                           |
| `Account`          | Account             | Konto                 | Login of a person, with a role                                          |
| `Booking`          | Booking             | Buchung               | Formerly "Teilnahme"; actions "Eintragen" (book) / "Austragen" (cancel) |
| Guest booking      | Guest               | Gast / extern         | Booking without an account (name + email)                               |
| Trial              | Trial lesson        | Probestunde           |                                                                         |
| `WaitlistEntry`    | Waiting list        | Warteliste            | Timed invites instead of "notify all"                                   |
| `Pass`             | Pass                | Karte (z. B. 10er)    | Prepaid visits, formerly "Credits"                                      |
| `Receipt`          | Receipt             | Quittung / Beleg      | Gapless numbering per year; corrections via cancellation receipt        |
| Release window     | Release             | Freischaltung         | Next weeks become bookable at a configured weekday/time                 |
| Visible days       | Visible days        | Sichtbare Tage        | Per-account override of how far ahead lessons are visible               |
| `Message`          | Announcement        | Mitteilung            | In-app and/or email                                                     |
| Calendar feed      | Calendar feed       | Kalender-Abo          | iCal URL with a secret token                                            |

Roles: `guest` (not logged in) · `member` (Mitglied) · `instructor` (Lehrkraft) · `office` (Büro) ·
`admin` (Administration).
