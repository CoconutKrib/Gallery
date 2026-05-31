// Internal Library page: browse photos that have been copied to the managed library.
// Folder-tree navigation (year → month → event) and full photo grid.
window.Gallery = window.Gallery || {};
Gallery.pages = Gallery.pages || {};

Gallery.pages.library = async function() {
  const app = document.getElementById('app');
  Gallery.utils.setActiveNav('/library');

  // Check if internal library is enabled.
  let settings;
  try {
    settings = await Gallery.utils.api('/api/settings');
  } catch (e) {
    app.innerHTML = `<div class="page-content"><div class="empty"><p>Error loading settings.</p></div></div>`;
    return;
  }
  if (!settings.internal_library || !settings.internal_library.enabled) {
    app.innerHTML = `<div class="page-content">
      <h2 class="page-title">Internal Library</h2>
      <div class="empty">
        <div class="empty-icon">⚙️</div>
        <p>Internal library is not enabled. Configure a library path in <a href="/settings" data-link>Settings</a>.</p>
      </div>
    </div>`;
    return;
  }

  app.innerHTML = `
    <div class="library-layout">
      <aside class="library-sidebar" id="library-sidebar">
        <h3 class="sidebar-title">Library</h3>
        <div id="library-tree" class="loading">Loading…</div>
      </aside>
      <main class="library-main">
        <div id="library-header">
          <h2 class="page-title">All Photos</h2>
        </div>
        <div id="library-grid" class="loading">Loading…</div>
      </main>
    </div>`;

  await loadLibraryTree();
  await loadLibraryPhotos();
};

async function loadLibraryTree() {
  const treeEl = document.getElementById('library-tree');
  if (!treeEl) return;
  try {
    const nodes = await Gallery.utils.api('/api/library/tree');
    if (!nodes.length) {
      treeEl.innerHTML = '<p class="sidebar-empty">No photos yet.</p>';
      return;
    }
    treeEl.className = '';
    treeEl.innerHTML = `
      <ul class="tree-list">
        <li class="tree-item tree-item--active" onclick="loadLibraryPhotos()">All photos</li>
        ${nodes.sort((a, b) => a.year < b.year ? 1 : -1).map(node => `
          <li class="tree-year">
            <span onclick="loadLibraryPhotos('${node.year}')">${Gallery.utils.esc(node.year)}</span>
            <ul>
              ${(node.months || []).sort((a, b) => a.month < b.month ? 1 : -1).map(m => `
                <li class="tree-month">
                  <span onclick="loadLibraryPhotos('${node.year}', '${m.month}')">${Gallery.utils.esc(m.month)}</span>
                  ${m.events && m.events.length ? `<ul>
                    ${m.events.map(ev => `
                      <li class="tree-event" onclick="loadLibraryPhotos('${node.year}', '${m.month}', '${Gallery.utils.esc(ev.slug)}')">
                        ${Gallery.utils.esc(ev.slug)} <span class="tree-count">${ev.count}</span>
                      </li>`).join('')}
                  </ul>` : ''}
                </li>`).join('')}
              ${node.undated ? `
                <li class="tree-month tree-undated">
                  <span onclick="loadLibraryPhotos('_undated')">_undated</span>
                </li>` : ''}
            </ul>
          </li>`).join('')}
      </ul>`;
  } catch (e) {
    treeEl.innerHTML = `<p class="sidebar-empty">Error: ${Gallery.utils.esc(e.message)}</p>`;
  }
}

window.loadLibraryPhotos = async function(year, month, slug) {
  const gridEl = document.getElementById('library-grid');
  const headerEl = document.getElementById('library-header');
  if (!gridEl) return;

  // Update active tree item.
  document.querySelectorAll('.tree-item--active').forEach(el => el.classList.remove('tree-item--active'));

  gridEl.className = 'loading';
  gridEl.textContent = 'Loading…';

  // Build header label.
  let label = 'All Photos';
  if (year === '_undated') label = 'Undated Photos';
  else if (year && month && slug) label = `${year} / ${month} / ${slug}`;
  else if (year && month) label = `${year} / ${month}`;
  else if (year) label = year;
  if (headerEl) headerEl.innerHTML = `<h2 class="page-title">${Gallery.utils.esc(label)}</h2>`;

  let data;
  try {
    data = await Gallery.utils.api('/api/library/photos');
  } catch (e) {
    gridEl.className = '';
    gridEl.innerHTML = `<div class="empty"><p>Error: ${Gallery.utils.esc(e.message)}</p></div>`;
    return;
  }

  // Client-side filter by path prefix.
  let photos = data.photos || [];
  if (year === '_undated') {
    photos = photos.filter(p => p.relative_path.startsWith('_undated/') || p.relative_path === '_undated');
  } else if (year && month && slug) {
    const prefix = `${year}/${month}/${slug}/`;
    photos = photos.filter(p => p.relative_path.startsWith(prefix));
  } else if (year && month) {
    const prefix = `${year}/${month}/`;
    photos = photos.filter(p => p.relative_path.startsWith(prefix));
  } else if (year) {
    const prefix = `${year}/`;
    photos = photos.filter(p => p.relative_path.startsWith(prefix));
  }

  gridEl.className = '';
  if (!photos.length) {
    gridEl.innerHTML = `<div class="empty"><div class="empty-icon">🖼️</div><p>No photos here.</p></div>`;
    return;
  }

  gridEl.innerHTML = `<div class="photo-grid">
    ${photos.map(p => `
      <div class="photo-card" onclick="Gallery.utils.navigate('/photo/${p.photo_sha256}')">
        <img class="photo-card__thumb" src="/api/photos/${p.photo_sha256}/thumbnail" alt="" loading="lazy">
        <div class="photo-card__info">
          <div class="photo-card__filename">${Gallery.utils.esc(p.relative_path.split('/').pop())}</div>
          <div class="photo-card__date">${Gallery.utils.formatDate(p.copied_at)}</div>
          ${p.true_date_unknown ? '<span class="staging-badge staging-badge--undated">undated</span>' : ''}
        </div>
      </div>`).join('')}
  </div>`;
};
