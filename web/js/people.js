// People page: list of all people and individual person detail.
// Routes: /people (list), /people/:id (detail)

window.Gallery = window.Gallery || {};
Gallery.pages = Gallery.pages || {};

// Entry point called by the router.
// personID is set when the route matches /people/:id.
// opts.action === 'review' navigates to the Face Review page.
// When the route has no capture groups (/faces/review), the router passes the opts object
// as the first argument; normalise that here.
Gallery.pages.people = function(personID, opts) {
  if (personID && typeof personID === 'object') { opts = personID; personID = null; }
  if (opts && opts.action === 'review') {
    renderFaceReview();
  } else if (personID) {
    renderPersonDetail(personID);
  } else {
    renderPeopleList();
  }
};

// ---- People list ----

async function renderPeopleList() {
  const app = document.getElementById('app');
  app.innerHTML = '<p class="loading-msg">Loading…</p>';

  try {
    const people = await Gallery.utils.api('/api/people');

    let html = `
      <div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:8px">
        <h1 style="margin:0">People</h1>
        <button class="btn" onclick="peopleShowNewModal()">+ Add Person</button>
      </div>`;

    if (!people.length) {
      html += `<div class="empty"><p>No people tagged yet.</p>
        <p>Open a photo in the Library view and use the "People in this photo" panel to add tags.</p></div>`;
    } else {
      html += '<div class="person-grid">';
      for (const p of people) {
        const count = p.photo_count || 0;
        html += `
          <div class="person-card" onclick="Gallery.utils.navigate('/people/${p.id}')">
            <div class="person-avatar">👤</div>
            <div class="person-name">${Gallery.utils.esc(p.name || '(unnamed)')}</div>
            <div class="person-count">${count} photo${count !== 1 ? 's' : ''}</div>
          </div>`;
      }
      html += '</div>';
    }

    // Hidden new-person modal.
    html += `
      <div id="new-person-modal" class="modal-overlay" style="display:none"
           onclick="if(event.target===this)peopleHideNewModal()">
        <div class="modal-box">
          <h2>Add Person</h2>
          <form onsubmit="peopleCreate(event)">
            <div class="form-group">
              <label>Name</label>
              <input type="text" name="name" class="form-input" required autofocus placeholder="e.g. Alice Smith">
            </div>
            <div class="form-group">
              <label>Notes <span class="form-hint">(optional)</span></label>
              <textarea name="notes" rows="2" class="form-input" placeholder="Any notes…"></textarea>
            </div>
            <div class="form-actions">
              <button type="submit" class="btn">Create</button>
              <button type="button" class="btn" style="background:var(--card);border:1px solid var(--border)"
                      onclick="peopleHideNewModal()">Cancel</button>
              <span id="people-create-status" class="form-status"></span>
            </div>
          </form>
        </div>
      </div>`;

    app.innerHTML = html;
  } catch (e) {
    app.innerHTML = `<div class="empty"><p>Error: ${Gallery.utils.esc(e.message)}</p></div>`;
  }
}

window.peopleShowNewModal = function() {
  const m = document.getElementById('new-person-modal');
  if (m) m.style.display = '';
};

window.peopleHideNewModal = function() {
  const m = document.getElementById('new-person-modal');
  if (m) m.style.display = 'none';
};

window.peopleCreate = async function(evt) {
  evt.preventDefault();
  const form = evt.target;
  const name = form.elements.name.value.trim();
  const notes = form.elements.notes.value.trim() || null;
  if (!name) return;

  const statusEl = document.getElementById('people-create-status');
  if (statusEl) statusEl.textContent = 'Creating…';

  try {
    await Gallery.utils.api('/api/people', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name, notes }),
    });
    Gallery.utils.navigate('/people');
  } catch (e) {
    if (statusEl) statusEl.textContent = 'Error: ' + e.message;
  }
};

// ---- Person detail ----

async function renderPersonDetail(personID) {
  const app = document.getElementById('app');
  app.innerHTML = '<p class="loading-msg">Loading…</p>';

  try {
    const [person, photosResp] = await Promise.all([
      Gallery.utils.api('/api/people/' + personID),
      Gallery.utils.api('/api/people/' + personID + '/photos'),
    ]);

    const photos = photosResp.photos || [];
    const total  = photosResp.total  || 0;

    let html = `
      <div style="margin-bottom:12px">
        <a href="/people" data-link style="font-size:13px;color:var(--accent);text-decoration:none">← All People</a>
      </div>
      <div class="person-detail-header">
        <div class="person-avatar person-avatar--lg">👤</div>
        <div class="person-detail-info">
          <form onsubmit="personSave(event, ${person.id})">
            <div class="form-group">
              <label>Name</label>
              <input type="text" name="name" value="${Gallery.utils.esc(person.name)}"
                     class="form-input" required>
            </div>
            <div class="form-group">
              <label>Notes</label>
              <textarea name="notes" rows="2" class="form-input">${Gallery.utils.esc(person.notes || '')}</textarea>
            </div>
            <div class="form-actions">
              <button type="submit" class="btn btn--sm">Save</button>
              <span id="person-save-status" class="form-status"></span>
              <button type="button" class="btn btn--sm btn--danger"
                      onclick="personDelete(${person.id}, '${Gallery.utils.esc(person.name)}')"
                      style="margin-left:auto">Delete person</button>
            </div>
          </form>
        </div>
      </div>
      <h2 class="section-title">${total} photo${total !== 1 ? 's' : ''}</h2>`;

    if (!photos.length) {
      html += '<div class="empty"><p>No library photos tagged with this person yet.</p></div>';
    } else {
      html += '<div class="photo-grid">';
      for (const p of photos) {
        html += `
          <a href="/library" data-link class="photo-card"
             title="${Gallery.utils.esc(p.relative_path || '')}">
            <img src="/api/photos/${p.photo_sha256}/thumbnail" alt="" loading="lazy">
            ${p.title ? `<div class="photo-card-label">${Gallery.utils.esc(p.title)}</div>` : ''}
          </a>`;
      }
      html += '</div>';
    }

    // Merge section — load all other people for typeahead.
    html += `
      <div style="margin-top:24px;padding-top:16px;border-top:1px solid var(--border)">
        <h3 style="margin:0 0 8px;font-size:14px">Merge into another person</h3>
        <p style="font-size:12px;color:var(--fg-muted);margin:0 0 8px">
          All face tags will be reassigned to the selected person, then this record will be deleted.
        </p>
        <div style="display:flex;gap:8px;align-items:center">
          <input list="merge-people-list" id="merge-into-input" class="form-input"
                 placeholder="Type a name…" style="max-width:240px">
          <datalist id="merge-people-list"></datalist>
          <button class="btn btn--sm btn--danger" onclick="personMerge(${person.id})">Merge &amp; Delete</button>
          <span id="merge-status" class="form-status"></span>
        </div>
      </div>`;

    app.innerHTML = html;

    // Populate merge datalist.
    Gallery.utils.api('/api/people').then(all => {
      const dl = document.getElementById('merge-people-list');
      if (!dl) return;
      dl.innerHTML = (all || [])
        .filter(p => p.id !== person.id)
        .map(p => `<option value="${Gallery.utils.esc(p.name)}" data-id="${p.id}">`)
        .join('');
      // Store for lookup.
      window._mergePeopleCache = (all || []).filter(p => p.id !== person.id);
    }).catch(() => {});
  } catch (e) {
    app.innerHTML = `<div class="empty"><p>Error: ${Gallery.utils.esc(e.message)}</p></div>`;
  }
}

window.personSave = async function(evt, personID) {
  evt.preventDefault();
  const form = evt.target;
  const statusEl = document.getElementById('person-save-status');
  if (statusEl) statusEl.textContent = 'Saving…';

  const notes = form.elements.notes.value.trim();
  try {
    await Gallery.utils.api('/api/people/' + personID, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        name:  form.elements.name.value.trim(),
        notes: notes,  // empty string clears notes on the server
      }),
    });
    if (statusEl) {
      statusEl.textContent = 'Saved.';
      setTimeout(() => { statusEl.textContent = ''; }, 2000);
    }
  } catch (e) {
    if (statusEl) statusEl.textContent = 'Error: ' + e.message;
  }
};

window.personDelete = async function(personID, name) {
  if (!confirm(`Delete "${name}" from the people registry?\n\nAll face tags linked to this person will be unlinked (not deleted). Photos are not affected.`)) {
    return;
  }
  try {
    await Gallery.utils.api('/api/people/' + personID, { method: 'DELETE' });
    Gallery.utils.navigate('/people');
  } catch (e) {
    alert('Error deleting person: ' + e.message);
  }
};

window.personMerge = async function(fromID) {
  const input = document.getElementById('merge-into-input');
  const statusEl = document.getElementById('merge-status');
  const name = (input && input.value || '').trim();
  if (!name) { if (input) input.focus(); return; }

  const cache = window._mergePeopleCache || [];
  const target = cache.find(p => p.name.toLowerCase() === name.toLowerCase());
  if (!target) {
    if (statusEl) statusEl.textContent = 'Person not found — pick from the list';
    return;
  }

  if (!confirm(`Merge this person into "${target.name}"?\n\nAll their face tags will move to "${target.name}" and this record will be deleted. This cannot be undone.`)) return;

  if (statusEl) statusEl.textContent = 'Merging…';
  try {
    await Gallery.utils.api('/api/people/' + fromID + '/merge', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ into: target.id }),
    });
    Gallery.utils.navigate('/people/' + target.id);
  } catch (e) {
    if (statusEl) statusEl.textContent = 'Error: ' + e.message;
  }
};

// ---- Face Review ----
// Route: /faces/review
// Two-panel layout: left = clustered unidentified faces, right = auto-suggestions.

async function renderFaceReview() {
  const app = document.getElementById('app');
  app.innerHTML = '<p class="loading-msg">Loading…</p>';

  const rs = Gallery.recognitionStatus || {};
  if (!rs.enabled) {
    app.innerHTML = `<div class="empty"><div class="empty-icon">⚙️</div>
      <p>Face recognition is not enabled. Set <code>recognition.enabled = true</code> in config.</p></div>`;
    return;
  }
  if (!rs.available) {
    app.innerHTML = `<div class="empty"><div class="empty-icon">⚙️</div>
      <p>Face recognition is enabled but models are not yet available (loading…).</p></div>`;
    return;
  }

  try {
    const [unidentified, suggestions, people] = await Promise.all([
      Gallery.utils.api('/api/faces/unidentified'),
      Gallery.utils.api('/api/faces/suggestions'),
      Gallery.utils.api('/api/people'),
    ]);

    const cpuWarning = rs.execution_provider === 'CPU'
      ? `<div class="warning-banner" style="background:#664400;color:#ffd;padding:8px 12px;border-radius:4px;margin-bottom:12px">
           ⚠️ Running on CPU — face recognition will be slow. For best results configure a GPU.
         </div>`
      : '';

    app.innerHTML = `
      ${cpuWarning}
      <div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:12px">
        <h1 style="margin:0">Face Review</h1>
        <button class="btn" id="btn-recluster">Re-run clustering</button>
      </div>
      <div style="display:grid;grid-template-columns:1fr 1fr;gap:16px;align-items:start">
        <div id="fr-clusters"></div>
        <div id="fr-suggestions"></div>
      </div>`;

    document.getElementById('btn-recluster').addEventListener('click', async () => {
      const btn = document.getElementById('btn-recluster');
      btn.disabled = true;
      btn.textContent = 'Clustering…';
      try {
        const r = await Gallery.utils.api('/api/faces/cluster', { method: 'POST' });
        btn.textContent = `Done (${r.clusters} clusters)`;
        await renderFaceReview();
      } catch (e) {
        btn.textContent = 'Error: ' + e.message;
        btn.disabled = false;
      }
    });

    // Build people lookup for suggestions panel.
    const peopleMap = {};
    for (const p of (people || [])) peopleMap[p.id] = p.name;

    renderClusters(unidentified || [], people || []);
    renderSuggestions(suggestions || [], peopleMap);
  } catch (e) {
    app.innerHTML = `<div class="error-msg">Error loading face review: ${Gallery.utils.esc(e.message)}</div>`;
  }
}

function faceCropHTML(sha256, bx, by, bw, bh) {
  const size = 60;
  const bgW = Math.round(size / bw);
  const bgH = Math.round(size / bh);
  const bgX = -Math.round(bx / bw * size);
  const bgY = -Math.round(by / bh * size);
  return `<div style="width:${size}px;height:${size}px;border-radius:4px;overflow:hidden;display:inline-block;flex-shrink:0;
    background: url('/api/photos/${sha256}/thumbnail') no-repeat ${bgX}px ${bgY}px / ${bgW}px ${bgH}px;
    background-color:#222"></div>`;
}

function groupByCluster(faces) {
  const groups = new Map(); // cluster_id (or 'null-'+id) → [face]
  for (const f of faces) {
    const key = f.cluster_id != null ? String(f.cluster_id) : 'solo-' + f.id;
    if (!groups.has(key)) groups.set(key, []);
    groups.get(key).push(f);
  }
  return groups;
}

function renderClusters(faces, people) {
  const el = document.getElementById('fr-clusters');
  if (!el) return;
  const groups = groupByCluster(faces);

  const peopleOptions = people.map(p =>
    `<option value="${p.id}">${Gallery.utils.esc(p.name)}</option>`
  ).join('');

  if (groups.size === 0) {
    el.innerHTML = `<h2 style="margin:0 0 8px">Unidentified Faces</h2>
      <div class="empty"><p>No unidentified faces.</p></div>`;
    return;
  }

  let html = `<h2 style="margin:0 0 8px">Unidentified Faces <small style="font-weight:normal;color:var(--fg-muted)">${faces.length}</small></h2>`;
  for (const [key, group] of groups) {
    const isCluster = !key.startsWith('solo-');
    const faceIDs = group.map(f => f.id);
    html += `<div class="face-cluster-card" data-face-ids="${faceIDs.join(',')}" style="background:var(--bg2);border-radius:6px;padding:12px;margin-bottom:12px">
      <div style="font-size:0.75em;color:var(--fg-muted);margin-bottom:6px">${isCluster ? 'Cluster ' + key : 'Unclassified'} — ${group.length} face${group.length > 1 ? 's' : ''}</div>
      <div style="display:flex;flex-wrap:wrap;gap:4px;margin-bottom:10px">
        ${group.map(f => faceCropHTML(f.photo_sha256, f.bbox_x, f.bbox_y, f.bbox_w, f.bbox_h)).join('')}
      </div>
      <div style="display:flex;gap:6px;align-items:center">
        <select class="cluster-person-sel" style="flex:1;padding:4px">
          <option value="">— assign to person —</option>
          ${peopleOptions}
        </select>
        <button class="btn btn-sm" onclick="assignCluster('${faceIDs.join(',')}', this)">Confirm</button>
      </div>
    </div>`;
  }
  el.innerHTML = html;
}

function renderSuggestions(suggestions, peopleMap) {
  const el = document.getElementById('fr-suggestions');
  if (!el) return;

  if (suggestions.length === 0) {
    el.innerHTML = `<h2 style="margin:0 0 8px">Auto-Suggestions</h2>
      <div class="empty"><p>No pending suggestions.</p></div>`;
    return;
  }

  let html = `<h2 style="margin:0 0 8px">Auto-Suggestions <small style="font-weight:normal;color:var(--fg-muted)">${suggestions.length}</small></h2>`;
  for (const f of suggestions) {
    const personName = f.person_id ? (peopleMap[f.person_id] || 'Unknown') : '?';
    html += `<div id="sug-${f.id}" style="background:var(--bg2);border-radius:6px;padding:10px 12px;margin-bottom:10px;display:flex;gap:10px;align-items:center">
      ${faceCropHTML(f.photo_sha256, f.bbox_x, f.bbox_y, f.bbox_w, f.bbox_h)}
      <div style="flex:1;min-width:0">
        <div style="font-weight:600">${Gallery.utils.esc(personName)}</div>
        <div style="font-size:0.75em;color:var(--fg-muted)">confidence: ${f.confidence != null ? f.confidence.toFixed(2) : '—'}</div>
      </div>
      <div style="display:flex;gap:6px">
        <button class="btn btn-sm btn-success" onclick="confirmSuggestion(${f.id})">✓</button>
        <button class="btn btn-sm btn-danger" onclick="rejectSuggestion(${f.id})">✗</button>
      </div>
    </div>`;
  }
  el.innerHTML = html;
}

window.assignCluster = async function(faceIDsStr, btn) {
  const card = btn.closest('.face-cluster-card');
  const sel = card.querySelector('.cluster-person-sel');
  const personID = parseInt(sel.value, 10);
  if (!personID) { alert('Please select a person first.'); return; }

  const faceIDs = faceIDsStr.split(',').map(Number);
  btn.disabled = true;
  btn.textContent = '…';
  try {
    for (const id of faceIDs) {
      await Gallery.utils.api('/api/faces/' + id + '/confirm', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ person_id: personID }),
      });
    }
    card.style.opacity = '0.4';
    card.innerHTML = `<div style="color:var(--fg-muted);font-size:0.85em">✓ Assigned ${faceIDs.length} face${faceIDs.length > 1 ? 's' : ''}</div>`;
  } catch (e) {
    btn.disabled = false;
    btn.textContent = 'Confirm';
    alert('Error: ' + e.message);
  }
};

window.confirmSuggestion = async function(faceID) {
  const card = document.getElementById('sug-' + faceID);
  try {
    await Gallery.utils.api('/api/faces/' + faceID + '/confirm', { method: 'POST' });
    if (card) { card.style.opacity = '0.4'; card.innerHTML = '<div style="padding:8px;color:var(--fg-muted)">✓ Confirmed</div>'; }
  } catch (e) {
    alert('Error: ' + e.message);
  }
};

window.rejectSuggestion = async function(faceID) {
  const card = document.getElementById('sug-' + faceID);
  try {
    await Gallery.utils.api('/api/faces/' + faceID + '/reject', { method: 'POST' });
    if (card) { card.style.opacity = '0.4'; card.innerHTML = '<div style="padding:8px;color:var(--fg-muted)">✗ Rejected</div>'; }
  } catch (e) {
    alert('Error: ' + e.message);
  }
};
