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
- `internal/app`: wiring and cross-cutting middleware (security headers, request IDs, recovery,
  access log and metrics, cross-origin protection; see "Observability") and the health probes. In
  `httpapi`, a middleware resolves the client IP (`internal/clientip`, trusting
  `X-Forwarded-For` only from `TRUSTED_PROXIES`), the session middleware resolves the
  session cookie (`__Host-studio_session` over HTTPS, `studio_session` otherwise; ADR 0004) to the
  signed-in account, and the access middleware after it enforces
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
  `lockLivePersons`, which refuses a merged or anonymized person), so the full order is persons → term → class →
  lesson → passes. A merge of two persons locks both, then the terms, classes and lessons of what
  it moves; the member import locks every person its rows may find in one call (in ID order)
  before the first row ([ADR 0015](decisions/0015-import-duplicates-and-merge.md)). Anonymizing a
  person locks it, its guardians and dependents (and every admin's person when its account is an
  admin's) in one call through `lockPersonSet`, then the terms, classes and lessons of what it
  cancels; the account administration locks the account's person, and every admin's with it when
  the account is an admin's, so the last-admin guard sees concurrent changes
  ([ADR 0016](decisions/0016-privacy-and-account-administration.md)).
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
  both stores' locks and constraints (the member import, merging persons, anonymizing, the account
  administration and the retention of personal data) live in
  `internal/service/servicetest` and run from the tests of `service` and of the PostgreSQL store.
- A **dry run** (the member import, a merge's preview) is the write's own code in a read-write
  transaction that returns a sentinel error, so the store rolls it back.
- **Schema:** goose migrations in `internal/store/postgres/migrations` are embedded in the binary
  and are the schema of record; sqlc generates the query code from them (`just sqlc`, checked by
  `just check-sqlc`). `serve` uses PostgreSQL when `DATABASE_URL` is set and refuses to start unless
  exactly the embedded migrations are applied (checked read-only; `studio-schedule migrate up`); without it, `serve` refuses to start unless
  `DEMO_MODE=true`, and then runs the memory store with the demo data and logs a warning. `studio-schedule seed` loads the demo data into an empty
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
  `releasedUntil`, `nextReleaseAt` and the server's `now`. `service.ComputeReleaseWindow` is the one
  place that computes it, the optional manual `Settings.ReleasedThrough` date included (it only
  extends the window; ADR 0003, amendment 2026-10-31).

## Background jobs

`internal/jobs` runs named jobs on a Go ticker each. `app.Jobs(deps)` registers them:
`generate-lessons` (hourly, over a rolling horizon), `purge-sessions` (hourly, deletes expired
sessions and email links), `waitlist-expiry` (every minute), `announcements` (every minute, mails the
announcements whose publication has come, once, at most 100 mails a run;
[ADR 0014](decisions/0014-announcements-and-mail-log.md)),
`purge-mail` (daily, removes the bodies of mails sent 30 days ago and the mails of 180 days ago),
`purge-personal-data` (daily, anonymizes the guests of lessons that ended 12 months ago and deletes
audit events older than 3 years and import runs older than 1 year;
[ADR 0016](decisions/0016-privacy-and-account-administration.md)) and,
when a mailer is configured, `deliver-mail` (every minute), and with a push sender, `deliver-push`
(every minute, and woken when a request queues a notification;
[ADR 0017](decisions/0017-pwa-and-web-push.md)). The server and the WASM build run every job
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
`Waitlist().CountByLessons`, `Bookings().CountActivePersons`, `Bookings().CountsByPersons` (active,
attended and late-canceled bookings by person) with one `Persons().ListByIDs` of the persons who
booked, and `Receipts().TotalsByDay`, besides the catalog. So a year
(about 1,500 lessons) costs the same handful of queries, which keeps the single-threaded WASM demo
responsive.

- Ranges are school dates in the school time zone: the last 4 or 12 weeks and the current term end
  today; a custom range has at most 366 days (422 beyond). A lesson belongs to the school date it
  starts on, a receipt to its issue date. Periods are weeks from Monday, or calendar months for
  ranges beyond 26 weeks, the first and the last cut to the range.
- Canceled lessons are only counted as such. Utilization is active bookings per place; the no-show
  rate counts only bookings whose attendance was taken; an active member is a person with an
  active booking in the range. Pass sales net cancellations (Storno) on the day they were issued.
- Aggregates, besides two lists of names: per-instructor rows name the staff member, and the top
  participants are the 20 persons with the most active bookings in the range (attended and
  late-canceled bookings beside), ties by family name, given name and ID. Merged persons
  (tombstones, ADR 0015) and anonymized ones (ADR 0016) are left out, guests have no person. The
  page links each to the office's person page.
- Every lesson counts under its own room, course type and instructor, which the office may have
  changed from its class's; a class row names the course type of its latest lesson in the range.
- The page (`/admin/statistics`) renders recharts through shadcn `chart`; the build puts recharts
  and every module only it uses (decided on the module graph, `web/vite.chunks.ts`) in its own
  `charts` chunk, which only that route loads, and fails if the entry's chunks hold any of them.
  Every chart has its figures in a table.

## Observability

`app.New` wraps the router in this order, outermost first: security headers, request ID, access
log and metrics, panic recovery, cross-origin protection. The access log wraps the recovery, so a
panicking request is logged and counted as the 500 it answers (one that aborts the connection,
`http.ErrAbortHandler`, as a 500 too).

- **Request IDs** (`internal/requestid`): every request gets an ID, the `X-Request-Id` of the
  request if the direct peer is one of `TRUSTED_PROXIES` and the value is sane (at most 128
  letters, digits and `-._~:+/=@`), otherwise a new UUIDv7 (`Deps.RequestIDs`, separate from the
  entity IDs). It is echoed in the `X-Request-Id` response header, carried in the request's
  context, added as `requestId` to every log line logged with that context (a `slog` handler
  wrapper, so the access log, the recovery log and any service log name it) and to every
  problem+json body (`httpapi.ErrorModel`, which replaces Huma's error type and keeps its schema
  name; the problems written outside Huma carry it too).
- **Access log:** one line per request: method, path, status, duration and request ID. The API
  (its routes, and the API paths that match none) and every 5xx log at Info; the health probes and
  the web app's pages and files (the requests that fall through to `Deps.UI`, a dozen per visit) at
  Debug.
- **Secrets in paths** (`internal/app/redact.go`): a path parameter named `token` or `…Token`
  (`/ical/{token}.ics`, `/api/v1/waitlist-invites/{token}`) is a secret: the path of a matched
  route is logged rebuilt from its pattern with that parameter as `<redacted>` and the others as
  sent; a path that matches no route loses everything after the static prefix of any route with
  a secret parameter (`/ical/<redacted>`). The recovery log and the 404 detail use the same
  paths. `TestNoSecretPathInLogs` requests every route of the OpenAPI document and near misses
  and fails if a secret value reaches the log or a response.
- **Health probes:** `/healthz` (always 200 `{"status":"ok"}` while the process serves) and
  `/readyz` (200 when `Deps.Ready` passes within 3 s, otherwise a 503 problem; the cause is only
  logged). Both answer GET and HEAD and are plain router routes outside the API and its OpenAPI
  document, public, `Cache-Control: no-store`. Since `/readyz` is public, a result answers the
  probes for 2 s and concurrent probes share one check, so a flood costs one check per 2 s; a
  change shows within 2 s (plus the 3 s timeout for a hanging database). It logs the change to not
  ready (Warn, with the cause) and back (Info), not every failing probe. The server passes
  `postgres.Store.Ready`: the same schema check as `serve` at startup (`checkSchema`: every
  embedded migration applied, none unknown, with the versions from goose's provider), on the
  store's pool. The
  memory store (demo, `serve` with `DEMO_MODE=true`) has no check and is always ready. The web demo
  serves the probes too but never asks. `/api/v1/health` stays as the API's own version check.
- **Metrics** (`Deps.Metrics`, no-op by default): the access log reports every request with the
  pattern of the matched chi route (`/api/v1/lessons/{lessonId}`, or `other` for the web app's pages
  and unknown paths, so the label stays bounded), the method (`OTHER` for non-standard ones) and the
  status; `app.Jobs` reports every job run with its duration and result (a panic is a failure).
  The Prometheus implementation is in `cmd/studio-schedule/metrics.go` only (`check-wasm-deps`
  keeps `client_golang` out of the demo). With `METRICS_ADDR` (`--metrics-addr`) `serve` starts a
  second HTTP server that serves `/metrics` and nothing else, never on the public listener, and
  shuts it down with the main one. `METRICS_ADDR` may not take the socket of `HTTP_ADDR` (the same
  port on the same host, or on any host where either listens on every interface). If the metrics
  server cannot listen, `serve` logs an error and serves on without metrics; only the public
  server's failure ends the process. Besides the request and job metrics it reports the outbox's
  pending and failed mails, counted at scrape time (`app.UnsentMails`, one query per scrape that
  reads the partial indexes `outbox_mails_due_idx` and `outbox_mails_failed_idx` only), the build
  version, and Go's runtime and process collectors.

## Privacy

What the school stores about a person, and how it goes
([ADR 0016](decisions/0016-privacy-and-account-administration.md)):

- **Export**: `service.PersonService.Export` (office) and `service.FamilyService.Export` (a member,
  for their family) collect one document in one read-only transaction (`service/export.go`); the
  web app saves the API's JSON answer as a file, so the demo needs no download route.
- **Anonymization**: `service.PersonService.Anonymize` (`service/anonymize.go`) erases the person's
  data (`domain.Person.Anonymize`), removes its links, memberships, entries, staff links and
  account, cancels its future bookings through the ledger, and keeps its history; receipts are
  never touched. `PersonRepo.List` leaves anonymized persons out, `lockLivePersons` refuses them.
- **Accounts**: `service/accounts_admin.go` (disable, enable, role, delete; deleting one's own
  account). The last-admin guard counts the admins who can sign in (`mayActAs`); ending an
  account's sessions (`endSessions`: disabling, deleting, a new password) audits each running
  impersonation as ended. `AccountRepo.Delete` follows the foreign keys of migration 00023: it deletes what is
  the account's, clears it where records only name it, re-points the audit events about it to its
  person, and fails (409 `in-use`) while receipts, the audit log, announcements or duplicate
  dismissals name it.
- **Retention**: `service.RetentionService` behind the `purge-personal-data` job, push
  subscriptions and messages included (ADR 0017).

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
- Calendar feeds: the one API route outside `/api/v1` is `GET /ical/{token}.ics`, an iCalendar document
  (`text/calendar`, written by `internal/ical`) that calendar apps poll with a secret token, built
  from what the token's account may see at the time
  ([ADR 0011](decisions/0011-calendar-feeds.md)).

## Frontend

- React 19, Vite, TypeScript strict, Tailwind v4, shadcn/ui components in `web/src/components/ui`.
- TanStack Router for routing (browser history in production, hash history in the demo), TanStack
  Query for server state, react-hook-form + zod for forms, react-i18next (`en` default, `de`).
- `web/src/api/client.ts` is the only place that knows the transport (`VITE_TRANSPORT=http|wasm`).
- PWA ([ADR 0017](decisions/0017-pwa-and-web-push.md)): the service worker `web/src/sw.ts`
  (vite-plugin-pwa, `injectManifest`) precaches the app shell and shows push notifications;
  `web/src/pwa.ts` registers it and offers updates. Offline, `PersistQueryClientProvider` restores
  an allow-listed copy of the query cache from IndexedDB (`web/src/lib/offlineCache.ts`), queries
  pause (`watchOnline`, `useOnline`), and `transportFetch` refuses writes. The account page's
  `PushCard` subscribes the device (`web/src/features/push`).

## Themes

The look is the school's, the same for every visitor
([ADR 0018](decisions/0018-themes-and-layouts.md)): `domain.Theme` on the branding card is
`programme` (the app's own look, the zero value) or `werkstatt` (the previous dance-school app's
visual language), and the public settings carry it, so a visitor gets it before signing in. The
background image is a setting of its own, stored like the receipt logo; only Werkstatt shows it.

- **Slots:** `web/src/themes/registry.ts` lists the themes, `slots.ts` types their layout slots
  (the shell, the schedule list, its day heading and lesson card, home, profile, the lesson
  participants). A page computes its view model and its actions (book, cancel, filters, forms) once
  and renders `useSlot('…')`; a slot neither fetches nor mutates. Programme provides every slot; a
  slot another theme leaves out is Programme's, so every other page keeps Programme's structure in
  that theme's colors and fonts.
- **Loading:** Programme is in the entry chunk. Werkstatt is a chunk of its own
  (`import('./werkstatt')`) with its CSS and font, its rules scoped under `[data-theme='werkstatt']`;
  the build guard in `web/vite.chunks.ts` fails when the entry loads any of it. The school's accent
  (`html:root`) still overrides the primary color; the text contrast of Werkstatt's tokens
  (≥ 4.5:1, light and dark) is pinned in `werkstatt/tokens.test.ts`.
- **Switching:** `web/public/theme.js` sets the theme cached by the last visit as
  `<html data-theme>` before the first paint, so a returning visitor sees no flash of the other
  look. `ThemeProvider` (in `RootLayout`, so bare routes drop it) then follows the public settings:
  a changed theme takes over without a reload, with a loading state while its chunk loads, and a
  theme that fails to load (offline, say) is replaced by Programme and not cached.
- **Demo:** the web demo offers one fictional school per theme ([demo-mode.md](demo-mode.md), "Two
  demo schools"); `web/e2e/werkstatt.spec.ts` drives the Werkstatt one.

## Command line

The binary's commands administer a school from the server's shell
([ADR 0020](decisions/0020-command-line.md)); there is no remote mode. `cmd/studio-schedule/cli` is
the command layer: a command is a `cli.Spec` (its use, flags, access level, whether it is
destructive, what it prints as JSON), and the layer loads the configuration, opens the database
after checking its schema, builds the services with `app.NewServices` (the same as `app.New`'s, so
a command and its HTTP operation share their rules, validation and audit), resolves the account of
`--as` (or `STUDIO_AS`) through `service.AuthService.Actor` and checks its role against the access
level. The principal carries `Via` (the command line and the OS user), which every audit event
written for it stores in `AuditDetails.Via`. The layer also presents the result (text, or one JSON
document with `--output json`), maps errors to exit codes 0–4 and stderr (`ExitCode`, `Report`),
asks before a destructive change (`Env.Confirm`, `--yes`) and reads secrets and search terms from
stdin (`Env.ReadLine`). The commands stay thin, one file per group in `cmd/studio-schedule`, each
added with one line in `root.go`. Tests run the tree in-process (`cliRun`) on the memory store and
PostgreSQL, with golden files for the output, and [cli.md](cli.md), the reference, is generated
from the tree (`just cli-docs`); CI fails when it is stale, as for `api/openapi.json`.

## Testing

- **Layers:** each package tests its own code. The stores share the `storetest` contract suite
  (every repository method, on memory and on PostgreSQL), and service scenarios that need real
  locks and constraints live in `internal/service/servicetest` (see "Transactions and
  concurrency"). `internal/app` drives the HTTP API through `app.New` on the memory store, and
  `cmd/studio-schedule` runs its commands in-process through cobra against a PostgreSQL test
  database (`testdb_test.go`).
- **Store failures:** `internal/service/fault_test.go` wraps the memory store in a `FaultStore`
  that fails a chosen repository call, the start of a transaction or its commit with
  `ErrInjected`. `SweepFaults` runs an operation once, then once per store call it made with that
  call failing, and checks that the error comes back and that nothing was written, so no
  half-written rows and no queued mail or push. The repository wrappers are generated from
  `internal/store/store.go` (`go generate ./internal/service` after removing an interface method).
  `internal/app/faultstore_test.go` does the same for the API's generic 500 without internals.
- **Coverage:** `just test-cover` writes one cross-package profile (`go test -coverpkg=./...`), so a
  statement counts whichever package's test runs it, and `just coverage` reports it with
  `tools/coverage`. The report gives the total and then the packages with the most uncovered
  statements; the shared test suites, generated sqlc code and `tools/` are left out. It is also the
  gate: below `coverage_min` (total) or `coverage_min_package` (any package) in the justfile, it
  names each miss and fails. CI prints the report in the log and in the job summary, then runs
  the gate. The floors go up as coverage grows and go down only by a decision recorded in PLAN.md
  (Phase 9).
- **What counts:** a test asserts behaviour. A branch no input can reach is deleted, not excluded
  or covered artificially. Code is never excluded from coverage.

## Decision records

Architectural decisions are recorded in [docs/decisions](decisions/).
