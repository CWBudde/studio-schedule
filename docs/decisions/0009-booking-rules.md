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

**Canceled lessons.** When the school cancels a lesson (ADR 0007), its bookings stay booked: they
record who was affected, and Phase 6 decides about passes and refunds. A member may still cancel
such a booking (never late). Booking a canceled lesson is a 409 `closed` with why `canceled`.

**Booking source.** `bookings.source` says how a booking came about: `self` (a member booked) or
`member` (a class member's automatic booking, written by the auto-enrolment of chunk B of Phase 4).
Bookings the office or an instructor make get a source of their own with the staff actions later in
Phase 4; until then no such source exists. Term shortening, a class's weekday move and retiring a class ignore
and cancel the automatic `member` bookings; only other bookings still block those edits (ADR 0007).

**Class members.** Generation books the class members into each lesson it creates, from their
`since` date, as `member` bookings, and ignores the booking cutoff. Adding a member books the
future lessons that have not started, even beyond capacity; the booking event then records an
override. A member releases a single date by a normal cancellation, which frees the place; taking
it back is a normal booking under the booking cutoff while a place is free. Generation only books
persons without any booking for that lesson, canceled ones included, so a released date stays
released. Removing a class member cancels their future bookings of that class without a mail. A
class refuses members beyond its capacity (409 `full`) and a capacity below its member count (422).

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
  whether a late cancellation uses up a visit.
- Bookings of canceled lessons remain, so the office knows whom a cancellation affected.
- A full lesson answers `full`; the UI offers the waiting list once it exists.
- The availability costs extra queries per request, independent of the schedule's size: one for
  visitors (the counts), four for signed-in callers (the counts, the caller's person, their
  dependents and the family's bookings).
