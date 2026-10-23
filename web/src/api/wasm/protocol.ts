/** Header list as ordered name/value pairs (keeps duplicates such as Set-Cookie). */
export type HeaderPairs = [string, string][];

/** Main thread -> worker. */
export interface WorkerRequestMessage {
  type: 'request';
  id: number;
  method: string;
  /** Path plus query, e.g. `/api/v1/health?x=1`. */
  url: string;
  headers: HeaderPairs;
  body: ArrayBuffer | null;
}

export type MainToWorkerMessage = WorkerRequestMessage;

/** Worker -> main thread. */
export type WorkerToMainMessage =
  | { type: 'ready' }
  | { type: 'fatal'; message: string }
  | {
      type: 'response';
      id: number;
      status: number;
      statusText: string;
      headers: HeaderPairs;
      body: ArrayBuffer;
    }
  | { type: 'error'; id: number; message: string };

/** Request shape passed to the Go side: `StudioScheduleAPI.fetch(req)`. */
export interface GoBridgeRequest {
  method: string;
  url: string;
  headers: HeaderPairs;
  body: Uint8Array | null;
}

/** Response shape resolved by the Go side. */
export interface GoBridgeResponse {
  status: number;
  headers: HeaderPairs | Record<string, string> | null | undefined;
  body: Uint8Array | null | undefined;
}

/** The object the Go program installs as `globalThis.StudioScheduleAPI`. */
export interface GoBridgeAPI {
  fetch(request: GoBridgeRequest): Promise<GoBridgeResponse>;
  /** Either `true` once ready, or a promise that resolves when ready. */
  ready?: boolean | PromiseLike<unknown>;
}
