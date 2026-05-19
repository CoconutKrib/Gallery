// Photo detail page.
window.Gallery = window.Gallery || {};
Gallery.pages = Gallery.pages || {};

Gallery.pages.photo = async function(sha256) {
  const app = document.getElementById('app');
  Gallery.utils.setActiveNav('/browse');
  app.innerHTML = `<div class="photo-detail"><div class="loading">Loading…</div></div>`;

  let photo;
  try {
    photo = await Gallery.utils.api(`/api/photos/${sha256}`);
  } catch (e) {
    app.innerHTML = `<div class="photo-detail"><div class="empty"><p>Photo not found.</p></div></div>`;
    return;
  }

  const e = Gallery.utils.esc;
  const fmt = Gallery.utils.formatDate;

  // Back button — go back or to browse.
  const back = `<div class="photo-detail-back" onclick="history.length > 1 ? history.back() : Gallery.utils.navigate('/browse')">
    ← Back
  </div>`;

  // Main image.
  const img = `<img class="photo-detail-image" src="${e(photo.image_url)}"
    alt="${e(photo.filename)}"
    onclick="window.open('${e(photo.image_url)}','_blank')">`;

  // Flags.
  let flagsHtml = '';
  if (photo.flags && photo.flags.length) {
    flagsHtml = photo.flags.map(f => `<span class="flag-badge">${e(f.replace('_', ' '))}</span>`).join('');
  }

  // EXIF table rows (only show non-null values).
  const exifRows = [
    ['Date taken', fmt(photo.captured_at)],
    ['Camera',     [photo.camera_make, photo.camera_model].filter(Boolean).join(' ') || null],
    ['Lens',       photo.lens_model],
    ['ISO',        photo.iso],
    ['Aperture',   photo.aperture != null ? 'f/' + photo.aperture.toFixed(1) : null],
    ['Shutter',    photo.shutter_speed],
    ['Focal length', photo.focal_length != null ? photo.focal_length.toFixed(1) + ' mm' : null],
    ['Dimensions', photo.width && photo.height ? `${photo.width} × ${photo.height} px` : null],
    ['Latitude',   Gallery.utils.formatCoord(photo.latitude, 'N', 'S')],
    ['Longitude',  Gallery.utils.formatCoord(photo.longitude, 'E', 'W')],
    ['Altitude',   photo.altitude != null ? photo.altitude.toFixed(1) + ' m' : null],
  ].filter(([, v]) => v != null && v !== '');

  const exifTable = exifRows.map(([label, val]) =>
    `<tr><td>${e(label)}</td><td>${e(String(val))}</td></tr>`
  ).join('');

  // File info.
  const fileRows = [
    ['Filename', photo.filename],
    ['SHA-256', photo.sha256.substring(0, 16) + '…'],
    ['Ingested', fmt(photo.ingested_at)],
    ['Path', photo.filepath],
  ].map(([label, val]) =>
    `<tr><td>${e(label)}</td><td>${e(String(val))}</td></tr>`
  ).join('');

  // Duplicates.
  let dupesHtml = '';
  if (photo.duplicates && photo.duplicates.length) {
    dupesHtml = photo.duplicates.map(d =>
      `<div class="dupe-path">${e(d.filepath)}</div>`
    ).join('');
  } else {
    dupesHtml = `<p style="color:var(--muted);font-size:13px">No duplicates found.</p>`;
  }

  app.innerHTML = `<div class="photo-detail">
    ${back}
    ${img}
    ${flagsHtml ? `<div style="margin-top:12px">${flagsHtml}</div>` : ''}
    <div class="photo-detail-meta">
      <div class="meta-card">
        <h3>EXIF</h3>
        <table class="exif-table"><tbody>${exifTable || '<tr><td colspan="2" style="color:var(--muted)">No EXIF data</td></tr>'}</tbody></table>
      </div>
      <div>
        <div class="meta-card" style="margin-bottom:var(--gap)">
          <h3>File</h3>
          <table class="exif-table"><tbody>${fileRows}</tbody></table>
        </div>
        <div class="meta-card">
          <h3>Duplicate locations</h3>
          ${dupesHtml}
        </div>
      </div>
    </div>
  </div>`;
};
