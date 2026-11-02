# MCP server (Phase 12): implementation plan

> **For agentic workers:** this plan is run by `ccbatch run plan`, one headless session per
> checkbox below, phase by phase (`--prefix 1.`, then `2.`, then `3.`). Each item starts from
> `main` and opens its own pull request; a phase starts only after the previous phase is merged.
> Your item's details are in the section under its checkbox. Before you change anything, read:
>
> - that section;
> - `CLAUDE.md`;
> - [ADR 0022](../../decisions/0022-mcp-server.md), the decision record this plan implements;
> - [ADR 0020](../../decisions/0020-command-line.md);
> - PLAN.md "Phase 12";
> - the files your section names.
>
> Keep the test-first order: write the failing test, see it fail, implement, see it pass, commit.

**Goal:** an assistant (Claude Desktop, Claude Code or any other MCP client) can work with a school
through `studio-schedule mcp`. It follows the same rules as every other caller: the account's access
level, the services' validation and the audit log.

**Architecture:**

- **Transport:** stdio only (ADR 0022). The command `studio-schedule mcp --as <email>` runs on the
  command layer of ADR 0020 (`cmd/studio-schedule/cli`). It opens the database like every command and
  calls the services of `app.NewServices` through `cli.Env`.
- **Server package:** `cmd/studio-schedule/mcpserver`. It holds the server, the tool registry and the
  call wrapper, which on every call:
  - resolves the account again;
  - checks the role against the tool's access level;
  - writes the `mcp-tool-called` audit event.
- **Tool results:** each tool has a typed input and returns the document its command prints with
  `--output json` (the `httpapi.New…` converters). A parity test checks each pair.

**Tech stack:** Go, `github.com/modelcontextprotocol/go-sdk` v1.8.0 (`mcp` package; server, stdio and
in-memory transports), cobra, the memory store and PostgreSQL (testcontainers). The only web change is
two locale strings and the audit page's channel label.

**Spec:** [ADR 0022](../../decisions/0022-mcp-server.md) and PLAN.md Phase 12.

## Global constraints

- **Don't edit PLAN.md.** Parallel pull requests would conflict there. Say in the PR body which
  PLAN.md lines your item closes; they are ticked after the merge.
- **No real data.** Use fictional names and example domains only (`example.org`).
- **Layering:** tools call service methods only. No tool touches `store` directly, there is no SQL
  outside `internal/store/postgres`, and `httpapi` holds no business logic.
- **Server only:** nothing MCP-related may reach `cmd/wasm`. 1.1 puts the SDK on the forbidden list
  of `scripts/check-wasm-deps.sh`, and `just check-wasm-deps` must pass.
- **Personal data (ADR 0022):**
  - Without `--personal-data`, only tools, resources and prompts that return no person data are
    listed, and a direct call by name to an unlisted one fails like an unknown tool.
  - No tool returns a date of birth.
  - A search term or a write's arguments never reach a log line or an audit event.
- **Both stores:** a new or changed repo method gets its `internal/store/storetest` case, run on
  memory and PostgreSQL (`just test-pg`, Docker). After a query change, run `just sqlc`. After a
  repository interface change, regenerate `internal/service/fault_gen_test.go`.
- **Generated files:**
  - After changing a command, run `just cli-docs` (`docs/cli.md`; CI checks it).
  - After an API schema change (e.g. a new `AuditChannel` value), run `just gen-api`.
  - `just check-generated` must pass.
- **Coverage gate:** 95 % total and 80 % per package (`just test-cover`, `just coverage`). A branch no
  input can reach is deleted, never excluded.
- **Lint locally** with golangci-lint v2.14.0, CI's version: `PATH=$HOME/go/bin:$PATH just lint`.
  forbidigo bans `time.Now()` and `fmt.Print*` outside `cmd/`; use `env.Clock` and `slog`.
- **Commits:** Conventional Commits in English, ending with
  `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.
- **Docs:** `docs/mcp.md` lists every tool, resource and prompt in one table per kind. Add your rows
  and keep the order: by access level, then by name.

## Review focus

The inputs and conditions most likely to bite, each with its test in the owning task:

1. **Mid-session change:** an account disabled or demoted after `mcp` started is refused on its next
   call. The call fails with an error result, nothing runs, and an audit row records the refusal.
   Owner: Task 1.1.
2. **Arguments stay private:** no search term or tool argument reaches the log or an audit event.
   Test it by capturing the `slog` output and the audit rows of a call with a distinctive argument
   (`"Zebulon"`) and asserting that the string appears in neither. Owners: Task 1.1 (the wrapper) and
   Task 2.1 (the person search).
3. **Unlisted means uncallable:** without `--personal-data`, every person tool, `person://` resource
   and person prompt is absent from the lists, and a direct call by name fails. Owners: Tasks 2.1
   and 2.3.
4. **Anonymized and merged persons** show as the API shows them (`anonymized`, tombstones left out of
   searches), never with their former name. Owner: Task 2.1.
5. **Writes need their group:** a write tool called by name without its `--allow-writes` group is
   refused, even when the client sends it anyway. Owner: Task 3.1.

---

## Task 1.1

- [x] 1.1 `mcp-server` The `studio-schedule mcp` server over stdio with the call wrapper (actor checked again on every call, `mcp-tool-called` audit, `domain.ChannelMCP`), `--personal-data` and `--allow-writes`, and the first read tools that return no person data — see "Task 1.1".
  - Done: `domain.ChannelMCP`, `AuditMCPToolCalled` (actor, no target, Via mcp, `Tool` and
    `Outcome` both set, and only on it), migration 00036, `AuthService.RecordToolCall`, and the
    audit API's `tool`/`outcome` (`just gen-api`). `cli.Spec.Channel` and `cli.Env.Actor` (exit 3
    after a disable, 4 after a demotion), plus `Env.Stdin`/`Env.Stdout` for the transport.
    `cmd/studio-schedule/diagnose` now computes the `doctor` and `db check` reports for both the
    commands and the tools; the schema errors moved to `cli.ErrSchemaOutdated`/`ErrSchemaNewer`.
    `mcpserver` keeps the plan's names, with three deviations:
    - `Options` also carries `Checks diagnose.Env` (the command's env, for `doctor`; `New` refuses
      nil) and `Logger`;
    - `tool.add` is `func(s *Server, t tool)`, built by the generic `handler[In]`;
    - `ParseWriteGroups` validates `--allow-writes` (a field error, exit 2).

    Tools: `schedule` (public; without `--personal-data` each lesson drops `bookedFor`, `waitlist`
    and `renter`; instructors stay, as the public schedule shows them), `statistics` (office;
    `topParticipants` absent), `doctor` and `db_check` (admin). Member and instructor get
    `schedule`, the office also `statistics`, the admin all four. Every signed-in role reaches
    `schedule`, so the exit 4 for "a role that reaches no tool" cannot happen and has no branch
    (docs/mcp.md says so). Tests: `TestMCP*` in `cmd/studio-schedule/mcp_test.go` drive the real
    `mcp` command through pipes on both stores. They cover the tool list per role and flag,
    the audit row, Review focus 1 (disable and demotion: an error result, nothing ran, one
    `forbidden` row) and Review focus 2 (`"Zebulon"` in five arguments; it is in neither stderr
    nor the audit rows), the command's exit codes 2/3, and parity against `lessons list`,
    `stats`, `doctor` and `db check`. `mcpserver`'s own tests cover the write-group and
    personal-data filters with a test-only registry (no real write or person tools yet), every
    input schema, each outcome kind, a failed audit write, the inputs, and goldens in
    `mcpserver/testdata` (`-update`). Coverage 99.0 % total; `mcpserver` and `mcp.go` at 100 %.

    Discoveries:
    - The SDK's typed `mcp.AddTool` validates the arguments before the handler runs, so a bad call
      would skip the wrapper and its audit row. The tools use `Server.AddTool` and decode the
      arguments only after the account check; a bad call is an audited `invalid` call.
    - A call to an unlisted tool is the SDK's JSON-RPC error `unknown tool`: nothing runs and no
      row is written.
    - The SDK's logger gets no request parameters, so the command shares its stderr logger.
    - Audit events keep their actor (RESTRICT, ADR 0016), so an account that has called a tool can
      be disabled but no longer deleted. docs/mcp.md advises a dedicated account.

    Residual gaps:
    - On PostgreSQL, the refusal row of an account deleted before its first call fails on the
      foreign key; the call is refused all the same, with "internal error" and a log line.
    - SIGINT or SIGTERM ends `mcp` with exit 1 ("context canceled").

PLAN.md Phase 12: "Read tools", the server part ("With them the server itself …") and the tools
without person data. The other read tools are Task 2.1.

**Files:**

- **Create:**
  - `cmd/studio-schedule/mcpserver/server.go`: `New`, `Options` and the `Server` wrapper around
    `*mcp.Server`;
  - `mcpserver/tool.go`: the `tool` type, the registry and the call wrapper;
  - `mcpserver/tools_schedule.go`, `mcpserver/tools_stats.go`, `mcpserver/tools_checks.go`: the
    tools;
  - `cmd/studio-schedule/mcp.go`: the cobra command;
  - the tests: `mcpserver/*_test.go` and `cmd/studio-schedule/mcp_test.go` (the parity tests, next to
    `cliRun`);
  - `docs/mcp.md`;
  - the migration `internal/store/postgres/migrations/00036_mcp_audit.sql`. Use the next free number
    and check `ls internal/store/postgres/migrations` first.
- **Modify:**
  - `internal/domain/audit.go`: `ChannelMCP`, `AuditMCPToolCalled`, its `ActorOptional`/target rules
    (no target) and an `AuditDetails` field for the tool and the outcome;
  - `cmd/studio-schedule/cli/cli.go`: `Spec.Channel`, and `Env.Actor` to resolve the account again;
  - `cmd/studio-schedule/root.go`: one line registers `newMCPCmd`;
  - `scripts/check-wasm-deps.sh`: add `github\.com/modelcontextprotocol` to `forbidden`;
  - `go.mod` and `go.sum`;
  - `web/src/locales/{en,de}/admin.json`: the action's label and `viaMcp` next to `viaCli`;
  - the audit page component that renders `viaCli` (find it with `rg viaCli web/src`);
  - `docs/privacy.md`: a section saying that the assistant's provider receives what the tools return,
    and that the operator needs a data processing agreement before `--personal-data`;
  - `CLAUDE.md`: a code-map bullet "MCP server";
  - `docs/cli.md`, regenerated.

**Interfaces (Produces; Tasks 2.x and 3.1 rely on these exact names):**

```go
package mcpserver

// Options are the flags of `mcp`.
type Options struct {
    PersonalData bool        // --personal-data
    AllowWrites  []WriteGroup // --allow-writes, validated
}

type WriteGroup string // "bookings", "lessons", "announcements", "mail", "jobs"
func WriteGroups() []WriteGroup
func (g WriteGroup) Valid() bool

// Actor resolves the account of --as again (cli.Env.Actor).
type Actor func(ctx context.Context) (service.Principal, error)

// New builds the server for the principal resolved at start: the tools,
// resources and prompts its role reaches under opts.
func New(svc app.Services, actor Actor, start service.Principal, clk clock.Clock, opts Options, version string) (*Server, error)
func (s *Server) Run(ctx context.Context, t mcp.Transport) error
func (s *Server) MCP() *mcp.Server // for the in-memory tests

// tool is one entry of the registry. Every tool file appends its tools to
// the registry with one func: registry = append(registry, scheduleTools...)
// in an init-free list in tool.go: var registry = [][]tool{scheduleTools(), statsTools(), checkTools()}.
type tool struct {
    name         string
    description  string
    access       httpapi.Access
    personalData bool       // listed only with Options.PersonalData
    write        WriteGroup // "" for a read; listed only when the group is allowed
    annotations  mcp.ToolAnnotations
    add          func(s *mcp.Server, h handler) // registers it with its typed input via mcp.AddTool
}

// call runs fn as the current principal: Actor again, MinRole check,
// audit mcp-tool-called with the outcome, never the arguments.
func (s *Server) call(ctx context.Context, t tool, fn func(ctx context.Context, p service.Principal) (any, error)) (*mcp.CallToolResult, error)
```

```go
package cli
// Spec.Channel is the channel of the principal of --as; zero is ChannelCLI.
// Env.Actor resolves --as again through AuthService.Actor with the Spec's
// channel and checks it against the Spec's Access (exit codes as at start).
func (e *Env) Actor(ctx context.Context) (service.Principal, error)
```

```go
package domain
const ChannelMCP AuditChannel = "mcp"
const AuditMCPToolCalled AuditAction = "mcp-tool-called"
// AuditDetails gains: Tool string `json:"tool,omitempty"`; Outcome string `json:"outcome,omitempty"` ("ok" or the error kind)
```

Adjust a signature if the SDK forces it, keep the names, and say so in the PR body: the later tasks
are written against these.

**Steps:**

1. **Domain (test first):** write `internal/domain/audit_test.go` cases.
   - `ChannelMCP` is valid.
   - `AuditMCPToolCalled` with no target is valid, and with a target it is refused.
   - Add the constants and rules, then the migration. It inserts `mcp-tool-called` into
     `audit_actions`, and its Down deletes the events, then the row, like `00032_event_audit.sql`.
   - Run `go test ./internal/domain`, then `just test-pg` for the audit storetest.
   - If `AuditChannel` or the action is in the OpenAPI schema, run `just gen-api`.
2. **CLI layer:** add `Spec.Channel` and `Env.Actor`, with tests in `cli_test.go`:
   - the channel lands in `Principal.Via`;
   - `Env.Actor` after `accounts disable` fails with the exit code 3 error;
   - after a demotion below Access, it fails with exit code 4.
3. **Server and wrapper:** write the in-process tests first, with the go-sdk client
   (`mcp.NewInMemoryTransports`) and both stores like `stores(t, …)` in `clirun_test.go`.
   - **The tool list:**
     - per role: member, instructor, office, admin;
     - with and without `--personal-data`;
     - per `--allow-writes` group. There are no write tools yet, so assert that the group filter works
       with a test-only tool, or leave the write case to 3.1 and say so.
   - **Every tool's input schema** is a valid JSON schema object.
   - **Review focus 1:**
     1. start as office;
     2. disable the account through `env.Services`;
     3. the next call is an error result;
     4. one `mcp-tool-called` row with outcome `forbidden` or the error kind.
        Then the same with a demotion to member.
   - **Review focus 2 (the wrapper):** a call with the argument `"Zebulon"`; capture the slog handler
     and the audit rows; the string is in neither.
   - **The audit row:** actor, `Via{Channel: mcp, OSUser}`, `Tool` and `Outcome`, and no target.
4. **The command `mcp`** (`cmd/studio-schedule/mcp.go`):
   - The Spec has `Access: httpapi.AccessSignedIn`, `Channel: domain.ChannelMCP`, no `--output` and no
     `--yes`.
   - Its flags are `--personal-data` and `--allow-writes` (a string slice, validated in
     `Spec.Validate`: an unknown group is exit code 2).
   - It runs `mcpserver.New(...).Run(ctx, &mcp.StdioTransport{})`. Nothing but MCP may go to stdout;
     logs go to stderr.
   - Tests:
     - an unknown account exits 3 before the server speaks;
     - an unknown group exits 2;
     - a role that reaches no tool exits 4. If every signed-in role reaches the schedule, assert that
       instead and note it in docs/mcp.md.
5. **Tools without person data** (all `readOnlyHint: true`, `idempotentHint: true`):
   - `schedule`:
     - **input:** `{from, to}` school dates, defaults today/+7, at most `service.MaxScheduleDays`;
       `location`, `courseType` optional; whatever the schedule filter takes.
     - **access:** `public`, with the visibility of `ScheduleService.Schedule` for the principal.
     - **result:** the body of `getSchedule` (`httpapi.NewSchedule…`), including `freePlaces`. Without
       `--personal-data` it must contain no person data: check the body for instructor names and
       booked persons, and leave the instructor's name in only if the public API shows it to
       anonymous callers.
     - **parity:** against `lessons list --from --to --output json` as office.
   - `statistics`:
     - **input:** `{range, from, to}` as `stats`.
     - **access:** `office`.
     - **result:** the body of `getStatistics` _without_ the top participants (2.1 adds them with
       `--personal-data`).
     - **parity:** against `stats --output json` with the top participants removed from the command's
       document.
   - `doctor` and `db_check`:
     - **access:** `admin`.
     - **result:** the documents of `doctor --output json` and `db check --output json`.
     - **parity:** against both.
     - Reuse the commands' functions: move what `doctor.go`/`db.go` compute into a function both
       call, and don't copy it.
   - **Golden results:** write them for each tool under `cmd/studio-schedule/mcpserver/testdata/`
     with an `-update` flag, like the CLI goldens.
6. **Docs:**
   - `docs/mcp.md`:
     - what it is;
     - setup for Claude Desktop (a `claude_desktop_config.json` snippet with `command`, `args`, `env`
       `DATABASE_URL`/`STUDIO_AS`);
     - Claude Code (`claude mcp add studio-schedule -- studio-schedule mcp --as office@example.org`);
     - over SSH;
     - the flags;
     - the tool table (name, access, personal data, write group, mirrors);
     - privacy.
   - The `docs/privacy.md` section and the `CLAUDE.md` code map bullet.
   - Run `just cli-docs`.
7. **Verify:**
   - `go build ./...`
   - `just test`
   - `just test-pg`
   - `PATH=$HOME/go/bin:$PATH just lint`
   - `just check-generated`
   - `just check-wasm-deps`
   - `just test-cover && just coverage`
   - `just web-check`

---

## Task 2.1

- [ ] 2.1 `mcp-person-reads` The read tools that need `--personal-data`: a lesson with participants and waiting list, person search, a person's bookings and passes, receipts, the mail log, and the statistics' top participants — see "Task 2.1".

PLAN.md Phase 12, "Read tools": the person part. This task needs Task 1.1 on main.

**Files:**

- **Create:** `cmd/studio-schedule/mcpserver/tools_persons.go`, `tools_lessons.go`,
  `tools_receipts.go`, `tools_mail.go` and their tests; parity tests in
  `cmd/studio-schedule/mcp_persons_test.go`.
- **Modify:** `mcpserver/tool.go` (one registry entry per file), `mcpserver/tools_stats.go` (the top
  participants with `PersonalData`), `docs/mcp.md` (rows).

**Interfaces:**

- **Consumes:** from Task 1.1, `tool`, `registry`, `(*Server).call`, `Options.PersonalData`, the
  in-memory test harness.
- **Produces:** the tool names `lesson`, `search_persons`, `person_bookings`, `person_passes`,
  `receipts`, `mail_log`. Task 3.1 refers to persons by the IDs these return.

**Tools.** All are `personalData: true`, `readOnlyHint`, `idempotentHint`.

- **`lesson`:**
  - **input:** `{lessonId}`.
  - **access:** `staff`. The service's `manages` check applies: an instructor sees only their own
    lessons, and any other lesson is a not-found error result.
  - **result:** the lesson with its participants and waiting list, as the participants operation
    returns them (`service/participants.go`, `httpapi/participants.go`).
  - No command mirrors it, so it has no parity test; document its schema.
- **`search_persons`:**
  - **input:** `{term}`.
  - **access:** `office`.
  - Uses `PersonService` search. The term is never logged or audited (Review focus 2: assert it with
    `"Zebulon"`).
  - **result:** `persons search --output json`, which reads its term from stdin. The parity test feeds
    the term on stdin.
- **`person_bookings` and `person_passes`:**
  - **input:** `{personId}`.
  - **access:** `office`.
  - **result:** what `persons show --output json` and `passes list --person … --output json` print,
    each restricted to its part. Check the commands' flags first. If `persons show` already returns
    both, make one tool `person` that mirrors it, and say so in the PR.
- **`receipts`:**
  - **input:** as `receipts list` (year, number range or person).
  - **access:** `office`.
  - **parity:** against `receipts list --output json`.
- **`mail_log`:**
  - **input:** as `mail list` (status, template, page).
  - **access:** `admin`, like the mail log.
  - **result:** bodies never, as the API gives them.
  - **parity:** against `mail list --output json`.
- **`statistics`:** with `--personal-data`, it includes the top participants, and its parity test
  compares the whole `stats --output json`.

**Tests:**

- **Listing:** none of these tools is listed without `--personal-data`, and a direct call by name
  fails (Review focus 3).
- **Instructor access:** an instructor gets an error result for `lesson` on another instructor's
  lesson.
- **Anonymized and merged persons:** an anonymized person appears as `anonymized` in `lesson` and
  `person_bookings`. A merged person (tombstone) is not found by `search_persons`. (Review focus 4;
  seed them with `PersonService.Anonymize` and `.Merge`.)
- **No birth dates:** no result contains `birthDate`/`dateOfBirth`. Assert it on the JSON of every
  tool's golden.
- **Both stores**, plus the parity tests.

**Verify:** `go build ./...`, `just test`, `just test-pg`, `PATH=$HOME/go/bin:$PATH just lint`,
`just check-generated`, `just test-cover && just coverage`.

---

## Task 2.2

- [x] 2.2 `mcp-free-rooms` A service method for the rooms of a location free at a date, time and duration, and its read tool `free_rooms` — see "Task 2.2".
  - Done: `CatalogService.FreeRooms` and `FreeRoomsQuery` as planned (`internal/service/free_rooms.go`),
    plus `MaxFreeRoomsDuration`. It reads with existing methods only (no repo change): the school's
    zone (`loadSchool`), the rooms list, and per room `ListRoomOverlaps`, the overlap the office
    meets when it places a lesson (`roomFree`), rather than the date range read. By construction
    this also counts a lesson that started the day before and is still running, which no test
    covers on its own. Field errors on `date`, `start` and `duration`, checked before the store is
    touched; an unknown location and a school without settings are not found. Rentals block
    (pinned), and so do hidden lessons.

    The tool `free_rooms` is `public` like `listRooms`, has no person data, and returns
    `{rooms: [{id, name, location: {id, name}}]}`. The location names come from `Catalog.Locations`.

    Tests:
    - `servicetest.FreeRooms` runs on both stores (`TestFreeRoomsScenarios`, memory and PostgreSQL).
      It covers overlap on both edges (back to back is free), canceled and other-date lessons, a
      rental, a hidden lesson, the location filter and order, and an unknown location. The
      instants come from the school's zone, and one case fails if the date is read in UTC.
    - Memory unit tests cover validation, `ErrNoSchool`, and a `SweepFaults` sweep over every
      store call.
    - The tool's tests check each time case against the office's `schedule` of that day. They
      also cover a golden (`testdata/free_rooms.json`), the hidden rental a member cannot see,
      the input errors, the schema, the audit row, and no settings or a failing store. The
      `mcp_test.go` tool lists and Review focus 2 (`"Zebulon"` in each of the four arguments)
      include it.

    Coverage is 99.0 % total. `free_rooms.go`, `tools_rooms.go` and `mcpserver` are at 100 %.

    Deviations and discoveries:
    - `date`, `start` and `durationMinutes` are required. A missing `start` would otherwise
      decode to 00:00, a valid time.
    - `durationMinutes` is its own type `minutes`. With a plain `int`, the decoder's error
      repeats a non-integer value, which `decode` promises never to do.
    - The minutes are clamped before the multiplication: 2^53+60 minutes would otherwise
      overflow to a valid 60 minutes.
    - `handler`'s doc no longer says "plain string fields".
    - The tool shows a member that a room is taken by a lesson they cannot see (a rental or a
      hidden lesson), but never which lesson or who. docs/mcp.md says so.

PLAN.md Phase 12, "Read tools": "rooms free at a time". This task needs Task 1.1 on main.

**Files:**

- **Create:**
  - `internal/service/free_rooms.go` and `free_rooms_test.go`;
  - `cmd/studio-schedule/mcpserver/tools_rooms.go` and its test.
- **Modify:** `mcpserver/tool.go` (one registry entry), `docs/mcp.md` (a row and the result schema).

**Interfaces:**

- **Consumes:** from Task 1.1, `tool`, `registry`, `(*Server).call`.
- **Produces:**
  ```go
  // FreeRooms returns the rooms of location (all locations when zero) that
  // no lesson that is not canceled overlaps on date from start for duration.
  // Ordered as the rooms' positions.
  func (s *CatalogService) FreeRooms(ctx context.Context, q FreeRoomsQuery) ([]domain.Room, error)
  type FreeRoomsQuery struct {
      Date     domain.Date
      Start    domain.TimeOfDay
      Duration time.Duration // > 0, at most 12 h
      Location domain.LocationID // optional
  }
  ```
  Put it on the service that owns rooms. Find it with `rg "func .*Rooms\(" internal/service`, and if
  that is not `CatalogService`, use that service and say so.

**Steps:**

1. Write the service tests first, on both stores via `internal/service/servicetest` or the service
   tests' store helper.
   - **Overlap:** a lesson 18:00–19:00 in room A. A query for 18:30, 60 min excludes A, and a query
     for 19:00, 60 min includes A (back to back is free).
   - **Excluded lessons:** a canceled lesson does not block. A lesson on another date does not block.
   - **Location filter:** it applies.
   - **Validation:** a zero or > 12 h duration is a field error on `duration`; an invalid date is one
     on `date`.
   - **Rentals:** decide whether a rental lesson (a lesson with a rental kind) blocks the room. Read
     `domain.Lesson`; rentals occupy a room, so it should block. Pin that decision with a test.
2. Implement it with existing reads: the rooms list and the lessons of that date (the lesson repo's
   date range read). Only if no existing read fits, add a repo method with storetest cases on both
   stores.
3. **The tool `free_rooms`:**
   - **input:** `{date, start "HH:MM", durationMinutes, locationId?}`.
   - **access:** `public`; rooms are public in the schedule. If they aren't, use `signedIn` and say
     why.
   - `personalData: false`, `readOnlyHint`.
   - **result:** `{rooms: [{id, name, location}]}`, documented in `docs/mcp.md`.
   - It has no parity test (no command); write a golden.

**Verify:** `go build ./...`, `just test`, `just test-pg`, `PATH=$HOME/go/bin:$PATH just lint`,
`just check-generated`, `just test-cover && just coverage`.

---

## Task 2.3

- [ ] 2.3 `mcp-resources` MCP resources: the school settings, the current term's timetable, the glossary, and the templates `lesson://{id}` and `person://{id}` — see "Task 2.3".

PLAN.md Phase 12, "Resources". This task needs Task 1.1 on main.

**Files:**

- **Create:** `cmd/studio-schedule/mcpserver/resources.go` and its test.
- **Modify:**
  - `mcpserver/server.go`, which adds the resources in `New`, filtered like the tools: access level
    and `PersonalData`;
  - `docs/mcp.md`, a resources table.

**Interfaces:**

- **Consumes:** from Task 1.1, `New`, `Options`, `Actor`, `(*Server).call` (so a resource read is
  checked and audited like a tool call; `Tool` holds the resource URI's template, never the ID).
- **Produces:** the URIs `school://settings`, `school://timetable`, `school://glossary`,
  `lesson://{id}` and `person://{id}`.

**Resources:**

- **`school://settings`:**
  - The public settings: name, time zone, currency, languages and booking rules, as `getPublicSettings`
    or `settings get --output json` returns them for the account's level.
  - **access:** `signedIn`; office-only areas only for office.
  - `application/json`.
- **`school://timetable`:**
  - The current term's classes with weekday, time, room, course type and audience, as
    `classes list --output json` (parity).
  - **access:** `public` if the public schedule shows classes, else `office`. Check the operation's
    access and use it.
- **`school://glossary`:**
  - `docs/glossary.md` embedded with `go:embed`, as `text/markdown`, so the model uses the school's
    words.
  - `docs/` is outside the package: embed it through a small exported package or copy it with a
    generator check. Prefer a `docs/glossary.go` `package docs` with `//go:embed glossary.md` if the
    module layout allows it.
- **`lesson://{id}`** (template):
  - The same document as the `lesson` tool of Task 2.1 when it exists; else the schedule lesson.
  - **access:** `staff` with the service's `manages` check.
  - `personalData: true`.
- **`person://{id}`** (template):
  - `persons show --output json`.
  - **access:** `office`.
  - `personalData: true`.

**Tests:**

- **Listing:** per role and per `--personal-data`. Without the flag, neither template is listed, and
  reading `person://<id>` directly fails (Review focus 3).
- **Audit:** a read writes one `mcp-tool-called` row whose `Tool` is the template (`person://{id}`),
  not the concrete URI.
- **Parity** for the timetable and `person://`, plus goldens.

**Verify:** `go build ./...`, `just test`, `just test-pg`, `PATH=$HOME/go/bin:$PATH just lint`,
`just check-generated`, `just test-cover && just coverage`.

---

## Task 2.4

- [ ] 2.4 `mcp-prompts` MCP prompts in the school's language: "weekly overview", "whom to invite from the waiting list", "draft an announcement for a canceled lesson" — see "Task 2.4".

PLAN.md Phase 12, "Prompts". This task needs Task 1.1 on main.

**Files:**

- **Create:**
  - `cmd/studio-schedule/mcpserver/prompts.go`;
  - `mcpserver/prompts/{en,de}.json` (the texts) or a Go map;
  - the tests.
- **Modify:** `mcpserver/server.go` (adds the prompts in `New`), `docs/mcp.md` (a prompts table).

**Interfaces:**

- **Consumes:** from Task 1.1, `New`, `Options`, `(*Server).call` (a prompt get is checked and
  audited like a tool call).
- **Produces:** the prompt names `weekly_overview`, `waitlist_invitations` and
  `canceled_lesson_announcement`.

**Prompts:**

- **What a prompt is:** a template of messages that tells the model which tools to call. It contains
  no data of its own, so it reads nothing but the school's language. The language is the settings'
  UI language, `de` or `en`, through the settings service; any other value falls back to `en`.
- **`weekly_overview`:**
  - **args:** `week` (a date in the week, optional).
  - **access:** `staff`.
  - It asks for a summary of that week's lessons via `schedule`: cancellations, full lessons, free
    places.
- **`waitlist_invitations`:**
  - **args:** `lessonId`.
  - **access:** `office`.
  - `personalData: true` (it leads the model to the waiting list).
  - It asks whom to invite, using `lesson`.
- **`canceled_lesson_announcement`:**
  - **args:** `lessonId`.
  - **access:** `office`.
  - It asks for a draft announcement text for the canceled lesson, in the school's language. It
    reminds the model that the announcement tool only saves drafts and that the office publishes them.
- **Wording:**
  - German texts use "du" or "Sie" consistently with `web/src/locales/de`; check which one it uses.
  - No real names in examples.

**Tests:**

- **Listing:** per role and `--personal-data` (`waitlist_invitations` is absent without it).
- **Language:**
  - with a German school the text is German, and with English it is English;
  - golden texts for both languages;
  - a missing required argument is an error.
- **Audit:** a prompt get writes one `mcp-tool-called` row.

**Verify:** `go build ./...`, `just test`, `just test-pg`, `PATH=$HOME/go/bin:$PATH just lint`,
`just check-generated`, `just test-cover && just coverage`.

---

## Task 3.1

- [ ] 3.1 `mcp-writes` The write tools, listed per `--allow-writes` group: book or cancel for a person, cancel a lesson with notification, create an announcement draft, requeue failed mail, run a job — see "Task 3.1".

PLAN.md Phase 12, "Write tools (opt-in)". This task needs Tasks 1.1 and 2.1 on main.

**Files:**

- **Create:** `cmd/studio-schedule/mcpserver/tools_writes_bookings.go`, `…_lessons.go`,
  `…_announcements.go`, `…_mail.go`, `…_jobs.go` and their tests; parity tests where a command exists,
  in `cmd/studio-schedule/mcp_writes_test.go`.
- **Modify:** `mcpserver/tool.go` (the registry entries), `docs/mcp.md` (rows, and a section on
  writes and annotations).

**Interfaces:**

- **Consumes:** from Task 1.1, `tool.write`, `WriteGroup`, `Options.AllowWrites`, `(*Server).call`;
  from Task 2.1, the person IDs that `search_persons` returns.
- **Produces:** the tool names `book_lesson`, `cancel_booking`, `cancel_lesson`,
  `draft_announcement`, `requeue_mail` and `run_job`.

**Tools.** Each is listed only when its group is in `--allow-writes`. The service's rules, validation
and audit events apply unchanged.

| tool                                            | group         | access | personal data                  | annotations                                           | mirrors                                                                                                             |
| ----------------------------------------------- | ------------- | ------ | ------------------------------ | ----------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------- |
| `book_lesson` `{lessonId, personId}`            | bookings      | office | yes                            | idempotent                                            | `BookingService` as the office books for a person. No command, no parity                                            |
| `cancel_booking` `{lessonId, personId}`         | bookings      | office | yes                            | destructive, idempotent                               | `BookingService` cancel. No command, no parity                                                                      |
| `cancel_lesson` `{lessonId, note?}`             | lessons       | office | no                             | destructive, idempotent                               | `lessons cancel --yes` (parity; notifies those booked)                                                              |
| `draft_announcement` `{title, body, audience…}` | announcements | office | no                             | not destructive                                       | `AnnouncementService` create as a draft: it never sets a send time, and the office publishes. No command, no parity |
| `requeue_mail` `{mailId}`                       | mail          | admin  | yes (the log names recipients) | destructive                                           | `mail requeue <id> --yes` (parity)                                                                                  |
| `run_job` `{name}`                              | jobs          | admin  | no                             | destructive for a job that mails or deletes, else not | `jobs run <name>` (parity)                                                                                          |

- **What the client approves:** the annotations replace the CLI's `--yes` (ADR 0022). No tool asks
  for confirmation itself.
- **Announcements: an open decision.** `domain.Announcement` has no draft state today. `PublishAt` is
  required and the announcement shows from then on, yet ADR 0022 says the tool "saves a draft and
  never sends one; the office publishes it in the app". Until the plan here names the chosen draft
  model, leave `draft_announcement` out and say so in the PR. That model would be a domain field, a
  migration and a web "publish" action. Never invent a draft state, and never create an announcement
  that publishes.
- **Which jobs:** tell a mailing or deleting job from the job list (`app.Jobs`). Mark it `destructive`
  per job in a small map next to the tool, and test the map against `app.Jobs` so a new job cannot
  be forgotten.

**Tests:**

- **Listing:**
  - the tool list per group: `--allow-writes bookings` lists exactly the two booking tools;
  - with no groups, no write tool is listed;
  - a write tool called by name without its group fails, and nothing changed in the store
    (Review focus 5).
- **Personal data:** without `--personal-data`, the booking tools and `requeue_mail` are not listed
  even with their group.
- **Booking rules:** `book_lesson` on a full lesson returns the service's error as an error result,
  and the booking count is unchanged.
- **Audit:** every write leaves its own audit events (e.g. `mail-requeued`) with
  `Via.Channel == mcp`, plus the `mcp-tool-called` row.
- **Draft announcement:** after `draft_announcement`, the `announcements` job sends nothing.
- **Both stores**, plus the parity tests for `cancel_lesson`, `requeue_mail` and `run_job`.

**Verify:** `go build ./...`, `just test`, `just test-pg`, `PATH=$HOME/go/bin:$PATH just lint`,
`just check-generated`, `just test-cover && just coverage`.
