# Gallery

A self-hosted family photo management webapp. A single Go binary serves both the REST API and all static frontend assets. It is single-user and read-only for source libraries: it never writes, moves, or deletes source photos. Discovered photos can be copied into an optional internal library for curation, annotation, people tagging, and face-recognition-assisted review.

## Features

| Page | Description |
|---|---|
| **Browse** | Navigate your photo library as a folder tree |
| **Search** | Filter by date range, camera make/model, GPS flag, and keyword |
| **Timeline** | Zoomable bar chart (decade → day) bucketing photos by capture date |
| **Map** | Interactive Leaflet map of all geotagged photos; radius search |
| **Settings** | Manage library paths, trigger re-scans, view scan history and issues |
| **Photo detail** | Full EXIF table, full-resolution image, duplicates list |
| **Staging** | Queue photos for internal-library copy with approve/reject workflow |
| **Library** | Browse curated internal library, edit metadata, and reorganize paths |
| **People** | Create/merge people, review tagged faces, browse a person’s photos |
| **Face review** | Cluster unidentified faces and apply/reject recognition suggestions |

## Requirements

- Go 1.25 or later
- CGO-capable build environment (face recognition uses `onnxruntime_go`)
- Network access to OpenStreetMap tile servers (for the map view)
- Optional runtime dependency for face recognition: `libonnxruntime` plus ONNX models

## Quickstart

### 1. Clone and build

```bash
git clone https://github.com/halleck/gallery
cd gallery
go build -o gallery .
```

### 2. Create a config file

Copy the example and edit the `scan_paths` to point at your photo folders:

```jsonc
{
  "scan_paths": [
    { "path": "/home/you/Photos", "label": "Photos" }
  ],
  "camera_whitelist": [],        // empty = accept all cameras
  "filename_filters": {
    "include": [],               // e.g. ["^IMG_","\\.jpg$"]
    "exclude": []
  },
  "auth": {
    "enabled": false,            // set true + password_hash to protect with a password
    "password_hash": "",
    "session_secret": ""
  },
  "db_path": "./gallery.db",
  "cache_dir": "./cache",
  "log_file": "",
  "log_level": "info",
  "scan_workers": 4,
  "event_gap_days": 2,
  "event_geo_km": 500,
  "session_ttl_hours": 24
}
```

Save it as `config.json`.

### 3. Scan your scan paths

```bash
./gallery --config config.json --scan
```

Output shows files found, ingested, skipped (already known), and any errors. Scanning is idempotent — re-running is safe.

### 4. Start the server

```bash
./gallery --config config.json
```

Open [http://localhost:8080](http://localhost:8080) in your browser.

## Configuration reference

| Key | Type | Description |
|---|---|---|
| `scan_paths` | array | List of `{ "path": "...", "label": "..." }` source roots to scan |
| `library_paths` | array | Legacy alias for `scan_paths` accepted for backward compatibility |
| `camera_whitelist` | array | `{ "make": "...", "model": "..." }` entries. Empty list = accept all cameras |
| `filename_filters.include` | array | Case-insensitive regex patterns; if set, files must match at least one |
| `filename_filters.exclude` | array | Case-insensitive regex patterns; exclusion wins over inclusion |
| `auth.enabled` | bool | Enable password protection |
| `auth.password_hash` | string | bcrypt hash of the password |
| `db_path` | string | Path to the SQLite database file |
| `cache_dir` | string | Directory for generated thumbnails |
| `log_file` | string | Optional log output file path (empty means stderr only) |
| `log_level` | string | `debug`, `info`, `warn`, or `error` |
| `scan_workers` | int | Parallel workers during a scan (default 4) |
| `event_gap_days` | int | Days of inactivity that separate two events (clustering) |
| `event_geo_km` | float | Geographic distance threshold for event clustering |
| `session_ttl_hours` | int | How long login sessions last |
| `internal_library` | object | `{ enabled, path }` for the managed copy destination |
| `dropzone` | object | `{ enabled, path }` for lenient ingest + auto-stage flow |
| `face_recognition` | object | ONNX runtime/model paths and thresholds for detection/matching/clustering |

### Face recognition config fields

When `face_recognition.enabled` is true, configure:

- `onnxruntime_lib`: path to `libonnxruntime`
- `model_dir`: directory containing models
- `detection_model`: detector filename (for example `det_10g.onnx`)
- `recognition_model`: embedder filename (for example `w600k_r50.onnx`)
- `detection_threshold`: face detector score threshold
- `recognition_threshold`: embedding match threshold
- `cluster_min_samples`: minimum cluster size for review grouping

### Enabling password protection

```bash
# Generate a bcrypt hash
htpasswd -bnBC 12 "" yourpassword | tr -d ':\n'
```

Set `auth.enabled: true` and paste the output into `auth.password_hash`. Also set `auth.session_secret` to a random hex string.

## Command-line flags

| Flag | Description |
|---|---|
| `--config <path>` | Path to the JSON config file (required) |
| `--scan` | Scan all configured scan paths then exit |

## API highlights

Most routes are under `/api/*` and require auth only when `auth.enabled = true`.

- Library and discovery:
  - `GET /api/libraries`
  - `GET /api/browse/{library_id}/{path...}`
  - `GET /api/photos`, `GET /api/photos/{sha256}`
- Scan and settings:
  - `POST /api/scan`, `GET /api/scan/status`
  - `GET /api/settings`, `POST /api/settings`
  - `GET /api/issues`
- Timeline, map, events, dedup:
  - `GET /api/timeline`
  - `GET /api/map`, `GET /api/map/nearby`
  - `GET /api/events`, `GET /api/events/{id}`
  - `GET /api/dedup/report`, `GET /api/dedup/subtree`
- Internal library workflow:
  - `GET/POST/PATCH/DELETE /api/staging...`
  - `POST /api/library/copy`, `POST /api/library/copy/{staging_id}`
  - `GET /api/library/photos`, `GET /api/library/tree`, `GET /api/library/status`
  - `PATCH /api/library/copies/{id}`, `DELETE /api/library/copies/{id}`
- People and recognition:
  - `GET/POST/PATCH/DELETE /api/people...`, `POST /api/people/{id}/merge`
  - `GET/POST /api/library/copies/{id}/faces`, `PATCH/DELETE /api/faces/{id}`
  - `GET /api/recognition/status`
  - `GET /api/faces/unidentified`, `GET /api/faces/suggestions`
  - `POST /api/faces/{id}/confirm`, `POST /api/faces/{id}/reject`, `POST /api/faces/cluster`

## Development

```bash
# Run against the bundled sample photos (5 JPEGs, 4 cameras)
rm -f test_gallery.db && rm -rf .test_cache
go run . --config test_config.json --scan
go run . --config test_config.json        # http://localhost:8080

# Vet
go vet ./...

# Tests
go test ./...
```

The frontend is plain vanilla JS with no build step. Edit files under `web/` and reload the browser.
