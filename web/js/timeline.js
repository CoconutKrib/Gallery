// Timeline view using Plotly bar chart.
window.Gallery = window.Gallery || {};
Gallery.pages = Gallery.pages || {};

Gallery.pages.timeline = async function() {
  const app = document.getElementById('app');
  Gallery.utils.setActiveNav('/timeline');

  const sp = new URLSearchParams(location.search);
  const zoom  = sp.get('zoom')  || 'year';
  const from  = sp.get('from')  || '';
  const to    = sp.get('to')    || '';

  app.innerHTML = `
  <div class="timeline-page">
    <div class="timeline-header">
      <h1>Timeline</h1>
      <div class="zoom-controls">
        ${['decade','year','month','week','day'].map(z =>
          `<button class="zoom-btn ${z === zoom ? 'active' : ''}"
            onclick="Gallery.timeline.setZoom('${z}')">${z}</button>`
        ).join('')}
      </div>
      <div class="timeline-range">
        <label>From <input type="date" id="tl-from" value="${Gallery.utils.esc(from)}"></label>
        <label>To   <input type="date" id="tl-to"   value="${Gallery.utils.esc(to)}"></label>
        <button class="btn-primary" onclick="Gallery.timeline.applyRange()">Apply</button>
      </div>
    </div>
    <div id="timeline-chart" class="timeline-chart">
      <div class="loading">Loading…</div>
    </div>
    <div id="timeline-info" class="timeline-info"></div>
    <div id="timeline-grid" class="timeline-grid"></div>
  </div>`;

  await Gallery.timeline.load(zoom, from, to);
};

Gallery.timeline = {
  currentZoom: 'year',
  currentFrom: '',
  currentTo:   '',

  setZoom(zoom) {
    const sp = new URLSearchParams(location.search);
    sp.set('zoom', zoom);
    history.pushState(null, '', '/timeline?' + sp.toString());
    Gallery.timeline.load(zoom, sp.get('from') || '', sp.get('to') || '');
    // Update button states.
    document.querySelectorAll('.zoom-btn').forEach(b => {
      b.classList.toggle('active', b.textContent === zoom);
    });
    this.currentZoom = zoom;
  },

  applyRange() {
    const from = document.getElementById('tl-from')?.value || '';
    const to   = document.getElementById('tl-to')?.value   || '';
    const sp   = new URLSearchParams(location.search);
    if (from) sp.set('from', from); else sp.delete('from');
    if (to)   sp.set('to', to);     else sp.delete('to');
    history.pushState(null, '', '/timeline?' + sp.toString());
    Gallery.timeline.load(sp.get('zoom') || 'year', from, to);
  },

  async load(zoom, from, to) {
    const chartEl = document.getElementById('timeline-chart');
    if (!chartEl) return;
    chartEl.innerHTML = '<div class="loading">Loading…</div>';

    const params = new URLSearchParams({ zoom });
    if (from) params.set('from', from);
    if (to)   params.set('to', to);

    let data;
    try {
      data = await Gallery.utils.api('/api/timeline?' + params.toString());
    } catch (e) {
      chartEl.innerHTML = `<div class="empty"><p>Error: ${Gallery.utils.esc(e.message)}</p></div>`;
      return;
    }

    this.currentZoom = zoom;
    this.currentFrom = from;
    this.currentTo   = to;

    if (!data.buckets.length) {
      chartEl.innerHTML = '<div class="empty"><div class="empty-icon">📅</div><p>No dated photos in this range.</p></div>';
      const info = document.getElementById('timeline-info');
      if (info && data.undated > 0) {
        info.innerHTML = `<span class="result-count">${data.undated} photo${data.undated !== 1 ? 's' : ''} have no date and are not shown.</span>`;
      }
      return;
    }

    const labels = data.buckets.map(b => b.label);
    const counts = data.buckets.map(b => b.count);

    // Render Plotly chart.
    chartEl.innerHTML = '';
    Plotly.newPlot(chartEl, [{
      type: 'bar',
      x: labels,
      y: counts,
      marker: { color: '#4f8ef7', opacity: 0.85 },
      hovertemplate: '%{x}<br>%{y} photos<extra></extra>',
    }], {
      paper_bgcolor: 'transparent',
      plot_bgcolor: 'transparent',
      font: { color: '#e2e4e9', family: '-apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,sans-serif', size: 12 },
      margin: { t: 16, b: 48, l: 48, r: 16 },
      xaxis: {
        tickfont: { color: '#8b9099' },
        gridcolor: '#2e3138',
        linecolor: '#2e3138',
      },
      yaxis: {
        tickfont: { color: '#8b9099' },
        gridcolor: '#2e3138',
        linecolor: '#2e3138',
        title: 'Photos',
        titlefont: { color: '#8b9099' },
      },
      bargap: 0.15,
    }, {
      displayModeBar: false,
      responsive: true,
    });

    // Click a bar → load that time window in the grid below.
    chartEl.on('plotly_click', evt => {
      const pt = evt.points[0];
      const bucket = data.buckets[pt.pointIndex];
      Gallery.timeline.loadGrid(bucket);
    });

    // Info line.
    const infoEl = document.getElementById('timeline-info');
    if (infoEl) {
      let msg = `${data.total} dated photo${data.total !== 1 ? 's' : ''}`;
      if (data.undated > 0) msg += ` · ${data.undated} undated (hidden)`;
      msg += ` · Click a bar to browse photos`;
      infoEl.innerHTML = `<span class="result-count">${msg}</span>`;
    }

    document.getElementById('timeline-grid').innerHTML = '';
  },

  async loadGrid(bucket) {
    const gridEl = document.getElementById('timeline-grid');
    if (!gridEl) return;
    gridEl.innerHTML = '<div class="loading">Loading…</div>';

    // Show a heading for the selected bucket.
    const fromDate = bucket.from ? bucket.from.slice(0, 10) : '';
    const toDate   = bucket.to   ? bucket.to.slice(0, 10)   : '';

    const params = new URLSearchParams({ per_page: '60' });
    if (fromDate) params.set('from', fromDate);
    if (toDate)   params.set('to', toDate);

    let data;
    try {
      data = await Gallery.utils.api('/api/photos?' + params.toString());
    } catch (e) {
      gridEl.innerHTML = `<div class="empty"><p>Error: ${Gallery.utils.esc(e.message)}</p></div>`;
      return;
    }

    if (!data.items.length) {
      gridEl.innerHTML = '<div class="empty"><p>No photos in this period.</p></div>';
      return;
    }

    const grid = data.items.map(p => {
      const hasFlags = p.flags && p.flags.length > 0;
      return `<div class="photo-card" onclick="Gallery.utils.navigate('/photo/${Gallery.utils.esc(p.sha256)}')">
        <img loading="lazy" src="${Gallery.utils.esc(p.thumbnail_url)}" alt="${Gallery.utils.esc(p.filename)}">
        <div class="photo-overlay">${Gallery.utils.esc(p.filename)}</div>
        ${hasFlags ? '<div class="flag-dot"></div>' : ''}
      </div>`;
    }).join('');

    const searchLink = fromDate
      ? `/search?from=${encodeURIComponent(fromDate)}&to=${encodeURIComponent(toDate)}`
      : '/search';

    gridEl.innerHTML = `
      <div class="timeline-grid-header">
        <strong>${Gallery.utils.esc(bucket.label)}</strong>
        <span style="color:var(--muted)">${data.total} photo${data.total !== 1 ? 's' : ''}</span>
        ${data.total > 60 ? `<a href="${Gallery.utils.esc(searchLink)}" data-link>View all in Search →</a>` : ''}
      </div>
      <div class="photo-grid">${grid}</div>`;

    gridEl.scrollIntoView({ behavior: 'smooth', block: 'start' });
  },
};
