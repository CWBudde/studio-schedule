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
- `--personal-data` (bool): also list the tools and resources that return person data. No tool of
  this version returns person data; the flag lists the resource templates `lesson://{id}` (staff)
  and `person://{id}` (office).
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

The account's role at start fixes the lists of tools and resources: one is listed when the role
reaches its access level (`public`, `signedIn`, `staff`, `office`, `admin`; ADR 0005). The process
can run for hours, so every call and every resource read resolves the account again and checks its
current role against the level. An account disabled or demoted since the start is refused on its
next call or read, nothing runs, and the refusal is audited: a call gets an error result, a read a
JSON-RPC error. A tool that is not listed (for its access level, without `--personal-data`, or
without its write group) fails like an unknown tool when called by name, and an unlisted resource
like an unknown one: "Resource not found", not audited.

## Tools

Every tool of this version only reads (`readOnlyHint`, `idempotentHint`) and returns no person
data. Each returns the JSON document of the command it mirrors, which is the response body of that
command's API operation in [api/openapi.json](../api/openapi.json); a test asserts that a tool and
its command return the same document. Prompts follow in later versions.

| Name         | Access   | Personal data | Write group | Mirrors                                          |
| ------------ | -------- | ------------- | ----------- | ------------------------------------------------ |
| `schedule`   | `public` | no            | –           | `lessons list --output json` (getSchedule)       |
| `statistics` | `office` | no            | –           | `stats --output json` (getStatistics), see below |
| `db_check`   | `admin`  | no            | –           | `db check --output json`                         |
| `doctor`     | `admin`  | no            | –           | `doctor --output json`                           |

**`schedule`**: the lessons from `from` through `to` (school dates, YYYY-MM-DD, both inclusive; by
default today and the next 6 days, at most 42 days), optionally only those of a `location`, a
`courseType`, a `level` or an `instructor` (each an ID). The account's own visibility applies: a
member sees what a member sees in the app, the office also hidden, unreleased and canceled
lessons. The result is the body of getSchedule, with the free places. Without `--personal-data`
each lesson leaves out `bookedFor`, `waitlist` and `renter`; the instructor's name stays, as the
public schedule shows it.

**`statistics`**: the school's figures of a `range`: `last4Weeks` (the default), `last12Weeks` or
`term`, or a custom range with `from` and `to`, as `stats` takes them. The result is the body of
getStatistics without `topParticipants`, which name persons.

**`db_check`**: the integrity checks of `db check`, all of them or those named in `only` (`passes`,
`capacity`, `tombstones`, `receipt-counters`). The result is the document of
`db check --output json`; a finding names records by their IDs only.

**`doctor`**: the checks of `doctor`. It takes no input. The configuration it checks is the
environment the client starts `mcp` with (`BASE_URL`, `SMTP_HOST`, `VAPID_*` and the rest of
[operations.md](operations.md#configuration-reference)). The result is the document of
`doctor --output json`.

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

## Audit

Every tool call, reads included, and every resource read writes an audit event `mcp-tool-called`:
the actor, `via` (`{channel: mcp, osUser}`, the OS user who started the server), the tool's name or
the resource's URI and the outcome: `ok`, `forbidden`, `not-found`, `invalid`, `conflict` or
`error`. A template's read records the template (`person://{id}`), never the ID it read. The event
never records the arguments and has no target, so the log does not reveal whom a call looked for. A
read of an unlisted resource fails before it reaches the server's code and writes no event. A write
keeps its own audit events as well. The audit page shows the event as "Assistant tool called", via
MCP.

The audit log keeps the events that name an account for 3 years
([ADR 0016](decisions/0016-privacy-and-account-administration.md)). Once an account has called a
tool or read a resource, it can be disabled but no longer deleted. Use a dedicated account for the
assistant, not a person's own.

## Privacy

Everything a tool or resource returns goes to the assistant's provider. Without `--personal-data`
neither returns person data. Turn the flag on only with a data processing agreement with that
provider; see [privacy.md](privacy.md#the-assistant-mcp).
