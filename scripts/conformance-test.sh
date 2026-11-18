#!/usr/bin/env bash
# Exercise an already running test fixture at BASE_URL.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."

go test ./internal/conformance -run '^TestBackendConformance$' -count=1 -v -args -conformance
