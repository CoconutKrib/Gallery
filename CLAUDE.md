# CLAUDE.md — Gallery project notes for AI assistants

## What this project is

A self-hosted family photo management webapp. Single Go binary serves both the REST API and all static frontend assets. Single-user, read-only filesystem (never writes/moves/deletes source photos).

## HEIC support — build prerequisites

HEIC decode is statically linked into the binary via CGO. The static libraries
(`libheif.a`, `libde265.a`) and C headers are committed to `heif/lib/linux-x64/`
and `heif/include/`, so **no system packages are needed** to build or run.

To rebuild the static libs from source (only needed when upgrading versions):
```bash
./heif/build_libs.sh   # requires cmake, g++, git
```

If CGO is disabled (`CGO_ENABLED=0`), the `!cgo` stub in `internal/heif/heif_stub.go`
kicks in — HEIC files are skipped during scan with a warning, everything else
works normally.

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
# (9 = 5 originals + 2 copies + 2 HEIC samples in samples/)
go run . --config test_config.json --scan
```

## Testing guidance

- The authoritative test strategy and expansion plan is documented in `spec_test_suite.md`.
- When implementing a major feature or behavior change, add or update unit tests in the same PR whenever practical.
- Keep unit tests fast and deterministic; reserve filesystem-heavy or end-to-end behavior for integration tests.
- Prefer asserting observable contracts (status codes, payload shape, DB side effects) over internal implementation details.

## Tech stack

| Layer | Choice |
|---|---|
| Language | Go 1.25, module `github.com/halleck/gallery` |
| DB driver | `modernc.org/sqlite` (pure Go, no CGO) |
| EXIF | `github.com/rwcarlsen/goexif/exif` |
| Image resize | `golang.org/x/image/draw` |
| Auth | `golang.org/x/crypto/bcrypt` |
| Face recognition | `github.com/yalue/onnxruntime_go` — CGO, loads `libonnxruntime` at runtime (not needed at compile time) |
| HEIC decode | Bundled `libheif` + `libde265` (static `.a` files in `heif/lib/linux-x64/`) — CGO shim via `internal/heif`; statically linked at build time, no runtime `.so` dependency |
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
- **Config key compatibility** — canonical config key is `scan_paths`; legacy `library_paths` is still accepted on load and mapped to the same field (`Config.UnmarshalJSON`).
- **Logging — always use `log/slog`, never `log.Printf`**. The global default logger is initialised by `logging.Setup()` in `main.go` (writes to stderr + optional `log_file`). Use structured key-value attributes, not `%v` format strings:
  ```go
  slog.Info("scan: done", "path", lp.Path, "found", stats.Found, "errors", stats.Errors)
  slog.Warn("thumbnail error", "path", job.SourcePath, "err", err)
  slog.Error("scan insert error", "path", path, "err", insertErr)
  ```
  Level guide: `Info` for normal milestones, `Warn` for recoverable problems (single file failures), `Error` for failures that affect correctness. `Debug` for verbose detail useful only when diagnosing. All HTTP requests are logged automatically by `logging.HTTPMiddleware` — do not log them manually in handlers. Never import `"log"` in new files; always import `"log/slog"`.
- **Internal library** — when `internal_library.enabled = true`, `config.Validate()` checks that the library path does not overlap any scan library path or dropzone path. `scanner.go` skips any walk subtree that is inside `internal_library.path` (via `isInternalLibraryPath()`). All staging/library/people APIs return `409 Conflict` if `internal_library.enabled = false`. The body class `library-enabled` is set at startup by `app.js` and controls CSS visibility of Stage buttons and the People/Face-Review nav links (via `.nav-library-only` class).
- **Settings API update semantics** — `POST /api/settings` supports partial updates by section; handlers should only send fields they intend to change so unrelated config is not overwritten.

## Project structure

Consider cross checking against architecture.md for more detailed runthrough

```
main.go                        # embed + wire Handlers + http.ListenAndServe
internal/
  config/config.go             # Config struct (incl. InternalLibraryConfig, DropzoneConfig), Validate(), atomic load/save
  db/
    db.go                      # Open SQLite (WAL, FK on), run embed.FS migrations
    migrations/001_initial.sql # Full schema
    migrations/002_internal_library.sql  # source col on photos; staging_queue; library_copies
    migrations/003_library_copy_metadata.sql  # title, description, override_date, event_id on library_copies
    migrations/004_people.sql  # people table; extended faces (person_id, bbox_*, source, confidence, embedding, verified)
    photos.go                  # Photo CRUD; InsertPhoto, scanPhotoRows
    queries.go                 # PhotoFilter + ListPhotosFiltered, GetGeotaggedPhotos, GetPhotosNearby
    duplicates.go              # duplicate_paths CRUD
    events.go                  # Event + photo_events CRUD (ClearEvents, InsertEvent, GetAllEvents, GetPhotosForEvent, GetEventForPhoto)
    dedup_report.go            # GetLibraryDedupSummaries, GetCrossPathOverlap, GetSubtreeDedupEntries
    library_paths.go           # UpsertLibraryPath, GetLibraryPathByID
    scan_runs.go               # InsertScanRun, FinishScanRun, GetAllLatestScanRuns
    staging.go                 # StagingEntry CRUD; InsertStagingEntry, ListStagingEntries, UpdateStagingEntry, SetStagingState
    library.go                 # LibraryCopy CRUD; InsertLibraryCopy, GetLibraryCopyByID, UpdateLibraryCopy, DeleteLibraryPhotoByID, ListLibraryCopiesFiltered, GetLibraryTree
    people.go                  # Person + Face CRUD; ListUnidentifiedFaces, ListUnverifiedSuggestions, GetVerifiedFacesWithEmbeddings, GetUnidentifiedFacesWithEmbeddings, SetFacePersonCandidate, MergePerson, ListFacesByPerson
  scan/
    scanner.go                 # Walk + filter; isInternalLibraryPath() skips managed copy tree; OnProgress callback; isSupportedExtension accepts .jpg/.jpeg/.heic/.heif
    scanner_filter_test.go     # Tests for include/exclude filter logic (case sensitivity, exclude-beats-include, multiple patterns)
    exif.go                    # EXIF extraction (JPEG via goexif, HEIC via internal/heif shim); Flags() returns []string{} never nil
    hash.go                    # SHA-256
    thumbnail.go               # 400px long-edge JPEG; decodeImage routes HEIC through heif.Decode
  heif/
    heif.go                    # CGO shim: Decode → image.NRGBA, DecodeConfig, ExtractEXIF → raw TIFF; statically links bundled libheif+libde265
    heif_stub.go               # !cgo fallback returning ErrNotAvailable
    heif_test.go               # Unit tests (decode, EXIF prefix stripping, invalid input)
    face_detect_test.go        # Targeted test: HEIC decode → NRGBA → SCRFD face detection
  cluster/
    cluster.go                 # Rule-based event clustering (gap days + geo distance); called after every scan
  recognition/
    recognition.go             # Status struct; Init(cfg); IsAvailable(); SetClusters/GetClusters/GetClusterIDForFace; package-level singleton
    detect.go                  # SCRFD-10G session; Detect(img) []Detection; anchor decoding + NMS
    embed.go                   # ArcFace/R50 session; Embed(img, det) []float32; L2-normalises output
    preprocess.go              # preprocessBGRFloat32, cropImage, l2NormalizeInPlace, EmbeddingToBytes, BytesToEmbedding
    cluster.go                 # Pure-Go union-find clustering; Cluster(faces, threshold, minSamples) []FaceCluster
    suggest.go                 # Pure-Go suggestion pipeline; Suggest(unidentified, verified, threshold) []Suggestion
  library/
    copy.go                    # CopyPhoto: path construction (year/month/event slug, _undated/), collision resolution, os.Copy; MovePhoto + pruneEmptyDirs for re-org; BuildRelDir/ResolveFilename exported
  auth/auth.go                 # In-memory session store (token→expiry), bcrypt, cookie
  api/
    router.go                  # Handlers struct, RegisterRoutes, authMiddleware, helpers
    photos.go                  # /api/photos (q, from, to, make, model, has_gps, flag); detail includes event_id + duplicates
    browse.go                  # /api/browse/{library_id}/{path...}, /api/libraries
    scan.go                    # ScanManager, /api/scan trigger+status; triggers cluster.Run after scan; runRecognitionPostScan (suggest + cluster) after cluster
    map.go                     # /api/map (all pins), /api/map/nearby?lat&lon&radius_km
    timeline.go                # /api/timeline?zoom=decade|year|month|week|day
    events.go                  # /api/events, /api/events/{id}
    dedup.go                   # /api/dedup/report, /api/dedup/subtree?prefix=
    settings.go                # /api/settings, /api/login, /api/logout, /api/issues, /api/recognition/status
    staging.go                 # /api/staging CRUD + approve/reject transitions
    library.go                 # /api/library/copy (bulk+single), /api/library/status, /api/library/photos, /api/library/tree, PATCH+DELETE /api/library/copies/{id}
    people.go                  # /api/people CRUD, /api/people/{id}/merge, /api/library/copies/{id}/faces, /api/faces/* (CRUD + confirm/reject/cluster)
web/
  index.html                   # SPA shell; loads all JS modules
  css/app.css                  # Dark theme (CSS custom properties)
  js/
    app.js                     # History API router; bootstraps Gallery.settings + library-enabled body class
    utils.js                   # api(), formatDate, formatCoord, esc, navigate, setActiveNav, stagePhoto(); handles 204 No Content
    browse.js                  # Folder browser + photo grid (Stage button on cards)
    photo.js                   # Photo detail + EXIF table + duplicates + event link
    search.js                  # Filter form + paginated grid (Stage button on cards)
    timeline.js                # Plotly bar chart + click-to-grid
    map.js                     # Leaflet map, all pins + radius search with circle overlay
    events.js                  # Event list cards + event detail photo grid
    dedup.js                   # Per-library summary, cross-library overlap, subtree analyser
    settings.js                # Full config editor: scan dirs, filters, scan/event/log/auth/recognition settings, status panels
    staging.js                 # Staging queue: two-panel review UI with annotation form
    library.js                 # Internal library browse: sidebar tree + filter bar + photo grid; edit panel (title/description/tags/dates/event); remove-with-confirmation; people/face-tagging panel
    people.js                  # /people list, /people/{id} detail (edit + merge), /faces/review (two-panel cluster+suggestion UI)
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
- ✅ **Phase 6** — Internal library infrastructure: `InternalLibraryConfig` + `DropzoneConfig` in `Config`, `config.Validate()` at startup, DB migration `002_internal_library.sql` (`source` col, `staging_queue`, `library_copies`), copy service (`internal/library/copy.go`), staging API (`/api/staging`), library API (`/api/library/*`), frontend `staging.js` + `library.js`, Stage button on photo cards (CSS-gated by `library-enabled` body class), scanner path exclusion.
- ✅ **Phase 7** — Dropzone source (lenient scanner, auto-stage on ingest, `source='dropzone'` on photos).
- ✅ **Phase 8** — Library copy editing & re-organisation: migration `003_library_copy_metadata.sql` (`title`, `description`, `override_date`, `event_id` on `library_copies`), `PATCH /api/library/copies/{id}` with re-org trigger (`MovePhoto`, `pruneEmptyDirs`), extended `GET /api/library/photos` filters (`source`, `has_date_override`, `true_date_unknown`, `tag`, `event_id`, `q`), edit panel in `library.js`.
- ✅ **Phase 9** — Library photo removal: `DELETE /api/library/copies/{id}` with full cascade (photo_events → staging_queue → duplicate_paths → library_copies → photos) + physical file delete, confirmation UI in `library.js`.
- ✅ **Phase 10** — People tagging + face recognition:
  - **Phase A (manual tagging)**: `people` + extended `faces` schema (migration `004_people.sql`), full CRUD API (`/api/people`, `/api/library/copies/{id}/faces`, `/api/faces/*`), tagging panel in `library.js`, `/people` browse + detail pages with merge action.
  - **Phase B (auto face detection)**: `internal/recognition` package (SCRFD + ArcFace ONNX sessions via `onnxruntime_go`); scanner auto-detects faces and stores bounding boxes + 512-dim embeddings per photo; `GET /api/recognition/status` endpoint; recognition-gated 501/503 responses.
  - **Phase C (identity clustering + review UI)**: Post-scan suggestion pipeline (per-person mean embedding nearest-neighbour matching) + union-find clustering of unidentified faces; in-memory cluster store; `/api/faces/unidentified`, `/api/faces/suggestions`, `/api/faces/cluster` endpoints; `/faces/review` two-panel UI; recognition status section in Settings.
- ✅ **Phase 11** — Settings synchronization overhaul: settings page reorganised into editable sections mapped to current `Config` fields (scan paths, whitelist, filename filters, scan/event tuning, logging, auth, face recognition), with section-local save actions using partial `POST /api/settings` payloads.
- ✅ **Phase 12** — HEIC support: `internal/heif` CGO shim with statically-linked `libheif` + `libde265` (bundled `.a` files in `heif/lib/linux-x64/`). `isSupportedExtension` accepts `.heic`/`.heif`. `decodeImage` routes HEIC for thumbnails and face detection. `ReadEXIF` extracts HEIC EXIF via `heif.ExtractEXIF` → `goexif`. Migration `005_heic.sql` adds `photos.format` column (`'jpeg'`/`'heic'`). `handlePhotoImage` serves JPEG thumbnail for HEIC; `?format=jpeg` on-the-fly transcode; `?original=1` for raw download. `photo.js` auto-appends `?format=jpeg` for HEIC photos. Full test coverage: 8 heif unit tests, 5 transcode API tests, 1 face-detection integration test. HEIC files are on par with JPEGs across all subsystems (foreign keys, tags, people, events, library copy, dedup).
- ✅ **Phase 13** — Concurrency refactor + async face detection queue: See `spec_concurrency_refactor.md` and `spec_face_recognition.md`.
  - Face detection moved from SCAN to COPY (only library photos, async via `FaceQueue`).
  - Persistent ONNX sessions (single SCRFD + ArcFace shared by background worker, server lifetime).
  - DB version tracking: `photos.recognition_version`, `recognition_status`, `recognition_error`.
  - Priority queue: 0=manual, 1=copy-time, 2=background catch-up.
  - `POST /api/photos/{sha256}/detect-faces`, `GET/POST /api/recognition/queue`, `POST /api/recognition/reprocess-all`.
  - "Detect Faces" button on photo detail + library edit panel; queue status in Settings.
  - Hash-then-read optimization: `ReadFileAndHash` reads once for both hash and thumbnail.
  - `Scanner.dbMu` serialises DB writes between walk and thumbnail workers.

## Known issues / backlog (from TODO.md)

- Manual lat/lon override for photos missing GPS — store as sidecar-style DB data with an `approximate` flag and optional radius.
- Notes/description fields per photo (discovery layer; separate from library copy annotations).
- Export of data/metadata.
- Dropzone watch mode (inotify/fsnotify) — instead of manual scan trigger.

## Sample data

`samples/` contains 5 test JPEGs from 4 cameras (HTC One, Canon EOS 700D, Apple iPhone SE, Samsung Techwin L100) plus 3 HEIC samples (`image1.heic`, `image2.heic` with EXIF, and `heic_face.heic` containing a detectable face). The iPhone SE photo has real GPS coordinates. `test_config.json` whitelists all four cameras with `scan_workers: 2`.

`samples/duplicates/` contains byte-identical copies of `SDC12869.JPG` and `IMG_0361.JPG`, used to exercise the duplicate detection and dedup report. A clean scan should show: found:9, ingested:7, duplicate:2, then cluster into 5 events (the 2 HEIC samples without dates fall into `_undated`).

## Gotchas encountered

- **`exif.BodySerialNumber`** is not exported by `goexif` — use raw string `"BodySerialNumber"` as field name (gracefully returns empty).
- **Rescan false-duplicates** — `PhotoExistsByHash` alone isn't enough. Compare `GetCanonicalFilepath` against the current path; only insert into `duplicate_paths` when they differ.
- **`flags` stores `"null"`** when inserting a nil `[]string`. Fix: initialise as `[]string{}` in `exif.go` and guard in `normalizeFlags()`.
- **`captured_at` strftime failure** — Go's `time.Time.String()` produces `"2012-04-15 18:58:46 +0100 BST"` which SQLite cannot parse with `strftime`. Always format as `time.RFC3339` before inserting.
- **Filename filter case sensitivity** — regex patterns compiled as-is are case-sensitive; `^dscn` won't match `DSCN0042.JPG`. All patterns are wrapped with `(?i)` via `caseInsensitivePattern()` before compiling.
- **Cluster `photoPoint` type** must be declared at package scope, not inside `Run()`, because `centroid()` takes `[]photoPoint` as a parameter.
- **Double-pointer pattern in `LibraryCopyUpdate`** — `**string` fields: outer `nil` = skip field, outer non-nil pointing to nil `*string` = clear column to NULL. This avoids ambiguity between "field not in JSON" and "field explicitly nulled".
- **`MovePhoto` parent-dir extraction** — uses `strings.LastIndex(lc.RelativePath, "/")` rather than any helper from another package; `splitPath` is not exported from `internal/db`.
- **`ListLibraryCopiesFiltered` column aliasing** — all `SELECT` columns must be prefixed with `lc.` (e.g. `lc.id, lc.photo_sha256, …`) when JOINing the `photos` table to avoid "ambiguous column name" SQLite errors.
- **`utils.js` 204 handling** — `Gallery.utils.api()` must short-circuit before calling `res.json()` when `res.status === 204`; calling `.json()` on an empty body throws a SyntaxError.
- **Router dispatch with no capture groups** — When a route pattern has no capture groups (e.g. `/faces/review`), `m.slice(1)` is `[]`, so the handler is called as `handler(opts)` with `personID = opts` and `opts = undefined`. `people.js` normalises this: `if (personID && typeof personID === 'object') { opts = personID; personID = null; }`.
- **Face recognition is always compiled in** — `internal/recognition` uses CGO (`onnxruntime_go`) but `libonnxruntime` does NOT need to be present at compile time. The library is loaded at runtime via `SetSharedLibraryPath`. If loading fails, the server starts normally and recognition endpoints return 501/503.
- **Double-pointer pattern in `FaceUpdate`** — `**int64` / `**float64` fields: outer `nil` = skip, outer non-nil inner nil = set column to NULL. Same pattern as `LibraryCopyUpdate`.
- **`cluster_id` is ephemeral** — face clusters are stored only in memory (`recognition.SetClusters`). They are recomputed after each scan and on `POST /api/faces/cluster`. A server restart clears them; the review UI handles this gracefully (faces show `cluster_id: null`).
- **HEIC static linking** — `internal/heif` uses `#cgo LDFLAGS` to statically link the bundled `.a` files. The libraries are committed to git (`heif/lib/linux-x64/`) so no system packages are needed. If you need to upgrade libheif/libde265, run `./heif/build_libs.sh` and commit the new `.a` files. The C headers in `heif/include/` must match the `.a` versions.
- **HEIC EXIF extraction** — HEIC stores EXIF in an ISOBMFF container (`Exif` item), not a JPEG APP1 segment. `goexif` cannot read this directly. The `internal/heif` shim extracts the raw bytes via `libheif`'s metadata API, then `stripHEICExifPrefix()` removes the HEIC wrapper bytes (handling three known layouts: "Exif\0\0" box prefix, 4-byte offset prefix, and bare TIFF) before passing to `goexif.Decode`.
- **HEIC serving paths** — Three query-param modes on `/api/photos/{sha256}/image`: (1) no param → HEIC serves 400px JPEG thumbnail, JPEG serves original; (2) `?original=1` → serves raw HEIC file; (3) `?format=jpeg` → on-the-fly full-resolution HEIC→JPEG transcode via `heif.Decode` + `jpeg.Encode`. `photo.js` auto-appends `?format=jpeg` for HEIC photos so they render in all browsers.
- **Nil EXIF + empty whitelist** — When the camera whitelist is empty, photos without EXIF are allowed through (synthesised empty EXIFData). Previously they were skipped even with no whitelist configured. This matters for HEIC files that lack EXIF metadata (e.g. `heic_face.heic`).
- **`detectAndStoreFaces` SQLITE_BUSY** — Face detection runs in thumbnail worker goroutines concurrent with the walk goroutine. Face inserts retry once after 50ms on `database is locked` errors to avoid losing detections in WAL mode.
