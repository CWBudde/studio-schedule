# MCP server

`studio-schedule mcp` lets an assistant work with a school: Claude Desktop, Claude Code or any
other client of the [Model Context Protocol](https://modelcontextprotocol.io). The client starts
the binary and speaks MCP with it over stdin and stdout. Like every command, the server opens the
database of `DATABASE_URL` or `--database-url`, checks its schema and calls the same services as
the HTTP API, so a tool and its operation share their rules, validation and audit. It acts as the
account of `--as` and can do no more than that account. There is no HTTP endpoint for MCP, and
the WASM demo has no MCP server. The decisions are in [ADR 0022](decisions/0022-mcp-server.md).

- [Setup](#setup)
- [Flags](#flags)
- [Exit codes at start](#exit-codes-at-start)
- [Access on every call](#access-on-every-call)
- [Tools](#tools)
- [Audit](#audit)
- [Privacy](#privacy)

## Setup

Give the assistant an account of its own, e.g. `assistant@example.org` with the role it needs
(see [Audit](#audit) for why). The examples below use `office@example.org`. Every value is
fictional; put in your own binary path, database URL and account.

**Claude Desktop.** Add the server to `claude_desktop_config.json` (on macOS in
`~/Library/Application Support/Claude/`, on Windows in `%APPDATA%\Claude\`) and restart the app:

```json
{
  "mcpServers": {
    "studio-schedule": {
      "command": "/usr/local/bin/studio-schedule",
      "args": ["mcp", "--as", "office@example.org"],
      "env": {
        "DATABASE_URL": "postgres://studio:change-me@db.example.org:5432/studio?sslmode=verify-full"
      }
    }
  }
}
```

Instead of `--as`, `env` may set `STUDIO_AS`; `--as` wins over it. The file holds the database
password, so only its owner should be able to read it.

**Claude Code.** Register the server once:

```sh
claude mcp add studio-schedule -- studio-schedule mcp --as office@example.org
```

The server gets `DATABASE_URL` from the environment Claude Code runs in, or from
`claude mcp add --env DATABASE_URL=…`, which stores it in Claude Code's configuration.

**Over SSH.** A client on another machine starts the server on the database's host:

```json
{
  "mcpServers": {
    "studio-schedule": {
      "command": "ssh",
      "args": [
        "schedule.example.org",
        "studio-schedule",
        "mcp",
        "--as",
        "office@example.org"
      ]
    }
  }
}
```

`DATABASE_URL` then comes from the environment on the remote side. A non-interactive SSH session
reads no login profile on most systems, so set it where such a session sees it, e.g. in a wrapper
script on the server that reads it from a file only its user can read. SSH needs a key without a
prompt, since the client cannot answer one. The audit log names the remote OS user.

## Flags

- `--as` (string): email of the account the server acts as (env `STUDIO_AS`). Required.
- `--database-url` (string): as for every command (env `DATABASE_URL`); prefer the env var.
- `--personal-data` (bool): also list the tools that return person data (the "Personal data"
  column of [Tools](#tools)), and the statistics' top participants. Read [Privacy](#privacy)
  first.
- `--allow-writes` (string list, comma-separated): also list the write tools of these groups:
  `bookings`, `lessons`, `announcements`, `mail`, `jobs`. The groups are checked at start; this
  version has no write tools yet.

`mcp` takes no `--output` and no `--yes`; both are usage errors. The client asks its user before a
write, which takes the place of `--yes` ([ADR 0022](decisions/0022-mcp-server.md)). Logs go to
stderr; stdout carries nothing but MCP.

## Exit codes at start

The server checks its flags and the account before it speaks MCP:

- `2`: an unknown flag, `--output` or `--yes`, or an unknown `--allow-writes` group.
- `3`: the `--as` account is unknown, unconfirmed or disabled.
- `4`: the account's role reaches no tool. Every signed-in role reaches `schedule`, so with
  today's tools this cannot happen.

Other failures at start, such as a database that cannot be opened or a schema that does not match
the binary, exit as they do for every command ([cli.md](cli.md#exit-codes)). Once running, the
server stays up until the client closes the connection, and a failed call is an error result, not
an exit.

## Access on every call

The account's role at start fixes the tool list: a tool is listed when the role reaches the tool's
access level (`public`, `signedIn`, `staff`, `office`, `admin`; ADR 0005). The process can run for
hours, so every call resolves the account again and checks its current role against the tool's
level. An account disabled or demoted since the start gets an error result on its next call,
nothing runs, and the refusal is audited. A tool that is not listed (for its access level, without
`--personal-data`, or without its write group) fails like an unknown tool when called by name.

## Tools

Every tool of this version only reads (`readOnlyHint`, `idempotentHint`). Those marked "yes" under
"Personal data" are listed only with `--personal-data`. Each tool that mirrors a command returns
that command's JSON document, which is the response body of that command's API operation in
[api/openapi.json](../api/openapi.json); a test asserts that a tool and its command return the same
document. `free_rooms` mirrors no command; its result is described below. No tool returns a date of
birth. Resources and prompts follow in later versions.

| Name              | Access   | Personal data | Write group | Mirrors                                                          |
| ----------------- | -------- | ------------- | ----------- | ---------------------------------------------------------------- |
| `schedule`        | `public` | no            | –           | `lessons list --output json` (getSchedule)                       |
| `free_rooms`      | `staff`  | no            | –           | – (no command; `CatalogService.FreeRooms`)                       |
| `lesson`          | `staff`  | yes           | –           | no command (listLessonParticipants), see below                   |
| `person_bookings` | `office` | yes           | –           | part of `persons export --output json` (exportPerson), see below |
| `person_passes`   | `office` | yes           | –           | `passes list --person --output json` (listPersonPasses)          |
| `receipts`        | `office` | yes           | –           | `receipts list --output json` (searchReceipts)                   |
| `search_persons`  | `office` | yes           | –           | `persons search --output json` (searchPersons)                   |
| `statistics`      | `office` | no            | –           | `stats --output json` (getStatistics), see below                 |
| `db_check`        | `admin`  | no            | –           | `db check --output json`                                         |
| `doctor`          | `admin`  | no            | –           | `doctor --output json`                                           |
| `mail_log`        | `admin`  | yes           | –           | `mail list --output json` (searchMails)                          |

**`free_rooms`**: the rooms free on `date` (a school date, YYYY-MM-DD) from `start` (the school's
wall clock time, HH:MM) for `durationMinutes` (whole minutes, more than 0 and at most 720, which is
12 hours), optionally only those of the location `locationId` (an ID; an unknown one is not found).
`date`, `start` and `durationMinutes` are required. A room is taken while any lesson that is not
canceled overlaps the time: a room rental and a hidden lesson take it too. The tool is for staff
(`staff`): a member would learn from it that a room is taken by a hidden lesson or a rental they do
not see in `schedule`. A lesson that ends when the time starts, or
starts when it ends, leaves the room free. The result is
`{rooms: [{id, name, location: {id, name}}]}`, ordered by location name, then as the location
orders its rooms; the list is empty when no room is free.

**`schedule`**: the lessons from `from` through `to` (school dates, YYYY-MM-DD, both inclusive; by
default today and the next 6 days, at most 42 days), optionally only those of a `location`, a
`courseType`, a `level` or an `instructor` (each an ID). The account's own visibility applies: a
member sees what a member sees in the app, the office also hidden, unreleased and canceled
lessons. The result is the body of getSchedule, with the free places. Without `--personal-data`
each lesson leaves out `bookedFor`, `waitlist` and `renter`; the instructor's name stays, as the
public schedule shows it.

**`lesson`**: the participants of the lesson `lessonId` (an ID from `schedule`), as the
participants page shows them to staff. The office reaches every lesson, an instructor or
accompanist only the lessons they teach or accompany; any other lesson is a not-found error
result. No command mirrors it, so its result is the body of listLessonParticipants: `lessonId`,
`capacity`, `active` (bookings that are not canceled), `held` (places held by waiting-list
invites), `freePlaces`, `waiting`, `dayOverAt` (when the attendance window ends), `bookings` (every
booking, canceled ones included, with `bookingId`, `personId`, `name`, `anonymized`, `guest`,
`contactEmail`, `contactPhone`, `status`, `source`, `trial`, `trialNote`, `late`, `bookedAt`,
`canceledAt` and `payment`) and `waitlist` (every entry, ended ones included, with `entryId`,
`personId`, `name`, `anonymized`, `status`, `position`, `stayOnList`, `joinedAt` and `expiresAt`).
An anonymized person is marked `anonymized`, with the fixed name.

**`search_persons`**: the persons whose name, email or member number contains every word of
`term`, as `persons search` finds them; merged and anonymized persons are left out. The term is
an argument of the call, as MCP has no other channel for it, but it is neither logged nor audited.
The result is the body of searchPersons; its IDs are what the person tools take.

**`person_bookings`**: the bookings and waiting-list entries of the person `personId`. No command
prints only these, so the result is `person` (the person as `search_persons` lists it, with
`anonymized`), and `bookings` and `waitlistEntries` as `persons export` has them. The rest of the
export, the date of birth among it, is left out. A merged person is found too; its bookings moved
to the person it was merged into.

**`person_passes`**: the passes of the person `personId`, as `passes list --person` lists them.

**`receipts`**: the receipts issued from `from` through `to` (school dates, both inclusive), as
`receipts list` takes them: without `to` up to today, without `from` from January 1 of the year of
`to`.

**`statistics`**: the school's figures of a `range`: `last4Weeks` (the default), `last12Weeks` or
`term`, or a custom range with `from` and `to`, as `stats` takes them. The result is the body of
getStatistics; without `--personal-data` it leaves out `topParticipants`, which name persons.

**`db_check`**: the integrity checks of `db check`, all of them or those named in `only` (`passes`,
`capacity`, `tombstones`, `receipt-counters`). The result is the document of
`db check --output json`; a finding names records by their IDs only.

**`doctor`**: the checks of `doctor`. It takes no input. The configuration it checks is the
environment the client starts `mcp` with (`BASE_URL`, `SMTP_HOST`, `VAPID_*` and the rest of
[operations.md](operations.md#configuration-reference)). The result is the document of
`doctor --output json`.

**`mail_log`**: the mail log, as `mail list` lists it: a page of the newest mails, optionally only
those of a `status` (`pending`, `sent`, `failed`) or a `template`; a page's `nextCursor` is the
`cursor` of the next. Bodies are never returned.

## Audit

Every call, reads included, writes an audit event `mcp-tool-called`: the actor, `via`
(`{channel: mcp, osUser}`, the OS user who started the server), the tool's name and the outcome:
`ok`, `forbidden`, `not-found`, `invalid`, `conflict` or `error`. It never records the arguments
and has no target, so the log does not reveal whom a call looked for. A write keeps its own audit
events as well. The audit page shows the event as "Assistant tool called", via MCP.

The audit log keeps the events that name an account for 3 years
([ADR 0016](decisions/0016-privacy-and-account-administration.md)). Once an account has called a
tool, it can be disabled but no longer deleted. Use a dedicated account for the assistant, not a
person's own.

## Privacy

Everything a tool returns goes to the assistant's provider. Without `--personal-data` the tools
return no person data. Turn the flag on only with a data processing agreement with that provider;
see [privacy.md](privacy.md#the-assistant-mcp).
