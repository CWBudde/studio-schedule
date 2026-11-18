#!/usr/bin/env bash
# Run the PHP API against disposable databases using the reference Go demo seed.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."

usage() {
	echo "usage: $0 [all|postgres|mysql|mariadb] [--controls-only | --scenario NAME]" >&2
	exit 2
}

case "${1:-all}" in
all) engines=(postgres mysql mariadb) ;;
postgres | mysql | mariadb) engines=("$1") ;;
*) usage ;;
esac
scenario=
case "${2:-}" in
"") test_name=TestBackendConformance ;;
--controls-only) test_name=TestPHPFixtureControls ;;
--scenario)
	scenario=${3:-}
	[[ $scenario =~ ^[a-z0-9_]+$ && $# == 3 ]] || usage
	test_name=TestBackendConformance
	;;
*) usage ;;
esac
filter="^${test_name}\$"
if [[ -n $scenario ]]; then
	filter+="/^${scenario}\$"
fi

work=$(mktemp -d "${TMPDIR:-/tmp}/studio-php-conformance.XXXXXXXX")
prefix="studio-conformance-php-$$"
image=studio-schedule-php-tests
postgres="$prefix-postgres"
database_container=
php_container=
cleanup() {
	if [[ -n $php_container ]]; then
		docker rm -f "$php_container" >/dev/null 2>&1 || true
	fi
	if [[ -n $database_container ]]; then
		docker rm -f "$database_container" >/dev/null 2>&1 || true
	fi
	docker rm -f "$postgres" >/dev/null 2>&1 || true
	rm -rf -- "$work"
}
trap cleanup EXIT

export TEST_CONTROL=true
TEST_CONTROL_TOKEN=$(openssl rand -hex 32)
export TEST_CONTROL_TOKEN
# The PHP router derives and validates this name before it can reset a database.
database=studio_conformance_$(printf '%s' "$TEST_CONTROL_TOKEN" | sha256sum | cut -c1-16)

CGO_ENABLED=0 go build -o "$work/studio-schedule" ./cmd/studio-schedule
"$work/studio-schedule" vapid-keys >"$work/vapid"
while IFS='=' read -r name value; do
	case "$name" in
	VAPID_PUBLIC_KEY | VAPID_PRIVATE_KEY) export "$name=$value" ;;
	esac
done <"$work/vapid"
docker build --tag "$image" - <php/test.Dockerfile
cache="${COMPOSER_CACHE_DIR:-${XDG_CACHE_HOME:-$HOME/.cache}/studio-schedule/composer}"
mkdir -p "$cache"
docker run --rm --user "$(id -u):$(id -g)" -v "$PWD/php:/app" -v "$cache:/composer-cache" \
	-e COMPOSER_HOME=/tmp/composer -e COMPOSER_CACHE_DIR=/composer-cache \
	-w /app "$image" composer install --no-interaction --prefer-dist

docker run -d --rm --name "$postgres" -p 127.0.0.1::5432 \
	-e POSTGRES_USER=studio -e POSTGRES_PASSWORD=fixture -e "POSTGRES_DB=$database" postgres:18-alpine >/dev/null
postgres_port=$(docker port "$postgres" 5432/tcp)
postgres_url="postgresql://studio:fixture@127.0.0.1:${postgres_port##*:}/$database?sslmode=disable"

wait_database() {
	local container=$1 engine=$2
	shift 2
	for _ in $(seq 1 60); do
		if docker exec "$container" "$@" >/dev/null 2>&1; then
			return
		fi
		sleep 1
	done
	docker logs --tail 30 "$container" >&2
	echo "$engine fixture database did not become ready" >&2
	return 1
}
wait_database "$postgres" postgres pg_isready -h 127.0.0.1 -U studio -d "$database"

export STUDIO_CONFORMANCE_STATE=/fixture/state.json STUDIO_CONFORMANCE_GO=/fixture/studio-schedule
run_php() {
	docker run --rm --network host --user "$(id -u):$(id -g)" \
		-v "$PWD:/repo:ro" -v "$work:/fixture" -w /repo \
		-e TEST_CONTROL -e TEST_CONTROL_TOKEN -e DATABASE_URL -e BASE_URL -e STUDIO_CONFORMANCE_SEED_URL \
		-e STUDIO_CONFORMANCE_STATE -e STUDIO_CONFORMANCE_GO -e LOG_LEVEL=warn -e SETUP_TOKEN= \
		-e VAPID_PUBLIC_KEY -e VAPID_PRIVATE_KEY -e VAPID_SUBJECT=mailto:conformance@example.test \
		"$@"
}
queue_fixture_mail() {
	local id="'00000000-0000-0000-0000-000000000001'"
	if [[ $engine != postgres ]]; then
		id="UNHEX('00000000000000000000000000000001')"
	fi
	local sql="INSERT INTO outbox_mails (id, recipient, subject, body_text, body_html, template, attempts, created_at, next_attempt_at)
		VALUES ($id, 'fixture-outbox@example.test', 'Fixture mail', 'Test message', '<p>Test message</p>', NULL, 0,
		'2027-02-05 12:00:00.123456', '2027-02-05 12:00:00.123456')"
	if [[ $engine == postgres ]]; then
		docker exec "$postgres" psql -X -U studio -d "$database" -v ON_ERROR_STOP=1 -c "$sql" >/dev/null
	else
		docker exec -e MYSQL_PWD=fixture "$database_container" mysql -u studio "$database" -e "$sql"
	fi
}
for engine in "${engines[@]}"; do
	export STUDIO_CONFORMANCE_SEED_URL="$postgres_url"
	case "$engine" in
	postgres) export DATABASE_URL="$postgres_url" ;;
	mysql | mariadb)
		database_container="$prefix-$engine"
		if [[ $engine == mysql ]]; then
			docker run -d --rm --name "$database_container" -p 127.0.0.1::3306 --tmpfs /var/lib/mysql \
				-e MYSQL_USER=studio -e MYSQL_PASSWORD=fixture -e MYSQL_ROOT_PASSWORD=fixture -e "MYSQL_DATABASE=$database" \
				mysql:8.0 --skip-log-bin >/dev/null
		else
			docker run -d --rm --name "$database_container" -p 127.0.0.1::3306 --tmpfs /var/lib/mysql \
				-e MARIADB_USER=studio -e MARIADB_PASSWORD=fixture -e MARIADB_ROOT_PASSWORD=fixture -e "MARIADB_DATABASE=$database" \
				mariadb:10.6 >/dev/null
		fi
		wait_database "$database_container" "$engine" env MYSQL_PWD=fixture mysql -h 127.0.0.1 -u studio "$database" -e 'SELECT 1'
		port=$(docker port "$database_container" 3306/tcp)
		export DATABASE_URL="$engine://studio:fixture@127.0.0.1:${port##*:}/$database"
		;;
	esac

	for base_path in '' /dance/schedule; do
		port=$(python3 -c 'import socket; s = socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1])')
		export BASE_URL="http://127.0.0.1:$port$base_path"
		: >"$work/state.json"
		php_container="$prefix-php"
		run_php "$image" php php/tests/Conformance/router.php --check
		if [[ $engine == "${engines[0]}" && -z $base_path ]]; then
			if BASE_URL="https://127.0.0.1:$port" run_php "$image" php php/tests/Conformance/router.php --check >/dev/null 2>&1 ||
				BASE_URL="http://example.test:$port" run_php "$image" php php/tests/Conformance/router.php --check >/dev/null 2>&1 ||
				TEST_CONTROL=false run_php "$image" php php/tests/Conformance/router.php --check >/dev/null 2>&1 ||
				DATABASE_URL="postgresql://studio:fixture@127.0.0.1:${postgres_port##*:}/postgres?sslmode=disable" \
					run_php "$image" php php/tests/Conformance/router.php --check >/dev/null 2>&1; then
				echo "The PHP fixture accepted a disallowed configuration" >&2
				exit 1
			fi
		fi
		# Use the host network so the development server binds and receives only loopback traffic.
		run_php -d --name "$php_container" "$image" php -S "127.0.0.1:$port" php/tests/Conformance/router.php >/dev/null

		ready=false
		for _ in $(seq 1 30); do
			if curl -fsS --max-time 2 "$BASE_URL/healthz" >/dev/null 2>&1; then
				ready=true
				break
			fi
			sleep 1
		done
		echo "PHP conformance on $engine at ${base_path:-/}"
		if [[ $ready != true ]] ||
			! go test ./internal/conformance -run "$filter" -count=1 -v -args -conformance | tee "$work/go-test.log"; then
			docker logs --tail 40 "$php_container" >&2
			exit 1
		fi
		if [[ -n $scenario ]] && ! grep -Fq -- "PASS: $test_name/$scenario " "$work/go-test.log"; then
			echo "Unknown conformance scenario: $scenario" >&2
			exit 2
		fi
		if [[ $test_name == TestPHPFixtureControls ]]; then
			queue_fixture_mail
			go test ./internal/conformance -run '^TestPHPFixtureOutbox$' -count=1 -v -args -conformance
		fi
		docker rm -f "$php_container" >/dev/null
		php_container=
	done
	if [[ -n $database_container ]]; then
		docker rm -f "$database_container" >/dev/null
		database_container=
	fi
done
