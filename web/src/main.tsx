import './zodConfig';
import { PersistQueryClientProvider } from '@tanstack/react-query-persist-client';
import { RouterProvider } from '@tanstack/react-router';
import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { startBackend } from '@/api/transport';
import { hasEnteredDemo } from '@/features/demo/entered';
import { indexedDbPersister, offlineCacheOptions, refreshRestored } from '@/lib/offlineCache';
import { registerServiceWorker } from '@/pwa';
import { queryClient, router } from '@/router';
import './i18n';
import './index.css';

// In the demo build this boots the Go/WASM backend in a Web Worker at once
// for a returning visitor; on a first visit the app's layout starts it, so
// the about page loads none of it.
if (hasEnteredDemo()) startBackend();
// The service worker: the app offline and its updates (ADR 0017).
registerServiceWorker();

const root = document.getElementById('root');
if (!root) throw new Error('Root element #root not found');

createRoot(root).render(
  <StrictMode>
    {/* The last schedule and My lessons survive a reload, for offline use (ADR 0017). */}
    <PersistQueryClientProvider
      client={queryClient}
      persistOptions={offlineCacheOptions(
        indexedDbPersister(),
        import.meta.env.VITE_OFFLINE_CACHE_VERSION,
      )}
      onSuccess={() => refreshRestored(queryClient)}
    >
      <RouterProvider router={router} />
    </PersistQueryClientProvider>
  </StrictMode>,
);
