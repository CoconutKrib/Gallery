// Map view — Leaflet-based geo/map page.
// Exposed as Gallery.pages.map

window.Gallery = window.Gallery || {};
Gallery.pages = Gallery.pages || {};

Gallery.pages.map = function () {
  Gallery.utils.setActiveNav('/map');

  const app = document.getElementById('app');
  app.innerHTML = `
    <div class="map-layout">
      <div class="map-toolbar">
        <span class="map-count" id="map-count">Loading…</span>
        <div class="map-search-group">
          <label class="map-label">Radius search</label>
          <input type="number" id="map-radius" class="map-input" value="10" min="0.1" max="20000" step="0.1" title="Radius in km">
          <span class="map-unit">km</span>
          <button id="map-pick-btn" class="btn btn-sm" title="Click on the map to set center">Pick center</button>
          <button id="map-search-btn" class="btn btn-sm btn-primary" disabled>Search</button>
          <button id="map-clear-btn" class="btn btn-sm" style="display:none">Clear</button>
        </div>
      </div>
      <div id="leaflet-map" class="map-container"></div>
    </div>`;

  // Fix Leaflet default icon paths (vendored).
  L.Icon.Default.imagePath = '/vendor/leaflet/images/';

  const map = L.map('leaflet-map', { zoomControl: true });

  L.tileLayer('https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png', {
    attribution: '&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> contributors',
    maxZoom: 19,
  }).addTo(map);

  const countEl      = document.getElementById('map-count');
  const radiusInput  = document.getElementById('map-radius');
  const pickBtn      = document.getElementById('map-pick-btn');
  const searchBtn    = document.getElementById('map-search-btn');
  const clearBtn     = document.getElementById('map-clear-btn');

  let allMarkers   = L.featureGroup().addTo(map);
  let radiusCircle = null;
  let centerLatLng = null;
  let picking      = false;

  function setCount(n) {
    countEl.textContent = n === 1 ? '1 photo' : `${n} photos`;
  }

  function makePopup(pin) {
    return `
      <div class="map-popup">
        <a href="/photo/${Gallery.utils.esc(pin.sha256)}" data-link>
          <img src="/api/photos/${Gallery.utils.esc(pin.sha256)}/thumbnail" alt="${Gallery.utils.esc(pin.filename)}" class="map-thumb">
        </a>
        <div class="map-popup-name">
          <a href="/photo/${Gallery.utils.esc(pin.sha256)}" data-link>${Gallery.utils.esc(pin.filename)}</a>
        </div>
        ${pin.captured_at ? `<div class="map-popup-date">${Gallery.utils.formatDate(pin.captured_at)}</div>` : ''}
        <div class="map-popup-coords">${pin.lat.toFixed(5)}, ${pin.lon.toFixed(5)}</div>
      </div>`;
  }

  function renderPins(pins) {
    allMarkers.clearLayers();
    for (const pin of pins) {
      const marker = L.marker([pin.lat, pin.lon]);
      marker.bindPopup(makePopup(pin), { maxWidth: 200 });
      // Intercept data-link clicks inside popup after it opens.
      marker.on('popupopen', () => {
        document.querySelectorAll('.map-popup a[data-link]').forEach(a => {
          a.addEventListener('click', e => {
            e.preventDefault();
            Gallery.utils.navigate(a.getAttribute('href'));
          });
        });
      });
      allMarkers.addLayer(marker);
    }
    setCount(pins.length);
    if (pins.length > 0) {
      map.fitBounds(allMarkers.getBounds().pad(0.15));
    } else {
      map.setView([20, 0], 2);
    }
  }

  // ── Load all geotagged photos ──────────────────────────────────────────────
  Gallery.utils.api('/api/map')
    .then(pins => renderPins(pins))
    .catch(err => { countEl.textContent = 'Error: ' + err.message; });

  // ── Picking mode ──────────────────────────────────────────────────────────
  function startPicking() {
    picking = true;
    pickBtn.textContent = 'Picking…';
    pickBtn.classList.add('btn-active');
    map.getContainer().style.cursor = 'crosshair';
  }

  function stopPicking() {
    picking = false;
    pickBtn.textContent = 'Pick center';
    pickBtn.classList.remove('btn-active');
    map.getContainer().style.cursor = '';
  }

  pickBtn.addEventListener('click', () => {
    if (picking) { stopPicking(); return; }
    startPicking();
  });

  map.on('click', e => {
    if (!picking) return;
    stopPicking();
    centerLatLng = e.latlng;
    searchBtn.disabled = false;

    // Draw or update the radius circle.
    const km = parseFloat(radiusInput.value) || 10;
    if (radiusCircle) { map.removeLayer(radiusCircle); }
    radiusCircle = L.circle(centerLatLng, {
      radius: km * 1000,
      color: 'var(--accent, #4f8ef7)',
      fillOpacity: 0.08,
      weight: 2,
    }).addTo(map);
  });

  radiusInput.addEventListener('input', () => {
    if (!radiusCircle || !centerLatLng) return;
    const km = parseFloat(radiusInput.value) || 10;
    radiusCircle.setRadius(km * 1000);
  });

  // ── Radius search ─────────────────────────────────────────────────────────
  searchBtn.addEventListener('click', () => {
    if (!centerLatLng) return;
    const km = parseFloat(radiusInput.value) || 10;
    const url = `/api/map/nearby?lat=${centerLatLng.lat}&lon=${centerLatLng.lng}&radius_km=${km}`;
    countEl.textContent = 'Searching…';
    Gallery.utils.api(url)
      .then(pins => {
        renderPins(pins);
        clearBtn.style.display = '';
        // Keep circle visible; fit bounds to circle + results.
        if (radiusCircle) {
          const group = L.featureGroup([radiusCircle, allMarkers]);
          map.fitBounds(group.getBounds().pad(0.1));
        }
      })
      .catch(err => { countEl.textContent = 'Error: ' + err.message; });
  });

  clearBtn.addEventListener('click', () => {
    centerLatLng = null;
    searchBtn.disabled = true;
    clearBtn.style.display = 'none';
    if (radiusCircle) { map.removeLayer(radiusCircle); radiusCircle = null; }
    countEl.textContent = 'Loading…';
    Gallery.utils.api('/api/map')
      .then(pins => renderPins(pins))
      .catch(err => { countEl.textContent = 'Error: ' + err.message; });
  });
};
