# Family Photo Gallery — Architecture

## 1. Overview

The application is a single Go binary that serves both the REST API and all frontend static assets. There is no separate build step for the frontend. The database is a single SQLite file. Thumbnails are cached on disk. Configuration lives in a JSON file.

```
┌─────────────────────────────────────────────────┐
│                  Browser                         │
│  Vanilla JS + Leaflet + Plotly + Tailwind        │
└─────────────┬───────────────────────────────────┘
              │ HTTP (REST + static assets)
┌─────────────▼───────────────────────────────────┐
│               Go HTTP Server                     │
│  ┌─────────┐ ┌──────────┐ ┌──────────────────┐  │
│  │ Auth    │ │ API      │ │ Static file       │  │
│  │ middleware│ │ handlers │ │ handler          │  │
│  └─────────┘ └────┬─────┘ └──────────────────┘  │
│                   │                              │
│  ┌────────────────▼─────────────────────────┐   │
│  │              Service layer                │   │
│  │  ScanService │ PhotoService │ EventService│   │
│  └────────────────┬─────────────────────────┘   │
│                   │                              │
│  ┌────────────────▼─────────────────────────┐   │
│  │           Database layer (SQLite)         │   │
│  └───────────────────────────────────────────┘  │
└─────────────────────────────────────────────────┘
         │                        │
   config.json              .cache/ (thumbnails)
```

---

## 2. Project Structure

```
gallery/
├── main.go                  # Entry point: loads config, opens DB, triggers scan/cluster, starts server
├── config.json              # Runtime configuration (user-editable)
├── gallery.db               # SQLite database (generated at runtime)
├── .cache/                  # Thumbnail cache (generated at runtime)
├── heif/                    # Bundled static HEIC decode libraries (libheif + libde265)
│   ├── build_libs.sh        # One-time script to build .a files from source
│   ├── include/             # C headers (committed)
│   └── lib/linux-x64/       # Static libraries (committed, ~4.7 MB)
│
├── internal/
│   ├── config/
│   │   └── config.go        # Config struct, load/save from JSON
│   │
│   ├── db/
│   │   ├── db.go            # Open connection, run migrations
│   │   ├── migrations/
│   │   │   ├── 001_initial.sql  # Full schema (photos, duplicates, scan_runs, events, photo_events, faces stub)
│   │   │   └── 002_internal_library.sql  # source column on photos, staging_queue, library_copies
│   │   ├── photos.go        # Photo CRUD + scanPhotoRows helper
│   │   ├── duplicates.go    # Duplicate path queries
│   │   ├── queries.go       # ListPhotosFiltered, GetGeotaggedPhotos, GetPhotosNearby (bounding-box + Haversine)
│   │   ├── events.go        # Event + photo_events CRUD (ClearEvents, InsertEvent, GetAllEvents, GetPhotosForEvent, GetEventForPhoto)
│   │   ├── dedup_report.go  # GetLibraryDedupSummaries, GetCrossPathOverlap, GetSubtreeDedupEntries
│   │   ├── scan_runs.go     # Scan run queries
│   │   ├── library_paths.go # Library path queries
│   │   ├── staging.go       # staging_queue CRUD (UpsertStaging, GetStaging, UpdateStagingState, ListByState)
│   │   └── library_copies.go # library_copies CRUD (InsertCopy, GetCopyBySHA, ListLibraryCopies)
│   │
│   ├── scan/
│   │   ├── scanner.go       # Directory walk, filter, dispatch; OnProgress callback; case-insensitive filename filters
│   │   ├── scanner_filter_test.go  # Unit tests: include/exclude logic, case insensitivity, exclude-beats-include
│   │   ├── exif.go          # EXIF extraction and camera whitelist validation
│   │   ├── hash.go          # SHA-256 content hashing
│   │   └── thumbnail.go     # JPEG thumbnail generation (400px long edge)
│   │
│   ├── cluster/
│   │   └── cluster.go       # Rule-based event clustering (gap days + geo distance); called after every scan
│   │
│   ├── recognition/
│   │   ├── recognition.go   # Status struct; Init(cfg); IsAvailable(); cluster state (SetClusters/GetClusters/GetClusterIDForFace)
│   │   ├── detect.go        # SCRFD-10G ONNX session; Detect(img) []Detection; anchor decoding + NMS
│   │   ├── embed.go         # ArcFace/R50 ONNX session; Embed(img, det) []float32; L2-normalises output
│   │   ├── preprocess.go    # preprocessBGRFloat32, cropImage, l2NormalizeInPlace, EmbeddingToBytes, BytesToEmbedding
│   │   ├── cluster.go       # Union-find clustering; Cluster(faces, threshold, minSamples) []FaceCluster
│   │   └── suggest.go       # Nearest-neighbour suggestion; Suggest(unidentified, verified, threshold) []Suggestion
│   │
│   ├── library/
│   │   └── copy.go          # CopyToLibrary: path construction, collision resolution, os.Copy, DB insert; bulk copy triggers cluster.Run
│   │
│   ├── heif/
│   │   ├── heif.go           # CGO shim: statically links bundled libheif+libde265; Decode, DecodeConfig, ExtractEXIF
│   │   ├── heif_stub.go      # !cgo fallback returning ErrNotAvailable
│   │   └── heif_test.go      # Unit tests (decode real HEIC, EXIF extraction, invalid input)
│   │
│   ├── api/
│   │   ├── router.go        # Route registration, Handlers struct, authMiddleware
│   │   ├── photos.go        # /api/photos list + detail (includes event_id, duplicates) + image/thumbnail serve
│   │   ├── browse.go        # /api/browse and /api/libraries handlers
│   │   ├── scan.go          # /api/scan trigger + status, ScanManager (triggers cluster.Run then runRecognitionPostScan after each scan)
│   │   ├── map.go           # /api/map (all pins), /api/map/nearby?lat&lon&radius_km
│   │   ├── timeline.go      # /api/timeline — bucket counts by zoom level (decade/year/month/week/day)
│   │   ├── events.go        # /api/events list, /api/events/{id} detail + photos
│   │   ├── dedup.go         # /api/dedup/report (library summaries + cross-path overlap), /api/dedup/subtree
│   │   ├── settings.go      # /api/settings get/post, login/logout, issues, /api/recognition/status
│   │   ├── staging.go       # /api/staging CRUD + approve/reject state transitions
│   │   ├── library.go       # /api/library/copy, /api/library/status, /api/library/photos, /api/library/tree
│   │   └── people.go        # /api/people CRUD + /api/people/{id}/merge; /api/library/copies/{id}/faces; /api/faces/* (CRUD, confirm, reject, cluster)
│   │
│   └── auth/
│       └── auth.go          # In-memory session store, bcrypt login, middleware helper
│
└── web/                     # Frontend static assets (embedded into binary)
    ├── index.html           # SPA shell with nav
    ├── css/
    │   └── app.css          # Dark-theme CSS (custom properties)
    ├── js/
    │   ├── app.js           # Client-side router (History API)
    │   ├── utils.js         # Shared: api(), formatDate, formatCoord, esc, navigate
    │   ├── browse.js        # Folder browser view
    │   ├── photo.js         # Photo detail view (EXIF, duplicates, event link)
    │   ├── search.js        # Search / filter view
    │   ├── timeline.js      # Timeline view (Plotly bar chart)
    │   ├── map.js           # Geo map view (Leaflet, radius search, circle overlay)
    │   ├── events.js        # Event list + event detail photo grid
    │   ├── dedup.js         # Dedup report (per-library summary, cross-path overlap, subtree analyser)
    │   ├── settings.js      # Settings / scan management; recognition status section
    │   ├── staging.js       # Staging queue: photo grid + annotation form + approve/reject actions
    │   ├── library.js       # Internal library browse: folder tree + photo grid; edit panel with people/face-tagging
    │   └── people.js        # /people list, /people/{id} detail (edit + merge), /faces/review (cluster + suggestion UI)
    └── vendor/
        ├── plotly.min.js    # Plotly basic bundle (vendored, ~1MB)
        └── leaflet/         # Leaflet 1.9.4 (JS, CSS, marker images)
```

---

## 3. Backend Architecture

### 3.1 Entry point (`main.go`)

1. Parse CLI flags: `--config` (default `./config.json`), `--port` (default `8080`), `--scan` (run scan then exit).
2. Load `config.json`; create with defaults if not present.
3. Open SQLite connection; run pending migrations.
4. In `--scan` mode: run scan pipeline for all library paths, then run `cluster.Run()`, then exit.
5. In server mode: register HTTP routes and start HTTP server.

### 3.2 Configuration (`internal/config`)

`Config` is a Go struct that maps directly to `config.json`. It is loaded once at startup and held in memory. Write operations (from the settings API) update the in-memory struct and flush to disk atomically (write to a temp file, then rename).

```go
type Config struct {
    LibraryPaths    []LibraryPath          `json:"scan_paths"`
    CameraWhitelist []CameraEntry          `json:"camera_whitelist"`
    FilenameFilters FilenameFilters        `json:"filename_filters"`
    InternalLibrary InternalLibraryConfig  `json:"internal_library"`
    Dropzone        DropzoneConfig         `json:"dropzone"`
    FaceRecognition FaceRecognitionConfig  `json:"face_recognition"`
    Auth            AuthConfig             `json:"auth"`
    DBPath          string                 `json:"db_path"`
    CacheDir        string                 `json:"cache_dir"`
    ScanWorkers     int                    `json:"scan_workers"`       // thumbnail goroutine pool size, default 4
    EventGapDays    int                    `json:"event_gap_days"`     // default 2
    EventGeoKm      float64                `json:"event_geo_km"`       // default 500
    SessionTTLHours int                    `json:"session_ttl_hours"`  // default 24
}

type InternalLibraryConfig struct {
    Path    string `json:"path"`
    Enabled bool   `json:"enabled"`
}

type DropzoneConfig struct {
    Path    string `json:"path"`
    Enabled bool   `json:"enabled"`
}
```

### 3.3 Database layer (`internal/db`)

- Uses `database/sql` with the `modernc.org/sqlite` driver (pure Go, no CGO dependency).
- Migrations are numbered SQL files embedded via `embed.FS`. On startup, the DB layer checks a `schema_migrations` table and applies any unapplied migrations in order.
- Each domain entity has its own query file (e.g. `photos.go`) exposing typed functions rather than raw SQL in handlers.
- All writes use transactions where more than one statement is involved.

### 3.4 Scan pipeline (`internal/scan`)

The scan is designed to be non-blocking relative to the HTTP server — it runs in a background goroutine, and progress is written to the `scan_runs` table so the frontend can poll `/api/scan/status`.

```
scanner.go (walk + filter)
    │
    ├── isSupportedExtension (case-insensitive: .jpg .jpeg)
    │
    ├── passesFilenameFilters (all patterns compiled with (?i); include then exclude; exclude beats include)
    │       │ pass
    │       ▼
    │   exif.go → read EXIF → check camera whitelist
    │       │ pass
    │       ▼
    │   hash.go → SHA-256
    │       │
    │       ▼
    │   db: hash exists?
    │   ├── yes → is this filepath the canonical path already in photos?
    │   │         ├── yes (rescan of known file) → skip silently, do NOT touch duplicate_paths
    │   │         └── no  (different path, same hash) → duplicate_paths upsert (ignore if (sha256,filepath) exists)
    │   └── no  → ingest
    │               ├── exif.go (full field extraction)
    │               ├── thumbnail job → buffered channel
    │               └── db: insert photos row
    │
    └── thumbnail worker pool (N goroutines consuming channel)
            └── thumbnail.go → decode JPEG → resize → write .cache/<xx>/<sha256>.jpg

After all paths scanned:
    └── cluster.Run() → clear events → group photos by gap/geo → InsertEvent + InsertPhotoEvent
```

**Thumbnail worker pool**: ingest enqueues thumbnail jobs onto a buffered channel. A fixed pool of `ScanWorkers` goroutines consumes the channel. This keeps image decoding/resizing parallel while preventing memory exhaustion.

**HEIC support**: HEIC/HEIF files are handled throughout the pipeline. A CGO shim (`internal/heif`) statically links bundled `libheif` + `libde265` (committed as `.a` files in `heif/lib/linux-x64/`). `isSupportedExtension` accepts `.heic`/`.heif` alongside `.jpg`/`.jpeg`. `decodeImage` routes HEIC through the shim to produce `image.NRGBA` for thumbnails and face detection. `ReadEXIF` extracts HEIC EXIF via the shim (HEIC stores EXIF in an ISOBMFF container, not a JPEG APP1 segment, so `goexif` cannot find it directly). The `photos.format` column tracks `'jpeg'` vs `'heic'` per ingested photo. When serving HEIC originals to browsers, the pre-generated JPEG thumbnail is served instead (most browsers cannot render HEIC natively); `?original=1` forces raw HEIC download; `?format=jpeg` transcodes the full-resolution HEIC to JPEG on the fly via the existing `heif.Decode` → `jpeg.Encode` pipeline.

**Internal library path exclusion**: at the start of every scan walk, `scanner.go` must compare the walk root against `config.InternalLibrary.Path` (and `config.Dropzone.Path` for dropzone-aware scans). If a root equals or is a subdirectory of the internal library path it is silently skipped before walking. This prevents the managed copy tree from being inadvertently treated as a source.

### 3.5 Event clustering (`internal/cluster`)

Triggered at the end of a successful scan run. Steps:

1. Load all photos with non-null `captured_at`, ordered by `captured_at ASC`.
2. Walk the list; start a new event group when:
   - Gap to previous photo > `EventGapDays`, **or**
   - Gap ≤ `EventGapDays` but Haversine distance to previous geotagged photo > `EventGeoKm` (only applied if both photos have GPS; if either lacks GPS, gap-only rule applies).
3. For each group, compute: `started_at`, `ended_at`, centroid lat/lon (mean of available GPS), `photo_count`.
4. Delete all existing `photo_events` and `events` rows, then reinsert. (Re-clustering is cheap at this scale; incremental patching adds complexity without meaningful benefit for 1k–10k photos.)

### 3.6 API handlers (`internal/api`)

All handlers follow the same pattern:

- Parse and validate query params / path params.
- Call a service/DB function.
- Write JSON response (`encoding/json`).
- On error, return appropriate HTTP status with a JSON `{"error": "..."}` body.

Pagination on list endpoints: `?page=1&per_page=50` (default 50). Response includes `total`, `page`, `per_page`, `items`.

### 3.7 Authentication (`internal/auth`)

- Middleware wraps all routes except the login endpoint and static assets.
- On login POST, compare submitted password against the bcrypt hash in config. On success, set a signed, HTTP-only session cookie (using a random 32-byte secret generated at first startup and stored in `config.json`).
- Middleware validates the cookie on every request. Redirects to login page on failure (for page routes) or returns 401 (for `/api/*`).
- When `auth.enabled` is false, the middleware is a no-op.

---

## 4. Frontend Architecture

### 4.1 Routing

A minimal client-side router in `app.js` intercepts `popstate` and link clicks, maps URL patterns to page modules, and renders the appropriate view into a `<main>` container. No framework — just a ~60-line router.

URL patterns match the slugs defined in requirements:
- `/browse/:libraryId/*path` → `browse.js`
- `/photo/:sha256` → `photo.js`
- `/search` → `search.js`
- `/map` → `map.js`
- `/timeline` → `timeline.js`
- `/events`, `/events/:id` → `events.js`
- `/dedup` → `dedup.js`
- `/settings` → `settings.js`
- `/staging` → `staging.js` (hidden when `internal_library.enabled = false`)
- `/library` → `library.js` (hidden when `internal_library.enabled = false`)
- `/people`, `/people/:id` → `people.js` (hidden when `internal_library.enabled = false`)
- `/faces/review` → `people.js` with `action: 'review'` (hidden unless recognition `enabled && available`)

### 4.2 Page modules

Each page module registers a function on `Gallery.pages[name]` that:
1. Sets active nav highlight via `Gallery.utils.setActiveNav()`.
2. Renders a loading skeleton into `<main id="app">`.
3. Fetches required data from the API.
4. Builds DOM via template literals.
5. Registers event listeners.

Shared utilities live in `utils.js`: `api()` (fetch wrapper with 401 redirect), `formatDate()`, `formatCoord()`, `esc()` (HTML entity escape), `navigate()` (History API pushState + dispatch).

### 4.3 Map view (`map.js`)

- Leaflet 1.9.4 (vendored) initialised with OpenStreetMap tiles.
- On page load: fetches `/api/map` and renders all geotagged photos as circle markers.
- Radius search: user clicks map to set a centre → `L.circle` overlay rendered → confirm fetches `/api/map/nearby?lat=…&lon=…&radius_km=…` → results grid below map.
- Haversine filtering is done server-side: bounding-box pre-filter in SQL (uses latitude/longitude indexes) followed by exact great-circle distance check in Go.

### 4.4 Timeline view (`timeline.js`)

- Plotly bar chart showing photo counts per bucket (bucket size determined by current zoom level).
- Clicking a bar navigates to the photo grid filtered to that time window.
- Zoom controls update the `zoom` query param and re-fetch bucket data.

### 4.5 Settings view (`settings.js`)

- Lists library paths with last-scan status and a per-path "Rescan" button.
- Rescan triggers `POST /api/scan` with the path ID; polls `GET /api/scan/status` every 2 seconds to show progress (files found / ingested / duplicate / error counts from `scan_runs`).
- Camera whitelist and filename filter editors with inline validation (regex patterns are tested client-side before saving).

### 4.6 Events view (`events.js`)

- List page: cards showing label (e.g. "14–18 Aug 2019"), photo count, date range, GPS centroid.
- Detail page: photo grid for the event; navigates to `/photo/:sha256` on click.
- Events are computed server-side by `cluster.Run()` and stored in `events` / `photo_events` tables.

### 4.7 Dedup view (`dedup.js`)

- Per-library summary table: total / unique / photos with duplicates elsewhere.
- Cross-library overlap table: shared hash count between every pair of library paths.
- Subtree analyser: enter a directory prefix, get per-file unique/dupe status with other-path list.

---

## 5. Database Migrations

Migrations are SQL files embedded in the binary using `//go:embed`. The `db` package maintains a `schema_migrations` table:

```sql
CREATE TABLE IF NOT EXISTS schema_migrations (
    version INTEGER PRIMARY KEY,
    applied_at DATETIME NOT NULL
);
```

On startup, the DB layer reads all embedded `migrations/*.sql` files, sorts by version number, and applies any not yet recorded in `schema_migrations`. This is a simple sequential migrator — no rollback support needed for a single-user local app.

---

## 6. Deployment

### 6.1 Standard

```
go build -o gallery ./...
./gallery --config config.json --port 8080
```

Static assets in `web/` are embedded into the binary via `//go:embed all:web`, so the single binary is self-contained.

### 6.2 Docker (optional, future)

A minimal `Dockerfile` can be provided:
```dockerfile
FROM golang:1.23-alpine AS build
WORKDIR /app
COPY . .
RUN go build -o gallery .

FROM alpine:3.20
WORKDIR /app
COPY --from=build /app/gallery .
VOLUME ["/data", "/photos"]
EXPOSE 8080
CMD ["./gallery", "--config", "/data/config.json"]
```

---

## 7. Key Dependencies

| Package | Purpose |
|---|---|
| `modernc.org/sqlite` | Pure-Go SQLite driver (no CGO) |
| `github.com/rwcarlsen/goexif/exif` | EXIF extraction from JPEG |
| `golang.org/x/image` | Image decoding/resizing for thumbnails |
| `golang.org/x/crypto/bcrypt` | Password hashing |
| `github.com/yalue/onnxruntime_go` | CGO bindings for ONNX Runtime; loads `libonnxruntime` dynamically at runtime |
| `libheif` + `libde265` (bundled, static `.a`) | HEIC/HEIF decode, EXIF extraction; statically linked into binary via CGO shim (`internal/heif`) |
| Plotly.js (vendored, basic bundle) | Timeline bar chart |
| Leaflet 1.9.4 (vendored) | Interactive geo map |

There is no npm build step.

---

## 8. Security Considerations

- Session secret is generated once at startup and stored in `config.json`; rotate by deleting the key and restarting.
- Session cookies are `HttpOnly`, `SameSite=Strict`, and `Secure` if served over HTTPS.
- File serving for originals and thumbnails validates that the resolved path is within the configured library paths or cache dir (path traversal prevention).
- The API does not expose the raw filesystem path in list responses where not needed — only in the photo detail view and dedup report.
- `config.json` should be protected by filesystem permissions (`chmod 600`) — noted in the README.

---

## 9. Implementation Order (Suggested Phases)

### Phase 1 — Core ingest ✅
- Config loading, DB schema + migrations, scan pipeline, EXIF extraction, SHA-256 dedup, thumbnail generation.
- Verified with real sample photos (5/5 ingested; rescan idempotent).

### Phase 2 — Gallery browsing ✅
- Folder browser, photo detail view, static asset serving, scan trigger/status, basic auth, settings view.
- All API endpoints and frontend pages verified working.

### Phase 3 — Search and timeline ✅
- Search/filter API: keyword, date range, make/model, GPS, flag filters wired into `/api/photos`.
- Timeline API: `/api/timeline?zoom=year|month|week|day|decade` returns bucket counts.
- `captured_at` stored as UTC RFC3339 so SQLite `strftime()` can group by time period.
- Frontend: `search.js` (filter form + paginated grid), `timeline.js` (Plotly bar chart + click-to-grid).
- Plotly basic bundle vendored at `web/vendor/plotly.min.js`.

### Phase 4 — Geo view ✅
- Leaflet 1.9.4 vendored; interactive map with all geotagged photos as markers.
- `/api/map` (all pins) and `/api/map/nearby?lat&lon&radius_km` (bounding-box SQL + Go-side Haversine).
- Radius search UX: click map → circle overlay → confirm → filtered results grid.

### Phase 5 — Events and dedup ✅
- `internal/cluster`: rule-based clustering (gap days + geo distance); runs after every scan.
- `internal/db/events.go`: `events` + `photo_events` CRUD.
- `internal/db/dedup_report.go`: per-library summary, cross-path overlap, subtree analysis.
- API: `/api/events`, `/api/events/{id}`, `/api/dedup/report`, `/api/dedup/subtree`.
- Photo detail API now includes `event_id`.
- Frontend: `events.js` (event list cards + detail grid), `dedup.js` (summary tables + subtree analyser).
- `samples/duplicates/` added for dedup testing; clean scan: found:7 ingested:5 duplicate:2 → 4 events.
- Filename filters: case-insensitive (`(?i)` wrap), exclude-beats-include, multiple patterns per list. Unit tests in `scanner_filter_test.go`.

### Phase 6 — Internal library + settings UI enhancements
- **Settings UI** (preliminary, mostly unrelated to the main feature): inline config editing (camera whitelist, filename filters), ingest issues panel.
- **Internal library infrastructure**: `InternalLibraryConfig` + `DropzoneConfig` in `Config`, DB migration `002_internal_library.sql` (`source` column on `photos`, `staging_queue`, `library_copies`), copy service (`internal/library/copy.go`), staging API (`/api/staging`), library API (`/api/library/copy`, `/api/library/status`, `/api/library/photos`, `/api/library/tree`), frontend `staging.js` + `library.js`.

### Phase 7 — Dropzone
- Lenient scanner mode (`strict` vs `lenient`), `source = 'dropzone'` tagging on ingested photos, auto-staging on ingest, `/api/scan` dropzone trigger.

### Phase 8 — Library copy editing & re-organisation ✅
- Migration `003_library_copy_metadata.sql`: `title`, `description`, `override_date`, `event_id` on `library_copies`.
- `PATCH /api/library/copies/{id}` with re-org trigger (`MovePhoto`, `pruneEmptyDirs`).
- Extended `GET /api/library/photos` filters: `source`, `has_date_override`, `true_date_unknown`, `tag`, `event_id`, `q`.
- Edit panel in `library.js`.

### Phase 9 — Library photo removal ✅
- `DELETE /api/library/copies/{id}` with full cascade: `photo_events → staging_queue → duplicate_paths → library_copies → photos` + physical file delete.
- Confirmation UI in `library.js`.

### Phase 10 — People tagging + face recognition ✅
- **Phase A (manual tagging)**: migration `004_people.sql` (`people` table + extended `faces`); full CRUD API for people and face tags; tagging panel in `library.js` edit panel; `/people` browse + `/people/{id}` detail with edit, delete, and merge actions.
- **Phase B (auto face detection)**: `internal/recognition` package (SCRFD-10G + ArcFace/R50 via `onnxruntime_go`); scanner auto-detects faces and stores bbox + 512-dim embeddings; `GET /api/recognition/status`; recognition-gated 501/503 on recognition endpoints.
- **Phase C (identity clustering + review UI)**: post-scan suggestion pipeline (per-person mean embedding nearest-neighbour); union-find clustering of unidentified faces; in-memory cluster store; `/api/faces/unidentified`, `/api/faces/suggestions`, `/api/faces/cluster`; `/faces/review` two-panel UI; recognition status section in Settings.

### Phase 11 — HEIC support ✅
- **Phase A (library bundling & CGO shim)**: `heif/build_libs.sh` builds `libde265` v1.0.15 + `libheif` v1.18.2 as static `.a` files committed to `heif/lib/linux-x64/`. C headers committed to `heif/include/`. `internal/heif` package provides `Decode` → `image.NRGBA`, `DecodeConfig`, `ExtractEXIF` → raw TIFF bytes. `!cgo` stub returns `ErrNotAvailable`. All 8 unit tests pass.
- **Phase B (scan pipeline integration)**: `isSupportedExtension` accepts `.heic`/`.heif`. `decodeImage` routes HEIC through `heif.Decode` for thumbnail generation and face detection. `ReadEXIF` extracts HEIC EXIF via `heif.ExtractEXIF` → `goexif` bridge.
- **Phase C (database & serving)**: Migration `005_heic.sql` adds `format` column (`'jpeg'`/`'heic'`). `Photo.Format` field populated at ingest, exposed in API responses. `handlePhotoImage` serves the pre-generated JPEG thumbnail for HEIC originals (browsers can't render HEIC natively); `?original=1` forces raw download; `?format=jpeg` transcodes full-resolution HEIC→JPEG on the fly via the existing `heif.Decode` → `jpeg.Encode` pipeline.
