# 0015 — Member import, duplicates and merging

- Status: accepted
- Date: 2026-10-25
- Amends: [ADR 0008](0008-families-and-dependents.md) ("merging duplicate persons is not planned")

## Context

Phase 7 asks for a member import from CSV files (a column mapping, a dry run, an idempotent
re-import, a run history) and for the detection and merging of duplicate persons. A school brings
its members from a spreadsheet or another member system, whose exports are usually German Excel
files: Windows-1252, semicolons, dates as DD.MM.YYYY, the parents' names and emails on the child's
row. The previous system matched persons by first and last name only, and its merge re-pointed
rows without a transaction and without regard to accounts.

Persons had no member number, so a second import could not find the persons of the first. Many
tables refer to a person (ADR 0008, amendment): guardian links in both directions, the account
(one per person), staff links, class memberships, bookings (written only through the ledger, ADR
0012), waiting-list entries, passes as owner and as payer, receipts as payer, and audit events.
Receipts are immutable (ADR 0012), so a merged person's receipts cannot be moved.

## Decision

### Member number

`persons.member_no` is the school's own member number: optional, unique (exactly, byte-wise),
trimmed, up to 40 characters (migration 00021). The office edits it in the person form (a number
another person has is a 409 `duplicate`) and finds persons by it in the person search. It is the key
by which the import finds a member again.

### Import

Office and admins import (`AccessOffice`): `inspectImport` reads a file, `runImport` tries or
imports it, `listImportRuns` lists the history (`/api/v1/admin/imports`). The command line runs the
same service: `studio-schedule import <file>` is a dry run unless `--apply`, maps the columns by
their headers or with `--map field=Header`, and `--notes Header` adds columns to the notes; its runs
have no account.

- **File** (`internal/csvimport`): at most 1 MiB, 5,000 rows and 100 columns. Valid UTF-8 (a byte
  order mark is dropped) is UTF-8, anything else Windows-1252 (`golang.org/x/text/encoding/charmap`;
  it adds about 150 KB to the demo's WebAssembly, 31 KB compressed). The delimiter is the one of `;`,
  `,` and tab the header line uses most outside quotes, `;` on a tie. The first row names the
  columns, each once ignoring case; cells are trimmed; empty rows are skipped. Dates are
  DD.MM.YYYY (one-digit days and months too) or YYYY-MM-DD. An unreadable file is a 422 on
  `body.content`.
- **Mapping**: a column fills at most one target field: the member's member number, given and
  family name, email, phone, birth date and notes, and given name, family name, email and phone of
  up to two guardians. The member number and both names are required, and so are a guardian's
  names when any of its columns is mapped (else a 422 on `body.mapping…`). Further columns can go
  into the member's notes, a line "Header: value" each. The server suggests a mapping from English
  and German header names ("Mitgliedsnummer", "Vorname", "E-Mail Elternteil 1", "Telefon Mutter",
  "Parent 2 email", …); the page remembers the last mapping in the browser (localStorage) and lays
  it over the suggestion for the headers a new file has.
- **Members** are found by member number. One not found is created; one found gets the mapped
  fields the row fills, and keeps the others (an empty cell never clears a field; an email that
  differs in case only is no change). The notes of a row (its notes column and notes columns)
  replace the person's notes when they differ.
- **Guardians** are found by email, an account's first, then a person's (ignoring case), else by
  name (folded like the duplicate detection, below) among the persons whose email does not
  contradict the row's. When the row names the guardian, an email match counts only with the same
  name: a family shares an address often, and a member or a sibling that carries the parent's email
  is not the parent (without the name rule a member with its mother's email was found as its own
  guardian). Several persons that match are `guardian-ambiguous`. A guardian not found is created;
  a found one gets the fields the row fills. The guardian is linked to the member unless it
  is already; links are added, never removed. A later row finds what an earlier one created, so
  siblings share their parents.
- **Problems** make a row fail as a whole; the other rows are imported. A row's problems are codes
  with the field they concern: `required`, `invalid` (an email, a date, a value too long), `future`
  (a birth date after today, school time zone), `member-no-repeated` (an earlier row of the file has
  the number), `guardian-ambiguous`, `guardian-is-member`, `guardian-cycle` (the guardian is a
  dependent of the member, ADR 0008). A new member that may be a duplicate (the same email, or the
  same name and birth date) is imported with a warning naming the persons; its family (the row's
  guardians and their dependents) does not count by the email alone, as in the duplicate list.
- **No accounts** are created; the office invites persons later (ADR 0008, amendment).
- **Dry run and apply**: the dry run is the same code in a transaction that is rolled back; it
  reports each row as `create`, `update`, `unchanged` or `error` with the fields it writes, its
  guardians (found by email or name, new, linked) and warnings. It names no person it creates (no
  ID), also where a later row finds that person again or warns about it. Applying sends the file
  again; the server keeps nothing in between.
- **Locks**: the whole file is one transaction. Before the first row it locks every stored person
  a row may find (its member by number, its guardians by email or name) in one `Persons().Lock`,
  so in ID order, and reads the persons again under those locks (repeated if a read finds more).
  Two imports, or an import and a merge (which locks its persons first as well), so take their
  person locks in one order; a person a row finds only through what an earlier row wrote is one of
  those, or one the run creates. Only the cycle check of a guardian link locks the member's
  dependents as it walks them, as the office's guardian link does; PostgreSQL ends such a rare
  deadlock with an error, which the store retries.
- **History**: an applied import is recorded in `import_runs`: file name, account (none for the
  command line), start and end, the counts, and the first problem of each failed row (at most 200).
  There is no undo; importing the same file again changes nothing.
- **Command line**: `studio-schedule import` prints every row and the counts; when a row failed it
  exits with status 1 (dry run or `--apply`), after importing the good rows, so a script notices.

### Duplicates

`listDuplicates` (`AccessOffice`) computes the pairs live over every person that is not merged:

- **strong**: the same email ignoring case, a person's own or its account's, or the same name and
  birth date;
- **weak**: the same name, at least one birth date unknown.

A family shares an email often: a guardian and its dependent, or two dependents of the same
guardian, never match by the email alone; they still match by name and birth date (a child entered
twice) or by name.

Names compare folded: lower case, umlauts spelled out (ä → ae, ß → ss), common accents dropped,
hyphens as spaces. Strong pairs come first; at most 200. `dismissDuplicate` records that a pair is
not the same (`person_duplicate_dismissals`, the pair ordered by ID, with who and when); it goes with
either person, and a merge moves the merged person's dismissals to the survivor (dropping the pair
of the two, and a pair the survivor has dismissed already).

### Merge

`mergePersons` (`AccessOffice`) merges the duplicate (`mergedId`) into the survivor. The office
picks which value the survivor keeps of name, email, phone, birth date and member number; the
notes of both are kept (the survivor's first, then the duplicate's unless the survivor's contain
them). The office may edit them in the merge dialog, which sends the text it chose (`notes`);
notes longer than 2,000 characters, combined or edited, refuse the preview and the merge with a
422 on `notes`. Without `apply` it is a preview: the same code, rolled back, reporting what moves
and what is canceled. Refused are a person with itself (422), an unknown or merged person (404), a
merge after which a guardian of the survivor would also be its dependent (409 `guardian-cycle`), and
two persons with accounts (409 `two-accounts`) unless the duplicate's account is disabled: that one
stays with the tombstone, and the survivor keeps its own.

In one transaction, locking both persons first (and the dependents the cycle check walks), then
the terms and classes of the duplicate's memberships, the lessons of its bookings and open
waiting-list entries in ID order, and the passes concerned (persons → term → class → lesson →
passes):

- **Guardian links** move in both directions; links the survivor has already, and links between
  the two, are dropped.
- **Bookings** move, canceled ones included, through the ledger (`updateBookings`, ADR 0012). Where
  the survivor has an active booking of a lesson that has not started (by the clock), the
  duplicate's active one is canceled first, timely and without releasing the date: a visit it cost
  comes back, and the place goes to the waiting list (ADR 0010). Of a lesson that has started,
  both bookings stay as history: the duplicate's moves to the survivor as it is (its attendance,
  and the visit it paid), marked `bookings.merged_from` with the duplicate. Such a booking is left
  out of the partial unique index of one active booking per person and lesson (migration 00021),
  the only place two active bookings of one person in one lesson can exist; the ledger settles
  each booking on its own, so nothing is redeemed or refunded by the move. Rolling the migration
  back moves those bookings back to their tombstones.
- **Waiting-list entries** that are open move, unless the survivor waits for the lesson or is
  booked into it; then they are withdrawn, and so is an open entry of the survivor for a lesson it
  now has a booking of. Ended entries stay with the tombstone as history.
- **Class memberships** move, unless the survivor is a member of the class; then the duplicate's
  ends, and its automatic bookings of the class's lessons that have not started follow the
  survivor's membership: one of a date the survivor is not active on (a pause, a later start) or
  released (ADR 0009) is canceled like a booking the survivor has already; the others follow the
  booking rule above. The duplicate's own bookings (not automatic) stay.
- **Passes** it owns or paid for, **staff links**, its **account** (when the survivor has none),
  its duplicate dismissals and persons merged into it earlier (so tombstones never chain) move.
- The duplicate becomes a **tombstone**: `persons.merged_into` names the survivor, its member number
  is cleared. Lists, searches, pickers and the duplicate detection leave it out
  (`PersonRepo.List`); its page shows it read-only with a link to the survivor. Its receipts stay
  on it and are listed under the survivor: My receipts and their PDFs include those of persons
  merged into the account's person; the mail of a later cancellation (Storno) of such a receipt
  goes to the survivor's account. A tombstone is not edited, deleted or merged again (404), and
  nothing new refers to it: every write that names a person (selling or assigning a pass, a payer,
  an account, a class membership, a staff link, staff booking a person, a guardian link, a
  dismissal, the family's own edits) locks it and refuses a tombstone (`lockLivePersons`), under
  the same lock the merge takes.
- The merge is recorded in the audit log (ADR 0014): `person-merged`, the survivor as the target,
  the duplicate and its name in the details.

## Consequences

- The import is idempotent through the member number, which every row must have.
- A person matched by name may be the wrong one if two share a name without emails; the row is then
  `guardian-ambiguous` and the office adds the email.
- A merge cannot be undone; the audit log and the tombstone keep what was merged. ADR 0008's
  "merging is not planned" no longer holds.
- Two persons who both sign in cannot be merged until an administrator disables one account; there
  is no account administration in Phase 7, so for now that takes the database.
- Every person query that lists persons leaves tombstones out; reading one by ID still finds it,
  for receipts and the audit log.
- After a merge, a lesson that had started may list the survivor twice (its own booking and the
  history booking); counts of active bookings count both, counts of persons once.
- The memory store indexes member numbers (`memberNos`, rebuilt on a snapshot's restore), so the
  uniqueness check of an import's rows does not scan every person in the single-threaded demo.

## Amendment (2026-10-25): account administration and anonymized persons

[ADR 0016](0016-privacy-and-account-administration.md) adds the account administration: an admin
disables one of two accounts before a merge, without the database. An anonymized person is left
out of lists, searches, the duplicate detection and the import like a tombstone, and is refused by
every write (`lockLivePersons`); the retention job deletes import runs older than one year.
