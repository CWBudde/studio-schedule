# CLAUDE.md

Guidance for AI coding agents working in this repository. Also read [AGENTS.md](AGENTS.md), which
points here.

## Project

`studio-schedule`: a generic dance/ballet school schedule and booking system. A Go backend (chi +
Huma v2, PostgreSQL) and a React frontend (Vite, TS, Tailwind v4, shadcn/ui, TanStack Router/Query,
react-i18next). The same Go app also compiles to WebAssembly with an in-memory store for the GitHub
Pages demo.

- Roadmap and status: [PLAN.md](PLAN.md). Work phase by phase, and tick checkboxes only when the
  item is implemented, tested and visible in the demo.
- Architecture: [docs/architecture.md](docs/architecture.md). Demo specifics:
  [docs/demo-mode.md](docs/demo-mode.md). Domain terms: [docs/glossary.md](docs/glossary.md).

## Commands

```sh
just test        # go test ./...
just lint        # golangci-lint + js/wasm vet
just fmt         # treefmt + prettier
just gen-api     # after changing any HTTP operation: regenerates api/openapi.json + TS types
just web-check   # pnpm -C web check && pnpm -C web test
just demo-serve  # WASM demo locally
just ci          # everything CI runs
```

## Rules

- **No real data.** Never add real school names, people, addresses, prices or credentials. Seed and
  test data are fictional. Secrets come from env/config only.
- **One app, two targets.** Everything goes through `internal/app.New`. Nothing may be demo-only
  except the transport (WASM bridge) and the store/mailer implementations. `cmd/wasm` must not
  depend on pgx, cobra, net/smtp, httptest or os/exec (`just check-wasm-deps`).
- **Layering:** `httpapi` → `service` → `store` interfaces. Services never import a concrete store.
  No SQL outside `internal/store/postgres`.
- **Transactions:** use `Store.InTx` and never nest it. Helpers take a `store.Tx`. Capacity checks
  go through `Lessons().Lock`.
- **Time:** never use `time.Local` or `time.Now()` directly in services; use `clock.Clock` and the
  school timezone from settings.
- **Both stores:** every repo method gets a case in `internal/store/storetest` and must pass for
  `memory` and `postgres`.
- **API changes:** a Huma operation with an explicit `OperationID` (camelCase), camelCase JSON, then
  `just gen-api`, then use the generated types in `web/src/api`. Errors are problem+json without
  internals.
- **Frontend:** shadcn/ui components (`pnpm dlx shadcn@latest add …`), TanStack Query for server
  state, react-hook-form + zod for forms, every user-facing string in `web/src/locales/{en,de}`.
- **Unimplemented means absent.** Don't add stubs that pretend to succeed.
- Commit messages and PR titles: Conventional Commits, in English.
