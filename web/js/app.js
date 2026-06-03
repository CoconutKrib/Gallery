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
    { pattern: /^\/map$/, page: 'map' },
    { pattern: /^\/events(?:\/(\d+))?$/, page: 'events' },
    { pattern: /^\/dedup$/, page: 'dedup' },
    { pattern: /^\/staging$/, page: 'staging' },
    { pattern: /^\/library$/, page: 'library' },
    { pattern: /^\/people(?:\/(\d+))?$/, page: 'people' },
    { pattern: /^\/faces\/review$/, page: 'people', action: 'review' },
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
          handler(...m.slice(1), { action: route.action || null });
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

  // Initial dispatch and settings bootstrap.
  document.addEventListener('DOMContentLoaded', async () => {
    // Load settings once to know if internal library is enabled.
    try {
      const s = await Gallery.utils.api('/api/settings');
      Gallery.settings = s;
      if (s.internal_library && s.internal_library.enabled) {
        document.body.classList.add('library-enabled');
      }
    } catch (e) {
      Gallery.settings = {};
    }
    // Fetch recognition status (non-blocking; used by people.js and library.js).
    Gallery.utils.api('/api/recognition/status').then(status => {
      Gallery.recognitionStatus = status;
      if (status && status.enabled && status.available) {
        const link = document.getElementById('nav-faces-review');
        if (link) link.style.display = '';
      }
    }).catch(() => {
      Gallery.recognitionStatus = { enabled: false, available: false };
    });
    dispatch(location.href);
  });

  return { dispatch };
})();

// Home page: redirect to browse.
Gallery.pages.home = function() {
  Gallery.utils.navigate('/browse');
};
