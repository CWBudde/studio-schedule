/// <reference types="vite/client" />

interface ImportMetaEnv {
  /** "http" (default) talks to the Go server; "wasm" runs the Go backend in a Web Worker. */
  readonly VITE_TRANSPORT?: 'http' | 'wasm';
  /** "hash" uses hash-based routing (static hosting such as GitHub Pages). */
  readonly VITE_ROUTER_HISTORY?: 'hash' | 'browser';
}

interface ImportMeta {
  readonly env: ImportMetaEnv;
}
