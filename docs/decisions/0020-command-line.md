# 0020 — Command line

- Status: accepted
- Date: 2026-11-01
- Amends: [0005](0005-rbac-and-impersonation.md) (a command acts as a named account; audit
  events record the channel), [0015](0015-import-duplicates-and-merge.md) (`import` gets an
  actor), [0016](0016-privacy-and-account-administration.md) (its guards apply to the commands)

## Context

The binary sets a school up and runs it (`serve`, `migrate`, `seed`, `init`, `import`,
`healthcheck`, `openapi`, `version`, `vapid-keys`), but nothing in it administers a school. An
operator who is locked out, has a GDPR request to answer, needs receipts for the tax adviser or
wants to check data integrity must open a browser or the database. Job runs exist only inside the
serving process (`internal/jobs`), so no other process can show them. Phase 11 adds the
administration commands, and the MCP server of Phase 12 builds on the same command layer.

## Decision

### Local only

Every command runs locally on the server's shell. It opens the database from `DATABASE_URL` or
`--database-url`, checks the schema as `serve` does (`openPostgres`), and calls the services. The
services come from the same constructor `app.New` uses, so a command and its HTTP operation share
their rules, validation and audit. A command works while the server is down and needs nothing
beyond shell access and the database's credentials.

There is no remote mode: no personal access tokens and no HTTP client. A command reaches the
services through `cli.Env`, never the store directly, so a remote mode could later put the same
calls behind an interface without changing the commands. That would be a new PLAN item with its
own ADR, covering a token table, token authentication, the account page and the PHP backend.
Phase 12's MCP server over stdio uses the local mode.

### The actor: `--as <email>`

A command that calls a service operation taking a principal needs `--as <email>` (env `STUDIO_AS`).
The email names an account that exists, is confirmed and is not disabled. The command acts as that
account: a `service.Principal` with its account, person and role, and no session. The command
checks the account's role against its own access level the way the access middleware does
(ADR 0005). Most administration commands need `office`; account administration, the mail log and
settings writes need `admin`, matching their HTTP operations. An unknown, unconfirmed or disabled
account exits with code 3, and a role too low for the command with code 4. Both happen before
anything runs.

`--as` is not a login. Whoever can run the binary with the database's credentials can already read
and write the database, so no password is asked. This is also the way back in: an admin who has
forgotten their password runs `accounts reset-password --as <their email> <their email>` and gets
a setup link.

The audit log records the channel. A principal built by the command line carries
`Principal.Via = {Channel: "cli", OSUser: <the OS user>}`, and every audit event written for it
stores that in `AuditDetails.Via`. Requests through the API leave it empty. The admin's audit view
shows "via command line (<os user>)".

Operator commands take no `--as`, because they call no principal-taking operation: `config check`,
`doctor`, `db check`, `receipts verify`, `jobs list|run`, and the existing `migrate`, `seed`,
`init`, `healthcheck`, `openapi`, `version` and `vapid-keys`. `import` keeps acting without an
account (ADR 0015) until it moves to the layer. After that it takes `--as`, and its runs name the
account.

### Output and exit codes

- **Text by default:** aligned tables, one record per line, with a header line only on a
  terminal (see the amendment on `text/tabwriter`). Dates and times are in the school's time
  zone, never the machine's.
- **`--output json`:** one JSON document on stdout. Where a command mirrors an API operation, the
  document is the operation's response body, with the field names and types of the OpenAPI schema
  (the generated `api/openapi.json` is the contract). A command without an operation documents its
  schema in docs/cli.md. The schema is stable: fields are only added, and a rename or removal waits
  for a major release.
- **Colour** only when stdout is a terminal and `NO_COLOR` is unset.
- **Errors** go to stderr as one line, `error: …`. A `*domain.FieldError` names the field the way
  the command spells it (`--email: …` for a flag, `key=value` for a setting). With
  `--output json`, stderr gets the error as a problem document (`title`, `status`, `detail`,
  `errors`), like the API's.
- **Exit codes:** 0 ok; 1 failed, including a check with findings and an import with failed rows;
  2 usage (an unknown flag, a missing argument, invalid input before anything runs, a refused
  confirmation); 3 not found (`domain.ErrNotFound`, also an unknown `--as`); 4 conflict
  (`domain.ErrConflict` and what wraps it, `ErrInUse` and `ErrDuplicate`, a refused guard such as
  `last-admin`, and `domain.ErrForbidden`, a role too low for the command).

### Safety

A command that deletes, anonymises, mails, cancels or merges asks for confirmation when stdin is a
terminal ("Anonymize Erika Example? [y/N]"), and needs `--yes` otherwise. Without `--yes` and
without a terminal, it refuses with exit code 2 and changes nothing. `--dry-run` is offered where
the service has one (`import`, the merge preview): it prints what would happen and writes nothing.
Secrets are never arguments: a search term, a password or a token comes from a prompt or stdin
(shell history, `ps`; ASVS 8.3.1). A command never prints a secret, except the setup link it was
asked for.

### Job runs are stored

The Runner records every run in `job_runs`: the job, the start and end, the result
(`ok|failed|skipped`) and the error message without internals. The table exists in both stores.
The daily `purge-personal-data` job (`service/retention.go`) prunes runs older than 30 days.

Each run takes a per-job lock beside the transactions, so the job's own transactions run while
it is held: on PostgreSQL a session-level advisory lock on a connection of its own (see the
amendment), released when the run ends or the connection drops, and on the memory store a mutex
per job name. A run whose lock another process holds (a second `serve`, or `jobs run`) is skipped
and recorded as `skipped`. `jobs list` shows each job's interval and its last run with its result.
`jobs run <name>` runs one job now, under the same lock, and waits for it.

### The command layer

`cmd/studio-schedule/cli` holds the shared layer:

- a command spec (use, flags, access level, whether it is destructive)
- the `Env` (store, services, clock, output, principal)
- the text and JSON presenters
- the mapping of errors to messages and exit codes
- the confirmation

The commands live in `cmd/studio-schedule` and stay thin: parse flags, call one service method,
present the result. There is no SQL in `cmd`. The global flags are `--database-url`,
`--output text|json`, `--yes` and `--as`, and `completion` covers bash, zsh, fish and PowerShell.
docs/cli.md is generated from the cobra tree, and CI fails when it is stale, as it does for
`api/openapi.json`.

## Consequences

- An admin can do on the shell what needs a browser today, and a lockout is fixed without SQL.
- Every command is tested in-process on the memory store and PostgreSQL. Its text and JSON output
  are golden files, and the coverage gate counts the commands.
- Anyone with shell access and the database's credentials can act as any admin. That was already
  true through the database itself, and the audit log now names the OS user.
- `AuditDetails` gains `Via`. The details are a `jsonb` object, so the field needs no migration.
- `job_runs` is a new table, and every job run takes a lock. A school that runs two `serve`
  instances no longer runs the same job twice at once.
- A remote mode with tokens and an HTTP client stays possible, and stays unbuilt.

## Amendment (2026-11-01, while building it)

- **Tables are aligned by the layer, not `text/tabwriter`.** `tabwriter` counts the bytes of a
  color's escape sequence as width, so on a color terminal a bold header or a colored cell
  shifted its column. The presenter (`cli.Text`) pads each column to its widest visible cell
  instead; on a pipe the output is the same as `tabwriter`'s.
- **Commands not yet on the layer print text only.** `serve`, `migrate`, `seed`, `init`,
  `healthcheck`, `openapi`, `version`, `vapid-keys` and `completion` refuse `--as`, `--yes` and
  `--output json` with exit code 2 rather than ignoring them. `--database-url` is a global flag
  that every command reads.
- **One connection holds a process's job locks.** On PostgreSQL one dedicated connection holds
  all of a process's job locks, not one connection per lock. With the pool's default size, jobs
  whose tickers coincide would each hold a lock connection while waiting for one for their
  transaction, and block the jobs and every request. Advisory locks are re-entrant within a
  session, so the store refuses a job name it already holds before asking the server.
- **The demo does not save job runs.** Its saved snapshot leaves `job_runs` out: its jobs run
  every minute, so saving them would save the demo every minute and let its saved data grow by
  thousands of rows a day.
- **`db check` builds its service itself.** The integrity checks are `service.IntegrityService`,
  one read-only transaction over a few aggregate store reads. `app.Services` has no field for it
  yet, so `db check` opens the database with the process's opener and builds the service on that
  store, instead of reaching it through `cli.Env.Services`. It calls the service only, never the
  store, and once `app.Services` has an `Integrity` field, the command takes it from there like
  every other command. The check agrees with the services rather than with a stricter reading:
  a lesson booked beyond its capacity is a finding only where the bookings' events record no
  override (ADR 0009), and an anonymized person keeps the passes it had (ADR 0016), so only a pass
  sold after the anonymization is a finding.
- **The timetable commands need `office`, also where the operation is public.** `lessons list`,
  `terms list` and `classes list` print what the office's pages show (hidden and canceled
  lessons, retired classes), so they need `office` like those pages, although `getSchedule` and
  `listTerms` are public; their JSON is still that operation's body (`listAllClasses` for the
  classes). `lessons list` adds a column the body lacks, the active bookings beside the places,
  from the same service read. `lessons cancel` has no `--notify`: `CancelLesson` always mails
  everyone booked, so the flag could only lie; the help and the question say it mails.
  `stats` leaves the top participants out of the text unless `--participants`, as they name
  persons; the JSON is the operation's full body, as the API gives it to the office.
- **`jobs run` asks no confirmation.** A job deletes (`purge-sessions`, `purge-mail`,
  `purge-personal-data`) or mails (`deliver-mail`) only what serve would on its next tick, so
  running it early changes nothing an operator has to confirm; its help says what each job does.
  A failed run exits with status 1 even when the job's error wraps a conflict or a missing row, so
  4 stays the held lock and 3 the unknown job. The jobs are built as serve builds them: the
  senders come from the same constructor through `cli.System.Delivery`, so `deliver-mail` and
  `deliver-push` are there exactly when serve has them.
- **`mail requeue <id>` asks too, not only `--failed`.** Queuing a mail again mails it, so both
  forms confirm like every command that mails. `mail test` sends straight through the mail server,
  never through the outbox, so its template (`test`) is not one of the mail log's templates.
- **The `accounts` group (2026-11-01).** `accounts list`, `show`, `set-role`, `disable`, `enable`,
  `reset-password`, `sessions revoke` and `delete` need `admin`, `accounts create` needs `office`
  like `createPersonAccount` (only an admin creates an admin). An account is named by its email or
  ID. `show` prints an item of `listAccounts`, and `delete` prints the account as it was, because
  `deleteAccount` answers without a body. `disable` and `create` ask for confirmation too (one ends
  sessions, the other mails), and `reset-password` always does, since a new link stops the older
  ones. Its link is valid for an hour (`ResetTokenTTL`). Issuing a reset link and ending sessions
  are audited (see the next bullet); the impersonations that end are audited as before. The
  `last-admin` guard cannot be reached from the command line alone: `--as` needs an admin who can
  sign in, and an admin's own account is refused as `own-account` first.
- **Reset links and ended sessions are audited (2026-11-01).** `password-reset-issued` targets the
  account and records whether the link was mailed or printed (`AuditDetails.Delivery`), never the
  link. `sessions-revoked` targets the account, or nothing for `--all`, and counts the sessions
  that had not ended yet (`AuditDetails.Sessions`). Both record the `--as` account as the actor and
  its `Via`. The actions are rows of `audit_actions` (migration 00029).
- **`accounts setup-link <email|id>`, an operator command (2026-11-01).** A first admin whose
  `init` link expired before they confirmed could not get back in: `--as` needs a confirmed
  account, so `reset-password` did not reach them, and without mail nothing did. `setup-link`
  prints a new setup link like `init`'s (`AuthService.IssueSetupLink`: valid for 72 hours,
  `InviteTokenTTL`; it replaces the pending links) for an account that has not confirmed and has
  no password of its own. It takes no `--as`, and works only while no admin can sign in, counted
  as the `last-admin` guard counts (a confirmed, enabled admin; under the same person locks). Once
  one can, it is refused as `admin-exists` (exit 4) before anything else is looked at, and changes
  nothing, so it never gets round an admin, who uses `reset-password`. A confirmed account is
  refused as `confirmed` (exit 4), an unknown one exits 3. It confirms like the other destructive
  commands, since the new link stops the pending one. Its audit event, `setup-link-issued`, is the
  second one without an actor (after an account its holder deleted): `AuditAction.ActorOptional`
  and the CHECK `audit_events_actor` (migration 00029) admit it, the domain requires its `Via` to
  be the command line, with the OS user, and the link as printed. The command gets the OS user
  from `cli.System.OSUser` through `newAccountsCmd`, since `cli.Env` exposes only the principal's
  `Via`. The audit view shows such an event as "An operator" with the via line.
- **`settings`: the file is an argument, and the areas include receipts and passes.** `-o` is the
  global `--output`, so `settings export [file]` takes its file as an argument, like
  `settings import <file>`. Besides the cards of ADR 0013 (`school`, `branding`, `regional`,
  `booking-rules`), the receipt settings (`receipts`) and the redemption mode (`passes`) are areas
  too: each is a pair of API operations whose request body is flat `key=value` data. The export is
  `{"version": 1, …}` with every area's request body; an area in it replaces the whole area on
  import, in one transaction (`SettingsService.Import`), and a key the API requires is required
  there too. `settings set` is not destructive: it changes only the values it names.
- **`persons export` writes a file with `--file`, not `-o`.** `-o` is the global `--output`'s
  short flag, and cobra panics on a command's flag that reuses it, so the export takes
  `--file`/`-f` (mode 0600, an existing file only with `--force`). Its document is the export's
  JSON in either output format; with `--file`, stdout gets one line of text, or nothing with
  `--output json`. A command whose operation answers without a body
  (`persons duplicates dismiss`) prints nothing with `--output json`. `persons merge --apply`
  runs the preview first, so it asks with both names and refuses a conflict before the question.
- **Receipts write files with `--file` and know one format.** `receipts pdf` writes its PDF with
  `--file`/`-f` (mode 0600, an existing file only with `--force`), since `-o` is `--output`'s
  short flag. `receipts export` writes CSV only: it has no `--format` and refuses
  `--output json`. `receipts verify` does not look for duplicate numbers or a receipt canceled
  twice, which both stores refuse with unique constraints; it checks instead that a number ends in
  its year and sequence and that a Storno negates its original.
