// Settings page — synchronised with Config struct and /api/settings.
// Every field from GET /api/settings is rendered. Editable sections POST
// only their own fields (no cross-section overwrites).
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

  // Store settings on Gallery.settings so section handlers can read them.
  Gallery.settings._data = settings;

  // Local editable copies for whitelist / filters (existing pattern).
  Gallery.settings._whitelist = JSON.parse(JSON.stringify(settings.camera_whitelist || []));
  Gallery.settings._filters = JSON.parse(JSON.stringify({
    include: settings.filename_filters ? (settings.filename_filters.include || []) : [],
    exclude: settings.filename_filters ? (settings.filename_filters.exclude || []) : [],
  }));

  const esc = Gallery.utils.esc;
  const fmt = Gallery.utils.formatDate;

  // ── 1. Libraries ──────────────────────────────────────────────────────────
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

  // ── 2. Internal Library ───────────────────────────────────────────────────
  const il = settings.internal_library || {};
  const internalLibraryHtml = `<div class="settings-section" id="section-internal-library">
    <h2>Internal Library</h2>
    <div class="settings-row">
      <span class="label">Status</span>
      <span class="value">${il.enabled ? '<span class="pill pill-ok">enabled</span>' : '<span class="pill pill-warn">disabled</span>'}</span>
    </div>
    <div class="settings-row">
      <span class="label">Path</span>
      <span class="value">${esc(il.path || '—')}</span>
    </div>
  </div>`;

  // ── 3. Dropzone ───────────────────────────────────────────────────────────
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

  // ── 6. Scan Settings (editable) ──────────────────────────────────────────
  const scanSettingsHtml = `<div class="settings-section" id="section-scan-settings">
    <h2>Scan Settings</h2>
    <div class="settings-row">
      <span class="label">Scan workers</span>
      <input class="settings-input settings-input-sm" type="number" min="1"
             id="ss-scan-workers" value="${settings.scan_workers || 4}" />
    </div>
    <div class="settings-row">
      <span class="label">Event gap (days)</span>
      <input class="settings-input settings-input-sm" type="number" min="1"
             id="ss-event-gap" value="${settings.event_gap_days || 2}" />
    </div>
    <div class="settings-row">
      <span class="label">Event geo (km)</span>
      <input class="settings-input settings-input-sm" type="number" min="0.1" step="any"
             id="ss-event-geo" value="${settings.event_geo_km || 500}" />
    </div>
    <div class="settings-row">
      <span class="label">Session TTL (hours)</span>
      <input class="settings-input settings-input-sm" type="number" min="1"
             id="ss-session-ttl" value="${settings.session_ttl_hours || 24}" />
    </div>
    <div class="settings-save-row">
      <button class="scan-btn" onclick="Gallery.settings.saveScanSettings(this)">Save</button>
      <span id="ss-status" class="scan-status"></span>
    </div>
  </div>`;

  // ── 7. Logging ────────────────────────────────────────────────────────────
  const loggingHtml = `<div class="settings-section" id="section-logging">
    <h2>Logging</h2>
    <div class="settings-row">
      <span class="label">Log level</span>
      <select class="settings-input settings-input-sm" id="log-level">
        <option value="debug"${settings.log_level === 'debug' ? ' selected' : ''}>debug</option>
        <option value="info"${settings.log_level === 'info' ? ' selected' : ''}>info</option>
        <option value="warn"${settings.log_level === 'warn' ? ' selected' : ''}>warn</option>
        <option value="error"${settings.log_level === 'error' ? ' selected' : ''}>error</option>
      </select>
    </div>
    <div class="settings-row">
      <span class="label">Log file</span>
      <input class="settings-input" type="text" id="log-file"
             value="${esc(settings.log_file || '')}" placeholder="empty = stderr only" />
    </div>
    <div class="settings-save-row">
      <button class="scan-btn" onclick="Gallery.settings.saveLogging(this)">Save</button>
      <span id="log-status" class="scan-status"></span>
    </div>
    <div style="margin-top:8px;font-size:12px;color:var(--muted)">
      ⚠ Restart the server for log changes to take effect.
    </div>
  </div>`;

  // ── 8. Auth ───────────────────────────────────────────────────────────────
  const authData = settings.auth || {};
  const authHtml = `<div class="settings-section" id="section-auth">
    <h2>Authentication</h2>
    <div class="settings-row">
      <span class="label">Status</span>
      <span class="value">${authData.enabled ? '<span class="pill pill-ok">enabled</span>' : '<span class="pill pill-warn">disabled</span>'}</span>
      <button class="scan-btn" id="auth-toggle-btn"
              onclick="Gallery.settings.toggleAuth(this)">${authData.enabled ? 'Disable' : 'Enable'}</button>
    </div>
    <div id="auth-password-form" style="display:${authData.enabled ? 'block' : 'none'};margin-top:12px">
      <div class="settings-row">
        <span class="label">New password</span>
        <input class="settings-input" type="password" id="new-password"
               placeholder="Enter new password" />
      </div>
      <div class="settings-save-row">
        <button class="scan-btn" onclick="Gallery.settings.changePassword(this)">Change password</button>
        <span id="auth-pw-status" class="scan-status"></span>
      </div>
    </div>
  </div>`;

  // ── 9. Face Recognition ───────────────────────────────────────────────────
  const fr = settings.face_recognition || {};
  const faceRecognitionHtml = `<div class="settings-section" id="section-recognition">
    <h2>Face Recognition</h2>
    <div id="recognition-status-panel"><span style="color:var(--muted)">Loading…</span></div>
    <div style="margin-top:16px;border-top:1px solid var(--border);padding-top:12px">
      <h3>Configuration</h3>
      <div class="settings-row">
        <span class="label">Enabled</span>
        <label class="toggle-switch">
          <input type="checkbox" id="fr-enabled" ${fr.enabled ? 'checked' : ''} />
          <span class="toggle-slider"></span>
        </label>
      </div>
      <div class="settings-row">
        <span class="label">ONNX Runtime lib</span>
        <input class="settings-input" type="text" id="fr-onnxruntime-lib"
               value="${esc(fr.onnxruntime_lib || '')}" placeholder="path to libonnxruntime" />
      </div>
      <div class="settings-row">
        <span class="label">Model dir</span>
        <input class="settings-input" type="text" id="fr-model-dir"
               value="${esc(fr.model_dir || '')}" placeholder="directory with .onnx models" />
      </div>
      <div class="settings-row">
        <span class="label">Detection model</span>
        <input class="settings-input" type="text" id="fr-detection-model"
               value="${esc(fr.detection_model || '')}" placeholder="det_10g.onnx" />
      </div>
      <div class="settings-row">
        <span class="label">Recognition model</span>
        <input class="settings-input" type="text" id="fr-recognition-model"
               value="${esc(fr.recognition_model || '')}" placeholder="w600k_r50.onnx" />
      </div>
      <div class="settings-row">
        <span class="label">Detection threshold</span>
        <input class="settings-input settings-input-sm" type="number" min="0" max="1" step="0.01"
               id="fr-detection-threshold" value="${fr.detection_threshold ?? 0.5}" />
      </div>
      <div class="settings-row">
        <span class="label">Recognition threshold</span>
        <input class="settings-input settings-input-sm" type="number" min="0" max="1" step="0.01"
               id="fr-recognition-threshold" value="${fr.recognition_threshold ?? 0.4}" />
      </div>
      <div class="settings-row">
        <span class="label">Cluster min samples</span>
        <input class="settings-input settings-input-sm" type="number" min="1"
               id="fr-cluster-min-samples" value="${fr.cluster_min_samples ?? 2}" />
      </div>
      <div class="settings-save-row">
        <button class="scan-btn" onclick="Gallery.settings.saveFaceRecognition(this)">Save</button>
        <span id="fr-status" class="scan-status"></span>
      </div>
      <div style="margin-top:8px;font-size:12px;color:var(--muted)">
        ⚠ Restart the server for recognition changes to take effect.
      </div>
    </div>
  </div>`;

  // ── 10. System (read-only) ────────────────────────────────────────────────
  const systemHtml = `<div class="settings-section" id="section-system">
    <h2>System</h2>
    <div class="settings-row"><span class="label">DB path</span><span class="value">${esc(settings.db_path)}</span></div>
    <div class="settings-row"><span class="label">Cache dir</span><span class="value">${esc(settings.cache_dir)}</span></div>
  </div>`;

  // ── 11. Recent Scans ──────────────────────────────────────────────────────
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

  // ── Assemble page ─────────────────────────────────────────────────────────
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

    ${internalLibraryHtml}
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

    ${scanSettingsHtml}
    ${loggingHtml}
    ${authHtml}
    ${faceRecognitionHtml}
    ${systemHtml}

    <div class="settings-section">
      <h2>Recent Scans</h2>
      ${scanRunsHtml}
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
  Gallery.settings.loadRecognitionStatus();
};

// ── Settings module ───────────────────────────────────────────────────────────
Gallery.settings = {
  scanPollTimer: null,
  _whitelist: [],
  _filters: { include: [], exclude: [] },
  _issuesLoaded: false,
  _data: null,

  // ── Face Recognition Status ────────────────────────────────────────────────
  async loadRecognitionStatus() {
    const panel = document.getElementById('recognition-status-panel');
    if (!panel) return;
    try {
      const s = await Gallery.utils.api('/api/recognition/status');
      const esc = Gallery.utils.esc;
      let statusBadge, details = '';
      if (!s.enabled) {
        statusBadge = `<span class="pill pill-warn">disabled</span>`;
        details = `<div class="settings-row"><span class="label">Config</span>
          <span class="value" style="color:var(--muted)">Set <code>face_recognition.enabled = true</code> to activate</span></div>`;
      } else if (!s.available) {
        statusBadge = `<span class="pill pill-warn">unavailable</span>`;
        details = `<div class="settings-row"><span class="label">Reason</span>
          <span class="value" style="color:var(--danger)">${esc(s.reason || 'unknown')}</span></div>`;
      } else {
        const epColor = s.execution_provider === 'CPU' ? 'var(--warning, #f90)' : 'var(--ok, #4c4)';
        statusBadge = `<span class="pill pill-ok">active</span>`;
        details = `<div class="settings-row"><span class="label">Execution provider</span>
          <span class="value" style="color:${epColor}">${esc(s.execution_provider)}</span></div>`;
        if (s.execution_provider === 'CPU') {
          details += `<div class="settings-row"><span style="color:var(--warning,#f90);font-size:12px">
            ⚠ Running on CPU — face recognition will be slow for large libraries. Consider installing CUDA for GPU acceleration.</span></div>`;
        }
      }
      panel.innerHTML = `<div class="settings-row"><span class="label">Status</span>
        <span class="value">${statusBadge}</span></div>${details}`;
    } catch (e) {
      if (panel) panel.innerHTML = `<span style="color:var(--muted)">Unavailable: ${Gallery.utils.esc(e.message)}</span>`;
    }
  },

  // ── Scan Settings save ─────────────────────────────────────────────────────
  async saveScanSettings(btn) {
    btn.disabled = true;
    const statusEl = document.getElementById('ss-status');
    statusEl.textContent = 'Saving…';
    try {
      const scanWorkers = parseInt(document.getElementById('ss-scan-workers').value, 10);
      const eventGapDays = parseInt(document.getElementById('ss-event-gap').value, 10);
      const eventGeoKm = parseFloat(document.getElementById('ss-event-geo').value);
      const sessionTtlHours = parseInt(document.getElementById('ss-session-ttl').value, 10);
      const body = {};
      if (scanWorkers > 0) body.scan_workers = scanWorkers;
      if (eventGapDays > 0) body.event_gap_days = eventGapDays;
      if (eventGeoKm > 0) body.event_geo_km = eventGeoKm;
      if (sessionTtlHours > 0) body.session_ttl_hours = sessionTtlHours;
      await Gallery.utils.api('/api/settings', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
      });
      statusEl.textContent = 'Saved.';
    } catch (err) {
      statusEl.textContent = 'Error: ' + err.message;
    } finally {
      btn.disabled = false;
    }
  },

  // ── Logging save ───────────────────────────────────────────────────────────
  async saveLogging(btn) {
    btn.disabled = true;
    const statusEl = document.getElementById('log-status');
    statusEl.textContent = 'Saving…';
    try {
      const logLevel = document.getElementById('log-level').value;
      const logFile = document.getElementById('log-file').value;
      await Gallery.utils.api('/api/settings', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ log_level: logLevel, log_file: logFile }),
      });
      statusEl.textContent = 'Saved.';
    } catch (err) {
      statusEl.textContent = 'Error: ' + err.message;
    } finally {
      btn.disabled = false;
    }
  },

  // ── Auth toggle ────────────────────────────────────────────────────────────
  async toggleAuth(btn) {
    btn.disabled = true;
    const data = Gallery.settings._data;
    const newEnabled = !data.auth.enabled;
    try {
      await Gallery.utils.api('/api/settings', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ auth_enabled: newEnabled }),
      });
      data.auth.enabled = newEnabled;
      const badge = btn.parentElement.querySelector('.value');
      badge.innerHTML = newEnabled ? '<span class="pill pill-ok">enabled</span>' : '<span class="pill pill-warn">disabled</span>';
      btn.textContent = newEnabled ? 'Disable' : 'Enable';
      document.getElementById('auth-password-form').style.display = newEnabled ? 'block' : 'none';
    } catch (err) {
      alert('Error: ' + err.message);
    } finally {
      btn.disabled = false;
    }
  },

  // ── Password change ────────────────────────────────────────────────────────
  async changePassword(btn) {
    btn.disabled = true;
    const statusEl = document.getElementById('auth-pw-status');
    const pwInput = document.getElementById('new-password');
    const pw = pwInput.value;
    if (!pw) { pwInput.focus(); btn.disabled = false; return; }
    statusEl.textContent = 'Saving…';
    try {
      await Gallery.utils.api('/api/settings', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ new_password: pw }),
      });
      statusEl.textContent = 'Password changed.';
      pwInput.value = '';
    } catch (err) {
      statusEl.textContent = 'Error: ' + err.message;
    } finally {
      btn.disabled = false;
    }
  },

  // ── Face Recognition config save ───────────────────────────────────────────
  async saveFaceRecognition(btn) {
    btn.disabled = true;
    const statusEl = document.getElementById('fr-status');
    statusEl.textContent = 'Saving…';
    try {
      const body = {
        face_recognition: {
          enabled: document.getElementById('fr-enabled').checked,
          onnxruntime_lib: document.getElementById('fr-onnxruntime-lib').value,
          model_dir: document.getElementById('fr-model-dir').value,
          detection_model: document.getElementById('fr-detection-model').value,
          recognition_model: document.getElementById('fr-recognition-model').value,
          detection_threshold: parseFloat(document.getElementById('fr-detection-threshold').value) || 0.5,
          recognition_threshold: parseFloat(document.getElementById('fr-recognition-threshold').value) || 0.4,
          cluster_min_samples: parseInt(document.getElementById('fr-cluster-min-samples').value, 10) || 2,
        },
      };
      await Gallery.utils.api('/api/settings', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
      });
      statusEl.textContent = 'Saved.';
    } catch (err) {
      statusEl.textContent = 'Error: ' + err.message;
    } finally {
      btn.disabled = false;
    }
  },

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