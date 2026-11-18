#!/usr/bin/env bash
set -euo pipefail

if [[ ${1:-serve} != serve ]]; then
	exec studio-schedule "$@"
fi
if (($# > 1)); then
	echo 'Usage: studio-schedule-php serve' >&2
	exit 2
fi

fpm=
nginx=
stopping=false
stop_servers() {
	local signal=$1
	if [[ -n $nginx ]]; then
		kill -s "$signal" "$nginx" 2>/dev/null || true
	fi
	if [[ -n $fpm ]]; then
		kill -s "$signal" "$fpm" 2>/dev/null || true
	fi
}
trap '
	stopping=true
	if [[ -S /tmp/studio-fpm.sock && -s /tmp/studio-nginx.pid ]]; then
		stop_servers QUIT
	else
		stop_servers TERM
	fi
' TERM INT QUIT

php-fpm --nodaemonize --fpm-config /etc/studio-schedule/php-fpm.conf &
fpm=$!
nginx -e stderr -c /etc/studio-schedule/nginx.conf -g 'daemon off;' &
nginx=$!
if [[ $stopping == true ]]; then
	stop_servers TERM
fi

status=0
wait -n "$fpm" "$nginx" || status=$?
if [[ $stopping == false ]]; then
	stop_servers TERM
fi
wait "$nginx" "$fpm" 2>/dev/null || true
if [[ $stopping == true ]]; then
	exit 0
fi
if ((status == 0)); then
	status=1
fi
exit "$status"
