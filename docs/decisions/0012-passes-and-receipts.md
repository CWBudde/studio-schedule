# 0012 — Passes and receipts

- Status: accepted (passes implemented; receipts follow in Phase 6 B)
- Date: 2026-10-25

## Context

Phase 6 adds prepaid passes and receipts. `domain.Pass`, `PassType` and `PassRedemption` have existed
since Phase 1 (visit passes only), `bookings.prepaid` was never written, and ADR 0009 left open what
a late cancellation, a no-show, a lesson the school canceled and a staff removal cost. The previous
system charged a "credit" with two statements, so the credit it logged could differ from the one it
charged, and derived receipt numbers from an auto-increment with a hard-coded year. The school decided
(2026-10-25): visit passes and time passes; redemption on booking or on attendance as a school
setting; class members pay their fee outside the app; a booking without a pass goes through unpaid;
late cancellations and no-shows cost a visit; a pass covers any lesson and belongs to one person,
whom someone else may pay for; receipts are payment confirmations for pass sales only.

## Decision

### Passes

**Pass types** (`pass_types`, office: `listPassTypes`, `createPassType`, `updatePassType`,
`deletePassType` under `/api/v1/admin/pass-types`). A type has a name (unique ignoring ASCII case, a
409 `duplicate`), a kind, a gross price in minor units, a VAT rate in basis points (1900 = 19 %, 0 for
none; the receipts' tax mode decides what is printed), and whether it is sold (`active`):

- `visits`: 1–1000 visits, valid `validityDays` days from the pass's first day, or without expiry
  for 0;
- `time`: any number of visits (one booking per lesson, as always) for `validityDays` days (≥ 1).

A pass copies what it needs from its type when it is sold (kind, visits, last day), so editing a
type affects only later sales. Retiring (`active: false`) stops the sale; a sold type cannot be
deleted (409 `in-use`).

**Passes** (`passes`) belong to one person. The office sells one (`sellPass`, `POST
/api/v1/admin/persons/{personId}/passes/sell`, body `{passTypeId, payerId?, validFrom?, price?,
note?}`) or gives one away (`assignPass`, `…/passes/assign`, `{passTypeId, validFrom?, note?}`,
price 0, no payer, no receipt). `validFrom` defaults to today in the school's zone; the last day is
`validFrom + validityDays − 1` (none for a visit pass without validity); the price defaults to the
type's, and `payerId` names who paid when it is not the owner (a parent for a child; the receipt
names the payer). Only an active type is sold (422 on `body.passTypeId`; an unknown payer on
`body.payerId`); an unknown person is a 404. The pass records who sold it (the real account, the
admin when impersonating). **A new pass pays for nothing booked before**: the office settles earlier
unpaid bookings at the desk. `voidPass` (`POST /api/v1/admin/passes/{passId}/void`) voids a pass at any
time: it pays for nothing more. What it paid for in lessons that have started stays paid; its bookings
of lessons ahead give their visits back and use another pass of the person that covers their dates,
if any (as after a move, below). It locks those lessons in ID order, then the owner's passes. A 409
`already-voided` the second time.
Passes are never deleted, and a person who owns or paid for one cannot be deleted.

**A pass covers a school date** when it is active, the date lies within its days and a visit pass
has a visit left (`domain.Pass.Covers`); it is **valid on** the date without the visit
(`domain.Pass.ValidOn`), which is what keeps a visit it paid for paid. The date of a lesson is the date of its start in the school
zone, so a lesson at 00:30 belongs to the next day even when it is still the previous day in UTC.

### Redemption

**When a booking costs a visit** (`charges.due`, one rule for every path):

| Booking                                                               | Costs a visit |
| --------------------------------------------------------------------- | ------------- |
| a class member's automatic booking (`member` source)                  | never         |
| a trial (`bookings.trial`)                                            | never         |
| any booking of a lesson the school canceled                           | never         |
| canceled late (after the cancellation cutoff)                         | yes           |
| canceled in time, or removed by staff (never late)                    | no            |
| active, redemption mode `booking`                                     | yes           |
| active, redemption mode `attendance`: attended or no-show             | yes           |
| active, redemption mode `attendance`: attendance not taken (`booked`) | not yet       |

Members pay their class fee outside the app, so their automatic bookings never use a pass. **Trials
are free**: a trial lesson is the school's offer to get to know it, and only staff mark one, so the
school decides. **Guests** own no pass; a guest booking that costs a visit is shown unpaid and paid at
the desk. The **redemption mode** is a school setting (`settings.redemption_mode`), default
`attendance`, as in the previous system: nothing is charged before the person came (or failed to come
without canceling in time). `booking` charges at booking time and gives the visit back on a timely
cancellation. The office reads it (`getPassSettings`, `GET /api/v1/admin/settings/passes`), an admin
sets it (`updatePassSettings`, `PUT`, `{redemptionMode}`); a change does not touch what was redeemed:
a booking follows the new mode the next time it changes. The receipt settings form of Phase 6 B and
the settings UI of Phase 7 take it up.

**Visits move only on transitions** (`charges.settle`, `internal/service/redemption.go`). A write
compares whether the booking was due before it with whether it is due after it, in the same
transaction under the lesson lock: becoming due uses a pass (if one covers the lesson's date),
stopping being due gives the redemption back, and everything else changes nothing. So writes that
change nothing (the same attendance, the same trial flag), a no-show after attended, a change of the
redemption mode and a pass bought later never touch an existing booking: a booking that was unpaid
when it became due stays unpaid until the office settles it at the desk.

**The ledger** (`internal/service/ledger.go`) is the one way to write a booking or update a lesson:
`createBooking`, `updateBookings` (several bookings of a lesson settle together, with one lock of
their persons' passes) and `updateLesson`. Every path goes through it: `bookLesson`, accepting a
waiting-list invite (in the app and by link), `addLessonParticipant`, `updateLessonParticipant`,
every cancellation (`cancelBooking`: the member's, cancel-following, the office's when a membership
ends or pauses, staff removing a participant), `setAttendance`, `markAllAttended`, `cancelLesson`,
`restoreLesson`, `updateLesson`, a class edit that moves its following lessons, and the automatic
member bookings of generation and enrolment (never due). `TestLedgerIsTheOnlyWriter` fails when code
of the service package writes bookings or updates lessons through the store directly.

**Moves.** When a lesson moves to another school date (`updateLesson`, or a class edit it follows),
every booking of it that a pass paid for keeps that pass only if it is valid on the new date;
otherwise the redemption goes back and another pass of the person that covers the new date pays, or
none (`charges.recheck`). Unpaid bookings stay unpaid.

The transitions:

- booking mode: booked → redeemed; canceled in time → refunded; canceled late → kept; the school
  cancels the lesson → refunded (the late cancellations of it too); restored → redeemed again (the
  late ones too: the late cancellation stands); staff remove a participant → refunded.
- attendance mode: attended or no-show → redeemed; back to `booked` → refunded; canceled late →
  redeemed; a removal after attendance → refunded.
- a trial set or cleared refunds or redeems accordingly.

**Which pass** (`pickPass`): of the person's passes that cover the lesson's date, time passes first
(no visit is spent while a time pass covers the day), then visit passes; among them the one that
expires first (none last), then the oldest sale, then the ID. A visit pass loses a visit; a refund
deletes the redemption and gives the visit back (to a voided or expired pass too, as a record). No
covering pass: the booking stays **unpaid**, and no later sale pays it automatically.

**Concurrency.** `Passes().LockByPersons` locks the passes of the persons concerned (`SELECT … FOR
UPDATE` in ID order), together, after the lesson lock: **term → class → lesson → passes of the
persons**. Nothing locks passes before a lesson (selling inserts, voiding locks its lessons first),
so the order holds; a transaction that settles several lessons one after the other (a class edit
moving its lessons) locks the passes of their persons per lesson, and the rare deadlock that this
can meet is retried like any other. Two bookings of the same person in different lessons, each
under its own lesson lock, meet at the pass lock, and only one gets the last visit. `pass_redemptions`
has `UNIQUE (booking_id)` as the backstop, `visits_remaining >= 0` a CHECK. The memory store serialises
everything anyway; `storetest` and a PostgreSQL test of the booking service (`TestRedeemLastVisitConcurrently`)
cover the lock.

**How a booking is paid** is derived, not stored (`bookings.prepaid` is dropped in migration 00016):
`payment` on the participant list (`listLessonParticipants`) and in My lessons (upcoming and history)
is `paid` (with the pass: its type's name, kind, visits left, last day), `unpaid`, `free` or
`pending` (attendance mode, attendance not taken). It costs up to three queries per list (the
redemptions of the bookings, their passes, the pass types), whatever the number of bookings. The
booking responses of `bookLesson` and `cancelBooking` do not carry it; the client reads it from My
lessons.

**My passes** (`listMyPasses`, `GET /api/v1/my/passes`, `signedIn`): the passes of the signed-in
person and their dependents, managed ones included (ADR 0008), with `usable` (covers today), the
visits left, the days, the payer, `useCount` and the latest ten bookings each paid for (by when it
paid; lesson summary, status, when), the latest lesson first; usable passes first, then the others,
newest sale first. The uses are bounded, so a time pass used for years stays cheap to list. The office's note
is left out; the office's list of a person's passes (`listPersonPasses`, `GET
/api/v1/admin/persons/{personId}/passes`) has it. Both cost a fixed number of queries.

**Demo.** The demo redeems on attendance. It sells Clara (the member login) a 10-visit pass and her
daughter Ella one that Clara paid, both used by drop-ins in the week before the visit that the office
marked attended; Kai a 30-day pass with a drop-in; Irene a single visit given away; and Vera a single
visit voided the day after it was sold by mistake.

### Receipts (Phase 6 B)

Decided, implemented in Phase 6 B:

- Receipts are **payment confirmations for pass sales** (a drop-in is a single-visit pass), recording
  the payment method (cash, transfer, card). The app is not a cash register (no TSE); the docs say so.
- Numbers `PREFIX-YYYY-NNNN`, gapless per year (the year of the issue date in school time), from a
  counter row locked in the issuing transaction; a cancellation (Storno) is in the same series and
  references the original. Receipts and lines are immutable (no update or delete in the repository, a
  PostgreSQL trigger rejects both).
- `sellPass` issues the receipt in its transaction and sets `passes.receipt_id` (the column exists;
  its foreign key comes with the table). Assignments get none.
- Tax: the school's tax mode (small business, exempt with a note, standard VAT) and the pass type's
  VAT rate; gross prices, net and VAT printed; the issuer block, tax data and logo are snapshotted
  per receipt.
- A Storno voids the pass if it is unused; a full Storno of a used pass is refused (the office voids
  the pass separately).
- The PDF is rendered at issuance (typst compiled to WebAssembly, run in-process, the same in the
  demo), stored as the immutable original and mailed with the outbox as an attachment to the payer.

## Consequences

- Whether a booking is paid is always the current state of its redemption; the rules are one
  function, and the ledger is the only writer, so a new write path settles by construction.
- Changing the redemption mode is not retroactive; a booking settles by the new rule only on its
  next transition (e.g. attendance set back to unknown refunds a visit redeemed on booking).
- A lesson moved to another date keeps a redemption only from a pass valid on the new date.
- Unpaid bookings need the office: there is no automatic payment by a pass sold later.
- Personal data: passes and their uses belong to the person; Phase 8's export includes them, and
  deletion must keep what receipts reference.
