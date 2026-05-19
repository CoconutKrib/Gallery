// Client-side router for Gallery.
// Registers page handlers and dispatches based on URL pathname.

window.Gallery = window.Gallery || {};
Gallery.pages = Gallery.pages || {};

Gallery.router = (() => {
  const routes = [
    { pattern: /^\/browse(?:\/(\d+))?(?:\/(.*))?$/, page: 'browse' },
    { pattern: /^\/photo\/([0-9a-f]{64})$/, page: 'photo' },
    { pattern: /^\/search$/, page: 'search' },
    { pattern: /^\/timeline$/, page: 'timeline' },
    { pattern: /^\/settings$/, page: 'settings' },
    { pattern: /^\/$/, page: 'home' },
  ];

  function dispatch(url) {
    const path = new URL(url, location.origin).pathname;
    for (const route of routes) {
      const m = path.match(route.pattern);
      if (m) {
        const handler = Gallery.pages[route.page];
        if (handler) {
          handler(...m.slice(1));
          return;
        }
      }
    }
    // Fallback: show a "not found" / coming-soon message.
    document.getElementById('app').innerHTML =
      `<div class="empty"><div class="empty-icon">🚧</div><p>Coming soon.</p></div>`;
  }

  // Intercept all <a data-link> clicks and internal relative links.
  document.addEventListener('click', e => {
    const a = e.target.closest('a');
    if (!a) return;
    const href = a.getAttribute('href');
    if (!href || href.startsWith('http') || href.startsWith('//') || href.startsWith('#')) return;
    e.preventDefault();
    Gallery.utils.navigate(href);
  });

  // Handle back/forward.
  window.addEventListener('popstate', () => dispatch(location.href));

  // Initial dispatch.
  document.addEventListener('DOMContentLoaded', () => dispatch(location.href));

  return { dispatch };
})();

// Home page: redirect to browse.
Gallery.pages.home = function() {
  Gallery.utils.navigate('/browse');
};
