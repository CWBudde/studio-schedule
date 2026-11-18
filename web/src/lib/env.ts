export type Transport = 'http' | 'wasm';

/** How the frontend reaches the API: a real HTTP server or the in-browser WASM backend. */
export const transport: Transport = import.meta.env.VITE_TRANSPORT === 'wasm' ? 'wasm' : 'http';

/** True for the static demo build where the Go backend runs as WebAssembly in the browser. */
export const isDemo = transport === 'wasm';

export const routerHistory: 'hash' | 'browser' =
  import.meta.env.VITE_ROUTER_HISTORY === 'hash' ? 'hash' : 'browser';

export const repositoryUrl = 'https://github.com/CWBudde/studio-schedule';

/** The app's root path, with a trailing slash. The server sets it before the page loads. */
export const appPath =
  typeof document !== 'undefined' && document.querySelector('base[href]')
    ? new URL(document.baseURI).pathname
    : import.meta.env.BASE_URL;
