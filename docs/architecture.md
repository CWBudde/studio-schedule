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
| Entry     | `cmd/studio-schedule` | `cmd/wasm` (`//go:build js && wasm`) |

## Layers (Go)

- `internal/domain`: entities, typed IDs, enums (roles, statuses), domain errors. No dependencies.
- `internal/service`: use cases. It depends only on interfaces: `store.Store`, `clock.Clock`, the ID
  generator `ids.Source`, password hasher. It returns domain errors.
- `internal/store`: repository interfaces, `InTx`, and the `storetest` contract suite that every
  implementation must pass.
- `internal/httpapi`: Huma operations. It maps domain errors to problem+json and does no business
  logic.
- `internal/app`: wiring and cross-cutting middleware (recovery, cross-origin protection, later
  sessions and request IDs).

## Transactions and concurrency

`Store.InTx(ctx, mode, fn)` is the only way to touch data.

- **Postgres:** READ COMMITTED transactions. Aggregates that need serialisation (a lesson's
  capacity, a person's passes, the receipt counter of a year) are locked explicitly through repo
  methods (`Lessons().Lock`), which use `SELECT … FOR UPDATE`. Serialization and deadlock failures
  are retried. Constraints are backstops: a partial unique index for active bookings,
  `visits_remaining >= 0`, and a unique redemption per booking.
- **Memory:** one mutex for all transactions (so they are serialised). Tables are copied on their
  first write in a transaction; commit swaps the copies in, rollback drops them. The memory store
  checks the same keys, references and delete restrictions as the PostgreSQL schema.
- `InTx` is never nested; a nested call fails with `store.ErrNestedTx`. Helpers receive a
  `store.Tx`. After a failed write the callback must return an error, because PostgreSQL aborts the
  transaction.
- Both stores return instants in UTC with microsecond precision and sort text byte-wise (collation
  `C`), so `storetest` can compare results with `==`.

## Time

- The school timezone is a setting (IANA name). Class templates store local wall time (weekday,
  start, end). Lessons store instants (`timestamptz`), computed in Go so that DST is handled
  correctly.
- `time.Local` is forbidden (golangci-lint forbidigo). The WASM build embeds `time/tzdata`.
- Services take the current time from `clock.Clock`, so tests use a fixed clock.

## API contract

- Huma generates OpenAPI 3.1 from Go types. `just gen-api` writes `api/openapi.json` and generates
  `web/src/api/schema.d.ts` with openapi-typescript. CI fails when either is stale.
- Conventions: base path `/api/v1`, camelCase JSON, explicit camelCase `operationId`s, RFC 9457
  problem+json errors without internal details.
- Cross-site request forgery: Go's `http.CrossOriginProtection` (Sec-Fetch-Site / Origin based) on
  unsafe methods. Sessions use HttpOnly SameSite=Lax cookies (Phase 3).

## Frontend

- React 19, Vite, TypeScript strict, Tailwind v4, shadcn/ui components in `web/src/components/ui`.
- TanStack Router for routing (browser history in production, hash history in the demo), TanStack
  Query for server state, react-hook-form + zod for forms, react-i18next (`en` default, `de`).
- `web/src/api/client.ts` is the only place that knows the transport (`VITE_TRANSPORT=http|wasm`).

## Decision records

Architectural decisions are recorded in [docs/decisions](decisions/).
