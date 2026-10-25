import type { Plugin } from 'vite';

/**
 * The vendor chunks of the build. The chart library (recharts) and every
 * module that only it uses go to the `charts` chunk, which only the
 * statistics page loads; the rest of node_modules goes to `react` and
 * `vendor`. "Only it uses" is decided on the module graph, not on a list of
 * package names, so a new dependency of recharts follows it on its own.
 */

/** The modules of the chart library itself. */
const chartLibrary = /node_modules[\\/]recharts[\\/]/;
const nodeModules = /node_modules[\\/]/;

/** What the chunking sees of a module. */
interface GraphModule {
  importers: readonly string[];
  dynamicImporters: readonly string[];
}

type ModuleGraph = (id: string) => GraphModule | null;

/**
 * The modules of the chart library and those only it uses, among `ids` and
 * everything that imports them: the largest set of node_modules in which
 * every module but the library's own has importers, all of them in the
 * set. App code is never in the set, so what the app imports (React, clsx,
 * …) is not either. An importer that the bundle leaves out (a recharts
 * chart the app never imports) still counts, as part of the library.
 */
export function chartsOnly(ids: Iterable<string>, graph: ModuleGraph): Set<string> {
  const importersOf = (id: string) => {
    const m = graph(id);
    return m ? [...m.importers, ...m.dynamicImporters] : [];
  };

  const set = new Set<string>();
  const queue = [...ids];
  for (let id = queue.pop(); id !== undefined; id = queue.pop()) {
    if (set.has(id) || !nodeModules.test(id)) continue;
    set.add(id);
    queue.push(...importersOf(id));
  }

  for (let changed = true; changed;) {
    changed = false;
    for (const id of set) {
      if (chartLibrary.test(id)) continue;
      const importers = importersOf(id);
      if (importers.length === 0 || importers.some((i) => !set.has(i))) {
        set.delete(id);
        changed = true;
      }
    }
  }
  return set;
}

/**
 * The code-splitting groups and a plugin that fails the build if the
 * entry's chunks (the entry and what it imports statically) hold any
 * module of the `charts` chunk.
 */
export function vendorChunks() {
  // The node_modules the `vendor` group's test saw: Rolldown calls every
  // test of a group before the group's first name().
  const seen = new Set<string>();
  let charts: Set<string> | undefined;
  const chartsOf = (graph: ModuleGraph) => (charts ??= chartsOnly(seen, graph));

  const groups = [
    {
      name: 'react',
      test: /node_modules[\\/](react|react-dom|scheduler)[\\/]/,
      priority: 20,
    },
    // Before `charts`: a group takes its modules' dependencies along, so the
    // modules the charts share with the app must be taken first.
    {
      name: (id: string, ctx: { getModuleInfo: ModuleGraph }) =>
        chartsOf((m) => ctx.getModuleInfo(m)).has(id) ? null : 'vendor',
      debugName: 'vendor',
      test: (id: string) => {
        if (!nodeModules.test(id)) return false;
        seen.add(id);
        charts = undefined;
        return true;
      },
      priority: 15,
    },
    {
      name: (id: string, ctx: { getModuleInfo: ModuleGraph }) =>
        chartsOf((m) => ctx.getModuleInfo(m)).has(id) ? 'charts' : null,
      debugName: 'charts',
      test: nodeModules,
      priority: 10,
    },
  ];

  const guard: Plugin = {
    name: 'studio-schedule:charts-chunk-guard',
    apply: 'build',
    generateBundle(_options, bundle) {
      const exclusive = chartsOnly(this.getModuleIds(), (id) => this.getModuleInfo(id));
      const chunks = new Map(
        Object.values(bundle).flatMap((c) => (c.type === 'chunk' ? [[c.fileName, c]] : [])),
      );
      const initial = new Set<string>();
      const queue = [...chunks.values()].filter((c) => c.isEntry).map((c) => c.fileName);
      for (let name = queue.pop(); name !== undefined; name = queue.pop()) {
        if (initial.has(name)) continue;
        initial.add(name);
        queue.push(...(chunks.get(name)?.imports ?? []));
      }
      for (const name of initial) {
        const leaked = chunks.get(name)?.moduleIds.filter((id) => exclusive.has(id)) ?? [];
        if (leaked.length > 0) {
          this.error(
            `${name} is loaded with the entry but holds chart modules: ${leaked.slice(0, 5).join(', ')}`,
          );
        }
      }
    },
  };

  return { groups, guard };
}
