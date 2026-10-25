#!/usr/bin/env bash
# Runs the server smoke tests (web/e2e-server, web/playwright.server.config.ts)
# against bin/studio-schedule (`just build`) on PostgreSQL, with Mailpit
# catching the mails. Arguments go to `playwright test`.
#
# Without E2E_DATABASE_URL it starts PostgreSQL and Mailpit in Docker, on
# ports of their own (E2E_PG_PORT, E2E_MAILPIT_SMTP_PORT, E2E_MAILPIT_PORT),
# and removes both afterwards. CI passes its service containers instead:
# E2E_DATABASE_URL (an empty database), MAILPIT_URL (Mailpit's web API) and
# MAILPIT_SMTP (host:port). The database is migrated and seeded first.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BIN="$ROOT_DIR/bin/studio-schedule"
PG_IMAGE=postgres:18-alpine
MAILPIT_IMAGE=axllent/mailpit:v1.31
PG_CONTAINER=studio-schedule-e2e-postgres
MAILPIT_CONTAINER=studio-schedule-e2e-mailpit

if [[ ! -x $BIN ]]; then
	echo "$BIN missing, run 'just build' first" >&2
	exit 1
fi

cleanup() {
	docker rm -f "$PG_CONTAINER" "$MAILPIT_CONTAINER" >/dev/null 2>&1 || true
}

if [[ -z ${E2E_DATABASE_URL:-} ]]; then
	pg_port="${E2E_PG_PORT:-55432}"
	smtp_port="${E2E_MAILPIT_SMTP_PORT:-51025}"
	mailpit_port="${E2E_MAILPIT_PORT:-58025}"

	cleanup # left over from an interrupted run
	trap cleanup EXIT
	docker run -d --rm --name "$PG_CONTAINER" \
		-e POSTGRES_USER=studio -e POSTGRES_PASSWORD=smoke -e POSTGRES_DB=studio \
		-p "127.0.0.1:$pg_port:5432" "$PG_IMAGE" >/dev/null
	docker run -d --rm --name "$MAILPIT_CONTAINER" \
		-p "127.0.0.1:$smtp_port:1025" -p "127.0.0.1:$mailpit_port:8025" "$MAILPIT_IMAGE" >/dev/null

	export E2E_DATABASE_URL="postgres://studio:smoke@localhost:$pg_port/studio?sslmode=disable"
	export MAILPIT_URL="http://localhost:$mailpit_port"
	export MAILPIT_SMTP="localhost:$smtp_port"

	echo "waiting for PostgreSQL"
	for _ in $(seq 60); do
		# pg_isready answers before the entrypoint's restart; a query does not.
		if docker exec "$PG_CONTAINER" psql -U studio -d studio -c 'select 1' >/dev/null 2>&1; then
			break
		fi
		sleep 1
	done
fi

: "${MAILPIT_URL:?set MAILPIT_URL with E2E_DATABASE_URL}"
: "${MAILPIT_SMTP:?set MAILPIT_SMTP with E2E_DATABASE_URL}"

echo "waiting for Mailpit"
for _ in $(seq 30); do
	curl -fsS "$MAILPIT_URL/api/v1/info" >/dev/null 2>&1 && break
	sleep 1
done

export DATABASE_URL="$E2E_DATABASE_URL"
"$BIN" migrate up
"$BIN" seed

cd "$ROOT_DIR/web"
pnpm exec playwright test -c playwright.server.config.ts "$@"
