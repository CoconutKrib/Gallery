// Settings page — Phase 6: inline editing of whitelist/filters + issues panel.
window.Gallery = window.Gallery || {};
Gallery.pages = Gallery.pages || {};

Gallery.pages.settings = async function () {
  const app = document.getElementById('app');
  Gallery.utils.setActiveNav('/settings');
  app.innerHTML = `<div class="settings-page"><h1>Settings</h1><div class="loading">Loading…</div></div>`;

  let settings, scanStatus;
  try {
    [settings, scanStatus] = await Promise.all([
      Gallery.utils.api('/api/settings'),
      Gallery.utils.api('/api/scan/status'),
    ]);
  } catch (err) {
    app.querySelector('.loading').textContent = 'Failed to load settings.';
    return;
  }

  // Local editable copies.
  Gallery.settings._whitelist = JSON.parse(JSON.stringify(settings.camera_whitelist || []));
  Gallery.settings._filters = JSON.parse(JSON.stringify({
    include: settings.filename_filters ? (settings.filename_filters.include || []) : [],
    exclude: settings.filename_filters ? (settings.filename_filters.exclude || []) : [],
  }));

  const esc = Gallery.utils.esc;
  const fmt = Gallery.utils.formatDate;

  // ── Library rows ──────────────────────────────────────────────────────────
  const libraryRows = (settings.library_paths || []).map(lp => {
    const run = (scanStatus.last_runs || []).find(r => r.library_label === lp.label);
    const lastScan = run
      ? `Last scan: ${fmt(run.started_at)} — ${run.files_ingested} ingested, ${run.files_duplicate} dupes`
      : 'Never scanned';
    return `<div class="settings-row">
      <span class="label">${esc(lp.label || lp.path)}</span>
      <span class="value" style="font-size:12px;color:var(--muted)">${esc(lp.path)}</span>
      <button class="scan-btn" data-path-label="${esc(lp.label)}"
              onclick="Gallery.settings.triggerScan(this)">Scan</button>
    </div>
    <div class="scan-status" id="scan-status-${esc(lp.label)}">${esc(lastScan)}</div>`;
  }).join('');

  // ── Scan run rows ─────────────────────────────────────────────────────────
  const scanRunsHtml = (scanStatus.last_runs || []).map(r =>
    `<div class="settings-row">
      <span class="label">${esc(r.library_label)}</span>
      <span class="value" style="font-size:12px">
        ${fmt(r.started_at)} — found:${r.files_found} ingested:${r.files_ingested}
        dupes:${r.files_duplicate} errors:${r.files_error}
        ${r.finished_at ? '' : ' <span class="pill">running</span>'}
      </span>
    </div>`
  ).join('') || `<div class="settings-row"><span style="color:var(--muted)">No scans yet</span></div>`;

  // ── Dropzone section ─────────────────────────────────────────────────────
  const dropzoneSectionHtml = (() => {
    const dz = settings.dropzone;
    if (!dz) return '';
    const enabledBadge = dz.enabled
      ? `<span class="pill pill-ok">enabled</span>`
      : `<span class="pill pill-warn">disabled</span>`;
    const scanBtnHtml = dz.enabled && dz.path
      ? `<button class="scan-btn" onclick="Gallery.settings.triggerDropzoneScan(this)">Scan Dropzone</button>
         <span id="scan-dropzone-status" class="scan-status"></span>`
      : '';
    return `<div class="settings-section">
      <h2>Dropzone ${enabledBadge}</h2>
      <div class="settings-row"><span class="label">Path</span><span class="value">${esc(dz.path || '—')}</span></div>
      ${scanBtnHtml ? `<div style="margin-top:12px">${scanBtnHtml}</div>` : ''}
    </div>`;
  })();

  app.innerHTML = `<div class="settings-page">
    <h1>Settings</h1>

    <div class="settings-section">
      <h2>Libraries</h2>
      ${libraryRows || '<div class="settings-row"><span style="color:var(--muted)">No libraries configured</span></div>'}
      <div style="margin-top:12px">
        <button class="scan-btn" onclick="Gallery.settings.triggerScanAll(this)">Scan All Libraries</button>
        <span id="scan-all-status" class="scan-status"></span>
      </div>
    </div>

    ${dropzoneSectionHtml}

    <div class="settings-section" id="section-whitelist">
      <h2>Camera Whitelist
        <span class="settings-hint">Leave empty to accept all cameras</span>
      </h2>
      <div id="whitelist-list"></div>
      <div class="settings-add-row">
        <input id="wl-make"  class="settings-input" placeholder="Make (e.g. Apple)" />
        <input id="wl-model" class="settings-input" placeholder="Model (blank = all)" />
        <button class="settings-add-btn" onclick="Gallery.settings.whitelistAdd()">Add</button>
      </div>
      <div class="settings-save-row">
        <button class="scan-btn" onclick="Gallery.settings.saveWhitelist(this)">Save Whitelist</button>
        <span id="wl-status" class="scan-status"></span>
      </div>
    </div>

    <div class="settings-section" id="section-filters">
      <h2>Filename Filters
        <span class="settings-hint">Regex patterns (case-insensitive). Exclude beats include.</span>
      </h2>
      <div class="filter-list-label">Include patterns</div>
      <div id="filter-include-list"></div>
      <div class="settings-add-row">
        <input id="fi-include" class="settings-input" placeholder="Pattern (e.g. ^IMG_)" />
        <button class="settings-add-btn" onclick="Gallery.settings.filterAdd('include')">Add</button>
        <span id="fi-include-err" class="settings-err"></span>
      </div>
      <div class="filter-list-label" style="margin-top:12px">Exclude patterns</div>
      <div id="filter-exclude-list"></div>
      <div class="settings-add-row">
        <input id="fi-exclude" class="settings-input" placeholder="Pattern (e.g. ^thumb_)" />
        <button class="settings-add-btn" onclick="Gallery.settings.filterAdd('exclude')">Add</button>
        <span id="fi-exclude-err" class="settings-err"></span>
      </div>
      <div class="settings-save-row">
        <button class="scan-btn" onclick="Gallery.settings.saveFilters(this)">Save Filters</button>
        <span id="fi-status" class="scan-status"></span>
      </div>
    </div>

    <div class="settings-section">
      <h2>Recent Scans</h2>
      ${scanRunsHtml}
    </div>

    <div class="settings-section">
      <h2>Configuration</h2>
      <div class="settings-row"><span class="label">DB path</span><span class="value">${esc(settings.db_path)}</span></div>
      <div class="settings-row"><span class="label">Cache dir</span><span class="value">${esc(settings.cache_dir)}</span></div>
      <div class="settings-row"><span class="label">Scan workers</span><span class="value">${settings.scan_workers}</span></div>
      <div class="settings-row"><span class="label">Event gap (days)</span><span class="value">${settings.event_gap_days}</span></div>
      <div class="settings-row"><span class="label">Event geo (km)</span><span class="value">${settings.event_geo_km}</span></div>
    </div>

    <div class="settings-section" id="section-issues">
      <h2>Ingest Issues
        <button class="settings-toggle-btn" id="issues-toggle"
                onclick="Gallery.settings.toggleIssues()">Show</button>
      </h2>
      <div id="issues-panel" style="display:none"></div>
    </div>
  </div>`;

  Gallery.settings.renderWhitelist();
  Gallery.settings.renderFilters();
};

// ── Settings module ───────────────────────────────────────────────────────────
Gallery.settings = {
  scanPollTimer: null,
  _whitelist: [],
  _filters: { include: [], exclude: [] },
  _issuesLoaded: false,

  // ── Whitelist ──────────────────────────────────────────────────────────────
  renderWhitelist() {
    const list = document.getElementById('whitelist-list');
    if (!list) return;
    if (this._whitelist.length === 0) {
      list.innerHTML = `<div class="settings-row"><span style="color:var(--muted)">All cameras accepted (whitelist empty)</span></div>`;
      return;
    }
    list.innerHTML = this._whitelist.map((c, i) =>
      `<div class="settings-row">
        <span class="label">${Gallery.utils.esc(c.make)}</span>
        <span class="value">${Gallery.utils.esc(c.model || '— all models —')}</span>
        <button class="settings-del-btn" onclick="Gallery.settings.whitelistRemove(${i})" title="Remove">✕</button>
      </div>`
    ).join('');
  },

  whitelistAdd() {
    const makeEl  = document.getElementById('wl-make');
    const modelEl = document.getElementById('wl-model');
    const make  = (makeEl.value || '').trim();
    const model = (modelEl.value || '').trim();
    if (!make) { makeEl.focus(); return; }
    this._whitelist.push({ make, model });
    makeEl.value = '';
    modelEl.value = '';
    this.renderWhitelist();
  },

  whitelistRemove(i) {
    this._whitelist.splice(i, 1);
    this.renderWhitelist();
  },

  async saveWhitelist(btn) {
    btn.disabled = true;
    const statusEl = document.getElementById('wl-status');
    statusEl.textContent = 'Saving…';
    try {
      await Gallery.utils.api('/api/settings', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ camera_whitelist: this._whitelist }),
      });
      statusEl.textContent = 'Saved.';
    } catch (err) {
      statusEl.textContent = 'Error: ' + err.message;
    } finally {
      btn.disabled = false;
    }
  },

  // ── Filename filters ───────────────────────────────────────────────────────
  renderFilters() {
    this._renderFilterList('include');
    this._renderFilterList('exclude');
  },

  _renderFilterList(kind) {
    const list = document.getElementById('filter-' + kind + '-list');
    if (!list) return;
    const patterns = this._filters[kind];
    if (patterns.length === 0) {
      list.innerHTML = `<div class="settings-row"><span style="color:var(--muted)">None</span></div>`;
      return;
    }
    list.innerHTML = patterns.map((p, i) =>
      `<div class="settings-row">
        <code class="filter-pattern">${Gallery.utils.esc(p)}</code>
        <button class="settings-del-btn" onclick="Gallery.settings.filterRemove('${kind}',${i})" title="Remove">✕</button>
      </div>`
    ).join('');
  },

  filterAdd(kind) {
    const inputId = kind === 'include' ? 'fi-include' : 'fi-exclude';
    const errId   = inputId + '-err';
    const inputEl = document.getElementById(inputId);
    const errEl   = document.getElementById(errId);
    const val = (inputEl.value || '').trim();
    if (!val) { inputEl.focus(); return; }
    // Validate regex client-side.
    try {
      new RegExp(val); // eslint-disable-line no-new
    } catch (_) {
      errEl.textContent = 'Invalid regex';
      inputEl.focus();
      return;
    }
    errEl.textContent = '';
    this._filters[kind].push(val);
    inputEl.value = '';
    this._renderFilterList(kind);
  },

  filterRemove(kind, i) {
    this._filters[kind].splice(i, 1);
    this._renderFilterList(kind);
  },

  async saveFilters(btn) {
    btn.disabled = true;
    const statusEl = document.getElementById('fi-status');
    statusEl.textContent = 'Saving…';
    try {
      await Gallery.utils.api('/api/settings', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ filename_filters: this._filters }),
      });
      statusEl.textContent = 'Saved.';
    } catch (err) {
      statusEl.textContent = 'Error: ' + err.message;
    } finally {
      btn.disabled = false;
    }
  },

  // ── Issues panel ───────────────────────────────────────────────────────────
  async toggleIssues() {
    const panel  = document.getElementById('issues-panel');
    const toggle = document.getElementById('issues-toggle');
    if (!panel) return;
    const open = panel.style.display !== 'none';
    panel.style.display = open ? 'none' : 'block';
    toggle.textContent  = open ? 'Show' : 'Hide';
    if (!open && !this._issuesLoaded) {
      await this.loadIssues(panel);
    }
  },

  async loadIssues(panel) {
    panel.innerHTML = '<div class="loading" style="padding:16px">Loading issues…</div>';
    try {
      const data = await Gallery.utils.api('/api/issues?per_page=200');
      this._issuesLoaded = true;
      if (!data.items || data.items.length === 0) {
        panel.innerHTML = '<div class="settings-row"><span style="color:var(--muted)">No issues found — all photos ingested cleanly.</span></div>';
        return;
      }
      const esc = Gallery.utils.esc;
      const fmt = Gallery.utils.formatDate;
      const flagLabel = { missing_gps: 'No GPS', exif_error: 'EXIF error', unknown_camera: 'Unknown camera' };
      const rows = data.items.map(item => {
        const flags = (item.flags || []).map(f =>
          `<span class="pill pill-warn">${esc(flagLabel[f] || f)}</span>`
        ).join(' ');
        const date = item.captured_at ? fmt(item.captured_at) : '—';
        return `<div class="issue-row">
          <a href="/photo/${esc(item.sha256)}" onclick="Gallery.utils.navigate('/photo/${esc(item.sha256)}');return false;"
             class="issue-filename">${esc(item.filename)}</a>
          <span class="issue-meta">${esc(item.camera_make)} ${esc(item.camera_model)} · ${date}</span>
          <div class="issue-flags">${flags}</div>
          <div class="issue-path">${esc(item.filepath)}</div>
        </div>`;
      }).join('');
      panel.innerHTML = `<div class="issues-count">${data.total} photo${data.total !== 1 ? 's' : ''} with issues</div>${rows}`;
    } catch (err) {
      panel.innerHTML = `<div class="settings-row"><span style="color:var(--danger)">Failed to load issues: ${Gallery.utils.esc(err.message)}</span></div>`;
    }
  },

  // ── Scan helpers ────────────────────────────────────────────────────────────
  async triggerScan(btn) {
    btn.disabled = true;
    const label = btn.dataset.pathLabel;
    const statusEl = document.getElementById('scan-status-' + label);
    if (statusEl) statusEl.textContent = 'Starting scan…';
    try {
      await Gallery.utils.api('/api/scan', { method: 'POST', body: '{}', headers: { 'Content-Type': 'application/json' } });
      Gallery.settings.pollScanStatus(label, statusEl, btn);
    } catch (err) {
      if (statusEl) statusEl.textContent = 'Error: ' + err.message;
      btn.disabled = false;
    }
  },

  async triggerDropzoneScan(btn) {
    btn.disabled = true;
    const statusEl = document.getElementById('scan-dropzone-status');
    if (statusEl) statusEl.textContent = 'Starting dropzone scan…';
    try {
      await Gallery.utils.api('/api/scan', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ source: 'dropzone' }),
      });
      Gallery.settings.pollScanStatus('Dropzone', statusEl, btn);
    } catch (err) {
      if (statusEl) statusEl.textContent = 'Error: ' + err.message;
      btn.disabled = false;
    }
  },

  async triggerScanAll(btn) {
    btn.disabled = true;
    const statusEl = document.getElementById('scan-all-status');
    statusEl.textContent = 'Starting…';
    try {
      await Gallery.utils.api('/api/scan', { method: 'POST', body: '{}', headers: { 'Content-Type': 'application/json' } });
      Gallery.settings.pollScanStatus(null, statusEl, btn);
    } catch (err) {
      statusEl.textContent = 'Error: ' + err.message;
      btn.disabled = false;
    }
  },

  pollScanStatus(label, statusEl, btn) {
    if (this.scanPollTimer) clearInterval(this.scanPollTimer);
    this.scanPollTimer = setInterval(async () => {
      try {
        const status = await Gallery.utils.api('/api/scan/status');
        if (status.running) {
          const s = status.live_stats || {};
          const msg = `Running${status.current_label ? ' — ' + status.current_label : ''}: found ${s.found || 0}, ingested ${s.ingested || 0}…`;
          if (statusEl) statusEl.textContent = msg;
        } else {
          clearInterval(this.scanPollTimer);
          const last = (status.last_runs || []).find(r => !label || r.library_label === label);
          if (statusEl && last) {
            statusEl.textContent = `Done — found:${last.files_found} ingested:${last.files_ingested} dupes:${last.files_duplicate} errors:${last.files_error}`;
          } else if (statusEl) {
            statusEl.textContent = 'Scan complete.';
          }
          if (btn) btn.disabled = false;
        }
      } catch (_) { clearInterval(this.scanPollTimer); if (btn) btn.disabled = false; }
    }, 1500);
  },
};
