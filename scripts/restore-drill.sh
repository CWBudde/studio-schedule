#!/usr/bin/env bash
# Restore drill (docs/operations.md, "Backups and restore"): migrates and
# seeds a fresh database, backs it up with `pg_dump --format=custom`,
# restores the dump into a second, empty database with `pg_restore
# --exit-on-error`, and checks the copy: every migration applied, the same
# rows and triggers as the original, `serve` ready on it (/readyz) and a
# seeded account able to sign in.
#
# pg_dump and pg_restore run inside the PostgreSQL container, so their
# version matches the server's. Without RESTORE_DRILL_CONTAINER the drill
# starts postgres:18-alpine (port RESTORE_DRILL_PG_PORT) and removes it
# afterwards; CI names its service container, which must have the user
# `studio` with the password `drill`, a database `source` and its port at
# RESTORE_DRILL_PG_PORT on localhost. BIN is the studio-schedule binary
# (default: built from this checkout into a temporary directory).
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PG_IMAGE=postgres:18-alpine
PG_USER=studio
PG_PASSWORD=drill
pg_port="${RESTORE_DRILL_PG_PORT:-55433}"
app_port="${RESTORE_DRILL_PORT:-8091}"
container="${RESTORE_DRILL_CONTAINER:-}"
own_container=""
server_pid=""

work="$(mktemp -d)"
cleanup() {
	if [[ -n $server_pid ]]; then
		kill "$server_pid" 2>/dev/null || true
		wait "$server_pid" 2>/dev/null || true
	fi
	if [[ -n $own_container ]]; then
		docker rm -f "$own_container" >/dev/null 2>&1 || true
	fi
	rm -rf "$work"
}
trap cleanup EXIT

if [[ -z ${BIN:-} ]]; then
	echo "building studio-schedule"
	(cd "$ROOT_DIR" && go build -o "$work/studio-schedule" ./cmd/studio-schedule)
	BIN="$work/studio-schedule"
fi

if [[ -z $container ]]; then
	container=studio-schedule-restore-drill
	own_container="$container"
	docker rm -f "$container" >/dev/null 2>&1 || true
	docker run -d --rm --name "$container" \
		-e POSTGRES_USER="$PG_USER" -e POSTGRES_PASSWORD="$PG_PASSWORD" -e POSTGRES_DB=source \
		-p "127.0.0.1:$pg_port:5432" "$PG_IMAGE" >/dev/null
fi

psql() { docker exec -i -e PGOPTIONS="-c client_min_messages=warning" "$container" psql -U "$PG_USER" -v ON_ERROR_STOP=1 -qtA "$@"; }
url() { echo "postgres://$PG_USER:$PG_PASSWORD@localhost:$pg_port/$1?sslmode=disable"; }

echo "== waiting for PostgreSQL"
for _ in $(seq 60); do
	psql -d source -c 'select 1' >/dev/null 2>&1 && break
	sleep 1
done

echo "== migrate and seed the original"
DATABASE_URL="$(url source)" "$BIN" migrate up >/dev/null
DATABASE_URL="$(url source)" "$BIN" seed >/dev/null

echo "== back up with pg_dump --format=custom"
docker exec "$container" pg_dump -U "$PG_USER" -d source --format=custom >"$work/studio.dump"
echo "dump: $(wc -c <"$work/studio.dump" | tr -d ' ') bytes"

echo "== restore into a new database with pg_restore --exit-on-error"
psql -d source -c 'drop database if exists restored' -c 'create database restored'
docker exec -i "$container" pg_restore -U "$PG_USER" -d restored --exit-on-error --single-transaction \
	<"$work/studio.dump"

echo "== compare the copy with the original"
# Row counts of every table, and the triggers (receipts are immutable by
# trigger, ADR 0012).
counts="select string_agg(format('%s=%s', c.relname,
	(xpath('/row/n/text()', query_to_xml(format('select count(*) as n from %I.%I', n.nspname, c.relname), false, true, '')))[1]::text), ' ' order by c.relname)
	from pg_class c join pg_namespace n on n.oid = c.relnamespace
	where c.relkind = 'r' and n.nspname = 'public'"
triggers="select string_agg(tgname, ' ' order by tgname) from pg_trigger where not tgisinternal"
for q in "$counts" "$triggers"; do
	original="$(psql -d source -c "$q")"
	copy="$(psql -d restored -c "$q")"
	if [[ $original != "$copy" ]]; then
		printf 'the copy differs from the original:\n  original: %s\n  copy:     %s\n' "$original" "$copy" >&2
		exit 1
	fi
done
echo "same rows and triggers"

export DATABASE_URL
DATABASE_URL="$(url restored)"

echo "== migrate status on the copy"
status="$("$BIN" migrate status)"
echo "$status" | tail -n 3
if grep -q pending <<<"$status" || ! grep -q applied <<<"$status"; then
	echo "the copy's schema is not fully migrated" >&2
	exit 1
fi

echo "== serve the copy"
HTTP_ADDR="127.0.0.1:$app_port" BASE_URL="http://localhost:$app_port" LOG_LEVEL=warn "$BIN" serve &
server_pid=$!
base="http://127.0.0.1:$app_port"
ready=""
for _ in $(seq 60); do
	if [[ "$(curl -s -o /dev/null -w '%{http_code}' "$base/readyz")" == 200 ]]; then
		ready=1
		break
	fi
	sleep 1
done
if [[ -z $ready ]]; then
	echo "/readyz did not answer 200" >&2
	exit 1
fi
echo "/readyz: 200"

# A seeded account signs in, and its session reads its own data.
curl -fsS -c "$work/cookies" -H 'Content-Type: application/json' \
	-d '{"email":"olga.ledgerby@example.net","password":"demo-password"}' \
	"$base/api/v1/auth/login" >/dev/null
me="$(curl -fsS -b "$work/cookies" "$base/api/v1/auth/me")"
if ! grep -q '"olga.ledgerby@example.net"' <<<"$me"; then
	echo "signed in, but /api/v1/auth/me answered: $me" >&2
	exit 1
fi
echo "signed in as a seeded account"

echo "restore drill passed"
