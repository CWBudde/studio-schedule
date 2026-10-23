# 0007 — Timetable administration

- Status: accepted
- Date: 2026-10-24

## Context

Until Phase 3 the catalogue (locations, rooms, staff, course types) and the timetable (terms,
classes) only came from the seed. The office needs to maintain them. These records are referenced
by generated lessons, bookings and class memberships (ADR 0003), so editing or deleting one has
consequences beyond the record. The previous system let the office delete rows that lessons still
used, and its forms showed a bare "error" when the database refused something.

## Decision

**Who.** Every write to the catalogue and the timetable is an `office` operation (ADR 0005), so
admins can do it too. Reads keep their current level: the public lists stay public, and office-only
list variants exist only where the public list hides something (inactive staff, a staff member's
person link).

**Write conventions.** Every entity uses the same shape:

- `POST` on the collection creates and answers 201 with the entity.
- `PUT /{id}` replaces the editable fields and answers 200 with the entity.
- `DELETE /{id}` answers 204.
- Request bodies carry no IDs. New IDs come from `ids.Source`.

**Errors the UI can explain.**

- A 422 names the field. Domain rules carry the JSON name of their field (`domain.FieldError`),
  and the API reports it as `body.<field>`, the same location Huma uses for schema errors.
- A 409 carries a stable `reason` member in its problem document:
  - `in-use`: something still refers to the record.
  - `duplicate`: the name is taken.

  The UI shows a sentence per reason. It never shows constraint names or other internals.

**Deleting and retiring.**

- What nothing refers to can be deleted.
- A location with rooms, a room used by classes or lessons, and a course type used by classes or
  lessons answer `in-use`.
- Staff are deactivated instead of deleted. They then disappear from the public lists and filters
  but stay on past lessons.
- A class or term with past lessons is retired, not deleted.

**Names.** Location names are unique within the school, and room names within their location,
ignoring case.

**Classes and their lessons.** Editing a class or a term changes the generated lessons that lie in
the future and were not edited individually; they follow the class in the same transaction.
Lessons in the past and lessons that were edited one by one stay as they are. Terms may overlap,
e.g. a summer workshop term within the regular term. The class-to-lesson link this needs is the
watermark or slot link ADR 0003 asks for, and it comes with the class slice.

**Concurrency.** The last write wins. There is no `If-Match` or version column: the office is a
few people, and a stale form overwrites at most what the same person could have changed.

**Audit.** Catalogue and timetable edits are not written to `audit_events`, which stays for
security events (ADR 0005). Lesson administration decides separately about cancellations.

## Consequences

- Every entity slice of the admin area follows the same API shape and reuses the same table, form
  and delete-confirmation components.
- A refused delete tells the office why, so it can retire the record or remove what uses it.
- Two people editing the same record at the same time can overwrite each other without notice.
- Who changed the timetable is not recorded.
