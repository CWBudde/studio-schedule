# 0021 — Levels and class audience

- Status: accepted (levels); the class audience follows
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

This record is written in two steps. The levels themselves (this step) come first; the class
audience, which moves `classes.level` onto level rows, follows in the next one.

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
- As for rooms there is no unique constraint on the position: two concurrent creates may tie, the
  tie falls back to the name, and the next reorder numbers the levels 1..n again.
- Deleting a level that classes use will be a 409 `in-use` (`domain.ErrInUse`) once classes refer
  to levels; until then nothing refers to them.

**A fresh school has none.** Neither the migration nor `init` creates levels: the office adds the
school's own on the Levels page (Admin → Levels, after Course types). The demo seed creates the
programme school's scale: Beginner (B), Intermediate (I), Advanced (A), Professional (P).

**The web.** The Levels page lists the levels in their order with name and code, "move up" and
"move down" buttons ("Move <level> up/down"; the first has no up, the last no down), each move
sending the whole new order, and edit and delete actions; deleting asks first. The table has no
column sorting or search, so it always shows the order the buttons change.

## Class audience

Follows with the next step of the Werkstatt catalogue plan: a class states a level span
(`levelFromId`, `levelToId`, the ends in either order), or all levels, or no level; plus a stage
and an age range in months. The fixed enum (now `domain.SkillLevel`) and `classes.level` give way
to level rows then, and this section records the span rule, the level filter and the migration.

## Consequences

- Level names are not translated. A school that teaches in two languages picks the names it wants
  to show, possibly with both words ("Anfänger / Beginner").
- Until the class audience lands, levels exist beside the fixed class level: the Levels page edits
  a scale that no class uses yet.
- Codes are unique, so the short form of a span ("A/M") is never ambiguous.
