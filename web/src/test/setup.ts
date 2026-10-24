import '@testing-library/jest-dom/vitest';
import { cleanup } from '@testing-library/react';
import { afterEach } from 'vitest';
import i18n from '@/i18n';

// Taken before any test can install fake timers.
const realSetTimeout = globalThis.setTimeout;

afterEach(async () => {
  cleanup();
  // Radix's FocusScope (dialogs, sheets, popovers) restores the focus in a
  // setTimeout(0) after it unmounts. Let that timer fire now, while the jsdom
  // environment is still in place: after a file's last test vitest may tear
  // the environment down first, and the late timer then builds its
  // CustomEvent from Node's global, which jsdom rejects as an unhandled error.
  // Node runs timers of the same delay in order, so this one comes after it.
  await new Promise((resolve) => realSetTimeout(resolve, 0));
  await i18n.changeLanguage('en');
});

// jsdom lacks a few APIs that Radix UI primitives use.
if (!('ResizeObserver' in globalThis)) {
  globalThis.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  };
}
Element.prototype.hasPointerCapture ??= () => false;
Element.prototype.releasePointerCapture ??= () => undefined;
Element.prototype.scrollIntoView ??= () => undefined;
// jsdom does not implement scrolling; the router scrolls on navigation.
window.scrollTo = () => undefined;
// jsdom lacks matchMedia, which the toaster uses for its theme.
window.matchMedia ??= (query: string) =>
  ({
    matches: false,
    media: query,
    onchange: null,
    addEventListener: () => undefined,
    removeEventListener: () => undefined,
    addListener: () => undefined,
    removeListener: () => undefined,
    dispatchEvent: () => false,
  }) as MediaQueryList;
