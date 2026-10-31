// Follow the OS colour scheme before first paint to avoid a flash.
(function () {
  var mq = window.matchMedia('(prefers-color-scheme: dark)');
  var apply = function () {
    document.documentElement.classList.toggle('dark', mq.matches);
  };
  apply();
  mq.addEventListener('change', apply);
})();

// The school's theme of the last visit (src/themes/themeCache.ts), before first
// paint too; the app replaces it with the school's current one. The demo's
// about page keeps its own look.
(function () {
  if (location.hash.indexOf('#/about') === 0) return;
  try {
    var theme = localStorage.getItem('studio-schedule-theme');
    if (theme === 'programme' || theme === 'werkstatt') {
      document.documentElement.setAttribute('data-theme', theme);
    }
  } catch (e) {
    // No storage (a private window): the app sets the theme once it starts.
  }
})();
