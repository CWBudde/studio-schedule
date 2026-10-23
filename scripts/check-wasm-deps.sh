#!/usr/bin/env bash
# Guards the WASM demo build against server-only dependencies. The demo must
# run the same application code as production, but without database drivers,
# SMTP or CLI frameworks, which would bloat the binary or fail at runtime.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

forbidden='^(github\.com/spf13/cobra|github\.com/jackc/pgx|github\.com/pressly/goose|net/smtp|net/http/httptest|os/exec)(/|$)'

deps=$(GOOS=js GOARCH=wasm go list -deps ./cmd/wasm)
if bad=$(grep -E "$forbidden" <<<"$deps"); then
	echo "cmd/wasm depends on forbidden packages:" >&2
	echo "$bad" >&2
	exit 1
fi
echo "cmd/wasm dependency guard: ok"
