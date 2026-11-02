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
- `--personal-data` (bool): also list the tools and prompts that lead to person data. No tool of
  this version returns person data, so the flag lists only the prompt `waitlist_invitations`.
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

The account's role at start fixes the tool and prompt lists: a tool or prompt is listed when the
role reaches its access level (`public`, `signedIn`, `staff`, `office`, `admin`; ADR 0005). The
process can run for hours, so every tool call and prompt get resolves the account again and checks
its current role against the level. An account disabled or demoted since the start gets an error
on its next call or get, nothing runs, and the refusal is audited. A tool that is not listed (for
its access level, without `--personal-data`, or without its write group) fails like an unknown
tool when called by name; a prompt that is not listed fails as `unknown prompt`.

## Tools

Every tool of this version only reads (`readOnlyHint`, `idempotentHint`) and returns no person
data. Each returns the JSON document of the command it mirrors, which is the response body of that
command's API operation in [api/openapi.json](../api/openapi.json); a test asserts that a tool and
its command return the same document. Resources follow in a later version.

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
for the text only: it tells the model to publish nothing, since an announcement tool saves a draft
and never sends it, and the office reviews and publishes it in the app.

**`waitlist_invitations`**: whom to invite from the waiting list of the lesson `lessonId`: the free
places, the places that open invites hold, and the waiting entries next in line. It tells the model
to book or invite no one. It leads to the tool `lesson`, which returns a lesson's bookings and
waiting list; this version has no such tool yet, so the prompt can be listed but not followed
through.

The text is in the school's language, the settings' default locale: `de` or `en`, anything else
English, and English before the school is set up. The German texts address the model with "du",
as the app does. The listed name, title, description and arguments are English, like the tools'.

Invalid arguments (a required one missing, a lesson ID that is not an ID, a week that is not a
date, an argument the prompt does not take) are a JSON-RPC invalid-params error (`-32602`). The
error names the argument, never its value. A refusal and an unexpected error are an internal error
(`-32603`), with the message a tool's error result would have.

## Audit

Every tool call, reads included, and every prompt get writes an audit event `mcp-tool-called`: the
actor, `via` (`{channel: mcp, osUser}`, the OS user who started the server), the tool's or prompt's
name and the outcome: `ok`, `forbidden`, `not-found`, `invalid` (also a get with invalid
arguments), `conflict` or `error`. It never records the arguments and has no target, so the log
does not reveal whom a call looked for. A write keeps its own audit events as well. The audit page
shows the event as "Assistant tool called", via MCP.

The audit log keeps the events that name an account for 3 years
([ADR 0016](decisions/0016-privacy-and-account-administration.md)). Once an account has called a
tool or got a prompt, it can be disabled but no longer deleted. Use a dedicated account for the
assistant, not a person's own.

## Privacy

Everything a tool returns goes to the assistant's provider. Without `--personal-data` the tools
return no person data. Turn the flag on only with a data processing agreement with that provider;
see [privacy.md](privacy.md#the-assistant-mcp).
