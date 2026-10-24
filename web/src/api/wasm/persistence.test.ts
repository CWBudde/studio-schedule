import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import {
  claim,
  claimOrGiveUp,
  clearSnapshot,
  resetDemoData,
  SNAPSHOT_FORMAT,
  SnapshotSaver,
  toSnapshot,
  type DemoSnapshot,
  type SnapshotStorage,
  type SnapshotTx,
} from './persistence';

/**
 * IndexedDB's guarantees in memory: transactions run one after the other,
 * and a failed one changes nothing.
 */
class MemoryStorage implements SnapshotStorage {
  readonly data = new Map<string, unknown>();
  private queue: Promise<unknown> = Promise.resolve();
  writes = 0;

  transact<T>(_mode: 'readonly' | 'readwrite', body: (tx: SnapshotTx) => Promise<T>): Promise<T> {
    const run = async () => {
      const copy = new Map(this.data);
      const tx: SnapshotTx = {
        get: async (key) => copy.get(key),
        put: async (key, value) => {
          copy.set(key, value);
        },
        delete: async (key) => {
          copy.delete(key);
        },
      };
      const result = await body(tx);
      this.data.clear();
      for (const [k, v] of copy) this.data.set(k, v);
      this.writes++;
      return result;
    };
    const next = this.queue.then(run, run);
    this.queue = next.catch(() => undefined);
    return next;
  }

  get current(): unknown {
    return this.data.get('current');
  }
}

function snapshot(n: number): DemoSnapshot {
  return { format: SNAPSHOT_FORMAT, store: new Uint8Array([n]), cookies: [], savedAt: n };
}

const cookie = { name: 'studio_session', value: 't', path: '/', expiresAt: null, createdAt: 0 };

describe('toSnapshot', () => {
  it('accepts a saved snapshot', () => {
    const record = { ...snapshot(1), cookies: [cookie] };
    expect(toSnapshot(record)).toEqual(record);
  });

  it('ignores a missing or foreign record', () => {
    expect(toSnapshot(undefined)).toBeNull();
    expect(toSnapshot({ ...snapshot(1), format: 0 })).toBeNull();
    expect(toSnapshot({ format: SNAPSHOT_FORMAT, store: 'nope', cookies: [] })).toBeNull();
    expect(toSnapshot({ ...snapshot(1), cookies: 'nope' })).toBeNull();
  });

  it('drops malformed cookies', () => {
    const malformed = [
      null,
      'session=1',
      { ...cookie, name: '' },
      { ...cookie, name: 1 },
      { ...cookie, value: undefined },
      { ...cookie, path: 'api' },
      { ...cookie, expiresAt: 'tomorrow' },
      { ...cookie, expiresAt: Number.NaN },
      { ...cookie, createdAt: -1 },
      { ...cookie, createdAt: 1.5 },
    ];
    const later = { ...cookie, name: 'later', expiresAt: 1_900_000_000_000, createdAt: 1 };
    expect(toSnapshot({ ...snapshot(1), cookies: [cookie, ...malformed, later] })?.cookies).toEqual(
      [cookie, later],
    );
  });
});

describe('claim and fenced saves', () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it('hands the saved data to the new owner and refuses saves of the old one', async () => {
    const storage = new MemoryStorage();
    const onError = vi.fn();

    const first = await claim(storage);
    expect(first.snapshot).toBeNull();
    let n = 0;
    const old = new SnapshotSaver(storage, first.epoch, async () => snapshot(++n), onError, 10);
    old.schedule();
    await old.flush();
    expect(storage.current).toEqual(snapshot(1));

    // Another tab takes over: it claims and reads the latest save.
    const second = await claim(storage);
    expect(second.epoch).toBeGreaterThan(first.epoch);
    expect(second.snapshot).toEqual(snapshot(1));

    // The old tab still saves (it was frozen): refused, silently, and for good.
    old.schedule();
    await old.flush();
    expect(storage.current).toEqual(snapshot(1));
    expect(onError).not.toHaveBeenCalled();
    old.schedule();
    await vi.advanceTimersByTimeAsync(100);
    expect(n).toBe(2);

    const current = new SnapshotSaver(storage, second.epoch, async () => snapshot(9), onError, 10);
    current.schedule();
    await current.flush();
    expect(storage.current).toEqual(snapshot(9));
  });

  it('refuses a save that was under way when another tab claimed the data', async () => {
    const storage = new MemoryStorage();
    const first = await claim(storage);
    let release!: () => void;
    const old = new SnapshotSaver(
      storage,
      first.epoch,
      async () => {
        await new Promise<void>((resolve) => (release = resolve));
        return snapshot(1);
      },
      undefined,
      10,
    );
    old.schedule();
    await vi.advanceTimersByTimeAsync(10);

    await claim(storage);
    release();
    await old.flush();
    expect(storage.current).toBeUndefined();
  });

  it('keeps the owner when the snapshot is cleared', async () => {
    const storage = new MemoryStorage();
    const first = await claim(storage);
    const saver = new SnapshotSaver(storage, first.epoch, async () => snapshot(1), undefined, 10);
    saver.schedule();
    await saver.flush();

    await clearSnapshot(storage);
    expect(storage.current).toBeUndefined();
    expect((await claim(storage)).epoch).toBe(first.epoch + 1);
  });

  it('gives up claiming when IndexedDB does not answer', async () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => undefined);
    const hanging: SnapshotStorage = { transact: () => new Promise(() => undefined) };
    let result: unknown = 'pending';
    const claiming = claimOrGiveUp(hanging, 3_000).then((r) => (result = r));
    await vi.advanceTimersByTimeAsync(2_999);
    expect(result).toBe('pending');
    await vi.advanceTimersByTimeAsync(1);
    await claiming;
    expect(result).toBeNull();

    const failing: SnapshotStorage = { transact: () => Promise.reject(new Error('blocked')) };
    expect(await claimOrGiveUp(failing)).toBeNull();
    expect(warn).toHaveBeenCalled();
    warn.mockRestore();
  });
});

describe('SnapshotSaver', () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  async function owned() {
    const storage = new MemoryStorage();
    return { storage, epoch: (await claim(storage)).epoch };
  }

  it('saves once after a burst of changes', async () => {
    const { storage, epoch } = await owned();
    let n = 0;
    const capture = vi.fn(async () => snapshot(++n));
    const saver = new SnapshotSaver(storage, epoch, capture, undefined, 100);

    saver.schedule();
    await vi.advanceTimersByTimeAsync(50);
    saver.schedule();
    await vi.advanceTimersByTimeAsync(50);
    saver.schedule();
    expect(capture).not.toHaveBeenCalled();

    await vi.advanceTimersByTimeAsync(100);
    await saver.flush();
    expect(capture).toHaveBeenCalledTimes(1);
    expect(storage.current).toEqual(snapshot(1));

    saver.schedule();
    await vi.advanceTimersByTimeAsync(100);
    await saver.flush();
    expect(storage.current).toEqual(snapshot(2));
  });

  it('runs saves one after the other', async () => {
    const { storage, epoch } = await owned();
    let release!: () => void;
    const order: string[] = [];
    const capture = vi
      .fn<() => Promise<DemoSnapshot>>()
      .mockImplementationOnce(async () => {
        order.push('first capture');
        await new Promise<void>((resolve) => (release = resolve));
        return snapshot(1);
      })
      .mockImplementationOnce(async () => {
        order.push('second capture');
        return snapshot(2);
      });
    const saver = new SnapshotSaver(storage, epoch, capture, undefined, 10);

    saver.schedule();
    await vi.advanceTimersByTimeAsync(10);
    saver.schedule();
    await vi.advanceTimersByTimeAsync(10);
    expect(order).toEqual(['first capture']);

    release();
    await saver.flush();
    expect(order).toEqual(['first capture', 'second capture']);
    expect(storage.current).toEqual(snapshot(2));
  });

  it('keeps going after a failed save and reports only the first failure', async () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => undefined);
    const { storage, epoch } = await owned();
    const capture = vi
      .fn<() => Promise<DemoSnapshot>>()
      .mockRejectedValueOnce(new Error('quota'))
      .mockRejectedValueOnce(new Error('quota'))
      .mockResolvedValue(snapshot(3));
    const onError = vi.fn();
    const saver = new SnapshotSaver(storage, epoch, capture, onError, 10);

    for (let i = 0; i < 3; i++) {
      saver.schedule();
      await saver.flush();
    }
    expect(warn).toHaveBeenCalledTimes(2);
    expect(onError).toHaveBeenCalledTimes(1);
    expect(storage.current).toEqual(snapshot(3));
    warn.mockRestore();
  });

  it('flush saves a scheduled change at once and waits for it', async () => {
    const { storage, epoch } = await owned();
    const saver = new SnapshotSaver(storage, epoch, async () => snapshot(1), undefined, 10_000);

    await saver.flush();
    expect(storage.current).toBeUndefined();

    saver.schedule();
    await saver.flush();
    expect(storage.current).toEqual(snapshot(1));

    // The flushed change is not saved a second time.
    const writes = storage.writes;
    await vi.advanceTimersByTimeAsync(10_000);
    expect(storage.writes).toBe(writes);
  });

  it('stops when the Go program exits: no saves, no waiting, no error', async () => {
    const { storage, epoch } = await owned();
    const onError = vi.fn();
    let fail!: (err: Error) => void;
    // Go exited while encoding: the snapshot never comes, or fails.
    const capture = vi.fn(() => new Promise<DemoSnapshot>((_, reject) => (fail = reject)));
    const saver = new SnapshotSaver(storage, epoch, capture, onError, 10);

    saver.schedule();
    await vi.advanceTimersByTimeAsync(10);
    saver.schedule();
    saver.stop();

    await expect(saver.flush()).resolves.toBeUndefined();
    fail(new Error('Go program has already exited'));
    await vi.advanceTimersByTimeAsync(100);
    expect(capture).toHaveBeenCalledTimes(1);
    expect(onError).not.toHaveBeenCalled();
    expect(storage.current).toBeUndefined();
  });
});

describe('resetDemoData', () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  function steps(clear: () => Promise<void>) {
    const calls: string[] = [];
    return {
      calls,
      steps: {
        stop: () => calls.push('stop'),
        clear: async () => {
          calls.push('clear');
          await clear();
        },
        markFailed: () => calls.push('failed'),
        reload: () => calls.push('reload'),
      },
    };
  }

  it('stops the worker, deletes the snapshot and reloads', async () => {
    const { calls, steps: s } = steps(async () => undefined);
    await resetDemoData(s);
    expect(calls).toEqual(['stop', 'clear', 'reload']);
  });

  it('reloads anyway, and remembers the failure, when the delete fails or hangs', async () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => undefined);

    const failing = steps(() => Promise.reject(new Error('blocked')));
    await resetDemoData(failing.steps);
    expect(failing.calls).toEqual(['stop', 'clear', 'failed', 'reload']);

    const hanging = steps(() => new Promise(() => undefined));
    const done = resetDemoData(hanging.steps, 1_000);
    await vi.advanceTimersByTimeAsync(999);
    expect(hanging.calls).toEqual(['stop', 'clear']);
    await vi.advanceTimersByTimeAsync(1);
    await done;
    expect(hanging.calls).toEqual(['stop', 'clear', 'failed', 'reload']);
    warn.mockRestore();
  });
});
