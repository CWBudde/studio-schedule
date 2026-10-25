# receipt-pdf

The receipt renderer of studio-schedule (ADR 0012): the template
[`receipt.typ`](receipt.typ) and its fonts compiled with
[typst](https://typst.app) into a WebAssembly reactor module. The server runs
it with wazero (`internal/pdf/typstwasm`), the web demo with the browser's
WebAssembly (`web/src/api/wasm/receiptPdf.ts`), so both render the same bytes.

- Input: the length of the JSON data (little-endian u32), the data (the
  template's `/data.json`, built by `internal/service/receipt_pdf.go`), then
  the optional logo (`/logo.png` or `/logo.jpg`).
- Exports: `alloc(len) → ptr`, `render(ptr, len) → status` (0: PDF, 1: error
  text), `result_ptr()`, `result_len()`.

## Build

`just typst-wasm` builds the module reproducibly in a pinned Docker image
(Rust 1.99.1 on linux/amd64, `rust-toolchain.toml`, `Cargo.lock`, paths
remapped) and installs it to `internal/pdf/typstwasm/receipt.wasm` with its
sha256 pinned next to it. CI rebuilds it (`just typst-wasm-check`) and fails
unless the bytes match. The module is committed with Git LFS.

After changing the template or the crate: `just typst-wasm`, then
`go test ./internal/pdf/... -update` to accept the new golden checksums
(check the PDF first).

For a quick look at the template, render the sample natively with any Rust
toolchain (the output differs in bytes from the module, not in looks):

    cargo run --release -- testdata/sample.json > /tmp/receipt.pdf

Licenses of the fonts and crates: [THIRD_PARTY.md](THIRD_PARTY.md).
