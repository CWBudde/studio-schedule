#!/usr/bin/env bash
# Run the black-box suite against the Go server at the root and a mount path.
# CONFORMANCE_DATABASE_URL can supply a test PostgreSQL service. Otherwise
# start a PostgreSQL container for this run.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

work="$(mktemp -d)"
server_pid=
container=
cleanup() {
	if [[ -n $server_pid ]]; then
		kill "$server_pid" 2>/dev/null || true
		wait "$server_pid" 2>/dev/null || true
	fi
	if [[ -n $container ]]; then
		docker rm -f "$container" >/dev/null 2>&1 || true
	fi
	rm -rf "$work"
}
trap cleanup EXIT

export LOG_LEVEL=warn TEST_CONTROL=true
TEST_CONTROL_TOKEN="$(openssl rand -hex 32)"
export TEST_CONTROL_TOKEN
export DATABASE_URL="${CONFORMANCE_DATABASE_URL:-}"
if [[ -z $DATABASE_URL ]]; then
	container="studio-conformance-pg-$$"
	docker run -d --rm --name "$container" -p 127.0.0.1::5432 \
		-e POSTGRES_USER=studio -e POSTGRES_PASSWORD=conformance -e POSTGRES_DB=studio \
		postgres:18-alpine >/dev/null
	port="$(docker port "$container" 5432/tcp)"
	DATABASE_URL="postgres://studio:conformance@127.0.0.1:${port##*:}/studio?sslmode=disable"
	db_ready=false
	for _ in $(seq 1 60); do
		if docker exec -e PGPASSWORD=conformance "$container" psql \
			-h 127.0.0.1 -U studio -d studio -c 'select 1' >/dev/null 2>&1; then
			db_ready=true
			break
		fi
		sleep 1
	done
	if [[ $db_ready != true ]]; then
		docker logs --tail 30 "$container" >&2
		echo "The temporary PostgreSQL server did not become ready" >&2
		exit 1
	fi
fi

go build -o "$work/studio-schedule" ./cmd/studio-schedule
"$work/studio-schedule" vapid-keys >"$work/vapid"
while IFS='=' read -r name value; do
	case "$name" in
	VAPID_PUBLIC_KEY | VAPID_PRIVATE_KEY) export "$name=$value" ;;
	esac
done <"$work/vapid"
export VAPID_SUBJECT="mailto:conformance@example.test"

export HTTP_ADDR="127.0.0.1:${CONFORMANCE_PORT:-$((20000 + RANDOM % 20000))}"

for base_path in '' /dance/schedule; do
	export BASE_URL="http://${HTTP_ADDR}${base_path}"
	"$work/studio-schedule" serve >"$work/server.log" 2>&1 &
	server_pid=$!

	ready=false
	for _ in $(seq 1 60); do
		if ! kill -0 "$server_pid" 2>/dev/null; then
			break
		fi
		if curl -fsS --max-time 2 "$BASE_URL/readyz" >/dev/null 2>&1 &&
			printf 'Authorization: Bearer %s\n' "$TEST_CONTROL_TOKEN" |
			curl -fsS --max-time 2 --header @- "$BASE_URL/api/test/outbox" >/dev/null 2>&1; then
			ready=true
			break
		fi
		sleep 1
	done
	if [[ $ready != true ]] || ! ./scripts/conformance-test.sh; then
		cat "$work/server.log" >&2
		exit 1
	fi

	kill "$server_pid"
	wait "$server_pid"
	server_pid=
done
