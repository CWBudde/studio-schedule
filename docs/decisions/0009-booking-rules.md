# 0009 — Booking rules

- Status: accepted
- Date: 2026-10-24

## Context

Phase 4 lets members book lessons. The schema has had bookings, booking events, the partial unique
index `bookings_one_active_key` and `Lessons().Lock` since Phase 1, and the settings carry a
`BookingCutoff` and a `CancellationCutoff`, but nothing used them. The release window and early
access (ADR 0003) decide what a member sees; the family check (ADR 0008) decides for whom an
account may act. ADR 0007 left open what a lesson's cancellation does to its bookings and how class
membership becomes bookings, and ADR 0008 left open whether guardians book for managed dependents.
The previous system booked by "notify all" on a free place and let parents cancel until the start
without a trace.

## Decision

**Who books for whom.** A signed-in account books and cancels for its own person and for all of
its dependents (`service.dependentOf`), managed dependents included (a teenager with their own
login can also book for themself). This closes ADR 0008's open question. A person outside the
family, and a booking of such a person, is a 404, never a 403. Everything also works while an admin
is signed in as the account (ADR 0005).

**What can be booked.** _Early access is early booking:_ a member books exactly what they may see
under ADR 0003 (`viewer.sees`): public and members-only lessons up to their visible days, and
workshops and private lessons once published. A lesson the caller may not see is a 404, as in
`getLesson`. Staff (instructor, office, admin) book themselves **under member rules**: they see
every lesson, but one that members may not book yet (not released and beyond the caller's early
access) or at all (hidden) is a 409 `closed` with why `release`.

**Book** (`bookLesson`, `POST /api/v1/lessons/{lessonId}/bookings`, body `{personId}`, 201 with the
booking). One transaction: `InTx → family check → Lessons().Lock → checks → Create → booking event
→ mail`. The checks, in this order, after the 404s:

1. `release` (staff only, above);
2. `canceled`: the school canceled the lesson;
3. `started`: the lesson has started or is over (`now >= startsAt`);
4. `cutoff`: `now >= startsAt − BookingCutoff`, so booking closes exactly at the cutoff;
5. `already-booked`: the person has an active booking for the lesson. It is checked under the lesson
   lock; the unique index is the fallback and maps to the same reason. The stores report that index
   as a duplicate, apart from a missing reference: a person deleted after the family check (the
   person is not locked) is a 404, never `already-booked`;
6. `full`: the active bookings fill the capacity.

The first four are the 409 `closed` with a `why` member (`release`, `canceled`, `started`, `cutoff`)
so the UI can say why; the others are their own reasons. Active bookings are those not canceled:
attended and no-show bookings hold their place. Booking again after a cancellation creates a new
booking (a new row with its own history); a canceled booking is never reactivated.

**Cancel** (`cancelBooking`, `POST /api/v1/bookings/{bookingId}/cancel`, 200 with the booking).
Own and dependents' bookings only (404 otherwise, guest bookings included). The booking's lesson
is locked, the booking read again under the lock, and then:

- A booking canceled already is a 409 `already-canceled` (not idempotent, so the UI notices a stale
  view; nothing changes).
- A booking whose lesson has started cannot be canceled (422 on `path.bookingId`, like the lesson
  edits of ADR 0007). Removing a participant after the start is a staff action (later in Phase 4).
- Otherwise it is canceled and its place is free in the same transaction. From
  `startsAt − CancellationCutoff` on (exactly at the cutoff included) the cancellation is allowed
  but **late**: `bookings.late` and the event's `late` are set. A booking of a lesson the school
  canceled is never late.
- The freed place will invite the next waiting-list entry in the same transaction, under the same
  lock. The waiting list does not exist yet; `BookingService.cancel` marks the spot.

**Cancel the following weeks** (`previewCancelFollowing` as `GET`, `cancelFollowing` as `POST
/api/v1/bookings/{bookingId}/cancel-following`). For a booking of a class lesson: that booking
first, then the same person's other active bookings of the same class whose lessons have the same
or a later slot date and have not started, ordered by start (a detached lesson the office moved
may start before the requested one), each with its own late flag. The preview changes nothing; the
apply recomputes the list in one transaction: it locks the class (term → class → lesson), then its
lessons from the requested one's slot date on in the order of their IDs, and decides from the rows
read under the locks. A lesson moved into the past meanwhile is skipped, a booking made meanwhile is
canceled too; only the requested booking's own lesson having started fails. The mail lists the
canceled lessons by start. A booking of a one-off lesson, or of a lesson that has started, is a 422 on `path.bookingId`; a
canceled one a 409 `already-canceled`.

_Up to a date, and pausing a membership (2026-10-24)._ Both operations take an end date `until`
(the preview as query parameter, the apply in its body `{until?}`); only lessons whose slot date is
on or before it are canceled. Without it, it is the last day of the class's term, and the answer
names the effective `until`, so the UI can prefill it. An `until` before the requested lesson's slot
date is a 422 on `query.until` or `body.until`. For a person who is a member of the class, canceling
the following weeks is a **pause**: the membership stays, `class_members.paused_until` is set to
the later of `until` and a pause the member has already (under the class lock the apply holds),
and the answer carries that `pausedUntil` (the preview says it would). The pause's cancellations
do not release their dates (below), so ending a pause books them again. The checks run in the
same order in the preview and the apply: the booking first (one-off lesson, canceled, started),
then `until`. `domain.ClassMember.ActiveOn` is false for slot dates up to the pause, so
generation and enrollment skip them; lessons after it that exist keep their member bookings, and
those generated later book the member again. For anyone else (bookings of their own in a class
lesson), the same `until` applies without a pause, and the cancellations are ordinary ones that
release their dates. The office sees a pause in the class's member list while it lasts (a pause
that ended before today is left out of the list) and can end it (`updateClassMember`, below).

**Canceled lessons.** When the school cancels a lesson (ADR 0007), its bookings stay booked: they
record who was affected, and Phase 6 decides about passes and refunds. A member may still cancel
such a booking (never late). Booking a canceled lesson is a 409 `closed` with why `canceled`.

**Booking source.** `bookings.source` says how a booking came about: `self` (a member booked) or
`member` (a class member's automatic booking, written by the auto-enrolment of chunk B of Phase 4).
Bookings the office or an instructor make get a source of their own with the staff actions later in
Phase 4; until then no such source exists. Term shortening, a class's weekday move and retiring a class ignore
the automatic `member` bookings and delete them with the lessons they remove; only other bookings
still block those edits (ADR 0007, amendment).

**Class members.** Generation books the class members into each lesson it creates that has not
started, from their `since` date (on or before the lesson's slot date), as `member` bookings
without an actor, and ignores the booking cutoff; a lesson generated for a past date gets none.
Adding a member (`addClassMember`) books them into the class's lessons that have not started (by
their start, so a lesson the office moved from an earlier slot into the future too) and whose slot
date they are active on, whatever their detached state, those the school canceled included;
even beyond capacity, and the booking event then records an override (`override`).
Members win: generation records an override too if older data has more members than places. The
office account is the actor of these bookings. A member releases a single date by a normal
cancellation, which frees the place; taking it back is a normal booking (`self`, a new row) under
the booking cutoff while a place is free. **Released dates.** A cancellation by the person or their
family (`cancelBooking`, and `cancelFollowing` of a person who is not a member of the class) sets
`bookings.released`; the office's cancellations (ending or changing a membership) and a member's
pause do not. Neither generation nor enrollment books a person into a lesson they have an active
or a released booking for, so a released date stays released, while a date the office or a pause
canceled is booked again when the person is active on it again. Removing a class member (`removeClassMember`) cancels their active bookings of the
class's lessons that have not started, whatever their source (a taken-back date included), with
the office as the actor, never late and without a mail; bookings of lessons that have started stay.
A class refuses members beyond its capacity (409 `full`; a person who is a member already is still
a 409 `duplicate`), new members of a retired class (422 on `path.classId`), and a capacity below its
member count it lowers to (422 on `capacity`); a class with more members than places from older
data stays editable otherwise. Canceled lessons (not started) get the new member's booking like
the others, as their bookings stay (above). `updateClassMember` (office, `PUT
/api/v1/classes/{classId}/members/{personId}`, body `{since?, pausedUntil?}`; 422 on
`path.classId` for a retired class) replaces the dates of a membership, which ends a pause when
`pausedUntil` is left out. It cancels the member's active automatic bookings of lessons that have
not started and that they are no longer active on (a new pause, a later `since`), not released,
never late, without a mail, and then books them like a new member into the lessons they are now
active on. Both run under the
class lock (term → class → lesson), so membership and member count cannot change meanwhile. The
office's timetable edits delete lessons together with their member bookings (ADR 0007, amendment).

**History.** Every booking change adds a booking event with the new status, the time, the actor and
the `late` and `override` flags. The actor is the real account: the admin when impersonating (as
for `lessons.canceled_by`). System bookings (generation) have no actor.

**Mails.** The acting account (the account the session acts as, also when an admin impersonates it)
gets a "booked" mail (`booking-confirmed`) and a "canceled" mail (`booking-canceled`), in its
language (ADR 0008), naming the person, course type, school-local date and time, room and location;
a late cancellation says so. Canceling the following weeks sends one mail listing every canceled
booking. Automatic member bookings and their cancellation by the office send no mail.

**Availability.** `getSchedule` and `getLesson` carry per lesson `freePlaces` (capacity minus
active bookings, at least 0) and, for a signed-in caller, `bookedFor`: the caller and dependents
with an active booking (person, name, booking ID), the caller first. Both come from one query each
for the whole schedule (`Bookings().CountActiveByLessons`, `ListActiveByLessonsAndPersons`), not one
per lesson. Visitors who are not signed in get the free places only. Everyone also gets
`bookingClosesAt` (start − `BookingCutoff`) and `lateCancelFrom` (start − `CancellationCutoff`,
absent for a lesson the school canceled), computed from the settings read once per request.

**Lock order.** Term, then class, then lesson (generation and class edits already lock this way).
Whoever locks several lessons of a class holds the class lock first, so their order among
themselves cannot deadlock.

## Consequences

- One rule set for members and staff: the office books itself like a member and handles exceptions
  through the staff actions that come later in Phase 4 (beyond capacity, after the start, guests).
- Late cancellations are recorded but cost nothing yet; Phase 6 (passes) reads `late` to decide
  whether a late cancellation uses up a visit (it does; ADR 0012).
- Bookings of canceled lessons remain, so the office knows whom a cancellation affected.
- A full lesson answers `full`; the UI offers the waiting list once it exists.
- The availability costs extra queries per request, independent of the schedule's size: one for
  visitors (the counts), four for signed-in callers (the counts, the caller's person, their
  dependents and the family's bookings).

## Amendment (2026-10-24): staff actions

The office, and an instructor or accompanist on the lessons they teach (ADR 0007, amendment), manage
a lesson's participants. All operations are at the access level `staff` and check in the service
who may act on which lesson: the office and admins on every lesson; anyone else only on a lesson
whose instructor or accompanist is an **active** staff member linked to their person
(`staff.person_id`). Any other lesson, and a staff account without such a link, gets a 404, like a
stranger's family (ADR 0008).

**Booking source `staff`.** Bookings that staff make have the source `staff` (migration 00013 adds it
to the CHECK). The booking event names who: the real account, the admin when impersonating.

**Guests.** A booking has a person or a guest. A guest has a name and an optional email
(`bookings.guest_email` is nullable since 00013); only staff add guests. A guest with an email gets
the confirmation, the cancellation and the lesson mails (ADR 0007) in the school's default
language, greeted by name; one without an email gets nothing. A guest has no account to cancel
with, so their `booking-confirmed` asks them to contact the school to cancel (the settings hold no
contact data of the school yet, so the mail names the school only). Guests are not unique per lesson.

**Trial.** `bookings.trial` (since Phase 1) marks a trial lesson; `bookings.trial_note` (00013) is an
optional note for the instructor (at most 500 characters), and only a trial booking has one (a note
without the flag is a 422 on `trialNote`). No limits on trials yet.

**Participants** (`listLessonParticipants`, `GET /api/v1/lessons/{lessonId}/participants`). Every
booking of the lesson, canceled ones included, ordered by booking time, with the participant's name,
a guest marker, status, source, trial and note, the late flag and the times; and the lesson's
waiting list, ended entries included, in its order, with position and status (an invite whose expiry
has passed shows as `expired`), plus the capacity, the active bookings, the held places, the free
places and the waiting entries (`waiting`, counted by the service). _Contact data_ is kept simple: `contactEmail` is the head of the
person's mail chain (their confirmed account, else their own address, else their first guardian's
confirmed account; ADR 0007) or a guest's address, and `contactPhone` is the person's phone, else
the phone of their first guardian who has one (guardians ordered by name). A disabled account has
no `contactEmail`. It costs a fixed number of queries per lesson.

**Window.** Staff ignore the release window and the booking cutoff. Instructors and accompanists add,
mark trials, remove and take attendance **until the end of the lesson's school day** (midnight after
its start in the school time zone, or the lesson's end if that is later; `dayOverAt` in the list);
afterwards it is a 422 on the path (`path.lessonId` or `path.bookingId`), and the office makes the
change, which may at any time. Nobody adds to a lesson the school canceled (409 `closed` with why
`canceled`).

**Add** (`addLessonParticipant`, `POST /api/v1/lessons/{lessonId}/participants`, body `{personId |
guest {name, email?}, trial?, trialNote?, confirmOverCapacity?}`, 201). Under the lesson lock, after
settling the waiting list like `bookLesson` (ADR 0010): a person with an active booking is a 409
`already-booked`; a lesson whose active bookings and the places held by others' invites fill its
capacity is a 409 `full`, unless `confirmOverCapacity` is set, which the booking event records as
`override`. A person whose own invite holds a place takes it without confirming (the invite is
`accepted`, the event notes `waitlist`); any other open entry of the person is withdrawn. **One
capacity rule** (`fitOf`) serves `bookLesson`, this add and accepting an invite: the places others'
invites hold count as taken, the place the person's own invite holds is theirs. So an invited person
gets their reserved place even when the office has booked over capacity next to the invite since;
the booking event then records `override`, as the lesson goes beyond its capacity. Before the start, the person's
recipient chain (ADR 0010) or a guest with an email gets `booking-confirmed`; a walk-in after the
start gets no mail. Both or neither of `personId` and `guest` is a 422 on `body`, an unknown person a
422 on `body.personId`, a bad guest a 422 on `body.guest.name` or `body.guest.email`.

**Trial update** (`updateLessonParticipant`, `PUT /api/v1/bookings/{bookingId}/participant`, body
`{trial, trialNote?}`) sets the flag and the note of an active booking (a canceled one is a 409
`already-canceled`); it records no event, as the status does not change.

**Remove** (`removeLessonParticipant`, `POST /api/v1/bookings/{bookingId}/remove`) cancels through
the shared `cancelBooking`: never late, with the staff account as actor, and the freed place goes to
the waiting list. A person's booking is **released** (`bookings.released`, like the family's own
cancellation): staff took them off that date, so neither enrollment nor generation books a class
member into it again when the office later changes their membership; taking it back is an add. Also
after the start (a walk-in added by mistake), within the window, and on a lesson the school canceled
(to tidy it up; trials may be marked there too). Before the start of a lesson that takes place, the
participant's recipients get `booking-canceled`; after the start, or for a canceled lesson, nobody,
and a canceled lesson invites nobody from its list. A canceled booking is a 409 `already-canceled`.

**Attendance** (`setAttendance`, `POST /api/v1/bookings/{bookingId}/attendance`, body `{status:
attended | no_show | booked}`; `markAllAttended`, `POST /api/v1/lessons/{lessonId}/attendance/all`,
which sets every `booked` booking to `attended` and returns those it changed). From the lesson's
start (before it a 422 on the path), within the window; a lesson the school canceled is a 409
`closed` with why `canceled`, a canceled booking a 409 `already-canceled`. Every change adds a
booking event with the new status and the actor; setting the status a booking has changes nothing.
`booked` sets the attendance back to unknown. A no-show is only a status until Phase 6 (passes).

**History** (`listBookingEvents`, `GET /api/v1/admin/bookings/{bookingId}/events`, office only, so
under `/admin` like the office's other reads): every event
of a booking, oldest first, with status, `late`, `override`, `note` and the actor's account, name and
email (none for the system).

**Finding persons** (`searchPersonsForStaff`, `GET /api/v1/staff/persons?q=…`, access `staff`).
Instructors need to find the person they add, but not the office's person records: the search
matches the given and family name only (not email addresses), needs at least two characters, returns
at most 20, ordered by family and given name, and gives only the ID, the name and the birth year (to
tell namesakes apart), no contact data. The office keeps `listPersons`.

**Demo.** Felix Keysworth, the demo accompanist, has an instructor account (signing in by email, as
the login page lists Alina for instructors), and Alina's ballet workshop, which Felix accompanies,
has a guest with an email, one without and a trial with a note, added by Alina.

## Amendment (2026-10-24): instructor view

The `/teach` page lists the lessons a staff member teaches and lets them message their participants.

**Lessons** (`listTeachLessons`, `GET /api/v1/teach?from=&to=&staffId=`, access `staff`). The lessons
that start in a range of school dates: by default the week before today, today and the week after
(`from` = today − 7, `to` = `from` + 15, exclusive; a `from` alone gets the same length), at most 31
days. An instructor or accompanist gets the lessons of their **active** staff records linked to their
person, as instructor or accompanist (`Lessons().ListByStaff`, served by the indexes on
`(instructor_id, starts_at)` and `(accompanist_id, starts_at)`, migration 00014); without a staff
link the list is empty, not an error. The office gets every lesson, or with `staffId` one staff
member's; for an instructor `staffId` only narrows their own. Canceled and hidden lessons are
included. Each lesson has the schedule's summary fields plus the caller's `role` (`instructor`,
`accompanist`, or `office` for a lesson of others), `active`, `held`, `freePlaces`, `waiting`,
`trials`, `guests`, `dayOverAt`, `attendanceOpen` (the lesson takes place and has started, and for
instructors and accompanists its day is not over: the rule of `setAttendance`) and
`attendanceTaken` (it takes place, has started and has no active booking left `booked`; a lesson
without bookings counts as taken). The counts come from one grouped count of the active bookings
of all lessons (`Bookings().CountsByLessons`: active, trials, guests, still `booked`) and one read of
their open waiting-list entries, not per lesson.

**Notices** (`notifyParticipants`, `POST /api/v1/lessons/{lessonId}/notify`, body `{message}`, 200
`{recipients}`). Same access as the participant list (the office every lesson, an instructor or
accompanist their own, 404 otherwise). The message is plain text: line breaks (CR LF, CR, U+2028,
U+2029) are normalized to `\n` and the space around it is trimmed, then it must have 1 to 1000
characters, no control characters other than line breaks and tabs, and no invisible format
characters (Unicode `Cf`: bidi overrides and isolates, zero-width spaces, BOM) other than the
zero-width joiner and non-joiner that emoji sequences and some scripts need (a 422 on
`body.message`; the schema only caps the raw input at 4000 characters). It goes to the recipient chain of every **active** booking and to guests with an
email (ADR 0007, ADR 0010), each address once with the participants it is for, each in its
language (a guest in the school's), as the `lesson-notice` mail: school, lesson (course type, date
and time in the school zone, room, location, a line if the school canceled it), the sender's name
and the message, escaped in HTML with its line breaks kept. Notices about lessons that are over or
canceled are allowed: staff may want to share something after the class or explain a cancellation.

The **sender** is the account that sends, the admin while impersonating (like the actor of booking
events). The mail names it as the schedule does when one of its active staff records is the lesson's
instructor or accompanist (the staff display name), else by its person's name. Its address is the
mail's **Reply-To** (`outbox_mails.reply_to`, migration 00014; the SMTP
sender writes the header), so replies reach the staff member, not the school's no-reply address.

**Limits.** An account sends at most **5 notices in an hour** (a sliding hour), and a lesson gets at
most **3 notices a school day**, from anyone; beyond either it is a 429 problem with `Retry-After`
(the longer wait if both apply). The limits must survive restarts and hold for several server
processes, so they are not in-memory limiters (ADR 0006): each sent notice is recorded in
`lesson_notices` (lesson, sender, time, number of recipients), which the limits count. Counting the
outbox rows would count mails, not notices. A notice outlives its lesson (`lesson_id` becomes NULL
when the office deletes the lesson): it still counts for its sender's hour and records what was
sent. The lesson lock, then the sender's person lock, taken just before the count, serialize
concurrent notices, so neither limit can be overrun. A lesson nobody
is mailed for sends nothing and counts no notice. The notice records also say who sent what when;
the text itself is in the outbox mails.

**422 reasons** (applies to the staff actions above). The staff actions answer a change their time window refuses with a
422 on the path parameter (`path.lessonId` or `path.bookingId`), which by itself does not say
whether the lesson has not started or its day is over, and the UI should not guess from its own
clock. Like a 409 carries `reason` (the `Conflict` problem), these 422s are the `Invalid` problem
with a top-level `reason`: `not-started` (attendance before the start: `setAttendance`,
`markAllAttended`) or `day-over` (an instructor or accompanist after the lesson's school day:
`addLessonParticipant`, `updateLessonParticipant`, `removeLessonParticipant`, `setAttendance`,
`markAllAttended`). The `errors` list keeps the location. A 422 about the request body has no
`reason`. The client reads `problem.reason` of a 422 response, as it reads it of a 409.

## Amendment (2026-10-25): My lessons

A signed-in account sees the bookings and waiting-list entries of its family, the same persons it
books for: its own person and all its dependents, managed ones included. Both operations are at the
access level `signedIn`; a person outside the family never shows.

**Upcoming** (`listMyLessons`, `GET /api/v1/my/lessons`, `{timezone, now, upcoming, waitlist}`).
`upcoming` are the active bookings of lessons that are not over (`endsAt > now`, so a lesson in
progress is still listed), by the lessons' start, then by booking time. Each has the booking (as
`bookLesson` returns it), the person's name, the lesson's summary as in the schedule (status
`canceled` for a lesson the school canceled, whose booking stays booked), `lateCancelFrom` and
`canCancel` (the booking is active and its lesson has not started: the rule of `cancelBooking`).
Canceled bookings of lessons ahead are not listed; the history has them once their lessons are over.
`waitlist` are the open entries of lessons that have not started (the job ends them at the start),
by the lessons' start and then the caller first, each with the entry as in the schedule's
`waitlist` (status, `expired` for an invite past its expiry, `position`, `expiresAt`,
`stayOnList`), the lesson's summary and `bookingClosesAt`.

**History** (`listMyLessonHistory`, `GET /api/v1/my/lessons/history?before=`, `{timezone, now,
before, items, nextBefore, hasMore}`). The bookings of every status (booked when nobody took
attendance, attended, no-show, canceled with its `late` flag; a lesson the school canceled by its
status) of lessons that are over (`endsAt <= now`) and started in `[nextBefore, before)`, the
newest first. A page is **three calendar months** in the school time zone: `before` defaults to now
(a later one is now), `nextBefore` is three months earlier and the `before` of the next page, and
`hasMore` says whether any older booking exists (one query with limit 1). Pages have a fixed length
rather than a fixed number of items, so a page may be empty while `hasMore` is true; three months of
a family's bookings are few enough to send at once.

**Visibility.** A booking is listed whatever its lesson's visibility: a hidden or unreleased lesson
the office booked the family into is still theirs, although the schedule does not show it to them.

**Cost.** A fixed number of queries per request, whatever the number of bookings: the family (two),
the bookings by persons and lesson times (`Bookings().ListByPersons`, a join on the lessons, served by
`bookings_person_idx`; the upcoming list asks for the active ones only), the lessons by ID
(`Lessons().ListByIDs`) and their references (the catalog, and only the classes they belong to,
`Classes().ListByIDs`); for the upcoming list also the family's open entries
(`Waitlist().ListOpenByPersons`) and the open entries of their lessons for the positions. The
bookings come by lesson start, then lesson, so a family's bookings of one lesson stay together.

## Amendment (2026-10-25): passes

ADR 0012 decides what bookings cost: class members' automatic bookings, trials, bookings of lessons
the school canceled, timely cancellations and staff removals never use a pass; late cancellations
and no-shows do. Every booking change in this ADR settles its pass in the same transaction, after the
lesson lock (term → class → lesson → passes). The participant list and My lessons carry each
booking's `payment`, which costs up to three more queries per request.
