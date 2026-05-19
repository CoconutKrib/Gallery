# Gallery

A self-hosted family photo management webapp. A single Go binary serves both the REST API and all static frontend assets. Single-user, read-only — it never writes, moves, or deletes your source photos.

## Features

| Page | Description |
|---|---|
| **Browse** | Navigate your photo library as a folder tree |
| **Search** | Filter by date range, camera make/model, GPS flag, and keyword |
| **Timeline** | Zoomable bar chart (decade → day) bucketing photos by capture date |
| **Map** | Interactive Leaflet map of all geotagged photos; radius search |
| **Settings** | Manage library paths, trigger re-scans, view scan history and issues |
| **Photo detail** | Full EXIF table, full-resolution image, duplicates list |

## Requirements

- Go 1.22 or later (no CGO required — the SQLite driver is pure Go)
- Network access to OpenStreetMap tile servers (for the map view)

## Quickstart

### 1. Clone and build

```bash
git clone https://github.com/halleck/gallery
cd gallery
go build -o gallery .
```

### 2. Create a config file

Copy the example and edit the `library_paths` to point at your photo folders:

```jsonc
{
  "library_paths": [
    { "path": "/home/you/Photos", "label": "Photos" }
  ],
  "camera_whitelist": [],        // empty = accept all cameras
  "filename_filters": {
    "include": [],               // e.g. ["*.jpg","*.JPG"]
    "exclude": []
  },
  "auth": {
    "enabled": false,            // set true + password_hash to protect with a password
    "password_hash": "",
    "session_secret": ""
  },
  "db_path": "./gallery.db",
  "cache_dir": "./cache",
  "scan_workers": 4,
  "session_ttl_hours": 24
}
```

Save it as `config.json`.

### 3. Scan your library

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
| `library_paths` | array | List of `{ "path": "...", "label": "..." }` objects pointing at photo roots |
| `camera_whitelist` | array | `{ "make": "...", "model": "..." }` entries. Empty list = accept all cameras |
| `filename_filters.include` | array | Glob patterns — only matching filenames are ingested. Empty = accept all |
| `filename_filters.exclude` | array | Glob patterns — matching filenames are skipped |
| `auth.enabled` | bool | Enable password protection |
| `auth.password_hash` | string | bcrypt hash of the password |
| `db_path` | string | Path to the SQLite database file |
| `cache_dir` | string | Directory for generated thumbnails |
| `scan_workers` | int | Parallel workers during a scan (default 4) |
| `event_gap_days` | int | Days of inactivity that separate two events (clustering) |
| `session_ttl_hours` | int | How long login sessions last |

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
| `--scan` | Scan all configured library paths then exit |

## Development

```bash
# Run against the bundled sample photos (5 JPEGs, 4 cameras)
rm -f test_gallery.db && rm -rf .test_cache
go run . --config test_config.json --scan
go run . --config test_config.json        # http://localhost:8080

# Vet
go vet ./...
```

The frontend is plain vanilla JS with no build step. Edit files under `web/` and reload the browser.
