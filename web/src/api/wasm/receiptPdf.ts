/**
 * Receipt PDFs in the demo (ADR 0012): the worker runs the same typst
 * WebAssembly module as the server (internal/pdf/typstwasm/receipt.wasm, copied
 * to the demo's assets by scripts/build-wasm.sh) with the browser's own
 * WebAssembly. Go asks for a render through `__studioScheduleRenderPDF`
 * (internal/wasmbridge/pdf_js.go).
 *
 * The module is a WASI reactor; it needs only a few WASI calls (no files,
 * no network), which the small shim below provides. Random numbers are zeros,
 * as on the server, so both produce the same bytes. This file imports nothing,
 * so Node runs it as is (scripts/receipt-pdf-smoke.mjs).
 */

/** Renders one input (see internal/pdf.Input) to a PDF. */
export type RenderPdf = (input: Uint8Array) => Uint8Array;

const WASI = 'wasi_snapshot_preview1';
const SUCCESS = 0;
const EBADF = 8;
const EINVAL = 28;
const ENOSYS = 52;
/** wazero's fake clocks: midnight UTC 2022-01-01, in nanoseconds. */
const FAKE_EPOCH_NANOS = 1_640_995_200_000n * 1_000_000n;
const MILLISECOND = 1_000_000n;
/** The name of errors typst reports; the instance stays usable. */
const TEMPLATE_ERROR = 'ReceiptTemplateError';

interface ReceiptExports {
  memory: WebAssembly.Memory;
  alloc(len: number): number;
  render(ptr: number, len: number): number;
  result_ptr(): number;
  result_len(): number;
  _initialize?: () => void;
}

/** The WASI functions the module calls; every other import fails with ENOSYS. */
function wasi(memory: () => WebAssembly.Memory): Record<string, (...args: never[]) => number> {
  const view = () => new DataView(memory().buffer);
  // The clocks of the server's runtime (wazero's defaults), per instance: the
  // wall clock starts at FAKE_EPOCH_NANOS, the monotonic one at 0, and both
  // advance by a millisecond per reading. Real clocks would make the bytes
  // depend on when the demo renders, if typst ever read them.
  let walltime = FAKE_EPOCH_NANOS - MILLISECOND;
  let nanotime = -MILLISECOND;
  return {
    random_get(ptr: number, len: number) {
      new Uint8Array(memory().buffer, ptr, len).fill(0);
      return SUCCESS;
    },
    environ_sizes_get(count: number, size: number) {
      view().setUint32(count, 0, true);
      view().setUint32(size, 0, true);
      return SUCCESS;
    },
    environ_get: () => SUCCESS,
    args_sizes_get(count: number, size: number) {
      view().setUint32(count, 0, true);
      view().setUint32(size, 0, true);
      return SUCCESS;
    },
    args_get: () => SUCCESS,
    clock_time_get(id: number, _precision: bigint, out: number) {
      let now: bigint;
      if (id === 0)
        now = walltime += MILLISECOND; // realtime
      else if (id === 1)
        now = nanotime += MILLISECOND; // monotonic
      else return EINVAL;
      view().setBigUint64(out, now, true);
      return SUCCESS;
    },
    // stdout and stderr: counted and dropped.
    fd_write(_fd: number, iovs: number, count: number, written: number) {
      const v = view();
      let total = 0;
      for (let i = 0; i < count; i++) total += v.getUint32(iovs + i * 8 + 4, true);
      v.setUint32(written, total, true);
      return SUCCESS;
    },
    fd_close: () => EBADF,
    fd_fdstat_get: () => EBADF,
    fd_filestat_get: () => EBADF,
    fd_prestat_get: () => EBADF,
    fd_prestat_dir_name: () => EBADF,
    fd_read: () => EBADF,
    fd_seek: () => EBADF,
    sched_yield: () => SUCCESS,
    proc_exit(code: number) {
      throw new Error(`receipt renderer exited with ${code}`);
    },
  };
}

/** Instantiates the compiled module and returns its render function. */
export async function instantiateReceiptPdf(module: WebAssembly.Module): Promise<RenderPdf> {
  // The shim needs the memory, which exists only after instantiation.
  const state: { exports?: ReceiptExports } = {};
  const shim = wasi(() => {
    if (!state.exports) throw new Error('receipt renderer: memory used before instantiation');
    return state.exports.memory;
  });

  const imports: Record<string, Record<string, WebAssembly.ImportValue>> = {};
  for (const imp of WebAssembly.Module.imports(module)) {
    if (imp.kind !== 'function')
      throw new Error(`receipt renderer: unsupported import ${imp.module}.${imp.name}`);
    const fns = (imports[imp.module] ??= {});
    fns[imp.name] = (imp.module === WASI && shim[imp.name]) || (() => ENOSYS);
  }

  const instance = await WebAssembly.instantiate(module, imports);
  const ex = instance.exports as unknown as ReceiptExports;
  state.exports = ex;
  ex._initialize?.();

  return (input) => {
    const ptr = ex.alloc(input.length);
    new Uint8Array(ex.memory.buffer, ptr, input.length).set(input);
    const status = ex.render(ptr, input.length);
    // A copy: the module reuses its result buffer.
    const out = new Uint8Array(ex.memory.buffer, ex.result_ptr(), ex.result_len()).slice();
    if (status !== 0) {
      const err = new Error(`receipt renderer: ${new TextDecoder().decode(out)}`);
      err.name = TEMPLATE_ERROR;
      throw err;
    }
    return out;
  };
}

/**
 * Returns an async render function that loads and instantiates the module on
 * its first call (the module is large: the demo loads it only when a receipt
 * is rendered). A failed load is retried on the next call.
 */
export function lazyReceiptPdf(
  load: () => Promise<WebAssembly.Module>,
): (input: Uint8Array) => Promise<Uint8Array> {
  let renderer: Promise<RenderPdf> | null = null;
  return async (input) => {
    renderer ??= load().then(instantiateReceiptPdf);
    try {
      return (await renderer)(input);
    } catch (err) {
      // A failed load, or an instance that trapped: start over next time.
      if (!(err instanceof Error && err.name === TEMPLATE_ERROR)) renderer = null;
      throw err;
    }
  };
}
