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
postgres_container=
postgres_dsn=
coverage_dir=$(mktemp -d "${TMPDIR:-/tmp}/studio-schedule-php-coverage.XXXXXXXX")
cleanup() {
	if [[ -n $container ]]; then
		docker rm -f "$container" >/dev/null 2>&1 || true
	fi
	if [[ -n $postgres_container ]]; then
		docker rm -f "$postgres_container" >/dev/null 2>&1 || true
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
		-v "$PWD/internal/store/postgres/migrations:/postgres-migrations:ro" \
		-v "$PWD/internal/auth/password/common-passwords.txt:/common-passwords.txt:ro" \
		-e STUDIO_PHP_TEST_COMMON_PASSWORDS=/common-passwords.txt \
		-v "$PWD/internal/mail/testdata:/mail-goldens:ro" \
		-v "$PWD/internal/service/testdata/feeds:/calendar-feeds:ro" \
		-v "$PWD/internal/pdf/typstwasm/testdata:/receipt-goldens:ro" \
		-e STUDIO_PHP_TEST_MAIL_GOLDENS=/mail-goldens \
		-e STUDIO_PHP_TEST_CALENDAR_FEEDS=/calendar-feeds \
		-e STUDIO_PHP_TEST_RECEIPT_GOLDENS=/receipt-goldens \
		-e STUDIO_PHP_TEST_POSTGRES_MIGRATIONS=/postgres-migrations \
		-e COMPOSER_HOME=/tmp/composer -e COMPOSER_CACHE_DIR=/composer-cache \
		-w /app "$@"
}

run_php "$image" composer validate --strict
run_php "$image" composer install --no-interaction --prefer-dist
run_php "$image" composer analyse -- --no-progress --memory-limit=512M
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
		installer_dsn="postgresql://studio:fixture@$container:5432/installer?sslmode=disable"
		;;
	mysql)
		docker run -d --rm --tmpfs /var/lib/mysql --network "$network" --name "$container" \
			-e MYSQL_DATABASE=studio -e MYSQL_USER=studio -e MYSQL_PASSWORD=fixture -e MYSQL_ROOT_PASSWORD=fixture \
			mysql:8.0 --skip-log-bin >/dev/null
		dsn="mysql://studio:fixture@$container:3306/studio"
		installer_dsn="mysql://studio:fixture@$container:3306/installer"
		;;
	mariadb)
		docker run -d --rm --tmpfs /var/lib/mysql --network "$network" --name "$container" \
			-e MARIADB_DATABASE=studio -e MARIADB_USER=studio -e MARIADB_PASSWORD=fixture -e MARIADB_ROOT_PASSWORD=fixture \
			mariadb:10.6 >/dev/null
		dsn="mariadb://studio:fixture@$container:3306/studio"
		installer_dsn="mariadb://studio:fixture@$container:3306/installer"
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

	# The installation tests deliberately build a schema and create accounts.
	# Give them a separate database from the schema and store tests.
	case "$engine" in
	postgres)
		docker exec "$container" psql -U studio -d postgres -v ON_ERROR_STOP=1 \
			-c 'CREATE DATABASE installer' >/dev/null
		;;
	mysql | mariadb)
		docker exec -e MYSQL_PWD=fixture "$container" mysql -u root -e \
			"CREATE DATABASE installer; GRANT ALL PRIVILEGES ON installer.* TO 'studio'@'%';"
		;;
	esac

	echo "PHP 8.3 on $engine"
	report="/coverage/$engine.php"
	run_php --network "$network" -e "STUDIO_PHP_TEST_DATABASE_URL=$dsn" \
		-e "STUDIO_PHP_TEST_INSTALLER_URL=$installer_dsn" \
		-v "$coverage_dir:/coverage" "$image" php vendor/bin/phpunit --exclude-group schema-equivalence --exclude-group database-copy --coverage-php "$report"
	reports+=("$report")
	if [[ $engine == postgres ]]; then
		postgres_container=$container
		postgres_dsn=$dsn
		container=
		continue
	fi
	if [[ -n $postgres_dsn ]]; then
		echo "Compare postgres and $engine migration schemas"
		run_php --network "$network" -e "STUDIO_PHP_TEST_POSTGRES_URL=$postgres_dsn" \
			-e "STUDIO_PHP_TEST_DATABASE_URL=$dsn" "$image" php vendor/bin/phpunit --group schema-equivalence
		echo "Copy database between postgres and $engine"
		copy_report="/coverage/copy-$engine.php"
		run_php --network "$network" -e "STUDIO_PHP_TEST_POSTGRES_URL=$postgres_dsn" \
			-e "STUDIO_PHP_TEST_DATABASE_URL=$dsn" -v "$coverage_dir:/coverage" \
			"$image" php vendor/bin/phpunit --group database-copy --coverage-php "$copy_report"
		reports+=("$copy_report")
	fi
	docker rm -f "$container" >/dev/null
	container=
done

# Each engine executes only its own SQL paths. Gate the union of the
# supported engines and keep single-engine runs for focused verification.
if [[ ${#engines[@]} == 3 ]]; then
	sodium_report=/coverage/password-sodium.php
	run_php -v "$coverage_dir:/coverage" "$image" \
		php -d disable_functions=password_hash,password_verify vendor/bin/phpunit \
		--filter PasswordHashTest --coverage-php "$sodium_report"
	reports=(--gate "${reports[@]}" "$sodium_report")
fi
run_php -v "$coverage_dir:/coverage:ro" "$image" php tools/check-coverage.php "${reports[@]}"
