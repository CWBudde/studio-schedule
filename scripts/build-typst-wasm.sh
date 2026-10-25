#!/usr/bin/env bash
# Builds the receipt renderer (tools/receipt-pdf, typst) to a WebAssembly
# reactor module, reproducibly (ADR 0012): in a pinned container image (Rust
# 1.99.1 on linux/amd64, the toolchain of rust-toolchain.toml), with the
# locked dependencies (Cargo.lock) and the paths remapped out of the binary.
# The host matters: cargo mixes the compiler's host into its symbol hashes,
# which changes what link-time optimization makes of the code, so the same
# sources give other bytes on macOS or arm64. Hence the container, also on
# CI.
#
#   scripts/build-typst-wasm.sh          build, install to internal/pdf/typstwasm, pin its sha256
#   scripts/build-typst-wasm.sh --check  build and fail unless it matches the pinned sha256
#                                        and the committed module (CI)
#
# Needs Docker (on Apple silicon it emulates amd64: a few minutes). The
# registry, the toolchain's targets and the build cache live in Docker
# volumes named studio-schedule-typst-*; the global Rust setup is never
# touched.
set -euo pipefail

IMAGE="rust:1.99.1-slim-bookworm@sha256:86234e857a413004348b471d11a4ebe1aa92d2eae2b6b143cac68fd1ac3c48be"

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT_REL="internal/pdf/typstwasm/receipt.wasm"

mode=build
inside=false
for arg in "$@"; do
	case $arg in
	--check) mode=check ;;
	--in-container) inside=true ;;
	*)
		echo "usage: $0 [--check]" >&2
		exit 2
		;;
	esac
done

if ! $inside; then
	args=(--in-container)
	[[ $mode == check ]] && args+=(--check)
	exec docker run --rm --platform linux/amd64 \
		-v "$ROOT_DIR:/work" -w /work \
		-v studio-schedule-typst-registry:/usr/local/cargo/registry \
		-v studio-schedule-typst-rustup:/usr/local/rustup \
		-v studio-schedule-typst-target:/target \
		"$IMAGE" scripts/build-typst-wasm.sh "${args[@]}"
fi

# ---- inside the container
CRATE_DIR=/work/tools/receipt-pdf
OUT="/work/$OUT_REL"
SHA_FILE="$OUT.sha256"

cd "$CRATE_DIR"
# Adds the target of rust-toolchain.toml (wasm32-wasip1) to the image's
# toolchain.
rustup toolchain install

# The binary must not name the paths of the build: the registry, the crate and
# the toolchain are remapped to fixed names.
export RUSTFLAGS="--remap-path-prefix=$CARGO_HOME/registry/src=/cargo --remap-path-prefix=$CRATE_DIR=/src --remap-path-prefix=$RUSTUP_HOME=/rustup"
export CARGO_TARGET_DIR=/target
export SOURCE_DATE_EPOCH=0
# typst-utils embeds the commit of the git repository it is built in (ours)
# unless this is set.
export TYPST_COMMIT_SHA="crates.io"

cargo build --locked --release --lib --target wasm32-wasip1
built="$CARGO_TARGET_DIR/wasm32-wasip1/release/receipt_pdf.wasm"

sha() { sha256sum "$1" | cut -d' ' -f1; }
sum=$(sha "$built")
size=$(wc -c <"$built")

if [[ $mode == check ]]; then
	pinned=$(cut -d' ' -f1 <"$SHA_FILE")
	committed=$(sha "$OUT")
	echo "receipt.wasm: built $sum ($size bytes), pinned $pinned, committed $committed"
	if [[ $sum != "$pinned" || $committed != "$pinned" ]]; then
		echo "receipt.wasm does not match tools/receipt-pdf: rebuild it with 'just typst-wasm'" >&2
		echo "(or the checkout has a Git LFS pointer instead of the module: git lfs pull)" >&2
		exit 1
	fi
	exit 0
fi

cp "$built" "$OUT"
chmod 0644 "$OUT"
echo "$sum  receipt.wasm" >"$SHA_FILE"
# On Linux the files would belong to root otherwise.
chown --reference="$CRATE_DIR/Cargo.toml" "$OUT" "$SHA_FILE"
echo "receipt.wasm: $size bytes, sha256 $sum"
