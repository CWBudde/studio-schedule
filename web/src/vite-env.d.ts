/// <reference types="vite/client" />
/// <reference types="vite-plugin-pwa/vanillajs" />

interface ImportMetaEnv {
  /** "http" (default) talks to the Go server; "wasm" runs the Go backend in a Web Worker. */
  readonly VITE_TRANSPORT?: 'http' | 'wasm';
  /** "hash" uses hash-based routing (static hosting such as GitHub Pages). */
  readonly VITE_ROUTER_HISTORY?: 'hash' | 'browser';
  /**
   * The version of the API's answers (a hash of src/api/schema.d.ts, set
   * by vite.config.ts): a saved offline cache of another version is dropped.
   */
  readonly VITE_OFFLINE_CACHE_VERSION: string;
}

interface ImportMeta {
  readonly env: ImportMetaEnv;
}
