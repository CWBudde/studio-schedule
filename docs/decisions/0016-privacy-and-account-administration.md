# 0016 — Privacy and account administration

- Status: accepted
- Date: 2026-10-25
- Amends: [ADR 0005](0005-rbac-and-impersonation.md),
  [ADR 0014](0014-announcements-and-mail-log.md), [ADR 0015](0015-import-duplicates-and-merge.md)

## Context

Phase 8 makes the app ready for a real school, which holds personal data of adults and of minors
and must answer the rights of the GDPR: a person may ask what the school stores about them (Art.
15, as a file they can take along, Art. 20) and to have it erased (Art. 17). Guests leave a name and
often an email on a booking and are never asked again. The audit log and the import history grow
without end. Until now only the database could disable an account (`accounts.disabled_at` existed
and was respected everywhere, but nothing set it), change a role or remove a login, and ADR 0015's
merge had to send the office to the database for two persons who both sign in.

What stands in the way of erasing:

- **Receipts** are immutable (ADR 0012): a trigger refuses every UPDATE and DELETE, and each
  receipt keeps a snapshot of the recipient's name and address. Tax law keeps them for ten years.
- **Bookings, passes and their redemptions** are the school's records of what was booked, paid and
  attended, and the statistics count them.
- **Accounts are referenced** by about a dozen tables, most of them `ON DELETE RESTRICT`.

## Decision

### Data export

`GET /api/v1/admin/persons/{personId}/export` (`office`) and `GET /api/v1/my/export?person={id}`
(`signedIn`) return one JSON document, which the web app saves as a file
(`data-export-<name>-<date>.json`) from the answer itself, so it works the same in the WebAssembly
demo, which has no server to download from.

- **Format**: `format: "studio-schedule.person-export"`, `version: 1` (it changes when a field
  changes its meaning or goes, not when one is added), `exportedAt`.
- **Contents**: the person with every field (the office's notes included: they are personal data
  of the person; a merged or anonymized person names that), its guardians, dependents and persons
  merged into it (ID and names only), its account (email, role, language, visible days, confirmed
  and disabled times; there is no creation time), the account's active calendar feeds (scope,
  location, created, last used) and the announcements it dismissed, its class memberships, every
  booking (canceled ones too) with its lesson's times and course type, every waiting-list entry
  (ended ones too), the passes it owns with their redemptions, and the receipts it or a person
  merged into it paid (metadata: number, kind, date, recipient name, total, whether canceled).
- **No secrets**: no password hash, session, link or calendar-feed token, and no receipt PDF.
- **Who**: the office exports any person, a tombstone (ADR 0015) or an anonymized person included.
  A member exports their own person and their dependents (`familyMember`, ADR 0008; anyone else is
  a 404). A dependent with an account of its own is exported by a guardian without what belongs to
  that account (account, feeds, dismissals): its holder exports those. Self-exports follow the
  survivor of a merge: the receipts of persons merged into it are included. The export is allowed
  while an admin is signed in as the account (support may help with it; it changes nothing).

### Anonymizing a person

`POST /api/v1/admin/persons/{personId}/anonymize` (`office`, behind a confirmation dialog on the
person's page) erases a person's personal data and keeps the person as the anonymous holder of its
history, in one transaction:

- **The person** gets the fixed name "Deleted" "person" (`domain.AnonymizedGivenName`,
  `AnonymizedFamilyName`) and loses email, phone,
  birth date, notes and member number; `persons.anonymized_at` records when (migration 00023, with
  a CHECK that the cleared columns stay empty). `Person.Anonymize` builds it, `Validate` holds it.
  The web app shows a translated "Deleted person" wherever it names the person: the API marks it
  `anonymized` on the person references that carry a name (person matches, class members and
  guardians, the audit log's persons, a lesson's participants and waiting list, the owner and
  payer of passes and receipts), and one helper (`web/src/lib/personName.ts`) translates it.
- **Removed**: guardian links in both directions, class memberships (their automatic bookings of
  lessons that have not started are canceled with all other future bookings, below), open
  waiting-list entries (withdrawn; a place an invite held goes to the next), staff links (the staff
  record stays with its display name, which the office maintains), the person's duplicate
  dismissals, and its account with the account's sessions, links, calendar feeds and announcement
  dismissals (below).
- **Canceled**: the person's active bookings of lessons that have not started, through the ledger
  (ADR 0012), on time and without releasing the date: a visit a pass paid comes back, and the
  place goes to the waiting list (ADR 0010). No mail goes to the person.
- **Kept**: past bookings, passes and redemptions, and receipts. Receipts are not touched at all:
  they are immutable, and their snapshot of the recipient's name stays, as the retention duty of
  receipts requires. The audit log still names the person (as "Deleted person").
- **Refused**: an unknown or merged person (404), an anonymized one (409 `anonymized`), the only
  guardian of a dependent (409 `sole-guardian`: the office links another guardian or removes the
  link first, so no child is left without a guardian by accident), a person whose account must be
  kept (409 `in-use`, below), the caller's own person (409 `own-account`), the person of an admin
  account unless an admin asks (403: deleting an admin's account is an admin's decision, like
  creating one, ADR 0008), and the person of the last admin who can sign in (409 `last-admin`).
- **Audited**: `person-anonymized`, the person as the target, no details.
- **Afterwards** an anonymized person behaves like a tombstone: `PersonRepo.List` leaves it out, so
  the person search, the pickers, the duplicate detection and the member import never see it;
  reading it by ID finds it (its page shows it read-only with an export button), and every write
  that names a person refuses it as unknown (`lockLivePersons`, 404).
- Lock order: the persons first, together in one ordered call (`lockPersonSet`, which reads again
  under the locks and adds a person that appeared meanwhile): the person, its guardians and
  dependents (whose links go, and whose other guardians the sole-guardian check counts), and, when
  its account is an admin's, every admin's person (the last-admin guard below). Then the terms and
  classes of its memberships, the lessons of its future bookings and open entries in ID order
  (`lockBookedLessons`, shared with the merge), and the passes (persons → term → class → lesson →
  passes).

### Retention

The daily job `purge-personal-data` (`service.RetentionService.Purge`, one transaction):

- **Guests**: a guest booking (no person) whose lesson ended more than 12 months ago gets the name
  `Guest` (`domain.AnonymousGuestName`; the column is NOT NULL for guests) and loses its email and
  trial note. The booking stays for the statistics. A booking anonymized already is not changed
  again.
- **Audit events** older than 3 years are deleted; **import runs** older than 1 year.
- "Older than" is strict, and months and years are calendar ones back from now in UTC
  (`time.AddDate`): an event exactly 3 years old stays until the next run. The store methods
  (`BookingRepo.AnonymizeGuests`, `AuditRepo.DeleteBefore`, `ImportRunRepo.DeleteStartedBefore`)
  have contract cases at these edges.
- Not here: the outbox keeps its own retention (ADR 0014), and receipts stay ten years and longer
  (there is no receipt purge).

### Account administration

Admins (`admin`) change accounts under `/api/v1/admin/accounts/{accountId}`, with row actions and
confirmation dialogs on the accounts page:

- **Disable** (`POST …/disable`) sets `disabled_at` and ends every session of the account and the
  impersonations it started; **enable** (`POST …/enable`) clears it. A disabled account signs in no
  more (already so since ADR 0004). Each impersonation of the account, or by it, that was still
  running is audited as `impersonation-ended` by the admin who started it, as signing out of it is
  (ADR 0005); deleting an account and a password change or reset, which end sessions the same way
  (`endSessions`), record it too.
- **Role** (`PUT …/role`, `member`, `instructor`, `office` or `admin`; anything else a 422 on
  `role`). Roles are read with every request, so the change applies at once; nothing else changes:
  staff links stay (an instructor demoted to member keeps the staff record but loses the
  instructor view; one promoted needs a staff link to teach). An impersonation whose account
  becomes an admin ends (admins are never impersonated, ADR 0005).
- **Delete** (`DELETE …`), below.
- **Guards**: the admin's own account is a 409 `own-account` for all three (another admin changes
  it; nobody locks themself out by mistake), and the last admin who can sign in is a 409
  `last-admin` (disable, demote, delete, deleting one's own account, and anonymizing its person).
  Only admin accounts that are enabled and confirmed count (`mayActAs`): an invited admin who has
  not set a password yet keeps nobody able to administer the school. When the account is an
  admin's, the guard reads the admins (`AccountRepo.ListByRole`) under the person locks of every
  admin account, taken with the account's own in one ordered call (persons come first in the lock
  order), so two admins who disable each other at the same time cannot both succeed. A change of a
  non-admin account, a promotion among them, cannot lessen the admins and locks only its own
  person.
- **Audited**: `account-disabled`, `account-enabled`, `account-role-changed` (details: the previous
  and the new role) with the account as the target, and `account-deleted` with the account's
  person as the target (the account is gone). An unchanged state or role changes nothing and is not
  audited. The actions are rows of `audit_actions` (migration 00023).

### Deleting an account

An account is deleted by an admin, or by its holder (`DELETE /api/v1/my/account` with the password;
below). The person always stays. What refers to the account (migration 00023 sets the foreign keys;
the memory store does the same in `AccountRepo.Delete`):

| Reference                                                                     | On delete                           |
| ----------------------------------------------------------------------------- | ----------------------------------- |
| `account_credentials`, `sessions` (own and as impersonator), `account_tokens` | deleted (CASCADE)                   |
| `calendar_feeds`, `announcement_dismissals`                                   | deleted (CASCADE, as before)        |
| `booking_events.actor_account_id`, `waitlist_entries.joined_by`               | forgotten (SET NULL)                |
| `lessons.canceled_by`, `passes.sold_by`, `import_runs.actor_id`               | forgotten (SET NULL)                |
| `lesson_notices.sender_id` (now nullable)                                     | forgotten (SET NULL)                |
| `audit_events.target_id`                                                      | re-pointed to the account's person  |
| `receipts.created_by`                                                         | kept: refuses the delete (RESTRICT) |
| `audit_events.actor_id`                                                       | kept: refuses the delete (RESTRICT) |
| `announcements.author_id`, `announcement_mailings.sender_id`                  | kept: refuses the delete (RESTRICT) |
| `person_duplicate_dismissals.dismissed_by`                                    | kept: refuses the delete (RESTRICT) |

- **Forgotten** are records that only say who did something in passing; they keep their meaning
  without it ("a deleted account").
- **Re-pointed**: an audit event about the account (impersonated, disabled, re-roled) is about its
  person afterwards, so the log keeps saying whom it concerned. `AuditEvent.Validate` allows these
  actions either target: the account while it exists, its person after (`AuditEvent.Retarget`).
- **Kept** are records whose author must stay attributable: receipts (tax law), the audit log
  (accountability: an admin cannot erase their trail by deleting their account), what the school
  published (announcements and their mailings), and the office's duplicate decisions. Only office
  and admin accounts write these; such an account is a 409 `in-use` ("disable it instead") until
  the records go (the audit log after 3 years; receipts not before ten years). Members and
  instructors are never refused, and anonymizing such a staff person is refused the same way.

**Deleting one's own account** (`signedIn`): the password is checked first (a wrong one is a 422 on
`body.password` and counts against the limit of wrong passwords per email, like a password change,
ADR 0006); refused while an admin is signed in as the account (403) and for the last admin who can
sign in (409 `last-admin`). The answer clears the session cookie. A confirmation goes to the old address
(`account-deleted`, en/de; it carries no link, so it is no secret-link mail, ADR 0014) in the
account's language, else the request's, else the school's. The audit event `account-deleted` of a
self-deletion has no actor: `audit_events.actor_id` is nullable for this action only (a CHECK), and
the log shows "The account holder". The web app offers it at the end of the account page, with the
password and a confirmation, and signs out.

### Demo

The demo seed adds nothing personal. Its admin can try every account action on the fictional
members; the last-admin and own-account guards keep the demo's only admin, and the office account
(which issued the seeded receipts) is kept by the `in-use` rule, so the demo cannot be locked.

## Consequences

- A person can take their data along and have it erased without the database; the school keeps
  what tax law and its own records need, and nothing more about guests than a year.
- An anonymized person stays as "Deleted person" in old bookings, class histories, statistics and
  the audit log. It cannot be undone.
- Former staff whose accounts issued receipts or acted in the audit log are disabled rather than
  deleted until those records expire; their person cannot be anonymized meanwhile. (A later
  version may null the audit actor after a grace period, or erase the staff person but keep a
  pseudonymous account.)
- ADR 0015's "two persons who both sign in cannot be merged until an administrator disables one
  account; … for now that takes the database" no longer needs the database.
- Events are no longer "only ever added": they are deleted after three years, and an account's
  deletion re-points the events about it.
