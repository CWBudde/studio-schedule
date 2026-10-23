set shell := ["bash", "-euo", "pipefail", "-c"]
set dotenv-load # recipes see the variables of a local .env (see .env.example)

version := `git describe --tags --always --dirty 2>/dev/null || echo dev`
ldflags := "-s -w -X main.version=" + version

# List available recipes
default:
    @just --list

# Install pinned formatting/lint tools (CI and fresh machines)
setup-deps:
    go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.2
    go install mvdan.cc/sh/v3/cmd/shfmt@v3.12.0
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

# Fail if the WASM build pulls in server-only dependencies
check-wasm-deps:
    ./scripts/check-wasm-deps.sh

# ---------------------------------------------------------------- codegen

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

# Run web checks (eslint, tsc, prettier) and unit tests
web-check:
    pnpm -C web check
    pnpm -C web test

# Everything CI runs
ci: check-tidy check-formatted lint test check-wasm-deps web-check demo-build

# ---------------------------------------------------------------- build & run

# Build the production binary with the UI embedded to bin/studio-schedule
build: web-build
    ./scripts/build-server.sh "{{ldflags}}"

# Run the API server (in-memory store until Phase 1)
serve *args:
    go run ./cmd/studio-schedule serve {{args}}

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
