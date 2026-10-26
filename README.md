# studio-schedule

Open-source schedule and booking software for dance and ballet schools: weekly timetables, class
bookings, waiting lists, family accounts, prepaid passes and receipts.

- **Backend:** Go (chi + [Huma](https://huma.rocks) → OpenAPI 3.1), PostgreSQL
- **Frontend:** React 19 + TypeScript, Vite, Tailwind CSS v4, [shadcn/ui](https://ui.shadcn.com),
  TanStack Router & Query, i18n (English, German)

> **Status:** early development. See [PLAN.md](PLAN.md) for the roadmap and the progress of each
> phase.

## Live demo

**https://cwbudde.github.io/studio-schedule/**

GitHub Pages can only host static files, so the demo works differently from a real installation:

- The **complete Go backend is compiled to WebAssembly** and runs **inside your browser** in a Web
  Worker. The React UI talks to it through the same HTTP API it uses in production.
- Instead of PostgreSQL the demo uses an **in-memory database** seeded with **fictional** data. Every
  change you make stays in your browser tab and is **lost when you reload** (later phases may add an
  optional browser-local snapshot).
- Emails are not sent; they show up in a demo mailbox instead.

Apart from that the demo runs the same code paths as a real deployment. See
[docs/demo-mode.md](docs/demo-mode.md) for details.

## Quick start (development)

Requirements: Go 1.27+, Node 24+, pnpm, [just](https://github.com/casey/just). Optional: treefmt,
golangci-lint.

```sh
just web-install     # install frontend dependencies
just serve           # API server on :8080 (the recipe sets DEMO_MODE=true: in-memory demo data unless DATABASE_URL is set)
                     # demo data: sign in with any demo account, password demo-password
just web-dev         # Vite dev server on :5173, proxies /api to :8080
```

Other useful recipes:

```sh
just test            # Go tests
just web-check       # ESLint, tsc, Prettier, Vitest
just lint            # golangci-lint (+ js/wasm vet)
just gen-api         # regenerate api/openapi.json and web/src/api/schema.d.ts
just demo-serve      # build Go → WASM + demo frontend and preview it locally
just build           # single binary with embedded UI → bin/studio-schedule
```

With PostgreSQL (18+), copy `.env.example` to `.env` and set `DATABASE_URL`, then:

```sh
just migrate up      # apply the embedded migrations (also: down, status)
just seed            # load the fictional demo data into the empty database (--anchor today: around today)
just serve           # refuses to start unless the schema matches the binary
```

For a real school, run `go run ./cmd/studio-schedule init` (or `bin/studio-schedule init`) instead
of `seed`: it asks for the school's name, time zone, language and currency and the first admin, and
prints a one-time link (valid 72 hours) that sets the admin's password; everything else is set in
the app under Administration → Settings. The PostgreSQL tests (`just test-pg`, also part of `just test`) need Docker, or a server in
`STUDIO_TEST_DATABASE_URL` whose role may create databases.

## Run with Docker

The image `ghcr.io/cwbudde/studio-schedule` (`edge` from `main`, `vX.Y.Z` and `latest` from
releases) runs the server for amd64 and arm64. `docker-compose.yml` adds PostgreSQL and Mailpit:

```sh
cp .env.example .env                    # set POSTGRES_PASSWORD
docker compose up -d --build            # needs `git lfs pull` first
docker compose run --rm app migrate up
docker compose run --rm app init        # or `seed` for the fictional demo data
```

The app is at http://localhost:8080 and Mailpit shows the sent mail at http://localhost:8025.

## Running it for a school

- [docs/operations.md](docs/operations.md): configuration reference, deployment behind a reverse
  proxy with TLS, mail, health checks, metrics and logs, backups and restore, upgrades, releases.
- [docs/privacy.md](docs/privacy.md): what personal data the app stores, why and for how long,
  export and erasure, and what the operator still has to do.
- [docs/security.md](docs/security.md): the OWASP ASVS Level 1 checklist and its open items.

## Repository layout

| Path                   | Purpose                                                       |
| ---------------------- | ------------------------------------------------------------- |
| `cmd/studio-schedule/` | CLI and HTTP server                                           |
| `cmd/wasm/`            | WebAssembly entry point for the browser demo                  |
| `internal/`            | Domain, services, stores, HTTP API, WASM bridge, embedded UI  |
| `web/`                 | React frontend                                                |
| `api/openapi.json`     | Generated API contract                                        |
| `docs/`                | Architecture, operations, privacy, security, decision records |

## Contributing

Commits and PR titles follow [Conventional Commits](https://www.conventionalcommits.org). Releases
are created by release-please. Run `just ci` before pushing.

## License

[MIT](LICENSE)
