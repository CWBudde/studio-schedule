#!/usr/bin/env bash
# Build the shared-hosting archive from the production web build and Composer lock file.
set -euo pipefail

output=$(realpath -m -- "${1:?usage: build-php-release.sh ZIP}")
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
web="$root/web/dist"
if [[ ! -s $web/index.html || ! -s $web/sw.js ]] || ! grep -Fq '<base href="./">' "$web/index.html"; then
	echo 'Build the production web UI first with pnpm -C web build.' >&2
	exit 1
fi
if [[ -n $(find "$web" \( -type l -o -name '.env' -o -name '.env.*' -o -name '*.php' -o -name '.htaccess' \) -print -quit) ]]; then
	echo 'The production web build contains an unexpected file or symbolic link.' >&2
	exit 1
fi

work=$(mktemp -d "${TMPDIR:-/tmp}/studio-php-release.XXXXXXXX")
trap 'rm -rf -- "$work"' EXIT
bundle="$work/bundle"
mkdir -p "$bundle/app/config" "$bundle/app/migrations" "$bundle/public"
cp -R "$web/." "$bundle/public/"
# A worktree may have previously built the browser-only demo.
rm -f "$bundle/public/app.wasm" "$bundle/public/receipt.wasm" "$bundle/public/wasm_exec.js"
cp "$root/php/public/index.php" "$root/php/public/.htaccess" "$bundle/public/"
printf '%s\n' 'Require all denied' >"$bundle/app/.htaccess"
cp -R "$root/php/assets" "$root/php/bin" "$root/php/src" "$root/php/templates" "$bundle/app/"
cp -R "$root/php/migrations/mysql" "$bundle/app/migrations/mysql"
cp -R "$root/internal/store/postgres/migrations" "$bundle/app/migrations/postgres"
cp "$root/php/composer.json" "$root/php/composer.lock" "$root/LICENSE" \
	"$root/internal/auth/password/common-passwords.txt" "$bundle/app/"

if ! git -C "$root" describe --tags --always >"$bundle/app/version.txt" 2>/dev/null; then
	printf '%s\n' dev >"$bundle/app/version.txt"
fi

cat >"$bundle/app/GPL-2.0-NOTICE.txt" <<'NOTICE'
Studio Schedule PHP release bundle

This bundle includes mPDF and is distributed under the GNU General Public
License, version 2. The licence is included in vendor/mpdf/mpdf/LICENSE.txt.
The Studio Schedule source code carries its original MIT notice in LICENSE.
NOTICE

image=studio-schedule-php-tests
docker build --tag "$image" - <"$root/php/test.Dockerfile"
cache=${COMPOSER_CACHE_DIR:-${XDG_CACHE_HOME:-$HOME/.cache}/studio-schedule/composer}
mkdir -p "$cache"
docker run --rm --user "$(id -u):$(id -g)" \
	-v "$bundle/app:/app" -v "$cache:/composer-cache" \
	-e COMPOSER_HOME=/tmp/composer -e COMPOSER_CACHE_DIR=/composer-cache \
	-w /app "$image" composer install --no-dev --no-interaction --prefer-dist --no-progress --optimize-autoloader

(cd "$bundle" && zip -X -q -r "$work/studio-schedule-php.zip" public app)
mkdir -p "$(dirname "$output")"
mv -- "$work/studio-schedule-php.zip" "$output"
echo "Built $output"
