#!/usr/bin/env bash
# Builds the Go application for GOOS=js GOARCH=wasm and places it, together
# with the matching wasm_exec.js from the active Go toolchain and the receipt
# renderer (receipt.wasm), into web/public so that Vite picks them up for the
# demo build.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT_DIR="${1:-$ROOT_DIR/web/public}"
VERSION="${VERSION:-$(git -C "$ROOT_DIR" describe --tags --always --dirty 2>/dev/null || echo dev)}"

GO_WASM_EXEC=""
for candidate in "$(go env GOROOT)/lib/wasm/wasm_exec.js" "$(go env GOROOT)/misc/wasm/wasm_exec.js"; do
	if [[ -f $candidate ]]; then
		GO_WASM_EXEC="$candidate"
		break
	fi
done
if [[ -z $GO_WASM_EXEC ]]; then
	echo "wasm_exec.js not found in GOROOT ($(go env GOROOT))" >&2
	exit 1
fi

mkdir -p "$OUT_DIR"
cp "$GO_WASM_EXEC" "$OUT_DIR/wasm_exec.js"

cd "$ROOT_DIR"
GOOS=js GOARCH=wasm go build \
	-trimpath \
	-ldflags="-s -w -X main.version=$VERSION" \
	-o "$OUT_DIR/app.wasm" \
	./cmd/wasm

# The receipt renderer (ADR 0012): the worker loads the same typst module as
# the server when it renders the first receipt.
RECEIPT_WASM="$ROOT_DIR/internal/pdf/typstwasm/receipt.wasm"
if [[ "$(head -c 4 "$RECEIPT_WASM" | od -An -c | tr -d ' ')" != '\0asm' ]]; then
	echo "$RECEIPT_WASM is not a WebAssembly module (a Git LFS pointer? run git lfs pull)" >&2
	exit 1
fi
cp "$RECEIPT_WASM" "$OUT_DIR/receipt.wasm"

raw=$(wc -c <"$OUT_DIR/app.wasm" | tr -d ' ')
gz=$(gzip -9 -c "$OUT_DIR/app.wasm" | wc -c | tr -d ' ')
echo "app.wasm: $raw bytes raw, $gz bytes gzip ($VERSION)"
receipt_raw=$(wc -c <"$OUT_DIR/receipt.wasm" | tr -d ' ')
receipt_gz=$(gzip -9 -c "$OUT_DIR/receipt.wasm" | wc -c | tr -d ' ')
echo "receipt.wasm: $receipt_raw bytes raw, $receipt_gz bytes gzip (loaded on the first receipt)"
