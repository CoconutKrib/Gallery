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

# Verify scan is idempotent (should show skipped:5, ingested:0)
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

## Key conventions

- **`captured_at` must be stored as UTC RFC3339** (`time.RFC3339`) so SQLite's `strftime()` can group by time bucket. Never pass a raw `time.Time` to the DB driver for this column — always call `.UTC().Format(time.RFC3339)`.
- **`flags` JSON column** — always serialise as `[]string{}` (non-nil slice), never `nil`. Read back through `normalizeFlags()` which handles legacy `"null"` strings.
- **Static assets** are embedded with `//go:embed all:web` in `main.go`. Use `fs.Sub(webFS, "web")` to strip the prefix so URLs like `/css/app.css` work correctly.
- **Go 1.22+ mux patterns** — method in pattern string: `"GET /api/photos"`. Path values via `r.PathValue("sha256")`.
- **Security**: all file serving validates paths are within configured library roots or cache dir (`pathIsWithinRoots`). Never serve arbitrary filesystem paths.
- **Auth middleware** — no-op when `auth.enabled = false`. API routes 401, page routes redirect to `/login`.

## Project structure

```
main.go                        # embed + wire Handlers + http.ListenAndServe
internal/
  config/config.go             # Config struct, atomic load/save
  db/
    db.go                      # Open SQLite (WAL, FK on), run embed.FS migrations
    migrations/001_initial.sql # Full schema
    photos.go                  # Photo CRUD; InsertPhoto, scanPhotoRows
    queries.go                 # PhotoFilter + ListPhotosFiltered, GetGeotaggedPhotos
    duplicates.go              # duplicate_paths CRUD
    library_paths.go           # UpsertLibraryPath, GetLibraryPathByID
    scan_runs.go               # InsertScanRun, FinishScanRun, GetAllLatestScanRuns
  scan/
    scanner.go                 # Walk + filter; OnProgress callback; canonical-path dedup fix
    exif.go                    # EXIF extraction; Flags() returns []string{} never nil
    hash.go                    # SHA-256
    thumbnail.go               # 400px long-edge JPEG; idempotent
  auth/auth.go                 # In-memory session store (token→expiry), bcrypt, cookie
  api/
    router.go                  # Handlers struct, RegisterRoutes, authMiddleware, helpers
    photos.go                  # /api/photos (q, from, to, make, model, has_gps, flag)
    browse.go                  # /api/browse/{library_id}/{path...}, /api/libraries
    scan.go                    # ScanManager, /api/scan trigger+status
    timeline.go                # /api/timeline?zoom=decade|year|month|week|day
    settings.go                # /api/settings, /api/login, /api/logout, /api/issues
web/
  index.html                   # SPA shell; loads vendor/plotly.min.js + all JS modules
  css/app.css                  # Dark theme (CSS custom properties)
  js/
    app.js                     # History API router
    utils.js                   # api(), formatDate, esc, navigate, setActiveNav
    browse.js                  # Folder browser + photo grid
    photo.js                   # Photo detail + EXIF table + duplicates
    search.js                  # Filter form + paginated grid (URL query state)
    timeline.js                # Plotly bar chart + click-to-grid
    settings.js                # Library paths, scan trigger/poll, config display
  vendor/plotly.min.js         # Plotly basic bundle (~1MB, vendored)
```

## Phase status

- ✅ **Phase 1** — Ingest pipeline (scan, EXIF, hash, dedup, thumbnails). Verified with 5 sample photos.
- ✅ **Phase 2** — HTTP server, auth, browse API, photo detail API, scan manager, settings API, frontend SPA.
- ✅ **Phase 3** — Search/filter view, timeline view (Plotly), `/api/timeline` endpoint.
- ⬜ **Phase 4** — Geo/map view (Leaflet, radius search, server-side Haversine).
- ⬜ **Phase 5** — Event clustering (rule-based, `internal/cluster`), event browsing, dedup report.
- ⬜ **Phase 6** — Settings UI enhancements (inline editing of whitelist/filters, issues panel).

## Known issues / backlog (from TODO.md)

- Manual lat/lon override for photos missing GPS — store as sidecar-style DB data with an `approximate` flag and optional radius.
- Notes/description fields per photo.
- Export of data/metadata.

## Sample data

`samples/` contains 5 test JPEGs from 4 cameras (HTC One, Canon EOS 700D, Apple iPhone SE, Samsung Techwin L100). The iPhone SE photo has real GPS coordinates. `test_config.json` whitelists all four cameras with `scan_workers: 2`.

## Gotchas encountered

- **`exif.BodySerialNumber`** is not exported by `goexif` — use raw string `"BodySerialNumber"` as field name (gracefully returns empty).
- **Rescan false-duplicates** — `PhotoExistsByHash` alone isn't enough. Compare `GetCanonicalFilepath` against the current path; only insert into `duplicate_paths` when they differ.
- **`flags` stores `"null"`** when inserting a nil `[]string`. Fix: initialise as `[]string{}` in `exif.go` and guard in `normalizeFlags()`.
- **`captured_at` strftime failure** — Go's `time.Time.String()` produces `"2012-04-15 18:58:46 +0100 BST"` which SQLite cannot parse with `strftime`. Always format as `time.RFC3339` before inserting.
