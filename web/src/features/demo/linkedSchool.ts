import { isDemo } from '@/lib/env';
import { markDemoEntered } from './entered';
import { chooseDemoSchool, takeDemoParam } from './school';

// A demo link with `?demo=<school>` (the README's, or the reload of
// switchDemoSchool) opens that school directly: it counts as the visitor's
// choice and skips the about page. main.tsx imports this module before the
// router, whose hash history removes the page's query string when it starts
// (lib/hashHistory.ts).
if (isDemo) {
  const linked = takeDemoParam(new URL(location.href));
  if (linked) {
    chooseDemoSchool(linked);
    markDemoEntered();
  }
}
