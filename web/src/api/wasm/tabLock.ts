/**
 * One active tab: the demo keeps its data in IndexedDB, which all tabs of the
 * origin share, so only one tab at a time may run the backend and save. That
 * tab holds a Web Lock. Another tab shows a notice instead; "Use here" takes
 * the lock over:
 *
 *  1. the new tab broadcasts `takeover` and queues for the lock,
 *  2. the active tab saves what is pending, stops its worker, releases the
 *     lock and shows the notice itself (`onHandOver`, then `onLost`),
 *  3. the new tab gets the lock and starts the backend from the saved data.
 *
 * If the active tab does not answer in time (frozen in the background), the
 * new tab steals the lock; the old tab sees that as `onLost` when it runs
 * again.
 */

/** The parts of `navigator.locks` used here. */
export interface LockManagerLike {
  request(
    name: string,
    options: LockOptions,
    callback: (lock: Lock | null) => unknown,
  ): Promise<unknown>;
}

/** The parts of `BroadcastChannel` used here. */
export interface ChannelLike {
  postMessage(message: unknown): void;
  addEventListener(type: 'message', listener: (event: MessageEvent) => void): void;
}

export interface TabLockOptions {
  /** `navigator.locks`; without it every tab runs the demo. */
  locks?: LockManagerLike;
  channel?: ChannelLike;
  name?: string;
  /** How long a takeover waits for the active tab to hand over. */
  waitMs?: number;
  /**
   * How long a start waits for the lock before it shows "open in another
   * tab": on a reload the old page may not have released it yet.
   */
  startWaitMs?: number;
  /** Called on the active tab before it gives the lock to another tab. */
  onHandOver: () => Promise<void>;
  /** Called when this tab is no longer the active one. */
  onLost: () => void;
}

export const LOCK_NAME = 'studio-schedule-demo';

/** How long a takeover waits before it takes the lock by force. */
export const TAKEOVER_WAIT_MS = 5_000;

const TAKEOVER = 'takeover';

export class TabLock {
  private release: (() => void) | null = null;
  private active = false;
  private readonly options: TabLockOptions;

  constructor(options: TabLockOptions) {
    this.options = options;
    options.channel?.addEventListener('message', (event) => {
      const data = event.data as { type?: string } | null;
      if (data?.type === TAKEOVER && this.release) void this.handOver();
    });
  }

  /**
   * Makes this tab the active one if no other tab is, or, with `takeOver`,
   * in any case. Resolves to whether this tab is active.
   */
  async acquire(takeOver: boolean): Promise<boolean> {
    if (!this.options.locks) return true;
    if (!takeOver) return this.wait(this.options.startWaitMs ?? 1_500);

    this.options.channel?.postMessage({ type: TAKEOVER });
    // Else the active tab did not hand over in time.
    return (
      (await this.wait(this.options.waitMs ?? TAKEOVER_WAIT_MS)) || this.request({ steal: true })
    );
  }

  /** Waits up to `ms` for the lock; resolves to whether this tab got it. */
  private async wait(ms: number): Promise<boolean> {
    const abort = new AbortController();
    const timer = setTimeout(() => abort.abort(), ms);
    try {
      return await this.request({ signal: abort.signal });
    } catch {
      return false;
    } finally {
      clearTimeout(timer);
    }
  }

  private request(options: LockOptions): Promise<boolean> {
    const { locks, name = LOCK_NAME } = this.options;
    return new Promise<boolean>((resolve, reject) => {
      let granted = false;
      locks!
        .request(name, options, (lock) => {
          if (!lock) {
            resolve(false);
            return undefined;
          }
          granted = true;
          this.active = true;
          resolve(true);
          // Held until released (hand-over) or stolen.
          return new Promise<void>((release) => {
            this.release = release;
          });
        })
        .catch((err: unknown) => {
          if (!granted) {
            reject(err); // gave up waiting
            return;
          }
          // Another tab stole the lock (possibly while this one handed over).
          this.release = null;
          this.lose();
        });
    });
  }

  private async handOver(): Promise<void> {
    const release = this.release;
    this.release = null;
    try {
      await this.options.onHandOver();
    } finally {
      release?.();
      this.lose();
    }
  }

  private lose(): void {
    if (!this.active) return;
    this.active = false;
    this.options.onLost();
  }
}
