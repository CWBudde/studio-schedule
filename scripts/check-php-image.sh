#!/usr/bin/env bash
# Exercise the release image with fresh databases and no host ports.
set -euo pipefail

image=${1:?usage: check-php-image.sh IMAGE}
if [[ $(docker image inspect --format '{{.Config.User}}' "$image") != www-data:www-data ]]; then
	echo 'The PHP image must run as www-data.' >&2
	exit 1
fi
docker run --rm --network none --entrypoint php "$image" -r '
	if (ini_get("display_errors") || !ini_get("log_errors")) {
		fwrite(STDERR, "The PHP image must log errors without displaying them.\n");
		exit(1);
	}
'

network="studio-php-image-$$"
database="$network-db"
app="$network-app"
invalid_nginx=$(mktemp "${TMPDIR:-/tmp}/studio-php-invalid-nginx.XXXXXXXX")
cleanup() {
	docker rm -f "$app" "$database" >/dev/null 2>&1 || true
	docker network rm "$network" >/dev/null 2>&1 || true
	rm -f -- "$invalid_nginx"
}
trap cleanup EXIT
docker network create --internal "$network" >/dev/null

printf '%s\n' 'invalid_studio_fixture_directive;' >"$invalid_nginx"
chmod 0644 "$invalid_nginx"
docker run -d --name "$app" --network none --read-only --tmpfs /tmp:rw,nosuid,nodev,mode=1777 \
	-v "$invalid_nginx:/etc/studio-schedule/nginx.conf:ro" "$image" >/dev/null
if [[ $(timeout 15s docker wait "$app") != 1 ]]; then
	docker logs "$app" >&2
	echo 'The image did not exit promptly after the nginx startup failure.' >&2
	exit 1
fi
docker rm "$app" >/dev/null

probe() {
	docker exec "$app" php -r '
		[$script, $path, $expectedStatus, $expectedBody, $token] = array_pad($argv, 5, "");
		$curl = curl_init("http://127.0.0.1:8080$path");
		curl_setopt_array($curl, [CURLOPT_PROXY => "", CURLOPT_RETURNTRANSFER => true, CURLOPT_TIMEOUT_MS => 1500]);
		if ($token !== "") {
			curl_setopt($curl, CURLOPT_HTTPHEADER, ["Authorization: Bearer $token"]);
		}
		$body = curl_exec($curl);
		$status = curl_getinfo($curl, CURLINFO_RESPONSE_CODE);
		if ($status !== (int) $expectedStatus || !is_string($body) || !str_contains($body, $expectedBody)) {
			fwrite(STDERR, "$path returned HTTP $status, expected $expectedStatus with the required content.\n");
			exit(1);
		}
	' "$@"
}

metrics_token=fictional-metrics-fixture-token
for engine in postgres mysql mariadb; do
	mount=/dance/schedule
	case "$engine" in
	postgres)
		mount=
		docker run -d --name "$database" --network "$network" \
			-e POSTGRES_DB=studio -e POSTGRES_USER=studio -e POSTGRES_PASSWORD=fixture postgres:18-alpine >/dev/null
		dsn="postgresql://studio:fixture@$database:5432/studio?sslmode=disable"
		;;
	mysql)
		docker run -d --name "$database" --network "$network" --tmpfs /var/lib/mysql \
			-e MYSQL_DATABASE=studio -e MYSQL_USER=studio -e MYSQL_PASSWORD=fixture -e MYSQL_ROOT_PASSWORD=fixture \
			mysql:8.0 --skip-log-bin >/dev/null
		dsn="mysql://studio:fixture@$database:3306/studio"
		;;
	mariadb)
		docker run -d --name "$database" --network "$network" --tmpfs /var/lib/mysql \
			-e MARIADB_DATABASE=studio -e MARIADB_USER=studio -e MARIADB_PASSWORD=fixture -e MARIADB_ROOT_PASSWORD=fixture \
			mariadb:10.6 >/dev/null
		dsn="mariadb://studio:fixture@$database:3306/studio"
		;;
	esac
	docker run -d --name "$app" --network "$network" --read-only --tmpfs /tmp:rw,nosuid,nodev,mode=1777 \
		-e "DATABASE_URL=$dsn" -e "BASE_URL=https://school.example.test$mount" -e "METRICS_TOKEN=$metrics_token" "$image" >/dev/null
	ready=false
	for _ in $(seq 1 45); do
		if docker exec "$app" studio-schedule healthcheck --addr 127.0.0.1:8080 >/dev/null 2>&1 &&
			docker exec "$app" php -r '
				require "/opt/studio-schedule/app/vendor/autoload.php";
				StudioSchedule\Store\Database::connect((string) getenv("DATABASE_URL"))->fetchOne("SELECT 1");
			' >/dev/null 2>&1; then
			ready=true
			break
		fi
		if [[ $(docker inspect --format '{{.State.Running}}' "$app") != true ]]; then
			break
		fi
		sleep 1
	done
	if [[ $ready != true ]]; then
		docker logs "$app" >&2
		echo "PHP image or $engine did not start." >&2
		exit 1
	fi
	probe "$mount/readyz" 503 'not ready'
	probe "$mount/metrics" 401 'metrics bearer token required'
	probe "$mount/metrics" 503 'metrics unavailable' "$metrics_token"
	probe "$mount/api/v1/health" 503 'urn:studio-schedule:maintenance'
	probe "$mount/" 200 "<base href=\"$mount/\">"
	probe "$mount/sw.js" 200 'precache'
	probe "$mount/app/config/.env" 404 'no route'
	docker exec "$app" studio-schedule migrate up
	probe "$mount/readyz" 200 '"status":"ok"'
	probe "$mount/healthz" 200 '"status":"ok"'
	probe "$mount/api/v1/health" 200 '"status":"ok"'
	docker exec "$app" studio-schedule jobs:run
	probe "$mount/metrics" 200 'studio_schedule_build_info{version="' "$metrics_token"
	probe "$mount/metrics" 200 'studio_schedule_http_requests_total{method="GET",route="/api/v1/health",status="200"}' "$metrics_token"
	probe "$mount/metrics" 200 'studio_schedule_job_runs_total{job="purge-sessions",result="success"} 1' "$metrics_token"

	if [[ $engine == mariadb ]]; then
		# If one server exits, the container exits so a restart policy can restart the pair.
		docker exec "$app" bash -c 'kill -QUIT "$(cat /tmp/studio-nginx.pid)"'
		[[ $(timeout 15s docker wait "$app") == 1 ]]
	else
		docker stop --time 10 "$app" >/dev/null
		[[ $(docker inspect --format '{{.State.ExitCode}}' "$app") == 0 ]]
	fi
	docker rm -f "$app" "$database" >/dev/null
	echo "PHP image passed on $engine at ${mount:-/}."
done
