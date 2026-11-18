#!/usr/bin/env bash
# Exercise an unpacked PHP release using only files from the archive.
set -euo pipefail

archive=${1:?usage: check-php-release.sh ZIP}
work=$(mktemp -d "${TMPDIR:-/tmp}/studio-php-release-check.XXXXXXXX")
trap 'rm -rf -- "$work"' EXIT

unzip -tq "$archive"
unzip -q "$archive" -d "$work"
for path in public/index.php public/.htaccess public/index.html public/sw.js \
	app/.htaccess app/bin/studio-schedule app/vendor/autoload.php app/common-passwords.txt app/LICENSE \
	app/GPL-2.0-NOTICE.txt app/vendor/mpdf/mpdf/LICENSE.txt; do
	if [[ ! -s $work/$path ]]; then
		echo "Missing release file: $path" >&2
		exit 1
	fi
done
[[ -x $work/app/bin/studio-schedule ]]
[[ $(<"$work/app/.htaccess") == 'Require all denied' ]]
[[ -d $work/app/config && ! -e $work/app/config/.env ]]
[[ ! -e $work/app/tests && ! -e $work/app/vendor/bin/phpunit ]]
[[ ! -e $work/public/app.wasm && ! -e $work/public/receipt.wasm && ! -e $work/public/wasm_exec.js ]]

docker run --rm -i --network none --user "$(id -u):$(id -g)" \
	-v "$work:/bundle:ro" -v "$work/app:/private-app:ro" -v "$work/public:/relocated-public:ro" \
	-w /bundle studio-schedule-php-tests bash -s <<'BUNDLE'
set -euo pipefail
php app/bin/studio-schedule --help > /tmp/studio-help.txt
php -r '
    require "app/vendor/autoload.php";
    $postgres = StudioSchedule\Store\Schema::migrationVersions("app/migrations/postgres");
    $mysql = StudioSchedule\Store\Schema::migrationVersions("app/migrations/mysql");
    if ($postgres === [] || $postgres !== $mysql || !str_contains((string) file_get_contents("/tmp/studio-help.txt"), "jobs:run")) {
        exit(1);
    }
'

for mount in '' /dance/schedule; do
    base_url="http://127.0.0.1:8080$mount"
    document_root=public
    app_directory=
    if [[ -n $mount ]]; then
        document_root=/relocated-public
        app_directory=/private-app
    fi
    BASE_URL="$base_url" STUDIO_SCHEDULE_APP_DIR="$app_directory" LOG_LEVEL=error \
        php -S 127.0.0.1:8080 -t "$document_root" "$document_root/index.php" > /tmp/studio-php.log 2>&1 &
    server=$!
    trap 'kill "$server" 2>/dev/null || true; cat /tmp/studio-php.log >&2' EXIT
    php -r '
        $base = $argv[1];
        $curl = curl_init("$base/healthz");
        curl_setopt($curl, CURLOPT_RETURNTRANSFER, true);
        curl_setopt($curl, CURLOPT_TIMEOUT_MS, 500);
        for ($retry = 0; $retry < 20; ++$retry) {
            if (curl_exec($curl) === "{\"status\":\"ok\"}") {
                $ready = true;
                break;
            }
            usleep(100000);
        }
        curl_setopt($curl, CURLOPT_URL, "$base/");
        $index = curl_exec($curl);
        if (!isset($ready) || !is_string($index) || !str_contains($index, "<base href=\"$argv[2]/\">")
            || !preg_match("~src=\"\\./(assets/[^\"]+\\.js)\"~", $index, $match)) {
            fwrite(STDERR, "The deployed app shell did not load.\n");
            exit(1);
        }
        curl_setopt($curl, CURLOPT_URL, "$base/$match[1]");
        $asset = curl_exec($curl);
        if (!is_string($asset) || $asset === "" || $asset !== file_get_contents("public/$match[1]")) {
            fwrite(STDERR, "The deployed app did not serve its bundled JavaScript.\n");
            exit(1);
        }
    ' "$base_url" "$mount"
    kill "$server"
    wait "$server" || [[ $? -eq 143 ]]
    trap - EXIT
done
BUNDLE
