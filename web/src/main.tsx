import './zodConfig';
import { PersistQueryClientProvider } from '@tanstack/react-query-persist-client';
import { RouterProvider } from '@tanstack/react-router';
import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { startBackend } from '@/api/transport';
import { indexedDbPersister, offlineCacheOptions } from '@/lib/offlineCache';
import { registerServiceWorker } from '@/pwa';
import { queryClient, router } from '@/router';
import './i18n';
import './index.css';

// In the demo build this boots the Go/WASM backend in a Web Worker.
startBackend();
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
    >
      <RouterProvider router={router} />
    </PersistQueryClientProvider>
  </StrictMode>,
);
