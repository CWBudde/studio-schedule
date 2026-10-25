import { createHash } from 'node:crypto';
import { readFileSync } from 'node:fs';
import path from 'node:path';
import tailwindcss from '@tailwindcss/vite';
import { tanstackRouter } from '@tanstack/router-plugin/vite';
import react from '@vitejs/plugin-react';
import type { Plugin } from 'vite';
import { VitePWA } from 'vite-plugin-pwa';
import { defineConfig } from 'vitest/config';
import { vendorChunks } from './vite.chunks.ts';

const chunks = vendorChunks();

/**
 * The version of the offline cache (ADR 0017): the API's answers change
 * with its schema, so a saved cache of another schema is dropped.
 */
const offlineCacheVersion = createHash('sha256')
  .update(readFileSync(path.resolve(import.meta.dirname, 'src/api/schema.d.ts')))
  .digest('hex')
  .slice(0, 16);

/** The app's own accent and the light theme's background (as the server's manifest has them). */
const themeColor = '#7f1f35';
const backgroundColor = '#faf7f2';

/**
 * Links the server's manifest (`GET /manifest.webmanifest`, built from the
 * school's settings) into index.html. The demo has a static one instead,
 * which the PWA plugin generates and links itself.
 */
function serverManifestLink(base: string): Plugin {
  return {
    name: 'studio-schedule:server-manifest-link',
    transformIndexHtml: () => [
      {
        tag: 'link',
        attrs: { rel: 'manifest', href: `${base}manifest.webmanifest` },
        injectTo: 'head',
      },
    ],
  };
}

// https://vite.dev/config/
export default defineConfig(({ mode }) => {
  // GitHub Pages serves the demo from a sub path, e.g. VITE_BASE=/studio-schedule/
  const base = process.env.VITE_BASE ?? '/';
  const demo = mode === 'demo';

  return {
    base,
    define: {
      'import.meta.env.VITE_OFFLINE_CACHE_VERSION': JSON.stringify(offlineCacheVersion),
    },
    plugins: [
      // Must run before the React plugin.
      tanstackRouter({ target: 'react', autoCodeSplitting: true }),
      react(),
      tailwindcss(),
      chunks.guard,
      // The service worker (ADR 0017): our own src/sw.ts with Workbox's
      // precaching; src/pwa.ts registers it and offers updates.
      VitePWA({
        strategies: 'injectManifest',
        srcDir: 'src',
        filename: 'sw.ts',
        registerType: 'prompt',
        injectRegister: false,
        manifest: demo
          ? {
              id: base,
              name: 'Studio Schedule (demo)',
              short_name: 'Studio demo',
              description: 'Class schedule and booking (fictional demo data)',
              lang: 'en',
              start_url: base,
              scope: base,
              display: 'standalone',
              theme_color: themeColor,
              background_color: backgroundColor,
              icons: [
                { src: 'icons/icon-192.png', sizes: '192x192', type: 'image/png' },
                { src: 'icons/icon-512.png', sizes: '512x512', type: 'image/png' },
                {
                  src: 'icons/icon-maskable-512.png',
                  sizes: '512x512',
                  type: 'image/png',
                  purpose: 'maskable',
                },
              ],
            }
          : false,
        includeManifestIcons: false,
        injectManifest: {
          // The app shell; never the demo's backend (app.wasm, receipt.wasm,
          // wasm_exec.js), which is large and loads from the network.
          globPatterns: ['**/*.{js,css,html,svg,png,woff2}', 'theme.js'],
          globIgnores: ['**/*.wasm', 'wasm_exec.js'],
        },
        devOptions: { enabled: false },
      }),
      ...(demo ? [] : [serverManifestLink(base)]),
    ],
    resolve: {
      alias: {
        '@': path.resolve(import.meta.dirname, './src'),
      },
    },
    server: {
      proxy: {
        '/api': 'http://localhost:8080',
        // Calendar feeds (ADR 0011) live outside /api.
        '/ical': 'http://localhost:8080',
        // The manifest is built from the school's settings (ADR 0017).
        '/manifest.webmanifest': 'http://localhost:8080',
      },
    },
    build: {
      rolldownOptions: {
        output: {
          // Split rarely-changing vendor code from app code for better caching,
          // and the chart library from what the entry loads (vite.chunks.ts).
          codeSplitting: { groups: chunks.groups },
        },
      },
    },
    worker: {
      // The WASM bridge worker is a module worker.
      format: 'es',
    },
    test: {
      environment: 'jsdom',
      globals: true,
      // e2e/ holds the Playwright specs (`pnpm e2e`).
      include: ['src/**/*.test.{ts,tsx}'],
      setupFiles: ['./src/test/setup.ts'],
      css: false,
      // The browser's time zone must never leak into school dates and times: run
      // the tests far away from the school (Europe/Berlin in the fixtures).
      env: { TZ: 'America/New_York' },
      // Keep resource usage modest.
      maxWorkers: 2,
    },
  };
});
