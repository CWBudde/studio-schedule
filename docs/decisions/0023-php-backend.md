# 0023 — PHP backend

- Status: accepted
- Date: 2026-11-18
- Amends: [0001](0001-same-app-in-wasm-demo.md) (a second server backend; the WASM demo stays
  Go), [0020](0020-command-line.md) and [0022](0022-mcp-server.md) (the operator commands get PHP
  twins; the administration commands and the MCP server stay Go-only)

## Context

Many schools rent web space from a host that runs PHP behind Apache or nginx and nothing else: no
Go binary, no long-running process, no container, no Kubernetes, often no `exec` and on the
cheapest plans no shell. Those hosts are the reason for a PHP backend. Schools that can run a
container already have the Go one.

The previous system had two backends whose role numbers, field names and endpoints drifted apart
(PLAN.md, "Lessons from the previous attempt"). A second backend is only worth having if it serves
exactly the same contract. So the Go app stays the reference implementation: it owns
`api/openapi.json`, the PostgreSQL migrations, the golden files and the WASM demo, and a black-box
conformance suite decides when a PHP feature is done.

The eight open questions of PLAN.md Phase 13, and the ones their answers raised (MySQL ownership,
switching data between databases, installing without a shell, a subdirectory install, the PDF
licence, web push, the coverage floor), were each asked and answered before this record.

## Decision

### Hosting: shared hosting first, a container too

The PHP backend targets shared hosting: PHP-FPM (or mod_php) behind Apache or nginx, cron, no
daemon, no `exec`, possibly no shell. Every other decision below has to work there. It also ships
as a container image (PHP-FPM + nginx), for schools that want PHP in a container, but the container
gets no feature shared hosting lacks.

### Databases: PostgreSQL, MySQL 8.0 and MariaDB 10.6, all from the start

Shared hosts offer MySQL or MariaDB far more often than PostgreSQL. The PHP backend supports all
three: PostgreSQL (the same schema as Go), MySQL 8.0 or later, MariaDB 10.6 or later. Every PHP
store method, and every API area, is done only when its tests and its conformance cases pass on all
three. There is no "PostgreSQL first, MySQL later".

The Go backend stays PostgreSQL-only. The MySQL schema belongs to the PHP backend: one migration
set `php/migrations/mysql`, one file per Go migration with the same version number. A CI test
migrates a PostgreSQL and a MySQL database to the latest version and compares tables, columns
(with their types mapped), nullability, keys, unique constraints and the checks, so a Go migration
without its MySQL twin fails the build. Where MySQL lacks a feature the PostgreSQL schema uses, the
twin says how it replaces it: `timestamptz` → `DATETIME(6)` in UTC (the store converts); partial
unique indexes → a generated column that is `NULL` outside the condition plus a unique index on it
(MariaDB has no functional indexes); the triggers that keep issued receipts immutable → MySQL
triggers with `SIGNAL`; advisory locks → `GET_LOCK`/`RELEASE_LOCK` on one dedicated connection.

The shared MySQL/MariaDB migrations store UUIDs as 16 bytes, clock times as microsecond-precision
`TIME`, and intervals as signed microseconds. Generated binary keys fold ASCII letters only, as
PostgreSQL does under collation `"C"`, and enforce conditional uniqueness. InnoDB index limits
bound indexed byte strings. MySQL needs 8.0.19 or later for common CHECK constraint syntax.
The installation account must be able to create triggers. A host with MySQL binary logging
enabled must permit this for the installation account. The receipt triggers reject `UPDATE`
and `DELETE`. Protect archived receipts from `TRUNCATE` by keeping the `DROP` privilege out of
the account used to serve requests.
A populated upgrade from before migration 8 needs database time-zone data for named school
zones. That legacy backfill fails if the database cannot perform the local date conversion.

### Switching backends and databases: `db:copy`

On PostgreSQL a school switches between Go and PHP without touching its data: both read and write
the same schema and the same `goose_db_version`. A school on MySQL or MariaDB cannot run Go. For
it, the PHP command `db:copy` (also an action of the installer, below) copies a complete database
into an empty one of another kind, MySQL/MariaDB → PostgreSQL and back, at the same schema
version, in one transaction on the target, and then runs the `db check` integrity checks on the
target. A failed check rolls the copy back.

### PHP 8.3 and small PSR libraries

PHP 8.3 or later. No full-stack framework: Slim 4 for PSR-7/15 routing and middleware, Doctrine
DBAL for the three databases (no ORM), symfony/mailer (SMTP only, like Go), symfony/uid (UUIDv7),
Twig for the mail templates and the receipt, a PSR-3 logger, a PSR-20 clock. The code keeps the Go
layering: `Http` → `Service` → `Store` interfaces, SQL only in the store implementations, one store
per dialect where the SQL differs. Quality gates: PHPUnit, PHPStan at max level, PHP-CS-Fixer.

### Receipt PDF: mPDF with a ported template

Shared hosting has no `exec` and no WebAssembly runtime extension, and a pure-PHP WebAssembly
interpreter would be far too slow for typst. So the receipt template `tools/receipt-pdf/receipt.typ`
is ported to a Twig/HTML template rendered by mPDF, with the same fonts (Libertinus, OFL). It is
checked against the text-extraction golden tests of the typst renderer: the same fields, amounts,
numbers and order. The layout may differ in detail; the content may not.

mPDF is GPL-2.0-only. The code under `php/` stays MIT, but the PHP release zip bundles `vendor/`
with mPDF, so the zip states that it is distributed under GPL-2.0 and ships mPDF's licence. The Go
backend and the repository as a whole stay MIT.

### Jobs: cron every minute, wake after the response

`bin/studio-schedule jobs:run`, called by cron every minute, runs the due jobs of `app.Jobs` (ADR
0020's list and schedules), each under the job lock (`pg_advisory_lock` / `GET_LOCK`), so runs never
overlap, and records each run in `job_runs` like Go. `Deps.Wake` (a mail to send now, an
announcement due now) runs the woken job after the response is sent: with `fastcgi_finish_request()`
where PHP-FPM provides it, otherwise the next cron run picks it up. A wake never delays the
response.

### Process-local state in the database

Go keeps the rate limits, the `/readyz` cache and the metrics in its process. PHP has no process
that outlives a request, so the rate-limit buckets and the readiness cache live in the database,
in tables both backends' migrations create (Go never reads them), with APCu used instead when the
extension is present. Metrics are counted in the database and served at `/metrics` on the main
host, only with `Authorization: Bearer <METRICS_TOKEN>`; without `METRICS_TOKEN` there is no
`/metrics`. Shared hosting cannot open a second listener like Go's `METRICS_ADDR`.

### Migrations and the repository

The PHP backend lives in `php/` in this repository (composer, its own `phpunit.xml`, PHPStan and
PHP-CS-Fixer configuration), so a change and its PHP twin land in one pull request. On PostgreSQL it
reads Go's goose files from `internal/store/postgres/migrations` in place (the `-- +goose Up/Down`
and `StatementBegin/End` markers) and writes the same `goose_db_version` table, so either backend
can migrate and both refuse a schema that does not match. CI builds the release zip: `vendor/`
(production only), the built web UI, both migration sets copied in, the GPL notice.

The zip has two folders: `public/` (`index.php`, `.htaccess`, the web UI) becomes the document root
or the subdirectory, `app/` (code, `vendor/`, migrations) sits outside it where the host allows,
and `public/index.php` finds it. Configuration comes from the environment with the names of
docs/operations.md; where a host cannot set environment variables, from `app/config/.env` with the
same names (the environment wins).

### Commands: the operator commands only

`bin/studio-schedule` ports the operator commands: `migrate`, `seed`, `init`, `import`,
`jobs:run`, `vapid-keys`, `healthcheck`, `doctor`, `db check` and `db:copy`. The administration
commands of ADR 0020 and the MCP server of ADR 0022 stay Go-only: they have no remote mode, so they
open the database directly, and a school on PostgreSQL can run the Go binary on a machine that
reaches it. A school on MySQL has the web UI for administration.

### Installing and upgrading without a shell

A web installer at `<base>/setup` exists only while `SETUP_TOKEN` is set, and every request to it
carries the token. It checks the environment (PHP version, extensions, database connection, a
writable directory for logs), applies the migrations to an empty database, creates the school's
settings and the first admin as `init` does, and from then on refuses to create another. Seed data
stays a CLI command.

Uploading a newer release over an older one leaves the schema behind. Until it is migrated the API
answers 503 with a `maintenance` problem and `/readyz` fails, as Go refuses to start. The installer
then lists the pending migrations and applies them after an admin signs in on the setup page (the
token plus an admin's email and password, checked against the existing tables). Migrations never
run by themselves, so a school can back up first. `db:copy` is offered on the same page.

MySQL and MariaDB can commit schema changes before the full upgrade finishes. Before upgrading
either engine, stop writes from the site and scheduled jobs, then take and verify a full
pre-upgrade backup. Keep those writers stopped until the upgrade or restore finishes.
The migration runner records a recovery marker in the database before executing schema changes
and clears it after the entire batch succeeds. If an upgrade stops with that marker in place,
the API stays in maintenance mode, readiness fails, and subsequent migration attempts are
refused. Replace the affected database with the full pre-upgrade backup, including
`goose_db_version`, before retrying. For an interrupted first install, start over with a fresh
empty database. PostgreSQL applies its upgrade in a transaction and rolls back on failure.

### A subdirectory install, in both backends

A school may install the app at the root of a domain or subdomain, or in a subdirectory
(`https://example.org/schedule/`). Both backends support it, so the contract and the web build stay
the same: `BASE_URL` may carry a path, which becomes the prefix of `/api/v1`, `/ical`, `/healthz`,
`/readyz`, the SPA and its assets, the web manifest, the service worker's scope, and the links in
mails and feeds. Under a path the session cookie is `studio_session` with `Path=<prefix>` and
`Secure`; the `__Host-` prefix needs `Path=/`, so it stays for root installs only. This is built in
Go and the web UI first, and the conformance suite runs under a prefix too.

### Web push: minishlink/web-push

Web push (ADR 0017) uses `minishlink/web-push` (MIT), with the VAPID keys of the same `VAPID_*`
variables, checked against the RFC 8291 test vectors Go's `internal/webpush` uses.

### Passwords

The PHP backend verifies the argon2id PHC strings Go writes with `password_verify` where PHP is
built with Argon2, else with `sodium_crypto_pwhash_str_verify`, and writes new hashes with Go's
parameters. A test verifies hashes made by Go, and Go verifies hashes made by PHP.

### The conformance suite and its control API

The conformance suite is Go, black-box against a `BASE_URL`, and checks every response against
`api/openapi.json`. It needs to set the clock, reset the database, load the demo seed and read the
outbox. Both backends get a test control API under `/api/test/…` for that, registered only when
`TEST_CONTROL=true`, and refused at start with an `https://` `BASE_URL`, so it can never be on in
production. It is not part of `api/openapi.json`.

### Coverage

PHP coverage (PHPUnit with PCOV) is reported after every run and gated like Go's: 95 % of all
statements, 80 % per namespace across all three supported database engines.

## Consequences

- A school on PHP-only web space can run the app, on PostgreSQL, MySQL or MariaDB, and install and
  upgrade it without a shell.
- Every feature from now on (Phase 14 included) lands in both backends, and its conformance cases
  pass on Go and on PHP with each of the three databases. CI runs PostgreSQL, MySQL 8.0 and MariaDB
  10.6.
- Every Go migration needs its MySQL twin; the schema-equivalence test enforces it.
- Receipts from PHP look slightly different from Go's, with the same content. The PHP release zip
  is GPL-2.0 because of mPDF.
- Go gains a base path, the test control API and two tables it never reads (rate limits, readiness
  cache) through its migrations.
- The administration commands and the MCP server need the Go binary and PostgreSQL.
- Not built: a remote mode for the CLI, an MCP server in PHP, the WASM demo in PHP, a typst renderer
  in PHP.
