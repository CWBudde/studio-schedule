# Werkstatt catalogue: room order, class audiences and a school of its own

Date: 2026-11-01. Follows [the themes and layouts spec](2026-10-30-themes-and-layouts-design.md)
and ADR 0018.

## Goal

The Werkstatt demo ("Tanzhaus am Lindenhof") should be comparable with the previous app it is
modelled on: the same kinds of courses, the same way of stating levels, a weekly plan of the same
density and shape, and the rooms in the order that school shows them. Today both demo schools
share every person, class and lesson, the Werkstatt catalogue is the programme school's English
one, levels are one of five fixed values (the previous app had free text), and room columns sort
alphabetically (Oben, Trainingsraum, Unten).

Three changes get there:

1. **Room order.** A room has a position; lists and the Werkstatt room columns follow it.
2. **Class audience.** Levels become a list the school manages. A class states a level range,
   a stage and an age range instead of one fixed level.
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

**Store.** Migration `00033_room_position.sql` (the next free number at the time of writing):
`rooms.position integer NOT NULL CHECK (position > 0)`, backfilled per location in the current
name order, so nothing visible changes for an existing school. No unique constraint (a reorder
writes all positions in one transaction).
`RoomRepo` lists rooms ordered by location, then position, then name, then id. A new
`RoomRepo.SetOrder(ctx, locationID, []RoomID)` writes positions 1..n; it fails with
`domain.ErrValidation` unless the IDs are exactly that location's rooms.

**Service and API.**

- `createRoom` appends: position = the location's highest + 1. The create and update bodies
  don't take a position.
- New operation `setRoomOrder` (office): `PUT /api/v1/locations/{locationId}/room-order` with
  `{"roomIds": [...]}` → 204, beside the other catalogue writes (`/api/v1/rooms`). A list that misses a room, repeats one or names another location's
  room is a 422 on `body.roomIds`.
- Rooms in the API (`listRooms`, the schedule's `room`, the admin lists) carry `position`.

**Web.**

- The office's locations page lists a location's rooms in position order, each with "move up" and
  "move down" buttons (accessible names "Move <room> up/down"; the first has no up, the last no
  down). Each move sends the whole new order.
- `roomColumns` (Werkstatt) sorts by location name, then room position, then room name, then id.
- Room pickers and the schedule's location/room filter options follow the API order.

## 2. Class audience

A class says who it is for with three optional parts, and the cards show them. Levels are no
longer a fixed list: each school keeps its own, in its own words, and the office manages them.

**Levels.** The previous app stored a class level as free text. Here a level is a record of the
school, like a course type:

- `domain.Level{ID LevelID, Name, Code string, Position int}`, with a new typed ID
  `domain.LevelID`.
- `Name` is required, 1–60 characters, trimmed, and unique per school ignoring case (a duplicate
  is `domain.ErrDuplicate`, a 409 `duplicate`).
- `Code` is an optional short label of at most 8 characters ("A", "M"), unique when set.
- `Position` is 1-based and orders the scale, from least to most advanced.
- Names are data, not locale strings, with one name per level. The programme school's levels
  are English, Werkstatt's German.

A class has a level **range**, or is for **all levels**, or states no level at all (common for
children's classes, which state a stage and an age instead). The range is the span between two
levels, `From` and `To`, equal for one level. The two ends may be stored in either order: the span
covers every level whose position lies between theirs. Reordering the levels therefore never
makes a stored class invalid, and the web always shows the range from the lower level to the
higher one.

**Stage.** An optional whole number from 1 to 9, the step within a course's progression
("Stufe 2"; children's ballet runs Stufe 1–3, youth ballet 1–4).

**Age.** An optional range in months, `AgeFrom`, `AgeTo` (0 = not stated; months so that
"ab 3½ J." is exact). The web shows years, halves as "½": "ab 3 J.", "4–6 J.", "ab 12 J."; en
"from 3 yrs", "4–6 yrs".

**Domain.** In `internal/domain/timetable.go`, `Class.Level` becomes
`Levels LevelRange{From, To LevelID}` (zero = no level), `AllLevels bool`, `Stage int` and
`AgeFrom, AgeTo AgeMonths`. The `domain.Level` enum and its `Levels()`/`Valid()` go away.
`Validate`:

- `From` and `To` are both set or both zero.
- `AllLevels` is not combined with a range.
- `Stage` is 0 or 1–9.
- Ages are 0 or 1–1200, and `AgeTo` is 0 or ≥ `AgeFrom`.

The class service checks that both level IDs exist. Errors are field errors named like the body
fields (`levelFromId`, `levelToId`, `allLevels`, `stage`, `ageFromMonths`, `ageToMonths`).

**Store.**

- A new `LevelRepo` on `store.Tx` (`Levels()`): `List` (by position, then name, then id), `Get`,
  `Create`, `Update`, `Delete` and `SetOrder(ctx, []LevelID)`. Each method gets a `storetest`
  case on both stores.
- `SetOrder` writes positions 1..n. It fails with `domain.ErrValidation` unless the IDs are
  exactly the school's levels, the same rule as `RoomRepo.SetOrder`.
- `Delete` of a level a class uses fails with `domain.ErrInUse` (FK `ON DELETE RESTRICT`).
- Migration `00034_levels.sql` creates `levels` (uuid id, name, code, position; unique on
  `lower(name)` and on `lower(code)`).
- Migration `00035_class_audience.sql` (the next free numbers at the time of writing):
  - inserts a row for each old enum value an existing class uses, named and coded in English
    (Beginner B, Intermediate I, Advanced A, Professional P), in scale order;
  - replaces `classes.level` with `level_from_id` and `level_to_id` (nullable, both or neither),
    `all_levels boolean NOT NULL DEFAULT false` (not with a range), `stage smallint` and
    `age_from_months`/`age_to_months smallint` (nullable, CHECKs as above);
  - points each existing class at its row (`level_from_id = level_to_id`) and turns `open`
    into `all_levels = true`.

  A fresh database has no levels, and `init` creates none: the office adds the school's own.

**API.**

- Public `listLevels`, `GET /api/v1/levels`: the schedule filter needs it when nobody is signed
  in.
- Office writes, registered `withConflict` and pinned in `internal/app/access_test.go`:
  - `createLevel` `POST /api/v1/levels` → 201;
  - `updateLevel` `PUT /api/v1/levels/{levelId}`;
  - `deleteLevel` `DELETE /api/v1/levels/{levelId}` → 204, or 409 `in-use`;
  - `setLevelOrder` `PUT /api/v1/level-order` with `{"levelIds": [...]}` → 204. A list
    that misses a level, repeats one or names an unknown one is a 422 on `body.levelIds`.
- Class bodies replace `level` with `levelFromId`, `levelToId` and `allLevels`, plus `stage`,
  `ageFromMonths` and `ageToMonths`.
- Class responses, the schedule lesson and the statistics row carry `levelFrom` and `levelTo` as
  a `LevelRef{id, name, code}` (the same pattern as `CourseTypeRef`), ordered lower first, plus
  `allLevels`, `stage`, `ageFromMonths` and `ageToMonths`. Each is omitted when absent. A
  service reads the level list once per request, not once per lesson.
- One-off lessons have none of them.

**Schedule filter.** `level=<levelId>` replaces the enum. It keeps lessons of classes whose span
covers that level's position, and of all-levels classes. Classes without a level and one-off
lessons don't match a level filter, as one-off lessons don't today. An unknown ID matches nothing.
The filter's options come from `listLevels`, and the web's filter parsing drops an old enum value
from a bookmark.

**Web.**

- A new office page **Levels**, next to Course types in the admin navigation, built like
  `CourseTypesPage` and `CourseTypeSheet`:
  - the levels in position order, with name and code;
  - a sheet to create and edit one (name, code);
  - "move up" and "move down" buttons (accessible names "Move <level> up/down"; the first has
    no up, the last no down), each move sending the whole new order;
  - delete with a confirmation. A level in use is refused with the in-use message.
- One formatter, `formatAudience` in `web/src/lib/audience.ts`, renders the parts in the user's
  language:
  - the long form for the programme theme uses level names, e.g.
    "Beginner–Intermediate · Stage 2 · 4–6 yrs";
  - the short form for Werkstatt uses codes and falls back to the name, e.g.
    "A/M · Stufe 2 · 4–6 J.".

  "All levels" / "alle Level" is a locale string. The cards, the lesson sheet, the timetable
  week list and the statistics table use the formatter.

- The admin class sheet edits the parts:
  - "from" and "to" selects with the school's levels ("to" defaults to "from"), and "All
    levels" as the alternative to a range. With no levels defined, the selects are replaced by
    a link to the Levels page;
  - a stage number;
  - two age fields in years, half years allowed.

  It validates with zod against the same rules, and the server's 422s land on the fields.

**Command line.** The `classes` and `stats` tables print the range by level name, lower first,
or "all levels". There are no `levels` commands.

**ADR 0021** "Levels and class audience" records the levels table, the span rule, all levels, stage and age,
the filter rule and the migration.

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

| position | room          | capacity |
| -------- | ------------- | -------- |
| 1        | Oben          | 24       |
| 2        | Unten         | 18       |
| 3        | Online        | 40       |
| 4        | Trainingsraum | 8        |

**Levels.** Each school's seed creates its own levels:

| position | programme    | code | Werkstatt       | code |
| -------- | ------------ | ---- | --------------- | ---- |
| 1        | Beginner     | B    | Einsteiger      | E    |
| 2        | Intermediate | I    | Anfänger        | A    |
| 3        | Advanced     | A    | Mittelstufe     | M    |
| 4        | Professional | P    | Fortgeschritten | F    |
| 5        |              |      | Profi           | P    |

The programme classes keep their levels (`open` becomes all levels). The Werkstatt plan below
names levels by code.

**Werkstatt course types** (each with a short, newly written German description):

Tanzzwerge, Tänzerische Früherziehung, Kinderballett Vorstufe, Kinderballett, Ballett Jugend,
Ballett Jugend & Spitze, Spitze, Ballett Erwachsene, Ballett 45+, Tanz für Einsteiger,
Tanzen 50+, Contemporary, Contemporary Kids, Hip-Hop & Contemporary Kids, Hip-Hop & Contemporary
Teens, Modern Jazz, Jazz Dance, Stepptanz, Irish Dance, Stretch & Strength, Qigong, Taiji,
Morning Workout.

**Werkstatt weekly plan**: 70 classes from Monday to Saturday, with Sunday free.

Each row gives the room, the time, the course type, the audience and the capacity. Audience
codes: E/A/M/F level ranges by the codes above, "alle" = all levels, "St." = stage, ages in
years. Instructors are assigned from the Werkstatt staff so that no instructor teaches two
classes at once.

_Montag_

| Raum   | Zeit        | Kurs                      | Zielgruppe    | Plätze |
| ------ | ----------- | ------------------------- | ------------- | ------ |
| Online | 10:15–11:15 | Qigong                    | alle          | 40     |
| Oben   | 14:55–15:45 | Tanzzwerge                | ab 3 J.       | 12     |
| Oben   | 15:45–16:35 | Tänzerische Früherziehung | St. 2, 4–6 J. | 14     |
| Unten  | 15:45–16:45 | Ballett Jugend            | St. 2         | 18     |
| Oben   | 16:45–18:00 | Ballett Jugend & Spitze   | St. 1         | 18     |
| Unten  | 16:45–17:45 | Contemporary Kids         | A, 8–12 J.    | 16     |
| Online | 17:45–18:45 | Taiji                     | alle          | 40     |
| Unten  | 17:45–19:15 | Irish Dance               | F             | 18     |
| Oben   | 18:00–19:10 | Contemporary              | A/M           | 18     |
| Oben   | 19:15–20:15 | Stretch & Strength        | alle          | 16     |
| Unten  | 19:15–20:15 | Stepptanz                 | E/A           | 16     |
| Oben   | 20:15–21:45 | Ballett Erwachsene        | M/F           | 18     |
| Unten  | 20:15–21:45 | Ballett 45+               | M             | 16     |

_Dienstag_

| Raum          | Zeit        | Kurs                    | Zielgruppe  | Plätze |
| ------------- | ----------- | ----------------------- | ----------- | ------ |
| Online        | 08:45–09:15 | Morning Workout         | alle        | 40     |
| Unten         | 10:30–11:45 | Contemporary            | E/A         | 18     |
| Unten         | 12:00–13:15 | Ballett Erwachsene      | A/M         | 18     |
| Oben          | 15:00–16:00 | Kinderballett Vorstufe  | St. 1       | 14     |
| Unten         | 15:00–16:00 | Kinderballett Vorstufe  | St. 3       | 14     |
| Oben          | 16:00–17:00 | Ballett Jugend          | St. 1       | 18     |
| Unten         | 16:00–17:00 | Ballett Jugend          | St. 3       | 18     |
| Trainingsraum | 17:00–17:30 | Spitze                  | E, ab 12 J. | 8      |
| Oben          | 17:30–18:45 | Tanz für Einsteiger     | E           | 18     |
| Unten         | 17:30–18:30 | Stepptanz               | E/A         | 16     |
| Oben          | 18:45–20:00 | Tanzen 50+              | alle        | 18     |
| Unten         | 18:45–20:00 | Ballett Jugend & Spitze | St. 3       | 18     |
| Oben          | 20:00–21:30 | Contemporary            | F           | 18     |
| Unten         | 20:00–21:30 | Tanz für Einsteiger     | A           | 18     |

_Mittwoch_

| Raum          | Zeit        | Kurs                         | Zielgruppe     | Plätze |
| ------------- | ----------- | ---------------------------- | -------------- | ------ |
| Online        | 09:45–10:45 | Qigong                       | alle           | 40     |
| Oben          | 11:00–12:15 | Qigong                       | alle           | 16     |
| Unten         | 11:00–12:15 | Ballett Erwachsene           | M              | 18     |
| Oben          | 14:25–15:25 | Kinderballett                | St. 1          | 14     |
| Unten         | 14:25–15:15 | Tänzerische Früherziehung    | St. 1, 4–6 J.  | 14     |
| Unten         | 15:15–16:15 | Kinderballett Vorstufe       | St. 2          | 14     |
| Oben          | 15:25–16:25 | Ballett Jugend               | St. 2          | 18     |
| Unten         | 16:15–17:15 | Hip-Hop & Contemporary Kids  | alle, 8–12 J.  | 16     |
| Trainingsraum | 16:25–16:55 | Spitze                       | A, ab 12 J.    | 8      |
| Oben          | 17:00–18:15 | Ballett Erwachsene           | A/M            | 18     |
| Unten         | 17:25–18:40 | Ballett Jugend & Spitze      | St. 2          | 18     |
| Trainingsraum | 18:20–18:50 | Spitze                       | E/A, ab 12 J.  | 8      |
| Unten         | 18:45–19:45 | Modern Jazz                  | alle           | 18     |
| Oben          | 18:55–19:55 | Hip-Hop & Contemporary Teens | alle, 12–17 J. | 16     |
| Oben          | 20:00–21:15 | Ballett Erwachsene           | A/M            | 18     |

_Donnerstag_

| Raum  | Zeit        | Kurs                      | Zielgruppe | Plätze |
| ----- | ----------- | ------------------------- | ---------- | ------ |
| Unten | 14:40–15:30 | Tanzzwerge                | ab 3 J.    | 12     |
| Oben  | 15:30–16:20 | Tänzerische Früherziehung | St. 3      | 14     |
| Unten | 15:30–16:30 | Kinderballett Vorstufe    | St. 1      | 14     |
| Unten | 16:30–17:30 | Kinderballett             | St. 2      | 14     |
| Oben  | 16:30–17:45 | Ballett Jugend            | St. 4      | 18     |
| Unten | 17:30–18:40 | Taiji                     | M/F        | 16     |
| Oben  | 17:45–19:10 | Ballett Erwachsene        | A/M        | 18     |
| Unten | 18:45–20:00 | Contemporary              | E/A        | 18     |
| Oben  | 19:15–20:30 | Irish Dance               | M          | 18     |
| Unten | 20:00–21:30 | Contemporary              | M          | 18     |

_Freitag_

| Raum   | Zeit        | Kurs                      | Zielgruppe    | Plätze |
| ------ | ----------- | ------------------------- | ------------- | ------ |
| Oben   | 10:15–11:30 | Tanzen 50+                | alle          | 18     |
| Oben   | 12:30–13:45 | Tanz für Einsteiger       | E/A           | 18     |
| Oben   | 15:25–16:15 | Tänzerische Früherziehung | St. 1, 4–6 J. | 14     |
| Unten  | 15:50–16:40 | Tänzerische Früherziehung | St. 2, 4–6 J. | 14     |
| Oben   | 16:20–17:20 | Kinderballett             | St. 1         | 14     |
| Unten  | 16:45–17:45 | Kinderballett             | St. 3         | 14     |
| Oben   | 17:25–18:40 | Tanz für Einsteiger       | E             | 18     |
| Unten  | 17:45–19:15 | Ballett Jugend & Spitze   | St. 1         | 18     |
| Online | 18:45–19:45 | Taiji                     | alle          | 40     |
| Oben   | 18:45–20:00 | Ballett Erwachsene        | A/M           | 18     |
| Unten  | 19:15–20:30 | Jazz Dance                | alle          | 18     |
| Oben   | 20:15–21:45 | Tanz für Einsteiger       | A             | 18     |

_Samstag_

| Raum   | Zeit        | Kurs                | Zielgruppe | Plätze |
| ------ | ----------- | ------------------- | ---------- | ------ |
| Online | 10:00–11:30 | Qigong              | M/F        | 40     |
| Oben   | 10:15–11:45 | Tanz für Einsteiger | A          | 18     |
| Unten  | 11:00–12:30 | Irish Dance         | E/A        | 18     |
| Oben   | 12:00–13:15 | Ballett Erwachsene  | A/M        | 18     |
| Unten  | 13:15–14:30 | Modern Jazz         | alle       | 18     |
| Oben   | 13:30–14:45 | Contemporary        | M/F        | 18     |

**Werkstatt workshops.** They take Sunday, the one day without classes: the Sunday on or after
each workshop's day offset. They play the same roles as the programme school's: one full with a
waiting list, one with guests and a trial its instructor added, and a room rental. Their titles
are German (e.g. Floor Barre, Pas de deux, Yoga für Tänzerinnen und Tänzer).

**Werkstatt passes and announcements.** These are German and fictional, e.g. "10er-Karte" and
"Monatskarte", with prices in whole euros, which are invented too.

## Review focus

- Reordering levels after classes use them: every class's span stays valid and is shown lower
  level first; a span's members follow the new order (a level moved between the two ends now
  belongs to it).
- Deleting a level a class uses: a 409 `in-use`, the Levels page shows the in-use message, and
  nothing is deleted.
- A level filter on a level strictly between a class's two ends: the class is in the result.
- The admin class sheet on a school without levels: no empty selects, a link to the Levels page,
  and saving a class without a level works.
- Two levels whose names differ only in case, or a code used twice: a 409 `duplicate` on create
  and on rename.
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

- Level lists per course type, translated level names, and `levels` commands on the command
  line.
- Booking rules by age or level (who may book): the audience is shown and filtered, not
  enforced.
- Real teachers or participants of the previous app: not now, possibly later with consent.
