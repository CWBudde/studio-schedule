set shell := ["bash", "-euo", "pipefail", "-c"]
set dotenv-load # recipes see the variables of a local .env (see .env.example)

version := `git describe --tags --always --dirty 2>/dev/null || echo dev`
ldflags := "-s -w -X main.version=" + version

# List available recipes
default:
    @just --list

# Install pinned formatting/lint tools (CI and fresh machines)
setup-deps:
    go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0
    go install mvdan.cc/sh/v3/cmd/shfmt@v3.12.0
    go install github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1
    npm install -g prettier@3.7.4

# Install web dependencies
web-install:
    pnpm -C web install --frozen-lockfile

# ---------------------------------------------------------------- formatting

# Format all files (Go, shell, docs via treefmt; web via prettier)
fmt:
    treefmt --allow-missing-formatter
    pnpm -C web format

# Fail if any file is not formatted
check-formatted:
    ./scripts/error-on-diff.sh treefmt --allow-missing-formatter --no-cache

# ---------------------------------------------------------------- lint

# Lint Go code (host and js/wasm targets)
lint:
    golangci-lint run ./...
    GOOS=js GOARCH=wasm go vet ./cmd/wasm/... ./internal/wasmbridge/...

# Apply automatic lint fixes, then format
fix:
    golangci-lint run --fix ./...
    just fmt

# Fail if go.mod/go.sum are not tidy
check-tidy:
    ./scripts/error-on-diff.sh go mod tidy

# Fail if generated files (OpenAPI spec, TS types) are stale
check-generated:
    ./scripts/error-on-diff.sh sh -c 'ln -s "{{ justfile_directory() }}/web/node_modules" web/node_modules && just gen-api'

# Fail if the sqlc-generated query code is stale
check-sqlc:
    ./scripts/error-on-diff.sh just sqlc

# Fail if the WASM build pulls in server-only dependencies
check-wasm-deps:
    ./scripts/check-wasm-deps.sh

# Fail on known vulnerabilities: reachable ones in Go code and the standard
# library, high or critical ones in the web app's production dependencies.
# Needs network (vulnerability databases), so `just ci` leaves it out; CI runs it.
vuln:
    go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...
    pnpm -C web audit --prod --audit-level high

# Fail unless receipt.wasm is reproducible from tools/receipt-pdf (needs Docker)
typst-wasm-check:
    ./scripts/build-typst-wasm.sh --check

# ---------------------------------------------------------------- codegen

# Regenerate the PostgreSQL query code (internal/store/postgres/internal/sqlc) after changing queries or migrations
sqlc:
    cd internal/store/postgres && sqlc generate

# Rebuild the receipt renderer (tools/receipt-pdf, typst) to internal/pdf/typstwasm/receipt.wasm, reproducibly in Docker
typst-wasm:
    ./scripts/build-typst-wasm.sh

# Regenerate api/openapi.json from the Go code and the TypeScript types from it
gen-api:
    go run ./cmd/studio-schedule openapi > api/openapi.json
    pnpm -C web gen:api

# ---------------------------------------------------------------- test

# Run Go tests
test:
    go test ./...

# Run Go tests with the race detector
test-race:
    go test -race ./...

# Run the PostgreSQL store tests (Docker container, or STUDIO_TEST_DATABASE_URL)
test-pg *args:
    go test -race -count=1 ./internal/store/postgres {{args}}

# Run web checks (eslint, tsc, prettier) and unit tests
web-check:
    pnpm -C web check
    pnpm -C web test

# Install the browser for the e2e tests (once per machine)
e2e-install:
    pnpm -C web e2e:install

# Build the demo and run the Playwright e2e tests against it (args go to playwright test).
# On macOS, caffeinate keeps the Mac from idle-sleeping mid-run: a sleep freezes
# the browsers, and every test running at that moment then fails on its 30 s
# timeout after the wake-up, minutes later.
e2e *args: demo-build
    {{ if os() == "macos" { "caffeinate -i " } else { "" } }}pnpm -C web e2e {{args}}

# Build the server and run the smoke tests (web/e2e-server) against `serve` on PostgreSQL, with
# Mailpit catching the mails; both run in Docker and are removed afterwards (args go to playwright test)
e2e-server *args: build
    {{ if os() == "macos" { "caffeinate -i " } else { "" } }}./scripts/e2e-server.sh {{args}}

# Back up a seeded database with pg_dump, restore it into a new one and check that the copy serves
# (PostgreSQL in Docker; see docs/operations.md, "Backups and restore")
restore-drill:
    ./scripts/restore-drill.sh

# Everything CI runs (the e2e tests need `just e2e-install` once)
ci: check-tidy check-formatted check-sqlc lint test check-wasm-deps typst-wasm-check web-check e2e

# ---------------------------------------------------------------- build & run

# Build the production binary with the UI embedded to bin/studio-schedule
build: web-build
    ./scripts/build-server.sh "{{ldflags}}"

# Run the API server (PostgreSQL if DATABASE_URL is set, otherwise in-memory with demo data)
serve *args:
    go run ./cmd/studio-schedule serve {{args}}

# Apply, roll back or list database migrations (needs DATABASE_URL), e.g. `just migrate up`
migrate *args:
    go run ./cmd/studio-schedule migrate {{args}}

# Load the fictional demo data into an empty, migrated database (needs DATABASE_URL)
seed *args:
    go run ./cmd/studio-schedule seed {{args}}

# Start the Vite dev server (proxies /api to :8080)
web-dev:
    pnpm -C web dev

# Build the web app for production (HTTP transport)
web-build:
    pnpm -C web build

# Build the Go app to WASM into web/public
wasm:
    ./scripts/build-wasm.sh

# Build the GitHub Pages demo (WASM transport) to web/dist
demo-build base="/studio-schedule/": wasm
    VITE_BASE={{base}} pnpm -C web build:demo

# Serve the demo build locally
demo-serve base="/studio-schedule/": (demo-build base)
    VITE_BASE={{base}} pnpm -C web preview --mode demo
