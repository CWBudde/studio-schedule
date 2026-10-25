// Renders the sample receipt with the typst module (internal/pdf/typstwasm)
// through the demo worker's WASI shim (web/src/api/wasm/receiptPdf.ts) on
// Node's WebAssembly, as the browser does, and checks that the bytes are the
// server's (the golden checksum of TestGoldenWithoutLogo).
//
//   node scripts/receipt-pdf-smoke.mjs
//
// Node runs the TypeScript shim directly (type stripping); it needs Node
// 23.6 or later (CI runs 24).
import { createHash } from 'node:crypto';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const [major, minor] = process.versions.node.split('.').map(Number);
if (major < 23 || (major === 23 && minor < 6)) {
  console.error(`receipt-pdf-smoke needs Node 23.6 or later to run the TypeScript shim (this is ${process.version})`);
  process.exit(1);
}

const { lazyReceiptPdf } = await import('../web/src/api/wasm/receiptPdf.ts');

const root = join(dirname(fileURLToPath(import.meta.url)), '..');
const dir = join(root, 'internal/pdf/typstwasm');

const bytes = readFileSync(join(dir, 'receipt.wasm'));
if (bytes.subarray(0, 4).toString('latin1') !== '\0asm') {
  console.error('receipt.wasm is not a WebAssembly module (a Git LFS pointer? run git lfs pull)');
  process.exit(1);
}

const data = readFileSync(join(dir, 'testdata/receipt.json'), 'utf8').replace('"logo": "png"', '"logo": ""');
const json = new TextEncoder().encode(data);
const input = new Uint8Array(4 + json.length);
new DataView(input.buffer).setUint32(0, json.length, true);
input.set(json, 4);

let t = performance.now();
const render = lazyReceiptPdf(() => WebAssembly.compile(bytes));
const pdf = await render(input);
const first = performance.now() - t;
t = performance.now();
const again = await render(input);
const second = performance.now() - t;

const sha = (b) => createHash('sha256').update(b).digest('hex');
const want = readFileSync(join(dir, 'testdata/receipt-nologo.pdf.sha256'), 'utf8').trim();

console.log(
  `receipt PDF: ${pdf.length} bytes, first render ${first.toFixed(0)} ms (with compilation), then ${second.toFixed(0)} ms`,
);

if (sha(pdf) !== want || sha(again) !== want) {
  console.error(`sha256 ${sha(pdf)} / ${sha(again)}, want ${want} (the server's)`);
  process.exit(1);
}

// A template error comes back as an error, and the instance stays usable.
const broken = new Uint8Array([1, 0, 0, 0, 0x7b]);
await render(broken).then(
  () => {
    console.error('a broken input rendered');
    process.exit(1);
  },
  (err) => console.log(`broken input: ${err.message.split('\n')[0]}`),
);
if (sha(await render(input)) !== want) process.exit(1);
console.log('receipt PDF smoke test: ok (same bytes as the server)');
