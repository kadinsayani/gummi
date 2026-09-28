// theme-boot.js: a classic (non-module) script in <head> that applies the
// saved theme before the first paint, so a dark-theme reader never sees a
// light flash. theme.js owns the toggle; this only reads what it saved.
(function () {
  try {
    var t = localStorage.getItem('gummi-web:theme')
    if (t === 'dark' || t === 'light') document.documentElement.dataset.theme = t
  } catch (e) { /* storage blocked: follow the system */ }
})()
