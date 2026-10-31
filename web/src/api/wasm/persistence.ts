import type { DemoSchool } from '@/features/demo/school';
import type { StoredCookie } from './cookieJar';

/**
 * Keeps the demo's data across page loads: the worker saves the Go store
 * (`StudioScheduleAPI.snapshot()`) and its cookie jar in IndexedDB after
 * every committed change (Go calls `__studioScheduleChanged()`) and hands
 * both back on the next start. Only the tab that holds the demo's lock runs
 * a worker (tabLock.ts). As a second line of defence against a tab that lost
 * the lock but still saves (it was frozen when another tab took over), saves
 * are fenced: each worker claims the data with a new epoch, and a save is
 * written only while the stored epoch is still its own. Each demo school
 * has its own record (`snapshotKey`), the epoch is one for both: one worker
 * per origin runs, whichever school it runs. The saved state lives until
 * "Reset demo" clears the school's record; a snapshot of another build (or
 * another school) is refused by the Go side, which then starts from the seed
 * (docs/demo-mode.md).
 */

/** What is saved: one record per school, replaced on every save. */
export interface DemoSnapshot {
  /** Version of this record's shape; a record of another version is ignored. */
  format: typeof SNAPSHOT_FORMAT;
  /** The Go store, as `StudioScheduleAPI.snapshot()` returns it. */
  store: Uint8Array;
  /** The worker's cookie jar (the session). */
  cookies: StoredCookie[];
  /** Epoch milliseconds of the save. */
  savedAt: number;
}

export const SNAPSHOT_FORMAT = 1;

/** One transaction on the saved data. */
export interface SnapshotTx {
  get(key: string): Promise<unknown>;
  put(key: string, value: unknown): Promise<void>;
  delete(key: string): Promise<void>;
}

/** Where the snapshot is kept: IndexedDB, or a fake in tests. */
export interface SnapshotStorage {
  /**
   * Runs `body` as one transaction: all or nothing, and in order with the
   * transactions of other workers and tabs.
   */
  transact<T>(mode: 'readonly' | 'readwrite', body: (tx: SnapshotTx) => Promise<T>): Promise<T>;
}

/**
 * The record of each school's snapshot. Programme's is `current`, the record
 * of the demo before it had two schools: its visitors keep their data.
 */
const SNAPSHOT_KEYS: Record<DemoSchool, string> = {
  programme: 'current',
  werkstatt: 'current-werkstatt',
};
const OWNER = 'owner';

/** The record that holds `school`'s snapshot. */
export function snapshotKey(school: DemoSchool): string {
  return SNAPSHOT_KEYS[school];
}

/** A save of a worker whose claim another worker has replaced. */
export class StaleOwnerError extends Error {
  constructor() {
    super('Another tab owns the demo data now');
    this.name = 'StaleOwnerError';
  }
}

/** What a worker gets when it claims the saved data. */
export interface Claim {
  epoch: number;
  snapshot: DemoSnapshot | null;
}

/**
 * Makes the caller the owner of the saved data and reads `school`'s
 * snapshot, in one transaction: saves of earlier owners (of either school)
 * are refused from then on.
 */
export function claim(storage: SnapshotStorage, school: DemoSchool): Promise<Claim> {
  return storage.transact('readwrite', async (tx) => {
    const owner = await tx.get(OWNER);
    const epoch = (typeof owner === 'number' ? owner : 0) + 1;
    await tx.put(OWNER, epoch);
    return { epoch, snapshot: toSnapshot(await tx.get(snapshotKey(school))) };
  });
}

/**
 * Claims the saved data at start, or returns null if IndexedDB fails or does
 * not answer within `timeoutMs`: the demo then starts from the seed and saves
 * nothing.
 */
export async function claimOrGiveUp(
  storage: SnapshotStorage,
  school: DemoSchool,
  timeoutMs = 3_000,
): Promise<Claim | null> {
  let timer: ReturnType<typeof setTimeout> | undefined;
  try {
    return await Promise.race([
      claim(storage, school),
      new Promise<never>((_, reject) => {
        timer = setTimeout(() => reject(new Error('IndexedDB did not answer')), timeoutMs);
      }),
    ]);
  } catch (err) {
    console.warn('[studio-schedule] the demo data cannot be saved in this browser', err);
    return null;
  } finally {
    clearTimeout(timer);
  }
}

/** Saves `school`'s `snapshot` if `epoch` still owns the data; else StaleOwnerError. */
export function saveSnapshot(
  storage: SnapshotStorage,
  school: DemoSchool,
  epoch: number,
  snapshot: DemoSnapshot,
): Promise<void> {
  return storage.transact('readwrite', async (tx) => {
    if ((await tx.get(OWNER)) !== epoch) throw new StaleOwnerError();
    await tx.put(snapshotKey(school), snapshot);
  });
}

/**
 * Deletes `school`'s saved snapshot; the other school's stays, and so does
 * the owner epoch, so stale saves stay refused.
 */
export function clearSnapshot(storage: SnapshotStorage, school: DemoSchool): Promise<void> {
  return storage.transact('readwrite', (tx) => tx.delete(snapshotKey(school)));
}

/** The record as a snapshot, or null if it is none (or of another version). */
export function toSnapshot(value: unknown): DemoSnapshot | null {
  if (typeof value !== 'object' || value === null) return null;
  const record = value as Partial<Record<keyof DemoSnapshot, unknown>>;
  if (
    record.format !== SNAPSHOT_FORMAT ||
    !(record.store instanceof Uint8Array) ||
    !Array.isArray(record.cookies)
  ) {
    return null;
  }
  return {
    format: SNAPSHOT_FORMAT,
    store: record.store,
    // A malformed cookie is dropped; at worst the visitor is signed out.
    cookies: record.cookies.filter(isCookie),
    savedAt: typeof record.savedAt === 'number' ? record.savedAt : 0,
  };
}

function isCookie(value: unknown): value is StoredCookie {
  if (typeof value !== 'object' || value === null) return false;
  const c = value as Partial<Record<keyof StoredCookie, unknown>>;
  return (
    typeof c.name === 'string' &&
    c.name !== '' &&
    typeof c.value === 'string' &&
    typeof c.path === 'string' &&
    c.path.startsWith('/') &&
    (c.expiresAt === null || (typeof c.expiresAt === 'number' && Number.isFinite(c.expiresAt))) &&
    typeof c.createdAt === 'number' &&
    Number.isInteger(c.createdAt) &&
    c.createdAt >= 0
  );
}

/**
 * Saves `school`'s snapshots, debounced: `schedule` after each change, and the save
 * runs once the changes have paused for `delayMs`. Saves never overlap.
 * `onError` hears about the first failed save only (e.g. no quota, or
 * IndexedDB blocked in a private window), so the visitor is told once. A
 * save refused because another worker claimed the data stops the saver
 * silently, as does `stop` (the Go program exited).
 */
export class SnapshotSaver {
  private timer: ReturnType<typeof setTimeout> | null = null;
  private running: Promise<void> = Promise.resolve();
  private failed = false;
  private stopped = false;
  private readonly storage: SnapshotStorage;
  private readonly school: DemoSchool;
  private readonly epoch: number;
  private readonly capture: () => Promise<DemoSnapshot>;
  private readonly onError: (err: unknown) => void;
  private readonly delayMs: number;

  constructor(
    storage: SnapshotStorage,
    school: DemoSchool,
    epoch: number,
    capture: () => Promise<DemoSnapshot>,
    onError: (err: unknown) => void = () => undefined,
    delayMs = 250,
  ) {
    this.storage = storage;
    this.school = school;
    this.epoch = epoch;
    this.capture = capture;
    this.onError = onError;
    this.delayMs = delayMs;
  }

  /** Saves after the next pause in changes. */
  schedule(): void {
    if (this.stopped) return;
    if (this.timer !== null) clearTimeout(this.timer);
    this.timer = setTimeout(() => {
      this.timer = null;
      this.enqueue();
    }, this.delayMs);
  }

  /** Saves a scheduled change now; resolves once all saves so far are done. */
  flush(): Promise<void> {
    if (this.stopped) return Promise.resolve();
    if (this.timer !== null) {
      clearTimeout(this.timer);
      this.timer = null;
      this.enqueue();
    }
    return this.running;
  }

  /** Saves nothing any more, and drops a scheduled save. */
  stop(): void {
    this.stopped = true;
    if (this.timer !== null) clearTimeout(this.timer);
    this.timer = null;
  }

  private enqueue(): void {
    this.running = this.running.then(() => this.save());
  }

  private async save(): Promise<void> {
    if (this.stopped) return;
    try {
      const snapshot = await this.capture();
      if (this.stopped) return;
      await saveSnapshot(this.storage, this.school, this.epoch, snapshot);
    } catch (err) {
      if (this.stopped) return;
      if (err instanceof StaleOwnerError) {
        this.stop();
        return;
      }
      console.warn('[studio-schedule] could not save the demo data', err);
      if (!this.failed) {
        this.failed = true;
        this.onError(err);
      }
    }
  }
}

/** The steps of "Reset demo", injectable for tests. */
export interface ResetSteps {
  /** Stops this tab's worker, so nothing is saved any more. */
  stop(): void;
  /** Deletes the saved snapshot of the school this page runs. */
  clear(): Promise<void>;
  /** Remembers, across the reload, that the reset failed. */
  markFailed(): void;
  reload(): void;
}

/**
 * Resets the demo without relying on the worker (which may have crashed or
 * hang in a save): stop it, delete the school's snapshot from this thread,
 * reload. A delete that fails or takes longer than `timeoutMs` is reported after the
 * reload instead of blocking the page.
 */
export async function resetDemoData(steps: ResetSteps, timeoutMs = 5_000): Promise<void> {
  steps.stop();
  let timer: ReturnType<typeof setTimeout> | undefined;
  try {
    await Promise.race([
      steps.clear(),
      new Promise<never>((_, reject) => {
        timer = setTimeout(() => reject(new Error('Deleting the demo data timed out')), timeoutMs);
      }),
    ]);
  } catch (err) {
    console.warn('[studio-schedule] could not delete the demo data', err);
    steps.markFailed();
  } finally {
    clearTimeout(timer);
  }
  steps.reload();
}

const DB_NAME = 'studio-schedule-demo';
const STORE_NAME = 'snapshot';

/**
 * The saved data in IndexedDB: database `studio-schedule-demo`, object store
 * `snapshot` with the records `current` (Programme's snapshot),
 * `current-werkstatt` (Werkstatt's) and `owner` (the epoch of the worker
 * that may save, one for both schools).
 */
export function indexedDbStorage(): SnapshotStorage {
  let db: Promise<IDBDatabase> | null = null;
  const open = () => {
    db ??= new Promise<IDBDatabase>((resolve, reject) => {
      const req = indexedDB.open(DB_NAME, 1);
      req.onupgradeneeded = () => req.result.createObjectStore(STORE_NAME);
      req.onsuccess = () => resolve(req.result);
      req.onerror = () => reject(req.error ?? new Error('indexedDB.open failed'));
    });
    // Let a later call try again after a failure.
    db.catch(() => {
      db = null;
    });
    return db;
  };

  const settle = <T>(req: IDBRequest<T>) =>
    new Promise<T>((resolve, reject) => {
      req.onsuccess = () => resolve(req.result);
      req.onerror = () => reject(req.error ?? new Error('IndexedDB request failed'));
    });

  return {
    async transact<T>(
      mode: 'readonly' | 'readwrite',
      body: (tx: SnapshotTx) => Promise<T>,
    ): Promise<T> {
      const tx = (await open()).transaction(STORE_NAME, mode);
      const store = tx.objectStore(STORE_NAME);
      const done = new Promise<void>((resolve, reject) => {
        tx.oncomplete = () => resolve();
        tx.onerror = () => reject(tx.error ?? new Error('IndexedDB transaction failed'));
        tx.onabort = () => reject(tx.error ?? new Error('IndexedDB transaction aborted'));
      });
      // The body's requests run inside the transaction: each continues from
      // the previous one's success event, which keeps the transaction active.
      let result: T;
      try {
        result = await body({
          get: (key) => settle(store.get(key)),
          put: async (key, value) => {
            await settle(store.put(value, key));
          },
          delete: async (key) => {
            await settle(store.delete(key));
          },
        });
      } catch (err) {
        done.catch(() => undefined);
        try {
          tx.abort();
        } catch {
          // Already finished.
        }
        throw err;
      }
      await done;
      return result;
    },
  };
}
