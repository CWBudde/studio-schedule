# studio-schedule — Implementation Plan

Generic schedule and booking software for dance and ballet schools: weekly timetables, bookings,
waiting lists, prepaid passes and receipts. A Go kernel serves a JSON/OpenAPI API. A React +
shadcn/ui frontend consumes it.

This is a rewrite. An earlier system (a PHP backend, a partial Go port, several legacy web apps)
supplied the **domain knowledge only**: no code, data or history is carried over. The section
[Lessons from the previous attempt](#lessons-from-the-previous-attempt) lists what must not be
repeated.

Status legend: `[x]` done · `[ ]` open · `[~]` in progress / partially done

---

## Guiding principles

1. **One application, two targets.** `internal/app.New(...)` returns a single `http.Handler`. It
   runs as a normal server on PostgreSQL in production. For the GitHub Pages demo it is compiled to
   WebAssembly and runs in a Web Worker with an in-memory store. No feature is demo-only and no
   feature is faked in the demo.
2. **The API contract is the OpenAPI spec generated from Go (Huma v2).** TypeScript types are
   generated from it (`just gen-api`), and CI fails if they drift.
3. **Business logic lives in `internal/service` and depends only on interfaces** (Store, Clock, IDs,
   PasswordHasher, Outbox). Two store implementations, `memory` and `postgres`, must pass the same
   contract test suite.
4. **Correctness first.** Booking, pass redemption and receipt numbering are race-free by design
   (transaction plus row lock, with DB constraints as backstops).
5. **Time is explicit.** The school timezone lives in settings. Lessons are instants (`timestamptz`)
   and class templates use local wall time. `time.Local` is forbidden by lint.
6. **Generic and public.** No real school, person or price data anywhere. Seed data is fictional.
7. **Every phase ends shippable.** Each phase has backend, frontend, tests, docs and an updated live
   demo.

## Architecture overview

See [docs/architecture.md](docs/architecture.md) for details.

```
cmd/studio-schedule/      CLI: serve | migrate | seed | import | openapi | version
cmd/wasm/                 js/wasm entry point: same app, memory store, demo seed, tzdata
internal/domain/          entities, typed IDs, roles, domain errors
internal/clock/           Clock interface (system / fixed)
internal/ids/             ID Source interface (UUIDv7 / deterministic sequence)
internal/service/         business logic (interfaces only)
internal/store/           Store/Tx/Repo interfaces, storetest contract suite
internal/store/memory/    in-memory implementation (demo, fast tests)
internal/store/postgres/  pgx v5 + sqlc + goose migrations            (Phase 1)
internal/httpapi/         chi + Huma v2 handlers, /api/v1
internal/auth/            sessions, password hashing, RBAC          (Phase 3)
internal/mail/            templates, outbox worker, SMTP sender     (Phase 3)
internal/app/             wiring: app.New(deps) http.Handler
internal/wasmbridge/      fetch-style bridge between JS and the http.Handler
internal/webui/           embedded production web UI
web/                      React 19 + Vite + TS + Tailwind v4 + shadcn/ui + TanStack Router/Query
api/openapi.json          generated API contract
```

## Domain model (target)

Phase 1 refines this outline. "Legacy" names are listed in [docs/glossary.md](docs/glossary.md).

| Area          | Tables                                                                                                                |
| ------------- | --------------------------------------------------------------------------------------------------------------------- |
| Settings      | `settings` (singleton: school name, timezone, locale, release rule, cutoffs, currency, receipt prefix, legal data)    |
| People        | `persons`, `person_guardians` (parents book for children)                                                             |
| Accounts      | `accounts` (case-insensitive email, role enum, visible-days override), `sessions` (hashed token), `password_resets`   |
| Catalogue     | `locations`, `rooms` (capacity), `staff` (instructor / accompanist), `course_types`                                   |
| Timetable     | `terms` (date ranges, e.g. a summer timetable), `classes` (weekly template), `class_members`                          |
| Lessons       | `lessons` (instants, status, visibility, kind, capacity; unique `(class_id, starts_at)`)                              |
| Booking       | `bookings` (person or guest; booked / cancelled / attended / no_show; trial, prepaid), `booking_events` (audit)       |
| Waitlist      | `waitlist_entries` (hashed random invite token, expiry)                                                               |
| Passes        | `pass_types`, `passes` (`visits_remaining >= 0`), `pass_redemptions` (unique booking)                                 |
| Receipts      | `receipt_counters` (gapless per year), `receipts`, `receipt_lines` (immutable; corrections are cancellation receipts) |
| Communication | `messages`, `message_reads`, `outbox_emails`                                                                          |
| Integration   | `calendar_feeds` (secret token), `import_runs`                                                                        |

Roles: `guest` (anonymous) · `member` · `instructor` · `office` · `admin`. Roles are stored as enum
strings, never as numbers.

---

## Phase 0 — Project setup

Goal: the repository, tooling, CI and a live GitHub Pages demo exist. The demo proves the full path
from the React UI through the Web Worker and the WASM bridge to the Go handler, the service and the
in-memory store.

- [x] Repository `CWBudde/studio-schedule` (public, MIT), `.gitignore`, `.editorconfig`
- [x] PLAN.md, README.md (incl. live-demo note), CLAUDE.md/AGENTS.md, docs/ (architecture, demo mode,
      glossary, decision records)
- [x] Go module `github.com/cwbudde/studio-schedule` with the layout above
  - [x] `domain` (Settings, Role, errors), `clock`, `store` interfaces, `storetest`, `store/memory`
  - [x] `service.SettingsService`, `httpapi` (chi + Huma): `GET /api/v1/health`,
        `GET /api/v1/settings/public`, OpenAPI at `/api/v1/openapi.json`, docs at `/api/v1/docs`
  - [x] `app.New` with recover middleware and `http.CrossOriginProtection`
  - [x] `cmd/studio-schedule` (serve, openapi, version), `internal/webui` (embedded UI with SPA
        fallback)
  - [x] `cmd/wasm` + `internal/wasmbridge`: Promise-based `StudioScheduleAPI.fetch`, a goroutine per
        request, panic recovery, a custom ResponseWriter, `time/tzdata`
- [x] Web app `web/`: React 19, Vite, TS strict, Tailwind v4, shadcn/ui, TanStack Router + Query,
      react-i18next (en, de), openapi-typescript + openapi-fetch
  - [x] Transport abstraction: `http` (dev proxy / same origin) or `wasm` (Web Worker + cookie jar)
  - [x] Loading screen, crash screen, demo banner, language switcher, home page with health and
        settings
  - [x] ESLint, Prettier, Vitest
- [x] Tooling: justfile, treefmt, golangci-lint v2 (incl. forbidigo for `time.Local`), WASM build
      script, WASM dependency guard, `error-on-diff` checks (tidy, format, generated)
- [x] GitHub Actions: `tests` (lint, format, unit, web + WASM build/size/smoke), PR-title check,
      release-please, Pages deploy
- [x] Live demo at https://cwbudde.github.io/studio-schedule/

## Phase 1 — Domain & storage foundation

Goal: a production-grade persistence layer with two interchangeable implementations.

- [x] Typed IDs (UUIDv7) generated in Go via an injectable `IDs` source (2026-10-23) — generic
      `domain.ID[K]` with one alias per table, canonical-only parsing; `ids.Source` with `V7`
      (prod) and a deterministic `Sequence` (tests, seed); forbidigo bans `uuid.New*` outside
      `internal/ids`; `app.Deps.IDs` (default `V7`) feeds the lesson generator
- [ ] Domain entities and validation for all tables in the domain model (no behaviour yet)
      (2026-10-23) — plain structs with `Validate()` for every business table, string enums with
      `Valid()`, value types `Money`/`Date`/`TimeOfDay`, `Settings` extended (currency, receipt
      prefix, release rule, cutoffs, visible days); conventions in ADR 0002. `sessions`,
      `password_resets`, `outbox_emails` and `receipt_counters` arrive with Phases 3/6/7.
      partial: visible in the demo with the seed dataset
- [x] `store` interfaces per aggregate. Explicit ordering on every list method, no generic query
      builder (2026-10-23) — settings, persons + guardians, accounts, locations, rooms, staff,
      course types, terms, classes + members, lessons, bookings + events; waitlist, passes,
      receipts, messages, feeds and imports get their repositories in their phases
  - [x] `InTx(ctx, mode, fn)`. Document the "never nest InTx" rule; internal helpers take a `Tx`
        (2026-10-23) — nesting is detected through the context and fails with `ErrNestedTx`
  - [x] `Lessons().Lock(id)` as the domain-level serialisation primitive (2026-10-23) — row
        lock (`FOR UPDATE`) in PostgreSQL, the transaction mutex in memory; needs a read-write
        transaction
- [x] `storetest` contract suite: CRUD, case-insensitive email uniqueness, ordering, NULL handling,
      rollback, conflict errors, lock semantics (concurrent bookings with N goroutines)
      (2026-10-23) — 180 parallel cases per store, incl. 20 goroutines booking a capacity-5 lesson
- [x] `store/memory`: one transaction mutex plus copy-on-write snapshot for rollback; passes the suite
      (2026-10-23) — generic copy-on-write tables cloned on a transaction's first write; same keys,
      references and delete restrictions as the schema; UTC/µs instants; core aggregates
- [x] `store/postgres`: pgx v5 + sqlc, goose migrations embedded, `migrate up/down/status` CLI
      (2026-10-23) — pgx v5.11, sqlc v1.31.1 (generated code in `internal/sqlc`), goose v3.28
      provider with an advisory lock; constraint violations mapped to domain errors in the store
  - [x] Enums instead of flag sets; citext; partial unique index for active bookings; CHECK
        constraints (2026-10-23) — text + CHECK enums; emails as `text COLLATE "C"` with a unique
        `lower(email)` index instead of citext (citext folds non-ASCII, see ADR 0002)
  - [x] READ COMMITTED with `SELECT … FOR UPDATE`, retry on 40001/40P01 (2026-10-23) — up to 5
        attempts with linear backoff; giving up reports `ErrConflict`
  - [x] Passes the contract suite (local: docker; CI: Postgres service container) (2026-10-23) —
        testcontainers `postgres:18-alpine` or `STUDIO_TEST_DATABASE_URL`, one cloned database
        per test
- [ ] Config: env + flags (`DATABASE_URL`, `HTTP_ADDR`, `BASE_URL`, `SMTP_*`, `SESSION_*`), no
      hard-coded secrets, `.env.example` (2026-10-23) — `internal/config`: flag > env > default,
      validation, redacted logging; `serve --addr/--database-url/--base-url`. partial: `SMTP_*` and
      `SESSION_*` arrive with the mailer and sessions in Phase 3
- [x] `seed` command plus a deterministic fictional demo dataset (school, rooms, staff, course types,
      a term with ~25 weekly classes, members with families) (2026-10-23) — `internal/seed`: 2
      locations, 5 rooms, 9 staff, 9 course types, a term with 26 classes, 3 one-off workshops,
      40 persons in families with guardians and accounts; `studio-schedule seed` loads it into an
      empty, migrated database only. Server and `seed` use the school year 2026/27; the web demo
      builds it around the visit date (`seed.DemoAt`), so its schedule is never empty
- [x] `serve` chooses the store: `postgres` when `DATABASE_URL` is set, otherwise `memory` with demo
      seed (with a warning log) (2026-10-23) — refuses to start while migrations are pending
- [ ] Demo: the WASM build loads the full demo dataset; optional IndexedDB snapshot; "Reset demo"
      button (2026-10-23) — `cmd/wasm` loads the full dataset for the visit date via
      `seed.LoadDemoAt`. partial: IndexedDB
      snapshot and "Reset demo" once the demo can change data (Phase 3)
- [x] CI: Postgres contract tests job; sqlc generate drift check (2026-10-23) — `test-unit` runs
      the PostgreSQL contract tests via testcontainers on the runner's Docker; `test-format` runs
      `just check-sqlc`

## Phase 2 — Public schedule (read-only)

Goal: anyone can browse the published timetable. This is the first real demo value.

- [x] Catalogue services and endpoints (read): locations, rooms, staff, course types (2026-10-23)
      — `listLocations`, `listRooms`, `listStaff` (active, no person), `listCourseTypes`; the
      schedule filters list locations, staff and course types
- [ ] Terms and classes (read). Lesson generation from class templates for a date range: idempotent
      (unique `(class_id, starts_at)`), DST-correct, respects term and class validity ranges
      (2026-10-23) — `listTerms`, `listClasses`; `LessonGenerator` at school wall-clock time
      (`Date.At`), reads existing lessons first, inclusive term range (classes have no range of
      their own, ADR 0003); the demo's schedule shows the generated lessons. partial: `listTerms`
      and `listClasses` have no UI yet (a term or class overview)
- [ ] Release window: settings `release_weekday`, `release_time`, `release_weeks_ahead`; lessons
      beyond the window are invisible to members; special lessons can be published early; per-account
      visible-days override (used later) (2026-10-23) — server-only `ComputeReleaseWindow`: released
      through the release weekday N weeks after the last release (ADR 0003); one-off lessons and
      workshops are published early; the schedule counts down to the next release (server clock)
      and says up to which day classes are published. partial: the per-account visible-days
      override with accounts (Phase 3)
- [ ] Visibility rules: `public | members | hidden`; cancelled lessons stay visible with a status
      (2026-10-23) — guests see public, released lessons; invisible lessons are a 404 like unknown
      ones; the demo shows canceled lessons struck through with their status. partial: members
      and staff with accounts (Phase 3)
- [x] `GET /api/v1/schedule?from&to&location&instructor&courseType&level`, `GET /api/v1/lessons/{id}`
      (2026-10-23) — `getSchedule` (≤ 42 days, filters, `releasedUntil`/`nextReleaseAt`/`now`) and
      `getLesson`, used by the schedule page and the lesson sheet; the WASM smoke test checks the
      demo schedule and that its workshops are published early
- [x] OpenAPI schema for domain IDs: `type: string, format: uuid` (registered in `httpapi`, so
      `domain` stays dependency-free), visible in the generated TS types (2026-10-23) — every ID
      type is a registry alias of one inline UUID schema instead of per-field tags; tests walk the
      spec and read `domain/id.go` so a new ID type cannot miss it
- [x] Background job runner abstraction (Go ticker in server and WASM) for lesson generation
      (2026-10-23) — `internal/jobs`; both targets run every job once at startup, then
      `generate-lessons` hourly over a rolling horizon
- [x] UI: schedule list view (day groups, the previous system's "board"), week grid; filters; lesson
      detail sheet; countdown to the next release; empty and error states; mobile-first
      (2026-10-23) — `/schedule` with all state in the URL (view, date, filters, open lesson),
      14-day list and Monday–Sunday grid (one day per screen on phones), a note up to which day
      classes are released, and a "next lessons" teaser on the home page
- [x] UI: i18n for all strings; date/time formatting in the school timezone (`Intl` with an explicit
      `timeZone`) (2026-10-23) — `lib/schoolTime.ts`; a `schedule` namespace in en and de with a
      key-parity test; web tests run with `TZ=America/New_York` so browser-zone leaks fail
- [x] Tests: generation across DST changes, release window edge cases (Friday 17:59 / 18:00),
      component tests, Playwright e2e against the demo build (2026-10-23) — Go tests for DST
      generation and the release edges; component tests for every schedule part; Playwright
      (`web/e2e`, `just e2e`, CI job `test-e2e`) drives the WASM demo build in desktop and phone
      Chromium: home, list, week grid, filters, lesson sheet, countdown, early workshops, deep
      links, German

## Phase 3 — Accounts, auth & administration basics

Goal: people can sign up and log in; staff can maintain the timetable.

- [ ] Persons and family: guardians manage dependents (children without their own login)
- [ ] Registration with email confirmation; login and logout; server-side sessions (random token in
      an HttpOnly, Secure, SameSite=Lax cookie; only the token hash is stored); sliding expiry
- [ ] Password hashing behind `PasswordHasher` (argon2id; cheap parameters via build tag for the
      demo; pre-hashed seed passwords)
- [ ] Password reset (hashed single-use token with expiry); change password; change email with
      re-confirmation
- [ ] RBAC middleware/helpers on Huma operations; admin impersonation ("login as") with audit and a
      visible banner
- [ ] Login rate limiting, security headers (CSP, HSTS in production), consistent problem+json
      errors without internal details
- [ ] Mail: outbox table, worker, SMTP sender, EN/DE templates (`html/template` + text alternative);
      demo: "Demo mailbox" panel listing outbox mails
- [ ] Admin CRUD: locations, rooms, staff, course types, terms, classes, class members
- [ ] Lesson administration: edit, cancel (notifies booked participants), hide, add one-off/special
      lessons, clone day, clone week
- [ ] UI: login, registration, confirm, reset; profile (person data, dependents, password, language);
      admin area with shadcn data tables and forms (react-hook-form + zod, schemas derived from the
      OpenAPI types)
- [ ] Demo: login page lists demo accounts per role (member, instructor, office, admin)
- [ ] Tests: auth flows, RBAC matrix per endpoint, mail rendering snapshots, e2e (login, admin edits a
      class)

## Phase 4 — Booking & waiting list

Goal: the core member workflow.

- [ ] Book a lesson for yourself or a dependent, as one transaction:
      `InTx → Lessons().Lock → CountActive < capacity → Insert → booking_event → outbox`; booking
      cutoff and release window enforced; duplicate booking is a conflict
- [ ] Cancel a booking (sign-out deadline configurable; late cancellation flagged); "also cancel
      following weeks" for class members
- [ ] Class members are auto-enrolled into generated lessons and can release single dates (this
      replaces the old paper "swap list")
- [ ] Staff actions: guest bookings (name + email, no account), trial flag, add or remove
      participants beyond capacity (explicit override)
- [ ] Waitlist: join and leave; when a spot frees up, the next entry gets a **timed invite** (random
      token, only the hash stored, expiry configurable); accepting books under the lesson lock;
      expired invites move on to the next entry; "stay on list" option (decision record required)
- [ ] Instructor view: today's lessons, participant list, attendance check (attended / no-show),
      notify participants
- [ ] "My lessons" (upcoming, history), booking audit trail for office/admin
- [ ] UI: book/cancel from schedule and detail views, for-whom selector (family), waitlist state,
      optimistic updates with rollback, toast feedback
- [ ] Tests: concurrent booking (contract suite, both stores), cutoffs, waitlist invite
      expiry/acceptance, e2e booking flow in the demo

## Phase 5 — Calendar feeds

- [ ] Per-subject feed tokens (location, instructor, personal bookings); random secret, revocable;
      only the token hash is stored
- [ ] `GET /ical/{token}.ics` (RFC 5545, VTIMEZONE, stable UIDs, cancelled lessons as
      `STATUS:CANCELLED`)
- [ ] UI: "Subscribe" dialog with copy-URL; demo: download via the bridge as a Blob
- [ ] Tests: golden files, timezone/DST correctness

## Phase 6 — Passes & receipts

Goal: prepaid passes and legally sound receipts.

- [ ] Pass types (visits, validity, price in minor units); sell or assign a pass (office); pass
      overview for members
- [ ] Redemption on attendance or booking (configurable): oldest valid pass first, under the
      transaction, with a unique redemption per booking; refund on cancellation within the deadline
- [ ] Receipts: gapless per-year numbering via `receipt_counters` in the same transaction; immutable
      receipts and lines; corrections only via cancellation receipts referencing the original
- [ ] PDF generation in pure Go (works in WASM too), template driven by settings (school legal data,
      tax note, logo); email receipt via outbox
- [ ] UI: pass management, receipt list/detail, PDF download (Blob in the demo)
- [ ] Tests: concurrent redemption, numbering under concurrency, PDF golden tests (text extraction)

## Phase 7 — Communication & administration

- [ ] Announcements: audience (all / role / course type / lesson), publish window, in-app display
      (dismissible), optional email
- [ ] Mail log (outbox history with status, resend)
- [ ] Statistics dashboard: bookings per week, utilisation per class/room, no-show rate,
      active members, pass sales (aggregated in Go; shadcn charts)
- [ ] Member CSV import with column mapping, dry-run preview, idempotent re-import (`member_no`),
      import run history
- [ ] Duplicate detection and merge for persons (email, name + birth date)
- [ ] Settings UI: school profile, branding (name, accent colour, logo), timezone, locale, release
      rule, cutoffs, receipt data
- [ ] Tests: import fixtures, merge correctness, statistics against seeded data

## Phase 8 — Production readiness

- [ ] Single binary with embedded UI; `Dockerfile` (distroless); `docker-compose.yml` (app +
      Postgres + Mailpit)
- [ ] Container image to ghcr.io on release (GitHub App or PAT token for release-please so tags
      trigger workflows)
- [ ] Structured logging (slog), request IDs, `/healthz` + `/readyz`, optional Prometheus metrics
- [ ] Backup/restore documentation (pg_dump), migration strategy, upgrade notes
- [ ] GDPR: data export per person, deletion/anonymisation (receipts retained), guest-data retention
      job, privacy notes in docs
- [ ] Playwright smoke suite against `serve` + Postgres in CI (real cookies, CSRF, Postgres semantics)
- [ ] Security review (OWASP ASVS L1 checklist), dependency scanning (Renovate, govulncheck)
- [ ] Operator documentation: configuration reference, deployment guide

## Phase 9 — Optional modules

- [ ] Media library (video links per course type, members only)
- [ ] Embeddable public schedule widget (iframe/script) for the school website
- [ ] Events/workshops page with separate pricing
- [ ] PWA (installable, offline schedule cache, web push for waitlist invites)
- [ ] Legacy data migration from the previous system into the generic import format. This lives in
      a **separate private repository** because it touches real data; the public repo only provides
      the import format and the `import` command.

---

## Lessons from the previous attempt

Issues found in the previous system that this rewrite must avoid:

- **Capacity race:** the participant count and the insert were separate statements, with no lock and
  no transaction. → Lock the lesson inside a transaction; partial unique index.
- **Pass charging race:** one statement selected the credit and another updated it; the logged credit
  could differ from the one charged. → Single transactional repo method plus unique redemption per
  booking.
- **Receipt numbers** derived from `AUTO_INCREMENT − offset` with a hard-coded year suffix. → Gapless
  per-year counter row in the same transaction.
- **Guessable waitlist invite codes** (truncated md5 of IDs); confirmation needed no login. → 32
  random bytes, hash stored, expiry.
- **Waitlist "notify everyone and clear the list"** silently lost people. → Timed invites in order.
- **Role numbers drifted** between two backends. → Enum strings, one source of truth (OpenAPI).
- **Raw SQL errors returned to clients**; SQL built by string interpolation; a shell command built
  from user input during receipt generation. → problem+json without internals; sqlc; no shell-outs.
- **Hard-coded secrets, IDs and names** (DB passwords, API keys, location IDs, instructor names). →
  Config only; settings in the DB; lint and review.
- **Stubs that returned success** (avatar upload, emails) and **docs that overstated progress**. →
  Unimplemented endpoints don't exist or return 501; PLAN.md checkboxes only after tests and demo.
- **Timezone handling** depended on the server's local zone. → Explicit school timezone; `time.Local`
  forbidden.
- **Two parallel frontends and APIs drifted** (field names, endpoints). → Generated types plus a
  drift check in CI.
- **The release rule existed three times** (server, browser client, rewrite) with different times,
  computed in the browser's timezone and compared by date. → Computed once on the server from
  settings; the API returns `releasedUntil`, `nextReleaseAt` and `now`; comparisons use instants.
- **Cloning a day shifted lessons by an hour across DST**, and copying deleted the target day first.
  → Lessons are generated at school wall-clock time and generation only adds what is missing.
- **The release job had no caller**, so nothing ran on schedule. → A job runner wired into both
  targets and covered by tests.

## Risks

| Risk                                                                      | Mitigation                                                                                   |
| ------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------- |
| Demo (memory store, worker cookies) drifts from production behaviour      | Shared contract suite; Playwright smoke against server + Postgres (Phase 8)                  |
| Every query must be implemented twice (memory + Postgres)                 | Narrow per-aggregate repos; aggregation and statistics in Go services                        |
| WASM binary size / first load on mobile                                   | Size reported in CI; loading screen; dependency guard; lazy start                            |
| release-please with `GITHUB_TOKEN` doesn't trigger CI on release PRs/tags | Add a PAT/GitHub App secret `RELEASE_PLEASE_TOKEN` when needed (workflow already prefers it) |
| Personal data of minors                                                   | GDPR features in Phase 8; fictional seed data only; no real data in this repo                |
