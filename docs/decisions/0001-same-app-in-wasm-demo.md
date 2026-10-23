# 0001 — Run the same Go application as WASM for the public demo

- Status: accepted
- Date: 2026-10-23

## Context

The project needs a public, always-available demo. The only free hosting available is GitHub Pages,
which serves static files only. A mock backend written in TypeScript would drift from the real
behaviour and double the work.

## Decision

Compile the real Go application (`internal/app.New`) to `GOOS=js GOARCH=wasm` and run it in a Web
Worker. The frontend talks to it through a `fetch`-compatible bridge. Persistence is provided by an
in-memory store implementation behind the same `store.Store` interface that PostgreSQL implements.
A shared contract test suite keeps the two stores behaviourally identical.

Alternatives considered:

- **SQLite in WASM** (modernc does not support js/wasm; ncruces needs wazero, which would mean a
  WASM interpreter running inside WASM): rejected.
- **PGlite bridged to Go via a fake `net.Conn`:** a single SQL store everywhere, but it adds
  ~3 MB, it is immature, and Go would block on JS promises inside the driver. Kept as a fallback
  spike if maintaining the memory store becomes too costly.

## Consequences

- Every repository method is implemented twice (memory, Postgres) and covered by `storetest`.
- Repositories stay narrow. Aggregations and statistics are computed in Go services.
- `cmd/wasm` must not import server-only packages (guarded in CI).
- The demo resets on reload. The README, `docs/demo-mode.md` and a banner in the demo UI say so.
