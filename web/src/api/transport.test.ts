import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { DEMO_SCHOOL_KEY, switchDemoSchool } from '@/features/demo/school';
import type { MainToWorkerMessage, WorkerToMainMessage } from './wasm/protocol';

// The web demo's transport: the worker learns the visitor's demo school, and
// "Reset demo" deletes that school's saved data only.
vi.mock('@/lib/env', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/lib/env')>()),
  transport: 'wasm',
  isDemo: true,
}));

// Switching schools reloads the page, which jsdom cannot do.
vi.mock('@/features/demo/school', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/features/demo/school')>()),
  switchDemoSchool: vi.fn(),
}));

const { storage, clearSnapshot } = vi.hoisted(() => ({
  storage: { transact: vi.fn() },
  clearSnapshot: vi.fn<(...args: unknown[]) => Promise<void>>(async () => undefined),
}));
vi.mock('./wasm/persistence', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./wasm/persistence')>()),
  indexedDbStorage: () => storage,
  clearSnapshot,
}));

/** Stands in for the module worker; records what the page posts. */
class FakeWorker {
  static created: FakeWorker[] = [];
  readonly posted: MainToWorkerMessage[] = [];
  private readonly listeners: ((event: MessageEvent<WorkerToMainMessage>) => void)[] = [];

  constructor() {
    FakeWorker.created.push(this);
  }

  postMessage(message: MainToWorkerMessage): void {
    this.posted.push(message);
  }

  addEventListener(type: string, listener: (event: MessageEvent<WorkerToMainMessage>) => void) {
    if (type === 'message') this.listeners.push(listener);
  }

  /** The worker answers the page. */
  answer(message: WorkerToMainMessage): void {
    for (const listener of this.listeners) listener(new MessageEvent('message', { data: message }));
  }

  terminate(): void {}
}

async function load() {
  vi.resetModules();
  const mod = await import('./transport');
  mod.startBackend();
  await vi.waitFor(() => expect(FakeWorker.created).toHaveLength(1));
  return { mod, worker: FakeWorker.created[0]! };
}

beforeEach(() => {
  FakeWorker.created = [];
  clearSnapshot.mockClear();
  vi.mocked(switchDemoSchool).mockClear();
  vi.stubGlobal('Worker', FakeWorker);
  // jsdom has no lock manager, so this tab runs the demo at once (tabLock.ts);
  // no channel either. The reload after a reset makes jsdom log "Not
  // implemented: navigation".
  vi.stubGlobal('BroadcastChannel', undefined);
});

afterEach(() => {
  localStorage.removeItem(DEMO_SCHOOL_KEY);
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe('the web demo transport', () => {
  it('starts the worker with the Programme school by default', async () => {
    const { mod, worker } = await load();
    expect(worker.posted[0]).toEqual({ type: 'init', school: 'programme' });

    await mod.demoControls!.reset();
    expect(clearSnapshot).toHaveBeenCalledWith(storage, 'programme');
  });

  it("starts the worker with the visitor's school and resets only its data", async () => {
    localStorage.setItem(DEMO_SCHOOL_KEY, 'werkstatt');
    const { mod, worker } = await load();
    expect(worker.posted[0]).toEqual({ type: 'init', school: 'werkstatt' });

    await mod.demoControls!.reset();
    expect(clearSnapshot).toHaveBeenCalledTimes(1);
    expect(clearSnapshot).toHaveBeenCalledWith(storage, 'werkstatt');
  });

  it('resets the school this page runs, even after another tab chose the other one', async () => {
    const { mod } = await load();
    // Another tab switched the school (it stores the choice and reloads itself).
    localStorage.setItem(DEMO_SCHOOL_KEY, 'werkstatt');

    await mod.demoControls!.reset();
    expect(clearSnapshot).toHaveBeenCalledWith(storage, 'programme');
  });

  it('saves pending changes before it switches the school', async () => {
    const { mod, worker } = await load();

    const switched = mod.switchSchool('werkstatt');
    await vi.waitFor(() => expect(worker.posted.some((m) => m.type === 'flush')).toBe(true));
    // The reload waits for the save.
    expect(switchDemoSchool).not.toHaveBeenCalled();

    const flush = worker.posted.find((m) => m.type === 'flush')!;
    worker.answer({ type: 'flushed', id: (flush as { id: number }).id });
    await switched;
    expect(switchDemoSchool).toHaveBeenCalledExactlyOnceWith('werkstatt');
  });
});
