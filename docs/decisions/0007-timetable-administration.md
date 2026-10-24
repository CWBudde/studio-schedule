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
- Staff are deactivated instead of deleted once classes or lessons refer to them; a staff member
  nothing refers to (one created by mistake) can be deleted. Deactivated staff disappear from the
  public lists and filters but stay on their lessons, past and future. Deactivating is allowed
  while they still teach; the office reassigns their classes.
- A class with past lessons is retired, not deleted. A term has no retired state: it ends by its
  date, and is deleted only while it has no classes; otherwise the delete is `in-use`, and the
  office deletes or retires its classes (2026-10-24). A class can be deleted, with its
  lessons, while none of them has started or been booked and it has no members; otherwise the
  delete is `in-use`. A retired class (`classes.retired`) generates no more lessons, leaves the
  public timetable, stays in the office's list and can be reactivated. Retiring deletes its future
  lessons that follow it (below) and have no bookings; booked ones stay until lesson
  administration cancels them.

**Names.** Location names and course type names are unique within the school, and room names
within their location, ignoring case. Staff display names are not unique.

**Classes and their lessons.** Editing a class or a term changes the generated lessons that lie in
the future and were not edited individually; they follow the class in the same transaction.
Lessons in the past and lessons that were edited one by one stay as they are. Terms may overlap,
e.g. a summer workshop term within the regular term. The class-to-lesson link this needs is the
slot link of ADR 0003: every generated lesson keeps the school date of its slot (2026-10-24).

- A future lesson _follows_ its class while it still is what the class generated: scheduled,
  public and regular, at its slot's start and end, with the class's course type, room, staff and
  capacity. Anything else counts as edited one by one; no separate flag is needed until lesson
  administration adds one.
- A follower takes the new time, room, staff and capacity, keeping its wall-clock time across DST.
  Its bookings move with it. A capacity below the active bookings of a follower is a 422 on
  `capacity`.
- A new weekday moves a follower within its Monday-based week. When that slot has passed, lies
  outside the term, or the week has a lesson that stays, the follower is deleted, or stays as it
  is while it has bookings. A lesson left on an earlier weekday follows no more.
- A weekly class has at most one lesson a week: generation skips a Monday-based week that has a
  lesson of the class, wherever it lies in that week.
- A class edit and lesson generation lock the class row (`Classes().Lock`), so generation never
  sees a half-moved class. A term edit, a class edit or creation, and generation lock the term
  row first (`Terms().Lock`), so nothing fills slots from a term's old dates (2026-10-24).
- After the edit, the class's missing lessons of the horizon are generated, as for a new class.
- A class stays in its term; the term is not part of the edit.

**Terms and their lessons.** A term's new date range takes the lessons of its classes along in the
same transaction (2026-10-24). Term names need not be unique.

- A lesson whose slot date the new range leaves out is deleted while it lies ahead, follows its
  class and has no bookings.
- A lesson left out that has started, has bookings or was edited one by one refuses the new range
  with a 422 on `startsOn` or `endsOn`, whichever leaves it out. The office moves or cancels it
  first; history never ends up outside its term.
- A longer range gets the class's missing lessons of the horizon that have not started, as after a
  class edit (ADR 0003: generation does not create the past).

**Class members.** The office enrolls persons in a class with an optional "since" date and ends a
membership by removing it. A person is a member once (409 `duplicate`); an unknown person is a 422
on `personId`. A class with members cannot be deleted. Memberships do not book anyone until Phase
4 (2026-10-24).

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
