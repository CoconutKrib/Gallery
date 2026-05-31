// Browse page: folder tree + photo grid.
window.Gallery = window.Gallery || {};
Gallery.pages = Gallery.pages || {};

Gallery.pages.browse = async function(libraryId, relPath) {
  const app = document.getElementById('app');
  Gallery.utils.setActiveNav('/browse');

  // If no library selected, show library list.
  if (!libraryId) {
    app.innerHTML = `<div class="browse-layout">
      <nav class="browse-sidebar" id="sidebar"></nav>
      <div class="browse-main" id="browse-main">
        <p class="section-title">Libraries</p>
        <div id="lib-list" class="loading">Loading…</div>
      </div>
    </div>`;
    await loadLibraryList();
    return;
  }

  app.innerHTML = `<div class="browse-layout">
    <nav class="browse-sidebar" id="sidebar"><div class="loading">Loading…</div></nav>
    <div class="browse-main" id="browse-main"><div class="loading">Loading…</div></div>
  </div>`;

  const cleanPath = (relPath || '').replace(/^\//, '');
  const apiPath = cleanPath
    ? `/api/browse/${libraryId}/${cleanPath}`
    : `/api/browse/${libraryId}`;

  let data;
  try {
    data = await Gallery.utils.api(apiPath);
  } catch (e) {
    document.getElementById('browse-main').innerHTML =
      `<div class="empty"><p>Error loading directory: ${Gallery.utils.esc(e.message)}</p></div>`;
    return;
  }

  renderSidebar(data);
  renderMain(data, libraryId);
};

async function loadLibraryList() {
  const el = document.getElementById('lib-list');
  try {
    const libs = await Gallery.utils.api('/api/libraries');
    if (!libs.length) {
      el.innerHTML = `<div class="empty"><p>No libraries configured. Add one in <a href="/settings" data-link>Settings</a>.</p></div>`;
      return;
    }
    el.innerHTML = libs.map(lib => `
      <div class="library-item" onclick="Gallery.utils.navigate('/browse/${lib.id}')">
        <span class="icon">📁</span>
        <span>${Gallery.utils.esc(lib.label || lib.path)}</span>
      </div>
    `).join('');
  } catch (e) {
    el.innerHTML = `<div class="empty"><p>Failed to load libraries.</p></div>`;
  }
}

function renderSidebar(data) {
  const sidebar = document.getElementById('sidebar');
  const libId = data.library_id;
  const currentPath = data.relative_path || '';

  // Build breadcrumb segments for tree highlighting.
  let html = `<div class="library-item active" onclick="Gallery.utils.navigate('/browse/${libId}')">
    <span class="icon">📁</span>
    <span>${Gallery.utils.esc(data.library_label)}</span>
  </div>`;

  // Show subdirs relevant to current path + children.
  if (data.subdirs && data.subdirs.length) {
    for (const subdir of data.subdirs) {
      const isActive = subdir.path === currentPath;
      html += `<div class="tree-node ${isActive ? 'active' : ''}"
        onclick="Gallery.utils.navigate('/browse/${libId}/${subdir.path}')">
        <span class="chevron">▶</span>
        <span>${Gallery.utils.esc(subdir.name)}</span>
      </div>`;
    }
  }

  sidebar.innerHTML = html;
}

function renderMain(data, libraryId) {
  const main = document.getElementById('browse-main');

  // Build breadcrumb.
  let breadcrumb = `<div class="breadcrumb">
    <a href="/browse/${libraryId}" data-link>${Gallery.utils.esc(data.library_label)}</a>`;
  if (data.relative_path) {
    const parts = data.relative_path.split('/');
    let cumPath = '';
    for (let i = 0; i < parts.length; i++) {
      cumPath = cumPath ? cumPath + '/' + parts[i] : parts[i];
      const isLast = i === parts.length - 1;
      breadcrumb += `<span class="sep">/</span>`;
      if (isLast) {
        breadcrumb += `<span>${Gallery.utils.esc(parts[i])}</span>`;
      } else {
        breadcrumb += `<a href="/browse/${libraryId}/${cumPath}" data-link>${Gallery.utils.esc(parts[i])}</a>`;
      }
    }
  }
  breadcrumb += `</div>`;

  let content = breadcrumb;

  // Subdirs as folder buttons in the grid area.
  if (data.subdirs && data.subdirs.length) {
    content += `<div class="section-title" style="margin-top:8px">Folders</div>
    <div style="display:flex;flex-wrap:wrap;gap:8px;margin-bottom:20px">`;
    for (const s of data.subdirs) {
      content += `<div class="library-item" style="border:1px solid var(--border);border-radius:var(--radius);cursor:pointer"
        onclick="Gallery.utils.navigate('/browse/${libraryId}/${s.path}')">
        <span>📁</span> ${Gallery.utils.esc(s.name)}
      </div>`;
    }
    content += `</div>`;
  }

  // Photos grid.
  if (!data.photos || !data.photos.length) {
    if (!data.subdirs || !data.subdirs.length) {
      content += `<div class="empty"><div class="empty-icon">📷</div><p>No photos in this folder.</p></div>`;
    }
  } else {
    content += `<div class="section-title">${data.photos.length} Photo${data.photos.length !== 1 ? 's' : ''}</div>
    <div class="photo-grid">`;
    for (const p of data.photos) {
      const hasFlag = p.flags && p.flags.some(f => f !== 'missing_gps');
      content += `<div class="photo-card" onclick="Gallery.utils.navigate('/photo/${p.sha256}')">
        <img src="${Gallery.utils.esc(p.thumbnail_url)}" loading="lazy" alt="${Gallery.utils.esc(p.filename)}">
        ${hasFlag ? '<div class="flag-dot" title="Data issues"></div>' : ''}
        <div class="photo-overlay">${Gallery.utils.esc(p.filename)}</div>
        <button class="stage-btn" title="Add to staging queue" onclick="Gallery.utils.stagePhoto('${Gallery.utils.esc(p.sha256)}', event)">+Stage</button>
      </div>`;
    }
    content += `</div>`;
  }

  main.innerHTML = content;
}
