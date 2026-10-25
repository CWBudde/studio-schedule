/**
 * The paths the service worker never answers with the app's index.html
 * (ADR 0017): the API, the calendar feeds, the manifest, and the server's
 * probes and metrics. A navigation to one of them (opened in a tab, or a
 * monitor that follows a browser) goes to the server. Shared by the
 * service worker and its test, so it imports nothing.
 */
export function navigationDenylist(base: string): RegExp[] {
  const b = base.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
  return [
    new RegExp(`^${b}api([/?]|$)`),
    new RegExp(`^${b}ical/`),
    new RegExp(`^${b}manifest\\.webmanifest([?]|$)`),
    new RegExp(`^${b}(healthz|readyz|metrics)([/?]|$)`),
  ];
}

/** Whether the service worker leaves a navigation to `path` (with its query) to the server. */
export function bypassesServiceWorker(base: string, path: string): boolean {
  return navigationDenylist(base).some((re) => re.test(path));
}
