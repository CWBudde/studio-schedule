// Whether this browser has opened the web demo before: until then, the demo's
// home page is its about page (docs/demo-mode.md). Storage can be unavailable
// (private windows, blocked site data); the about page then shows on every
// visit, but "Open the demo" still works for this page load.
const enteredKey = 'studio-schedule-demo-entered';

let entered = false;

export function hasEnteredDemo(): boolean {
  if (entered) return true;
  try {
    return localStorage.getItem(enteredKey) !== null;
  } catch {
    return false;
  }
}

export function markDemoEntered(): void {
  entered = true;
  try {
    localStorage.setItem(enteredKey, '1');
  } catch {
    // The about page then shows again on the next visit.
  }
}
