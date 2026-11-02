# 0021 — Levels and class audience

- Status: accepted
- Date: 2026-11-01
- Amends: [0007](0007-timetable-administration.md) (a new catalogue record the office maintains)

## Context

The previous app this one is modelled on stored a class's level as free text, so every school
wrote its own words ("Anfänger", "Mittelstufe", "A/M") and nothing could filter or order them. This
app went the other way: a class had one of five fixed values (`beginner`, `intermediate`,
`advanced`, `professional`, `open`), translated by the web. That fits the programme demo school,
but not a school whose scale has five steps of its own, German names and short codes, or classes
that span two levels. The school decided (2026-11-01) that levels become records of the school,
like course types: the office names them, orders them and gives them a code; a class then states a
span of them, or all levels, plus a stage and an age range.

This record was written in two steps: the levels themselves first (migration 00034), then the
class audience, which moved `classes.level` onto level rows (migration 00035).

## Decision

**A level is a record of the school.** `domain.Level{ID LevelID, Name, Code string, Position int}`
(`internal/domain/level.go`), stored in `levels` (migration 00034):

- `Name` is required, at most 60 characters (runes: an umlaut is one), trimmed by the service.
  Names are data, not locale strings: the programme school's are English, another school's are in
  its own language, and the web shows them as they are.
- `Code` is an optional short label of at most 8 characters ("A", "MC"), trimmed; an empty code is
  stored as `NULL`, so any number of levels can have none.
- Names, and codes when set, are unique ignoring ASCII case: two unique indexes on `lower(name)`
  and `lower(code)`, mapped to `domain.ErrDuplicate`, a 409 `duplicate`. The columns use collation
  `"C"`, as `course_types.name` does, so `lower()` folds ASCII letters only and the sort order is
  byte order: PostgreSQL and the memory store agree ("Ä" and "ä" are two names). The 409 does not
  say whether the name or the code is taken; the web's level form says "name or code".
- `Position` is 1-based and orders the scale from least to most advanced. Lists order by position,
  then name and ID.

**The office maintains them** (`service.CatalogService`, `httpapi/catalog_admin.go`), like course
types:

- `listLevels` (`GET /api/v1/levels`) is public: the schedule's level filter needs it before
  anyone signs in.
- `createLevel` (`POST /api/v1/levels`, 201), `updateLevel` (`PUT /api/v1/levels/{levelId}`) and
  `deleteLevel` (`DELETE …`, 204) are `office` writes registered `withConflict`. The bodies take a
  name and a code, never a position: a new level goes after the last one (highest position + 1),
  and an edit keeps its place. An unknown level is a 404 before any field check.
- `setLevelOrder` (`PUT /api/v1/level-order`, `office`, 204) takes `levelIds` and numbers them
  1..n, by the same set rule as `setRoomOrder` (ADR 0007, amendment "Room order"): the list must
  name every level exactly once and nothing else (`LevelRepo.SetOrder` checks it under a lock of
  all levels), otherwise it is a 422 on `levelIds`. An office that reorders the list it read before
  someone else added a level gets the 422; the Levels page says so and reloads the list.
- `LevelRepo.Update` writes the name and the code only, never the position. An edit therefore
  cannot write back a position a concurrent reorder has just changed, which `RoomRepo` needed a
  row lock for (a room's position also changes when it moves to another location; a level's only
  through the order).
- `createLevel` and `setLevelOrder` first take `LevelRepo.LockScale`, in PostgreSQL a
  transaction-scoped advisory lock, because the row lock of `SetOrder` cannot stop an insert.
  Concurrent creates therefore take positions one after another, and a create during a reorder
  waits and goes after the reordered levels, so the reorder cannot commit an order that misses it.
  There is still no unique constraint on the position; the lock keeps the positions apart.
- Deleting a level that a class uses as either end of its span is a 409 `in-use`
  (`domain.ErrInUse`): `LevelRepo.Delete` refuses it in both stores (in PostgreSQL the foreign keys
  are `ON DELETE RESTRICT`). The office first changes or retires the classes.

**A fresh school has none.** Neither the migration nor `init` creates levels: the office adds the
school's own on the Levels page (Admin → Levels, after Course types). The demo seed creates the
programme school's scale: Beginner (B), Intermediate (I), Advanced (A), Professional (P).

**The web.** The Levels page lists the levels in their order with name and code, "move up" and
"move down" buttons ("Move <level> up/down"; the first has no up, the last no down), each move
sending the whole new order, and edit and delete actions; deleting asks first. The table has no
column sorting or search, so it always shows the order the buttons change.

## Class audience

**A class states who it is for** (`internal/domain/audience.go`, `domain.Class`), each part
optional:

- **A level span** `Levels domain.LevelRange{From, To}`: two levels of the school's scale, both or
  neither (a 422 on `levelToId` otherwise). A class for one level names it twice. The ends may be
  stored in either order, because the office can reorder the scale after the class was saved: the
  span is always read lower level first (`service.Audience.Low`/`High`, by position, then ID), and
  a level moved between the two ends now belongs to the span. An unknown level on a create or an
  update is a 422 on `levelFromId` or `levelToId` ("level does not exist").
- **All levels** `AllLevels`: the class is for everyone, whatever their level. It excludes a span
  (a 422 on `allLevels`). It replaces the old `open`.
- **A stage** `Stage` 1–9 (a 422 on `stage` otherwise): the step within a course's progression
  (children's ballet runs stages 1–3), shown as "Stage 2" / "Stufe 2". It is a number, not a
  record.
- **An age range** `AgeFrom`, `AgeTo` in months (`domain.AgeMonths`, 1–1200, a 422 on
  `ageFromMonths`/`ageToMonths`); either end alone is fine ("from 3½", "up to 6"), and when both
  are given the range must not end before it starts. Months because children's classes go by
  half years; the web enters and shows years with halves ("3½").

A class with none of them states nothing, which is fine: a school without levels keeps working,
and the children's classes of the programme demo state only an age.

**The service resolves it.** `service.Audience` holds the span's levels resolved (`audienceOf`, a
class pointing at a missing level is a broken reference) plus the other parts.
`TimetableService`, the admin class list, the schedule (`ScheduledLesson.Audience`, from one
`Levels().List` per call) and the statistics' class rows carry it. The API embeds it in `Class`,
`LessonSummary` and the statistics row as `levelFrom`/`levelTo` (`LevelRef{id, name, code?}`, lower
first), `allLevels`, `stage`, `ageFromMonths` and `ageToMonths`, each omitted when absent. The class
bodies take `levelFromId`, `levelToId`, `allLevels`, `stage`, `ageFromMonths` and
`ageToMonths`.

**The level filter.** `getSchedule`'s `level` is a level ID. A lesson matches when its class is for
all levels or the level's position lies between the span's ends (inclusive,
`Audience.Covers`); a class without a level, a one-off lesson without a class and an unknown level
ID match nothing. The web offers the school's levels in their order and hides the filter while the
school has none; an old bookmark with an enum value (`?level=beginner`) is dropped.

**The web shows it** with `formatAudience` (`web/src/lib/audience.ts`): the long form with level
names ("Beginner–Intermediate · Stage 2 · 4–6 yrs") on the Programme card, the lesson sheet, the
timetable and the statistics; the short form with codes, falling back to the name ("A/M · Stufe 2
· 4–6 J."), on the Werkstatt card. The class sheet picks "No level", "All levels" or a level, then
"up to" a second one, a stage and the ages in years; a school without levels sees a link to the
Levels page instead of the selects.

**The migration** (00035) moves the old fixed value onto level rows. For each of `beginner`,
`intermediate`, `advanced` and `professional` that a class uses, it uses a level the school already
has by that English name (ignoring case) or else creates one ("Beginner" B, …, after the school's
own levels in the old order, without the code if another level has it); the class then spans that
level to itself. `open` becomes all levels. `classes.level` is dropped; `level_from_id` and
`level_to_id` reference `levels` `ON DELETE RESTRICT`, and CHECKs mirror the domain rules (both
ends or none, all levels without a span, stage 1–9, ages 1–1200 and in order). Down is lossy: it
restores `level` from the name of the span's lower end (by position, then ID) if it is one of the
four (ignoring case), else `open`, and leaves the level rows.

## Consequences

- Level names are not translated. A school that teaches in two languages picks the names it wants
  to show, possibly with both words ("Anfänger / Beginner").
- Renaming a level renames it on every class at once; reordering the scale can widen or narrow a
  span, which is what a school that reorders means.
- The filter compares positions only, so a level filter cannot express "exactly this level"; a
  class for one level and a span through it both match.
- Codes are unique, so the short form of a span ("A/M") is never ambiguous.
