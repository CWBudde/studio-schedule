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

## Amendment (2026-10-24): managed dependents and the office's persons view

**Managed dependents.** A dependent that has its own account (one the office created for a grown-up
child), is linked to a staff record, or is itself the guardian of someone (a young parent who is
still linked to their own parents) is a _managed dependent_: its owner or the office maintains its
data, not its guardians. The last case also keeps a guardian from deleting a person whose own
dependents would lose their guardian; the same read-only rule applies, so that "managed" means one
thing.

- A guardian sees it in the family with `managed: true`, read-only. Editing it is a 409 with the
  reason `managed`.
- Removing it only ends the caller's guardianship. The person is never deleted by a guardian, even
  when the caller was its last guardian. Before, the removal tried to delete the person and failed
  on the account, staff link or guardian link with a 409 that named only bookings and class
  memberships. Now only those two can refuse the removal of a dependent that is not managed.
- The 409 of a refused removal (bookings, class memberships) is now the `Conflict` problem with the
  reason `in-use` (ADR 0007), like the office's deletions.

**Office** (`office`, under `/api/v1/admin/persons`). Next to the search (`listPersons`), the office
sees a person with its guardians, dependents, account (email, role, status), staff links and class
memberships (`getPerson`), creates persons and edits every field including the email and its notes
(`createPerson`, `updatePerson`; a person's email is contact data, the account's email stays the
login name). It links a guardian to a dependent (`addGuardian`; an existing link is a 409
`duplicate`, an unknown person a 422 on `guardianId`) and removes the link (`removeGuardian`).
Removing the last guardian keeps the person: the office decides what happens to it.

- Guardian links never form a cycle. A guardian who is the person itself, or one of its dependents
  (directly or through other dependents), is a 422 on `guardianId`.
- A person deleted while the link is being added is a 404, not a `duplicate`.

**Person lock.** Checks of a person's links run under a row lock on the person (`Persons().Lock`,
`SELECT … FOR UPDATE` in ID order), like the lesson and term locks. `addGuardian` locks both persons
and every dependent its cycle check walks through, so two concurrent links that would close a cycle
together meet on a lock, and the second sees the first. A guardian's edit and removal of a
dependent, `deletePerson`, `createPersonAccount` and linking a staff member to a person lock the
person too, so nothing makes a dependent managed between the check and the write. A dependent's
removal can then only be refused for what is not managed: its 409 `in-use` says "e.g. bookings or
class memberships".

**Deleting persons** (`deletePerson`, 2026-10-24; replaces "the office does not delete persons").
The office deletes a person that nothing refers to, e.g. one created by mistake. While an account,
a staff link, bookings, class memberships or a guardian link in either direction refer to it, the
deletion is a 409 `in-use` and nothing changes. The office first removes what it can (guardian
links, class memberships); accounts and bookings keep the person for good. Merging duplicate
persons is still not planned.

**Accounts for existing persons** (`createPersonAccount`). The office creates an account with an
email and a role for a person who has none. Only an admin may create an admin account; for the
office that is a 403. A person with an account is a 409 `has-account`, an email that another
account has a 409 `duplicate`. The office is trusted, so it is told; ADR 0006's "same answer" rule covers the public
flows only. A concurrent request that takes the email first is a 409 `duplicate` as well (the
stores report a taken account email, a second account of a person, an existing guardian link and
an existing class membership as `duplicate`), and a person deleted meanwhile is a 404. The account starts unconfirmed and
without a password, so it cannot sign in, and a registration for its address leaves it alone
(ADR 0004). An invitation mail (`invite`, en/de) carries a `reset-password` link valid for 72 hours.
Setting the password with it confirms the account, since the link proves the address. A password
reset is therefore also open to such an invited account (unconfirmed, enabled, without a password):
when the invitation expires, "forgot password" sends a new link. An unconfirmed registration still
confirms with its own link.

**Booking for managed dependents** (decided in ADR 0009, 2026-10-24). A guardian books and cancels
for every dependent, managed ones included, e.g. a teenager with their own login, who can also
book for themself. `dependentOf` stays the only family check.

## Amendment (2026-10-25): member numbers, the import and merging

[ADR 0015](0015-import-duplicates-and-merge.md) replaces "merging duplicate persons is not planned":
the office merges a duplicate into another person, which moves everything but the receipts, and
keeps the duplicate as a hidden tombstone. A person also gets an optional, unique member number,
which the office edits and searches, and by which the member import finds members again; the import
links guardians it finds or creates to the members, and never removes a link. Every write of the
office's persons view refuses a tombstone as unknown (404), and a person something was merged into
cannot be deleted.
