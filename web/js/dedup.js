// Dedup report page: per-library summary, cross-path overlap, and subtree analysis.
window.Gallery = window.Gallery || {};
Gallery.pages = Gallery.pages || {};

Gallery.pages.dedup = async function() {
  const app = document.getElementById('app');
  Gallery.utils.setActiveNav('/dedup');

  app.innerHTML = `<div class="page-content">
    <h2 class="page-title">Deduplication Report</h2>

    <section class="dedup-section">
      <h3>Per-Library Summary</h3>
      <div id="dedup-libraries" class="loading">Loading…</div>
    </section>

    <section class="dedup-section">
      <h3>Cross-Library Overlap</h3>
      <div id="dedup-overlaps" class="loading">Loading…</div>
    </section>

    <section class="dedup-section">
      <h3>Subtree Analysis</h3>
      <form class="subtree-form" id="subtree-form" autocomplete="off">
        <input type="text" id="subtree-prefix" placeholder="Enter full directory path, e.g. /photos/2019" class="subtree-input">
        <button type="submit" class="btn-primary">Analyse</button>
      </form>
      <div id="dedup-subtree"></div>
    </section>
  </div>`;

  loadReport();

  document.getElementById('subtree-form').addEventListener('submit', async e => {
    e.preventDefault();
    const prefix = document.getElementById('subtree-prefix').value.trim();
    if (!prefix) return;
    await loadSubtree(prefix);
  });
};

async function loadReport() {
  let data;
  try {
    data = await Gallery.utils.api('/api/dedup/report');
  } catch (err) {
    document.getElementById('dedup-libraries').innerHTML = `<p class="error">Failed to load report.</p>`;
    document.getElementById('dedup-overlaps').innerHTML = '';
    return;
  }

  // Per-library summary table.
  const libEl = document.getElementById('dedup-libraries');
  if (!data.libraries || data.libraries.length === 0) {
    libEl.innerHTML = `<div class="empty"><p>No library data.</p></div>`;
  } else {
    libEl.innerHTML = `<table class="dedup-table">
      <thead><tr>
        <th>Library</th>
        <th>Total photos</th>
        <th>Unique</th>
        <th>Have duplicates elsewhere</th>
      </tr></thead>
      <tbody>
        ${data.libraries.map(l => `<tr>
          <td>${Gallery.utils.esc(l.label || `Library ${l.library_path_id}`)}</td>
          <td>${l.total_photos}</td>
          <td>${l.unique_photos}</td>
          <td>${l.duplicate_photos}</td>
        </tr>`).join('')}
      </tbody>
    </table>`;
  }

  // Cross-path overlap table.
  const ovEl = document.getElementById('dedup-overlaps');
  if (!data.overlaps || data.overlaps.length === 0) {
    ovEl.innerHTML = `<div class="empty"><p>No cross-library duplicates found.</p></div>`;
  } else {
    ovEl.innerHTML = `<table class="dedup-table">
      <thead><tr>
        <th>Library A</th>
        <th>Library B</th>
        <th>Shared photos</th>
      </tr></thead>
      <tbody>
        ${data.overlaps.map(o => `<tr>
          <td>${Gallery.utils.esc(o.label_a || `Library ${o.library_path_id_a}`)}</td>
          <td>${Gallery.utils.esc(o.label_b || `Library ${o.library_path_id_b}`)}</td>
          <td>${o.shared_photo_count}</td>
        </tr>`).join('')}
      </tbody>
    </table>`;
  }
}

async function loadSubtree(prefix) {
  const el = document.getElementById('dedup-subtree');
  el.innerHTML = `<div class="loading">Analysing…</div>`;

  let data;
  try {
    data = await Gallery.utils.api(`/api/dedup/subtree?prefix=${encodeURIComponent(prefix)}`);
  } catch (err) {
    el.innerHTML = `<p class="error">${Gallery.utils.esc(err.message)}</p>`;
    return;
  }

  if (data.total === 0) {
    el.innerHTML = `<div class="empty"><p>No photos found under <code>${Gallery.utils.esc(prefix)}</code>.</p></div>`;
    return;
  }

  const rows = data.entries.map(e => {
    const dupeInfo = e.duplicate_count > 0
      ? `<span class="badge badge--warn">${e.duplicate_count} dupe${e.duplicate_count !== 1 ? 's' : ''}</span>`
      : `<span class="badge badge--ok">unique</span>`;
    const otherPaths = e.other_paths.length
      ? `<div class="subtree-dupes">${e.other_paths.map(p => `<span class="dupe-path">${Gallery.utils.esc(p)}</span>`).join('')}</div>`
      : '';
    return `<tr>
      <td><a href="/photo/${Gallery.utils.esc(e.sha256)}" onclick="Gallery.utils.navigate('/photo/${Gallery.utils.esc(e.sha256)}');return false;">${Gallery.utils.esc(e.filepath.replace(prefix, '').replace(/^\//, ''))}</a></td>
      <td>${dupeInfo}</td>
      <td>${otherPaths}</td>
    </tr>`;
  }).join('');

  el.innerHTML = `
    <div class="subtree-summary">
      <strong>${data.total}</strong> photos &nbsp;·&nbsp;
      <strong>${data.unique}</strong> unique &nbsp;·&nbsp;
      <strong>${data.with_dupes}</strong> have duplicates
    </div>
    <table class="dedup-table dedup-table--subtree">
      <thead><tr><th>Path</th><th>Status</th><th>Duplicate locations</th></tr></thead>
      <tbody>${rows}</tbody>
    </table>`;
}
