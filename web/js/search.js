// Search / filter view.
window.Gallery = window.Gallery || {};
Gallery.pages = Gallery.pages || {};

Gallery.pages.search = async function(params) {
  const app = document.getElementById('app');
  Gallery.utils.setActiveNav('/search');

  // Parse current URL query string into filter state.
  const sp = new URLSearchParams(location.search);
  const state = {
    q:       sp.get('q')       || '',
    from:    sp.get('from')    || '',
    to:      sp.get('to')      || '',
    make:    sp.get('make')    || '',
    model:   sp.get('model')   || '',
    has_gps: sp.get('has_gps') || '',
    flag:    sp.get('flag')    || '',
    page:    parseInt(sp.get('page') || '1', 10),
  };

  app.innerHTML = `
  <div class="search-page">
    <form class="search-filters" id="search-form" autocomplete="off">
      <div class="filter-row">
        <div class="filter-field filter-field--wide">
          <label>Keyword</label>
          <input type="text" name="q" value="${Gallery.utils.esc(state.q)}" placeholder="filename or path…">
        </div>
        <div class="filter-field">
          <label>From</label>
          <input type="date" name="from" value="${Gallery.utils.esc(state.from)}">
        </div>
        <div class="filter-field">
          <label>To</label>
          <input type="date" name="to" value="${Gallery.utils.esc(state.to)}">
        </div>
        <div class="filter-field">
          <label>Camera make</label>
          <input type="text" name="make" value="${Gallery.utils.esc(state.make)}" placeholder="e.g. Canon">
        </div>
        <div class="filter-field">
          <label>Camera model</label>
          <input type="text" name="model" value="${Gallery.utils.esc(state.model)}" placeholder="e.g. EOS 700D">
        </div>
        <div class="filter-field">
          <label>GPS</label>
          <select name="has_gps">
            <option value="">Any</option>
            <option value="true"  ${state.has_gps === 'true'  ? 'selected' : ''}>With GPS</option>
            <option value="false" ${state.has_gps === 'false' ? 'selected' : ''}>Without GPS</option>
          </select>
        </div>
        <div class="filter-field">
          <label>Flag</label>
          <select name="flag">
            <option value="">Any</option>
            <option value="missing_gps"  ${state.flag === 'missing_gps'  ? 'selected' : ''}>missing_gps</option>
            <option value="missing_date" ${state.flag === 'missing_date' ? 'selected' : ''}>missing_date</option>
          </select>
        </div>
        <div class="filter-field filter-field--action">
          <label>&nbsp;</label>
          <button type="submit" class="btn-primary">Search</button>
        </div>
      </div>
    </form>
    <div id="search-results" class="search-results">
      <div class="loading">Enter a filter and press Search.</div>
    </div>
  </div>`;

  const form = document.getElementById('search-form');
  form.addEventListener('submit', e => {
    e.preventDefault();
    const fd = new FormData(form);
    const newSp = new URLSearchParams();
    for (const [k, v] of fd.entries()) {
      if (v) newSp.set(k, v);
    }
    newSp.set('page', '1');
    history.pushState(null, '', '/search?' + newSp.toString());
    Gallery.search.load(newSp);
  });

  // Auto-load if URL already has filters.
  if ([...sp.values()].some(v => v && v !== '1')) {
    Gallery.search.load(sp);
  }
};

Gallery.search = {
  async load(sp) {
    const resultsEl = document.getElementById('search-results');
    if (!resultsEl) return;
    resultsEl.innerHTML = '<div class="loading">Searching…</div>';

    const apiSp = new URLSearchParams(sp);
    apiSp.set('per_page', '60');

    let data;
    try {
      data = await Gallery.utils.api('/api/photos?' + apiSp.toString());
    } catch (e) {
      resultsEl.innerHTML = `<div class="empty"><p>Error: ${Gallery.utils.esc(e.message)}</p></div>`;
      return;
    }

    if (!data.items.length) {
      resultsEl.innerHTML = '<div class="empty"><div class="empty-icon">🔍</div><p>No photos match these filters.</p></div>';
      return;
    }

    const page    = parseInt(sp.get('page') || '1', 10);
    const perPage = 60;
    const total   = data.total;
    const pages   = Math.ceil(total / perPage);

    const grid = data.items.map(p => {
      const hasFlags = p.flags && p.flags.length > 0;
      return `<div class="photo-card" onclick="Gallery.utils.navigate('/photo/${Gallery.utils.esc(p.sha256)}')">
        <img loading="lazy" src="${Gallery.utils.esc(p.thumbnail_url)}" alt="${Gallery.utils.esc(p.filename)}">
        <div class="photo-overlay">${Gallery.utils.esc(p.filename)}</div>
        ${hasFlags ? '<div class="flag-dot"></div>' : ''}
      </div>`;
    }).join('');

    let pagination = '';
    if (pages > 1) {
      const prevSp = new URLSearchParams(sp); prevSp.set('page', String(Math.max(1, page - 1)));
      const nextSp = new URLSearchParams(sp); nextSp.set('page', String(Math.min(pages, page + 1)));
      pagination = `<div class="pagination">
        ${page > 1 ? `<button onclick="Gallery.search.goPage('${prevSp}')">‹ Prev</button>` : '<button disabled>‹ Prev</button>'}
        <span>${page} / ${pages} &nbsp;<span style="color:var(--muted)">(${total} photos)</span></span>
        ${page < pages ? `<button onclick="Gallery.search.goPage('${nextSp}')">Next ›</button>` : '<button disabled>Next ›</button>'}
      </div>`;
    } else {
      pagination = `<div class="result-count">${total} photo${total !== 1 ? 's' : ''}</div>`;
    }

    resultsEl.innerHTML = pagination + `<div class="photo-grid">${grid}</div>`;
  },

  goPage(spStr) {
    const sp = new URLSearchParams(spStr);
    history.pushState(null, '', '/search?' + sp.toString());
    Gallery.search.load(sp);
  },
};
