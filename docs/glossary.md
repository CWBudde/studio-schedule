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
| Booking source     | (not shown)         | (nicht sichtbar)      | `self`, `member` (auto-enrolment) or `staff` (ADR 0009)                 |
| Late cancellation  | Late cancellation   | Späte Absage          | Canceled after the cancellation cutoff; allowed, flagged (ADR 0009)     |
| Auto-enrolment     | Auto-enrolment      | Automatische Buchung  | Class members are booked into their class's lessons (ADR 0009)          |
| Release a date     | Release this date   | Termin freigeben      | A class member cancels one lesson; the place goes to others (0009)      |
| Pause (membership) | Paused until        | Pausiert bis          | Class member canceled the following weeks up to a date (0009)           |
| Guest booking      | Guest               | Gast / extern         | Booking without a person record: a name, optional email (staff only)    |
| Trial              | Trial lesson        | Probestunde           | Flag on a booking, with an optional note for the instructor             |
| Participants       | Participants        | Teilnehmende          | A lesson's bookings and waiting list, for its staff (ADR 0009)          |
| Attendance         | Attendance          | Anwesenheit           | `attended` / `no_show`; instructors until the day's end, then office    |
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
| Detached           | Edited individually | Einzeln bearbeitet    | Lesson of a class the office edited; follows the class no more (0007)   |
| One-off lesson     | One-off lesson      | Einzeltermin          | Lesson without a class, e.g. a workshop; created by the office (0007)   |
| Room taken         | Room taken          | Raum belegt           | Another lesson uses the room then (409 `room-taken`; ADR 0007)          |

Roles: `guest` (not logged in) · `member` (Mitglied) · `instructor` (Lehrkraft) · `office` (Büro) ·
`admin` (Administration).
