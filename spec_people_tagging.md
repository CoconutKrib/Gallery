# Spec: People Tagging

## Goals

Allow users to tag which people appear in photos in the internal library. In the
interim, tagging is entirely manual — presence-only or with an optional drawn
bounding box. In a later phase, automated face detection and recognition can be
enabled at runtime: a single binary runs on any machine, and the heavier AI
features are activated only when the onnxruntime shared library and model files
are present. No separate "AI build" is required.

**Non-goals:**
- Facial recognition as a hard dependency (the app must work fully without it)
- Tagging people in scan-library photos (people are library-scoped — only `library_copies`)
- Cloud-based facial recognition services
- Shipping or downloading model weights (operator responsibility)

---

## Deployment Phases

| Phase | What ships | Runtime requirement |
|---|---|---|
| **A — Manual tagging** | `people` table, extended `faces`, full CRUD API, tagging UI on library photo detail, `/people` browse page | None — no extra libraries or models needed |
| **B — Face detection** | Auto-populate `faces` rows with bounding boxes during scan; user confirms/removes and assigns people | `libonnxruntime.so` + SCRFD detection model |
| **C — Face recognition** | Embedding extraction, identity clustering, suggestion pipeline, review UI | `libonnxruntime.so` + SCRFD detection model + ArcFace embedding model |

**Single binary:** there is only one build target — `go build ./...`. The
`internal/recognition` package is always compiled in (it requires CGO, so a C
compiler must be present on the build machine, but `libonnxruntime` does **not**
need to be installed at compile time). At runtime, `recognition.Init(cfg)` attempts
to dynamically load the shared library via `SetSharedLibraryPath`. If this fails
(library not found, models missing, etc.) the server starts normally and face
recognition features are marked unavailable. Manual tagging (Phase A) is always
fully functional regardless.

---

## Schema — Migration `004_people.sql`

### New table: `people`

The people registry is implicitly scoped to the internal library. There is only
one internal library (a config value, not a DB row), so no FK is needed.

```sql
CREATE TABLE IF NOT EXISTS people (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    name          TEXT    NOT NULL DEFAULT '',
    notes         TEXT,
    created_at    TEXT    NOT NULL DEFAULT (datetime('now'))
);
```

`cover_face_id` is added in the same migration after `faces` is extended (see
below), avoiding a forward-reference problem:

```sql
ALTER TABLE people ADD COLUMN cover_face_id INTEGER REFERENCES faces(id);
```

### Extend existing `faces` table

`faces` was created as an empty placeholder in `001_initial.sql` with only `id`
and `photo_id`. All new columns are added via `ALTER TABLE`:

```sql
-- Identity assignment
ALTER TABLE faces ADD COLUMN person_id   INTEGER REFERENCES people(id);

-- Optional bounding box (normalised 0.0–1.0 relative to image dimensions)
-- All four are NULL for a presence-only manual tag
ALTER TABLE faces ADD COLUMN bbox_x      REAL;
ALTER TABLE faces ADD COLUMN bbox_y      REAL;
ALTER TABLE faces ADD COLUMN bbox_w      REAL;
ALTER TABLE faces ADD COLUMN bbox_h      REAL;

-- Provenance
-- 'manual' = entered by the user
-- 'auto'   = produced by the face detection pipeline
ALTER TABLE faces ADD COLUMN source      TEXT NOT NULL DEFAULT 'manual';

-- Detection confidence score; NULL for manual tags
ALTER TABLE faces ADD COLUMN confidence  REAL;

-- 512-dim ArcFace embedding stored as raw little-endian float32 bytes (~2 KB).
-- NULL until Phase C recognition pipeline runs.
ALTER TABLE faces ADD COLUMN embedding   BLOB;

-- 1 = user confirmed (all manual tags; auto tags after user accepts suggestion)
-- 0 = auto-detected but not yet confirmed
ALTER TABLE faces ADD COLUMN verified    INTEGER NOT NULL DEFAULT 0;

ALTER TABLE faces ADD COLUMN created_at  TEXT    NOT NULL DEFAULT (datetime('now'));
```

Indexes:

```sql
CREATE INDEX IF NOT EXISTS idx_faces_photo_id  ON faces(photo_id);
CREATE INDEX IF NOT EXISTS idx_faces_person_id ON faces(person_id);
CREATE INDEX IF NOT EXISTS idx_faces_source    ON faces(source);
CREATE INDEX IF NOT EXISTS idx_faces_verified  ON faces(verified);
```

### Face row lifecycle

| State | `source` | `person_id` | `bbox_*` | `embedding` | `verified` |
|---|---|---|---|---|---|
| Manual presence tag | `manual` | set | NULL | NULL | 1 |
| Manual located tag | `manual` | set | set | NULL | 1 |
| Auto-detected, unidentified | `auto` | NULL | set | set | 0 |
| Auto-detected, suggested match | `auto` | set (candidate) | set | set | 0 |
| Confirmed (auto or manual) | either | set | set or NULL | set or NULL | 1 |

### Bounding box convention

`bbox_x` and `bbox_y` are the top-left corner of the face region, `bbox_w` and
`bbox_h` are the width and height. All four values are normalised to the range
0.0–1.0 relative to the full image dimensions, matching the convention used by
most ONNX face detection models (SCRFD, RetinaFace).

### Embedding storage

512 × 4 bytes = 2 KB per face. A 10,000-face library stores ~20 MB of embeddings
in SQLite — well within range for a single-user family gallery.

### Circular FK note

`people.cover_face_id → faces(id)` and `faces.person_id → people(id)` form a
cycle. SQLite handles this without issue as long as at least one side is created
before the other. The migration creates `people` first (without `cover_face_id`),
extends `faces`, then adds `cover_face_id` to `people`. Cascade behaviour:

- `DELETE people` row → set `faces.person_id = NULL` (handled in application
  layer, not as a DB cascade, to avoid FK violation with `cover_face_id`)
- `DELETE faces` row → set `people.cover_face_id = NULL` (application layer)

---

## Config

Add to `config.json` and `internal/config/config.go`:

```json
"face_recognition": {
  "enabled": false,
  "onnxruntime_lib": "/usr/lib/libonnxruntime.so",
  "model_dir": "",
  "detection_model": "scrfd_10g_bnkps.onnx",
  "recognition_model": "w600k_r50.onnx",
  "detection_threshold": 0.5,
  "recognition_threshold": 0.35,
  "cluster_min_samples": 3
}
```

```go
type FaceRecognitionConfig struct {
    Enabled              bool    `json:"enabled"`
    OnnxruntimeLib       string  `json:"onnxruntime_lib"`       // path to libonnxruntime.so/.dylib/.dll
    ModelDir             string  `json:"model_dir"`
    DetectionModel       string  `json:"detection_model"`
    RecognitionModel     string  `json:"recognition_model"`
    DetectionThreshold   float64 `json:"detection_threshold"`
    RecognitionThreshold float64 `json:"recognition_threshold"`
    ClusterMinSamples    int     `json:"cluster_min_samples"`
}
```

`config.Validate()` does **not** check for model files — validation of the
recognition runtime is deferred to `recognition.Init()` at startup (so the server
can still start even if the library is absent). The only hard check at validate
time is that `recognition_threshold` is in 0.0–1.0 when `enabled = true`.

When `enabled = false` the entire `face_recognition` block is skipped; `Init()`
is never called and no CGO is invoked at startup.

---

## API

All people/face endpoints are behind the existing auth middleware. They return
`409 Conflict` if `internal_library.enabled = false`, consistent with
`/api/staging` and `/api/library/*`.

### People CRUD

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/people` | List all people. Response includes `id`, `name`, `notes`, `cover_face_id`, `photo_count`. |
| `POST` | `/api/people` | Create a person. Body: `{"name": "...", "notes": "..."}`. Returns 201 with new record. |
| `GET` | `/api/people/{id}` | Person detail. Includes first page of tagged photos. |
| `PATCH` | `/api/people/{id}` | Update `name`, `notes`, `cover_face_id`. Uses double-pointer pattern for nullable fields (same as `LibraryCopyUpdate`). |
| `DELETE` | `/api/people/{id}` | Delete person. Sets `faces.person_id = NULL` for all linked faces; sets `people.cover_face_id = NULL` first to break the cycle. Does not delete face rows. |
| `GET` | `/api/people/{id}/photos` | Paginated library photo grid for this person. Supports `page`, `per_page`. |

### Face tagging

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/library/copies/{id}/faces` | List all face tags on a library copy. Returns bbox, person info, `source`, `verified`. |
| `POST` | `/api/library/copies/{id}/faces` | Add a manual face tag. Body: `{"person_id": N, "bbox_x"?: f, "bbox_y"?: f, "bbox_w"?: f, "bbox_h"?: f}`. Sets `source='manual'`, `verified=1`. Returns 201. |
| `DELETE` | `/api/faces/{id}` | Remove a face tag. 404 if not found. |
| `PATCH` | `/api/faces/{id}` | Update `person_id`, bbox coords, or `verified`. Used to confirm or reassign auto-detected faces. |

### Recognition status

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/recognition/status` | Returns current capability state. Always succeeds (200). See response shape below. |

Response:

```json
{
  "enabled": true,
  "available": false,
  "execution_provider": null,
  "reason": "libonnxruntime not found at /usr/lib/libonnxruntime.so"
}
```

| Field | Type | Meaning |
|---|---|---|
| `enabled` | bool | `face_recognition.enabled` from config — user intent |
| `available` | bool | runtime init succeeded; library + models loaded successfully |
| `execution_provider` | string\|null | `"CUDA"`, `"CPU"`, or `null` when unavailable |
| `reason` | string\|null | Human-readable explanation when `available = false`, null otherwise |

The frontend reads this endpoint at startup and uses it to drive the three UI states
described in the Frontend section below.

### Recognition (Phase B/C, gated on `enabled AND available`)

All endpoints below return:
- `501 Not Implemented` when `enabled = false` (user has not turned it on)
- `503 Service Unavailable` with `{"reason": "..."}` when `enabled = true` but `available = false` (library/models missing)

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/faces/unidentified` | Paginated list of `source='auto'` faces with `person_id IS NULL`. Returns face crops described by photo SHA256 + bbox. |
| `GET` | `/api/faces/suggestions` | List of auto-suggested face→person pairs (`verified=0`, `person_id` set). |
| `POST` | `/api/faces/{id}/confirm` | Set `verified=1` on the current `person_id` assignment. |
| `POST` | `/api/faces/{id}/reject` | Clear `person_id`, reset to unidentified (`person_id=NULL`, `verified=0`). |
| `POST` | `/api/faces/cluster` | Trigger a manual re-cluster of all unidentified face embeddings. Returns `{"status": "ok", "clusters": N}`. |

---

## Go Package Structure

### `internal/db/people.go` (new)

```go
type Person struct {
    ID          int64
    Name        string
    Notes       *string
    CoverFaceID *int64
    PhotoCount  int
    CreatedAt   time.Time
}

type PersonUpdate struct {
    Name        *string
    Notes       **string   // double-pointer: outer nil = skip; outer non-nil inner nil = clear
    CoverFaceID **int64
}

type Face struct {
    ID         int64
    PhotoID    int64
    PersonID   *int64
    BboxX      *float64
    BboxY      *float64
    BboxW      *float64
    BboxH      *float64
    Source     string     // 'manual' | 'auto'
    Confidence *float64
    Embedding  []byte
    Verified   bool
    CreatedAt  time.Time
}

type FaceUpdate struct {
    PersonID **int64
    BboxX    **float64
    BboxY    **float64
    BboxW    **float64
    BboxH    **float64
    Verified *bool
}
```

Exported functions:
- `InsertPerson(db, name, notes string) (int64, error)`
- `GetPersonByID(db, id int64) (*Person, error)`
- `ListPeople(db) ([]Person, error)` — LEFT JOIN faces for `photo_count`
- `UpdatePerson(db, id int64, u PersonUpdate) error`
- `DeletePerson(db, id int64) error` — NULLs cover + face assignments
- `InsertFace(db, f Face) (int64, error)`
- `ListFacesForLibraryCopy(db, libraryID int64) ([]Face, error)`
- `GetFaceByID(db, id int64) (*Face, error)`
- `DeleteFace(db, id int64) error`
- `UpdateFace(db, id int64, u FaceUpdate) error`
- `ListUnidentifiedFaces(db, page, perPage int) ([]Face, error)`
- `ListUnverifiedSuggestions(db) ([]Face, error)`
- `ListFacesByPerson(db, personID int64, page, perPage int) ([]Face, error)`

### `internal/recognition/` (new)

No build tags. The package is always compiled in. It uses CGO via
`github.com/yalue/onnxruntime_go`, which dynamically loads `libonnxruntime` at
runtime rather than linking it at compile time — so the build machine only needs
a C compiler (standard on Linux/macOS), not the onnxruntime library itself.

| File | Purpose |
|---|---|
| `recognition.go` | `Status` struct; `Init(cfg) Status`; `IsAvailable() bool`; package-level singleton |
| `detect.go` | Load SCRFD ONNX session; `Detect(img) ([]Detection, error)` |
| `embed.go` | Load ArcFace/R50 ONNX session; `Embed(img, Detection) ([]float32, error)` |
| `cluster.go` | Pure Go — cosine similarity matrix + agglomerative clustering; no CGO; `Cluster(faces []FaceEmbedding) []FaceCluster` |
| `suggest.go` | Pure Go — `Suggest(unidentified, personMeans []FaceEmbedding, threshold float64) []Suggestion` |

`Init(cfg FaceRecognitionConfig) Status` is called once at server startup:

```go
type Status struct {
    Available         bool
    ExecutionProvider string  // "CUDA", "CPU", or ""
    Reason            string  // non-empty when Available == false
}
```

Init sequence:
1. Call `onnxruntime.SetSharedLibraryPath(cfg.OnnxruntimeLib)` + `onnxruntime.InitializeEnvironment()` → if either fails, return `Status{Available: false, Reason: err.Error()}`
2. Try to create a CUDA session options object → if it succeeds, set `ExecutionProvider = "CUDA"`; otherwise fall back to CPU and set `ExecutionProvider = "CPU"` with a log warning
3. Load SCRFD model from `cfg.ModelDir/cfg.DetectionModel` → if missing or invalid, return unavailable with reason
4. If recognition model path is configured, load ArcFace model → if missing, detection-only mode (Phase B); log at Info level
5. Return `Status{Available: true, ExecutionProvider: ...}`

The returned `Status` is stored in the `Handlers` struct and served by
`GET /api/recognition/status`. Callers in `scanner.go` and `scan.go` check
`recognition.IsAvailable()` before invoking any inference.

### `internal/api/people.go` (new)

Handlers registered in `router.go` under the `internal_library.enabled` guard:

```
GET  /api/people                         handleListPeople
POST /api/people                         handleCreatePerson
GET  /api/people/{id}                    handleGetPerson
PATCH /api/people/{id}                   handleUpdatePerson
DELETE /api/people/{id}                  handleDeletePerson
GET  /api/people/{id}/photos             handlePersonPhotos

GET  /api/library/copies/{id}/faces      handleListFaces
POST /api/library/copies/{id}/faces      handleAddFace
DELETE /api/faces/{id}                   handleDeleteFace
PATCH /api/faces/{id}                    handleUpdateFace

GET  /api/faces/unidentified             handleUnidentifiedFaces
GET  /api/faces/suggestions              handleSuggestions
POST /api/faces/{id}/confirm             handleConfirmFace
POST /api/faces/{id}/reject              handleRejectFace
POST /api/faces/cluster                  handleTriggerCluster
```

Recognition handlers (`handleUnidentifiedFaces` onwards) return `501` when
`Enabled == false` and `503` when `Enabled == true` but `Available == false`.
`handleRecognitionStatus` always returns 200.

### Changes to existing files

| File | Change |
|---|---|
| `internal/config/config.go` | Add `FaceRecognitionConfig` field (incl. `OnnxruntimeLib`); lighten `Validate()` |
| `internal/api/router.go` | Register new routes from `people.go`; pass recognition `Status` into `Handlers` |
| `internal/api/settings.go` | Add `handleRecognitionStatus` |
| `internal/scan/scanner.go` | After thumbnail: if `recognition.IsAvailable()`, call `Detect` + `Embed`, persist via `db.InsertFace` |
| `internal/api/scan.go` | After `cluster.Run`: call suggest + cluster when `IsAvailable()` |
| `main.go` | Call `recognition.Init(cfg)` after config load; pass `Status` into `api.NewHandlers(...)` |
| `web/js/app.js` | Register `/people` and `/people/:id` routes; fetch recognition status at boot |
| `web/js/library.js` | Add "People" section to the library copy edit panel |
| `web/index.html` | Nav link for People (CSS-gated by `library-enabled`) |

---

## Recognition Pipeline (Phase C)

### During scan

After thumbnail generation, if `FaceRecognition.Enabled = true`:

1. Decode JPEG → call `recognition.Detect(img, threshold)` → returns `[]Detection`
2. For each detection: call `recognition.Embed(img, detection)` → returns `[]float32`
3. Persist each detection as a `faces` row:
   - `source = 'auto'`, `verified = 0`, `person_id = NULL`
   - `bbox_*` set from detection coordinates (normalise to 0–1)
   - `embedding` = serialised float32 slice (little-endian bytes)
   - `confidence` = detection score

Idempotency: skip if a `faces` row with `source='auto'` already exists for this
`photo_id` (same rescan logic as thumbnails).

### After scan (in `api/scan.go`)

Runs after `cluster.Run` (event clustering), same as today's post-scan hook:

**Step 1 — Suggest identities:**
- Load all verified faces with embeddings, grouped by `person_id`
- Compute per-person mean embedding vector
- For each unidentified face (`person_id IS NULL`): compute cosine distance to each
  person mean; if best match < `recognition_threshold`, set `person_id` (candidate),
  leave `verified = 0`

**Step 2 — Cluster unidentified:**
- Fetch all `person_id IS NULL, embedding IS NOT NULL` faces
- Build pairwise cosine similarity matrix
- Run agglomerative clustering with `cluster_min_samples` minimum cluster size
- Store cluster results in-memory; expose via `GET /api/faces/unidentified`
  (grouped by inferred cluster, so the review UI can present them together)

Clustering results are ephemeral (not stored in DB). They are recomputed on each
scan completion and on explicit `POST /api/faces/cluster`.

### Cosine similarity

```
sim(a, b) = dot(a, b) / (||a|| * ||b||)
distance  = 1 - sim(a, b)
```

Two faces are considered the same person when `distance < recognition_threshold`
(default 0.35). This threshold is conservative; users can tune it in config.

### Model recommendations

| Model | Purpose | Format | Size | License |
|---|---|---|---|---|
| `SCRFD_10G_KPS` | Face detection | ONNX | ~30 MB | Apache 2.0 |
| `w600k_r50` (buffalo_l pack) | Face embeddings | ONNX | ~170 MB | **Non-commercial research only** |

**Model license note:** The `w600k_r50.onnx` recognition model from InsightFace's
buffalo_l pack is available for non-commercial research use only. The gallery code
is MIT-licensed and does not bundle any models. Operators must download models
separately and accept the InsightFace model licence. For commercial deployments, a
permissively-licensed embedding model (e.g. a self-trained ArcFace checkpoint or
the AdaFace MIT-licensed models) should be substituted.

---

## Recognition Library Evaluation

| Library | Language | CGO | License | Capabilities | Notes |
|---|---|---|---|---|---|
| **onnxruntime-go** | Go | Yes | MIT | Detection + embeddings via any ONNX model | **Recommended.** Requires onnxruntime C shared library. Build-tag gated. |
| **pigo** | Go | No | MIT | Detection only (bounding boxes + landmarks) | Suitable for Phase B detection-only. No embeddings, so cannot do recognition/clustering. |
| **go-face** | Go | Yes (dlib) | MIT | Detection + 128-dim dlib embeddings | Requires dlib installed. Accuracy lower than ArcFace. |
| **InsightFace python-package** | Python | — | MIT (code) / non-commercial (models) | Full pipeline | Python runtime required; model licence restriction. |
| **CompreFace** | Docker sidecar | — | Apache 2.0 | REST API, full pipeline | Adds ops complexity (Docker required); no CGO. |

**Decision:** `github.com/yalue/onnxruntime_go`. This library loads
`libonnxruntime` at runtime via `SetSharedLibraryPath` — the shared library is
not required at compile time, only at runtime. A single binary covers all
deployment scenarios. The build machine requires a C compiler (CGO) but not the
nxruntime library or headers. GPU acceleration is selected automatically at
runtime via the CUDA execution provider if available, falling back to CPU with a
warning.

---

## Frontend UI

### New page: `/people`

- Grid of person cards: avatar (face crop from `cover_face_id`, or a placeholder
  silhouette), name, photo count
- "Add person" button → inline modal with a name field
- Each card links to `/people/{id}`
- Nav link gated by `library-enabled` body class (same as Stage buttons)

### New page: `/people/{id}`

- Person header: avatar, editable name, notes textarea, "Set cover photo" link
- Paginated photo grid (same component as library grid)
- "Merge into…" action: typeahead pick of another person; all faces of this person
  are reassigned to the target, then this person record is deleted

### Tagging panel (in `library.js` edit panel)

- New collapsible section: **People in this photo**
- Lists current face tags (name badge; if bbox is set, clicking the badge
  highlights the region as an overlay on the thumbnail)
- **Add person** button: typeahead search over existing people names, with a
  "Create new person" option if no match
- Optional bounding box: after picking a person, a "Draw region" toggle lets the
  user drag a rectangle on the photo thumbnail before saving (Phase A can ship
  without this; the API already accepts nullable bbox)
- Remove button on each tag

### Recognition feature states in the UI

The frontend calls `GET /api/recognition/status` at boot and stores the result.
Every recognition-related UI element observes these three states:

| State | `enabled` | `available` | UI presentation |
|---|---|---|---|
| **Off** | false | any | Toggle switch shown but off; click to enable in config |
| **Unavailable** | true | false | Toggle shown as disabled/greyed-out; tooltip shows `reason` (e.g. "libonnxruntime not found") |
| **Active — GPU** | true | true, CUDA | Normal; no warning |
| **Active — CPU** | true | true, CPU | Active but with an amber badge: "Running on CPU — may be slow for large libraries" |

The recognition toggle lives in the Settings page (alongside existing scan
configuration). The People browse and tagging UI is always available regardless
of recognition state — manual tagging works in all states.

### Recognition review page: `/faces/review` (Phase C, only shown when `enabled AND available`)

- Two-panel layout
- Left: unidentified face clusters (grouped by similarity cluster); each cluster
  shows a strip of face crops and a "Name this person" input
- Right: suggested matches (auto-assigned but unverified); each card shows the
  face crop, the suggested name, and Confirm / Not this person buttons
- Batch confirm button for a whole cluster
- When `execution_provider = "CPU"`, a persistent banner: "Face recognition is
  running on CPU. Processing may be slow — consider installing CUDA for
  GPU acceleration."

---

## Verification Steps

1. `go build ./...` on a machine with a C compiler but **without** libonnxruntime — succeeds
2. Start server with `face_recognition.enabled = false` →
   `GET /api/recognition/status` returns `{"enabled":false,"available":false,"reason":null}`
3. Start server with `enabled = true` but bad `onnxruntime_lib` path →
   `GET /api/recognition/status` returns `{"enabled":true,"available":false,"reason":"...not found..."}`;
   server starts normally; manual tagging fully operational
4. Apply migration, run `go run . --config test_config.json --scan` — no crash;
   `SELECT count(*) FROM faces` = 0 (no auto-detection without onnxruntime)
5. `POST /api/people {"name":"Alice"}` → 201; `GET /api/people` → `[{id:1, name:"Alice", photo_count:0}]`
6. `POST /api/library/copies/{id}/faces {"person_id":1}` → 201
7. `GET /api/library/copies/{id}/faces` → face row with `bbox_x=null`, `verified=1`
8. `GET /api/people/1/photos` → photo appears
9. `DELETE /api/people/1` → 204; face row persists with `person_id=null`
10. `GET /api/faces/unidentified` → 501 (`enabled=false`)
11. `enabled=true` + bad lib path → `GET /api/faces/unidentified` → 503 with `{"reason":"..."}`
12. *(Phase B/C)* With libonnxruntime installed + models present →
    `GET /api/recognition/status` returns `{"enabled":true,"available":true,"execution_provider":"CUDA"}` (or `"CPU"`)
13. *(Phase B/C)* Scan runs → `faces` rows populated with bbox + embedding;
    `GET /api/faces/unidentified` returns results
