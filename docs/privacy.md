# Privacy

What personal data Studio Schedule stores, why, how long, and how a school answers the requests of
the people it concerns. It describes the software, for the school (the controller) and its
operator; it is not a privacy policy and no legal advice. The decisions behind it are in
[ADR 0016](decisions/0016-privacy-and-account-administration.md) and the ADRs it links.

Everything lives in the PostgreSQL database. The app sets one cookie (the session, after sign-in),
loads nothing from other sites (fonts and scripts are part of the app), has no analytics or
tracking, and stores no IP addresses or browser details.

## What the app stores

| Data               | Of whom                                   | What                                                                                                                                                       | Why                                                 | Kept                                                                                |
| ------------------ | ----------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------- | ----------------------------------------------------------------------------------- |
| Persons            | members, their children, guardians, staff | given and family name; optionally email, phone, birth date, member number, the office's notes                                                              | who takes part, contact, age groups                 | until the office anonymizes the person                                              |
| Guardian links     | parents and their children                | who may act for whom                                                                                                                                       | parents book and manage for their children          | until removed or the person is anonymized                                           |
| Accounts           | everyone who signs in                     | email, role, language, visible days, confirmed and disabled times, the password as an argon2id hash                                                        | sign-in and permissions                             | until deleted by an admin or its holder                                             |
| Sessions and links | account holders                           | hashes of session tokens and of email links (confirm, reset, email change, invitation), with their times                                                   | staying signed in, the links in mails               | sessions 14 days idle, at most 90; links 1 to 72 hours; deleted hourly once expired |
| Class memberships  | members                                   | class, since, paused until                                                                                                                                 | automatic bookings of the weekly classes            | until ended or the person is anonymized                                             |
| Bookings           | members and their children                | lesson, status, times, attendance, late cancellation, and every change with who made it                                                                    | places, attendance, passes, the school's statistics | kept as the school's records                                                        |
| Guest bookings     | guests without a person record            | name, email, the instructor's note on a trial                                                                                                              | a place in a lesson for someone without an account  | name, email and note cleared 12 months after the lesson                             |
| Waiting list       | members and their children                | entries, invitations (token hashes), who joined                                                                                                            | offering freed places in order                      | kept as history; removed when the person is anonymized                              |
| Passes             | members and their children                | pass type, owner, payer, sale, validity, every visit used                                                                                                  | prepaid visits                                      | kept as the school's records                                                        |
| Receipts           | buyers                                    | number, date, lines, total, payment method, the recipient's name and address as printed, the PDF                                                           | tax law                                             | never changed or deleted by the app (see below)                                     |
| Mails              | recipients                                | address, subject, text and HTML body, delivery state and errors                                                                                            | sending, and the office's mail log                  | bodies 30 days after sending, the mail 180 days after it was written                |
| Announcements      | account holders                           | which announcements an account dismissed                                                                                                                   | showing each one once                               | until the account is deleted                                                        |
| Calendar feeds     | account holders                           | scope, location, created, last used, the token's hash                                                                                                      | the lessons in the member's calendar app            | until revoked or the account is deleted                                             |
| Audit log          | staff and the persons concerned           | who did what to whom and when (impersonation, account administration, anonymization, merges, mail requeues), without personal details beyond the reference | accountability of staff actions                     | 3 years                                                                             |
| Import runs        | the office                                | file name, who, when, counts, and the messages of the rows that could not be imported                                                                      | the history of member imports                       | 1 year                                                                              |
| Staff              | instructors and accompanists              | display name, link to the person                                                                                                                           | the timetable                                       | as long as the school keeps the staff record                                        |

The retention periods run automatically: the hourly `purge-sessions` job and the daily `purge-mail`
and `purge-personal-data` jobs. The application log (stderr) records requests without query strings
and without client addresses; its retention is the operator's (below).

Children's data is entered and managed by their guardians and the office. A child signs in only
with an account of its own (a managed dependent, e.g. a teenager), which the school creates.

## Export (right of access, data portability)

Both export one JSON file (`studio-schedule.person-export`, version 1) with everything the app holds
about a person: the person with every field (the office's notes included), guardians, dependents
and merged persons (names only), the account, its calendar feeds and dismissed announcements,
class memberships, every booking and waiting-list entry, the passes with their visits, and the
receipts the person paid (as data, not the PDFs). It never contains password hashes, session or
link tokens, or calendar feed tokens.

- **The office**: "Export data" on a person's page, for any person (also an anonymized or merged
  one).
- **The person**: "Your data" on the account page, for themselves and each of their dependents.
  What belongs to a dependent's own account is exported by that account's holder.

## Erasure

**Anonymizing a person** (the office, on the person's page, after a confirmation). In one step, the
person becomes "Deleted person": name replaced, email, phone, birth date, notes and member number
cleared. Its guardian links, class memberships, open waiting-list entries, staff links and its
account (with sessions, links, calendar feeds and dismissals) are removed; future bookings are
canceled on time (visits paid with a pass come back). Past bookings, passes and receipts stay, as
anonymous history. It cannot be undone and is recorded in the audit log. The app refuses to
anonymize the only guardian of a child (link another guardian first), the person of the last admin,
the caller's own person, and the person of an office or admin account that issued receipts or
acted in the audit log (disable that account instead, until those records expire).

**Deleting an account.** A member deletes their own account on the account page (with the
password); an admin deletes any account. The person record stays with its bookings and passes; to
erase it too, the office anonymizes it. A confirmation mail goes to the old address.

**Guests** need no request: their name and email are cleared 12 months after the lesson.

## What stays, and why

- **Receipts** are immutable: the database refuses every change and deletion, and each keeps the
  recipient's name and address as printed. Tax law requires keeping them (ten years in Germany, the
  basis of ADR 0012); the app has no receipt purge, so they stay until the operator removes them
  outside the app once the period has passed. Anonymizing a person leaves them untouched.
- **The audit log** keeps naming an anonymized person (as "Deleted person") and the staff who acted,
  for 3 years.
- **Anonymous history** (past bookings, attendance, passes) stays for the school's records and
  statistics, without a name.
- **Backups** keep everything as it was when they were taken (see
  [operations.md](operations.md#retention-of-backups)).

## What the operator still has to do

The software gives the school the tools; the school and its operator remain responsible for:

- **A privacy policy** for members and guests, e.g. on the school's website, with its address in the
  legal notice of the app (Administration → Settings; plain text), naming the controller, the purposes and legal bases above, the retention periods,
  the recipients (hosting, mail provider) and the rights of the people concerned. Parents consent
  for their children where consent is the basis.
- **Processor agreements** (Art. 28 GDPR) with the hosting provider, the managed database provider
  if any, the mail provider and the backup storage.
- **Security of the installation**: TLS (`BASE_URL` with `https://`), updates, access to the server
  and the database, encrypted backups, see [operations.md](operations.md) and
  [security.md](security.md).
- **Log and backup retention**: the app's log and the reverse proxy's access log (which records
  client addresses and query strings, such as the office's search terms) and the backups follow
  the operator's own retention periods; keep them short.
- **Answering requests** within the legal deadlines: export or anonymize through the app, and
  anonymize again after restoring a backup that predates an erasure.
- **Never loading the demo data** (`seed`) into an installation with real people: its accounts share
  a published password.
- **Records of processing** and, where required, a data protection officer.
