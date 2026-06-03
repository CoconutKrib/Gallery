// Search / filter view.
window.Gallery = window.Gallery || {};
Gallery.pages = Gallery.pages || {};

Gallery.pages.search = async function(params) {
  const app = document.getElementById('app');
  Gallery.utils.setActiveNav('/search');

  // Parse current URL query string into filter state.
  const sp = new URLSearchParams(location.search);
  const state = {
    q:                        sp.get('q') || '',
    from:                     sp.get('from') || '',
    to:                       sp.get('to') || '',
    make:                     sp.get('make') || '',
    model:                    sp.get('model') || '',
    has_gps:                  sp.get('has_gps') || '',
    flag:                     sp.get('flag') || '',
    source:                   sp.get('source') || '',
    in_library:               sp.get('in_library') || 'true',
    tag:                      sp.get('tag') || '',
    event_id:                 sp.get('event_id') || '',
    has_date_override:        sp.get('has_date_override') || '',
    true_date_unknown:        sp.get('true_date_unknown') || '',
    person_id:                sp.get('person_id') || '',
    face_verified:            sp.get('face_verified') || 'true',
    face_source:              sp.get('face_source') || '',
    include_unverified_faces: sp.get('include_unverified_faces') || 'false',
    order:                    sp.get('order') || 'captured_at_desc',
    page:                     parseInt(sp.get('page') || '1', 10),
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
          <label>Scope</label>
          <select name="in_library">
            <option value="true" ${state.in_library === 'true' ? 'selected' : ''}>Library only</option>
            <option value="false" ${state.in_library === 'false' ? 'selected' : ''}>Not in library</option>
            <option value="any" ${state.in_library === 'any' ? 'selected' : ''}>Any</option>
          </select>
        </div>
        <div class="filter-field">
          <label>Source</label>
          <select name="source">
            <option value="">Any</option>
            <option value="scan" ${state.source === 'scan' ? 'selected' : ''}>Scan</option>
            <option value="dropzone" ${state.source === 'dropzone' ? 'selected' : ''}>Dropzone</option>
          </select>
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
        <div class="filter-field">
          <label>Tag</label>
          <input type="text" name="tag" value="${Gallery.utils.esc(state.tag)}" placeholder="e.g. favorite">
        </div>
        <div class="filter-field">
          <label>Event ID</label>
          <input type="number" name="event_id" min="1" step="1" value="${Gallery.utils.esc(state.event_id)}" placeholder="e.g. 7">
        </div>
        <div class="filter-field">
          <label>Date override</label>
          <select name="has_date_override">
            <option value="">Any</option>
            <option value="true" ${state.has_date_override === 'true' ? 'selected' : ''}>Has override</option>
            <option value="false" ${state.has_date_override === 'false' ? 'selected' : ''}>No override</option>
          </select>
        </div>
        <div class="filter-field">
          <label>Date unknown</label>
          <select name="true_date_unknown">
            <option value="">Any</option>
            <option value="true" ${state.true_date_unknown === 'true' ? 'selected' : ''}>Yes</option>
            <option value="false" ${state.true_date_unknown === 'false' ? 'selected' : ''}>No</option>
          </select>
        </div>
        <div class="filter-field filter-field--wide">
          <label>Person</label>
          <input type="text" id="search-person" list="search-people-list" placeholder="Type person name...">
          <datalist id="search-people-list"></datalist>
          <input type="hidden" name="person_id" id="search-person-id" value="${Gallery.utils.esc(state.person_id)}">
        </div>
        <div class="filter-field">
          <label>Face verified</label>
          <select name="face_verified">
            <option value="any" ${state.face_verified === 'any' ? 'selected' : ''}>Any</option>
            <option value="true" ${state.face_verified === 'true' ? 'selected' : ''}>Confirmed</option>
            <option value="false" ${state.face_verified === 'false' ? 'selected' : ''}>Unverified</option>
          </select>
        </div>
        <div class="filter-field">
          <label>Face source</label>
          <select name="face_source">
            <option value="">Any</option>
            <option value="manual" ${state.face_source === 'manual' ? 'selected' : ''}>Manual</option>
            <option value="auto" ${state.face_source === 'auto' ? 'selected' : ''}>Auto</option>
          </select>
        </div>
        <div class="filter-field">
          <label>Include unverified</label>
          <select name="include_unverified_faces">
            <option value="false" ${state.include_unverified_faces === 'false' ? 'selected' : ''}>No</option>
            <option value="true" ${state.include_unverified_faces === 'true' ? 'selected' : ''}>Yes</option>
          </select>
        </div>
        <div class="filter-field">
          <label>Sort</label>
          <select name="order">
            <option value="captured_at_desc" ${state.order === 'captured_at_desc' ? 'selected' : ''}>Date desc</option>
            <option value="captured_at_asc" ${state.order === 'captured_at_asc' ? 'selected' : ''}>Date asc</option>
            <option value="filename" ${state.order === 'filename' ? 'selected' : ''}>Filename</option>
          </select>
        </div>
        <div class="filter-field filter-field--action">
          <label>&nbsp;</label>
          <button type="submit" class="btn-primary">Search</button>
        </div>
        <div class="filter-field filter-field--action">
          <label>&nbsp;</label>
          <button type="button" class="btn" onclick="Gallery.search.clearAll()">Clear all</button>
        </div>
      </div>
    </form>
    <div id="search-active-filters" class="result-count"></div>
    <div id="search-results" class="search-results">
      <div class="loading">Loading...</div>
    </div>
  </div>`;

  await Gallery.search.initPeoplePicker(state.person_id);
  Gallery.search.renderActiveFilters(new URLSearchParams(location.search));

  const form = document.getElementById('search-form');
  form.addEventListener('submit', e => {
    e.preventDefault();
    const fd = new FormData(form);

    // Resolve typed person to id.
    const typedName = (document.getElementById('search-person') || {}).value || '';
    const personID = Gallery.search.resolvePersonID(typedName.trim());
    if (personID) {
      fd.set('person_id', String(personID));
    } else {
      fd.delete('person_id');
    }

    const newSp = new URLSearchParams();
    for (const [k, v] of fd.entries()) {
      if (!v) continue;

      // Keep explicit defaults for stable behavior.
      if (k === 'in_library' && v === 'true') { newSp.set(k, v); continue; }
      if (k === 'face_verified' && v === 'true') { newSp.set(k, v); continue; }
      if (k === 'include_unverified_faces' && v === 'false') { newSp.set(k, v); continue; }

      // Drop semantic "any" values.
      if ((k === 'in_library' || k === 'face_verified') && v === 'any') continue;
      if (k === 'order' && v === 'captured_at_desc') continue;

      newSp.set(k, v);
    }
    newSp.set('page', '1');
    history.pushState(null, '', '/search?' + newSp.toString());
    Gallery.search.renderActiveFilters(newSp);
    Gallery.search.load(newSp);
  });

  // Always auto-load; defaults keep search scoped to curated library content.
  if (!sp.get('in_library')) sp.set('in_library', 'true');
  if (!sp.get('face_verified')) sp.set('face_verified', 'true');
  if (!sp.get('include_unverified_faces')) sp.set('include_unverified_faces', 'false');
  Gallery.search.load(sp);
};

Gallery.search = {
  _peopleCache: [],

  async initPeoplePicker(selectedPersonID) {
    try {
      const people = await Gallery.utils.api('/api/people');
      this._peopleCache = Array.isArray(people) ? people : [];
      const datalist = document.getElementById('search-people-list');
      if (datalist) {
        datalist.innerHTML = this._peopleCache
          .map(p => `<option value="${Gallery.utils.esc(p.name)}"></option>`)
          .join('');
      }

      if (selectedPersonID) {
        const pid = parseInt(selectedPersonID, 10);
        const person = this._peopleCache.find(p => p.id === pid);
        const input = document.getElementById('search-person');
        if (person && input) input.value = person.name;
      }
    } catch (_) {
      this._peopleCache = [];
    }
  },

  resolvePersonID(name) {
    if (!name) return null;
    const person = this._peopleCache.find(p => (p.name || '').toLowerCase() === name.toLowerCase());
    return person ? person.id : null;
  },

  renderActiveFilters(sp) {
    const el = document.getElementById('search-active-filters');
    if (!el) return;

    const labels = {
      q: 'Keyword', from: 'From', to: 'To', make: 'Make', model: 'Model', has_gps: 'GPS',
      flag: 'Flag', source: 'Source', in_library: 'Scope', tag: 'Tag', event_id: 'Event',
      has_date_override: 'Date override', true_date_unknown: 'Date unknown', person_id: 'Person',
      face_verified: 'Face verified', face_source: 'Face source', include_unverified_faces: 'Include unverified',
      order: 'Sort'
    };
    const defaults = {
      in_library: 'true',
      face_verified: 'true',
      include_unverified_faces: 'false',
      order: 'captured_at_desc'
    };

    const chips = [];
    for (const [k, v] of sp.entries()) {
      if (k === 'page' || k === 'per_page') continue;
      if (!v) continue;
      if (defaults[k] && defaults[k] === v) continue;

      let val = v;
      if (k === 'person_id') {
        const pid = parseInt(v, 10);
        const person = this._peopleCache.find(p => p.id === pid);
        val = person ? person.name : v;
      }
      chips.push(`<button class="btn btn--sm" onclick="Gallery.search.removeFilter('${Gallery.utils.esc(k)}')">${Gallery.utils.esc(labels[k] || k)}: ${Gallery.utils.esc(val)} x</button>`);
    }

    if (!chips.length) {
      el.innerHTML = '<span style="color:var(--muted)">Default filters active: Library only, confirmed faces only.</span>';
      return;
    }
    el.innerHTML = chips.join(' ');
  },

  async load(sp) {
    const resultsEl = document.getElementById('search-results');
    if (!resultsEl) return;
    resultsEl.innerHTML = '<div class="loading">Searching...</div>';

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
      resultsEl.innerHTML = '<div class="empty"><div class="empty-icon">?</div><p>No photos match these filters.</p></div>';
      return;
    }

    const page    = parseInt(sp.get('page') || '1', 10);
    const perPage = 60;
    const total   = data.total;
    const pages   = Math.ceil(total / perPage);

    const grid = data.items.map(p => {
      const hasFlags = p.flags && p.flags.length > 0;
      const tags = Array.isArray(p.tags) ? p.tags : [];
      const people = Array.isArray(p.people) ? p.people : [];
      const sourceBadge = p.source ? `<span class="pill">${Gallery.utils.esc(p.source)}</span>` : '';
      const eventBadge = p.event_id != null ? `<span class="pill">event:${Gallery.utils.esc(String(p.event_id))}</span>` : '';
      const tagBadges = tags.slice(0, 2).map(t => `<span class="pill">#${Gallery.utils.esc(t)}</span>`).join(' ');
      const peopleBadges = people.slice(0, 3).map(pe => `<span class="pill ${pe.verified ? 'pill-green' : ''}">${Gallery.utils.esc(pe.person_name || 'Unknown')}</span>`).join(' ');
      const extraTags = tags.length > 2 ? `<span class="pill">+${tags.length - 2}</span>` : '';

      return `<div class="photo-card" onclick="Gallery.utils.navigate('/photo/${Gallery.utils.esc(p.sha256)}')">
        <img loading="lazy" src="${Gallery.utils.esc(p.thumbnail_url)}" alt="${Gallery.utils.esc(p.filename)}">
        <div class="photo-overlay">${Gallery.utils.esc(p.filename)}</div>
        ${hasFlags ? '<div class="flag-dot"></div>' : ''}
        <button class="stage-btn" title="Add to staging queue" onclick="Gallery.utils.stagePhoto('${Gallery.utils.esc(p.sha256)}', event)">+Stage</button>
        <div class="photo-card__info">
          <div class="photo-card__filename">${Gallery.utils.esc(p.filename)}</div>
          <div class="photo-card__date">${Gallery.utils.formatDate(p.captured_at)}</div>
          <div class="photo-card__badges">${sourceBadge} ${eventBadge} ${tagBadges} ${extraTags} ${peopleBadges}</div>
        </div>
      </div>`;
    }).join('');

    let pagination = '';
    if (pages > 1) {
      const prevSp = new URLSearchParams(sp); prevSp.set('page', String(Math.max(1, page - 1)));
      const nextSp = new URLSearchParams(sp); nextSp.set('page', String(Math.min(pages, page + 1)));
      pagination = `<div class="pagination">
        ${page > 1 ? `<button onclick="Gallery.search.goPage('${prevSp}')">< Prev</button>` : '<button disabled>< Prev</button>'}
        <span>${page} / ${pages} &nbsp;<span style="color:var(--muted)">(${total} photos)</span></span>
        ${page < pages ? `<button onclick="Gallery.search.goPage('${nextSp}')">Next ></button>` : '<button disabled>Next ></button>'}
      </div>`;
    } else {
      pagination = `<div class="result-count">${total} photo${total !== 1 ? 's' : ''}</div>`;
    }

    resultsEl.innerHTML = pagination + `<div class="photo-grid">${grid}</div>`;
  },

  goPage(spStr) {
    const sp = new URLSearchParams(spStr);
    history.pushState(null, '', '/search?' + sp.toString());
    Gallery.search.renderActiveFilters(sp);
    Gallery.search.load(sp);
  },

  removeFilter(key) {
    const sp = new URLSearchParams(location.search);
    sp.delete(key);
    if (key === 'person_id') {
      const input = document.getElementById('search-person');
      if (input) input.value = '';
    }
    sp.set('page', '1');
    if (!sp.get('in_library')) sp.set('in_library', 'true');
    if (!sp.get('face_verified')) sp.set('face_verified', 'true');
    if (!sp.get('include_unverified_faces')) sp.set('include_unverified_faces', 'false');
    history.pushState(null, '', '/search?' + sp.toString());
    this.renderActiveFilters(sp);
    this.load(sp);
  },

  clearAll() {
    const sp = new URLSearchParams();
    sp.set('in_library', 'true');
    sp.set('face_verified', 'true');
    sp.set('include_unverified_faces', 'false');
    sp.set('page', '1');

    const form = document.getElementById('search-form');
    if (form) form.reset();
    const input = document.getElementById('search-person');
    if (input) input.value = '';

    history.pushState(null, '', '/search?' + sp.toString());
    this.renderActiveFilters(sp);
    this.load(sp);
  },
};
