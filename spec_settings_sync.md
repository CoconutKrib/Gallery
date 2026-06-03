# Spec: Settings API & UI synchronisation

## Problem

`GET /api/settings`, `POST /api/settings`, and the `/settings` frontend page are out of sync with the canonical `Config` struct in `internal/config/config.go`. Several fields exist in config but are not exposed through the API, and several API-exposed fields are not rendered or editable in the UI.

---

## Full gap analysis

### Fields in `Config` struct

| Config field | GET `/api/settings` | POST `/api/settings` | UI display | UI editable |
|---|---|---|---|---|
| `scan_paths` | ✅ | ✅ | ✅ scan buttons | — |
| `camera_whitelist` | ✅ | ✅ | ✅ | ✅ |
| `filename_filters` | ✅ | ✅ | ✅ | ✅ |
| `auth.enabled` | ✅ | ✅ (as `auth_enabled`) | ❌ missing | ❌ missing |
| `auth.password_hash` | intentionally omitted | ✅ (as `new_password`) | ❌ missing | ❌ missing |
| `auth.session_secret` | intentionally omitted | intentionally not writable | — | — |
| `db_path` | ✅ | intentionally read-only | ✅ read-only | — |
| `cache_dir` | ✅ | intentionally read-only | ✅ read-only | — |
| `log_file` | ❌ **missing** | ❌ **missing** | ❌ **missing** | ❌ **missing** |
| `log_level` | ❌ **missing** | ❌ **missing** | ❌ **missing** | ❌ **missing** |
| `scan_workers` | ✅ | ✅ | ✅ read-only | ❌ **should be editable** |
| `event_gap_days` | ✅ | ✅ | ✅ read-only | ❌ **should be editable** |
| `event_geo_km` | ✅ | ✅ | ✅ read-only | ❌ **should be editable** |
| `session_ttl_hours` | ✅ | ❌ **missing** | ❌ **missing** | ❌ **missing** |
| `internal_library` | ✅ | ✅ | ❌ **section entirely absent** | ❌ **missing** |
| `dropzone` | ✅ | ✅ | ✅ read-only status | — |
| `face_recognition` | ❌ **missing** | ❌ **missing** | ❌ **missing** (only runtime status shown) | ❌ **missing** |

---

## Desired end state

### `GET /api/settings` — complete response shape

Return **all** non-sensitive config fields. Sensitive fields (`password_hash`, `session_secret`) are always omitted.

```json
{
  "scan_paths": [...],
  "camera_whitelist": [...],
  "filename_filters": { "include": [...], "exclude": [...] },
  "auth": { "enabled": false },
  "db_path": "./gallery.db",
  "cache_dir": "./.cache",
  "log_file": "",
  "log_level": "info",
  "scan_workers": 4,
  "event_gap_days": 2,
  "event_geo_km": 500,
  "session_ttl_hours": 24,
  "internal_library": { "path": "", "enabled": false },
  "dropzone": { "path": "", "enabled": false },
  "face_recognition": {
    "enabled": false,
    "onnxruntime_lib": "",
    "model_dir": "",
    "detection_model": "",
    "recognition_model": "",
    "detection_threshold": 0.5,
    "recognition_threshold": 0.4,
    "cluster_min_samples": 2
  }
}
```

### `POST /api/settings` — `settingsUpdateRequest` additions

Add the missing writable fields to `settingsUpdateRequest`:

```go
SessionTTLHours *int                          `json:"session_ttl_hours"`
LogFile         *string                       `json:"log_file"`
LogLevel        *string                       `json:"log_level"`
FaceRecognition *config.FaceRecognitionConfig `json:"face_recognition"`
```

Handling rules:
- `session_ttl_hours`: apply if `> 0`
- `log_file`: apply as-is (empty string is valid — disables file logging)
- `log_level`: validate against allowed values (`debug`, `info`, `warn`, `error`) before applying; return `400` on invalid value
- `face_recognition`: apply the whole block if non-nil (same pattern as `internal_library` and `dropzone`)

`db_path` and `cache_dir` remain intentionally read-only (changing them at runtime would invalidate open DB handles and cached files).

---

## UI — settings sections

The settings page should be reorganised into the sections below. Fields noted as **editable** get an inline input and save button. Fields noted as **display** are shown read-only.

### 1. Libraries
*(no change — existing implementation is correct)*

### 2. Internal Library ← new section
Display `internal_library.enabled` badge and `internal_library.path`.  
Both fields are **editable** via POST `internal_library` (same as dropzone section pattern).

### 3. Dropzone
*(no change — existing implementation is correct)*

### 4. Camera Whitelist
*(no change)*

### 5. Filename Filters
*(no change)*

### 6. Scan Settings ← promoted from read-only "Configuration"
The following fields are editable inline; a single "Save" button POSTs only the changed fields:

| Field | Input type | Validation |
|---|---|---|
| `scan_workers` | number | ≥ 1 |
| `event_gap_days` | number | ≥ 1 |
| `event_geo_km` | number | > 0 |
| `session_ttl_hours` | number | ≥ 1 |

### 7. Logging ← new section
Editable inline with a single "Save" button:

| Field | Input type | Notes |
|---|---|---|
| `log_level` | select (`debug` / `info` / `warn` / `error`) | |
| `log_file` | text | empty = stderr only |

> **Note:** Changing `log_level` or `log_file` writes the new config to disk. The running logger is **not** hot-reloaded; the new values take effect on next server start. Display a notice in the UI: *"Restart the server for log changes to take effect."*

### 8. Auth ← new section
Display `auth.enabled` badge.  
Provide a toggle to enable/disable auth (POST `auth_enabled: true|false`).  
Provide a "Change password" form (POST `new_password: "..."`) — only shown when `auth.enabled` is true.  
The form uses `type="password"` inputs; never echoes the current hash.

### 9. Face Recognition
Keep the existing runtime status panel (`/api/recognition/status`).  
Add a **Configuration** sub-section beneath it that displays all `face_recognition` fields from `/api/settings`, with editable inputs for:

| Field | Input type | Notes |
|---|---|---|
| `face_recognition.enabled` | toggle | |
| `face_recognition.onnxruntime_lib` | text (file path) | |
| `face_recognition.model_dir` | text (dir path) | |
| `face_recognition.detection_model` | text (filename) | |
| `face_recognition.recognition_model` | text (filename) | |
| `face_recognition.detection_threshold` | number (0.0–1.0) | default 0.5 |
| `face_recognition.recognition_threshold` | number (0.0–1.0) | default 0.4 |
| `face_recognition.cluster_min_samples` | number | ≥ 1, default 2 |

A single "Save" button POSTs the whole `face_recognition` block.  
Display a notice: *"Restart the server for recognition changes to take effect."*

### 10. System ← renamed from "Configuration" (read-only fields)
Display only fields that are intentionally not editable at runtime:

| Field | Display |
|---|---|
| `db_path` | text |
| `cache_dir` | text |

### 11. Recent Scans
*(no change)*

### 12. Ingest Issues
*(no change)*

---

## Implementation approach

### Rule: API response is the single source of truth
The UI **always** derives its display from the `/api/settings` response. It never hard-codes default values. Every field in `GET /api/settings` must be rendered somewhere in the UI.

### Rule: POST sends only changed fields
Each editable section saves its own slice of config via a targeted POST containing only the fields it owns. This matches the existing pattern (whitelist, filters) and avoids inadvertent overwrites.

### Rule: Config struct drives API shape
When a new field is added to `Config`, the developer checklist is:
1. Add it to `handleGetSettings` response.
2. Add it to `settingsUpdateRequest` (if writable at runtime).
3. Add its handling to `handlePostSettings`.
4. Add it to the appropriate UI section (display + editable if applicable).

### Restart-required fields
Some fields (`log_file`, `log_level`, `face_recognition.*`) are written to disk but require a server restart to take effect. The UI should display a static notice in those sections. No special server-side mechanism is needed.

---

## Files to change

| File | Change |
|---|---|
| `internal/api/settings.go` | Add `log_file`, `log_level`, `face_recognition` to GET response; add 4 missing fields to `settingsUpdateRequest` and their handling in `handlePostSettings` |
| `web/js/settings.js` | Add Internal Library, Logging, Auth, and Face Recognition config sections; make scan/event/session fields editable |
