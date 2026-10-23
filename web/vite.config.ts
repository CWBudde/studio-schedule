import path from 'node:path';
import tailwindcss from '@tailwindcss/vite';
import { tanstackRouter } from '@tanstack/router-plugin/vite';
import react from '@vitejs/plugin-react';
import { defineConfig } from 'vitest/config';

// https://vite.dev/config/
export default defineConfig({
  // GitHub Pages serves the demo from a sub path, e.g. VITE_BASE=/studio-schedule/
  base: process.env.VITE_BASE ?? '/',
  plugins: [
    // Must run before the React plugin.
    tanstackRouter({ target: 'react', autoCodeSplitting: true }),
    react(),
    tailwindcss(),
  ],
  resolve: {
    alias: {
      '@': path.resolve(import.meta.dirname, './src'),
    },
  },
  server: {
    proxy: {
      '/api': 'http://localhost:8080',
    },
  },
  build: {
    rolldownOptions: {
      output: {
        // Split rarely-changing vendor code from app code for better caching.
        codeSplitting: {
          groups: [
            {
              name: 'react',
              test: /node_modules[\\/](react|react-dom|scheduler)[\\/]/,
              priority: 20,
            },
            { name: 'vendor', test: /node_modules[\\/]/, priority: 10 },
          ],
        },
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
});
