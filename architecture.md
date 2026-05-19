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
├── main.go                  # Entry point: loads config, opens DB, starts server
├── config.json              # Runtime configuration (user-editable)
├── gallery.db               # SQLite database (generated at runtime)
├── .cache/                  # Thumbnail cache (generated at runtime)
│
├── internal/
│   ├── config/
│   │   └── config.go        # Config struct, load/save from JSON
│   │
│   ├── db/
│   │   ├── db.go            # Open connection, run migrations
│   │   ├── migrations/
│   │   │   └── 001_initial.sql  # Full schema (photos, duplicates, scan_runs, events stub)
│   │   ├── photos.go        # Photo CRUD + scanPhotoRows helper
│   │   ├── duplicates.go    # Duplicate path queries
│   │   ├── queries.go       # Filtered list queries (ListPhotosFiltered with keyword/date, GetGeotaggedPhotos)
│   │   ├── scan_runs.go     # Scan run queries
│   │   └── library_paths.go # Library path queries
│   │
│   ├── scan/
│   │   ├── scanner.go       # Directory walk, filter, dispatch; OnProgress callback
│   │   ├── exif.go          # EXIF extraction and camera whitelist validation
│   │   ├── hash.go          # SHA-256 content hashing
│   │   └── thumbnail.go     # JPEG thumbnail generation (400px long edge)
│   │
│   ├── api/
│   │   ├── router.go        # Route registration, Handlers struct, authMiddleware
│   │   ├── photos.go        # /api/photos list (keyword/date/make/model/gps/flag filters), detail, image, thumbnail
│   │   ├── browse.go        # /api/browse and /api/libraries handlers
│   │   ├── scan.go          # /api/scan trigger + status, ScanManager
│   │   ├── timeline.go      # /api/timeline — bucket counts by zoom level (decade/year/month/week/day)
│   │   └── settings.go      # /api/settings get/post, login/logout, issues
│   │
│   └── auth/
│       └── auth.go          # In-memory session store, bcrypt login, middleware helper
│
└── web/                     # Frontend static assets (embedded into binary)
    ├── index.html           # SPA shell with nav
    ├── css/
    │   └── app.css          # Dark-theme CSS (custom properties, no Tailwind yet)
    ├── js/
    │   ├── app.js           # Client-side router (History API)
    │   ├── utils.js         # Shared: api(), formatDate, esc, navigate
    │   ├── browse.js        # Folder browser view
    │   ├── photo.js         # Photo detail view
    │   ├── search.js        # Search / filter view
    │   ├── timeline.js      # Timeline view (Plotly bar chart)
    │   └── settings.js      # Settings / scan management view
    └── vendor/
        └── plotly.min.js    # Plotly basic bundle (vendored, ~1MB)
```

---

## 3. Backend Architecture

### 3.1 Entry point (`main.go`)

1. Parse CLI flags: `--config` (default `./config.json`), `--port` (default `8080`).
2. Load `config.json`; create with defaults if not present.
3. Open SQLite connection; run pending migrations.
4. Register HTTP routes.
5. Start HTTP server.

### 3.2 Configuration (`internal/config`)

`Config` is a Go struct that maps directly to `config.json`. It is loaded once at startup and held in memory. Write operations (from the settings API) update the in-memory struct and flush to disk atomically (write to a temp file, then rename).

```go
type Config struct {
    LibraryPaths    []LibraryPath     `json:"library_paths"`
    CameraWhitelist []CameraEntry     `json:"camera_whitelist"`
    FilenameFilters FilenameFilters   `json:"filename_filters"`
    Auth            AuthConfig        `json:"auth"`
    DBPath          string            `json:"db_path"`
    CacheDir        string            `json:"cache_dir"`
    ScanWorkers     int               `json:"scan_workers"`       // thumbnail goroutine pool size, default 4
    EventGapDays    int               `json:"event_gap_days"`     // default 2
    EventGeoKm      float64           `json:"event_geo_km"`       // default 500
    SessionTTLHours int               `json:"session_ttl_hours"`  // default 24
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
    ├── file passes filters
    │       │
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
    │   └── no  → ingest.go
    │               ├── exif.go (full field extraction)
    │               ├── thumbnail job → buffered channel
    │               └── db: insert photos row
    │
    └── thumbnail worker pool (N goroutines consuming channel)
            └── thumbnail.go → decode JPEG/HEIC → resize → write .cache/
```

**Thumbnail worker pool**: ingest enqueues thumbnail jobs onto a buffered channel. A fixed pool of `ScanWorkers` goroutines consumes the channel. This keeps image decoding/resizing parallel while preventing memory exhaustion.

**HEIC decoding**: `github.com/strukturag/libheif` Go bindings or `github.com/jdeng/goheif` — to be evaluated at implementation time for CGO trade-offs. If CGO is undesirable, thumbnails for HEIC files can be deferred to a separate tool (e.g. `heif-convert` CLI) called via `os/exec`.

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

Server returns `index.html` for all non-API, non-asset routes (so deep links work on hard refresh).

### 4.2 Page modules

Each page module exports an `init(params)` function that:
1. Fetches required data from the API.
2. Builds DOM (via template literals or simple DOM manipulation — no virtual DOM).
3. Registers event listeners.
4. Updates the `<title>` and any nav state.

Shared utilities (API fetch wrapper with error handling, thumbnail lazy loading, date formatting) live in `js/utils.js`.

### 4.3 Map view (`map.js`)

- Leaflet.js initialised with OpenStreetMap tiles (configurable tile URL in config, defaulting to OSM).
- `MarkerCluster` plugin for cluster rendering.
- On radius search: user clicks map → Leaflet `L.circle` rendered → radius input shown → on confirm, navigate to `/map?lat=...&lon=...&radius=...` which triggers an API call to `/api/map/photos?lat=...&lon=...&radius=...`.
- Haversine filtering is done server-side in SQL using a bounding-box pre-filter (fast index scan) followed by exact distance calculation in Go.

### 4.4 Timeline view (`timeline.js`)

- Plotly bar chart showing photo counts per bucket (bucket size determined by current zoom level).
- Clicking a bar navigates to the photo grid filtered to that time window.
- Zoom controls update the `zoom` query param and re-fetch bucket data.

### 4.5 Settings view (`settings.js`)

- Lists library paths with last-scan status and a per-path "Rescan" button.
- Rescan triggers `POST /api/scan` with the path ID; polls `GET /api/scan/status` every 2 seconds to show progress (files found / ingested / duplicate / error counts from `scan_runs`).
- Camera whitelist and filename filter editors with inline validation (regex patterns are tested client-side before saving).

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
| `github.com/jdeng/goheif` (future) | HEIC decoding — deferred to a later phase |
| Plotly.js (vendored, basic bundle) | Timeline bar chart |

There is no npm build step. JS libraries (Leaflet, Plotly, Tailwind) will be added to `web/vendor/` as needed in later phases.

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

### Phase 4 — Geo view
- Map view with Leaflet, radius search, server-side geo filtering.

### Phase 5 — Events and dedup
- Rule-based event clustering, event browsing, dedup report.

### Phase 6 — Settings UI enhancements
- Inline config editing (camera whitelist, filename filters), ingest issues panel.
