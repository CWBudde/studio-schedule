# Glossary

Domain terms used in code and UI, with the German UI term and the name used in the previous system
where it differs.

| Term (code)        | English UI          | German UI             | Notes / previous name                                                   |
| ------------------ | ------------------- | --------------------- | ----------------------------------------------------------------------- |
| `Lesson`           | Lesson              | Stunde                | A concrete, dated occurrence (formerly "Unterrichtsstunde")             |
| `Class`            | Class               | Kurs                  | A recurring weekly template that generates lessons (formerly "Klasse")  |
| `ClassMember`      | Class member        | Kursmitglied          | Fixed enrolment in a class, "since" a date; auto-booked from Phase 4    |
| `Term`             | Term                | Zeitraum / Saison     | Date range for a timetable, e.g. a summer timetable ("Sommertanzplan")  |
| `CourseType`       | Course type / style | Stilrichtung          | Catalogue entry with description; name unique (ADR 0007)                |
| `Level`            | Level               | Niveau                | e.g. beginner, intermediate, advanced, professional, open               |
| `Location`, `Room` | Location, room      | Standort, Raum        | Rooms have a capacity; names unique (rooms per location; ADR 0007)      |
| `Staff`            | Instructor          | Lehrkraft             | Kind `instructor` or `accompanist` ("Korrepetition"); may be inactive   |
| `Person`           | Person              | Person                | Any human: member, child, guest                                         |
| `Guardian`         | Parent / guardian   | Erziehungsberechtigte | Can manage and book for dependents ("Kinder")                           |
| Dependent          | Child / dependent   | Kind                  | Person linked to a guardian, usually without an account (ADR 0008)      |
| Managed dependent  | Managed dependent   | Eigenständig          | Dependent with an account, staff link or own dependents; read-only      |
| `Account`          | Account             | Konto                 | Login of a person, with a role                                          |
| Invitation         | Invitation          | Einladung             | Mail with a password link for an account the office creates             |
| `Booking`          | Booking             | Buchung               | Formerly "Teilnahme"; actions "Eintragen" (book) / "Austragen" (cancel) |
| `BookingEvent`     | Booking history     | Buchungsverlauf       | Audit trail of status changes, with time and acting account             |
| Guest booking      | Guest               | Gast / extern         | Booking without an account (name + email)                               |
| Trial              | Trial lesson        | Probestunde           |                                                                         |
| `WaitlistEntry`    | Waiting list        | Warteliste            | Timed invites instead of "notify all"                                   |
| `Pass`             | Pass                | Karte (z. B. 10er)    | Prepaid visits, formerly "Credits"                                      |
| `Receipt`          | Receipt             | Quittung / Beleg      | Gapless numbering per year; corrections via cancellation receipt        |
| Release window     | Release             | Freischaltung         | Next weeks become bookable at a configured weekday/time                 |
| Visible days       | Visible days        | Sichtbare Tage        | Per-account override of how far ahead lessons are visible               |
| `Message`          | Announcement        | Mitteilung            | In-app and/or email                                                     |
| Calendar feed      | Calendar feed       | Kalender-Abo          | iCal URL with a secret token                                            |
| In use             | In use              | In Verwendung         | Still referenced, so it cannot be deleted (409 `in-use`; ADR 0007)      |
| Retire             | Retire / deactivate | Stilllegen            | Hide from future use instead of deleting what is referenced (ADR 0007)  |
| Duplicate          | Already taken       | Bereits vergeben      | Name taken or person already a member (409 `duplicate`; ADR 0007)       |
| Slot date          | (not shown)         | (nicht sichtbar)      | School date a generated lesson belongs to (`lessons.slot_date`)         |
| Follow             | (not shown)         | (nicht sichtbar)      | An unedited future lesson follows class and term edits (ADR 0007)       |

Roles: `guest` (not logged in) · `member` (Mitglied) · `instructor` (Lehrkraft) · `office` (Büro) ·
`admin` (Administration).
