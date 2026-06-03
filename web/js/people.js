// People page: list of all people and individual person detail.
// Routes: /people (list), /people/:id (detail)

window.Gallery = window.Gallery || {};
Gallery.pages = Gallery.pages || {};

// Entry point called by the router.
// personID is set when the route matches /people/:id.
Gallery.pages.people = function(personID) {
  if (personID) {
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

    app.innerHTML = html;
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
