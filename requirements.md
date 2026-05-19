# Family Photo Gallery — Requirements

## Overview

A locally-runnable photo management and gallery web application for a single family's photo archive. The primary goals are deduplication across scattered directory trees, chronological/geographical browsing, and event clustering — without ever modifying the source filesystem.

---

## Guiding Principles

- **Read-only filesystem** — the application never writes to, moves, or deletes source photo files.
- **Self-hosted, single user** — no multi-tenancy, no cloud dependency.
- **URL-addressable views** — gallery, search, geo, and timeline views all have shareable, bookmarkable URLs.
- **Minimal frontend dependencies** — vanilla JS; specific utility libraries (e.g. Plotly, Leaflet, Tailwind, FontAwesome) are acceptable. No React/Vue/Angular.
- **Configuration as JSON** — core settings (watched paths, camera whitelist, filename filters, auth) live in a JSON config file for easy inspection and manual editing outside the app.
- **SQLite as the database** — all ingested photo metadata, dedup hashes, and status flags stored in a single SQLite file.

---

## Tech Stack

| Layer | Choice |
|---|---|
| Backend | Go |
| Database | SQLite (single file) |
| Thumbnail cache | `.cache/` directory (JPEG thumbnails on disk, generated at ingest) |
| Frontend | Vanilla JS, HTML, CSS |
| Mapping | Leaflet.js |
| Timeline/charting | Plotly.js |
| Styling | Tailwind CSS |

---

## 1. Configuration

### 1.1 Config file (`config.json`)

Stores settings that should be human-readable and editable outside the app:

- **`library_paths`** — list of root directories to scan. Each entry has:
  - `path` (string)
  - `label` (string, user-defined name)
- **`camera_whitelist`** — list of `{ make, model }` objects (strings, case-insensitive match against EXIF). Serial number matching is optional/future.
- **`filename_filters`** — global include/exclude regex patterns applied to filenames (not full paths):
  - `include` — array of regex strings; file must match at least one (if non-empty)
  - `exclude` — array of regex strings; file is rejected if it matches any
- **`auth`** — `{ enabled: bool, password_hash: string }` (bcrypt hash)
- **`db_path`** — path to the SQLite file (default: `./gallery.db`)
- **`cache_dir`** — path to thumbnail cache (default: `./.cache`)

### 1.2 Settings UI

A settings/config view in the webapp surfaces:

- List of watched library paths (add/remove)
- Camera whitelist (add/remove entries)
- Filename include/exclude regex patterns (add/remove, with live validation)
- Rescan controls: trigger a rescan of one path or all paths
- Ingest error review panel (see §3.5)

Changes to non-secret settings (paths, whitelist, filters) update `config.json` via the API. Password changes hash before writing.

---

## 2. Scanning and Ingestion

### 2.1 Supported file types

- JPEG (`.jpg`, `.jpeg`)
- HEIC (`.heic`, `.HEIC`)

RAW formats are out of scope.

### 2.2 Scan process (per library path)

1. Recursively walk the directory tree.
2. For each file with a supported extension:
   a. Apply global filename include/exclude regex filters — skip if rejected.
   b. Read EXIF metadata.
   c. Check `Make` + `Model` against the camera whitelist. If no match (or no EXIF), **skip silently** — do not add to the database - move onto next and restart at a for next file.
   d. Compute SHA-256 hash of the file contents.
   e. Check if the hash already exists in the database (dedup):
      - **Duplicate**: check whether this exact (sha256, filepath) pair already exists in `duplicate_paths` (i.e. previously recorded on an earlier scan). If it does, skip silently — do not increment the duplicate counter or insert again. If it is a new duplicate location, insert a record into `duplicate_paths` (sha256, filepath, library_path_id, discovered_at) and increment the duplicate counter for this scan run. Either way, **stop processing this file** — skip steps f–h.
      - **New**: continue to step f.
   f. Extract all available EXIF fields (see §2.3).
   g. Generate a thumbnail (see §2.4).
   h. Insert a record into the `photos` table.
3. Record scan run metadata (timestamp, path, counts: found / skipped / ingested / duplicate / error).

### 2.3 EXIF fields to extract and store

| Field | EXIF tag | Notes |
|---|---|---|
| Capture datetime | `DateTimeOriginal` | Primary date; flag if missing |
| GPS latitude | `GPSLatitude` + `GPSLatitudeRef` | |
| GPS longitude | `GPSLongitude` + `GPSLongitudeRef` | |
| GPS altitude | `GPSAltitude` | Optional |
| Camera make | `Make` | |
| Camera model | `Model` | |
| Camera serial | `BodySerialNumber` | Store if present, not used for filtering |
| Lens model | `LensModel` | |
| ISO | `ISOSpeedRatings` | |
| Aperture | `FNumber` | |
| Shutter speed | `ExposureTime` | |
| Focal length | `FocalLength` | |
| Flash | `Flash` | |
| Image width/height | `PixelXDimension` / `PixelYDimension` | |
| Orientation | `Orientation` | For correct thumbnail rendering |

### 2.4 Thumbnails

- Generated at ingest time (not on demand).
- Stored as JPEG in `.cache/<sha256_prefix>/<sha256>.jpg` (subdirectory by first 2 chars of hash to avoid large flat dirs).
- Size: long edge 400px, aspect ratio preserved.
- Regenerated if missing on rescan; not regenerated if already present for that hash.

### 2.5 Ingest error / data deficiency flags

If a photo passes the camera whitelist check but has data quality issues, it is still ingested and flagged in the database:

| Flag | Condition |
|---|---|
| `missing_date` | No `DateTimeOriginal` in EXIF |
| `missing_gps` | No GPS coordinates (informational only, not an error) |

Flagged photos appear in the **Ingest Issues** panel in the settings view, showing filepath, hash, and which flag(s) are set.

---

## 3. Database Schema (SQLite)

### `photos`
| Column | Type | Notes |
|---|---|---|
| `id` | INTEGER PK | |
| `sha256` | TEXT UNIQUE | Content hash |
| `filepath` | TEXT | Canonical path where first seen |
| `library_path_id` | INTEGER FK | Which watched path it belongs to |
| `filename` | TEXT | |
| `captured_at` | DATETIME | Nullable |
| `latitude` | REAL | Nullable |
| `longitude` | REAL | Nullable |
| `altitude` | REAL | Nullable |
| `camera_make` | TEXT | |
| `camera_model` | TEXT | |
| `camera_serial` | TEXT | Nullable |
| `lens_model` | TEXT | Nullable |
| `iso` | INTEGER | Nullable |
| `aperture` | REAL | Nullable |
| `shutter_speed` | TEXT | Nullable |
| `focal_length` | REAL | Nullable |
| `flash` | INTEGER | Nullable |
| `width` | INTEGER | Nullable |
| `height` | INTEGER | Nullable |
| `orientation` | INTEGER | Nullable |
| `thumbnail_path` | TEXT | Path within cache dir |
| `flags` | TEXT | JSON array of flag strings |
| `ingested_at` | DATETIME | |

### `duplicate_paths`
Tracks every filepath where a given hash has been found (beyond the canonical one):

| Column | Type | Notes |
|---|---|---|
| `id` | INTEGER PK | |
| `sha256` | TEXT | FK to photos.sha256 |
| `filepath` | TEXT | The duplicate location |
| `library_path_id` | INTEGER FK | |
| `discovered_at` | DATETIME | |

Unique constraint on `(sha256, filepath)` — ensures a rescan never creates duplicate rows for the same file at the same location.

### `library_paths`
| Column | Type | Notes |
|---|---|---|
| `id` | INTEGER PK | |
| `path` | TEXT | |
| `label` | TEXT | |
| `last_scanned_at` | DATETIME | Nullable |

### `scan_runs`
| Column | Type | Notes |
|---|---|---|
| `id` | INTEGER PK | |
| `library_path_id` | INTEGER FK | |
| `started_at` | DATETIME | |
| `finished_at` | DATETIME | Nullable |
| `files_found` | INTEGER | |
| `files_skipped` | INTEGER | Files that didn't pass filter/whitelist |
| `files_ingested` | INTEGER | New photos added |
| `files_duplicate` | INTEGER | Hash already existed |
| `files_error` | INTEGER | IO/parse errors |

### `events`
| Column | Type | Notes |
|---|---|---|
| `id` | INTEGER PK | |
| `label` | TEXT | Auto-generated or user-set (future) |
| `started_at` | DATETIME | |
| `ended_at` | DATETIME | |
| `centroid_lat` | REAL | Nullable |
| `centroid_lon` | REAL | Nullable |
| `photo_count` | INTEGER | |

### `photo_events`
Junction table linking photos to events (many-to-many for future flexibility):

| Column | Type |
|---|---|
| `photo_id` | INTEGER FK |
| `event_id` | INTEGER FK |

---

## 4. Deduplication Report

A dedicated view in the webapp shows:

- **Per library path summary**: total photos, unique photos, duplicate count, photos unique to this path (not present in any other).
- **Cross-path overlap table**: for each pair of library paths, how many photos overlap (same hash found in both).
- **Subtree analysis**: for any selected directory subtree, show:
  - Total photos in subtree
  - How many are duplicated elsewhere (with links to the other locations)
  - How many are unique to this subtree (not found anywhere else)
  - A "delta" — photos present in a comparison subtree but missing from this one, and vice versa

This is read-only and informational. No delete or move operations.

---

## 5. Gallery / Browsing Views

### 5.1 Folder browser

- Navigates the directory tree of a selected library path.
- Shows folders as expandable nodes; selecting a folder shows its photos in a grid.
- Scroll between photos within a folder.
- URL pattern: `/browse/<library_path_id>/<relative_path>`

### 5.2 Photo detail view

- Full-size image display (served from disk, not thumbnail).
- EXIF data panel.
- Duplicate locations listed if any.
- Event membership if clustered.
- URL pattern: `/photo/<sha256>`

### 5.3 Search / filter

- Filter by: date range, camera make/model, has GPS, event, library path.
- Keyword search on filepath/filename.
- URL pattern: `/search?from=...&to=...&make=...` etc. (query string preserves state)

---

## 6. Geo View

- Leaflet.js map showing all geotagged photos as clustered markers.
- Click a cluster to expand or see photo thumbnails.
- **Radius search**: click a point on the map to set a centre, enter a radius in miles/km, and filter to photos within that circle. Rendered as a circle overlay on the map.
- URL pattern: `/map?lat=...&lon=...&radius=...`

---

## 7. Timeline View

- Horizontal timeline with zoom levels: decade → year → month → week → day.
- Background bar chart showing photo count per time bucket.
- Scrubbing / clicking a period loads the photo grid for that period.
- URL pattern: `/timeline?from=...&to=...&zoom=month`

---

## 8. Event Clustering (Rule-Based MVP)

Events are computed by the backend and stored in the `events` / `photo_events` tables. Clustering is triggered after a rescan.

### Rules

1. Sort all photos by `captured_at`.
2. Group consecutive photos where the gap between adjacent photos is ≤ 2 days.
3. If a gap is ≤ 2 days but the two photos are geographically distant (> 500 km apart, configurable), treat as a new event boundary.
4. Each resulting group becomes an event.
5. Event label is auto-generated as date range (e.g. "14–18 Aug 2019"). User labelling is future work.

Events are browseable from the gallery and linkable from the timeline and map views.

---

## 9. Authentication

- Optional HTTP basic password gate (toggled in `config.json`).
- Single password; bcrypt-hashed in config.
- Session cookie valid for configurable duration (default 24h).
- No user account management — single password, change via settings UI or direct config edit.

---

## 10. API Design (Backend → Frontend)

All API responses are JSON. Key endpoints:

| Method | Path | Description |
|---|---|---|
| GET | `/api/photos` | Paginated photo list with filters |
| GET | `/api/photos/:sha256` | Single photo metadata |
| GET | `/api/photos/:sha256/image` | Serve original file |
| GET | `/api/photos/:sha256/thumbnail` | Serve thumbnail |
| GET | `/api/browse/:library_id/*path` | Directory tree listing |
| GET | `/api/map/photos` | Geotagged photos (lat/lon/thumbnail) |
| GET | `/api/timeline` | Photo counts bucketed by time period |
| GET | `/api/events` | Event list |
| GET | `/api/events/:id` | Event detail + photos |
| GET | `/api/dedup/report` | Deduplication report |
| GET | `/api/dedup/subtree` | Subtree delta analysis |
| GET | `/api/settings` | Read current config |
| POST | `/api/settings` | Update config |
| POST | `/api/scan` | Trigger rescan (all or one path) |
| GET | `/api/scan/status` | Scan progress / last run stats |
| GET | `/api/issues` | Ingest issues/flags |

---

## 11. Non-Functional Requirements

- **Scale**: designed for 1,000–10,000 photos; queries should return in < 500ms for typical filters.
- **Concurrency**: Go backend handles concurrent thumbnail generation during scans using goroutines with a configurable worker pool.
- **No internet dependency** at runtime (all map tiles should work offline if needed, or use a tile provider the user configures — Leaflet supports both).
- **Single binary + static assets** deployment model where possible.
- **No framework build step** for frontend — plain `.js` and `.css` files served as static assets.

---

## Future Features

The following are explicitly out of scope for the initial implementation but should be kept in mind during schema and architecture decisions:

- **Write operations**: adding captions, tags, custom album creation, marking photos as excluded.
- **Face detection and recognition**: extract face embeddings per photo; API hooks to integrate with a separate face-recognition tool. Schema should reserve a `faces` table.
- **ML-based event clustering**: the `captured_at`, `latitude`, `longitude`, and sequence data already stored will serve as feature inputs. No additional schema changes required.
- **MCP server integration**: exposing photo search and metadata via an MCP server so AI assistants can query the library.
- **RAW file support**: preview generation from RAW formats (camera-vendor-specific, deferred).
- **Multi-user support**: per-user views or sharing links.
- **Export / download**: zip download of a filtered set or event.
- **Slideshow mode**: auto-advance full-screen view.
- **Map tile configuration**: offline tile support or configurable tile provider.
