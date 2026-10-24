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

- A future lesson _follows_ its class while it is not detached (below) and still is what the class
  generated: scheduled, public and regular, at its slot's start and end, with the class's course
  type, room, staff and capacity. Anything else counts as edited one by one.
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

- A lesson whose slot date the new range leaves out is deleted while it lies ahead and has no
  bookings, whether it follows its class, was edited one by one (detached) or is canceled: nobody
  is booked into it, so nobody misses it (2026-10-24, replacing "follows its class").
- A lesson left out that has started or has bookings refuses the new range with a 422 on
  `startsOn` or `endsOn`, whichever leaves it out. History never ends up outside its term, and the
  office handles the bookings first.
- A longer range gets the class's missing lessons of the horizon that have not started, as after a
  class edit (ADR 0003: generation does not create the past).

**Class members.** The office enrolls persons in a class with an optional "since" date and ends a
membership by removing it. A person is a member once (409 `duplicate`); an unknown person is a 422
on `personId`. A class with members cannot be deleted. Since Phase 4, members are booked into the
class's lessons automatically, and a class holds at most as many members as its capacity (see the
amendment below and ADR 0009).

**Lessons (2026-10-24).** The office edits, cancels and adds single lessons. Every lesson write is
an `office` operation; instructors get no write access (they are notified from Phase 4).

- _Detached._ An individually edited lesson is detached from its class (`lessons.detached`): every
  lesson write of the office sets the flag on a lesson of a class (a one-off lesson has no class
  to detach from), and it is never cleared, a restore included. A detached lesson does not follow
  class or term edits, whatever its fields. The implicit rule above stays for lessons edited before
  the flag existed (the migration starts every lesson undetached). Generation never touches an
  existing lesson: it skips the week of every lesson of the class, detached and moved ones
  included.
- _Edit._ The office replaces a lesson's school date, start time, duration, room, staff, capacity,
  visibility and kind with `PUT /lessons/{id}`; course type and class stay, and so does the slot
  date of a lesson of a class (ADR 0003). A lesson that has started is not edited any more: a 422
  whose error has the location `path.lessonId`, since the lesson the path names, not a field of
  the body, is the problem. A lesson created or moved to a start that is not in the future is a 409
  `past`; the office can save it anyway by repeating the request with `confirmPast: true`, e.g. to
  record a lesson after the fact. Like a class, a lesson ends on its school day (422 on
  `durationMinutes`). A capacity below the active bookings is a 422 on `capacity`. Hiding a lesson
  with bookings is allowed; the UI confirms it with the booking count, and nobody is mailed.
  Changing the kind is allowed on every lesson: a regular lesson that becomes a workshop or private
  lesson is published early from then on, and a workshop that becomes regular follows the release
  window (ADR 0003).
- _Cancel and restore._ Canceling a canceled lesson or restoring a scheduled one changes nothing
  and mails nobody, whether the lesson has started or not; a repeated cancel keeps the first note.
  Otherwise only a lesson that has not started can be canceled or restored (422 on
  `path.lessonId`, as for an edit). Cancelling takes an optional note and records when and by whom
  (`lessons.canceled_at`, `canceled_by`); an admin signed in as another account (ADR 0005) is
  recorded as themselves, not as the impersonated account. Restoring clears the three and keeps the
  lesson detached; it checks the room like an edit (409 `room-taken` unless the otherwise optional
  body sends `confirmRoomOverlap: true`), since another lesson may have taken the room while the
  lesson was canceled. The same transaction queues a mail (outbox) to everyone with an active
  booking: "lesson canceled" with the note, or "takes place after all" with the lesson's current
  time and room. Bookings stay as they are until Phase 4/6 decide about passes. The office sees the
  note, the time and the account in the lesson sheet; the public schedule shows only the status.
- _Changed lessons._ An edit of the start or the room of a scheduled lesson with active bookings
  queues a "lesson changed" mail to the same recipients, naming the old and the new start and room.
  Staff, capacity, duration and visibility edits send nothing. Moving a canceled lesson sends
  nothing either; its restore mail always states the time and room it then has, with a hint that
  they may have changed since the cancellation. The lesson does not remember what its cancel mail
  said, so the mail cannot tell whether they did.
- _No mails for the past._ A lesson whose start is not in the future mails nobody, e.g. one moved
  into the past with `confirmPast`. (Cancel and restore refuse started lessons anyway.)
- _Recipients._ For each active booking of a person: nobody if the person's account is disabled
  (no fallback); else the account's address if it is confirmed; else, for no account or an
  unconfirmed one, the person's own address; else the addresses of the guardians' accounts that
  are confirmed and not disabled (a guardian without such an account gets nothing, not even their
  own address). For a guest booking, the guest's address. Each address (ignoring case) gets one
  mail per change, in the language of its account (else the school's default), naming the booked
  persons it is about; an address reached both as an account's and as a person's own (a child
  with the parent's address) is mailed as the account's, with its language and name.
- _Room overlaps._ A create, an edit that changes the time or the room, and a restore check the
  room: another lesson that is not canceled and overlaps it there is a 409 `room-taken` that names
  that lesson (its ID, start and course type). The office can save anyway: the request repeated
  with `confirmRoomOverlap: true` skips the check. Canceled lessons do not count. When a create or
  an edit is both in the past and in a taken room, `past` comes first; the retry with
  `confirmPast` may then answer `room-taken`.
- _One-off lessons._ The office creates one-off lessons (no class): course type, room, staff,
  school date and start time, duration, capacity, visibility and kind (a workshop or a private
  lesson is published early, a regular one follows the release window; ADR 0003).
- _Deleting._ A lesson generated from a class is canceled, never deleted (409 `generated`):
  generation would create it again. A one-off lesson can be deleted while it has not started (422
  on `path.lessonId`, as for an edit) and no booking refers to it, canceled ones included (409
  `in-use` otherwise).
- _Cloning._ The office copies the scheduled lessons of a school day or a Monday-based week to
  another day or week, in any term (`previewLessonClone`, `cloneLessons`). A week's source and
  target are Mondays; the target differs from the source and does not lie before today (for a week:
  its Monday, so the current week is refused unless today is Monday), each a 422 on the field. The
  source may lie in the past. Copies are one-off lessons (no class, not detached) at the same
  school-local wall-clock time, so 17:00 stays 17:00 across a DST change, with the course type,
  room, staff, length, capacity, kind and visibility of the source; never bookings, never a
  cancellation. Canceled source lessons are left out. A source lesson is skipped, in this order, as
  `class-slot` when its class (not retired) has a slot in the target week within its term
  (generation covers it, whether or not it has run), as `started` when the copy would not start in
  the future (a target of today), as `staff-inactive` when its instructor or accompanist is no
  longer active (former staff; creating or editing a lesson does not check this), and as
  `room-taken` when another lesson that is not canceled, a lesson that generation will create for a
  class (not retired, a slot within its term that has not started, no lesson of the class in that
  week yet, e.g. beyond the horizon), or an earlier copy of the same clone (in the order of the
  source lessons) uses the room at an overlapping time, naming that lesson like the 409 (a copy
  without its ID in a preview, with `copyOf`, its source; a lesson not generated yet without ID and
  `copyOf`, with its `classId`). Generation does not check rooms, so a copy must not take a room a
  class will fill. So the same clone applied twice creates nothing the second time. The preview
  lists the copies and every skipped lesson with its reason; the clone recomputes it in one
  transaction, locking the terms and classes it reads in generation's order (all terms first, then
  their classes), and only adds lessons. Nobody is mailed. Cloned regular lessons follow the release
  window like any regular lesson.

**Concurrency.** The last write wins. There is no `If-Match` or version column: the office is a
few people, and a stale form overwrites at most what the same person could have changed.

**Audit.** Catalogue and timetable edits are not written to `audit_events`, which stays for
security events (ADR 0005). A lesson's cancellation records its time and account on the lesson
itself (above); other lesson edits are not recorded.

## Consequences

- Every entity slice of the admin area follows the same API shape and reuses the same table, form
  and delete-confirmation components.
- A refused delete tells the office why, so it can retire the record or remove what uses it.
- Two people editing the same record at the same time can overwrite each other without notice.
- Who changed the timetable is not recorded, except who canceled a lesson.
- A class edit no longer reaches a lesson the office has touched, even one it set back by hand to
  what the class generates; the office edits such a lesson one by one.

## Amendment (2026-10-24): member bookings

Class members are booked automatically into their class's lessons (source `member`, ADR 0009).
Term shortening, a class's weekday move and retiring a class ignore those bookings, active or
canceled; only other bookings (source `self`, later also the staff's) still keep a lesson or refuse
the change as described above. A canceled lesson keeps its bookings (ADR 0009).

- A lesson these edits remove is deleted, so its member bookings are deleted with it, together with
  their booking events: a booking cannot outlive its lesson, and a lesson that never takes place
  needs no record of who would have come. Canceling them first would leave the same rows pointing
  at nothing (`Bookings().DeleteByLessonAndSource`).
- A follower that moves keeps its member bookings, like any other booking.
- Deleting a class follows the same rule: once its last member is removed, the member bookings
  that removal canceled no longer keep it from being deleted.
- A class edit refuses a capacity it lowers below the class's member count or below the active
  bookings of a follower (422 on `capacity`). A follower booked beyond its capacity
  by a member's override moves along when the capacity stays.
- Taking back a released date creates a `self` booking (ADR 0009), which keeps its lesson like any
  other booking of a member.

## Amendment (2026-10-24): instructors write participants

Lessons stay the office's: instructors and accompanists get no write access to lessons, classes or
terms. From Phase 4 they **write the participants of their own lessons**: add persons and guests,
mark trials, remove participants and take attendance (ADR 0009, amendment). "Their own" means the
lesson's instructor or accompanist is an active staff member whose `staff.person_id` is the
signed-in account's person; the office acts on every lesson, and any other lesson is a 404. The
operations are at the access level `staff` (ADR 0005), which checks the role; the service checks the
lesson.
