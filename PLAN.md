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
   feature is faked in the demo. From Phase 13, a PHP backend implements the same API contract on
   the same schema as a separate implementation, held to the Go app by a conformance suite.
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
| Lessons       | `lessons` (instants, status, visibility, kind, capacity; class slot date, unique `(class_id, slot_date)`)             |
| Booking       | `bookings` (person or guest; booked / cancelled / attended / no_show; trial, prepaid), `booking_events` (audit)       |
| Waitlist      | `waitlist_entries` (hashed random invite token, expiry)                                                               |
| Passes        | `pass_types`, `passes` (`visits_remaining >= 0`), `pass_redemptions` (unique booking)                                 |
| Receipts      | `receipt_counters` (gapless per year), `receipts`, `receipt_lines` (immutable; corrections are cancellation receipts) |
| Events        | `events` (title, slug, price, sign-up window; sessions are lessons), `event_registrations` (Phase 10)                 |
| Communication | `announcements`, `announcement_dismissals`, `announcement_mailings`, `outbox_mails`                                   |
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
- [x] Domain entities and validation for all tables in the domain model (no behaviour yet)
      (2026-10-23) — plain structs with `Validate()` for every business table, string enums with
      `Valid()`, value types `Money`/`Date`/`TimeOfDay`, `Settings` extended (currency, receipt
      prefix, release rule, cutoffs, visible days); conventions in ADR 0002. `sessions`,
      `password_resets`, `outbox_emails` and `receipt_counters` arrive with Phases 3/6/7. The
      seed shows what has a repository; bookings, passes, waitlist and receipts reach the demo
      with their phases (2026-10-24)
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
- [x] Config: env + flags (`DATABASE_URL`, `HTTP_ADDR`, `BASE_URL`, `SMTP_*`), no hard-coded
      secrets, `.env.example` (2026-10-23) — `internal/config`: flag > env > default, validation,
      redacted logging; `serve --addr/--database-url/--base-url`; `SMTP_*` and `TRUSTED_PROXIES`
      came with Phase 3 (2026-10-24). No `SESSION_*`: ADR 0004 fixes the session lifetimes
- [x] `seed` command plus a deterministic fictional demo dataset (school, rooms, staff, course types,
      a term with ~25 weekly classes, members with families) (2026-10-23) — `internal/seed`: 2
      locations, 5 rooms, 9 staff, 9 course types, a term with 26 classes, 3 one-off workshops,
      40 persons in families with guardians and accounts; `studio-schedule seed` loads it into an
      empty, migrated database only. Server and `seed` use the school year 2026/27; the web demo
      builds it around the visit date (`seed.DemoAt`), so its schedule is never empty
- [x] `serve` chooses the store: `postgres` when `DATABASE_URL` is set, otherwise `memory` with demo
      seed (with a warning log) (2026-10-23) — refuses to start while migrations are pending
- [x] Demo: the WASM build loads the full demo dataset; optional IndexedDB snapshot; "Reset demo"
      button (2026-10-23) — `cmd/wasm` loads the full dataset for the visit date via
      `seed.LoadDemoAt`; since 2026-10-24 the whole memory store and the session survive a reload
      (IndexedDB snapshot until "Reset demo", see Admin CRUD → Demo)
- [x] CI: Postgres contract tests job; sqlc generate drift check (2026-10-23) — `test-unit` runs
      the PostgreSQL contract tests via testcontainers on the runner's Docker; `test-format` runs
      `just check-sqlc`

## Phase 2 — Public schedule (read-only) — ✅ DONE (2026-10-23)

Goal: anyone can browse the published timetable. This is the first real demo value.

- [x] Catalogue services and endpoints (read): locations, rooms, staff, course types (2026-10-23)
      — `listLocations`, `listRooms`, `listStaff` (active, no person), `listCourseTypes`; the
      schedule filters list locations, staff and course types
- [x] Terms and classes (read). Lesson generation from class templates for a date range: idempotent
      (unique `(class_id, starts_at)`), DST-correct, respects term and class validity ranges
      (2026-10-23) — `listTerms`, `listClasses`; `LessonGenerator` at school wall-clock time
      (`Date.At`), reads existing lessons first, inclusive term range (classes have no range of
      their own, ADR 0003); the demo's schedule shows the generated lessons; `/classes` (header
      link "Classes") lists the terms and the weekly classes of the chosen term (`?term=`, default
      the current one), Monday first, with time, level, room and staff
- [x] Release window: settings `release_weekday`, `release_time`, `release_weeks_ahead`; lessons
      beyond the window are invisible to members; special lessons can be published early; per-account
      visible-days override (used later) (2026-10-23) — server-only `ComputeReleaseWindow`: released
      through the release weekday N weeks after the last release (ADR 0003); one-off lessons and
      workshops are published early; the schedule counts down to the next release (server clock)
      and says up to which day classes are published; visible days are _early access_: members
      also see lessons up to today + N days (account override, else `DefaultVisibleDays`; never
      less than released), returned as `visibleUntil`; lesson generation reaches the longest one
- [x] Visibility rules: `public | members | hidden`; cancelled lessons stay visible with a status
      (2026-10-23) — guests see public, released lessons; invisible lessons are a 404 like unknown
      ones; the demo shows canceled lessons struck through with their status; members also see
      `members` lessons, staff every lesson (hidden and unreleased), each with a badge; the demo
      has one workshop for members and one hidden workshop
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

- [x] Persons and family: guardians manage dependents (children without their own login). The
      store already has persons and guardian links (`AddGuardian`, `ListDependents`,
      `ListGuardians`); no service, operation or page uses them yet (2026-10-23) — ADR 0008,
      `service.FamilyService` with `/api/v1/family`, the `/account` profile and the office's
      `/admin/persons` (2026-10-24)
  - [x] ADR "Families and dependents": a guardian sees and edits only their own person and their
        dependents (office sees everyone); how a second guardian is linked (proposed: office only
        in Phase 3); removing a dependent unlinks it and deletes the person only while nothing
        references it; a dependent who grows up gets an account for the existing person
        (2026-10-23) — ADR 0008, also covering the account's mail language
  - [x] Service and operations (`signedIn`): `getMyFamily` (own person + dependents), update own
        person data, create, update and remove a dependent; the guardian check lives in the
        service and is reused by booking for others in Phase 4 (2026-10-23) —
        `service/family.go` (`dependentOf`), `getMyFamily`, `updateMyPerson`, `addDependent`,
        `updateDependent`, `removeDependent`; each dependent lists its other guardians; removing
        the last guardian deletes the person, or answers 409 while bookings or class memberships
        refer to it; a birth date after today (school zone) is a 422 on `body.birthDate`
  - [x] Office: person list and detail with family links (`/admin/persons`), add and remove a
        guardian link; shares `listPersons` with the class-member subtask of Admin CRUD
        (2026-10-24) — `getPerson`, `createPerson`, `updatePerson` (email and notes included),
        `deletePerson` (409 `in-use` while anything refers to the person), `addGuardian` (a cycle
        is a 422 on `guardianId`; links are written under `Persons().Lock`) and `removeGuardian`
        (the office's removal keeps the person); `createPersonAccount` creates an unconfirmed
        account with a 72-hour invitation (EN/DE mail) whose link sets the password (409
        `has-account`, `duplicate` for a taken email; only admins create admins);
        `/admin/persons` searches through `?q=`, the detail page shows guardians, children,
        account, staff links and classes; e2e in `admin.spec.ts`
  - [x] A dependent with their own account (ADR 0008, created by the office): the guardian's
        removal only unlinks it (today `Persons().Delete` fails on the account and the API answers
        a misleading 409), and the guardian no longer edits the data its owner manages
        (2026-10-24) — a "managed" dependent (own account, staff link or dependents of its own,
        `managed` in `getMyFamily`) is only unlinked and its edit is a 409 `managed`; other
        refusals are the `Conflict` 409 `in-use`; ADR 0008 amended (whether guardians book for a
        managed dependent is open for the Phase 4 booking ADR); e2e in `account.spec.ts`
  - [x] A dependent the office linked to a staff record (`personId` on staff, 2026-10-24): removing
        it fails on the staff link, but `ErrDependentInUse` names only bookings and class
        memberships; say why, or only unlink it like a dependent with an account (2026-10-24) —
        a staff link makes the dependent managed, so it is only unlinked
  - [x] Demo: a demo family account with two children; tests for the guardian check (a stranger's
        dependent is a 404, not a 403) in service and HTTP tests (2026-10-23) — the demo member
        Clara Amberly has Ella and Finn (Tom is a second guardian, Hanna Birchmoor's Mia is a class
        member); `service/family_test.go`, `app/family_test.go`, `seed_test.go`
- [x] Registration with email confirmation; login and logout; server-side sessions (random token in
      an HttpOnly, Secure, SameSite=Lax cookie; only the token hash is stored); sliding expiry
      (2026-10-23) — `AuthService` and `/api/v1/auth/{register,confirm,login,logout,me}`; confirming
      needs the link and the password (no pre-registration takeover) and signs in; 14 days idle,
      90 days at most, extended at most daily; Secure unless `BASE_URL` is not https (ADR 0004)
- [x] Password hashing behind `PasswordHasher` (argon2id; cheap parameters via build tag for the
      demo; pre-hashed seed passwords) (2026-10-23) — `internal/auth/password` (PHC strings, OWASP
      parameters on the server, 1 MiB in `js`, rehash on login, bounded concurrency); every demo
      account has the pre-hashed password `demo-password`
- [x] Password reset (hashed single-use token with expiry); change password; change email with
      re-confirmation (2026-10-23) — `service/credentials.go` and five operations under
      `/api/v1/auth`; reset links last 1 hour and end every session, a password change ends every
      other one; the new address confirms by link and the old one gets a notice; unknown or taken
      addresses get the same answer; refused while impersonating; `/forgot-password`, `/reset-password`, `/confirm-email-change` and `/account` (ADR 0006)
- [x] RBAC middleware/helpers on Huma operations; admin impersonation ("login as") with audit and a
      visible banner (2026-10-23) — every operation is registered with an access level (`public`,
      `signedIn`, `staff`, `office`, `admin`; `x-access` in the OpenAPI document), one middleware
      answers 401/403, a test pins every level and calls every operation as every role; admins
      sign in as non-admin accounts for at most an hour from `/admin/accounts`, a banner on every
      page leads back, start and end go to `audit_events` (ADR 0005)
- [x] Login rate limiting, security headers (CSP, HSTS in production), consistent problem+json
      errors without internal details (2026-10-23) — `internal/ratelimit` (5 wrong passwords per email
      and 20 logins per client IP per 15 min, 5 mails per IP per hour; 429 with `Retry-After`, shown
      in the forms), client IP via `TRUSTED_PROXIES`; CSP without script `unsafe-inline`/`eval`,
      nosniff, `DENY` framing, same-origin referrer, HSTS for an https `BASE_URL`; every error is
      problem+json and 500s say nothing more (`internal/app/security_test.go`, ADR 0006)
- [x] Mail: outbox table, worker, SMTP sender, EN/DE templates (`html/template` + text alternative);
      demo: "Demo mailbox" panel listing outbox mails (2026-10-23) — mails go to `outbox_mails` in
      the causing transaction; the `deliver-mail` job claims one at a time and retries 5 times
      (`SMTP_*` config, `internal/mail/smtp` server only); the demo's header opens the mailbox, whose
      confirmation links open in the app
- [x] Admin CRUD: locations, rooms, staff, course types, terms, classes, class members. The repos
      already have Get/Create/Update/Delete/List; the work is services, write operations and the
      admin UI. Order: decisions and the two foundations first, then the entity slices (each slice
      is backend + UI + tests and can run in parallel; regenerate `api/openapi.json`,
      `schema.d.ts` and `routeTree.gen.ts` after merging), then demo and e2e (2026-10-24) —
      ADR 0007, both foundations, every entity slice (locations and rooms, staff, course types,
      terms, classes, class members), the demo's snapshot and "Reset demo", and their tests
  - [x] ADR 0007 "Timetable administration": `office` writes catalogue and timetable (admins
        inherit it), what an edit of a class or term does to its generated lessons (proposed:
        future, untouched lessons follow the class; past and individually edited lessons stay),
        retiring instead of deleting what is referenced (staff `active`; a class or term with past
        lessons), last write wins vs `If-Match`, whether edits go to `audit_events` (2026-10-24) —
        ADR 0007: office writes, retire what is referenced, future untouched lessons follow a
        class edit, terms may overlap, unique location and room names, last write wins, no audit
        of timetable edits
  - [x] Backend foundation: write-operation conventions (`POST` collection → 201 with the entity,
        `PUT /{id}`, `DELETE /{id}` → 204; request bodies without IDs, IDs from `ids.Source`);
        a 409 that says _why_ (stable reason such as `in-use` or `duplicate`, no internals) so the
        UI can explain a refused delete; validation errors per field in the 422; office-only list
        variants that include what the public lists hide (inactive staff, staff person links)
        (2026-10-24) — the write conventions, the `Conflict` 409 with `reason`
        (`in-use`, `duplicate`; `domain.ErrInUse`, `domain.ErrDuplicate`, `withConflict`) and
        per-field 422s (`domain.FieldError` via `on(field, …)` rules, `body.<field>`); office-only
        reads under `/api/v1/admin/…`: `listAllStaff` (inactive staff, person links) and
        `listPersons` (person search for pickers, `service.PersonService`)
  - [x] Frontend foundation: `/admin` layout with section navigation (office and admin see it in
        the header, everyone else gets the 403 page), shared CRUD kit: data table (sorting,
        search, empty and error states, phone layout as cards), form sheet/dialog with
        react-hook-form + zod built from the generated types, 422 → field errors, delete
        confirmation that shows the 409 reason, toasts; shadcn `dialog`, `alert-dialog`, `form`,
        `textarea`, `switch`, `popover` + `command` (combobox) (2026-10-24) — `/admin`
        layout with section navigation and an "Administration" menu item for office and admin,
        no-access and load-error states, `features/admin/crud` (`DataTable` with
        `@tanstack/react-table`, `FormSheet`, `ConfirmDelete`, 422 field marking, 409 reason
        sentences, toasts, a shared `DeleteDialog`), shadcn `alert-dialog`, `textarea`, `switch`
        and `popover` + `command` (`crud/PersonField`, a person combobox over `listPersons`);
        `dialog` comes with `command`, `form` is not needed (forms use the sheet and `TextField`)
  - [x] Locations and rooms: create, rename, delete (refused while rooms or classes use them);
        rooms listed per location with capacity; `/admin/locations` (2026-10-24) — six `office`
        operations (`createLocation` … `deleteRoom`, `service/catalog_admin.go`), unique names
        (migration 00006, which renames existing case duplicates), 409 `in-use` and `duplicate`,
        a 422 on `locationId` for an unknown location; `/admin/locations` lists locations with
        their room count and the rooms with location and capacity; e2e in `admin.spec.ts`
  - [x] Staff: create, edit name and kind, optional link to a person, deactivate/reactivate
        (inactive staff disappear from the public lists and filters, stay on past lessons);
        `/admin/staff` (2026-10-24) — `listAllStaff`, `createStaff`, `updateStaff` (`active` and
        `personId` in the body; an unknown person is a 422 on `personId`), `deleteStaff` only while
        unused (on a 409 `in-use` the dialog deactivates them in one click; ADR 0007); deactivating is allowed while they
        teach, and `listClasses` now names instructor and accompanist itself so the timetable
        still shows them; `/admin/staff` with an active switch, an All/Active/Inactive filter and
        a person combobox; e2e: a deactivated instructor leaves the schedule filter
  - [x] Course types: create, edit name and description, delete while unused; `/admin/course-types`
        (2026-10-24) — `createCourseType`, `updateCourseType`, `deleteCourseType`; names unique
        ignoring case (migration 00007, which renames existing case duplicates; 409
        `duplicate`), 409 `in-use` while classes or lessons use one; e2e in `admin.spec.ts`
  - [x] Terms: create, edit name and date range (refused when it would orphan classes or booked
        lessons, per ADR 0007), delete while empty; overlapping terms allowed or not per ADR;
        `/admin/terms` (2026-10-24) — `createTerm`, `updateTerm`, `deleteTerm`
        (`service/term_admin.go`); new dates delete the future lessons they leave out that still
        follow their class and have no bookings, and refuse with a 422 on `startsOn`/`endsOn` while
        such a lesson has started, has bookings or was edited; longer dates generate the lessons
        ahead; a term with classes is a 409 `in-use` (no retired state); terms may overlap and
        names need not be unique; `Terms().Lock` orders term edits, class edits and generation;
        `/admin/terms` adds, edits and deletes terms; e2e in `admin.spec.ts`
  - [x] Classes: create and edit within a term (course type, room, instructor, accompanist, level,
        weekday, start, duration, capacity defaulting to the room's), delete or retire;
        `/admin/terms/{id}/classes` as a weekly overview reusing the `/classes` layout
        (2026-10-24) — `createClass`, `updateClass`, `deleteClass` and the office's
        `listAllClasses` (retired ones included); unknown references are 422s on their field; a
        class with members, started or booked lessons is a 409 `in-use` and is retired instead
        (`classes.retired`: no new lessons, hidden from the public `/classes`, reactivatable);
        a read-only `/admin/terms` links to each term's `/admin/terms/{id}/classes`, a weekly
        overview on `WeekList` with a class sheet (capacity prefilled from the room), a retired
        badge and "Retire instead" in the delete dialog; e2e in `admin.spec.ts`
    - [x] Store: `Lessons().ListByClass(class, from)` and `Lessons().Delete`, plus the
          lesson-to-slot link or watermark from ADR 0003 so a moved lesson is not re-created;
          contract cases in `storetest` for both stores (2026-10-24) — `lessons.slot_date`
          (migration 00008, backfilled in the school time zone; unique `(class_id, slot_date)`
          replaces `(class_id, starts_at)`), `Classes().Lock`; generation keys on the class and
          the week, so a moved lesson is not re-created
    - [x] Service: an edit updates or re-creates the class's future generated lessons in the same
          transaction (time zone and DST through `Date.At`), then generation fills the rest; tests
          across a DST change and for lessons that must not move (2026-10-24) — future lessons
          that still equal what the class generated follow it, booked ones included (a capacity
          below their active bookings is a 422 on `capacity`), within their Monday-based week;
          edited lessons stay; generation creates only lessons that have not started (ADR 0003
          amended, the demo seeds its past week itself)
  - [x] Class members: list, add and remove persons with an optional "since" date; an office-only
        person search (`listPersons`, name and email) for the combobox; adding twice is a 409;
        no booking side effects until Phase 4 (2026-10-24) — `listClassMembers`,
        `addClassMember` (409 `duplicate`, a 422 on an unknown `personId`), `removeClassMember`
        (`service/class_members.go`); a "Members" sheet per class on `/admin/terms/{id}/classes`
        with the person combobox and a "since" date; e2e in `admin.spec.ts`
  - [x] Demo: the demo office account can do every action above; edits live in the worker's memory
        store until a reload, and the open Phase 1 item ("Reset demo" button, optional IndexedDB
        snapshot) lands with this slice (2026-10-24) — the worker saves a JSON snapshot of the
        whole memory store and its cookie jar to IndexedDB after every committed write (job writes
        included) and restores it on start, until "Reset demo" (header and crash card) clears it;
        a snapshot from another build or store shape is dropped; one tab runs the demo (Web Lock,
        "Use here" takes over, owner epoch fences stale saves); `memory/snapshot_test.go` fails
        for a table without a codec; e2e in `demo-reset.spec.ts`
  - [x] Tests and docs: every new operation pinned in `internal/app/access_test.go`; service tests
        per entity (validation, 409 reasons, lesson sync); component tests per admin page; e2e
        "office edits a class and the schedule shows the change" on desktop and phone; glossary
        and architecture updated (2026-10-24) — every admin operation is pinned, each slice has
        service, HTTP, component and e2e tests (`admin.spec.ts`, desktop and mobile); the
        glossary has duplicate, slot date and follow, the architecture notes how class and term
        edits fill lessons, ADR 0007 has the term and member rules
- [x] Lesson administration: edit, cancel (notifies booked participants), hide, add one-off/special
      lessons, clone day, clone week. Builds on the class-to-lesson sync of Admin CRUD
  - [x] ADR 0007 (extended): an individually edited lesson is detached from its class, so neither
        class edits nor generation touch it again; generated lessons are canceled, never deleted
        (generation would re-create them), and only one-offs without bookings can be deleted;
        copies made by cloning are one-off lessons; who may cancel (office; instructors their own
        lessons or not) (2026-10-24) — ADR 0007 "Lessons": office only; `detached` complements the
        implicit follow rule; started lessons are a 422, past dates need `confirmPast`; a room
        overlap is a 409 `room-taken` with `confirmRoomOverlap`; recipients are the person's
        confirmed account, else their email, else their guardians' accounts (none for a disabled
        account), no mails for past lessons; a term edit deletes the unbooked future lessons it
        leaves out; ADR 0003: only workshops and private lessons publish early
  - [x] Store: lesson fields for a detached flag and a cancellation note (migration, both stores,
        contract cases); a room-overlap query so an edit or clone can warn about a double-booked
        room (2026-10-24) — migration 00009 (`detached`, `cancel_note`, `canceled_at`,
        `canceled_by`), `Lessons().ListRoomOverlaps`; contract cases in both stores
  - [x] Edit a lesson: time, room, staff, capacity, visibility (hide/unhide as a quick action),
        kind; capacity below the active bookings is refused; office actions in the existing lesson
        sheet of the schedule (2026-10-24) — `updateLesson` (`service/lesson_admin.go`); a start or
        room change of a booked lesson mails "lesson changed"; "Manage this lesson" in the lesson
        sheet with edit, hide/unhide (asks when booked) and the room and past confirmations
  - [x] Cancel and restore with an optional note: one transaction marks the lesson and queues an
        EN/DE mail (outbox) to everyone with an active booking; bookings stay as they are until
        Phase 4/6 decide about passes; the schedule already shows canceled lessons (2026-10-24) —
        `cancelLesson`, `restoreLesson` (checks the room), `lesson-canceled`/`lesson-restored`
        mails with golden files; the sheet shows who canceled (the real admin when impersonating)
        and the note
  - [x] One-off and special lessons (workshop, private): create form with course type, room,
        staff, time, capacity, visibility; published early per ADR 0003; delete while unbooked
        (2026-10-24) — `createLesson`, `deleteLesson` (409 `generated` for a class lesson, `in-use`
        while booked); "New lesson" in the schedule toolbar; e2e in `lesson-admin.spec.ts`
  - [x] Clone day and clone week: pick source and target, preview what would be created and what
        is skipped (same room and time taken), then apply in one transaction; only adds, never
        deletes; lessons keep their wall-clock time across DST (2026-10-24) — `previewLessonClone`
        and `cloneLessons` (`service/lesson_clone.go`); skips `class-slot`, `started`,
        `staff-inactive` and `room-taken` (also by a class lesson generation will create, or by
        another copy); "Clone…" in the schedule toolbar with preview and result; e2e in
        `lesson-admin.spec.ts`
  - [x] Tests: cancellation mail golden files, clone across a DST change, generation leaves
        detached and moved lessons alone, e2e "office cancels a lesson and it shows as canceled"
        (2026-10-24) — partial: golden files, `TestUpdateLesson` (generation and class sync leave
        detached lessons alone) and the cancel e2e exist; `TestCloneDay` clones across the October
        DST change; the demo has no bookings yet, so no e2e shows the cancellation mail (Phase 4)
- [x] UI: login, registration, confirm, reset; profile (person data, dependents, password, language);
      admin area with shadcn data tables and forms (react-hook-form + zod, schemas derived from the
      OpenAPI types) (2026-10-23) — partial: login, registration and confirm pages, the signed-in
      header, the admin account list (`/admin/accounts`, with "sign in as"), the reset pages and the
      account settings for password and email, the profile's person data, dependents and
      language, and the `/admin` layout with locations and rooms, staff and course types are done;
      the rest of the admin area remains; (2026-10-24) the admin area is complete with the audit
      log and the lesson administration in the schedule
  - [x] Profile: person data card on `/account` (name, phone, birth date) using `getMyFamily`
        (2026-10-23) — `ProfileCard`; the header's name follows a change; editable while
        impersonating
  - [x] Dependents section on `/account`: list, add, edit and remove children (Persons and family)
        (2026-10-23) — `DependentsCard` with a sheet form and an in-row confirmation that says
        whether the child is unlinked or deleted; 409 and 404 explained; e2e in `account.spec.ts`
  - [x] Language preference: a language field on accounts (migration, both stores), set from the
        UI language at registration, editable on `/account`; mails are rendered in it (today the
        locale comes from the request, which a cancellation or waitlist mail does not have) and
        the UI switches to it after sign-in (2026-10-23) — `accounts.locale` (migration 00005),
        `updateLocale` and `me.locale`; mails use the account's language, then the request's, then
        the school's; login and email confirmation switch the UI (e2e)
  - [x] Admin area: covered by the subtasks of Admin CRUD and Lesson administration; the audit log
        page (`/admin/audit`, admins) lists `audit_events` (2026-10-24) — partial: `/admin/audit`
        (`listAuditEvents`, admin) lists the latest 200 events with actor and account resolved in
        two queries (`Accounts().ListByIDs`, `Persons().ListByIDs`) and a client-side search;
        e2e in `admin.spec.ts`; the lesson actions live in the schedule's lesson sheet and toolbar
        (Lesson administration)
- [x] Demo: login page lists demo accounts per role (member, instructor, office, admin)
      (2026-10-23) — `GET /api/v1/demo/logins` in demo installations (WASM demo, `serve` without a
      database); one click signs in
- [x] Tests: auth flows, RBAC matrix per endpoint, mail rendering snapshots, e2e (login, admin edits a
      class) (2026-10-23) — auth flow tests (service and HTTP), mail golden files, the RBAC
      matrix (`internal/app/access_test.go`) and e2e sign-in, registration, impersonation, password
      reset and account settings; "office edits a class and the schedule shows the change" in
      `admin.spec.ts` (2026-10-24)

## Phase 4 — Booking & waiting list

Goal: the core member workflow.

Order: the booking ADR, then book and cancel (everything else builds on them), then auto-enrolment,
staff actions, waitlist, instructor view and "My lessons" in parallel. The store already has
bookings, booking events, `CountActive` and `Lessons().Lock`; the waitlist has a domain type but
no repository yet.

- [x] ADR "Booking rules": who books for whom (self and dependents, via the family check of Phase
      3); `BookingCutoff` closes booking that long before the start; after `CancellationCutoff` a
      cancellation is allowed but flagged late (or refused); how class membership becomes bookings;
      what a canceled lesson does to its bookings (2026-10-24) — ADR 0009: family incl. managed
      dependents, member rules for everyone (early access books early, hidden is a 404), late
      cancellations allowed and flagged, no cancel after the start, bookings of a canceled lesson
      stay, member auto-bookings (source `member`) ignore the cutoff and are canceled by the office
      edits that remove their lessons
  - [x] Open from ADR 0008: may guardians book for a managed dependent (own account, staff link
        or dependents of its own)? (2026-10-24) — yes, ADR 0008 and 0009
- [x] Book a lesson for yourself or a dependent, as one transaction:
      `InTx → Lessons().Lock → CountActive < capacity → Insert → booking_event → outbox`; booking
      cutoff and release window enforced; duplicate booking is a conflict
  - [x] `BookingService.Book` and `bookLesson` (`signedIn`); 409 reasons `already-booked`, `full`
        (the UI offers the waitlist), `closed` (cutoff, release window, canceled lesson);
        confirmation mail (EN/DE, golden files) (2026-10-24) — `service/booking.go`; `closed`
        carries `why` (cutoff, release, canceled, started); the mail goes to the acting account
  - [x] Availability in the schedule: free places and "booked by me / my child" per lesson in
        `getSchedule` and `getLesson`, from one `CountActiveByLessons` query instead of one per
        lesson (both stores, contract cases) (2026-10-24) — `freePlaces`, `bookedFor`,
        `bookingClosesAt`, `lateCancelFrom`; `CountActiveByLessons` and
        `ListActiveByLessonsAndPersons`
- [x] Cancel a booking (sign-out deadline configurable; late cancellation flagged); "also cancel
      following weeks" for class members
  - [x] `cancelBooking` for own and dependents' bookings; the late flag stored on the booking or
        its event (migration); a cancellation frees the place for the waitlist in the same
        transaction (2026-10-24) — `cancelBooking`; migration 00010 (`late` on bookings and
        events, `source`, `override`); the freed place invites the waitlist once it exists
  - [x] "Also cancel following weeks": cancels the person's future bookings of the same class in
        one transaction, after a preview of the dates (2026-10-24) — `previewCancelFollowing`
        (GET) and `cancelFollowing`; one summary mail
- [x] Class members are auto-enrolled into generated lessons and can release single dates (this
      replaces the old paper "swap list")
  - [x] Generation books the class members into each lesson it creates; adding a member books
        the future lessons, removing one cancels them; a class cannot have more members than
        capacity (2026-10-24) — `service/enrollment.go`: source `member`, `Since` and pauses
        honoured, an override when a lesson is full; only dates the family released
        (`bookings.released`) stay out, so re-joining or ending a pause books again; retired
        classes take no members; term, weekday and retire edits remove member bookings with their
        lessons
  - [x] Releasing a date is a normal cancellation (the place goes to the waitlist); taking it back
        is a normal booking while a place is free (2026-10-24) — a released date stays released
        for generation and enrolment; "cancel following weeks" pauses a member until a date
        (`class_members.paused_until`, migration 00011), the office ends a pause with
        `updateClassMember`
- [x] Staff actions: guest bookings (name + email, no account), trial flag, add or remove
      participants beyond capacity (explicit override)
  - [x] Office and instructor operations on a lesson's participants: add a person or a guest,
        mark trial, remove; booking beyond capacity needs an explicit override that is recorded in
        the booking event (2026-10-24) — `service/participants.go`: office on every lesson,
        instructors and accompanists on their own until the end of the lesson's day; guests with
        an optional email; trial with a note; `confirmOverCapacity`; staff find persons by name
        (`searchPersonsForStaff`); "Participants" in the lesson sheet; e2e `participants.spec.ts`
- [x] Waitlist: join and leave; when a spot frees up, the next entry gets a **timed invite** (random
      token, only the hash stored, expiry configurable); accepting books under the lesson lock;
      expired invites move on to the next entry; "stay on list" option (decision record required)
  - [x] ADR "Waiting list": invite expiry setting, what "stay on list" means, whether accepting
        needs a sign-in, invites stop at the booking cutoff (2026-10-24) — ADR 0010: an invite
        holds the place, 12 h by default and never past the cutoff; the mail link opens a confirm
        page that accepts without a sign-in; "stay on the list" re-queues at the end
  - [x] Store: `WaitlistRepo` (migration, both stores, contract cases incl. ordering by
        `JoinedAt` and lookup by token hash) (2026-10-24) — migration 00012
        (`waitlist_entries`, `settings.waitlist_invite_expiry`)
  - [x] Join and leave (self and dependents); position in the list shown to the member
        (2026-10-24) — `joinWaitlist`, `leaveWaitlist`; the lesson sheet shows the position
  - [x] Invite: every transaction that frees a place invites the next waiting entry and queues the
        invite mail; accepting books under the lesson lock and fails cleanly when the invite has
        expired (2026-10-24) — `service/waitlister.go`; accept in the app or by link
        (`/waitlist/accept`); booking and joining settle the list first; e2e `waitlist.spec.ts`
  - [x] Expiry job (`internal/jobs`, both targets): expires due invites and invites the next entry
        (2026-10-24) — `waitlist-expiry`, every minute, one transaction per lesson
- [x] Instructor view: today's lessons, participant list, attendance check (attended / no-show),
      notify participants
  - [x] `/teach`: today's and the next lessons of the signed-in instructor (office sees all), with
        participants including guests and trials (2026-10-25) — `listTeachLessons`; two weeks
        around today by day with attendance states, the office filters by staff; lessons open in
        the lesson sheet with its participants; e2e `teach.spec.ts`
  - [x] Attendance: mark attended or no-show per participant and "all attended"; only from the
        lesson start on (2026-10-24) — `setAttendance`, `markAllAttended`; in the lesson sheet's
        participant list
  - [x] Notify participants: a short message to everyone booked, as mails through the outbox,
        rate-limited (2026-10-24) — `notifyParticipants` (Reply-To the sender, 5 per hour per
        account, 3 per lesson per day); "Message participants" on `/teach`
- [x] "My lessons" (upcoming, history), booking audit trail for office/admin
  - [x] `/my-lessons` for the account and its dependents: upcoming with cancel, history with
        status, waitlist entries (2026-10-25) — `listMyLessons`, `listMyLessonHistory` (three
        months per page); waiting lists, the next four weeks and "Load earlier" history; e2e
        `my-lessons.spec.ts`
  - [x] Booking history (`listBookingEvents`, office) in the participant list of the lesson sheet
        (2026-10-24) — `GET /api/v1/admin/bookings/{id}/events`, "History" per participant
- [x] UI: book/cancel from schedule and detail views, for-whom selector (family), waitlist state,
      optimistic updates with rollback, toast feedback
  - [x] Lesson sheet: book / cancel / join waitlist with a for-whom selector when the account has
        dependents; states for full, closed, late cancellation and canceled lessons (2026-10-24) —
        `features/schedule/booking/BookingSection.tsx` books and cancels with the
        selector, warns before a late cancellation and offers "also cancel following weeks";
        joins, leaves, accepts and declines the waiting list
  - [x] List and week grid: free-place and booked badges; optimistic update with rollback on a
        409, toasts with the reason (2026-10-24) — `AvailabilityBadges`; e2e `booking.spec.ts`
- [x] Tests: concurrent booking (contract suite, both stores), cutoffs, waitlist invite
      expiry/acceptance, e2e booking flow in the demo
  - [x] Service tests: cutoffs at the exact edge, booking for a stranger's child, override,
        auto-enrolment and release, invite chain on expiry (2026-10-24) — cutoff edges, strangers,
        override, auto-enrolment, release and pause; the invite chain in `waitlist_test.go`
  - [x] e2e in the demo: book, cancel, join the waitlist and accept the invite from the demo
        mailbox, instructor marks attendance; desktop and phone (2026-10-25) — `booking.spec.ts`,
        `my-lessons.spec.ts`, `waitlist.spec.ts`, `teach.spec.ts` (the instructor takes attendance
        of a lesson that started today), `participants.spec.ts`

## Phase 5 — Calendar feeds

- [x] Per-subject feed tokens (location, instructor, personal bookings); random secret, revocable;
      only the token hash is stored (2026-10-25) — ADR 0011; migration 00015 `calendar_feeds`;
      feeds for the family's bookings, the own teaching and the schedule of the school or one
      location; one active link per subject, a new one revokes the old; `/api/v1/my/calendar-feeds`
- [x] `GET /ical/{token}.ics` (RFC 5545, VTIMEZONE, stable UIDs, cancelled lessons as
      `STATUS:CANCELLED`) (2026-10-25) — `internal/ical` (no dependencies); visibility of the
      owner at poll time, four weeks back; ETag and 304; rate limits per IP and per feed; tokens
      redacted in logs
- [x] UI: "Subscribe" dialog with copy-URL; demo: download via the bridge as a Blob (2026-10-25)
      — on the schedule, My lessons and /teach; the feed list in the account settings; e2e
      `calendar-feeds.spec.ts`
- [x] Tests: golden files, timezone/DST correctness (2026-10-25) — golden `.ics` per scope across
      the March and October changes and in New York; VTIMEZONE for Berlin, New York, Tokyo and
      Sydney

## Phase 6 — Passes & receipts

Goal: prepaid passes and legally sound receipts.

- [x] Pass types (visits, validity, price in minor units); sell or assign a pass (office); pass
      overview for members (2026-10-25) — ADR 0012; visit and time passes with a VAT rate;
      sell, assign, void and retire in the admin area; My passes for the family
- [x] Redemption on attendance or booking (configurable): oldest valid pass first, under the
      transaction, with a unique redemption per booking; refund on cancellation within the deadline
      (2026-10-25) — `service/ledger.go` + `redemption.go`: on transitions only, time passes
      first, late cancellations and no-shows cost a visit, class-member bookings never redeem; a
      moved lesson or a voided pass moves its visits
- [x] Receipts: gapless per-year numbering via `receipt_counters` in the same transaction; immutable
      receipts and lines; corrections only via cancellation receipts referencing the original
      (2026-10-25) — migration 00017 with triggers against UPDATE/DELETE; issued with the sale;
      tax mode and VAT snapshotted; Storno voids an unused pass
- [x] PDF generation in pure Go (works in WASM too), template driven by settings (school legal data,
      tax note, logo); email receipt via outbox (2026-10-25) — changed by decision: typst compiled
      to WebAssembly (`tools/receipt-pdf`, reproducible Docker build, Git LFS), run with wazero on
      the server and with the browser's WebAssembly in the demo; mail with the PDF attached
- [x] UI: pass management, receipt list/detail, PDF download (Blob in the demo) (2026-10-25) —
      pass types, person passes, receipts page with Storno, receipt settings, My passes and My
      receipts; e2e `passes.spec.ts`, `receipts.spec.ts`
- [x] Tests: concurrent redemption, numbering under concurrency, PDF golden tests (text extraction)
      (2026-10-25) — both stores; real typst renders in all tax modes; `internal/pdf/pdftext`

## Phase 7 — Communication & administration

- [x] Announcements: audience (all / role / course type / lesson), publish window, in-app display
      (dismissible), optional email (2026-10-25) — ADR 0014; audience also by class; banner and
      home list, dismissed per account; email sent by the `announcements` job in batches, with a
      school-wide daily limit; edits never re-send; e2e `announcements.spec.ts`
- [x] Mail log (outbox history with status, resend) (2026-10-25) — admins only; filtered and
      paged, no bodies of secret-link mails; a failed mail is queued again (audited); the daily
      `purge-mail` job drops bodies after 30 days and rows after 180
- [x] Statistics dashboard: bookings per week, utilisation per class/room, no-show rate,
      active members, pass sales (aggregated in Go; shadcn charts) (2026-10-25) —
      `service/statistics.go` from a constant number of store reads; plus late cancellations,
      trials and guests, waitlist demand and figures per instructor; recharts in a lazily loaded
      chunk; e2e `statistics.spec.ts`
- [x] Member CSV import with column mapping, dry-run preview, idempotent re-import (`member_no`),
      import run history (2026-10-25) — ADR 0015; persons and guardians, UTF-8 or Windows-1252,
      `;`/`,`/tab, German or ISO dates; the dry run is the same code rolled back; CLI
      `studio-schedule import`; a fictional sample file in the demo; e2e `import.spec.ts`
- [x] Duplicate detection and merge for persons (email, name + birth date) (2026-10-25) — live
      strong and weak matches, family emails excluded, "not a duplicate" dismissals; the merge
      moves everything to the survivor through the ledger and keeps the duplicate as a hidden
      tombstone for its receipts; audited; e2e `duplicates.spec.ts`
- [x] Settings UI: school profile, branding (name, accent colour, logo), timezone, locale, release
      rule, cutoffs, receipt data (2026-10-25) — ADR 0013; one card per area, admin edits and the
      office reads; accent checked for contrast with a derived dark variant; logo and name in the
      header; timezone and currency only during first setup (`studio-schedule init`); the UI
      follows the school's language unless the visitor chose one; e2e `settings.spec.ts`
- [x] Tests: import fixtures, merge correctness, statistics against seeded data (2026-10-25) —
      `internal/csvimport` fixtures; merge and import scenarios on both stores
      (`internal/service/servicetest`); statistics of the demo seed in both stores

## Phase 8 — Production readiness

- [x] Single binary with embedded UI; `Dockerfile` (distroless); `docker-compose.yml` (app +
      Postgres + Mailpit) (2026-10-25) — changed by decision: Alpine runtime (amd64 + arm64,
      non-root, `studio-schedule healthcheck`); compose with PostgreSQL 18 and Mailpit;
      `LOG_FORMAT`/`LOG_LEVEL`; see docs/operations.md
- [ ] Container image to ghcr.io on release (GitHub App or PAT token for release-please so tags
      trigger workflows)
- [x] Structured logging (slog), request IDs, `/healthz` + `/readyz`, optional Prometheus metrics
      (2026-10-25) — request IDs from trusted proxies or generated, in logs and problem bodies;
      access log at Info with secret path parameters redacted; `/readyz` checks the schema
      (cached 2 s); metrics only on `METRICS_ADDR`
- [x] Backup/restore documentation (pg_dump), migration strategy, upgrade notes (2026-10-25) —
      docs/operations.md; forward-only migrations in production; `just restore-drill` (dump,
      restore, `migrate status`, `/readyz`, sign-in) runs in `just ci` and CI
- [x] GDPR: data export per person, deletion/anonymisation (receipts retained), guest-data retention
      job, privacy notes in docs (2026-10-25) — ADR 0016: JSON export by the office and for one's
      own family, anonymised person kept as "Deleted person", guest data cleared after 12 months,
      audit 3 years, imports 1 year; account administration and self-delete; docs/privacy.md
- [x] Playwright smoke suite against `serve` + Postgres in CI (real cookies, CSRF, Postgres semantics)
      (2026-10-25) — web/e2e-server with Mailpit (health, request IDs, CSRF, headers, booking,
      receipt PDF, password reset); demo data seeded around today (`seed --anchor today`)
- [x] Security review (OWASP ASVS L1 checklist), dependency scanning (Renovate, govulncheck)
      (2026-10-25) — docs/security.md; common-password check, password-changed mail, 30-day
      sessions, `__Host-` cookie, office searches by POST, no echoed input in problem bodies
- [x] Operator documentation: configuration reference, deployment guide (2026-10-25) —
      docs/operations.md, its configuration reference checked against the code by a test

## Phase 9 — Backend test coverage

Goal: at least 90 % statement coverage of the Go backend, held there by a CI gate, from tests that
check behaviour rather than lines. Coverage is measured as `tools/coverage` does it: one
cross-package profile (`go test -coverpkg=./...`, with the PostgreSQL store tests), leaving out the
shared test suites (`storetest`, `servicetest`), generated sqlc code and `tools/`.

Baseline (2026-10-31): **89.3 %** (13,948 of 15,611 statements). The most uncovered statements are
in `internal/service` (86.3 %, 1,046 uncovered), `cmd/studio-schedule` (70.3 %, 152),
`internal/httpapi` (93.5 %, 106) and `internal/store/postgres` (91.6 %, 91). A per-package count
gives only 53.8 %, because most code is run by other packages' tests.

Result (2026-11-01): **98.7 %** (15,370 of 15,565), gated at 95 % total and 80 % per package. Three
bugs found and fixed on the way: a Storno could undo a concurrent merge, a typst instance grown to
the 4 GiB maximum was kept, and `migrate up` hid the migrations applied before a failing one.
Behaviour pinned as it is, open for a decision, is in the notes of the items below.

- [x] Coverage after every test run: `just test-cover` writes `coverage.out` and `just coverage`
      reports it. `just ci` ends with the report. CI prints it in the job log and the job summary,
      also when a test failed, and uploads the profile (2026-10-31): `tools/coverage` (merges the
      per-binary blocks, lists the packages with the most uncovered statements)
- [x] `internal/service`: the uncovered error paths and edge cases of each service, as service tests
      or `servicetest` scenarios on both stores. Store errors only a broken database produces come
      from a fault-injecting `store.Store` wrapper in tests. A defensive branch that cannot be
      reached is removed, not excluded (2026-10-31): `FaultStore` (`internal/service/fault_test.go`,
      its repositories generated from `store.go` by `faultgen.go`, `go generate ./internal/service`)
      fails any store call, `InTx` or `Commit`; `SweepFaults` fails each call of an operation in turn
      and checks the error comes back and the data is unchanged. Sweeps over every service, focused
      edge-case tests, new `servicetest` cases (merge, import, privacy, push). `internal/service`
      86.3 % → 99.3 % (55 uncovered), total 89.4 % → 95.7 % (14,914 of 15,585). Bug fixed: a Storno
      could write back a pass a concurrent merge had moved (now a conflict). Removed as unreachable:
      `newToken`'s error (`crypto/rand` cannot fail), four more branches. Left: `Validate()` before
      writes of entities a service builds itself (ADR 0002 convention) and broken-reference branches
      the foreign keys prevent. Pinned for a decision: a booking redeemed on booking, canceled in time
      after a switch to redeem-on-attendance, keeps its visit spent
- [x] `cmd/studio-schedule`: the commands run in-process through cobra (`init`, `import`,
      `migrate`, `seed`, `healthcheck`, `vapid-keys`, `serve` startup refusals) against the test
      database (2026-10-31): the package has its own lazily started test database
      (`testdb_test.go`); `serve` starts for real on PostgreSQL and on the demo store and refuses an
      outdated, newer or damaged schema, a bad SMTP config and a used port; `main` returns its exit
      status through `runMain`. 70.3 % → 97.7 % (12 uncovered: failing library calls, shutdown
      timeouts, a broken embedded renderer); total 89.3 % → 90.3 %. Found: `migrate up` does not list
      the migrations it applied before a failing one (`postgres.Migrator.Up` drops
      `goose.PartialError.Applied`; fixed in the store on 2026-11-01, the reproducer runs);
      `seed`/`serve` call a gap in the goose table "outdated, run migrate up", which `migrate up`
      refuses
- [x] `internal/httpapi` and `internal/app`: the error mappings (`toHTTPError`, conflicts, field
      errors) and middleware branches without a test (2026-10-31): every `toHTTPError` mapping (a
      table test), the 500s a failing store causes through `app.New` (45 endpoints, on a test-only
      `faultStore` in `internal/app` that fails the transactions touching a poisoned repository:
      generic detail, `requestId` = `X-Request-Id`, the cause logged once at ERROR), the domain
      branches (impersonation, email change, rate limits, merge and import summaries, mail-log
      cursors, announcement audiences, waiting-list invites, the data export), an operation without
      an access level (500, fails closed), readiness probes that give up, path redaction, and the
      mail, push and purge jobs. `internal/httpapi` 93.5 % → 99.9 % (106 → 1 uncovered),
      `internal/app` 91.8 % → 99.7 % (27 → 1), total 89.4 % → 90.5 % on this item alone. No bug
      found; two unreachable branches removed (`getMe`'s own principal check behind
      `AccessSignedIn`, the error of encoding the all-string web manifest). Left: the panic on
      unparsable embedded mail templates (`app.newRenderer`) and the renaming of a booking-rules
      `FieldError` in `updateBookingRules`, unreachable while Huma's schema limits equal the
      domain's
- [x] Stores: the postgres error paths, and the sqlc queries no test runs. Each gets a contract case
      in `storetest` or is deleted as unused (2026-10-31): every one of the 226 sqlc queries has a
      caller, none deleted. Contract cases on both stores (read-only rejection of the push,
      announcement, receipt PDF, guardian and class member writes; unknown announcement, duplicate
      mailing, `GetCancellation` of no receipt, ordered `ListCancellations`, empty PDF), postgres
      tests that make the errors happen (out-of-range numbers fail before any SQL, a canceled context
      in a live transaction, corrupt JSON rows, a closed pool, a commit on a terminated backend,
      failed `Up`/`Down`, a broken goose version table), memory snapshot corruption and the lock
      wait. `internal/store/postgres` 91 → 12 uncovered, `internal/store/memory` 46 → 6,
      `internal/store` 4 → 0; total 89.4 % → 90.1 % (14,074 of 15,613). The rest is unreachable:
      library errors on fixed input (embedded migrations, `json.Marshal` of typed details, the
      second parse of a URL), `bytes.Buffer` writes, and a booking without its lesson. Found: memory's
      `PutPDF` checks read-only before an empty PDF, postgres the other way round; `Restore`
      accepts trailing data after a snapshot
- [x] Adapters: `internal/mail/smtp` (a fake SMTP server: rejects, timeouts, TLS), `internal/webpush`
      (an `httptest` push service: 404/410 drops the subscription, 429/5xx retries),
      `internal/pdf/pdftext`, `internal/pdf/typstwasm` (malformed input, module errors)
      (2026-10-31): smtp 87.7 → 100 % (4xx/5xx at every step, permanent vs. transient; hangs, a
      write cut off in DATA, failing randomness), webpush 83.7 → 90.6 % (every response class,
      no redirects, refused keys and payloads), pdftext 84.2 → 100 % (hand-built PDFs),
      typstwasm 77.4 → 100 % (hand-assembled modules: traps, out-of-memory pointers, waiting,
      cancel, close). Total 89.4 → 89.9 % (14,012 of 15,592). Bug: an instance grown to the
      4 GiB maximum was kept, as wazero's `Memory.Size` overflows to 0; it now counts pages.
      Removed: error checks of writes into a `bytes.Buffer` (smtp), the audience re-parse and
      the salt's `rand.Read` check (webpush). Left: webpush's checks on stdlib crypto calls,
      which our inputs cannot fail but whose docs do not promise it. "No AUTH offered" is a
      transient error (retried up to `MaxMailAttempts`); pdftext misses `/Type /Page>>` and
      multi-line `/Font` dicts (pinned, typst does not write them)
- [x] Gate: CI fails below 90 % (`tools/coverage -min 90`, also in `just ci`). The floor goes up as
      coverage grows and only goes down by a decision recorded here. Is there also a per-package
      floor (e.g. 80 %), so one well-tested package cannot hide another? To be decided when the
      gate lands (2026-11-01): with all items above on main the total is 98.7 % (15,370 of
      15,565). Decided: a total floor of **95 %** and a per-package floor of **80 %** (lowest
      today: `internal/pdf` 83.3 %, 1 of 6 statements uncovered; `internal/webui` 85.7 %).
      `tools/coverage -min -min-package` names each miss on stderr and exits 1; the floors are
      `coverage_min` and `coverage_min_package` in the justfile, which `just coverage` (and so
      `just ci`) and CI's "Coverage gate" step (after green tests, behind the always-run report)
      use
- [x] Docs: CLAUDE.md (new code comes with tests that keep the gate green; how to read
      `just coverage`), docs/architecture.md "Testing" (2026-11-01): the test layers, `FaultStore`
      and `SweepFaults`, the cross-package measurement and the gate

## Phase 10 — Events & workshops

Goal: a public events page for workshops, intensives and other special dates. Each event has its
own price, its own sign-up and its own receipt, apart from the weekly timetable and its passes.

Today a workshop is a one-off lesson of kind `workshop` (ADR 0007). Like every lesson, any pass
pays for it (ADR 0012), and receipts exist for pass sales only. This phase gives events their own
entity, price and payment path. The schedule keeps showing their sessions.

Order: the ADR first. Then the domain and the store, the office's event administration, the public
page and registration, and payment with receipts. Statistics, theme slots and the demo seed come
last.

- [ ] ADR "Events and workshops" (0019) settles the open decisions, each one asked first:
  - [ ] Model: an event (title, slug, description, image, course type, instructor(s), location)
        groups one or more sessions, which stay lessons (kind `workshop`) and so keep the schedule,
        calendar feeds, attendance and room overlap checks. Is a registration for the whole event
        only, or can a person sign up for single sessions?
  - [ ] Pricing: one gross price plus VAT rate per event (`domain.Money`, basis points as for pass
        types), or price tiers (early bird until a date, member/guest, reduced)? Does any pass, or
        only a named pass type, pay for an event (default: no pass pays for one)?
  - [ ] Payment: no online payment. A registration starts `unpaid`, and the office marks it paid at
        the desk. Is that the receipt moment? ADR 0012 then needs an amendment: receipts cover
        event fees too (lines, VAT snapshot, Storno, the same gapless counter)
  - [ ] Who registers: members for themselves and their dependents (family rules, ADR 0008). Can
        guests sign up from the public page with a name and email (confirmed by a mail link, rate
        limited), or does the office add them only, as for lessons today?
  - [ ] Capacity and waitlist: one capacity per event, checked under an event lock, and the
        existing waitlist chain reused (ADR 0010)? The lock order then becomes event → lessons
  - [ ] Cancellation: an event-specific deadline, refund or credit rules after it, and what an
        event cancelled by the school does to registrations and receipts
  - [ ] Visibility and release: publish date, sign-up window (opens/closes), and whether events
        ignore the timetable's release window as workshops do now (ADR 0003)
  - [ ] Existing one-off `workshop` lessons: do they stay as plain lessons, or does a migration or
        the demo seed turn them into events?
- [ ] Domain and store: `domain.Event`, `EventRegistration` (person or guest, status, paid state,
      price snapshot), typed IDs, `Validate()`; `Events()`/`EventRegistrations()` repos in memory
      and postgres (migration, sqlc), contract cases in `storetest`
- [ ] Office administration (`/api/v1/admin/events`, `office`): create/edit/delete events, add,
      move and remove sessions (lessons) under the class/lesson rules of ADR 0007, list
      registrations, add a registration for a person or guest, mark paid, cancel; `withConflict`
      409s and 422 field errors as elsewhere; audited
- [ ] Public API: `listEvents` (upcoming, published; free places, price, sessions) and `getEvent`
      by slug (`public`); `registerForEvent` and `cancelEventRegistration` (`signedIn`, and for
      guests if the ADR allows it); confirmation and cancellation mails (EN/DE, golden files)
- [ ] Payment and receipts: marking a registration paid issues a receipt in the same transaction
      (`ReceiptService`, the PDF template gets an event line), Storno on refund; "My events" with
      the registrations of the family and their receipts
- [ ] Web: `/events` list and `/events/$slug` detail page (dates, sessions, instructor, location,
      price, places left, register/cancel, waitlist), a nav entry, event sessions in the schedule
      linking to their event; office pages for events, sessions and registrations; every string in
      `locales/{en,de}`; school dates through `schoolTime.ts`
- [ ] Themes: `EventList` and `EventDetail` slots (ADR 0018) in Programme, a Werkstatt variant
- [ ] Integration: announcements can target an event's participants (ADR 0014); event sessions in
      calendar feeds; event revenue and sign-ups in the statistics; events in the person data
      export and anonymisation (ADR 0016)
- [ ] Demo seed: two or three fictional events around today (one free, one nearly full, one with
      a guest sign-up), relative to the visit day like the existing workshops
- [ ] Tests: concurrent registration for the last place (both stores), price snapshot and receipt
      golden files, cancellation edges, visibility of unpublished events; e2e `events.spec.ts`
      (browse, register, cancel, the office marks paid and the receipt appears) on desktop and
      phone; docs: architecture.md, glossary (event, registration), CLAUDE.md code map

## Phase 11 — Command line

Goal: the binary administers a school, not only serves it. An operator or admin can do on the
server's shell what would otherwise need a browser: get back in when locked out, script
maintenance, answer GDPR requests, export receipts for the tax adviser, check data integrity.

Today the commands cover setup and operations only: `serve`, `migrate up|down|status`, `seed`,
`init`, `import`, `healthcheck`, `openapi`, `version`, `vapid-keys`. Nothing covers day-to-day
administration. Job state lives only in the serving process (`internal/jobs`), so no command can
show it.

Order: the ADR, then the shared command layer (the MCP server of Phase 12 builds on it), then the
command groups by need: recovery and diagnosis first, then data, then reports.

- [x] ADR "Command line" (0020) settles the open decisions, each one asked first (2026-11-01)
      — [ADR 0020](docs/decisions/0020-command-line.md):
  - [x] Local, remote or both. A local command opens the database (`DATABASE_URL`) and calls the
        services through the `app` wiring, as `import` does. It works while the server is down
        but needs shell access. A remote command calls the HTTP API with a personal access token
        (`--server`, `--token`; a new `api_tokens` table with hashed tokens, the account's access
        level, an expiry, revocable on the account page). It works from a laptop and, unchanged,
        against the PHP backend (Phase 13). Proposal: operator commands local only, admin commands
        both, behind one client interface (2026-11-01) — local only, behind `cli.Env`; a remote mode
        with tokens is a later item with its own ADR
  - [x] Actor: who does a local command act as? A `system` principal with the OS user in the
        audit details, or `--as <admin email>` with that admin's access level (ADR 0005)?
        (2026-11-01) — `--as <email>` (env `STUDIO_AS`): that account's role, checked against the
        command's access level; `Principal.Via` puts the channel and OS user into
        `AuditDetails.Via`; operator commands take none
  - [x] Output for scripts: text tables by default, `--output json` with a stable schema (the
        OpenAPI types where a command mirrors an operation), documented exit codes (0 ok, 1
        failed, 2 usage, 3 not found, 4 conflict), colour only on a terminal (2026-11-01) — as
        proposed; JSON is the operation's response body where one exists; exit 4 also for a role too
        low
  - [x] Safety: a command that deletes, anonymises, mails or cancels asks for confirmation on a
        terminal and needs `--yes` otherwise. A `--dry-run` where the service supports one (as
        `import` does) (2026-11-01) — as proposed; without a terminal and `--yes` exit 2
  - [x] Job state: store the runs (`job_runs`: job, start, end, result, error), so `jobs list` can
        show them from another process, or keep them in memory and offer `jobs run` only
        (2026-11-01) — stored in `job_runs` (both stores, pruned after 30 days), a per-job lock
        (advisory on PostgreSQL), a run that loses the lock is `skipped`
- [x] Command layer: `cmd/studio-schedule/cli` (command → service call → presenter), no SQL in
      `cmd`. The services' validation and field errors are printed with the field's name. Global
      flags `--database-url`, `--output`, `--yes` (and `--server`, `--token` if the ADR chooses
      remote). `completion` for bash, zsh, fish and PowerShell. docs/cli.md is generated from
      cobra and CI checks it like `api/openapi.json`; `docs_test.go` covers the new flags
      (2026-11-01): `cli.Spec` (use, flags, access level, destructive, its JSON), `cli.Env`
      (services from the new `app.NewServices`, which `app.New` uses too; the `--as` principal;
      `Confirm`, `ReadLine`), the presenter and one error mapping (`ExitCode`, `Report`); a group
      is one file and one line in `root.go`. Global flags `--database-url` (no command has its
      own any more), `--output`, `--yes`, `--as` (env `STUDIO_AS`, `AuthService.Actor`,
      `Principal.Via` → `AuditDetails.Via`, shown as "via command line (<os user>)" in the audit
      view). `import` is on the layer and acts as the office account of `--as`. docs/cli.md comes
      from `TestCLIReference` (`just cli-docs`, in `check-generated`). Deviations, amended in the
      ADR: the layer aligns tables itself (`tabwriter` counts colour escapes as width), and the
      commands not on the layer refuse `--as`, `--yes` and `--output json`. Terminals are told by
      `os.ModeCharDevice` (no `x/term`), so `/dev/null` counts as one: a destructive command then
      prompts, reads nothing and refuses with exit 2 all the same
- [x] Diagnosis: `config check` validates the environment and flags without starting and prints
      the effective configuration with secrets redacted. `doctor` checks the database, the schema
      version, time zone and currency, the SMTP login, the VAPID keys, an https `BASE_URL` and the
      receipt module: one line per check, exit 1 if one fails (2026-11-01): both take serve's
      flags; `config check` exits 2 on an invalid value. `doctor` checks config, database,
      schema, settings (the domain's rules), smtp (`smtp.Sender.Verify`: STARTTLS and AUTH, no
      MAIL; skipped without `SMTP_HOST`), vapid (skipped without keys), base-url (http on
      localhost or unset is a warning) and receipts (`service.SampleReceiptInput` rendered by
      typstwasm), as ok/fail/warn/skip lines or JSON `{ok, checks}`
- [ ] Accounts and access: `accounts list|show`, `accounts create --role`, `accounts set-role`,
      `accounts reset-password` (prints a setup link like `init`, or mails it with `--mail`),
      `accounts sessions revoke` (one account or all), `accounts delete`. The guards of ADR 0016
      (`last-admin`, `own-account`) apply. This is the way back in when the last admin has
      forgotten their password
  - [x] The commands (2026-11-01): `accounts list|show|create|set-role|disable|enable|`
        `reset-password|sessions revoke|delete` (`cmd/studio-schedule/accounts.go`). An account is
        named by its email or ID (`AuthService.AccountByRef`). The JSON is the operations' bodies
        (`httpapi.NewAccountEntry`, `NewAccountList`, `NewPersonAccount`). `create` (with
        `--person <id>` and `--role`) is `office`, like `createPersonAccount`; the rest is `admin`.
        `reset-password` prints a one-hour link (`AuthService.IssuePasswordReset`), or with
        `--mail` mails it.
        `sessions revoke <account>|--all` prints how many running sessions ended
        (`RevokeSessions`, `RevokeAllSessions`, built on `endSessions`). `disable`, `create`,
        `reset-password`, `sessions revoke` and `delete` confirm. A refusal names its guard
        (`own-account`, `last-admin`, `in-use`; exit 4). The `last-admin` guard cannot be reached
        from the command line alone (amended in ADR 0020)
  - [ ] Audit the reset link and the ended sessions: new audit actions need a migration
        (`audit_actions`), the domain constants, the audit page's strings and the
        `listAuditEvents` description. The round's lanes kept the store and the API out of reach.
        The impersonations that end are audited already
  - [ ] An admin whose `init` link expired before the admin was confirmed cannot act as
        themselves (`--as` needs a confirmed account), so `reset-password` does not reach them.
        Without mail, nothing does. Decide whether `init` may issue a new link for the unconfirmed
        first admin
- [ ] Settings: `settings get [area]` and `settings set <area> key=value…` through
      `SettingsService` (same validation; time zone and currency stay locked once used).
      `settings export|import` as JSON, to set up a second school the same way
- [x] Jobs: `jobs list` (name, interval, last run and result) and `jobs run <name>` (one job now,
      e.g. `generate-lessons`, `purge-personal-data`, `deliver-mail`). A database lock keeps a
      serving instance and the command from running the same job at once (2026-11-01)
  - [x] Storage and lock (2026-11-01) — the Runner records every run in `job_runs` (both stores,
        `Tx.JobRuns()`; the memory store leaves the runs out of the demo's snapshot), under
        `Store.TryLockJob` beside the transactions: on PostgreSQL a session advisory lock on one
        dedicated pool connection for all of a process's job locks (one connection per lock could
        exhaust the pool and deadlock), on the memory store a mutex per name. A held lock records
        the run as `skipped`. `purge-personal-data` deletes runs after 30 days. `Runner.Jobs`,
        `Statuses` and `RunNow` (unknown name `ErrNotFound`, held lock `ErrConflict`) are ready for
        the commands
  - [x] The `jobs list` and `jobs run <name>` commands on the command layer (2026-11-01) —
        operator commands (no `--as`) on the jobs serve would run: `Env.Jobs` builds them with
        `app.Jobs` and the senders of serve's own constructor (`newDelivery`, through
        `cli.System.Delivery`), so `deliver-mail` needs `SMTP_HOST` and `deliver-push` the VAPID
        keys as in serve; the receipt renderer starts on the first receipt it renders. `jobs list`
        shows interval, latest start (school time zone), duration, result and error; `jobs run`
        prints the run and exits 1 when it failed (also when the job's error wraps a conflict), 3
        for an unknown job (listing the jobs), 4 for a held lock. Their JSON has no operation, so
        docs/cli.md documents it. Not destructive (ADR 0020, amendment): a job only does early what
        serve does on its tick, which its help spells out
- [x] Mail: `mail test --to` renders a test mail and sends it through the configured SMTP.
      `mail list --status failed`; `mail requeue <id>|--failed` is audited as `mail-requeued`
      (ADR 0014) (2026-11-01) — `mail test --to [--locale]` is an operator check like doctor's
      smtp line: the new `test` template (en, de; not in `TemplateNames`, since it never goes
      through the outbox) straight through the sender of `newDelivery`, exit 1 with the server's
      error, 2 without `SMTP_HOST`. `mail list` (`admin`, `--status`, `--template`, `--cursor`;
      the hint for the next page on stderr) prints searchMails' body, never bodies.
      `mail requeue <id>|--failed` (`admin`) prints requeueMail's body, or with `--failed`
      (`MailLogService.RequeueFailed`, one transaction, each mail audited) the counts; mails with a
      secret link stay failed. Deviation (ADR 0020, amendment): both forms confirm, since requeuing
      mails. Not built: a recipient filter, which as a search term would come from stdin
- [ ] Persons and privacy: `persons search` reads the search term from a prompt or stdin, never
      an argument (shell history, `ps`; ASVS 8.3.1). `persons export <id> -o file.json`,
      `persons anonymize <id>` (ADR 0016), `persons duplicates`,
      `persons merge <survivor> <duplicate>` (ADR 0015)
- [x] Timetable: `lessons list --date|--from --to`, `lessons generate --until`,
      `lessons cancel <id> --notify` (a sick instructor, from an SSH app on a phone),
      `terms list`, `classes list` (2026-11-01): all `office`, as the office's pages.
      `lessons list` (getSchedule's body; `--to` inclusive, at most 42 days) shows ID, date,
      time, class or kind, instructor, room, active bookings/places and status in the school's
      zone; `lessons generate --until` (`LessonGenerator.GenerateUntil`, now in `app.Services`)
      creates what has not started from today on and prints the count;
      `lessons cancel <id> [--note]` and the added `lessons restore <id> [--confirm-room-overlap]`
      confirm, name the lesson and print cancelLesson's/restoreLesson's body. `terms list`
      (listTerms) marks the current term, `classes list [--term]` (listAllClasses, retired ones
      included) defaults to it (`TimetableService.CurrentTerm`). Deviation: no `--notify`, as
      `CancelLesson` always mails everyone booked: the command says so in its help and question
- [ ] Passes and receipts: `passes list --person`, `receipts list --from --to`,
      `receipts pdf <number> -o file`, `receipts export --year --format csv` (for the tax
      adviser). `receipts verify` checks the numbers are gapless per year, every Storno names its
      original, and the totals match the lines
- [x] Integrity: `db check` compares pass visits with redemptions, bookings with capacity,
      tombstones with what still references them, and the receipt counters. It is read-only and
      exits 1 on a finding (2026-11-01): `service.IntegrityService.Check` (the MCP server's entry)
      runs the named checks `passes`, `capacity`, `tombstones` and `receipt-counters` in one
      read-only transaction over five aggregate store reads (`ListUnbalanced`, `ListStray`,
      `ListOverCapacity`, `ListTombstoneReferences`, `ListStates`; one query each on PostgreSQL);
      a finding names its check, the entity and its ID, never personal data. `db check [--only …]`
      prints them as a table or `{ok, checks, findings}`, exit 1 on a finding, 2 for an unknown
      check. The checks agree with the services, not a stricter reading: a redemption of a late
      cancellation stays, a lesson over capacity is a finding only beyond the bookings whose
      events record an override (ADR 0009), and an anonymized person's passes stay (ADR 0016), so
      only a pass sold after the anonymization, and only an active booking of a lesson that starts
      after it, is a finding. A clean demo school and data written only through the services
      (merge, anonymize, cancellations, overrides) have none, on both stores. Deviation, amended in
      ADR 0020: `app.Services` has no integrity service yet, so the command builds it on the store
      it opens with the process's opener (`newDBCmd(app, sys.Open)`)
- [ ] Reports: `stats --from --to` (the statistics service as a table or JSON);
      `events list|registrations <slug>|mark-paid` (Phase 10). `stats` is done (2026-11-01):
      `office`, `--range` (last4Weeks by default, last12Weeks, term, custom) or `--from --to`,
      the totals and the tables by period, class, room, course type and instructor; the top
      participants only with `--participants`, the JSON is getStatistics's full body. The
      `events` commands wait for Phase 10
- [ ] Tests: every command runs in-process (cobra `Execute` with buffers) on the memory store
      and PostgreSQL. They cover golden text and JSON output, exit codes, and the refusal without
      `--yes`. The commands count towards the coverage gate (Phase 9)
- [ ] Docs: docs/cli.md (generated); docs/operations.md (recovery, cron examples for scripted
      exports); the CLAUDE.md code map

## Phase 12 — MCP server

Goal: an assistant (Claude Desktop, Claude Code or any other MCP client) can work with a school.
It can answer "who is on the waiting list for Thursday's ballet class?", find a free room, draft an
announcement, or run the CLI's checks. It follows the same rules as every other caller: the
account's access level, the services' validation, the audit log.

Order: the ADR, then a read-only server over stdio built on the command layer of Phase 11, then
resources and prompts, then opt-in writes, then the remote transport if the ADR chooses it.

- [ ] ADR "MCP server" (0021) settles the open decisions, each one asked first:
  - [ ] Shape: `studio-schedule mcp` over stdio (the client starts the binary; it uses the
        database directly or the remote mode of Phase 11), a streamable HTTP endpoint `/mcp` in
        `serve` (for clients that connect to a URL), or both. An endpoint in `serve` becomes part
        of the API the PHP backend (Phase 13) must serve too. stdio in remote mode works against
        both backends without porting
  - [ ] Authentication: stdio acts as the CLI's actor (Phase 11). HTTP needs OAuth 2.1 as the MCP
        specification asks (the app as authorization server, PKCE, client registration), or the
        personal access tokens of Phase 11 as bearer tokens
  - [ ] One definition: are tools generated from the CLI command layer (name, input schema,
        handler, presenter), so a command and its tool cannot drift? The JSON output of Phase 11
        would then be the tool result
  - [ ] Which tools: read-only by default, with writes opted in per group by an admin (a setting
        or a flag). Never `accounts delete`, `persons anonymize` or `migrate down`. Are the tool
        annotations (`readOnlyHint`, `destructiveHint`) enough, or does a write need a
        confirmation round trip?
  - [ ] Personal data and minors: what may a language model see? Minimise data per tool (names
        and contact data only where the question needs them), make the office opt in, audit
        every tool call (`mcp-tool-called`: tool, actor, never the arguments of a search). A
        docs/privacy.md section explains that the assistant's provider sees what the tools return
  - [ ] SDK: the official `github.com/modelcontextprotocol/go-sdk` (server side only, kept out of
        `cmd/wasm` by `check-wasm-deps`)
- [ ] Read tools: the schedule (date range, location, course type), a lesson with participants
      and waitlist (staff and office), free places, rooms free at a time, person search, a
      person's bookings and passes, statistics, receipts, the mail log, `doctor`, `db check`
- [ ] Resources: the school settings, the current term's timetable, the glossary (so the model
      uses the school's words), and templates for `lesson://{id}` and `person://{id}` (office)
- [ ] Prompts in the school's language: "weekly overview", "whom to invite from the waiting
      list", "draft an announcement for a canceled lesson"
- [ ] Write tools (opt-in): book or cancel for a person (the booking rules apply), cancel a
      lesson with notification, create an announcement as a draft (the tool never sends one; the
      office publishes it), requeue failed mail, run a job
- [ ] Remote transport (if decided): `/mcp` behind the access middleware, with rate limits,
      request IDs and the ADR's authentication. The WASM demo has no MCP server (its transport
      is the browser), and it shows nothing that suggests one
- [ ] Tests: an in-process MCP client (go-sdk) against the server on both stores. They cover the
      tool list per access level, valid input schemas, golden results, refused writes and audit
      rows, and each tool against the CLI command it mirrors; the coverage gate of Phase 9 applies
- [ ] Docs: docs/mcp.md (setup for Claude Desktop, `claude mcp add` for Claude Code, other
      clients; the tool list; privacy); the CLAUDE.md code map

## Phase 13 — PHP backend

Goal: a second, equal backend in PHP for schools whose hosting runs PHP, not a Go binary. It
serves the same `/api/v1` contract and the same embedded web UI on the same PostgreSQL schema. A
school can switch between the Go and the PHP backend without migrating data. The Go app stays the
reference implementation: it owns `api/openapi.json` and the migrations, and it alone runs the WASM
demo.

The previous system had two backends whose role numbers and fields drifted apart (see
[Lessons](#lessons-from-the-previous-attempt)). So the PHP backend writes no contract of its own.
A black-box conformance suite runs against both backends in CI, and a PHP feature counts as done
only when its cases pass there.

Order: the ADR, then the conformance suite (proven against the Go server first), then the PHP
skeleton, then the API areas in the order of the phases. From then on, every new feature (Phase 14
included) lands in both backends.

- [ ] ADR "PHP backend" (0022) settles the open decisions, each one asked first:
  - [ ] Target hosting: shared hosting (Apache/nginx + PHP-FPM, cron, no daemon, often no
        `exec`), a container, or both. This decides the next three questions
  - [ ] Database: PostgreSQL only (same schema, data portable between the backends) or also
        MySQL/MariaDB, which shared hosts offer more often. MySQL means a second dialect: no
        `timestamptz`, no partial unique indexes, other triggers for the immutable receipts, and a
        second set of store tests
  - [ ] PHP version and framework: plain PSR-7/15 with small libraries (Slim or similar,
        Doctrine DBAL, symfony/mailer, symfony/uid, Twig), Symfony, or Laravel. The layering
        `http` → `service` → `store` interfaces must carry over
  - [ ] Receipt PDF: the typst module (`tools/receipt-pdf`) runs on wazero in Go. PHP options: a
        WebAssembly runtime extension, the typst CLI binary (only where `exec` is allowed, never
        with user input in the command), or a PHP PDF library with a ported template checked
        against the same text-extraction golden tests
  - [ ] Jobs without a daemon: a `bin/studio-schedule jobs:run` called by cron every minute,
        under a DB advisory lock so runs never overlap; how `Deps.Wake` (mail now, announcement
        due now) maps to "run this job at the end of the request"
  - [ ] Process-local state: the rate limits, the `/readyz` cache and the metrics keep state in
        the Go process. PHP keeps them in the database (or APCu where present). Are Prometheus
        metrics dropped for PHP?
  - [ ] Migrations: the PHP command reads the goose SQL files from
        `internal/store/postgres/migrations` and writes the same `goose_db_version` table, so
        either backend can migrate and refuse a mismatched schema. A copy or a release artifact
        keeps them in step
  - [ ] Repository and release: `php/` in this repository (composer, PHPUnit, PHPStan at max
        level, PHP-CS-Fixer), and a release zip with `vendor/` and the built web UI
- [ ] Conformance suite (Go, black-box, against a `BASE_URL`): it ports the scenarios of
      `internal/app/*_test.go` and `servicetest` to HTTP and checks every response against
      `api/openapi.json`. It needs a test control API that is off unless the environment enables
      it, in both backends: set the clock, reset the database, load the demo seed, read the
      outbox. It runs first against `serve`, then in CI against both (`just conformance`)
- [ ] PHP skeleton: config from env (the same names as docs/operations.md), PSR-3 logging with
      request IDs, problem+json in the shape of `httpapi.ErrorModel`, the middleware chain of
      `internal/app/observe.go` (security headers, request ID, access log with redacted `token`
      parameters, recovery, cross-origin protection), the access levels of ADR 0005, the session
      cookie (`__Host-studio_session` / `studio_session`), `/healthz` and `/readyz`, and the SPA
      fallback serving the built `web/dist`
- [ ] Shared building blocks: PSR-20 clock and school time zone (never the server zone), UUIDv7
      IDs, `Money`/`Date`/`TimeOfDay` value objects, argon2id through `password_verify` on the
      existing PHC hashes (a test with hashes made by Go), the common-password list, the mail
      templates ported to Twig and checked against the Go golden files in
      `internal/mail/testdata`, the iCal writer against the golden feeds in
      `internal/service/testdata/feeds`
- [ ] API areas, each done when its conformance cases pass on PHP:
  - [ ] Public settings, schedule and visibility, release window (ADR 0003)
  - [ ] Accounts, sessions, credentials, rate limits, impersonation (ADR 0004–0006)
  - [ ] Family, persons, invitations, catalogue and timetable administration (ADR 0007, 0008)
  - [ ] Booking, participants, waiting list with invites (ADR 0009, 0010), with the same lock
        order (`SELECT … FOR UPDATE`, term → class → lesson → passes)
  - [ ] Calendar feeds (ADR 0011)
  - [ ] Passes, redemption ledger, receipts with the gapless counter and PDF (ADR 0012)
  - [ ] Settings, branding, themes, `init` (ADR 0013, 0018)
  - [ ] Announcements, mail log, statistics (ADR 0014)
  - [ ] Import, duplicates and merge (ADR 0015)
  - [ ] Privacy, export, anonymisation, retention jobs (ADR 0016)
  - [ ] Web push with VAPID (ADR 0017)
  - [ ] Events and workshops (Phase 10)
- [ ] CLI parity: the operator commands (`migrate`, `seed`, `init`, `import`, `jobs:run`,
      `vapid-keys`, `healthcheck`) as PHP console commands. The admin commands of Phase 11 and the
      MCP server of Phase 12 work against PHP through their remote mode, if their ADRs choose it.
      Otherwise they are ported too
- [ ] Tests: PHPUnit for services and stores (PostgreSQL via Docker, like `just test-pg`),
      concurrency cases for capacity, redemption and receipt numbers against the real database,
      the Playwright suite `web/e2e-server` against the PHP backend, `just ci` and CI running all
      of it; PHP coverage (PHPUnit with PCOV) reported after every run and gated at 90 % like Go
      (Phase 9)
- [ ] Docs: deployment on shared hosting and in a container (docs/operations.md), choosing a
      backend, ADR 0001 amended (the WASM demo stays Go), CLAUDE.md rules for changes in both
      backends

## Phase 14 — Optional modules

Each module lands in both backends (Phase 13).

- [ ] Media library (video links per course type, members only)
- [ ] Embeddable public schedule widget (iframe/script) for the school website
- [x] PWA (installable, offline schedule cache, web push for waitlist invites) (2026-10-26) — ADR
      0017; own service worker (vite-plugin-pwa injectManifest), server manifest from the
      settings; offline: allow-listed query cache in IndexedDB, read-only, wiped on sign-out and
      401; web push for waiting-list invites through the stdlib `internal/webpush` (VAPID env,
      `vapid-keys`); the demo shows a real local notification
- [ ] MFA (TOTP or passkeys; its own ADR)
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

| Risk                                                                      | Mitigation                                                                                                          |
| ------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------- |
| Demo (memory store, worker cookies) drifts from production behaviour      | Shared contract suite; Playwright smoke against server + Postgres (Phase 8)                                         |
| Every query must be implemented twice (memory + Postgres)                 | Narrow per-aggregate repos; aggregation and statistics in Go services                                               |
| WASM binary size / first load on mobile                                   | Size reported in CI; loading screen; dependency guard; lazy start                                                   |
| release-please with `GITHUB_TOKEN` doesn't trigger CI on release PRs/tags | Add a PAT/GitHub App secret `RELEASE_PLEASE_TOKEN` when needed (workflow already prefers it)                        |
| Personal data of minors                                                   | GDPR features in Phase 8; fictional seed data only; no real data in this repo                                       |
| Go and PHP backends drift apart (as in the previous system)               | One contract from Go; black-box conformance suite against both in CI; shared migrations and golden files (Phase 13) |
