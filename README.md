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
- `model_dir`: directory containing ONNX model files
- `detection_model`: detector filename (default: `det_10g.onnx`, SCRFD‑10G)
- `recognition_model`: embedder filename (default: `w600k_r50.onnx`, ArcFace ResNet‑50)
- `detection_threshold`: face detector score threshold (default: 0.5)
- `recognition_threshold`: embedding match threshold (default: 0.8)
- `cluster_min_samples`: minimum cluster size for review grouping (default: 5)

The default models are from the [InsightFace Buffalo_L](https://github.com/deepinsight/insightface) package. When `detection_model` or `recognition_model` is left empty, the binary looks for `det_10g.onnx` and `w600k_r50.onnx` respectively inside `model_dir`. Only the detection model is required; if the recognition model is missing, the server runs in detection‑only mode (faces are found but not embedded or clustered).

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

## Packaging for deployment

`package.sh` builds the binary and bundles it with its runtime dependencies into a `.tar.gz` ready for deployment on another Linux x64 machine.

```bash
./package.sh          # CPU-only build (default, smaller)
./package.sh --gpu    # GPU build with CUDA/TensorRT providers
```

**What's included:**

| Component | Source |
|---|---|
| `gallery` binary | Built from source with `-ldflags="-s -w"` |
| `lib/libonnxruntime.so*` | ONNX runtime shared library (face recognition) |
| `models/det_10g.onnx` | SCRFD face detection model (17 MB) |
| `models/w600k_r50.onnx` | ArcFace face embedding model (167 MB) |
| `config.json.example` | Template config with relative paths |

Web assets and HEIC decode libraries are statically linked into the binary — no extra files needed for those.

**Prerequisites for face recognition:** Before running `package.sh`, download the ONNX runtime into the `onnx/` directory. The script auto-detects whichever variant is present:

```bash
# CPU build (~15 MB library, smaller package):
wget https://github.com/microsoft/onnxruntime/releases/download/v1.26.0/onnxruntime-linux-x64-1.26.0.tgz
tar -xzf onnxruntime-linux-x64-1.26.0.tgz -C onnx/

# GPU build (~440 MB library, CUDA 12 + TensorRT):
wget https://github.com/microsoft/onnxruntime/releases/download/v1.26.0/onnxruntime-linux-x64-gpu-1.26.0.tgz
tar -xzf onnxruntime-linux-x64-gpu-1.26.0.tgz -C onnx/
```

The InsightFace models in `insightface/buffalo_l/` are also required. Only `det_10g.onnx` and `w600k_r50.onnx` are packaged (the other three buffalo_l models are unused by gallery).

**Approximate package sizes:**

| Variant | Tarball |
|---|---|
| CPU (no GPU providers) | ~180 MB |
| GPU (+ CUDA/TensorRT) | ~360 MB |

**Deploying the package:**

```bash
tar -xzf gallery-<version>.tar.gz
cd gallery-<version>
cp config.json.example config.json   # edit scan_paths etc.
mkdir -p .cache
./gallery --config config.json
```

The example config uses relative paths (`./lib/libonnxruntime.so`, `./models`) so everything works as long as you run `gallery` from the package directory.
