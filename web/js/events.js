// Events page: list of photo events with date range, count, and thumbnail strip.
// Detail view shows all photos in the event as a grid.
window.Gallery = window.Gallery || {};
Gallery.pages = Gallery.pages || {};

Gallery.pages.events = async function(eventId) {
  const app = document.getElementById('app');
  Gallery.utils.setActiveNav('/events');

  if (eventId) {
    await renderEventDetail(app, parseInt(eventId, 10));
  } else {
    await renderEventList(app);
  }
};

async function renderEventList(app) {
  app.innerHTML = `<div class="page-content">
    <h2 class="page-title">Events</h2>
    <div id="event-list" class="loading">Loading…</div>
  </div>`;

  let data;
  try {
    data = await Gallery.utils.api('/api/events');
  } catch (e) {
    document.getElementById('event-list').innerHTML =
      `<div class="empty"><p>Error: ${Gallery.utils.esc(e.message)}</p></div>`;
    return;
  }

  const el = document.getElementById('event-list');
  if (!data.items || data.items.length === 0) {
    el.innerHTML = `<div class="empty">
      <div class="empty-icon">📅</div>
      <p>No events found. Run a scan to cluster photos into events.</p>
    </div>`;
    return;
  }

  el.innerHTML = data.items.map(e => `
    <div class="event-card" onclick="Gallery.utils.navigate('/events/${e.id}')">
      <div class="event-card__header">
        <span class="event-card__label">${Gallery.utils.esc(e.label)}</span>
        <span class="event-card__count">${e.photo_count} photo${e.photo_count !== 1 ? 's' : ''}</span>
      </div>
      <div class="event-card__dates">
        <span>${Gallery.utils.formatDate(e.started_at)}</span>
        ${e.started_at !== e.ended_at ? `<span class="event-card__sep">→</span><span>${Gallery.utils.formatDate(e.ended_at)}</span>` : ''}
      </div>
      ${e.centroid_lat != null ? `<div class="event-card__geo">
        📍 ${Gallery.utils.formatCoord(e.centroid_lat, 'N', 'S')}, ${Gallery.utils.formatCoord(e.centroid_lon, 'E', 'W')}
      </div>` : ''}
    </div>
  `).join('');
}

async function renderEventDetail(app, eventId) {
  app.innerHTML = `<div class="page-content">
    <a href="/events" class="back-link">← All events</a>
    <div id="event-detail" class="loading">Loading…</div>
  </div>`;

  let data;
  try {
    data = await Gallery.utils.api(`/api/events/${eventId}`);
  } catch (e) {
    document.getElementById('event-detail').innerHTML =
      `<div class="empty"><p>Event not found.</p></div>`;
    return;
  }

  const el = document.getElementById('event-detail');
  const geoLine = data.centroid_lat != null
    ? `<p class="event-detail__geo">📍 ${Gallery.utils.formatCoord(data.centroid_lat, 'N', 'S')}, ${Gallery.utils.formatCoord(data.centroid_lon, 'E', 'W')}</p>`
    : '';

  const grid = (data.photos || []).map(p => `
    <div class="photo-card" onclick="Gallery.utils.navigate('/photo/${p.sha256}')">
      <img class="photo-card__thumb" src="${p.thumbnail_url}" alt="${Gallery.utils.esc(p.filename)}" loading="lazy">
      <div class="photo-card__info">
        <div class="photo-card__filename">${Gallery.utils.esc(p.filename)}</div>
        <div class="photo-card__date">${Gallery.utils.formatDate(p.captured_at)}</div>
      </div>
    </div>
  `).join('');

  el.innerHTML = `
    <h2 class="page-title">${Gallery.utils.esc(data.label)}</h2>
    <p class="event-detail__dates">
      ${Gallery.utils.formatDate(data.started_at)}
      ${data.started_at !== data.ended_at ? ` → ${Gallery.utils.formatDate(data.ended_at)}` : ''}
    </p>
    ${geoLine}
    <p class="event-detail__count">${data.photo_count} photo${data.photo_count !== 1 ? 's' : ''}</p>
    <div class="photo-grid">${grid || '<div class="empty"><p>No photos in this event.</p></div>'}</div>
  `;
}
