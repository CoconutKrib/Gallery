// Shared utility functions for the Gallery frontend.
// Exposed as window.Gallery.utils.

window.Gallery = window.Gallery || {};

Gallery.utils = {
  // Fetch JSON from the API. Returns parsed JSON or throws on error.
  async api(path, options) {
    const res = await fetch(path, options);
    if (res.status === 401) {
      window.location.href = '/login';
      throw new Error('unauthorized');
    }
    const data = await res.json();
    if (!res.ok) throw new Error(data.error || 'request failed');
    return data;
  },

  // Format an ISO date string as a human-readable date/time.
  formatDate(iso) {
    if (!iso) return '—';
    const d = new Date(iso);
    if (isNaN(d)) return iso;
    return d.toLocaleDateString(undefined, { year: 'numeric', month: 'short', day: 'numeric' })
      + ' ' + d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' });
  },

  // Format a decimal degrees coordinate.
  formatCoord(val, posLabel, negLabel) {
    if (val == null) return null;
    const dir = val >= 0 ? posLabel : negLabel;
    return Math.abs(val).toFixed(6) + '° ' + dir;
  },

  // Escape HTML entities.
  esc(str) {
    if (str == null) return '';
    return String(str)
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;');
  },

  // Navigate to a URL using the History API.
  navigate(url) {
    history.pushState(null, '', url);
    Gallery.router.dispatch(url);
  },

  // Set the active nav link.
  setActiveNav(prefix) {
    document.querySelectorAll('.nav-links a').forEach(a => {
      a.classList.toggle('active', a.getAttribute('href') === prefix || a.getAttribute('href').startsWith(prefix + '/'));
    });
  },
};
