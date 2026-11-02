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
- `--personal-data` (bool): also list the tools that return person data. No tool of this version
  returns person data, so the flag lists nothing more yet.
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

Every tool of this version only reads (`readOnlyHint`, `idempotentHint`) and returns no person
data. Each returns the JSON document of the command it mirrors, which is the response body of that
command's API operation in [api/openapi.json](../api/openapi.json); a test asserts that a tool and
its command return the same document. Resources and prompts follow in later versions.

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
