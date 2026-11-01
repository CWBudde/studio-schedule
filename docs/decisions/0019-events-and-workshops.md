# 0019 — Events and workshops

- Status: accepted
- Date: 2026-11-01
- Amends: [0012](0012-passes-and-receipts.md) (receipts cover event fees),
  [0009](0009-booking-rules.md) (an event's sessions are not booked one by one)

## Context

A workshop today is a one-off lesson of kind `workshop` (ADR 0007). Like every lesson it is booked
one by one, any pass pays for it (ADR 0012), and it ignores the release window (ADR 0003). Schools
also run intensives, holiday courses and guest-teacher weekends: several dates sold as one thing,
at their own price, with a receipt of their own, apart from the weekly timetable and its passes.
Payment happens at the desk; the app takes no online payments. Phase 10 adds events. The eight
open questions of the plan were each asked and answered before this record.

## Decision

### An event groups sessions

`domain.Event` has a typed ID, a slug (unique, lowercase letters, digits and hyphens, used in
`/events/$slug`), a title, a description, a course type, a capacity (at least 1), a gross price
(`domain.Money`, 0 for a free event) with a VAT rate in basis points (as for pass types: 1900 =
19 %), a status, a sign-up window, a cancellation deadline and an optional image (stored and served
like the school's background image, ADR 0018).

Its **sessions** are lessons of kind `workshop` with the new optional `Lesson.EventID`. They stay
lessons, so the schedule, calendar feeds (ADR 0011), attendance, the room overlap check and the
announcement audiences keep working on them unchanged. An event stores no instructor and no
location of its own: those are its sessions' instructors, accompanists and rooms. An event without
sessions cannot be published.

A session belongs to its event: it is not booked one by one. `bookLesson` and staff booking refuse
it with a 409 `event-session`, and it has no waiting list of its own. Its participants are the
event's registered persons and guests. Existing one-off `workshop` lessons without an event stay
what they are: booked one by one and paid by passes. Nothing migrates; the demo seed adds two or
three events next to its workshops.

### Registration is for the whole event

`domain.EventRegistration` names the event and either a person or a guest (a name and an email,
exactly one of the two). It has:

- **status:** `waiting`, `invited`, `registered` or `canceled`;
- **payment:** `unpaid`, `paid` or `refunded`;
- **price snapshot:** the gross price and VAT rate when the registration was created; a later price
  change does not touch existing registrations;
- the receipt and its Storno;
- an invite's token hash and expiry, in the shape of ADR 0010;
- the account that registered it, and the times.

A person has at most one open registration (`waiting`, `invited` or `registered`) per event.

**Who registers.**

- Signed-in members register themselves and their dependents (the family rules of ADR 0008; a 404
  for anyone else's person).
- The office registers a person or adds a guest at the desk, as it adds guest bookings to lessons
  today (ADR 0009).
- There is no public sign-up form for guests.

**Free events** are `paid` from the start with no receipt, because there is nothing to pay.

### Capacity and the waiting list

**Capacity.** One capacity per event, checked under the event's lock (`Events().Lock`: `FOR UPDATE`
on PostgreSQL, a mutex per event in memory). The lock order is event → its lessons → the persons'
passes, before any lesson lock. The event is full when its registered places plus its open invites
fill the capacity.

**Waiting list.** A full event answers a member with a 409 `full` and offers the waiting list. The
waiting list is the event's registrations in `waiting` and `invited`, and follows ADR 0010:

- order by joining time, then by ID;
- one invite per freed place, a mailed token valid for `waitlist_invite_expiry` but never beyond the
  sign-up close;
- an open invite holds its place;
- an invite that runs out goes to the next person waiting.

Accepting the invite turns the registration into `registered`. The office may register beyond the
capacity with the same explicit confirmation as for lessons (ADR 0009, amendment).

### Release and the sign-up window

**Status.** An event is `draft` (only the office sees it), `published` (public in `listEvents` and
`getEvent`) or `canceled`. Events ignore the timetable's release window, as workshops do (ADR 0003):
publishing shows the event and its sessions at once.

**Sign-up window.** Registering is possible between `signupOpensAt` (optional: from publishing on)
and `signupClosesAt` (optional: the first session's start). Outside the window a member gets a 409
`closed`. The office can still register.

### Payment and receipts

The app takes no online payment. A paid registration starts `unpaid`, and the office marks it paid
at the desk with a payment method. **Marking it paid issues the receipt in the same transaction**,
through `ReceiptService` (ADR 0012, amendment "event fees"). A failure rolls back both the payment
and the receipt number.

### Cancellation

**The deadline.** Each event has a cancellation deadline, `cancelUntil` (optional: the sign-up
close).

- **Before the deadline**, a member cancels their own or a dependent's registration. A paid one is
  refunded by a Storno of its receipt and becomes `refunded`.
- **After the deadline**, a member gets a 409 `too-late`, and only the office cancels. It chooses
  whether to refund (Storno) or keep the payment.

Either way the freed place goes to the waiting list.

**An event the school cancels.** Every open registration is canceled, every paid receipt is
cancelled by Storno, its sessions are canceled like lessons (ADR 0007), and the registered persons
and guests are mailed, in the causing transaction.

### Data protection

A guest's name and email fall under ADR 0016's guest retention: 12 months after the event's last
session they become `Guest` without an email. Receipts keep their snapshot. A person's registrations
belong in their data export and stay, with the person, when it is anonymised.

### Left to the items that build it

Mail templates and their wording, the API shapes beyond the operation names in PLAN.md, the theme
slots, and how the lesson waiting list's code is shared with the events' are decided by the items
that build them and recorded here as amendments.

## Consequences

- An intensive is sold as one thing at one price and gets one receipt. Its dates still show in the
  schedule and in feeds like any lesson.
- No pass pays for an event, so a school that wants a pass-paid workshop keeps it a plain
  `workshop` lesson.
- The receipt series now holds pass sales and event fees. Both share the counter, the VAT rules and
  the Storno, so the tax adviser sees one series.
- A registration for a few dates of an intensive is not possible. The office registers the person
  for the whole event and settles the price by hand.
- Guests reach an event only through the office, so the public pages gain no unauthenticated write
  and no new abuse surface.

## Amendment (2026-11-01): the store

These choices were made while building the domain and both stores (migration 00031):

- **Slug.** Groups of lowercase ASCII letters and digits joined by single hyphens, at most 80
  characters.
- **Bounds.**
  - title: up to 200 characters; description: up to 10,000;
  - capacity: 1 to 10,000;
  - price: up to the pass price bound;
  - the course type is required.
- **Image.** It uses `domain.Background` (the same formats and 1 MiB) and lives in its own table,
  which is deleted with its event.
- **`Events().List` order.** By the first session's start (canceled sessions count), then events
  without sessions, then by ID.
- **Payment rules.**
  - A free registration is `paid` and never has a receipt.
  - A priced `paid` registration has a receipt, and an `unpaid` one has none.
  - `refunded` has the receipt and a different Storno receipt, and only `refunded` has a Storno.
- **Invites.** The invite fields are set exactly while a registration is `invited`. Accepting clears
  the token hash, so a second use of the link finds nothing.
- **Anonymized guests.** A guest named `Guest` without an email is valid: the one exception to the
  required guest email, so that ADR 0016's anonymization leaves a valid record.
- **`AnonymizeGuests`.** It counts canceled sessions towards an event's last session. An event
  without sessions keeps its guests.
- **`CountsByEvents(at)`.** It counts invites whose expiry is after the instant.
- **Restrictions.**
  - An event keeps its course type: `CourseTypeRepo.Delete` refuses it.
  - A registration keeps its person.
  - Priced events and registrations count towards the currency lock (`SettingsUsage.Prices`, ADR
    0013).
- **Receipts.** The schema keeps a receipt unique among registrations, but not between a pass and
  a registration. The payment service keeps those apart.

## Amendment (2026-11-01): office administration

These choices were made while building the office's administration (`service.EventService`,
`httpapi/event_admin.go`, migration 00032).

**Operations.** All are `office`.

- Under `/api/v1/admin/events`: `listAdminEvents`, `getAdminEvent`, `createEvent`, `updateEvent`,
  `deleteEvent`, `publishEvent`, `unpublishEvent` and `cancelEvent` (with an optional note).
- The image: `getAdminEventImage`, `putEventImage` and `deleteEventImage`, as for the school's
  background.
- Sessions and registrations: `addEventSession`, `listEventRegistrations` and
  `addEventRegistration`.
- Under `/api/v1/admin/event-registrations/{registrationId}`: `markEventRegistrationPaid` (`/pay`)
  and `officeCancelEventRegistration` (`/cancel`, with `refund`).

**Events.**

- A new event is a draft. Without a slug, the title gives one: lowercase, ä, ö, ü and ß spelled
  ae, oe, ue and ss, other accents dropped, every other run of characters one hyphen, cut at a
  hyphen to 80 characters. While another event has it, `-2`, `-3` and so on are tried (at most 50),
  each with a lookup by slug: a failed insert would end a PostgreSQL transaction. A title that
  gives no slug is a 422 on `slug`; a slug that is given and taken is a 409 `duplicate`.
- A new capacity goes to every session that has not started, canceled ones included, since they
  may be restored. A session's capacity is always its event's: `updateLesson` refuses another one
  (422 on `capacity`). A session is always public, since its event's status decides who sees it:
  `updateLesson` refuses another visibility (422 on `visibility`).
- Only lowering the capacity below the registrations and open invites is refused, so an event the
  office registered beyond its capacity stays editable.
- The course type of an event with sessions stays.
- Deleting is refused (`in-use`) once the event has a registration, canceled ones included, or a
  started session. It deletes the sessions with the event.
- Publishing needs a scheduled session (409 `no-sessions`) and a draft (409 `not-draft`). Taking an
  event back needs a published event (409 `not-published`) without open registrations (`in-use`).
- A draft takes no registrations (409 `closed`), so a draft never has open registrations. A
  canceled event takes neither registrations nor sessions (409 `closed`).

**Sessions.**

- `addEventSession` creates a public `workshop` lesson with the event's course type and capacity,
  under the checks of `createLesson`. Moving, canceling, restoring and deleting it are the lesson
  operations; deleting is refused (`in-use`) while the event has open registrations. Restoring a
  session of a canceled event is a 409 `closed`; it takes the event's lock first.
- A session of a draft event is staff's only: the schedule, `getLesson` and the schedule feed hide
  it from everyone else, with one read of the sessions' events.
- Booking a session, adding a participant to it and joining its waiting list are a 409
  `event-session`. The server has no `closed` reason for it: lessons carry `eventId`, so the web
  can tell a session.
- A clone never copies a session.
- The lesson mails of a session (moved, canceled, restored, a notice) go to the event's registered
  persons and guests, by the recipient chain of lessons.

**Cancellation and refunds.**

- `cancelEvent` cancels the sessions that have not started without their own mails, cancels every
  open registration, refunds every paid one by a Storno, and mails one `event-canceled` (EN/DE) per
  recipient, saying whether this cancellation refunded a fee. The refunds are in the same
  transaction, so an event with paid registrations is canceled only with complete receipt settings
  (409 `receipt-settings-incomplete` otherwise).
- `officeCancelEventRegistration` refunds only with `refund`; without it the payment stays.
- `cancelReceipt` on an event fee's receipt is a 409 `registration-open` while its registration is
  open: the office cancels the registration with a refund instead, which frees the place with the
  money. A registration canceled with its fee kept is refunded there, which marks it `refunded`.
  Once a scheduled session has started it is a 409 `event-attended`; canceling the registration or
  the event refunds even then.
- `markEventRegistrationPaid` is refused for a paid, free or refunded registration (409
  `already-paid`), a canceled one (409 `already-canceled`) and one on the waiting list (409
  `not-registered`). The payer defaults to the
  registered person; a guest pays for themself.

**Audit.** Four actions: `event-registration-added`, `event-registration-canceled` and
`event-registration-paid` target the registered person (nothing for a guest) and name the event
and the registration; the canceled one records `refund`, whether that cancellation refunded the fee. `event-canceled` has no target and names
the event.

**Receipt views.** A receipt names the event of its fee (`eventId`, `eventTitle`), a Storno the
event of the receipt it cancels. A receipt whose registration is open or whose event has started is
not `cancelable`.

**Not yet.** The event waiting list, which offers freed places (the Public API item), the
registration confirmation mails, and moving registrations when persons are merged (ADR 0015).
