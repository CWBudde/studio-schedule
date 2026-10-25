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
- `internal/app`: wiring and cross-cutting middleware (security headers, recovery, cross-origin
  protection). In `httpapi`, a middleware resolves the client IP (`internal/clientip`, trusting
  `X-Forwarded-For` only from `TRUSTED_PROXIES`), the session middleware resolves the
  `studio_session` cookie to the signed-in account, and the access middleware after it enforces
  each operation's declared access level ([ADR 0005](decisions/0005-rbac-and-impersonation.md)).
- `internal/ratelimit`: in-memory fixed-window limiters for logins and mail-sending requests
  ([ADR 0006](decisions/0006-credentials-rate-limits-and-headers.md)).

## Transactions and concurrency

`Store.InTx(ctx, mode, fn)` is the only way to touch data.

- **Postgres:** READ COMMITTED transactions. Aggregates that need serialisation (a lesson's
  capacity, a person's passes, the receipt counter of a year) are locked explicitly through repo
  methods (`Lessons().Lock`, `Passes().LockByPersons`), which use `SELECT … FOR UPDATE`. The lock
  order is term → class → lesson → the passes of the persons concerned
  ([ADR 0012](decisions/0012-passes-and-receipts.md)). Serialization and deadlock failures
  are retried. Constraints are backstops: a partial unique index for active bookings,
  `visits_remaining >= 0`, and a unique redemption per booking.
- **Memory:** one mutex for all transactions (so they are serialised). Tables are copied on their
  first write in a transaction; commit swaps the copies in, rollback drops them. The memory store
  checks the same keys, references and delete restrictions as the PostgreSQL schema.
- `InTx` is never nested; a nested call fails with `store.ErrNestedTx`. Helpers receive a
  `store.Tx`. After a failed write the callback must return an error, because PostgreSQL aborts the
  transaction.
- Both stores return instants in UTC with microsecond precision and sort text byte-wise (collation
  `C`), so `storetest` can compare results with `==`.
- `storetest` runs the same contract cases against both stores. The PostgreSQL tests use the
  server in `STUDIO_TEST_DATABASE_URL` or start `postgres:18-alpine` via testcontainers; each test
  gets its own database cloned from a migrated template.
- **Schema:** goose migrations in `internal/store/postgres/migrations` are embedded in the binary
  and are the schema of record; sqlc generates the query code from them (`just sqlc`, checked by
  `just check-sqlc`). `serve` uses PostgreSQL when `DATABASE_URL` is set and refuses to start unless
  exactly the embedded migrations are applied (checked read-only; `studio-schedule migrate up`); without it, `serve` runs the memory store
  with the demo data and logs a warning. `studio-schedule seed` loads the demo data into an empty
  database only.

## Time

- The school timezone is a setting (IANA name). Class templates store local wall time (weekday,
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
sessions and email links) and, when a mailer is configured, `deliver-mail` (every minute). The server and the WASM build run every job
once at startup (the demo before it answers its first request), then on its interval; Go timers
work in `js/wasm`, so both targets use the same code. Jobs must be idempotent: the generator reads
the existing lessons of each class and creates only the missing slots that have not started yet.
Office edits of a class or a term run the same per-class fill inside their own transaction, under
the class lock, after moving or deleting the future lessons the edit affects
([ADR 0007](decisions/0007-timetable-administration.md)).

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
