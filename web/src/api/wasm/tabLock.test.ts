import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { type ChannelLike, type LockManagerLike, TabLock } from './tabLock';

/** One exclusive lock with the Web Locks semantics the TabLock relies on. */
class FakeLocks implements LockManagerLike {
  private holder: { reject: (err: unknown) => void } | null = null;
  private readonly queue: (() => void)[] = [];

  /** The holding page goes away (closed or reloaded): its lock is released. */
  unload(): void {
    this.holder = null;
    this.queue.shift()?.();
  }

  request(
    name: string,
    options: LockOptions,
    callback: (lock: Lock | null) => unknown,
  ): Promise<unknown> {
    return new Promise((resolve, reject) => {
      const grant = () => {
        this.holder = { reject };
        Promise.resolve(callback({ name, mode: 'exclusive' } as Lock)).then((value) => {
          if (this.holder?.reject !== reject) return; // stolen meanwhile
          this.holder = null;
          resolve(value);
          this.queue.shift()?.();
        });
      };

      if (!this.holder) return grant();
      if (options.ifAvailable) return resolve(callback(null));
      if (options.steal) {
        this.holder.reject(new DOMException('stolen', 'AbortError'));
        return grant();
      }
      this.queue.push(grant);
      options.signal?.addEventListener('abort', () => {
        const i = this.queue.indexOf(grant);
        if (i >= 0) this.queue.splice(i, 1);
        reject(new DOMException('aborted', 'AbortError'));
      });
    });
  }
}

/** BroadcastChannels of one origin: a post reaches every other channel. */
function channels() {
  const all: ((data: unknown) => void)[] = [];
  return (): ChannelLike => {
    let listener: ((event: MessageEvent) => void) | null = null;
    const deliver = (data: unknown) => listener?.({ data } as MessageEvent);
    all.push(deliver);
    return {
      postMessage: (data) => {
        for (const other of all) if (other !== deliver) setTimeout(() => other(data), 0);
      },
      addEventListener: (_type, l) => {
        listener = l;
      },
    };
  };
}

function tab(
  locks: FakeLocks,
  channel: ChannelLike,
  handOver: () => Promise<void> = async () => undefined,
) {
  const events: string[] = [];
  const lock = new TabLock({
    locks,
    channel,
    waitMs: 1_000,
    onHandOver: async () => {
      events.push('hand over');
      await handOver();
      events.push('handed over');
    },
    onLost: () => events.push('lost'),
  });
  return { lock, events };
}

describe('TabLock', () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it('makes the first tab active and tells the next one', async () => {
    const locks = new FakeLocks();
    const channel = channels();
    const first = tab(locks, channel());
    const second = tab(locks, channel());

    expect(await first.lock.acquire(false)).toBe(true);
    let result: boolean | null = null;
    const starting = second.lock.acquire(false).then((ok) => (result = ok));
    // It waits a moment (a reloading page may still hold the lock), then gives up.
    await vi.advanceTimersByTimeAsync(1_499);
    expect(result).toBeNull();
    await vi.advanceTimersByTimeAsync(1);
    await starting;
    expect(result).toBe(false);
    expect(first.events).toEqual([]);
  });

  it('gets the lock on a reload once the old page lets go of it', async () => {
    const locks = new FakeLocks();
    const channel = channels();
    const old = tab(locks, channel());
    const reloaded = tab(locks, channel());
    await old.lock.acquire(false);

    let result: boolean | null = null;
    const starting = reloaded.lock.acquire(false).then((ok) => (result = ok));
    await vi.advanceTimersByTimeAsync(300);
    locks.unload();
    await starting;
    expect(result).toBe(true);
  });

  it('takes over after the active tab saved and stopped', async () => {
    const locks = new FakeLocks();
    const channel = channels();
    let finishSave!: () => void;
    const first = tab(locks, channel(), () => new Promise<void>((r) => (finishSave = r)));
    const second = tab(locks, channel());
    await first.lock.acquire(false);

    let active = false;
    const takeover = second.lock.acquire(true).then((ok) => (active = ok));
    await vi.advanceTimersByTimeAsync(0);
    expect(first.events).toEqual(['hand over']);
    // Until the active tab has saved, the new one waits.
    await vi.advanceTimersByTimeAsync(500);
    expect(active).toBe(false);

    finishSave();
    await takeover;
    expect(active).toBe(true);
    expect(first.events).toEqual(['hand over', 'handed over', 'lost']);

    // The old tab can take it back the same way.
    const busy = first.lock.acquire(false);
    await vi.advanceTimersByTimeAsync(1_500);
    expect(await busy).toBe(false);
    const back = first.lock.acquire(true);
    await vi.advanceTimersByTimeAsync(0);
    expect(await back).toBe(true);
    expect(second.events).toEqual(['hand over', 'handed over', 'lost']);
  });

  it('steals the lock from a tab that does not answer', async () => {
    const locks = new FakeLocks();
    const channel = channels();
    // A frozen tab: it never finishes handing over.
    const first = tab(locks, channel(), () => new Promise<void>(() => undefined));
    const second = tab(locks, channel());
    await first.lock.acquire(false);

    const takeover = second.lock.acquire(true);
    await vi.advanceTimersByTimeAsync(1_000);
    expect(await takeover).toBe(true);
    // When it runs again, it learns that it lost the lock.
    expect(first.events).toEqual(['hand over', 'lost']);
  });

  it('reports a stolen lock to the tab that held it', async () => {
    const locks = new FakeLocks();
    const silent: ChannelLike = { postMessage: () => undefined, addEventListener: () => undefined };
    const first = tab(locks, silent);
    const second = tab(locks, silent);
    await first.lock.acquire(false);

    const takeover = second.lock.acquire(true);
    await vi.advanceTimersByTimeAsync(1_000);
    expect(await takeover).toBe(true);
    expect(first.events).toEqual(['lost']);
  });

  it('lets every tab run the demo without Web Locks', async () => {
    const lock = new TabLock({ onHandOver: async () => undefined, onLost: () => undefined });
    expect(await lock.acquire(false)).toBe(true);
    expect(await lock.acquire(true)).toBe(true);
  });
});
