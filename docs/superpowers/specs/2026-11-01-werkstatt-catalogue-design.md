# Werkstatt catalogue: room order, class audiences and a school of its own

Date: 2026-11-01. Follows [the themes and layouts spec](2026-10-30-themes-and-layouts-design.md)
and ADR 0018.

## Goal

The Werkstatt demo ("Tanzhaus am Lindenhof") should be comparable with the previous app it is
modelled on: the same kinds of courses, the same way of stating levels, a weekly plan of the same
density and shape, and the rooms in the order that school shows them. Today both demo schools
share every person, class and lesson, the Werkstatt catalogue is the programme school's English
one, levels are one of five fixed values, and room columns sort alphabetically
(Oben, Trainingsraum, Unten).

Three changes get there:

1. **Room order.** A room has a position; lists and the Werkstatt room columns follow it.
2. **Class audience.** A class states a level range, a stage and an age range instead of one
   level.
3. **A school of its own.** The Werkstatt demo school gets its own people, staff, accounts,
   catalogue, weekly plan, workshops and passes, and a fourth room, "Online".

## Global constraints

- **No real data.** The Werkstatt catalogue below was derived from the previous app's weekly plan
  by structure only: course kinds, levels, durations, the rhythm of the week. Days, rooms and
  times were moved, courses renamed where the previous app used its own brand names, and nothing
  else was taken: no people (teachers, accompanists, participants), no prices, no descriptions,
  no places outside the house. Every person, name, email, description and pass in the Werkstatt
  data is invented. Emails use the reserved example domains only.
- Every user-facing string lives in `web/src/locales/{en,de}`.
- "Unimplemented means absent": no field or control without its behaviour.
- Both stores, every new repo method with a `storetest` case.
- API changes: explicit `OperationID`s, camelCase JSON, `just gen-api`, generated types in the web.
- Commit messages and PR titles: Conventional Commits in English.

## 1. Room order

**Domain.** `domain.Room` gets `Position int` (1-based within its location). `Validate` requires
`Position > 0`.

**Store.** Migration `00028_room_position.sql`: `rooms.position integer NOT NULL CHECK
(position > 0)`, backfilled per location in the current name order, so nothing visible changes
for an existing school. No unique constraint (a reorder writes all positions in one transaction).
`RoomRepo` lists rooms ordered by location, then position, then name, then id. A new
`RoomRepo.SetOrder(ctx, locationID, []RoomID)` writes positions 1..n; it fails with
`domain.ErrValidation` unless the IDs are exactly that location's rooms.

**Service and API.**

- `createRoom` appends: position = the location's highest + 1. The create and update bodies
  don't take a position.
- New operation `setRoomOrder` (office): `PUT /api/v1/admin/locations/{id}/room-order` with
  `{"roomIds": [...]}` → 204. A list that misses a room, repeats one or names another location's
  room is a 422 on `body.roomIds`.
- Rooms in the API (`listRooms`, the schedule's `room`, the admin lists) carry `position`.

**Web.**

- The office's locations page lists a location's rooms in position order, each with "move up" and
  "move down" buttons (accessible names "Move <room> up/down"; the first has no up, the last no
  down). Each move sends the whole new order.
- `roomColumns` (Werkstatt) sorts by location name, then room position, then room name, then id.
- Room pickers and the schedule's location/room filter options follow the API order.

## 2. Class audience

A class says who it is for with three optional parts, and the cards show them.

**Level range.** The scale gains a first step, **entry**:

| value | en | de | Werkstatt code |
|---|---|---|---|
| entry | Entry | Einsteiger | E |
| beginner | Beginner | Anfänger | A |
| intermediate | Intermediate | Mittelstufe | M |
| advanced | Advanced | Fortgeschritten | F |
| professional | Professional | Profi | P |
| open | All levels | Alle Level | alle Level |

A class has a range `From`–`To` on the ordered scale (equal for one level), or "open" (for
everyone), or no level at all (common for children's classes, which state a stage and an age
instead).

**Stage.** An optional whole number from 1 to 9, the step within a course's progression
("Stufe 2"; children's ballet runs Stufe 1–3, youth ballet 1–4).

**Age.** An optional range in months, `AgeFrom`, `AgeTo` (0 = not stated; months so that
"ab 3½ J." is exact). The web shows years, halves as "½": "ab 3 J.", "4–6 J.", "ab 12 J."; en
"from 3 yrs", "4–6 yrs".

**Domain.** In `internal/domain/timetable.go`, `Class.Level` becomes
`Levels LevelRange{From, To Level}` (zero = no level), `Stage int` and
`AgeFrom, AgeTo AgeMonths`. `Validate`:

- `From` and `To` are both empty or both valid.
- `open` only as `From == To == open`.
- `From <= To` on the scale.
- `Stage` is 0 or 1–9.
- Ages are 0 or 1–1200, and `AgeTo` is 0 or ≥ `AgeFrom`.

Errors are field errors (`levelFrom`, `levelTo`, `stage`, `ageFromMonths`, `ageToMonths`).

**Store.** Migration `00029_class_audience.sql` replaces `classes.level` with `level_from`,
`level_to` (nullable text with the scale CHECK), `stage smallint`, `age_from_months smallint` and
`age_to_months smallint` (nullable, CHECKs as above). Existing rows get `level_from = level_to =`
their level.

**Schedule filter.** `level=X` keeps lessons of classes whose range contains X, and open
classes. Classes without a level and one-off lessons don't match a level filter, as one-off
lessons don't today. The query parameter's enum gains `entry`.

**API.**

- Class bodies and responses, the schedule lesson and the statistics row replace `level` with
  `levelFrom` and `levelTo`, plus `stage`, `ageFromMonths` and `ageToMonths`, each omitted when
  absent.
- One-off lessons have none of them.

**Web.**

- One formatter, `formatAudience` in `web/src/lib/audience.ts`, renders the parts in the user's
  language:
  - the long form for the programme theme, e.g. "Beginner–Intermediate · Stage 2 · 4–6 yrs";
  - the short form for Werkstatt, e.g. "A/M · Stufe 2 · 4–6 J.".

  The cards, the lesson sheet, the timetable week list and the statistics table use it.
- The admin class sheet edits the parts:
  - two level selects, "from" and "to" ("to" defaults to "from"), plus an "All levels" choice;
  - a stage number;
  - two age fields in years, half years allowed.

  It validates with zod against the same rules, and the server's 422s land on the fields.
- CSV import, if it reads a class level, maps one level to `From == To`.
- ADR 0019 "Class audience" records the model, the scale and the filter rule.

## 3. A school of its own

**Seed shape.**

- `internal/seed` builds each demo school from its own tables: households, staff, course types,
  locations and rooms, classes, workshops, pass types, announcements and logins.
- The programme school's tables are today's, and its dataset stays identical. A test compares it
  with the dataset from before the refactor: same IDs, same records.
- The Werkstatt tables are new, so `TestDemoSchoolSameData` gives way to per-school tests: every
  school's data is valid, references resolve, classes in one room never overlap, workshops never
  overlap a class in their room, and the logins work.
- ADR 0018 gets an amendment: the schools no longer share data.

**Werkstatt people.**

- They're German, fictional, and mirror the programme school's scenarios:
  - families with children and a managed teenager;
  - single adults;
  - instructors (one former) and accompanists;
  - one account per role for the demo logins.
- Emails are `vorname.nachname@…` on example domains.
- Class members (1–4 per class) are assigned so the history, the statistics and the
  participants pages have data in every room.

**Werkstatt rooms**, one location "Tanzhaus am Lindenhof":

| position | room | capacity |
|---|---|---|
| 1 | Oben | 24 |
| 2 | Unten | 18 |
| 3 | Online | 40 |
| 4 | Trainingsraum | 8 |

**Werkstatt course types** (each with a short, newly written German description):

Tanzzwerge, Tänzerische Früherziehung, Kinderballett Vorstufe, Kinderballett, Ballett Jugend,
Ballett Jugend & Spitze, Spitze, Ballett Erwachsene, Ballett 45+, Tanz für Einsteiger,
Tanzen 50+, Contemporary, Contemporary Kids, Hip-Hop & Contemporary Kids, Hip-Hop & Contemporary
Teens, Modern Jazz, Jazz Dance, Stepptanz, Irish Dance, Stretch & Strength, Qigong, Taiji,
Morning Workout.

**Werkstatt weekly plan**: 70 classes from Monday to Saturday, with Sunday free.

Each row gives the room, the time, the course type, the audience and the capacity. Audience
codes: E/A/M/F level ranges, "alle" = open, "St." = stage, ages in years. Instructors are
assigned from the Werkstatt staff so that no instructor teaches two classes at once.

*Montag*

| Raum | Zeit | Kurs | Zielgruppe | Plätze |
|---|---|---|---|---|
| Online | 10:15–11:15 | Qigong | alle | 40 |
| Oben | 14:55–15:45 | Tanzzwerge | ab 3 J. | 12 |
| Oben | 15:45–16:35 | Tänzerische Früherziehung | St. 2, 4–6 J. | 14 |
| Unten | 15:45–16:45 | Ballett Jugend | St. 2 | 18 |
| Oben | 16:45–18:00 | Ballett Jugend & Spitze | St. 1 | 18 |
| Unten | 16:45–17:45 | Contemporary Kids | A, 8–12 J. | 16 |
| Online | 17:45–18:45 | Taiji | alle | 40 |
| Unten | 17:45–19:15 | Irish Dance | F | 18 |
| Oben | 18:00–19:10 | Contemporary | A/M | 18 |
| Oben | 19:15–20:15 | Stretch & Strength | alle | 16 |
| Unten | 19:15–20:15 | Stepptanz | E/A | 16 |
| Oben | 20:15–21:45 | Ballett Erwachsene | M/F | 18 |
| Unten | 20:15–21:45 | Ballett 45+ | M | 16 |

*Dienstag*

| Raum | Zeit | Kurs | Zielgruppe | Plätze |
|---|---|---|---|---|
| Online | 08:45–09:15 | Morning Workout | alle | 40 |
| Unten | 10:30–11:45 | Contemporary | E/A | 18 |
| Unten | 12:00–13:15 | Ballett Erwachsene | A/M | 18 |
| Oben | 15:00–16:00 | Kinderballett Vorstufe | St. 1 | 14 |
| Unten | 15:00–16:00 | Kinderballett Vorstufe | St. 3 | 14 |
| Oben | 16:00–17:00 | Ballett Jugend | St. 1 | 18 |
| Unten | 16:00–17:00 | Ballett Jugend | St. 3 | 18 |
| Trainingsraum | 17:00–17:30 | Spitze | E, ab 12 J. | 8 |
| Oben | 17:30–18:45 | Tanz für Einsteiger | E | 18 |
| Unten | 17:30–18:30 | Stepptanz | E/A | 16 |
| Oben | 18:45–20:00 | Tanzen 50+ | alle | 18 |
| Unten | 18:45–20:00 | Ballett Jugend & Spitze | St. 3 | 18 |
| Oben | 20:00–21:30 | Contemporary | F | 18 |
| Unten | 20:00–21:30 | Tanz für Einsteiger | A | 18 |

*Mittwoch*

| Raum | Zeit | Kurs | Zielgruppe | Plätze |
|---|---|---|---|---|
| Online | 09:45–10:45 | Qigong | alle | 40 |
| Oben | 11:00–12:15 | Qigong | alle | 16 |
| Unten | 11:00–12:15 | Ballett Erwachsene | M | 18 |
| Oben | 14:25–15:25 | Kinderballett | St. 1 | 14 |
| Unten | 14:25–15:15 | Tänzerische Früherziehung | St. 1, 4–6 J. | 14 |
| Unten | 15:15–16:15 | Kinderballett Vorstufe | St. 2 | 14 |
| Oben | 15:25–16:25 | Ballett Jugend | St. 2 | 18 |
| Unten | 16:15–17:15 | Hip-Hop & Contemporary Kids | alle, 8–12 J. | 16 |
| Trainingsraum | 16:25–16:55 | Spitze | A, ab 12 J. | 8 |
| Oben | 17:00–18:15 | Ballett Erwachsene | A/M | 18 |
| Unten | 17:25–18:40 | Ballett Jugend & Spitze | St. 2 | 18 |
| Trainingsraum | 18:20–18:50 | Spitze | E/A, ab 12 J. | 8 |
| Unten | 18:45–19:45 | Modern Jazz | alle | 18 |
| Oben | 18:55–19:55 | Hip-Hop & Contemporary Teens | alle, 12–17 J. | 16 |
| Oben | 20:00–21:15 | Ballett Erwachsene | A/M | 18 |

*Donnerstag*

| Raum | Zeit | Kurs | Zielgruppe | Plätze |
|---|---|---|---|---|
| Unten | 14:40–15:30 | Tanzzwerge | ab 3 J. | 12 |
| Oben | 15:30–16:20 | Tänzerische Früherziehung | St. 3 | 14 |
| Unten | 15:30–16:30 | Kinderballett Vorstufe | St. 1 | 14 |
| Unten | 16:30–17:30 | Kinderballett | St. 2 | 14 |
| Oben | 16:30–17:45 | Ballett Jugend | St. 4 | 18 |
| Unten | 17:30–18:40 | Taiji | M/F | 16 |
| Oben | 17:45–19:10 | Ballett Erwachsene | A/M | 18 |
| Unten | 18:45–20:00 | Contemporary | E/A | 18 |
| Oben | 19:15–20:30 | Irish Dance | M | 18 |
| Unten | 20:00–21:30 | Contemporary | M | 18 |

*Freitag*

| Raum | Zeit | Kurs | Zielgruppe | Plätze |
|---|---|---|---|---|
| Oben | 10:15–11:30 | Tanzen 50+ | alle | 18 |
| Oben | 12:30–13:45 | Tanz für Einsteiger | E/A | 18 |
| Oben | 15:25–16:15 | Tänzerische Früherziehung | St. 1, 4–6 J. | 14 |
| Unten | 15:50–16:40 | Tänzerische Früherziehung | St. 2, 4–6 J. | 14 |
| Oben | 16:20–17:20 | Kinderballett | St. 1 | 14 |
| Unten | 16:45–17:45 | Kinderballett | St. 3 | 14 |
| Oben | 17:25–18:40 | Tanz für Einsteiger | E | 18 |
| Unten | 17:45–19:15 | Ballett Jugend & Spitze | St. 1 | 18 |
| Online | 18:45–19:45 | Taiji | alle | 40 |
| Oben | 18:45–20:00 | Ballett Erwachsene | A/M | 18 |
| Unten | 19:15–20:30 | Jazz Dance | alle | 18 |
| Oben | 20:15–21:45 | Tanz für Einsteiger | A | 18 |

*Samstag*

| Raum | Zeit | Kurs | Zielgruppe | Plätze |
|---|---|---|---|---|
| Online | 10:00–11:30 | Qigong | M/F | 40 |
| Oben | 10:15–11:45 | Tanz für Einsteiger | A | 18 |
| Unten | 11:00–12:30 | Irish Dance | E/A | 18 |
| Oben | 12:00–13:15 | Ballett Erwachsene | A/M | 18 |
| Unten | 13:15–14:30 | Modern Jazz | alle | 18 |
| Oben | 13:30–14:45 | Contemporary | M/F | 18 |

**Werkstatt workshops.** They take Sunday, the one day without classes: the Sunday on or after
each workshop's day offset. They play the same roles as the programme school's: one full with a
waiting list, one with guests and a trial its instructor added, and a room rental. Their titles
are German (e.g. Floor Barre, Pas de deux, Yoga für Tänzerinnen und Tänzer).

**Werkstatt passes and announcements.** These are German and fictional, e.g. "10er-Karte" and
"Monatskarte", with prices in whole euros, which are invented too.

## Review focus

- A level filter on a school whose classes mostly state only a stage or age: the children's
  classes drop out of the filtered schedule. Expected, and the filter's label says "Level".
- An office that reorders rooms while another office user creates one: the reorder lists the old
  set and fails with a 422, and the page reloads the rooms.
- A class with an age range but no level, edited in the admin sheet: the "to" select stays empty,
  and saving keeps both levels empty.
- The Werkstatt schedule on a phone: four room columns don't fit, so the day falls back to the
  one-list layout it already uses below the column width.
- Switching the web demo's school resets to that school's own logins: the logins page shows the
  Werkstatt accounts, not the programme school's.

## Out of scope

- Booking rules by age or level (who may book): the audience is shown and filtered, not
  enforced.
- Real teachers or participants of the previous app: not now, possibly later with consent.
