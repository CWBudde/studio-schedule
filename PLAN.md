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
| Lessons       | `lessons` (instants, status, visibility, kind, capacity; class slot date, unique `(class_id, slot_date)`)             |
| Booking       | `bookings` (person or guest; booked / cancelled / attended / no_show; trial, prepaid), `booking_events` (audit)       |
| Waitlist      | `waitlist_entries` (hashed random invite token, expiry)                                                               |
| Passes        | `pass_types`, `passes` (`visits_remaining >= 0`), `pass_redemptions` (unique booking)                                 |
| Receipts      | `receipt_counters` (gapless per year), `receipts`, `receipt_lines` (immutable; corrections are cancellation receipts) |
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
