// Settings page.
window.Gallery = window.Gallery || {};
Gallery.pages = Gallery.pages || {};

Gallery.pages.settings = async function() {
  const app = document.getElementById('app');
  Gallery.utils.setActiveNav('/settings');
  app.innerHTML = `<div class="settings-page"><h1>Settings</h1><div class="loading">Loading…</div></div>`;

  let settings, scanStatus;
  try {
    [settings, scanStatus] = await Promise.all([
      Gallery.utils.api('/api/settings'),
      Gallery.utils.api('/api/scan/status'),
    ]);
  } catch (e) {
    app.querySelector('.loading').textContent = 'Failed to load settings.';
    return;
  }

  const e = Gallery.utils.esc;
  const fmt = Gallery.utils.formatDate;

  // Build library paths section with per-path scan buttons.
  const libraryRows = (settings.library_paths || []).map(lp => {
    const run = (scanStatus.last_runs || []).find(r => r.library_label === lp.label);
    const lastScan = run ? `Last scan: ${fmt(run.started_at)} — ${run.files_ingested} ingested, ${run.files_duplicate} dupes` : 'Never scanned';
    return `<div class="settings-row">
      <span class="label">${e(lp.label || lp.path)}</span>
      <span class="value" style="font-size:12px;color:var(--muted)">${e(lp.path)}</span>
      <button class="scan-btn" data-path-label="${e(lp.label)}" onclick="Gallery.settings.triggerScan(this)">Scan</button>
    </div>
    <div class="scan-status" id="scan-status-${e(lp.label)}">${e(lastScan)}</div>`;
  }).join('');

  // Camera whitelist.
  const whitelistRows = (settings.camera_whitelist || []).map(c =>
    `<div class="settings-row">
      <span class="label">${e(c.make)}</span>
      <span class="value">${e(c.model || 'All models')}</span>
    </div>`
  ).join('') || `<div class="settings-row"><span style="color:var(--muted)">All cameras accepted (whitelist empty)</span></div>`;

  // Scan status block.
  const scanRunsHtml = (scanStatus.last_runs || []).map(r =>
    `<div class="settings-row">
      <span class="label">${e(r.library_label)}</span>
      <span class="value" style="font-size:12px">
        ${fmt(r.started_at)} — found:${r.files_found} ingested:${r.files_ingested}
        dupes:${r.files_duplicate} errors:${r.files_error}
        ${r.finished_at ? '' : ' <span class="pill">running</span>'}
      </span>
    </div>`
  ).join('') || `<div class="settings-row"><span style="color:var(--muted)">No scans yet</span></div>`;

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

    <div class="settings-section">
      <h2>Camera Whitelist</h2>
      ${whitelistRows}
    </div>

    <div class="settings-section">
      <h2>Filename Filters</h2>
      <div class="settings-row">
        <span class="label">Include</span>
        <span class="value">${(settings.filename_filters.include || []).map(e).join(', ') || '<span style="color:var(--muted)">None (accept all)</span>'}</span>
      </div>
      <div class="settings-row">
        <span class="label">Exclude</span>
        <span class="value">${(settings.filename_filters.exclude || []).map(e).join(', ') || '<span style="color:var(--muted)">None</span>'}</span>
      </div>
    </div>

    <div class="settings-section">
      <h2>Recent Scans</h2>
      ${scanRunsHtml}
    </div>

    <div class="settings-section">
      <h2>Configuration</h2>
      <div class="settings-row"><span class="label">DB path</span><span class="value">${e(settings.db_path)}</span></div>
      <div class="settings-row"><span class="label">Cache dir</span><span class="value">${e(settings.cache_dir)}</span></div>
      <div class="settings-row"><span class="label">Scan workers</span><span class="value">${settings.scan_workers}</span></div>
      <div class="settings-row"><span class="label">Event gap (days)</span><span class="value">${settings.event_gap_days}</span></div>
      <div class="settings-row"><span class="label">Event geo (km)</span><span class="value">${settings.event_geo_km}</span></div>
    </div>
  </div>`;
};

Gallery.settings = {
  scanPollTimer: null,

  async triggerScan(btn) {
    // Find library_path_id by label — poll /api/libraries.
    btn.disabled = true;
    const label = btn.dataset.pathLabel;
    const statusEl = document.getElementById('scan-status-' + label);
    if (statusEl) statusEl.textContent = 'Starting scan…';
    try {
      await Gallery.utils.api('/api/scan', { method: 'POST', body: JSON.stringify({}), headers: { 'Content-Type': 'application/json' } });
      Gallery.settings.pollScanStatus(label, statusEl, btn);
    } catch (e) {
      if (statusEl) statusEl.textContent = 'Error: ' + e.message;
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
    } catch (e) {
      statusEl.textContent = 'Error: ' + e.message;
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
