# 0008 — Families, dependents and the account language

- Status: accepted
- Date: 2026-10-23

## Context

Parents book lessons for their children, and many children have no email address or are too young
for an account. The schema has had persons and guardian links (`person_guardians`) since Phase 1,
but nothing used them. Booking for others (Phase 4) needs a rule for whom an account may act for.
Mails that the server sends on its own, such as lesson cancellations or waitlist invites, have no
request whose language they could use. The previous system kept families as free text in the
member record, so the office booked children by hand.

## Decision

**Model.** Everyone is a person. A login is an account of exactly one person. A _dependent_ is a
person linked to one or more _guardians_ (persons) and usually has no account. A guardian's family
is their own person plus their dependents. Phase 4 lets an account book for exactly this family.
The check lives in the service (`service.dependentOf`) so that booking reuses it.

**What a guardian may do** (`signedIn`, `/api/v1/family`):

- See their own person data and their dependents. Each dependent comes with its other guardians,
  names only, read-only.
- Edit their own and their dependents' name, phone and birth date. The office's notes and a
  person's email stay untouched. A birth date must not lie after today (school time zone).
- Add a dependent: a new person with the caller as guardian.
- Remove a dependent. This ends the caller's guardianship. If no guardian is left, the person is
  deleted too. While bookings or class memberships still refer to it, the removal is refused with a
  409 and nothing changes: the office decides what happens to a child that is still enrolled.
- A person outside the caller's family is a 404, not a 403, so nobody can probe for persons by ID.
- All of this also works while an admin is signed in as the account (ADR 0005), so support can
  help with it. Only credentials stay refused while impersonating (ADR 0006).

**What a guardian may not do.** Link another guardian to a child: the second parent gets linked by
the office, in the admin persons view that comes with the admin area. An invitation flow by email
is not planned. A dependent who grows up gets an account for the existing person, created by the
office, so bookings and class memberships stay with that person. Merging duplicate persons is not
planned.

**Office.** The office sees and edits every person and every guardian link (admin area, Phase 3).

**Account language.** `accounts.locale` stores the language of the mails an account receives: a
mail template locale, or NULL for the school's default.

- Registration stores the UI language the request names, when a template matches it (`de-AT` is
  stored as `de`; an unmatched language stores nothing). A repeated pending registration replaces
  it.
- The account page changes it (`PUT /api/v1/auth/locale`), and `getMe` returns it.
- Every mail to an existing account is rendered in the account's language, else in the language of
  the request, else in the school's default. The confirmation and reset links, the email change and
  later server-initiated mails all follow this rule.
- The web app switches its UI to the account's language after a sign-in. The header's language
  switcher stays a browser-only setting.

## Consequences

- Booking for a child (Phase 4) needs no new permission model, only `dependentOf`.
- A family whose parents both have accounts needs the office once to link the second parent.
- A child cannot be removed by a parent while enrolled; the 409 tells the parent to ask the office.
- Server-initiated mails have a language without a request.
