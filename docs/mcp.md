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
- [Resources](#resources)
- [Prompts](#prompts)
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
- `--personal-data` (bool): also list the tools, resources and prompts that return or lead to person
  data (the "Personal data" columns of [Tools](#tools), [Resources](#resources) and
  [Prompts](#prompts)), and the statistics' top participants. Read [Privacy](#privacy) first.
- `--allow-writes` (string list, comma-separated): also list the write tools of these groups:
  `bookings`, `lessons`, `mail`, `jobs`. The groups are checked at start: any other, `announcements`
  too, is an unknown group (exit 2). No tool writes an announcement: an announcement has no draft
  state yet ([ADR 0022](decisions/0022-mcp-server.md), amendment of 2026-11-03).

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

The account's role at start fixes the lists of tools, resources and prompts: one is listed when the
role reaches its access level (`public`, `signedIn`, `staff`, `office`, `admin`; ADR 0005). The
process can run for hours, so every call, resource read and prompt get resolves the account again
and checks its current role against the level. An account disabled or demoted since the start is
refused on its next call, read or get, nothing runs, and the refusal is audited: a call gets an
error result, a read or get a JSON-RPC error. A tool that is not listed (for its access level,
without `--personal-data`, or without its write group) fails like an unknown tool when called by
name, an unlisted resource like an unknown one ("Resource not found", not audited), and an unlisted
prompt as `unknown prompt`.

## Tools

A tool without a write group only reads (`readOnlyHint`, `idempotentHint`); a write tool is listed
only with its group in `--allow-writes` ([Write tools](#write-tools)). Those marked "yes" under
"Personal data" are listed only with `--personal-data`. Each tool that mirrors a command returns
that command's JSON document, which is the response body of that command's API operation in
[api/openapi.json](../api/openapi.json); a test asserts that a tool and its command return the same
document. `free_rooms`, `book_lesson` and `cancel_booking` mirror no command; their results are
described below. No tool returns a date of birth.

| Name              | Access   | Personal data | Write group | Mirrors                                                          |
| ----------------- | -------- | ------------- | ----------- | ---------------------------------------------------------------- |
| `schedule`        | `public` | no            | –           | `lessons list --output json` (getSchedule)                       |
| `free_rooms`      | `staff`  | no            | –           | – (no command; `CatalogService.FreeRooms`)                       |
| `lesson`          | `staff`  | yes           | –           | no command (listLessonParticipants), see below                   |
| `book_lesson`     | `office` | yes           | `bookings`  | no command (addLessonParticipant), see below                     |
| `cancel_booking`  | `office` | yes           | `bookings`  | no command (removeLessonParticipant), see below                  |
| `cancel_lesson`   | `office` | no            | `lessons`   | `lessons cancel --yes --output json` (cancelLesson)              |
| `person_bookings` | `office` | yes           | –           | part of `persons export --output json` (exportPerson), see below |
| `person_passes`   | `office` | yes           | –           | `passes list --person --output json` (listPersonPasses)          |
| `receipts`        | `office` | yes           | –           | `receipts list --output json` (searchReceipts)                   |
| `search_persons`  | `office` | yes           | –           | `persons search --output json` (searchPersons)                   |
| `statistics`      | `office` | no            | –           | `stats --output json` (getStatistics), see below                 |
| `db_check`        | `admin`  | no            | –           | `db check --output json`                                         |
| `doctor`          | `admin`  | no            | –           | `doctor --output json`                                           |
| `mail_log`        | `admin`  | yes           | –           | `mail list --output json` (searchMails)                          |
| `requeue_mail`    | `admin`  | yes           | `mail`      | `mail requeue <id> --yes --output json` (requeueMail)            |
| `run_job`         | `admin`  | no            | `jobs`      | `jobs run <name> --output json`, see below                       |

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

### Write tools

A write tool is listed only when `--allow-writes` has its group, and a write tool with person data
only with `--personal-data` too. Called by name without its group, it fails like an unknown tool
and changes nothing. Each calls the service of the operation it mirrors, so the operation's rules,
its validation and its own records apply unchanged: a refusal is an error result with the
service's reason, and nothing is written.

| Group      | Tools                           | What it may change                                                |
| ---------- | ------------------------------- | ----------------------------------------------------------------- |
| `bookings` | `book_lesson`, `cancel_booking` | a person's booking of a lesson, and the mail about it             |
| `lessons`  | `cancel_lesson`                 | a lesson's status, and the mails to those booked                  |
| `mail`     | `requeue_mail`                  | a failed mail of the outbox, sent again by the deliver-mail job   |
| `jobs`     | `run_job`                       | whatever the job does: lessons, mails and pushes, or deleted data |

No tool writes an announcement: an announcement has no draft state yet, so there is no
`announcements` group ([ADR 0022](decisions/0022-mcp-server.md), amendment of 2026-11-03).

**Annotations.** The tool annotations take the place of the command line's `--yes`: the client
reads them and asks its user before a call, and no tool asks for a confirmation itself. A write tool
has `readOnlyHint: false` and sets the others as follows.

| Tool             | `destructiveHint` | `idempotentHint` | `openWorldHint` |
| ---------------- | ----------------- | ---------------- | --------------- |
| `book_lesson`    | false             | true             | false           |
| `cancel_booking` | true              | true             | false           |
| `cancel_lesson`  | true              | true             | false           |
| `requeue_mail`   | true              | false            | false           |
| `run_job`        | true              | false            | true            |

The tools that mail only queue the mails in the outbox, which the server's deliver-mail job sends,
so they stay within the school's data (`openWorldHint: false`). An annotation belongs to a tool,
not to its arguments: `run_job` is destructive because most jobs mail or delete
(`announcements`, `deliver-mail`, `deliver-push` and `waitlist-expiry` mail or push to people;
`purge-mail`, `purge-personal-data` and `purge-sessions` delete), although `generate-lessons` only
writes lessons. Its description names them, and `deliver-mail` and `deliver-push` reach outside
(`openWorldHint: true`).

**`book_lesson`**: books the person `personId` (an ID from `search_persons`) into the lesson
`lessonId` (from `schedule`) as the office does on the participants page: the booking's source is
`staff`, the release window and the booking cutoff do not apply, and the lesson may have started.
Refused: a lesson the school canceled, a room rental, an event's session, a person booked already,
and a full lesson; the tool never books over the capacity, which the app allows the office with a
confirmation. A person whose own waiting-list invite holds a place takes it. Before the lesson
starts, the person's mail recipients get the confirmation. The result is the booking, the body of
addLessonParticipant: `id`, `lessonId`, `personId`, `status`, `source`, `trial`, `bookedAt`,
`canceledAt` and `late`.

**`cancel_booking`**: cancels the active booking of the person `personId` on the lesson `lessonId`
as the office's "remove participant" does: never late, the place goes to the waiting list, and the
person's date is released, so a later change of a class membership does not book them in again.
It works after the start and on a lesson the school canceled. Before the start of a lesson that
takes place, the person's mail recipients get the cancellation. A person without an active booking
on the lesson is not found, so a repeated call changes nothing. The result is the canceled
booking, the body of removeLessonParticipant.

**`cancel_lesson`**: cancels the lesson `lessonId` with an optional `note` (at most 500
characters), as `lessons cancel` does: everyone with an active booking is mailed, the bookings cost
nothing, and the waiting list's invites go back to waiting. A lesson canceled already stays as it
is and nobody is mailed again; a lesson that has started is refused.

**`requeue_mail`**: queues the failed mail `mailId` (from `mail_log`) again, as `mail requeue`
does, recorded as `mail-requeued` with `via` mcp. A mail with a secret link, and a pending or sent
mail, is a conflict. There is no tool for `mail requeue --failed`.

**`run_job`**: runs the job `name` now and waits for it, as `jobs run` does, with the jobs of the
same configuration (deliver-mail only with `SMTP_HOST`, deliver-push only with the `VAPID_*` keys).
The result is the run, `{job, startedAt, endedAt, result, error}`. A run that failed is a result
too, with `result: failed` and its `error`, not an error result: the job ran. An unknown job is not
found (the message lists the jobs), and a job another process runs at the moment is a conflict.

## Resources

A resource is a document the client reads by its URI, for the assistant's context; a template's
URI names a record by its ID. Every resource only reads. A JSON resource is the response body of
the API operation it mirrors, or the document of the command, and a test asserts that the two
agree.

| URI                  | Access     | Personal data | Mirrors                                            |
| -------------------- | ---------- | ------------- | -------------------------------------------------- |
| `school://glossary`  | `public`   | no            | [glossary.md](glossary.md)                         |
| `school://timetable` | `public`   | no            | listClasses of the current term, see below         |
| `school://settings`  | `signedIn` | no            | `settings get --output json`, or getPublicSettings |
| `lesson://{id}`      | `staff`    | yes           | listLessonParticipants                             |
| `person://{id}`      | `office`   | yes           | `persons show --output json` without `birthDate`   |

**`school://glossary`**: the school's words, [glossary.md](glossary.md) as it is, as
`text/markdown`, so the assistant names things as the school and the app do.

**`school://timetable`**: the weekly classes of the current term, the one that includes today, as
the public timetable shows them: the body of listClasses (`application/json`) with weekday, start
time, duration, the IDs of the course type and the room (no names), instructor and accompanist,
audience and places. Retired classes are left out. `classes list --output json` (listAllClasses, office) has the same items with a
`retired` flag, retired classes included. Without a current term the read fails.

**`school://settings`**: the school's settings as the account's role may read them
(`application/json`). For the office and admins it is the document of `settings get --output json`:
every area, booking rules, receipts and passes included. For members and instructors it is the
body of getPublicSettings: the name, legal notice, branding, time zone, language and currency. The
role is the current one, so an account demoted to member reads the public settings.

**`lesson://{id}`**: a lesson's participants and waiting list as the participants page shows them,
the body of listLessonParticipants (`application/json`): every booking, canceled ones included,
with name, contact data, status, trial and payment, the waiting list in its order, and the counts
and free places. The office reads every lesson; an instructor or accompanist only the lessons they
teach or accompany, any other is not found. `{id}` is a lesson ID from `schedule`. No command
mirrors it.

**`person://{id}`**: a person with their guardians, dependents, account, linked staff members, class
memberships and the persons merged into them: the document of `persons show --output json`
(getPerson, `application/json`) without `birthDate`, since no result of the server has a date of
birth. A merged or anonymized person is shown too, as the API shows it.

A failed read is a JSON-RPC error, not an error result: a record that is not found or an `{id}`
that is no ID is invalid params with the reason, a refusal an invalid request.

## Prompts

A prompt is a template of messages that tells the model which tools to call for a question. It
holds no data of its own: a get reads only the school's language and time zone and fills in the
dates, the lesson ID and the limits. The model then calls the tools the prompt names, as the
account, with their own access and person-data rules. The listing follows the tools' rules: the
role at start must reach the prompt's access level, and a prompt that leads to person data is
listed only with `--personal-data`.

| Name                           | Access   | Personal data | Arguments             | Leads to   |
| ------------------------------ | -------- | ------------- | --------------------- | ---------- |
| `weekly_overview`              | `staff`  | no            | `week` (optional)     | `schedule` |
| `canceled_lesson_announcement` | `office` | no            | `lessonId` (required) | `schedule` |
| `waitlist_invitations`         | `office` | yes           | `lessonId` (required) | `lesson`   |

**`weekly_overview`**: a summary of one week's lessons, Monday to Sunday, day by day: the
cancellations, the full lessons and the free places, then the week's totals. `week` is a school
date (YYYY-MM-DD) in the week; by default this week. The prompt has the model call `schedule` for
that week.

**`canceled_lesson_announcement`**: a draft announcement about the canceled lesson `lessonId`: a
title and a plain-text body within the announcements' length limits, and a suggestion whom it is
for. The model finds the lesson with `schedule` and stops if it is not canceled. The prompt asks
for the text only: it tells the model to publish nothing, since no tool saves an announcement, and
the office reviews the text and publishes it in the app.

**`waitlist_invitations`**: whom to invite from the waiting list of the lesson `lessonId`: the free
places, the places that open invites hold, and the waiting entries next in line. It tells the model
to book or invite no one. It leads to the tool `lesson`, which returns a lesson's bookings and
waiting list.

The text is in the school's language, the settings' default locale: `de` or `en`, anything else
English, and English before the school is set up. The German texts address the model with "du",
as the app does. The listed name, title, description and arguments are English, like the tools'.

Invalid arguments (a required one missing, a lesson ID that is not an ID, a week that is not a
date, an argument the prompt does not take) are a JSON-RPC invalid-params error (`-32602`). The
error names the argument, never its value. A refusal and an unexpected error are an internal error
(`-32603`), with the message a tool's error result would have.

## Audit

Every tool call, reads included, every resource read and every prompt get writes an audit event
`mcp-tool-called`: the actor, `via` (`{channel: mcp, osUser}`, the OS user who started the server),
the tool's or prompt's name or the resource's URI, and the outcome: `ok`, `forbidden`, `not-found`,
`invalid` (also a get with invalid arguments), `conflict` or `error`. A template's read records the
template (`person://{id}`), never the ID it read. The event never records the arguments and has no
target, so the log does not reveal whom a call looked for. A read of an unlisted resource fails
before it reaches the server's code and writes no event. A write keeps its own records as
well: `requeue_mail` its `mail-requeued` event with `via` mcp, a booking its booking event and a
canceled lesson its `canceledBy`, both with the account as the actor. The audit page shows the event as "Assistant tool called", via MCP.

The audit log keeps the events that name an account for 3 years
([ADR 0016](decisions/0016-privacy-and-account-administration.md)). Once an account has called a
tool, read a resource or got a prompt, it can be disabled but no longer deleted. Use a dedicated account for the
assistant, not a person's own.

## Privacy

Everything a tool or resource returns goes to the assistant's provider. Without `--personal-data`
neither returns person data. Turn the flag on only with a data processing agreement with that
provider; see [privacy.md](privacy.md#the-assistant-mcp).
