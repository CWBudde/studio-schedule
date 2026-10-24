# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.
`AGENTS.md` points here for other agents.

## Project

`studio-schedule`: a generic dance/ballet school schedule and booking system. A Go backend (chi +
Huma v2, PostgreSQL) and a React frontend (Vite, TS, Tailwind v4, shadcn/ui, TanStack Router/Query,
react-i18next). The same Go app also compiles to WebAssembly with an in-memory store for the GitHub
Pages demo.

- Roadmap and status: [PLAN.md](PLAN.md). Work phase by phase, and tick checkboxes only when the
  item is implemented, tested and visible in the demo. PLAN.md also lists the bugs of the previous
  system that this rewrite must not repeat ("Lessons from the previous attempt").
- Architecture: [docs/architecture.md](docs/architecture.md). Demo specifics:
  [docs/demo-mode.md](docs/demo-mode.md). Domain terms: [docs/glossary.md](docs/glossary.md).
  Decisions: [docs/decisions](docs/decisions).

## Commands

```sh
just web-install # once: pnpm install for web/
just serve       # API server on :8080 (PostgreSQL if DATABASE_URL is set, else memory + demo data)
just migrate up  # apply migrations to DATABASE_URL (also: down, status); just seed loads demo data
just test-pg     # PostgreSQL store tests only (Docker via testcontainers, or STUDIO_TEST_DATABASE_URL)
just sqlc        # after changing internal/store/postgres/{migrations,queries}: regenerate sqlc code
just web-dev     # Vite on :5173, proxies /api to :8080
just test        # go test ./...
just test-race   # go test -race ./...
just lint        # golangci-lint + js/wasm vet
just fix         # golangci-lint --fix, then fmt
just fmt         # treefmt + prettier
just gen-api     # after changing any HTTP operation: regenerates api/openapi.json + TS types
just web-check   # pnpm -C web check (eslint, tsc, prettier) && pnpm -C web test (vitest)
just demo-serve  # WASM demo locally
just e2e         # builds the demo, runs the Playwright tests in web/e2e (once: just e2e-install)
just ci          # everything CI runs
```

Single tests:

```sh
go test ./internal/service -run TestSettings          # one Go package / test
go test ./internal/store/memory -run TestContract/Settings
pnpm -C web exec vitest run src/features/home          # one frontend file or directory
```

## Code map

- **Wiring:** `internal/app.New(Deps)` builds services, calls `httpapi.Register`, and adds the
  middleware (security headers → recovery → access log → cross-origin protection → router). Unknown non-`/api` paths
  fall through to `Deps.UI` (the embedded SPA in `internal/webui`). `cmd/studio-schedule` (cobra:
  `serve`, `migrate`, `seed`, `openapi`, `version`) and `cmd/wasm` both call it. `serve` uses
  PostgreSQL when `DATABASE_URL` is set (refusing to start unless the schema matches), otherwise the
  memory store with `seed.LoadDemo`.
- **Auth & mail:** `service.AuthService` (register → confirm with password → sessions; ADR 0004;
  password reset/change and email change in `service/credentials.go`, rate limits in
  `httpapi/ratelimit.go`; ADR 0006; the account's mail language in `SetLocale`, ADR 0008),
  the `studio_session` cookie middleware in `httpapi/auth.go` (`principalFrom(ctx)`), argon2id in
  `internal/auth/password`. Mails: render with `internal/mail` and call `enqueueMail` inside the
  causing transaction; the `deliver-mail` job sends them (`internal/mail/smtp`, server only).
  `Deps.Demo` (WASM demo, `serve` without DB) exposes `/api/v1/demo/{mailbox,logins}`.
- **Access:** every operation is registered with `register(api, Access…, op, handler)` in
  `httpapi/access.go` (`public`, `signedIn`, `staff`, `office`, `admin`; ADR 0005); the access
  middleware answers 401/403 before the handler runs. Admin impersonation lives in
  `service/impersonation.go`. What a caller _sees_ (e.g. schedule visibility) stays in the services.
  The `staff` operations on a lesson's participants (`service/participants.go`,
  `httpapi/participants.go`; ADR 0009, amendment) check in the service that the caller is the office
  or the lesson's instructor/accompanist (`manages`, a 404 otherwise).
- **Family:** `service.FamilyService` (`/api/v1/family`): own person data and dependents; a guardian
  reaches only their dependents (`dependentOf`, a 404 for anyone else; ADR 0008). A managed
  dependent (own account, staff link or dependents of its own) is read-only for guardians and only
  unlinked. The office's person administration and invitations are `service.PersonService`
  (`service/persons.go`, `service/invite.go`, `/api/v1/admin/persons`); guardian links are written
  under `Persons().Lock`.
- **Admin writes:** the office maintains catalogue and timetable (`service/catalog_admin.go`,
  `httpapi/catalog_admin.go`; classes in `service/class_admin.go`, whose edits move the future
  lessons along in `class_sync.go`, terms in `service/term_admin.go`, whose new dates delete or
  refuse the lessons they leave out, class members in `service/class_members.go`, and
  `httpapi/timetable_admin.go`; ADR 0007). Write operations are `POST` collection → 201, `PUT /{id}`,
  `DELETE /{id}` → 204, registered with `withConflict` so their 409 is the `Conflict` problem with a
  `reason` (`domain.ErrInUse` → `in-use`, `domain.ErrDuplicate` → `duplicate`). A domain rule built
  with `on(field, …)` fails as a `*domain.FieldError`, which `toHTTPError` reports as a 422 on
  `body.<field>`. Office-only reads live under `/api/v1/admin/…` (`listAllStaff`, `listAllClasses`, `listClassMembers`, and
  `listPersons` from `service.PersonService`, the person search for the office's pickers).
- **Bookings:** `service.BookingService` (`service/booking.go`, `httpapi/booking.go`; ADR 0009):
  book and cancel for the caller's family under `Lessons().Lock` with member rules for everyone,
  late cancellations flagged, "cancel following weeks" under the class lock. The schedule's
  availability (`freePlaces`, `bookedFor`, the cutoff instants) is filled in `service/schedule.go`.
- **Adding an endpoint** touches: a service in `internal/service`, a Huma operation in
  `internal/httpapi` registered with its access level (and pinned in `internal/app/access_test.go`;
  plus a field in `httpapi.Deps` and its construction in `app.New`),
  `just gen-api`, then a query/mutation in `web/src/api/queries.ts` using the generated `paths`.
- **Adding a repository:** extend the interfaces in `internal/store/store.go` (reached via
  `store.Tx`), implement it in `internal/store/memory` and `internal/store/postgres` (migration in
  `migrations/`, query in `queries/`, then `just sqlc`), and add
  contract cases to `storetest.Run`. Each store's `TestContract` runs that suite.
- **WASM demo:** `internal/wasmbridge` (`bridge_js.go` is build-tagged `js && wasm`) exposes
  `StudioScheduleAPI.fetch`. On the web side, `web/src/api/transport.ts` picks HTTP or the Worker
  bridge (`web/src/api/wasm/`) from `VITE_TRANSPORT`; nothing else in the frontend may know which.
- **Frontend layout:** file-based routes in `web/src/routes` (`routeTree.gen.ts` is generated by
  the TanStack Router Vite plugin, don't edit it), page components in `web/src/features/<area>`,
  shadcn components in `web/src/components/ui`. Component tests mock the API via
  `web/src/test/mockApi.ts` and render routed components with `renderInRouter` /
  `renderAtSchedule` (`web/src/test/utils.tsx`). School dates and times go through
  `web/src/lib/schoolTime.ts` with the school time zone, never the browser's; tests run with
  `TZ=America/New_York` to catch leaks. Playwright tests in `web/e2e` drive the demo build
  (desktop and phone Chromium, same far-away browser zone) through roles and accessible names.

## Rules

- **No real data.** Never add real school names, people, addresses, prices or credentials. Seed and
  test data are fictional. Secrets come from env/config only.
- **One app, two targets.** Everything goes through `internal/app.New`. Nothing may be demo-only
  except the transport (WASM bridge) and the store/mailer implementations. `cmd/wasm` must not
  depend on pgx, goose, cobra, net/smtp, httptest or os/exec (`just check-wasm-deps`). Keep request
  handlers cheap: the WASM runtime is single-threaded.
- **Layering:** `httpapi` → `service` → `store` interfaces. Services never import a concrete store.
  No SQL outside `internal/store/postgres`. `httpapi` maps domain errors (`internal/domain`) to
  problem+json and holds no business logic.
- **Transactions:** use `Store.InTx` and never nest it. Helpers take a `store.Tx`. Capacity checks
  go through `Lessons().Lock`.
- **Time:** never use `time.Local` or `time.Now()` directly in services; use `clock.Clock` and the
  school timezone from settings. golangci-lint (forbidigo) enforces this and also bans `fmt.Print*`
  outside `cmd/` (use `slog`).
- **IDs:** entities use the typed IDs in `internal/domain/id.go` (`domain.LessonID`, ...). New IDs
  come from an injected `ids.Source` (`ids.New[domain.LessonID](src)`); `ids.Sequence` gives
  deterministic IDs for tests and seeds. forbidigo bans `uuid.New*` outside `internal/ids`.
- **Domain:** plain structs with `Validate()` (errors wrap `domain.ErrValidation`), string enums with
  `Valid()`, zero value = absent, money as `domain.Money` (int64 minor units), `domain.Date` /
  `domain.TimeOfDay` for school-local dates and times. See
  [ADR 0002](docs/decisions/0002-domain-and-persistence-conventions.md).
- **Both stores:** every repo method gets a case in `internal/store/storetest` and must pass for
  `memory` and `postgres`.
- **API changes:** a Huma operation with an explicit `OperationID` (camelCase), camelCase JSON, then
  `just gen-api`, then use the generated types in `web/src/api`. Errors are problem+json without
  internals. CI fails if `api/openapi.json` or `web/src/api/schema.d.ts` is stale.
- **Frontend:** shadcn/ui components (`pnpm dlx shadcn@latest add …`), TanStack Query for server
  state, react-hook-form + zod for forms, every user-facing string in `web/src/locales/{en,de}`.
- **Unimplemented means absent.** Don't add stubs that pretend to succeed.
- Commit messages and PR titles: Conventional Commits, in English (release-please builds releases
  from them).
