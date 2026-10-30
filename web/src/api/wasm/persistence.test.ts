import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { DemoSchool } from '@/features/demo/school';
import {
  claim,
  claimOrGiveUp,
  clearSnapshot,
  resetDemoData,
  SNAPSHOT_FORMAT,
  snapshotKey,
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

    const first = await claim(storage, 'programme');
    expect(first.snapshot).toBeNull();
    let n = 0;
    const old = new SnapshotSaver(
      storage,
      'programme',
      first.epoch,
      async () => snapshot(++n),
      onError,
      10,
    );
    old.schedule();
    await old.flush();
    expect(storage.current).toEqual(snapshot(1));

    // Another tab takes over: it claims and reads the latest save.
    const second = await claim(storage, 'programme');
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

    const current = new SnapshotSaver(
      storage,
      'programme',
      second.epoch,
      async () => snapshot(9),
      onError,
      10,
    );
    current.schedule();
    await current.flush();
    expect(storage.current).toEqual(snapshot(9));
  });

  it('refuses a save that was under way when another tab claimed the data', async () => {
    const storage = new MemoryStorage();
    const first = await claim(storage, 'programme');
    let release!: () => void;
    const old = new SnapshotSaver(
      storage,
      'programme',
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

    await claim(storage, 'programme');
    release();
    await old.flush();
    expect(storage.current).toBeUndefined();
  });

  it('keeps the owner when the snapshot is cleared', async () => {
    const storage = new MemoryStorage();
    const first = await claim(storage, 'programme');
    const saver = new SnapshotSaver(
      storage,
      'programme',
      first.epoch,
      async () => snapshot(1),
      undefined,
      10,
    );
    saver.schedule();
    await saver.flush();

    await clearSnapshot(storage, 'programme');
    expect(storage.current).toBeUndefined();
    expect((await claim(storage, 'programme')).epoch).toBe(first.epoch + 1);
  });

  it('gives up claiming when IndexedDB does not answer', async () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => undefined);
    const hanging: SnapshotStorage = { transact: () => new Promise(() => undefined) };
    let result: unknown = 'pending';
    const claiming = claimOrGiveUp(hanging, 'programme', 3_000).then((r) => (result = r));
    await vi.advanceTimersByTimeAsync(2_999);
    expect(result).toBe('pending');
    await vi.advanceTimersByTimeAsync(1);
    await claiming;
    expect(result).toBeNull();

    const failing: SnapshotStorage = { transact: () => Promise.reject(new Error('blocked')) };
    expect(await claimOrGiveUp(failing, 'programme')).toBeNull();
    expect(warn).toHaveBeenCalled();
    warn.mockRestore();
  });
});

describe('one record per demo school', () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  async function saveAs(storage: MemoryStorage, school: DemoSchool, n: number) {
    const { epoch } = await claim(storage, school);
    const saver = new SnapshotSaver(storage, school, epoch, async () => snapshot(n), undefined, 10);
    saver.schedule();
    await saver.flush();
    return saver;
  }

  it('keeps Programme in `current` and Werkstatt in `current-werkstatt`', async () => {
    expect(snapshotKey('programme')).toBe('current');
    expect(snapshotKey('werkstatt')).toBe('current-werkstatt');

    const storage = new MemoryStorage();
    await saveAs(storage, 'programme', 1);
    await saveAs(storage, 'werkstatt', 2);
    expect(storage.data.get('current')).toEqual(snapshot(1));
    expect(storage.data.get('current-werkstatt')).toEqual(snapshot(2));
  });

  it("saves of one school leave the other's record alone", async () => {
    const storage = new MemoryStorage();
    await saveAs(storage, 'programme', 1);

    const werkstatt = await claim(storage, 'werkstatt');
    expect(werkstatt.snapshot).toBeNull();
    const saver = new SnapshotSaver(
      storage,
      'werkstatt',
      werkstatt.epoch,
      async () => snapshot(2),
      undefined,
      10,
    );
    saver.schedule();
    await saver.flush();

    expect(storage.current).toEqual(snapshot(1));
    expect((await claim(storage, 'programme')).snapshot).toEqual(snapshot(1));
    expect((await claim(storage, 'werkstatt')).snapshot).toEqual(snapshot(2));
  });

  it('shares one owner epoch: a claim for one school fences the saves of the other', async () => {
    const storage = new MemoryStorage();
    const programme = await saveAs(storage, 'programme', 1);

    // One worker per origin: the next one (here of the other school) takes over.
    await claim(storage, 'werkstatt');
    programme.schedule();
    await programme.flush();
    expect(storage.current).toEqual(snapshot(1));
    expect(storage.data.get('owner')).toBe(2);
  });

  it("reset deletes only the current school's record", async () => {
    const storage = new MemoryStorage();
    await saveAs(storage, 'programme', 1);
    await saveAs(storage, 'werkstatt', 2);

    await clearSnapshot(storage, 'werkstatt');
    expect(storage.data.has('current-werkstatt')).toBe(false);
    expect(storage.current).toEqual(snapshot(1));

    await saveAs(storage, 'werkstatt', 3);
    await clearSnapshot(storage, 'programme');
    expect(storage.current).toBeUndefined();
    expect(storage.data.get('current-werkstatt')).toEqual(snapshot(3));
  });

  it('restores the record of a visit before the second school as Programme', async () => {
    // As saved by the demo before it had two schools.
    const storage = new MemoryStorage();
    const earlier = { ...snapshot(5), cookies: [cookie] };
    storage.data.set('owner', 7);
    storage.data.set('current', earlier);

    expect(await claim(storage, 'werkstatt')).toEqual({ epoch: 8, snapshot: null });
    expect(await claim(storage, 'programme')).toEqual({ epoch: 9, snapshot: earlier });
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
    return { storage, epoch: (await claim(storage, 'programme')).epoch };
  }

  it('saves once after a burst of changes', async () => {
    const { storage, epoch } = await owned();
    let n = 0;
    const capture = vi.fn(async () => snapshot(++n));
    const saver = new SnapshotSaver(storage, 'programme', epoch, capture, undefined, 100);

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
    const saver = new SnapshotSaver(storage, 'programme', epoch, capture, undefined, 10);

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
    const saver = new SnapshotSaver(storage, 'programme', epoch, capture, onError, 10);

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
    const saver = new SnapshotSaver(
      storage,
      'programme',
      epoch,
      async () => snapshot(1),
      undefined,
      10_000,
    );

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
    const saver = new SnapshotSaver(storage, 'programme', epoch, capture, onError, 10);

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
