# Spec: People Tagging

## Goals

Allow users to tag which people appear in photos in the internal library. In the
interim, tagging is entirely manual — presence-only or with an optional drawn
bounding box. In a later phase, automated face detection and recognition can be
enabled as a compile-time-optional feature, requiring no GPU or network access,
but needing the onnxruntime C library to be installed.

**Non-goals:**
- Facial recognition as a hard dependency (the app must work fully without it)
- Tagging people in scan-library photos (people are library-scoped — only `library_copies`)
- Cloud-based facial recognition services

---

## Deployment Phases

| Phase | What ships | Recognition dependency |
|---|---|---|
| **A — Manual tagging** | `people` table, extended `faces`, full CRUD API, tagging UI on library photo detail, `/people` browse page | None — pure Go, no CGO |
| **B — Face detection** | Auto-populate `faces` rows with bounding boxes during scan; user confirms/removes and assigns people | pigo (pure Go) or onnxruntime-go (CGO, build tag `facerecog`) |
| **C — Face recognition** | Embedding extraction, identity clustering, suggestion pipeline, review UI | onnxruntime-go (CGO, build tag `facerecog`) |

The build tag `facerecog` gates all CGO code. `go build ./...` (no tags) must always
succeed and produce a fully functional binary for Phases A and below.

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
    ModelDir             string  `json:"model_dir"`
    DetectionModel       string  `json:"detection_model"`
    RecognitionModel     string  `json:"recognition_model"`
    DetectionThreshold   float64 `json:"detection_threshold"`
    RecognitionThreshold float64 `json:"recognition_threshold"`
    ClusterMinSamples    int     `json:"cluster_min_samples"`
}
```

`config.Validate()` must check when `enabled = true`:
- `model_dir` is non-empty and the directory exists
- both model files exist within `model_dir`
- `recognition_threshold` is in the range 0.0–1.0

When `enabled = false`, the entire `face_recognition` block is ignored at
runtime.

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

### Recognition (Phase C, gated on `face_recognition.enabled`)

All endpoints below return `501 Not Implemented` when `enabled = false`.

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

All files in this package use the `facerecog` build tag except `stub.go`.

| File | Build tag | Purpose |
|---|---|---|
| `detect.go` | `facerecog` | Load SCRFD ONNX model via onnxruntime-go; run detection on a decoded image; return `[]Detection{BboxX, BboxY, BboxW, BboxH, Confidence}` |
| `embed.go` | `facerecog` | Load ArcFace/R50 ONNX model; crop + align face from bounding box; return `[]float32` embedding (512-dim) |
| `cluster.go` | `facerecog` | Cosine similarity matrix + single-linkage agglomerative clustering; return `[]FaceCluster` |
| `suggest.go` | `facerecog` | For each unidentified face, compute cosine distance to per-person mean embeddings; emit `Suggestion{FaceID, PersonID, Score}` above threshold |
| `stub.go` | `!facerecog` | No-op stubs for all exported functions so the codebase compiles without onnxruntime |

The stub file exports the same function signatures returning zero values and a
descriptive `errors.New("face recognition not compiled in")` error. Callers in
`scanner.go` and `scan.go` check this error and skip silently.

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

All recognition handlers (`handleUnidentifiedFaces` onwards) return
`501 Not Implemented` when `cfg.FaceRecognition.Enabled == false`.

### Changes to existing files

| File | Change |
|---|---|
| `internal/config/config.go` | Add `FaceRecognitionConfig` field; extend `Validate()` |
| `internal/api/router.go` | Register new routes from `people.go` |
| `internal/scan/scanner.go` | After thumbnail: if `faceRecog.Enabled`, call `recognition.Detect` + `recognition.Embed`, persist results via `db.InsertFace` |
| `internal/api/scan.go` | After `cluster.Run`: call `recognition.SuggestIdentities(db, cfg)` and `recognition.ClusterUnidentified(db, cfg)` when enabled |
| `web/js/app.js` | Register `/people` and `/people/:id` routes; add `people-enabled` (or reuse `library-enabled`) body class |
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

**Decision:** onnxruntime-go, behind build tag `facerecog`. The `!facerecog` stub
ensures the standard single-binary build has zero extra dependencies.

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

### Recognition review page: `/faces/review` (Phase C, only shown when `enabled`)

- Two-panel layout
- Left: unidentified face clusters (grouped by similarity cluster); each cluster
  shows a strip of face crops and a "Name this person" input
- Right: suggested matches (auto-assigned but unverified); each card shows the
  face crop, the suggested name, and Confirm / Not this person buttons
- Batch confirm button for a whole cluster

---

## Verification Steps

1. `go build ./...` — succeeds with no face recognition dependency
2. Apply migration, run `go run . --config test_config.json --scan` — no crash;
   `SELECT count(*) FROM faces` = 0 (no auto-detection in standard build)
3. `POST /api/people {"name":"Alice"}` → 201; `GET /api/people` → `[{id:1, name:"Alice", photo_count:0}]`
4. `POST /api/library/copies/{id}/faces {"person_id":1}` → 201
5. `GET /api/library/copies/{id}/faces` → face row with `bbox_x=null`, `verified=1`
6. `GET /api/people/1/photos` → photo appears
7. `DELETE /api/people/1` → 204; face row persists with `person_id=null`
8. `GET /api/faces/unidentified` → 501 (recognition not enabled)
9. *(Phase C)* `go build -tags facerecog ./...` with onnxruntime installed → succeeds
10. *(Phase C)* Scan with `face_recognition.enabled = true` → `faces` rows populated with bbox + embedding; `GET /api/faces/unidentified` returns results
