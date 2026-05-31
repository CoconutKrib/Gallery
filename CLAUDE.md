# CLAUDE.md — Gallery project notes for AI assistants

## What this project is

A self-hosted family photo management webapp. Single Go binary serves both the REST API and all static frontend assets. Single-user, read-only filesystem (never writes/moves/deletes source photos).

## Build and run

```bash
# Build
go build ./...

# Re-ingest samples (wipe DB first for a clean run)
rm -f test_gallery.db && rm -rf .test_cache
go run . --config test_config.json --scan

# Start HTTP server
go run . --config test_config.json          # listens on :8080

# Verify scan is idempotent (should show skipped:7, ingested:0)
# (7 = 5 originals + 2 copies in samples/duplicates/)
go run . --config test_config.json --scan
```

## Tech stack

| Layer | Choice |
|---|---|
| Language | Go 1.25, module `github.com/halleck/gallery` |
| DB driver | `modernc.org/sqlite` (pure Go, no CGO) |
| EXIF | `github.com/rwcarlsen/goexif/exif` |
| Image resize | `golang.org/x/image/draw` |
| Auth | `golang.org/x/crypto/bcrypt` |
| Frontend | Vanilla JS, no framework, no build step |
| Charts | Plotly basic bundle vendored at `web/vendor/plotly.min.js` |
| Maps | Leaflet 1.9.4 vendored at `web/vendor/leaflet/` |

## Key conventions

- **`captured_at` must be stored as UTC RFC3339** (`time.RFC3339`) so SQLite's `strftime()` can group by time bucket. Never pass a raw `time.Time` to the DB driver for this column — always call `.UTC().Format(time.RFC3339)`.
- **`flags` JSON column** — always serialise as `[]string{}` (non-nil slice), never `nil`. Read back through `normalizeFlags()` which handles legacy `"null"` strings.
- **Static assets** are embedded with `//go:embed all:web` in `main.go`. Use `fs.Sub(webFS, "web")` to strip the prefix so URLs like `/css/app.css` work correctly.
- **Go 1.22+ mux patterns** — method in pattern string: `"GET /api/photos"`. Path values via `r.PathValue("sha256")`.
- **Security**: all file serving validates paths are within configured library roots or cache dir (`pathIsWithinRoots`). Never serve arbitrary filesystem paths.
- **Auth middleware** — no-op when `auth.enabled = false`. API routes 401, page routes redirect to `/login`.
- **Filename filters are case-insensitive** — all include/exclude regex patterns are automatically wrapped with `(?i)` at compile time (`caseInsensitivePattern()` in `scanner.go`). Exclude beats include: if both lists are configured, a file must pass include first, then not match any exclude.
- **Logging — always use `log/slog`, never `log.Printf`**. The global default logger is initialised by `logging.Setup()` in `main.go` (writes to stderr + optional `log_file`). Use structured key-value attributes, not `%v` format strings:
  ```go
  slog.Info("scan: done", "path", lp.Path, "found", stats.Found, "errors", stats.Errors)
  slog.Warn("thumbnail error", "path", job.SourcePath, "err", err)
  slog.Error("scan insert error", "path", path, "err", insertErr)
  ```
  Level guide: `Info` for normal milestones, `Warn` for recoverable problems (single file failures), `Error` for failures that affect correctness. `Debug` for verbose detail useful only when diagnosing. All HTTP requests are logged automatically by `logging.HTTPMiddleware` — do not log them manually in handlers. Never import `"log"` in new files; always import `"log/slog"`.

## Project structure

```
main.go                        # embed + wire Handlers + http.ListenAndServe
internal/
  config/config.go             # Config struct, atomic load/save
  db/
    db.go                      # Open SQLite (WAL, FK on), run embed.FS migrations
    migrations/001_initial.sql # Full schema
    photos.go                  # Photo CRUD; InsertPhoto, scanPhotoRows
    queries.go                 # PhotoFilter + ListPhotosFiltered, GetGeotaggedPhotos, GetPhotosNearby
    duplicates.go              # duplicate_paths CRUD
    events.go                  # Event + photo_events CRUD (ClearEvents, InsertEvent, GetAllEvents, GetPhotosForEvent, GetEventForPhoto)
    dedup_report.go            # GetLibraryDedupSummaries, GetCrossPathOverlap, GetSubtreeDedupEntries
    library_paths.go           # UpsertLibraryPath, GetLibraryPathByID
    scan_runs.go               # InsertScanRun, FinishScanRun, GetAllLatestScanRuns
  scan/
    scanner.go                 # Walk + filter; OnProgress callback; canonical-path dedup fix; case-insensitive filename filters ((?i) wrap)
    scanner_filter_test.go     # Tests for include/exclude filter logic (case sensitivity, exclude-beats-include, multiple patterns)
    exif.go                    # EXIF extraction; Flags() returns []string{} never nil
    hash.go                    # SHA-256
    thumbnail.go               # 400px long-edge JPEG; idempotent
  cluster/
    cluster.go                 # Rule-based event clustering (gap days + geo distance); called after every scan
  auth/auth.go                 # In-memory session store (token→expiry), bcrypt, cookie
  api/
    router.go                  # Handlers struct, RegisterRoutes, authMiddleware, helpers
    photos.go                  # /api/photos (q, from, to, make, model, has_gps, flag); detail includes event_id + duplicates
    browse.go                  # /api/browse/{library_id}/{path...}, /api/libraries
    scan.go                    # ScanManager, /api/scan trigger+status; triggers cluster.Run after scan
    map.go                     # /api/map (all pins), /api/map/nearby?lat&lon&radius_km
    timeline.go                # /api/timeline?zoom=decade|year|month|week|day
    events.go                  # /api/events, /api/events/{id}
    dedup.go                   # /api/dedup/report, /api/dedup/subtree?prefix=
    settings.go                # /api/settings, /api/login, /api/logout, /api/issues
web/
  index.html                   # SPA shell; loads all JS modules
  css/app.css                  # Dark theme (CSS custom properties)
  js/
    app.js                     # History API router (routes: browse, photo, search, timeline, map, events, dedup, settings)
    utils.js                   # api(), formatDate, formatCoord, esc, navigate, setActiveNav
    browse.js                  # Folder browser + photo grid
    photo.js                   # Photo detail + EXIF table + duplicates + event link
    search.js                  # Filter form + paginated grid (URL query state)
    timeline.js                # Plotly bar chart + click-to-grid
    map.js                     # Leaflet map, all pins + radius search with circle overlay
    events.js                  # Event list cards + event detail photo grid
    dedup.js                   # Per-library summary, cross-library overlap, subtree analyser
    settings.js                # Library paths, scan trigger/poll, config display
  vendor/
    plotly.min.js              # Plotly basic bundle (~1MB, vendored)
    leaflet/                   # Leaflet 1.9.4 (JS, CSS, marker images)
samples/
  *.JPG / *.jpg                # 5 real test photos from 4 cameras
  duplicates/                  # Byte-identical copies of 2 samples (for dedup testing)
```

## Phase status

- ✅ **Phase 1** — Ingest pipeline (scan, EXIF, hash, dedup, thumbnails). Verified with 5 sample photos.
- ✅ **Phase 2** — HTTP server, auth, browse API, photo detail API, scan manager, settings API, frontend SPA.
- ✅ **Phase 3** — Search/filter view, timeline view (Plotly), `/api/timeline` endpoint.
- ✅ **Phase 4** — Geo/map view (Leaflet, radius search, server-side Haversine).
- ✅ **Phase 5** — Event clustering (rule-based, `internal/cluster`), event browsing (`/events`, `/api/events`), dedup report (`/dedup`, `/api/dedup/report`, `/api/dedup/subtree`).
- ⬜ **Phase 6** — Settings UI enhancements (inline editing of whitelist/filters, issues panel).

## Known issues / backlog (from TODO.md)

- Manual lat/lon override for photos missing GPS — store as sidecar-style DB data with an `approximate` flag and optional radius.
- Notes/description fields per photo.
- Export of data/metadata.
- Settings UI enhancements: inline editing of camera whitelist, filename filters, issues panel (Phase 6).

## Sample data

`samples/` contains 5 test JPEGs from 4 cameras (HTC One, Canon EOS 700D, Apple iPhone SE, Samsung Techwin L100). The iPhone SE photo has real GPS coordinates. `test_config.json` whitelists all four cameras with `scan_workers: 2`.

`samples/duplicates/` contains byte-identical copies of `SDC12869.JPG` and `IMG_0361.JPG`, used to exercise the duplicate detection and dedup report. A clean scan should show: found:7, ingested:5, duplicate:2, then cluster into 4 events.

## Gotchas encountered

- **`exif.BodySerialNumber`** is not exported by `goexif` — use raw string `"BodySerialNumber"` as field name (gracefully returns empty).
- **Rescan false-duplicates** — `PhotoExistsByHash` alone isn't enough. Compare `GetCanonicalFilepath` against the current path; only insert into `duplicate_paths` when they differ.
- **`flags` stores `"null"`** when inserting a nil `[]string`. Fix: initialise as `[]string{}` in `exif.go` and guard in `normalizeFlags()`.
- **`captured_at` strftime failure** — Go's `time.Time.String()` produces `"2012-04-15 18:58:46 +0100 BST"` which SQLite cannot parse with `strftime`. Always format as `time.RFC3339` before inserting.
- **Filename filter case sensitivity** — regex patterns compiled as-is are case-sensitive; `^dscn` won't match `DSCN0042.JPG`. All patterns are wrapped with `(?i)` via `caseInsensitivePattern()` before compiling.
- **Cluster `photoPoint` type** must be declared at package scope, not inside `Run()`, because `centroid()` takes `[]photoPoint` as a parameter.
