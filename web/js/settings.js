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
  Gallery.settings._scanPaths = JSON.parse(JSON.stringify(settings.scan_paths || settings.library_paths || []));
  Gallery.settings._whitelist = JSON.parse(JSON.stringify(settings.camera_whitelist || []));
  Gallery.settings._filters = JSON.parse(JSON.stringify({
    include: settings.filename_filters ? (settings.filename_filters.include || []) : [],
    exclude: settings.filename_filters ? (settings.filename_filters.exclude || []) : [],
  }));

  const esc = Gallery.utils.esc;
  const fmt = Gallery.utils.formatDate;

  // ── 1. Scan Directories ───────────────────────────────────────────────────

  // ── 2. Internal Library ───────────────────────────────────────────────────
  const il = settings.internal_library || {};
  const internalLibraryHtml = `<div class="settings-section" id="section-internal-library">
    <h2>Internal Library</h2>
    <div class="settings-row">
      <span class="label">Enabled</span>
      <input type="checkbox" id="il-enabled" ${il.enabled ? 'checked' : ''} />
    </div>
    <div class="settings-row">
      <span class="label">Path</span>
      <input class="settings-input" type="text" id="il-path"
             value="${esc(il.path || '')}" placeholder="/path/to/internal-library" />
    </div>
    <div class="settings-row settings-note-row">
      <span class="value">Only one internal library path is supported at a time.</span>
    </div>
    <div class="settings-save-row">
      <button class="scan-btn" onclick="Gallery.settings.saveInternalLibrary(this)">Save Internal Library</button>
      <span id="il-status" class="scan-status"></span>
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
      <h2>Scan Directories</h2>
      <div id="scan-paths-list"></div>
      <div class="settings-add-row">
        <input id="sp-label" class="settings-input" placeholder="Label (e.g. Family Photos)" />
        <input id="sp-path" class="settings-input" placeholder="Path (e.g. /home/you/Pictures)" />
        <button class="settings-add-btn" onclick="Gallery.settings.scanPathAdd()">Add</button>
      </div>
      <div class="scan-status">Tip: Press Enter in either field to add quickly.</div>
      <div class="settings-save-row">
        <button class="scan-btn" onclick="Gallery.settings.saveScanPaths(this)">Save Scan Directories</button>
        <span id="sp-status" class="scan-status"></span>
      </div>
      <div id="path-conflict-warning" class="settings-warning" style="display:none"></div>
      <div style="margin-top:12px;padding-top:12px;border-top:1px solid var(--border)">
        <button class="scan-btn" onclick="Gallery.settings.triggerScanAll(this)">Scan All Directories</button>
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
  Gallery.settings.renderScanPaths(scanStatus.last_runs || []);
  Gallery.settings.setupScanPathInputs();
  Gallery.settings.setupInternalLibraryInputs();
  Gallery.settings.updatePathConflictWarnings();
  Gallery.settings.renderFilters();
  Gallery.settings.loadRecognitionStatus();
};

// ── Settings module ───────────────────────────────────────────────────────────
Gallery.settings = {
  scanPollTimer: null,
  _whitelist: [],
  _scanPaths: [],
  _filters: { include: [], exclude: [] },
  _issuesLoaded: false,
  _data: null,

  // ── Scan directories ─────────────────────────────────────────────────────
  renderScanPaths(lastRuns) {
    const list = document.getElementById('scan-paths-list');
    if (!list) return;

    const runs = Array.isArray(lastRuns) ? lastRuns : [];
    if (this._scanPaths.length === 0) {
      list.innerHTML = `<div class="settings-row"><span style="color:var(--muted)">No scan directories configured</span></div>`;
      return;
    }

    list.innerHTML = this._scanPaths.map((sp, i) => {
      const label = (sp.label || '').trim();
      const run = runs.find(r => r.library_label === label);
      const lastScan = run
        ? `Last scan: ${Gallery.utils.formatDate(run.started_at)} — ${run.files_ingested} ingested, ${run.files_duplicate} dupes`
        : 'Never scanned';
      return `<div class="settings-row">
        <input class="settings-input" value="${Gallery.utils.esc(sp.label || '')}"
               placeholder="Label" onchange="Gallery.settings.scanPathUpdate(${i}, 'label', this.value)" />
        <input class="settings-input" value="${Gallery.utils.esc(sp.path || '')}"
               placeholder="Path" onchange="Gallery.settings.scanPathUpdate(${i}, 'path', this.value)" />
        <button class="settings-del-btn" onclick="Gallery.settings.scanPathRemove(${i})" title="Remove">Remove</button>
      </div>
      <div class="scan-status">${Gallery.utils.esc(lastScan)}</div>`;
    }).join('');
  },

  setupScanPathInputs() {
    const addOnEnter = (e) => {
      if (e.key === 'Enter') {
        e.preventDefault();
        this.scanPathAdd();
      }
    };
    const labelEl = document.getElementById('sp-label');
    const pathEl = document.getElementById('sp-path');
    if (labelEl) labelEl.addEventListener('keydown', addOnEnter);
    if (pathEl) pathEl.addEventListener('keydown', addOnEnter);
  },

  setupInternalLibraryInputs() {
    const ilPathEl = document.getElementById('il-path');
    const ilEnabledEl = document.getElementById('il-enabled');
    if (ilPathEl) ilPathEl.addEventListener('input', () => this.updatePathConflictWarnings());
    if (ilEnabledEl) ilEnabledEl.addEventListener('change', () => this.updatePathConflictWarnings());
  },

  pathsOverlap(a, b) {
    const norm = (p) => {
      let x = (p || '').trim();
      if (!x) return '';
      x = x.replace(/\\/g, '/');
      x = x.replace(/\/+$/, '');
      return x.toLowerCase();
    };
    const na = norm(a);
    const nb = norm(b);
    if (!na || !nb) return false;
    return na === nb || na.startsWith(nb + '/') || nb.startsWith(na + '/');
  },

  updatePathConflictWarnings() {
    const warnEl = document.getElementById('path-conflict-warning');
    if (!warnEl) return;
    const ilEnabledEl = document.getElementById('il-enabled');
    const ilPathEl = document.getElementById('il-path');
    const ilEnabled = !!(ilEnabledEl && ilEnabledEl.checked);
    const ilPath = (ilPathEl && ilPathEl.value) ? ilPathEl.value.trim() : '';
    if (!ilEnabled || !ilPath) {
      warnEl.style.display = 'none';
      warnEl.textContent = '';
      return;
    }

    const conflicts = this._scanPaths.filter(sp => this.pathsOverlap(sp.path, ilPath));
    if (conflicts.length === 0) {
      warnEl.style.display = 'none';
      warnEl.textContent = '';
      return;
    }

    const labels = conflicts.map(sp => (sp.label || sp.path || '').trim() || '(unnamed)');
    warnEl.style.display = 'block';
    warnEl.textContent = `Path conflict: internal library overlaps scan path(s): ${labels.join(', ')}. Save will fail until paths do not overlap.`;
  },

  scanPathAdd() {
    const labelEl = document.getElementById('sp-label');
    const pathEl = document.getElementById('sp-path');
    const label = (labelEl.value || '').trim();
    const path = (pathEl.value || '').trim();
    if (!path) {
      pathEl.focus();
      return;
    }
    const dupPath = this._scanPaths.some(sp => (sp.path || '').trim().toLowerCase() === path.toLowerCase());
    if (dupPath) {
      pathEl.focus();
      pathEl.select();
      return;
    }
    this._scanPaths.push({ label, path });
    labelEl.value = '';
    pathEl.value = '';
    labelEl.focus();
    this.renderScanPaths();
    this.updatePathConflictWarnings();
  },

  scanPathUpdate(i, field, value) {
    if (!this._scanPaths[i]) return;
    this._scanPaths[i][field] = (value || '').trim();
    if (field === 'path') this.updatePathConflictWarnings();
  },

  scanPathRemove(i) {
    this._scanPaths.splice(i, 1);
    this.renderScanPaths();
    this.updatePathConflictWarnings();
  },

  async saveScanPaths(btn) {
    btn.disabled = true;
    const statusEl = document.getElementById('sp-status');
    statusEl.textContent = 'Saving…';
    try {
      const cleaned = this._scanPaths
        .map(sp => ({ label: (sp.label || '').trim(), path: (sp.path || '').trim() }))
        .filter(sp => sp.path !== '');
      const seen = new Set();
      const deduped = [];
      for (const sp of cleaned) {
        const key = sp.path.toLowerCase();
        if (seen.has(key)) continue;
        seen.add(key);
        deduped.push(sp);
      }
      await Gallery.utils.api('/api/settings', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ scan_paths: deduped }),
      });
      this._scanPaths = deduped;
      this.renderScanPaths();
      statusEl.textContent = 'Saved.';
    } catch (err) {
      statusEl.textContent = 'Error: ' + err.message;
    } finally {
      btn.disabled = false;
    }
  },

  async saveInternalLibrary(btn) {
    btn.disabled = true;
    const statusEl = document.getElementById('il-status');
    statusEl.textContent = 'Saving…';
    try {
      const enabled = document.getElementById('il-enabled').checked;
      const path = (document.getElementById('il-path').value || '').trim();
      await Gallery.utils.api('/api/settings', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          internal_library: {
            enabled,
            path,
          },
        }),
      });
      statusEl.textContent = 'Saved.';
      this.updatePathConflictWarnings();
    } catch (err) {
      statusEl.textContent = 'Error: ' + err.message;
    } finally {
      btn.disabled = false;
    }
  },

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

        // Queue status.
        const processing = s.queue_processing ? `Processing photo #${s.queue_processing}` : 'Idle';
        details += `<div class="settings-row"><span class="label">Queue</span>
          <span class="value">${esc(s.queue_queued || 0)} queued, ${esc(s.queue_done || 0)} done, ${esc(s.queue_errors || 0)} errors</span></div>
          <div class="settings-row"><span class="label">Worker</span><span class="value">${esc(processing)}</span></div>`;
      }

      // Reprocess button.
      const reprocessBtn = (s.available) ? `
        <div class="settings-row" style="margin-top:8px">
          <button class="scan-btn" id="reprocess-all-btn"
                  onclick="Gallery.settings.reprocessAll(this)">Reprocess All Photos</button>
          <span id="reprocess-all-status" class="scan-status"></span>
        </div>
        <div class="settings-row" style="font-size:12px;color:var(--muted)">
          Queues all photos needing face detection in captured_at order. Safe to run multiple times.
        </div>` : '';

      panel.innerHTML = `<div class="settings-row"><span class="label">Status</span>
        <span class="value">${statusBadge}</span></div>${details}${reprocessBtn}`;
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

  // ── Reprocess all photos for face detection ─────────────────────────────────
  async reprocessAll(btn) {
    btn.disabled = true;
    const statusEl = document.getElementById('reprocess-all-status');
    if (statusEl) statusEl.textContent = 'Enqueuing…';
    try {
      const resp = await Gallery.utils.api('/api/recognition/reprocess-all', {
        method: 'POST',
      });
      if (statusEl) {
        statusEl.textContent = `Queued ${resp.queued} of ${resp.total_eligible} eligible photos.`;
      }
    } catch (err) {
      if (statusEl) statusEl.textContent = 'Error: ' + err.message;
    } finally {
      btn.disabled = false;
      // Refresh status after a moment.
      setTimeout(() => this.loadRecognitionStatus(), 1500);
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
          const parts = [
            `found:${s.found || 0}`,
            `skipped:${s.skipped || 0}`,
            `ingested:${s.ingested || 0}`,
            `dupes:${s.duplicate || 0}`,
            `errors:${s.errors || 0}`,
          ];
          if ((s.auto_staged || 0) > 0 || status.current_label === 'Dropzone') {
            parts.push(`auto-staged:${s.auto_staged || 0}`);
          }
          const msg = `Running${status.current_label ? ' — ' + status.current_label : ''}: ${parts.join(' ')}…`;
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