// Internal Library page: browse photos that have been copied to the managed library.
// Folder-tree navigation (year → month → event), filter bar, edit/annotation panel,
// and photo removal with confirmation.
window.Gallery = window.Gallery || {};
Gallery.pages = Gallery.pages || {};

// Module-level state.
let _currentFilters = {};
let _currentPathFilter = null; // {year, month, slug} or null
let _selectedCopyID = null;
let _allPhotos = [];

Gallery.pages.library = async function() {
  const app = document.getElementById('app');
  Gallery.utils.setActiveNav('/library');
  _currentFilters = {};
  _currentPathFilter = null;
  _selectedCopyID = null;
  _allPhotos = [];

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
    <div class="library-layout" id="library-layout">
      <aside class="library-sidebar" id="library-sidebar">
        <h3 class="sidebar-title">Library</h3>
        <div id="library-tree" class="loading">Loading…</div>
      </aside>
      <main class="library-main">
        <div id="library-filter-bar" class="filter-bar">
          <input id="lib-q" type="text" placeholder="Search title, description, filename…" class="filter-input filter-input--wide">
          <select id="lib-source" class="filter-select">
            <option value="">All sources</option>
            <option value="scan">Scan</option>
            <option value="dropzone">Dropzone</option>
          </select>
          <select id="lib-date-override" class="filter-select">
            <option value="">Any date</option>
            <option value="true">Date overridden</option>
            <option value="false">No override</option>
          </select>
          <label class="filter-checkbox-label">
            <input id="lib-undated" type="checkbox"> Undated only
          </label>
          <input id="lib-tag" type="text" placeholder="Tag…" class="filter-input">
          <button onclick="Gallery.pages.libraryApplyFilters()" class="btn">Filter</button>
          <button onclick="Gallery.pages.libraryResetFilters()" class="btn btn--secondary">Reset</button>
        </div>
        <div id="library-header">
          <h2 class="page-title">All Photos</h2>
        </div>
        <div id="library-grid" class="loading">Loading…</div>
      </main>
      <aside class="library-edit-panel" id="library-edit-panel" style="display:none">
        <div id="library-edit-content"></div>
      </aside>
    </div>`;

  await loadLibraryTree();
  await loadLibraryPhotos();
};

Gallery.pages.libraryApplyFilters = function() {
  _currentFilters = {
    q:                document.getElementById('lib-q')?.value.trim() || '',
    source:           document.getElementById('lib-source')?.value || '',
    has_date_override: document.getElementById('lib-date-override')?.value || '',
    true_date_unknown: document.getElementById('lib-undated')?.checked ? 'true' : '',
    tag:              document.getElementById('lib-tag')?.value.trim() || '',
  };
  loadLibraryPhotos(_currentPathFilter?.year, _currentPathFilter?.month, _currentPathFilter?.slug);
};

Gallery.pages.libraryResetFilters = function() {
  _currentFilters = {};
  _currentPathFilter = null;
  if (document.getElementById('lib-q')) document.getElementById('lib-q').value = '';
  if (document.getElementById('lib-source')) document.getElementById('lib-source').value = '';
  if (document.getElementById('lib-date-override')) document.getElementById('lib-date-override').value = '';
  if (document.getElementById('lib-undated')) document.getElementById('lib-undated').checked = false;
  if (document.getElementById('lib-tag')) document.getElementById('lib-tag').value = '';
  loadLibraryPhotos();
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
  _currentPathFilter = (year || month || slug) ? {year, month, slug} : null;
  const gridEl = document.getElementById('library-grid');
  const headerEl = document.getElementById('library-header');
  if (!gridEl) return;

  document.querySelectorAll('.tree-item--active').forEach(el => el.classList.remove('tree-item--active'));
  gridEl.className = 'loading';
  gridEl.textContent = 'Loading…';

  let label = 'All Photos';
  if (year === '_undated') label = 'Undated Photos';
  else if (year && month && slug) label = `${year} / ${month} / ${slug}`;
  else if (year && month) label = `${year} / ${month}`;
  else if (year) label = year;
  if (headerEl) headerEl.innerHTML = `<h2 class="page-title">${Gallery.utils.esc(label)}</h2>`;

  // Build server-side query params from _currentFilters.
  const params = new URLSearchParams();
  if (_currentFilters.q) params.set('q', _currentFilters.q);
  if (_currentFilters.source) params.set('source', _currentFilters.source);
  if (_currentFilters.has_date_override) params.set('has_date_override', _currentFilters.has_date_override);
  if (_currentFilters.true_date_unknown) params.set('true_date_unknown', _currentFilters.true_date_unknown);
  if (_currentFilters.tag) params.set('tag', _currentFilters.tag);
  params.set('per_page', '500');

  let data;
  try {
    const qs = params.toString();
    data = await Gallery.utils.api('/api/library/photos' + (qs ? '?' + qs : ''));
  } catch (e) {
    gridEl.className = '';
    gridEl.innerHTML = `<div class="empty"><p>Error: ${Gallery.utils.esc(e.message)}</p></div>`;
    return;
  }

  // Client-side path-prefix filter for tree navigation.
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

  _allPhotos = photos;
  renderLibraryGrid(photos);
};

function renderLibraryGrid(photos) {
  const gridEl = document.getElementById('library-grid');
  if (!gridEl) return;
  gridEl.className = '';
  if (!photos.length) {
    gridEl.innerHTML = `<div class="empty"><div class="empty-icon">🖼️</div><p>No photos here.</p></div>`;
    return;
  }
  gridEl.innerHTML = `<div class="photo-grid">
    ${photos.map(p => `
      <div class="photo-card ${_selectedCopyID === p.id ? 'photo-card--selected' : ''}"
           onclick="librarySelectPhoto(${p.id})" data-copy-id="${p.id}">
        <img class="photo-card__thumb" src="/api/photos/${p.photo_sha256}/thumbnail" alt="" loading="lazy">
        <div class="photo-card__info">
          <div class="photo-card__filename">${Gallery.utils.esc(p.relative_path.split('/').pop())}</div>
          <div class="photo-card__date">${Gallery.utils.formatDate(p.copied_at)}</div>
          <div class="photo-card__badges">
            ${p.true_date_unknown ? '<span class="staging-badge staging-badge--undated">undated</span>' : ''}
            ${p.override_date ? '<span class="staging-badge staging-badge--override">date overridden</span>' : ''}
            ${p.title ? '<span class="staging-badge staging-badge--titled">' + Gallery.utils.esc(p.title) + '</span>' : ''}
          </div>
        </div>
      </div>`).join('')}
  </div>`;
}

window.librarySelectPhoto = function(copyID) {
  _selectedCopyID = copyID;
  // Highlight selected card.
  document.querySelectorAll('.photo-card--selected').forEach(el => el.classList.remove('photo-card--selected'));
  const card = document.querySelector(`[data-copy-id="${copyID}"]`);
  if (card) card.classList.add('photo-card--selected');

  const photo = _allPhotos.find(p => p.id === copyID);
  if (!photo) return;

  const panel = document.getElementById('library-edit-panel');
  const content = document.getElementById('library-edit-content');
  if (!panel || !content) return;

  panel.style.display = '';
  document.getElementById('library-layout')?.classList.add('library-layout--editing');

  content.innerHTML = `
    <div class="edit-panel-header">
      <h3 class="edit-panel-title">Edit Photo</h3>
      <button class="edit-panel-close" onclick="libraryClosePanel()" title="Close">✕</button>
    </div>
    <div class="edit-panel-thumb">
      <img src="/api/photos/${photo.photo_sha256}/thumbnail" alt="">
    </div>
    <div class="edit-panel-meta">
      <a href="/photo/${photo.photo_sha256}" data-link class="edit-panel-source-link">View original →</a>
      <div class="edit-panel-path">${Gallery.utils.esc(photo.relative_path)}</div>
    </div>
    <form id="lib-edit-form" onsubmit="librarySaveEdit(event, ${copyID})">
      <div class="form-group">
        <label>Title</label>
        <input type="text" name="title" value="${Gallery.utils.esc(photo.title || '')}" class="form-input">
      </div>
      <div class="form-group">
        <label>Description</label>
        <textarea name="description" rows="3" class="form-input">${Gallery.utils.esc(photo.description || '')}</textarea>
      </div>
      <div class="form-group">
        <label>Tags <span class="form-hint">(comma-separated)</span></label>
        <input type="text" name="tags" value="${Gallery.utils.esc((photo.tags || []).join(', '))}" class="form-input">
      </div>
      <div class="form-group">
        <label>Override date <span class="form-hint">(RFC3339, e.g. 1985-06-15T00:00:00Z)</span></label>
        <input type="text" name="override_date" value="${Gallery.utils.esc(photo.override_date || '')}" class="form-input">
      </div>
      <div class="form-group form-group--checkbox">
        <label>
          <input type="checkbox" name="true_date_unknown" ${photo.true_date_unknown ? 'checked' : ''}>
          Date unknown (archival) — always place in _undated/
        </label>
      </div>
      <div class="form-actions">
        <button type="submit" class="btn">Save</button>
        <span id="lib-save-status" class="form-status"></span>
      </div>
    </form>
    <div class="edit-panel-people">
      <h4>People in this photo</h4>
      <div class="face-tags" id="faces-list-${copyID}"><span class="no-faces-msg">Loading…</span></div>
      <div class="face-add-row">
        <input type="text" id="face-add-input-${copyID}" list="people-datalist-${copyID}"
               placeholder="Person name…" class="form-input form-input--sm">
        <button type="button" class="btn btn--sm"
                onclick="libraryAddFace(${copyID})">Tag</button>
      </div>
      <div class="face-add-row" style="margin-top:6px">
        <button type="button" class="btn btn--sm" id="detect-faces-btn-${copyID}"
                onclick="libraryDetectFaces(${copyID}, '${Gallery.utils.esc(photo.photo_sha256)}')">
          Auto-Detect Faces
        </button>
        <span id="detect-faces-status-${copyID}" style="margin-left:6px;font-size:12px;color:var(--muted)"></span>
      </div>
      <datalist id="people-datalist-${copyID}"></datalist>
    </div>
    <hr class="edit-panel-divider">
    <div class="edit-panel-danger">
      <button class="btn btn--danger" onclick="libraryConfirmRemove(${copyID}, '${Gallery.utils.esc(photo.relative_path.split('/').pop())}')">
        Remove from Library
      </button>
      <p class="danger-hint">Deletes the copy and all Gallery records. Original file is not affected.</p>
    </div>`;

  // Load faces asynchronously (panel is already visible).
  libraryLoadFaces(copyID);
};

window.libraryClosePanel = function() {
  _selectedCopyID = null;
  document.getElementById('library-edit-panel').style.display = 'none';
  document.getElementById('library-layout')?.classList.remove('library-layout--editing');
  document.querySelectorAll('.photo-card--selected').forEach(el => el.classList.remove('photo-card--selected'));
};

window.librarySaveEdit = async function(evt, copyID) {
  evt.preventDefault();
  const form = evt.target;
  const statusEl = document.getElementById('lib-save-status');
  if (statusEl) statusEl.textContent = 'Saving…';

  const tagsRaw = form.elements.tags.value.trim();
  const tags = tagsRaw ? tagsRaw.split(',').map(t => t.trim()).filter(Boolean) : [];

  const body = {
    title:             form.elements.title.value || null,
    description:       form.elements.description.value || null,
    tags,
    override_date:     form.elements.override_date.value.trim() || null,
    true_date_unknown: form.elements.true_date_unknown.checked,
  };
  // Null out empty strings so the server treats them as clears.
  if (body.title === '') body.title = null;
  if (body.description === '') body.description = null;

  try {
    const updated = await Gallery.utils.api('/api/library/copies/' + copyID, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    });
    if (statusEl) { statusEl.textContent = 'Saved.'; setTimeout(() => statusEl.textContent = '', 2000); }
    // Refresh the grid (path may have changed).
    const pf = _currentPathFilter;
    await loadLibraryPhotos(pf?.year, pf?.month, pf?.slug);
    // Re-select the same photo (use updated relative_path).
    if (updated && updated.id) {
      librarySelectPhoto(updated.id);
    }
  } catch (e) {
    if (statusEl) statusEl.textContent = 'Error: ' + e.message;
  }
};

window.libraryConfirmRemove = function(copyID, filename) {
  if (!confirm(`Remove "${filename}" from the library?\n\nThis will delete the copy and all Gallery records for this photo. The original file is not affected.`)) {
    return;
  }
  libraryRemovePhoto(copyID);
};

window.libraryRemovePhoto = async function(copyID) {
  try {
    await Gallery.utils.api('/api/library/copies/' + copyID, { method: 'DELETE' });
    libraryClosePanel();
    const pf = _currentPathFilter;
    await loadLibraryPhotos(pf?.year, pf?.month, pf?.slug);
    await loadLibraryTree();
  } catch (e) {
    alert('Remove failed: ' + e.message);
  }
};

// ---- People / face-tagging panel helpers ----

// Load (or reload) the face tags for the currently open copy.
window.libraryLoadFaces = async function(copyID) {
  const facesEl  = document.getElementById('faces-list-' + copyID);
  const datalist = document.getElementById('people-datalist-' + copyID);
  if (!facesEl) return;

  try {
    const [faces, people] = await Promise.all([
      Gallery.utils.api('/api/library/copies/' + copyID + '/faces'),
      Gallery.utils.api('/api/people'),
    ]);

    // Populate datalist for autocomplete.
    if (datalist) {
      datalist.innerHTML = people.map(p =>
        `<option value="${Gallery.utils.esc(p.name)}">`
      ).join('');
    }

    // Cache people list for use in libraryAddFace.
    window._facePeopleCache = people;

    if (!faces.length) {
      facesEl.innerHTML = '<span class="no-faces-msg">None tagged yet.</span>';
    } else {
      facesEl.innerHTML = faces.map(f => `
        <div class="face-tag" data-face-id="${f.id}">
          <span class="face-tag-name">${Gallery.utils.esc(f.person_name || '(unidentified)')}</span>
          <button class="face-tag-remove"
                  onclick="libraryRemoveFace(${f.id}, ${copyID})" title="Remove">✕</button>
        </div>`).join('');
    }
  } catch (e) {
    if (facesEl) facesEl.innerHTML = `<span class="no-faces-msg">Error: ${Gallery.utils.esc(e.message)}</span>`;
  }
};

// Tag a person in the currently open copy.
// If the typed name doesn't match an existing person, creates them first.
window.libraryAddFace = async function(copyID) {
  const input = document.getElementById('face-add-input-' + copyID);
  if (!input) return;
  const name = input.value.trim();
  if (!name) return;

  const people = window._facePeopleCache || [];
  let person = people.find(p => p.name.toLowerCase() === name.toLowerCase());

  try {
    if (!person) {
      person = await Gallery.utils.api('/api/people', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name }),
      });
    }

    await Gallery.utils.api('/api/library/copies/' + copyID + '/faces', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ person_id: person.id }),
    });

    input.value = '';
    await libraryLoadFaces(copyID);
  } catch (e) {
    alert('Error tagging person: ' + e.message);
  }
};

// Remove a face tag from a copy.
window.libraryRemoveFace = async function(faceID, copyID) {
  try {
    await Gallery.utils.api('/api/faces/' + faceID, { method: 'DELETE' });
    await libraryLoadFaces(copyID);
  } catch (e) {
    alert('Error removing tag: ' + e.message);
  }
};


  await loadLibraryTree();
  await loadLibraryPhotos();
};
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



// Trigger face detection for a library copy (async, enqueues to background worker).
window.libraryDetectFaces = async function(copyID, sha256) {
  const btn = document.getElementById('detect-faces-btn-' + copyID);
  const status = document.getElementById('detect-faces-status-' + copyID);
  if (btn) btn.disabled = true;
  if (status) status.textContent = 'Enqueuing…';
  try {
    const resp = await Gallery.utils.api('/api/photos/' + sha256 + '/detect-faces', {
      method: 'POST',
    });
    if (resp.queued) {
      if (status) status.textContent = 'Queued. Faces will appear shortly.';
    } else {
      if (status) status.textContent = resp.reason || 'Already processed.';
    }
  } catch (e) {
    if (status) status.textContent = 'Error: ' + e.message;
    if (btn) btn.disabled = false;
  }
};
