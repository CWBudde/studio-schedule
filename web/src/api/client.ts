import createClient from 'openapi-fetch';
import type { paths } from './schema';
import { transportFetch } from './transport';

/** Typed API client. The transport (HTTP or WASM worker) is chosen at build time. */
export const api = createClient<paths>({ baseUrl: '', fetch: transportFetch });

export type { paths };
