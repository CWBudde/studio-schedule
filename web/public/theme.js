// Follow the OS colour scheme before first paint to avoid a flash.
(function () {
  var mq = window.matchMedia('(prefers-color-scheme: dark)');
  var apply = function () {
    document.documentElement.classList.toggle('dark', mq.matches);
  };
  apply();
  mq.addEventListener('change', apply);
})();
