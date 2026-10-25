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
go run ./cmd/studio-schedule init  # new school: settings + first admin (prints a setup link)
just test-pg     # PostgreSQL store tests only (Docker via testcontainers, or STUDIO_TEST_DATABASE_URL)
just typst-wasm  # after changing tools/receipt-pdf: rebuild receipt.wasm reproducibly (Docker; Git LFS)
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
just wasm-smoke  # builds app.wasm, smoke-tests the bridge and the receipt renderer in Node
just e2e         # builds the demo, runs the Playwright tests in web/e2e (once: just e2e-install)
just e2e-server  # builds the server, smoke-tests `serve` on PostgreSQL + Mailpit (web/e2e-server; Docker)
just restore-drill  # pg_dump a seeded database, pg_restore it, check the copy serves (Docker)
just ci          # everything CI runs, except `just vuln` (govulncheck + pnpm audit, needs network); needs Docker
```

Single tests:

```sh
go test ./internal/service -run TestSettings          # one Go package / test
go test ./internal/store/memory -run TestContract/Settings
pnpm -C web exec vitest run src/features/home          # one frontend file or directory
```

## Code map

- **Wiring:** `internal/app.New(Deps)` builds services, calls `httpapi.Register`, and adds the
  middleware (security headers → request ID → access log and metrics → recovery → cross-origin
  protection → router; `internal/app/observe.go`). The router also serves `/healthz` and `/readyz`
  (`health.go`, `Deps.Ready`, cached for 2 s). Unknown non-`/api` paths
  fall through to `Deps.UI` (the embedded SPA in `internal/webui`). `cmd/studio-schedule` (cobra:
  `serve`, `migrate`, `seed`, `init`, `import`, `healthcheck`, `openapi`, `version`) and `cmd/wasm`
  both call it. `serve` uses
  PostgreSQL when `DATABASE_URL` is set (refusing to start unless the schema matches), otherwise the
  memory store with `seed.LoadDemo`.
- **Observability:** request IDs (`internal/requestid`) reach the logs through a `slog` handler and
  problem bodies as `requestId` (`httpapi.ErrorModel` replaces Huma's error type). `Deps.Metrics`
  is a no-op unless `serve` gets `METRICS_ADDR`: then `cmd/studio-schedule/metrics.go` (Prometheus,
  server only) serves `/metrics` on its own listener. Name a secret path parameter `token` or
  `…Token`: logs and error details redact it (`internal/app/redact.go`). See architecture.md
  "Observability".
- **Auth & mail:** `service.AuthService` (register → confirm with password → sessions; ADR 0004;
  password reset/change and email change in `service/credentials.go`, rate limits in
  `httpapi/ratelimit.go`; ADR 0006; the account's mail language in `SetLocale`, ADR 0008),
  the session cookie middleware in `httpapi/auth.go` (`principalFrom(ctx)`, `sessionTokenFrom(ctx)`;
  the cookie is `__Host-studio_session` with an https `BASE_URL` (`Deps.HostCookie`), else
  `studio_session`, so never read it as a Huma `cookie:` input), argon2id in
  `internal/auth/password`. Every new password goes through `checkPassword` (8–256 characters, not
  in the embedded common list `password.IsCommon`; a field error), and a change or reset mails
  `password-changed` (ADR 0004, amendment). Mails: render with `internal/mail` and call `enqueueMail` inside the
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
- **Receipts:** `service.ReceiptService` (`service/receipts.go`, ADR 0012) issues a receipt in the
  transaction of `PassService.Sell` (number from `ReceiptCounters().Next`, PDF rendered through
  `service.ReceiptRenderer` and stored, mail with the PDF attached via `OutboxMail.ReceiptID`),
  cancels one (Storno), lists them and keeps the receipt settings. The template data comes from
  `service/receipt_pdf.go`, the template itself is `tools/receipt-pdf/receipt.typ`.
- **Settings:** `service.SettingsService` (`service/settings.go`, `httpapi/settings.go`; ADR 0013):
  one card per area under `/api/v1/admin/settings/{school,branding,regional,booking-rules}` (office
  reads, admin writes); the time zone and currency are locked by `SettingsRepo.Usage`, a wider
  release window generates lessons at once. The accent's contrast and dark variant are
  `domain/color.go`, mirrored for the form in `web/src/lib/color.ts`; the header, title, accent,
  footer and (without a visitor's own choice, `web/src/i18n.ts`) the UI language follow the public
  settings (`web/src/components/layout/branding.ts`). The `init` command
  (`cmd/studio-schedule/init.go`, `service/setup.go`) sets up a new school and prints the first
  admin's setup link.
- **Admin writes:** the office maintains catalogue and timetable (`service/catalog_admin.go`,
  `httpapi/catalog_admin.go`; classes in `service/class_admin.go`, whose edits move the future
  lessons along in `class_sync.go`, terms in `service/term_admin.go`, whose new dates delete or
  refuse the lessons they leave out, class members in `service/class_members.go`, and
  `httpapi/timetable_admin.go`; ADR 0007). Write operations are `POST` collection → 201, `PUT /{id}`,
  `DELETE /{id}` → 204, registered with `withConflict` so their 409 is the `Conflict` problem with a
  `reason` (`domain.ErrInUse` → `in-use`, `domain.ErrDuplicate` → `duplicate`). A domain rule built
  with `on(field, …)` fails as a `*domain.FieldError`, which `toHTTPError` reports as a 422 on
  `body.<field>`. Office-only reads live under `/api/v1/admin/…` (`listAllStaff`, `listAllClasses`, `listClassMembers`, and
  `searchPersons` from `service.PersonService`, the person search for the office's pickers). A
  search by name, email or other personal data is a `POST …/search` with a JSON body, never a query
  parameter (ASVS 8.3.1; `searchPersons`, `searchPersonsForStaff`, `searchReceipts`, `searchMails`).
- **Bookings:** `service.BookingService` (`service/booking.go`, `httpapi/booking.go`; ADR 0009):
  book and cancel for the caller's family under `Lessons().Lock` with member rules for everyone,
  late cancellations flagged, "cancel following weeks" under the class lock. The schedule's
  availability (`freePlaces`, `bookedFor`, the cutoff instants) is filled in `service/schedule.go`.
- **Calendar feeds:** `service.CalendarFeedService` (`service/calendar_feeds.go`; ADR 0011) manages
  an account's links under `/api/v1/my/calendar-feeds` and renders `GET /ical/{token}.ics`, the
  one API route outside `/api/v1`, with `internal/ical` (RFC 5545 writer, VTIMEZONE from Go's zone data);
  golden files in `internal/service/testdata/feeds` (`go test ./internal/service -update`).
- **Passes:** `service.PassService` (`service/passes.go`; ADR 0012): pass types, sell/assign/void,
  My passes. Every booking or lesson write goes through `service/ledger.go`, which settles
  redemptions (`service/redemption.go`) on transitions only; lock order term → class → lesson →
  the persons' passes.
- **Statistics:** `service.StatisticsService` (`service/statistics.go`, `httpapi/statistics.go`,
  `office`): live aggregates of a range of school dates from a constant number of store reads
  (`CountsByLessons`, `CountLateCanceledByLessons`, `CountActivePersons`,
  `Waitlist().CountByLessons`, `Receipts().TotalsByDay`); see architecture.md "Statistics". The page
  is `web/src/features/admin/StatisticsPage.tsx`; recharts and what only it uses stay in the lazily
  loaded `charts` chunk (`web/vite.chunks.ts`, a build guard fails if the entry loads them).
- **Announcements:** `service.AnnouncementService` (`service/announcements.go`, ADR 0014): the
  office writes them (`/api/v1/admin/announcements`), accounts read and dismiss theirs
  (`/api/v1/announcements`, the banner in `web/src/features/announcements`). The audience
  resolves at read time (`reachOf`) and at mail time (`announcementRecipients`, the mail chain of
  `lesson_mail.go`). Writing one only saves it (and checks the daily limit under
  `Announcements().LockMail`); only `SendDue`, the `announcements` job (`app/jobs_communication.go`,
  woken through `Deps.Wake` when one is due now), mails it, in batches with a `MailCursor`. Lock
  order: `LockMail` → the announcement (`Announcements().Lock`); `Update` never writes the mail
  state (`SetMailState` does).
- **Mail log:** `service.MailLogService` (`service/mail_log.go`, ADR 0014; `/api/v1/admin/mails`,
  admins only): filtered, paged outbox, bodies never for `mail.SecretLink` templates, requeue of a
  failed mail (audited as `mail-requeued`), and the daily `purge-mail` job. `audit_events` targets
  an account, a person or nothing and has typed `domain.AuditDetails`; a new audit action is a row
  in `audit_actions` (migration) plus the domain constant.
- **Import & merge:** `service.ImportService` (`service/imports.go`, rows in `service/importer.go`;
  ADR 0015) reads CSV files through `internal/csvimport`, locks every person the rows may find up
  front (ID order), finds members by `persons.member_no` and guardians by email (with the name) or
  name, and runs a dry run as the same code in a transaction that returns `errDryRun`; applied runs
  go to `import_runs`; `studio-schedule import` uses it too (exit 1 when a row failed). Duplicates
  and the merge are `PersonService` methods (`service/duplicates.go`, `service/merge.go`): bookings
  move through the ledger (of a started lesson both stay, `bookings.merged_from`), the merged person
  becomes a tombstone (`persons.merged_into`, left out of `Persons().List`), audited as
  `person-merged`. Every write that names a person looks it up with `lockLivePersons`
  (`service/live_persons.go`), which refuses a tombstone. Scenarios on both stores live in
  `internal/service/servicetest`.
- **Privacy & accounts:** ADR 0016. The data export (`service/export.go`, office
  `exportPerson`, member `exportMyData` for their family; the web saves the JSON answer as a file),
  anonymizing a person (`service/anonymize.go`, `PersonService.Anonymize`: the person stays as
  "Deleted person", hidden like a tombstone, refused by `lockLivePersons`; receipts untouched; the
  API marks it `anonymized` where it names it, and the web shows names through
  `web/src/lib/personName.ts`, which translates it), the
  admin's account administration and deleting one's own account (`service/accounts_admin.go`,
  `httpapi/privacy.go`; guards `own-account`, `last-admin` counting admins who can sign in;
  `endSessions` audits the impersonations it ends), `AccountRepo.Delete` (its FK rules in
  migration 00023: what keeps an account is a 409 `in-use`), and the daily `purge-personal-data`
  job (`service/retention.go`, `app/jobs_privacy.go`). Scenarios on both stores:
  `servicetest.Privacy`.
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
  except the transport (WASM bridge) and the store, mailer and PDF renderer implementations (the
  typst module runs with wazero on the server, in the browser's WebAssembly in the demo). `cmd/wasm`
  must not depend on pgx, goose, cobra, net/smtp, httptest, os/exec, wazero or prometheus
  (`just check-wasm-deps`). Keep request
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
