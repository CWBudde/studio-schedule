#!/usr/bin/env bash
# Build the PHP-FPM/nginx image using a built shared-hosting archive.
set -euo pipefail

archive=${1:?usage: build-php-image.sh ZIP IMAGE}
image=${2:?usage: build-php-image.sh ZIP IMAGE}
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
work=$(mktemp -d "${TMPDIR:-/tmp}/studio-php-image.XXXXXXXX")
trap 'rm -rf -- "$work"' EXIT

unzip -q "$archive" -d "$work"
if [[ ! -s $work/public/index.php || ! -s $work/app/vendor/autoload.php || ! -x $work/app/bin/studio-schedule ]]; then
	echo 'Build the shared-hosting ZIP first with scripts/build-php-release.sh.' >&2
	exit 1
fi
cp "$root/php/container/"{Dockerfile,nginx.conf,php-fpm.conf,serve.sh} "$work/"
# ZIPs built with a private umask still need to be readable by www-data in the image.
chmod -R g+rX,go-w "$work/app" "$work/public"
chmod g+r,go-w "$work/nginx.conf" "$work/php-fpm.conf"
docker build --tag "$image" "$work"
