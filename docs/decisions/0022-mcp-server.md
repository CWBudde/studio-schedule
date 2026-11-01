# 0022 — MCP server

- Status: accepted
- Date: 2026-11-01
- Amends: [0020](0020-command-line.md) (a second channel on the command layer; the `--as` account
  is checked on every call), [0005](0005-rbac-and-impersonation.md) (audit events record the MCP
  channel)

## Context

An assistant (Claude Desktop, Claude Code or any other MCP client) should be able to work with a
school: answer "who is on the waiting list for Thursday's ballet class?", find a free room, draft
an announcement, run the checks of `doctor` and `db check`. It has to follow the same rules as
every other caller: the account's access level, the services' validation, the audit log.

Phase 11 built the command line on a shared layer (`cmd/studio-schedule/cli`, ADR 0020). It is
local only: a command opens the database and acts as the account of `--as`. There is no remote
mode, and there are no personal access tokens. A command's flags are bound in cobra closures, and
`cli.Spec` has no typed input. What a model may see matters more than what a shell user may see:
the school holds data about minors, and everything a tool returns goes to the assistant's
provider. The six open questions of the plan were each asked and answered before this record. The
plan called this record 0021. That number went to "Levels and class audience" of the Werkstatt
catalogue, which was planned first.

## Decision

### stdio only

The server is the command `studio-schedule mcp`, speaking MCP over stdio. The client starts the
binary. Like every command, it opens the database from `DATABASE_URL` or `--database-url`, checks
the schema, and calls the services that `app.NewServices` builds through `cli.Env`. A client on
another machine starts it over SSH (`ssh host studio-schedule mcp --as …`), which needs nothing
beyond what the command line needs.

There is no streamable HTTP endpoint `/mcp` in `serve`. Such an endpoint would need its own
authentication (OAuth 2.1 as the MCP specification asks, or tokens that do not exist yet), and it
would become part of the API that the PHP backend (Phase 13) must serve too. A remote transport
waits for a remote mode with its own ADR. The WASM demo has no MCP server.

### The actor: `--as`, checked on every call

`mcp` takes `--as <email>` (env `STUDIO_AS`) like an administration command. The account is
resolved at start through `AuthService.Actor`. An unknown, unconfirmed or disabled account exits
with code 3 before the server speaks, as on the command line. The account's role fixes the tool
list for the session: a tool is listed when the role reaches the tool's access level (ADR 0005's
levels). A role that reaches no tool exits with code 4 at start, like a command whose role is too
low.

The process can run for hours, so every tool call resolves the account again (`AuthService.Actor`
refuses a disabled one) and checks its current role against the tool's access level
(`Access.MinRole`). An account that was disabled or demoted since the start loses access on its
next call: the call fails with an error result, and nothing runs. The principal carries `Via = {Channel: "mcp", OSUser: <the OS user>}`,
and every audit event written for it stores that in `AuditDetails.Via`. The admin's audit view
shows "via MCP (<os user>)". `mcp` takes no `--output` and no `--yes`.

### One core, a parity test

A tool is not generated from its command. Each tool has its own typed input struct, whose JSON
schema the SDK derives. It calls the same service method as the command it mirrors and returns the
same document: the operation's response body built with the converters exported from
`internal/httpapi` (`New…`), which is also the command's `--output json`. A test runs each tool and
its command on the same data and asserts that the two documents are equal, so a tool cannot drift
from its command without a failing test. A tool that leaves person data out without
`--personal-data` (the statistics' top participants) returns the body without those fields, and
its parity test compares against the command's JSON without them. A tool without a command (book
or cancel a booking for a person, draft an announcement, the schedule's free rooms) returns the
body of the HTTP operation it mirrors, or documents its result schema in docs/mcp.md, and has no
parity test.

Rejected: deriving both from `cli.Spec`, which would first rework every command for a typed input;
and tools without any parity requirement.

### Read tools by default, writes per group

Read tools are listed for the account's access level without a flag. Write tools are listed only
for the groups that `--allow-writes <group,…>` names: `bookings` (book or cancel for a person),
`lessons` (cancel a lesson, which notifies those booked), `announcements` (create a draft), `mail`
(requeue failed mail) and `jobs` (run a job). An unknown group exits with code 2. Whoever
configures the client decides, in the client's configuration. There is no setting and no
migration for this.

These five groups are the only writes. No tool deletes or disables an account, anonymizes or
merges a person, writes settings, or migrates the database. The announcement tool saves a draft
and never sends one; the office publishes it in the app. `doctor` and `db check`, operator
commands on the shell, are tools for `admin` accounts, because the server always acts as an
account.

### Annotations, no confirmation round trip

Every tool carries the MCP annotations: `readOnlyHint` on reads, `destructiveHint` on a write that
cancels, mails or deletes (cancelling a booking or a lesson, requeueing mail, and running a job
that mails or deletes, although `jobs run` asks no confirmation on the shell), and
`idempotentHint` where a repeat changes nothing. The client asks its user before a write, as Claude Desktop and Claude Code do. The command
line's confirmation (ADR 0020, "Safety") does not apply on this channel: the client's approval
takes the place of `--yes`. A write still goes through the services' rules, validation and audit
events.

Rejected: a preview that returns a one-time token for a second, committing call, which doubles
every write and needs token state; and MCP elicitation, which not every client supports.

### Personal data: `--personal-data`

Without `--personal-data`, only tools that return no person data are listed: the schedule, free
places, rooms free at a time, the statistics totals (without the top participants), `doctor` and
`db check`. Person search, a lesson's participants and waiting list, a person's bookings and
passes, receipts, the mail log and the `person://` resources need the flag, as does any write that
names a person. The office opts in through the client's configuration, per client.

Every tool returns only the fields its question needs: names, contact data only for a tool about
contacting someone, never dates of birth. A tool shows an anonymized person as the API does
(`anonymized`). A search term is an argument of the tool call, since MCP has no other channel for
it, but it is never logged or audited. docs/privacy.md gets a section saying that the assistant's
provider receives what the tools return, and that the operator needs a data processing agreement
with that provider before turning on `--personal-data`.

### Every call is audited

Every tool call, reads included, writes an audit event `mcp-tool-called`: the tool's name, the
actor, its `Via` and the outcome (ok, or the error's kind), never the arguments. It has no target,
so the audit log does not reveal whom a search looked for. A write keeps its own audit events as
well. The action is a new row of `audit_actions` (a migration), a domain constant and the audit
page's strings, built with the server.

### The SDK

The server uses the official `github.com/modelcontextprotocol/go-sdk` (v1.8.0), on the server side
only. Its in-memory transport lets the tests run a real client against the server in-process.
`cmd/wasm` must not depend on it: the server item adds it to the forbidden list of
`check-wasm-deps` (`scripts/check-wasm-deps.sh`).

## Consequences

- An assistant can read a school's schedule and checks, and, with the flags, its people and five
  kinds of writes. It can do no more than the `--as` account.
- Nothing is added to the HTTP API, so the PHP backend (Phase 13) needs no MCP server of its own.
  Its schools get one once a remote mode exists.
- Whoever can run the binary with the database's credentials can start the server as any account,
  as with the command line. The audit log names the OS user and the channel.
- `AuditChannel` gains `mcp`, and `audit_actions` gains `mcp-tool-called`. The audit log grows by
  one row per tool call.
- The go-sdk is a new dependency, in the server binary only.
- Each tool comes with a parity test against its command, and the tools count towards the coverage
  gate (Phase 9).
