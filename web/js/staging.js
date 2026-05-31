// Staging queue page: review, annotate, approve or reject photos before
// copying them to the internal library.
window.Gallery = window.Gallery || {};
Gallery.pages = Gallery.pages || {};

let stagingState = {
  entries: [],
  selectedId: null,
  filterState: '',
  events: [],
};

Gallery.pages.staging = async function() {
  const app = document.getElementById('app');
  Gallery.utils.setActiveNav('/staging');

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
      <h2 class="page-title">Staging Queue</h2>
      <div class="empty">
        <div class="empty-icon">⚙️</div>
        <p>Internal library is not enabled. Configure a library path in <a href="/settings" data-link>Settings</a>.</p>
      </div>
    </div>`;
    return;
  }

  app.innerHTML = `
    <div class="staging-layout">
      <div class="staging-left" id="staging-left">
        <div class="staging-toolbar">
          <h2 class="page-title" style="margin:0">Staging Queue</h2>
          <div class="staging-filters">
            <button class="btn btn-sm active" data-state="">All</button>
            <button class="btn btn-sm" data-state="staged">Staged</button>
            <button class="btn btn-sm" data-state="approved">Approved</button>
            <button class="btn btn-sm" data-state="rejected">Rejected</button>
          </div>
          <button class="btn btn-primary" id="copy-all-btn">Copy all approved</button>
        </div>
        <div id="staging-copy-status"></div>
        <div id="staging-list" class="loading">Loading…</div>
      </div>
      <div class="staging-right" id="staging-right">
        <div class="empty staging-empty-hint">
          <div class="empty-icon">👈</div>
          <p>Select a photo to review and annotate it.</p>
        </div>
      </div>
    </div>`;

  // Load events for the picker.
  try {
    const evData = await Gallery.utils.api('/api/events');
    stagingState.events = evData.items || [];
  } catch (_) {}

  // Filter button handlers.
  app.querySelectorAll('.staging-filters .btn').forEach(btn => {
    btn.addEventListener('click', async () => {
      app.querySelectorAll('.staging-filters .btn').forEach(b => b.classList.remove('active'));
      btn.classList.add('active');
      stagingState.filterState = btn.dataset.state;
      await loadStagingList();
    });
  });

  document.getElementById('copy-all-btn').addEventListener('click', triggerCopyAll);

  await loadStagingList();
  pollCopyStatus();
};

async function loadStagingList() {
  const listEl = document.getElementById('staging-list');
  if (!listEl) return;
  listEl.className = 'loading';
  listEl.textContent = 'Loading…';
  try {
    const url = stagingState.filterState
      ? `/api/staging?state=${stagingState.filterState}`
      : '/api/staging';
    stagingState.entries = await Gallery.utils.api(url);
  } catch (e) {
    listEl.innerHTML = `<div class="empty"><p>Error: ${Gallery.utils.esc(e.message)}</p></div>`;
    return;
  }
  listEl.className = '';
  renderStagingList(listEl);
}

function renderStagingList(el) {
  if (!stagingState.entries.length) {
    el.innerHTML = `<div class="empty"><div class="empty-icon">📭</div><p>No photos in queue.</p></div>`;
    return;
  }
  el.innerHTML = stagingState.entries.map(e => {
    const active = e.id === stagingState.selectedId ? ' staging-item--active' : '';
    const stateClass = `staging-badge staging-badge--${e.state}`;
    return `<div class="staging-item${active}" data-id="${e.id}" onclick="selectStagingEntry(${e.id})">
      <img class="staging-item__thumb" src="/api/photos/${e.photo_sha256}/thumbnail" alt="" loading="lazy">
      <div class="staging-item__info">
        <div class="staging-item__sha">${e.photo_sha256.slice(0, 12)}…</div>
        ${e.title ? `<div class="staging-item__title">${Gallery.utils.esc(e.title)}</div>` : ''}
        <span class="${stateClass}">${e.state}</span>
        ${e.true_date_unknown ? '<span class="staging-badge staging-badge--undated">undated</span>' : ''}
      </div>
    </div>`;
  }).join('');
}

window.selectStagingEntry = async function(id) {
  stagingState.selectedId = id;
  // Re-render the list to update active state.
  const listEl = document.getElementById('staging-list');
  if (listEl) renderStagingList(listEl);

  const panel = document.getElementById('staging-right');
  panel.innerHTML = '<div class="loading">Loading…</div>';
  try {
    const entry = await Gallery.utils.api(`/api/staging/${id}`);
    let photo = null;
    try { photo = await Gallery.utils.api(`/api/photos/${entry.photo_sha256}`); } catch (_) {}
    renderAnnotationPanel(panel, entry, photo);
  } catch (e) {
    panel.innerHTML = `<div class="empty"><p>Error: ${Gallery.utils.esc(e.message)}</p></div>`;
  }
};

function renderAnnotationPanel(panel, entry, photo) {
  const exifRows = photo ? [
    ['Filename', photo.filename],
    ['Captured', Gallery.utils.formatDate(photo.captured_at)],
    ['Camera', [photo.camera_make, photo.camera_model].filter(Boolean).join(' ')],
    ['GPS', photo.latitude != null
      ? `${Gallery.utils.formatCoord(photo.latitude, 'N', 'S')}, ${Gallery.utils.formatCoord(photo.longitude, 'E', 'W')}`
      : null],
    ['Size', (photo.width && photo.height) ? `${photo.width} × ${photo.height}` : null],
  ].filter(r => r[1]).map(r =>
    `<tr><th>${Gallery.utils.esc(r[0])}</th><td>${Gallery.utils.esc(String(r[1]))}</td></tr>`
  ).join('') : '';

  const eventOptions = stagingState.events.map(ev =>
    `<option value="${ev.id}" ${entry.event_id === ev.id ? 'selected' : ''}>${Gallery.utils.esc(ev.label)}</option>`
  ).join('');

  const tagsVal = (entry.tags || []).join(', ');

  panel.innerHTML = `
    <div class="staging-panel">
      <div class="staging-panel__img-wrap">
        <img src="/api/photos/${entry.photo_sha256}/thumbnail" class="staging-panel__img" alt="">
        <a class="staging-panel__view-link" href="/photo/${entry.photo_sha256}" data-link>View full detail →</a>
      </div>

      ${exifRows ? `<table class="exif-table"><tbody>${exifRows}</tbody></table>` : ''}

      <form id="annotation-form" class="annotation-form" onsubmit="return false">
        <label>Title
          <input type="text" name="title" value="${Gallery.utils.esc(entry.title || '')}">
        </label>
        <label>Description
          <textarea name="description" rows="3">${Gallery.utils.esc(entry.description || '')}</textarea>
        </label>
        <label>Override date (RFC3339 UTC)
          <input type="text" name="override_date" placeholder="2024-06-15T12:00:00Z"
            value="${Gallery.utils.esc(entry.override_date || '')}">
        </label>
        <label class="checkbox-label">
          <input type="checkbox" name="true_date_unknown" ${entry.true_date_unknown ? 'checked' : ''}>
          True date unknown (archival/historic photo → placed in _undated/)
        </label>
        <label>Override GPS (lat, lon)
          <div class="input-row">
            <input type="number" step="any" name="override_lat" placeholder="Latitude"
              value="${entry.override_lat != null ? entry.override_lat : ''}">
            <input type="number" step="any" name="override_lon" placeholder="Longitude"
              value="${entry.override_lon != null ? entry.override_lon : ''}">
          </div>
        </label>
        <label>Event
          <select name="event_id">
            <option value="">— no event —</option>
            ${eventOptions}
          </select>
        </label>
        <label>Tags (comma-separated)
          <input type="text" name="tags" value="${Gallery.utils.esc(tagsVal)}">
        </label>
        <div class="staging-panel__actions">
          <button type="button" class="btn btn-sm" onclick="saveStagingAnnotations(${entry.id})">Save</button>
          <button type="button" class="btn btn-primary btn-sm" onclick="approveStagingEntry(${entry.id})">Approve</button>
          <button type="button" class="btn btn-danger btn-sm" onclick="rejectStagingEntry(${entry.id})">Reject</button>
        </div>
        <div id="annotation-status-${entry.id}" class="annotation-status"></div>
      </form>
    </div>`;
}

window.saveStagingAnnotations = async function(id) {
  const form = document.getElementById('annotation-form');
  if (!form) return;
  const data = new FormData(form);
  const tagsRaw = data.get('tags') || '';
  const tags = tagsRaw.split(',').map(t => t.trim()).filter(Boolean);
  const overrideLat = data.get('override_lat');
  const overrideLon = data.get('override_lon');
  const eventIdRaw = data.get('event_id');

  const body = {
    title: data.get('title') || null,
    description: data.get('description') || null,
    override_date: data.get('override_date') || null,
    override_lat: overrideLat !== '' ? parseFloat(overrideLat) : null,
    override_lon: overrideLon !== '' ? parseFloat(overrideLon) : null,
    event_id: eventIdRaw ? parseInt(eventIdRaw, 10) : null,
    tags,
    true_date_unknown: data.has('true_date_unknown'),
  };
  const statusEl = document.getElementById(`annotation-status-${id}`);
  try {
    await Gallery.utils.api(`/api/staging/${id}`, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    });
    if (statusEl) statusEl.textContent = 'Saved.';
    await loadStagingList();
  } catch (e) {
    if (statusEl) statusEl.textContent = `Error: ${e.message}`;
  }
};

window.approveStagingEntry = async function(id) {
  const statusEl = document.getElementById(`annotation-status-${id}`);
  try {
    await Gallery.utils.api(`/api/staging/${id}/approve`, { method: 'POST' });
    if (statusEl) statusEl.textContent = 'Approved.';
    stagingState.selectedId = null;
    await loadStagingList();
    document.getElementById('staging-right').innerHTML =
      `<div class="empty staging-empty-hint"><div class="empty-icon">✅</div><p>Photo approved.</p></div>`;
  } catch (e) {
    if (statusEl) statusEl.textContent = `Error: ${e.message}`;
  }
};

window.rejectStagingEntry = async function(id) {
  const statusEl = document.getElementById(`annotation-status-${id}`);
  try {
    await Gallery.utils.api(`/api/staging/${id}/reject`, { method: 'POST' });
    if (statusEl) statusEl.textContent = 'Rejected.';
    stagingState.selectedId = null;
    await loadStagingList();
    document.getElementById('staging-right').innerHTML =
      `<div class="empty staging-empty-hint"><div class="empty-icon">🚫</div><p>Photo rejected.</p></div>`;
  } catch (e) {
    if (statusEl) statusEl.textContent = `Error: ${e.message}`;
  }
};

async function triggerCopyAll() {
  const btn = document.getElementById('copy-all-btn');
  if (btn) btn.disabled = true;
  try {
    await Gallery.utils.api('/api/library/copy', { method: 'POST' });
    const statusEl = document.getElementById('staging-copy-status');
    if (statusEl) statusEl.innerHTML = `<div class="info-bar">Copy started…</div>`;
    pollCopyStatus();
  } catch (e) {
    const statusEl = document.getElementById('staging-copy-status');
    if (statusEl) statusEl.innerHTML = `<div class="error-bar">Error: ${Gallery.utils.esc(e.message)}</div>`;
    if (btn) btn.disabled = false;
  }
}

function pollCopyStatus() {
  const interval = setInterval(async () => {
    try {
      const s = await Gallery.utils.api('/api/library/status');
      const statusEl = document.getElementById('staging-copy-status');
      if (!statusEl) { clearInterval(interval); return; }
      if (s.running) {
        statusEl.innerHTML = `<div class="info-bar">Copying… (${s.copied} copied, ${s.errors} errors)</div>`;
      } else {
        statusEl.innerHTML = s.copied > 0 || s.errors > 0
          ? `<div class="info-bar">Last copy: ${s.copied} copied, ${s.skipped} skipped, ${s.errors} errors.${s.last_error ? ' Last error: ' + Gallery.utils.esc(s.last_error) : ''}</div>`
          : '';
        clearInterval(interval);
        const btn = document.getElementById('copy-all-btn');
        if (btn) btn.disabled = false;
        await loadStagingList();
      }
    } catch (_) { clearInterval(interval); }
  }, 1500);
}
