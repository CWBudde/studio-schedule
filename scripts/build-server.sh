#!/usr/bin/env bash
# Builds bin/studio-schedule with the production web UI embedded. The embed
# directory internal/webui/dist only holds a committed placeholder, so the
# built UI is staged there for the duration of the build and restored after.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
LDFLAGS="${1:--s -w}"
EMBED_DIR="$ROOT_DIR/internal/webui/dist"

cd "$ROOT_DIR"
if [[ ! -f web/dist/index.html ]]; then
	echo "web/dist missing, run 'just web-build' first" >&2
	exit 1
fi

restore() {
	find "$EMBED_DIR" -mindepth 1 ! -name index.html -exec rm -rf {} +
	git checkout -- "$EMBED_DIR/index.html"
}
trap restore EXIT

cp -R web/dist/. "$EMBED_DIR/"
mkdir -p bin
go build -trimpath -ldflags="$LDFLAGS" -o bin/studio-schedule ./cmd/studio-schedule
echo "built bin/studio-schedule"
