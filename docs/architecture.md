# Architecture

## Overview

```
 Browser                                   Server (production)
┌──────────────────────────────┐          ┌──────────────────────────────────────────┐
│ React app (web/)             │  HTTP    │ cmd/studio-schedule serve                │
│  openapi-fetch client ───────┼─────────▶│  internal/app.New → chi + Huma (/api/v1) │
│                              │          │   → internal/service → store.Store       │
└──────────────────────────────┘          │        → store/postgres (pgx, sqlc)      │
                                          └──────────────────────────────────────────┘
 Browser (GitHub Pages demo)
┌──────────────────────────────────────────────────────────────────────────────────┐
│ React app ── openapi-fetch(fetch = wasmFetch) ──postMessage──▶ Web Worker        │
│                                                       cookie jar                 │
│                                                       StudioScheduleAPI.fetch    │
│                                                       └▶ app.wasm (cmd/wasm)     │
│                                                          internal/app.New → ...  │
│                                                          → store/memory          │
└──────────────────────────────────────────────────────────────────────────────────┘
```

Both targets run the identical `internal/app.New` handler. Only these pieces differ:

| Concern   | Server                | WASM demo                            |
| --------- | --------------------- | ------------------------------------ |
| Transport | `net/http` server     | `internal/wasmbridge` + Web Worker   |
| Store     | `store/postgres`      | `store/memory` + fictional seed      |
| Mail      | Outbox + SMTP worker  | Outbox only, shown in the UI         |
| PDF       | typst module, wazero  | Same module, browser WebAssembly     |
| Entry     | `cmd/studio-schedule` | `cmd/wasm` (`//go:build js && wasm`) |

## Layers (Go)

- `internal/domain`: entities, typed IDs, enums (roles, statuses), domain errors. No dependencies.
- `internal/service`: use cases. It depends only on interfaces: `store.Store`, `clock.Clock`, the ID
  generator `ids.Source`, password hasher. It returns domain errors.
- `internal/store`: repository interfaces, `InTx`, and the `storetest` contract suite that every
  implementation must pass.
- `internal/httpapi`: Huma operations. It maps domain errors to problem+json and does no business
  logic.
- `internal/auth/password`: argon2id `PasswordHasher` (cheap parameters in the `js` build).
- `internal/mail`: mail templates (`en`, `de`; HTML plus text) and the `Mailer` interface;
  `internal/mail/smtp` is the server-only SMTP sender. Services write mails to the outbox in their
  transaction; a job sends them ([ADR 0004](decisions/0004-accounts-sessions-and-mail.md)).
- `internal/pdf`: the receipt renderer's input format; `internal/pdf/typstwasm` (server only) runs
  the typst WebAssembly module built from `tools/receipt-pdf` with wazero, the demo hands the same
  module to the browser through `internal/wasmbridge` (`service.ReceiptRenderer`,
  [ADR 0012](decisions/0012-passes-and-receipts.md)).
- `internal/app`: wiring and cross-cutting middleware (security headers, recovery, cross-origin
  protection). In `httpapi`, a middleware resolves the client IP (`internal/clientip`, trusting
  `X-Forwarded-For` only from `TRUSTED_PROXIES`), the session middleware resolves the
  `studio_session` cookie to the signed-in account, and the access middleware after it enforces
  each operation's declared access level ([ADR 0005](decisions/0005-rbac-and-impersonation.md)).
- `internal/ratelimit`: in-memory fixed-window limiters for logins and mail-sending requests
  ([ADR 0006](decisions/0006-credentials-rate-limits-and-headers.md)).
- `internal/csvimport`: reads the CSV files of the member import (UTF-8 or Windows-1252 through
  `golang.org/x/text/encoding/charmap`, `;`/`,`/tab detected, DD.MM.YYYY or ISO dates); the
  service maps the columns ([ADR 0015](decisions/0015-import-duplicates-and-merge.md)).

## Transactions and concurrency

`Store.InTx(ctx, mode, fn)` is the only way to touch data.

- **Postgres:** READ COMMITTED transactions. Aggregates that need serialisation (a lesson's
  capacity, a person's passes, the receipt counter of a year) are locked explicitly through repo
  methods (`Lessons().Lock`, `Passes().LockByPersons`), which use `SELECT … FOR UPDATE`. The lock
  order is term → class → lesson → the passes of the persons concerned
  ([ADR 0012](decisions/0012-passes-and-receipts.md)); what checks a person's links, or writes
  something that refers to a person, locks the persons first (`Persons().Lock`, through
  `lockLivePersons`, which refuses a merged person), so the full order is persons → term → class →
  lesson → passes. A merge of two persons locks both, then the terms, classes and lessons of what
  it moves; the member import locks every person its rows may find in one call (in ID order)
  before the first row ([ADR 0015](decisions/0015-import-duplicates-and-merge.md)).
  Serialization and deadlock failures are retried. Constraints are backstops: a partial unique index for active bookings
  (a booking a merge kept as history, `merged_from`, is left out of it),
  `visits_remaining >= 0`, and a unique redemption per booking.
- **Memory:** one mutex for all transactions (so they are serialised). Tables are copied on their
  first write in a transaction; commit swaps the copies in, rollback drops them. The memory store
  checks the same keys, references and delete restrictions as the PostgreSQL schema; a unique key
  it checks on every write has an index table of its own (member numbers: `memberNos`, derived from
  the persons and rebuilt on a snapshot's restore), so a large import stays linear in the demo.
- `InTx` is never nested; a nested call fails with `store.ErrNestedTx`. Helpers receive a
  `store.Tx`. After a failed write the callback must return an error, because PostgreSQL aborts the
  transaction.
- Both stores return instants in UTC with microsecond precision and sort text byte-wise (collation
  `C`), so `storetest` can compare results with `==`.
- `storetest` runs the same contract cases against both stores. The PostgreSQL tests use the
  server in `STUDIO_TEST_DATABASE_URL` or start `postgres:18-alpine` via testcontainers; each test
  gets its own database cloned from a migrated template. Service scenarios that must hold under
  both stores' locks and constraints (the member import, merging persons) live in
  `internal/service/servicetest` and run from the tests of `service` and of the PostgreSQL store.
- A **dry run** (the member import, a merge's preview) is the write's own code in a read-write
  transaction that returns a sentinel error, so the store rolls it back.
- **Schema:** goose migrations in `internal/store/postgres/migrations` are embedded in the binary
  and are the schema of record; sqlc generates the query code from them (`just sqlc`, checked by
  `just check-sqlc`). `serve` uses PostgreSQL when `DATABASE_URL` is set and refuses to start unless
  exactly the embedded migrations are applied (checked read-only; `studio-schedule migrate up`); without it, `serve` runs the memory store
  with the demo data and logs a warning. `studio-schedule seed` loads the demo data into an empty
  database only. A real installation starts with `studio-schedule init` instead: the school's
  settings and its first admin, whose password is set through a printed one-time link, so no mail
  server is needed yet ([ADR 0013](decisions/0013-settings-branding-and-init.md)).

## Time

- The school timezone is a setting (IANA name), chosen at the first setup: it cannot change once
  lessons exist, nor the currency once prices or receipts exist (ADR 0013). Class templates store local wall time (weekday,
  start, end). Lessons store instants (`timestamptz`), computed in Go so that DST is handled
  correctly.
- `time.Local` is forbidden (golangci-lint forbidigo). The WASM build embeds `time/tzdata`.
- Services take the current time from `clock.Clock`, so tests use a fixed clock.
- `domain.Date.At(TimeOfDay, loc)` turns a school date and wall-clock time into an instant (an
  ambiguous time takes the earlier instant, a time in the DST gap moves forward);
  `service.loadSchool` is the one place that resolves the school time zone.
- The release window and the lessons a guest sees are computed on the server only
  ([ADR 0003](decisions/0003-release-window-and-visibility.md)); the schedule response carries
  `releasedUntil`, `nextReleaseAt` and the server's `now`.

## Background jobs

`internal/jobs` runs named jobs on a Go ticker each. `app.Jobs(deps)` registers them:
`generate-lessons` (hourly, over a rolling horizon), `purge-sessions` (hourly, deletes expired
sessions and email links), `waitlist-expiry` (every minute), `announcements` (every minute, mails the
announcements whose publication has come, once, at most 100 mails a run;
[ADR 0014](decisions/0014-announcements-and-mail-log.md)),
`purge-mail` (daily, removes the bodies of mails sent 30 days ago and the mails of 180 days ago) and,
when a mailer is configured, `deliver-mail` (every minute). The server and the WASM build run every job
once at startup (the demo before it answers its first request), then on its interval; Go timers
work in `js/wasm`, so both targets use the same code. A request that leaves work for a job now
wakes it through `Deps.Wake` (a `jobs.Waker` both binaries pass to `app.New` and `app.Jobs`): an
announcement published with email goes out right after the response, not at the next tick. Jobs
must be idempotent: the generator reads
the existing lessons of each class and creates only the missing slots that have not started yet.
Office edits of a class or a term run the same per-class fill inside their own transaction, under
the class lock, after moving or deleting the future lessons the edit affects
([ADR 0007](decisions/0007-timetable-administration.md)).

## Statistics

The office's statistics (`GET /api/v1/admin/statistics`, `service.StatisticsService`) are computed
live from the operational data; there is no snapshot table (ADR 0001). A request reads the lessons
of the range (`Lessons().ListRange`), then one aggregate each: `Bookings().CountsByLessons`
(active, trials, guests, attended, no-shows), `Bookings().CountLateCanceledByLessons`,
`Waitlist().CountByLessons`, `Bookings().CountActivePersons` and `Receipts().TotalsByDay`, besides
the catalog. So a year
(about 1,500 lessons) costs the same handful of queries, which keeps the single-threaded WASM demo
responsive.

- Ranges are school dates in the school time zone: the last 4 or 12 weeks and the current term end
  today; a custom range has at most 366 days (422 beyond). A lesson belongs to the school date it
  starts on, a receipt to its issue date. Periods are weeks from Monday, or calendar months for
  ranges beyond 26 weeks, the first and the last cut to the range.
- Canceled lessons are only counted as such. Utilization is active bookings per place; the no-show
  rate counts only bookings whose attendance was taken; an active member is a person with an
  active booking in the range. Pass sales net cancellations (Storno) on the day they were issued.
- Aggregates only: no person is named; per-instructor rows name the staff member.
- Every lesson counts under its own room, course type and instructor, which the office may have
  changed from its class's; a class row names the course type of its latest lesson in the range.
- The page (`/admin/statistics`) renders recharts through shadcn `chart`; the build puts recharts
  and every module only it uses (decided on the module graph, `web/vite.chunks.ts`) in its own
  `charts` chunk, which only that route loads, and fails if the entry's chunks hold any of them.
  Every chart has its figures in a table.

## API contract

- Huma generates OpenAPI 3.1 from Go types. `just gen-api` writes `api/openapi.json` and generates
  `web/src/api/schema.d.ts` with openapi-typescript. CI fails when either is stale.
- Conventions: base path `/api/v1`, camelCase JSON, explicit camelCase `operationId`s, RFC 9457
  problem+json errors without internal details.
- Cross-site request forgery: Go's `http.CrossOriginProtection` (Sec-Fetch-Site / Origin based) on
  unsafe methods. Sessions are server-side rows behind an HttpOnly, SameSite=Lax cookie that holds
  a random token; only its hash is stored ([ADR 0004](decisions/0004-accounts-sessions-and-mail.md)).
- Authorization: every operation declares an access level (`public`, `signedIn`, `staff`, `office`,
  `admin`), published as `x-access` in the OpenAPI document and enforced by one middleware; a test
  pins the level of every operation ([ADR 0005](decisions/0005-rbac-and-impersonation.md)).
- Credentials: password reset, password change and email change use single-use email links;
  logins and mail-sending requests are rate-limited (429 with `Retry-After`); every response
  carries a Content Security Policy, `nosniff` and `Referrer-Policy`, plus HSTS when `BASE_URL` is
  https ([ADR 0006](decisions/0006-credentials-rate-limits-and-headers.md)).
- Calendar feeds: the one route outside `/api/v1` is `GET /ical/{token}.ics`, an iCalendar document
  (`text/calendar`, written by `internal/ical`) that calendar apps poll with a secret token, built
  from what the token's account may see at the time
  ([ADR 0011](decisions/0011-calendar-feeds.md)).

## Frontend

- React 19, Vite, TypeScript strict, Tailwind v4, shadcn/ui components in `web/src/components/ui`.
- TanStack Router for routing (browser history in production, hash history in the demo), TanStack
  Query for server state, react-hook-form + zod for forms, react-i18next (`en` default, `de`).
- `web/src/api/client.ts` is the only place that knows the transport (`VITE_TRANSPORT=http|wasm`).

## Decision records

Architectural decisions are recorded in [docs/decisions](decisions/).
