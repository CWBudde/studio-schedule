# 0012 — Passes and receipts

- Status: accepted (passes and receipts implemented)
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

### Receipts

**What a receipt is.** A payment confirmation for a pass sale (a drop-in is a single-visit pass),
recording the payment method (`cash`, `transfer`, `card`). The app is not a cash register: it keeps
no till, has no TSE, and the office records what was paid. Not legal advice; the school checks its
obligations. Assignments (`assignPass`) get no receipt.

**Issued with the sale** (`PassService.Sell` → `ReceiptService.issueSale`, one transaction): the
next number of the year from `receipt_counters` (`INSERT … ON CONFLICT DO UPDATE … RETURNING`, which
locks the year's row until the transaction ends, so numbers are gapless and a rollback gives the
number back), `PREFIX-YYYY-NNNN` with the year of the issue date in the school's time zone; one line
(the pass type, the owner, the validity) for the price paid; the receipt is stored, then the PDF is
rendered and stored, then the mail is queued, then the pass is created with `receipt_id` (a foreign
key, unique). Any failure (an invalid recipient, a render error) rolls back the sale and the number.
`sellPass` takes `paymentMethod` (default `cash`) and optionally `recipientName` (default: the
payer's name), `recipientAddress` and `recipientEmail`. The receipt keeps what it prints or uses:
the typed email only if the payer has no account (a payer with one gets the receipt there, and a
typed address is dropped). The owner and the payer are locked for the sale, so neither is deleted
before the pass refers to them; any other conflict of the store passes through. For the form,
`getReceiptSettings` reports `logoMaxBytes` and, in the standard tax mode in euros,
`addressRequiredAbove` (25000: the German small-amount invoice limit of 250 €, § 33 UStDV, above
which a receipt should name the recipient's address; a hint, not enforced).

**Every sale has a receipt**, so incomplete receipt settings refuse the sale: a 409
`receipt-settings-incomplete` that names what is missing. Complete means the issuer's name, address
and tax number or VAT ID, a tax mode, and in the exempt mode the note with its legal basis.

**Tax** (`domain.NewReceiptLine`). The school's tax mode, snapshotted on the receipt:

- `standard`: the price is gross and includes VAT at the pass type's rate; net = gross × 10000 /
  (10000 + rate), rounded to the minor unit half away from zero, **per line**, VAT = gross − net.
  The PDF prints net and VAT per rate and the gross total. A negated line rounds to the negated
  amounts, so a cancellation mirrors its original exactly. `Receipt.Validate` recomputes every line.
- `small_business` (§ 19 UStG): no VAT (rate 0 on the line), the default note or the school's.
- `exempt`: no VAT, the school's note naming the legal basis (required).

**Immutable.** `ReceiptRepo` has no update or delete; in PostgreSQL triggers reject `UPDATE`,
`DELETE` and `TRUNCATE` on `receipts`, `receipt_lines` and `receipt_pdfs`. The receipt snapshots the
issuer (name, address, tax number, email, phone), the tax mode and note, the footer, the language,
the payer and recipient, the currency and the amounts; the logo is snapshotted in the PDF.

**The PDF** is the stored original (`receipt_pdfs`, apart from the receipt so lists do not read it):
downloads and mails read it, never re-render it. It is rendered by [typst](https://typst.app)
compiled to a WebAssembly reactor (`tools/receipt-pdf`: the template `receipt.typ`, Libertinus
Serif, no file or network access, a fixed "today", the document ID from the receipt), from JSON that
`service/receipt_pdf.go` builds with every text preformatted. Receipts are printed **in the school's
default language** at issuance (German or English), because they are the school's records, not the
recipient's; the mail around it is in the recipient's language. **The renderer is a swappable
adapter** like the store and the mailer (`service.ReceiptRenderer`): the server runs the module with
wazero (`internal/pdf/typstwasm`: compiled once in the background at startup, cached in the user's
cache directory, a small pool of instances since an instance serves one render at a time), the web
demo runs the same module in the Worker's WebAssembly (`internal/wasmbridge`, ADR 0001; loaded on
the first render). Both give the same bytes for the same input (random numbers are zeros; a golden
checksum in Go and a Node smoke test of the Worker's shim compare them). Receipts without a PDF (the
seed's, which does not render) get it rendered on their first read, outside any transaction (in the
demo the first render waits for the module's download, which must not hold the store), and stored
once; a request that stored it meanwhile stored the same bytes. Amounts are formatted by the
currency's ISO 4217 exponent (`domain.CurrencyExponent`; the settings refuse unknown codes), and
the wording a seeded receipt shares with an issued one (the language, dates, the pass line) is in
`internal/receiptfmt`. The demo's WASI shim mirrors wazero's defaults: zero random bytes and fake
clocks (the wall clock from 2022-01-01, both advancing a millisecond per reading).

**The module is built reproducibly** (`just typst-wasm`): Rust 1.99.1 in a pinned linux/amd64
container image, `Cargo.lock`, paths remapped out of the binary, typst's commit stamp fixed (the
host matters: cargo mixes the compiler's host into symbol hashes, which changes what LTO makes of
the code). It is committed with Git LFS (`internal/pdf/typstwasm/receipt.wasm`, about 24 MB) with
its sha256 pinned next to it; CI rebuilds it and fails on a mismatch (`test-typst`), and every
workflow pulls the LFS files through the Actions cache.

**Storno** (`cancelReceipt`, `POST /api/v1/admin/receipts/{id}/cancel`, office, optional `reason`):
a cancellation receipt in the same series that negates every line and total, references the
original, keeps its tax mode and note, payer, recipient and payment method (the refund goes the same
way), takes the current issuer, and is mailed like the receipt. It voids the pass if the pass paid
for no visit (under the lock of the owner's passes); a pass that paid for visits is a 409
`pass-used` (the office voids the pass separately, `voidPass`); a receipt canceled before is a 409
`already-canceled` (`UNIQUE (cancels_receipt_id)` backs it), a cancellation a 409 `cancellation`.

**Delivery.** The mail (`receipt` template) goes to the payer's account in its language, else to
the `recipientEmail` typed at the sale in the school's language, else nowhere. The mail is in one
language: it describes the pass in the recipient's (a cancellation mail names the item only in the
receipt's own language). The outbox mail refers
to the receipt (`outbox_mails.receipt_id`) instead of copying the PDF; the `deliver-mail` job
attaches the stored PDF (`multipart/mixed`, base64), the demo mailbox lists the attachment and
serves it (`getDemoMailAttachment`).

**Reads.** The office: `searchReceipts` (year, a search in the number and the recipient's name,
paging, newest first, with the total; a POST with the filter in the body, so that names stay out
of URLs), `getReceipt` (with the cancellation or the canceled receipt,
the pass, and `cancelable`), `getReceiptPdf` (the PDF as an attachment named after the number). The list also returns `years`,
the years with receipts (from the counters), for its year filter. A list costs a fixed number of
queries.
Members: `listMyReceipts` and `getMyReceiptPdf`, the receipts **they paid** (the payer is the owner
unless someone else paid; a child's pass should be sold with the parent as payer); anyone else's is a 404.

**Settings.** `getReceiptSettings` (office: whether they are complete and what is missing) and
`updateReceiptSettings` (admin): issuer name, address, tax number, email, phone, tax mode, tax note,
footer and the number prefix; the logo apart (`updateReceiptLogo` with base64 data, PNG or JPEG of
at most 512 KiB whose bytes match the type, `deleteReceiptLogo`, `getReceiptLogo`), stored in
`settings.logo` and read only for rendering. Phase 7 folds the form into the settings UI.

**Demo.** The fictional issuer "Studio Demo Dance School" with a fictional address and tax number,
standard VAT at 19 %. The seed issues receipts for the four sold passes in the order they were sold,
paid by card, transfer and cash, and cancels Vera's single visit the day after its sale, which voids
it.

## Consequences

- Whether a booking is paid is always the current state of its redemption; the rules are one
  function, and the ledger is the only writer, so a new write path settles by construction.
- Changing the redemption mode is not retroactive; a booking settles by the new rule only on its
  next transition (e.g. attendance set back to unknown refunds a visit redeemed on booking).
- A lesson moved to another date keeps a redemption only from a pass valid on the new date.
- Unpaid bookings need the office: there is no automatic payment by a pass sold later.
- Personal data: passes and their uses belong to the person; Phase 8's export includes them, and
  deletion must keep what receipts reference (a payer of a receipt cannot be deleted; the receipt
  keeps the name and address it printed).
- A sale needs complete receipt settings and a working renderer; the server compiles the 24 MB
  module at startup (a few seconds, cached across restarts), the demo downloads it on the first
  receipt.
- Changing the template or typst means `just typst-wasm` and new golden checksums; receipts issued
  before keep their stored PDFs.

## Amendment (2026-10-25): erasure leaves receipts alone

Anonymizing a person ([ADR 0016](0016-privacy-and-account-administration.md)) does not touch its
receipts: they stay immutable with their snapshot of the recipient, for the retention duty of
receipts; its passes and redemptions stay as well. An account that created receipts is not deleted
(`receipts.created_by` refuses it) and is disabled instead.

## Amendment (2026-11-01): event fees

Events ([ADR 0019](0019-events-and-workshops.md)) are paid at the desk. When the office marks a
registration paid, that issues a receipt in the same transaction and series as a pass sale. The
receipt uses:

- the same counter and `PREFIX-YYYY-NNNN` number;
- the school's tax mode, issuer and note, snapshotted on the receipt;
- the payment method and the PDF;
- the mail, sent to the person's recipients, or to a guest's email in the school's language.

Its one line names the event and its dates, for the price snapshotted on the registration (gross
and VAT rate). The registration refers to the receipt (unique), as a pass does. A guest's
registration has no payer person. Its receipt keeps the guest's name and email as the recipient,
which `RecipientName` and `RecipientEmail` already snapshot.

A Storno of an event receipt (`cancelReceipt`, or the refund when a registration or event is
canceled) marks the registration `refunded`. It is refused with a 409 `event-attended` once the
event's first session has started, unless the office cancels the event or the registration with a
refund. The other Storno rules stay as they are: the same series, the negated lines, and
`already-canceled`.

As built (2026-11-01): the receipt mail goes to the payer as for a pass sale, to the payer's
account or else the address typed at the desk, not along the person's recipient chain; a guest's
goes to the guest's email. A session the school canceled does not count as started for
`event-attended`.
