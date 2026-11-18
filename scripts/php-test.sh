#!/usr/bin/env bash
# Run PHP 8.3 quality gates against one supported engine, or all three.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

case "${1:-all}" in
postgres | mysql | mariadb) engines=("$1") ;;
all) engines=(postgres mysql mariadb) ;;
*)
	echo "usage: $0 [all|postgres|mysql|mariadb]" >&2
	exit 2
	;;
esac

image=studio-schedule-php-tests
network="studio-schedule-php-$$"
container=
coverage_dir=$(mktemp -d "${TMPDIR:-/tmp}/studio-schedule-php-coverage.XXXXXXXX")
cleanup() {
	if [[ -n $container ]]; then
		docker rm -f "$container" >/dev/null 2>&1 || true
	fi
	docker network rm "$network" >/dev/null 2>&1 || true
	rm -rf -- "$coverage_dir"
}
trap cleanup EXIT

docker build --tag "$image" - <php/test.Dockerfile
cache="${COMPOSER_CACHE_DIR:-${XDG_CACHE_HOME:-$HOME/.cache}/studio-schedule/composer}"
mkdir -p "$cache"
run_php() {
	docker run --rm --user "$(id -u):$(id -g)" \
		-v "$PWD/php:/app" -v "$cache:/composer-cache" \
		-e COMPOSER_HOME=/tmp/composer -e COMPOSER_CACHE_DIR=/composer-cache \
		-w /app "$@"
}

run_php "$image" composer validate --strict
run_php "$image" composer install --no-interaction --prefer-dist
run_php "$image" composer analyse -- --no-progress
run_php "$image" composer check-format

docker network create "$network" >/dev/null
reports=()
for engine in "${engines[@]}"; do
	container="$network-$engine"
	case "$engine" in
	postgres)
		docker run -d --rm --network "$network" --name "$container" \
			-e POSTGRES_DB=studio -e POSTGRES_USER=studio -e POSTGRES_PASSWORD=fixture \
			postgres:18-alpine >/dev/null
		dsn="postgresql://studio:fixture@$container:5432/studio?sslmode=disable"
		;;
	mysql)
		docker run -d --rm --network "$network" --name "$container" \
			-e MYSQL_DATABASE=studio -e MYSQL_USER=studio -e MYSQL_PASSWORD=fixture -e MYSQL_ROOT_PASSWORD=fixture \
			mysql:8.0 >/dev/null
		dsn="mysql://studio:fixture@$container:3306/studio"
		;;
	mariadb)
		docker run -d --rm --network "$network" --name "$container" \
			-e MARIADB_DATABASE=studio -e MARIADB_USER=studio -e MARIADB_PASSWORD=fixture -e MARIADB_ROOT_PASSWORD=fixture \
			mariadb:10.6 >/dev/null
		dsn="mariadb://studio:fixture@$container:3306/studio"
		;;
	esac

	ready=false
	for _ in $(seq 1 60); do
		if run_php --network "$network" -e "STUDIO_PHP_TEST_DATABASE_URL=$dsn" "$image" php -r \
			'require "vendor/autoload.php"; StudioSchedule\Store\Database::connect((string)getenv("STUDIO_PHP_TEST_DATABASE_URL"))->fetchOne("SELECT 1");' \
			>/dev/null 2>&1; then
			ready=true
			break
		fi
		sleep 1
	done
	if [[ $ready != true ]]; then
		docker logs --tail 30 "$container" >&2
		echo "$engine did not become ready" >&2
		exit 1
	fi

	echo "PHP 8.3 on $engine"
	report="/coverage/$engine.php"
	run_php --network "$network" -e "STUDIO_PHP_TEST_DATABASE_URL=$dsn" \
		-v "$coverage_dir:/coverage" "$image" php vendor/bin/phpunit --coverage-php "$report"
	reports+=("$report")
	docker rm -f "$container" >/dev/null
	container=
done

# Each engine executes only its own SQL paths. Gate the union of the
# supported engines and keep single-engine runs for focused verification.
if [[ ${#engines[@]} == 3 ]]; then
	reports=(--gate "${reports[@]}")
fi
run_php -v "$coverage_dir:/coverage:ro" "$image" php tools/check-coverage.php "${reports[@]}"
