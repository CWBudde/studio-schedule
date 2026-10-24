# 0010 — Waiting list

- Status: accepted
- Date: 2026-10-24

## Context

ADR 0009 answers a full lesson with a 409 `full` and leaves a spot in `cancelBooking` where a freed
place would go to a waiting list. `domain.WaitlistEntry` has existed since Phase 1 with its
statuses, but without a table, a repository or a setting. The previous system notified everyone
interested when a place freed, so the fastest click won and the others were disappointed. Tokens
sent by mail follow ADR 0004 (32 random bytes, only their SHA-256 stored), mails go out through the
outbox in the causing transaction, in the recipient's language (ADR 0008), and background jobs run
in both targets (`internal/jobs`, `app.Jobs`).

## Decision

**Which lessons.** Every lesson a member may book (ADR 0009, member rules) has a waiting list, but
only while it is full: its active bookings and the places held by invites (below) fill its capacity.
Lists are unlimited, and a member sees their place in it. A person is on a lesson's list at most
once at a time (`waitlist_entries_one_open_key`); ended entries stay as history.

**Join** (`joinWaitlist`, `POST /api/v1/lessons/{lessonId}/waitlist`, body `{personId,
stayOnList?}`, 201 with the entry and its `position`). For the caller's own person or a dependent
(404 otherwise, as for booking), under the lesson lock, in this order: the lesson must be bookable
under member rules (404, or 409 `closed` with `why`), the person not booked (409 `already-booked`)
and not on the list (409 `already-waiting`), and the lesson full (409 `not-full`: book instead). The
entry records the real account that added it (`joined_by`, the admin when impersonating). No mail.

**Order.** Entries are invited in `joined_at` order, then by ID. `position` counts the waiting
entries up to and including an entry (1 is the next to be invited); invited entries have none.

**Invites.** Whenever a place frees, the oldest waiting entry is invited, one per free place: the
capacity minus the active bookings minus the places held by invites. An invite is a fresh random
token (ADR 0004) whose hash the entry stores, valid for the setting `waitlist_invite_expiry`
(default 12 hours, between a minute and 30 days) but never beyond the booking cutoff
(`bookingClosesAt`). It is mailed (`waitlist-invite`) to the person's recipients, the chain of the
lesson mails (ADR 0007: their confirmed account, else their own address, else their guardians'
confirmed accounts), each in their language, with the link `<app URL>waitlist/accept?token=…` and
the expiry in school time. A person whom that chain reaches nobody is passed over and keeps
waiting: an invite nobody learns of would only block the place. No invites go out for a lesson that
the school canceled, that members may not see (hidden), or from its booking cutoff on.

Places are offered (`waitlister.offer`, one helper, in the transaction and under the lesson lock of
what freed them) after every cancellation (`cancelBooking`: a member's own, canceling the following
weeks, the office ending or changing a membership), every lesson edit (a higher capacity, a lesson
visible again or moved so that booking is open again), a class edit that raises the capacity of the
future lessons that follow it, a lesson's restore, an invite that is declined or ends (withdrawn,
expired), and a person's removal. Booking and joining free no place, but they first settle the
lesson's list (below): an invite that ran out goes to the next waiting entry, not to whoever books
or joins first, and that settling is kept even when the booking or the join is then refused.

**The reserve.** An open invite holds its place until it expires: `bookLesson` is a 409 `full` when
the active bookings and the held places fill the capacity, `freePlaces` in the schedule and the
lesson counts them, and a lesson edit or a class edit that lowers the capacity below the active
bookings and the held places is a 422 on `capacity` (one that keeps or raises it stays possible, as
for a lesson booked beyond its capacity by members). Only the invited person may take the
place: their own invite does not count against them, so booking them (`bookLesson`) takes it like
accepting. An invite whose expiry has passed holds nothing, even before the job ends it. Waiting
entries hold nothing either: a place that no invite holds may be booked directly (after the invite
of a lone stay-on-list entry ran out, say). Class members win as before (ADR 0009): enrollment books
them beyond the capacity and consumes no invite; a place someone else's invite holds counts as
taken, so such a booking records an override. Accepting does not check the capacity again: the
invite reserved the place.

**Accepting.** The link opens a confirm page: `getWaitlistInvite` (`GET
/api/v1/waitlist-invites/{token}`) shows the lesson, the person's given name, the expiry, the
invite's `state` (`invited`, `accepted`, `expired`, `withdrawn`) and `closed` (why the lesson
cannot be booked any more, if so), and changes nothing, so mail scanners that open links take no
place. `acceptWaitlistInviteByToken` (`POST` on the same path) takes it. Both need no sign-in (the
token is the proof) and are rate-limited per client IP (20 in 15 minutes, `RateLimits.InviteLinks`;
ADR 0006). Signed-in members accept in the app too (`acceptWaitlistInvite`, `POST
/api/v1/waitlist/{entryId}/accept`, family only). Both book the person under the lesson lock:
source `self`, the booking event noting `waitlist`; the entry becomes `accepted`. In the app the
event records the real account and the acting account gets the `booking-confirmed` mail, as for
`bookLesson`; by link the event has no actor (nobody signed in) and the confirmation goes to the
person's recipients, like the invite. The checks: a waiting entry is a 409 `not-invited`, an
invite that ran out (or was declined or voided, or whose lesson started) a 409 `invite-expired`, one
accepted already or whose entry left the list a 409 `invite-used`, and a lesson that the school
canceled, that started, whose booking closed or that was hidden meanwhile a 409 `closed` with `why`.
Single use comes from the status: an ended entry keeps its token hash, so a second click learns
what happened instead of a bare 404. An entry that goes back to waiting keeps its old link's hash
until a new invite replaces it: the link then shows `state` `waiting` with the `position`, and accepting by it is a 409
`invite-expired`. A token that finds nothing (unknown, or replaced by a newer invite) is a 404.

**Declining, expiry, staying on the list.** `declineWaitlistInvite` (`POST
/api/v1/waitlist/{entryId}/decline`) ends an invite early. An entry that ends its invite this way
or by expiry goes back to the end of the list (status `waiting`, a new `joined_at`, the invite's
window cleared, its link's hash kept) if it was joined with `stayOnList` and booking is still open; otherwise it expires. Either
way the place goes to the next waiting entry, never back to the same entry in that round. An expiry
mails `waitlist-expired` to the recipients (saying whether the person stays on the list); a decline
mails nothing, as the person did it themselves.

**Leaving** (`leaveWaitlist`, `DELETE /api/v1/waitlist/{entryId}`, 204) withdraws an open entry of
the family (404 otherwise); a place its invite held goes to the next. An entry that ended is a 409
`not-waiting`.

**Booked otherwise.** When a person on a lesson's list gets booked another way (`bookLesson`, class
enrollment, later the staff actions), their entry ends in the same transaction: an invite that holds
a place is `accepted` (the booking took that place), a waiting entry `withdrawn`.

**The job and the lesson's start.** The job `waitlist-expiry` runs every minute (and once at
startup) in the server and the WASM demo; it needs no mailer, since mails only go to the outbox. It
lists the lessons with an invite whose expiry has passed or that have started with open entries
(`Waitlist().ListDueLessons`) and settles each in a transaction of its own, under its lock, so a
lesson that fails neither undoes nor holds up the others (the job reports their errors joined): from the start on,
every open entry expires, without a mail (the lesson is over for them); before it, due invites end
as above and their places are offered. A second run at the same time changes nothing. `bookLesson` and `joinWaitlist` settle the lesson
the same way before they count its places. Reads and accepts do not: an invite past its expiry holds
no place, shows as `expired` in the schedule, and cannot be accepted (an accept takes only its own
place, so it cannot jump the queue).

**A canceled lesson** keeps its waiting entries and voids its invites: they go back to `waiting`
with their old `joined_at` (first in line again, it was not their fault), their links showing
them waiting, without a mail; the bookings' `lesson-canceled` mail is ADR 0007's. Restoring the lesson
offers its free places.

**Removing persons and lessons.** Entries go with their lesson and their person (`ON DELETE
CASCADE`; the memory store does the same). Before a person is deleted (a dependent removed without
another guardian, ADR 0008, or the office's `deletePerson`), their open entries are withdrawn under
the lessons' locks, so the places their invites held go to the next; if the deletion then fails
(bookings), nothing changes. A dependent who stays (another guardian, or managed) keeps their
entries: someone still acts for them. Lessons with entries are only deleted by the office's
timetable edits (with their member bookings, ADR 0009); their entries go without a mail.

**Availability.** `getSchedule` and `getLesson` count the held places in `freePlaces` and give a
signed-in caller `waitlist`: the caller and dependents on the list (person, name, entry, status
`waiting`/`invited`/`expired`, `position`, `expiresAt`, `stayOnList`), the caller first. One more
query for the whole schedule (`Waitlist().ListOpenByLessons`) serves both.

**Conflict reasons.** New: `not-full`, `already-waiting`, `not-invited`, `invite-expired`,
`invite-used`, `not-waiting`; `full` and `closed` as in ADR 0009.

**Demo.** The seed has a small tap workshop 10 days after the visit, booked up by four members, with
two others on its list (one staying on it), none of the demo logins' families, so the demo shows a
full lesson and its list. The demo mailbox shows the invites; their links open the app's confirm
page.

**Lock order.** As in ADR 0009: term, then class, then lesson; a person's lock comes before the
lessons whose entries their removal withdraws.

## Consequences

- A freed place goes to exactly one person at a time; nobody races for it.
- A stay-on-list entry can be invited again and again for as long as places free and booking is
  open; one that never answers costs each place its expiry time, at most until the cutoff.
- An invite costs the place up to the expiry setting: a short setting fills lessons faster, a long
  one gives families time to read their mail.
- Persons without any reachable address are never invited; the office sees them on the list later
  (staff actions) and can book them directly.
- The schedule needs one more query per request; the job one query a minute while nothing is due.

## Amendment (2026-10-24): overbooked lessons

Staff may book beyond the capacity next to an open invite (ADR 0009, amendment: `confirmOverCapacity`).
The invite still reserves its place: booking the invited person (`bookLesson`, or staff adding them)
and accepting the invite take it without a `full`, by the one capacity rule of ADR 0009 (`fitOf`), and
because the lesson then goes beyond its capacity, the booking event records `override`. Accepting
still never refuses for capacity.
