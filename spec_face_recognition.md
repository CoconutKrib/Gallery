# Face Detection & Recognition Pipeline

## Status: Implemented ✅

---

## 1. Overview

The face recognition pipeline automatically detects faces in library photos,
generates identity embeddings for each face, suggests which unknown faces match
known people, and clusters similar unidentified faces together. The user reviews
and confirms these suggestions through a dedicated UI.

The pipeline uses **two separate ONNX models** from the InsightFace family:

| Model | Purpose | Input | Output |
|-------|---------|-------|--------|
| **SCRFD** (`det_10g.onnx`) | Face **detection** — locate faces in an image | 640×640 RGB image | Bounding boxes + confidence scores |
| **ArcFace** (`w600k_r50.onnx`) | Face **embedding** — create a 512-dim vector that uniquely represents a face | 112×112 cropped face | L2-normalised 512-dim float32 embedding |

The insight is: SCRFD says *where* faces are; ArcFace says *who* each face is
(represented as a 512-dimensional point in "face space"). Two faces of the same
person will have embeddings that are close together (cosine similarity near 1.0);
two different people will be far apart.

---

## 2. Pipeline stages

```
┌─────────────────────────────────────────────────────────────────────┐
│  STAGE 1: DETECT + EMBED (background FaceQueue worker)               │
│                                                                      │
│  Photo ingested into library                                         │
│       │                                                              │
│       ▼                                                              │
│  CopyPhoto → EnqueueFaceDetection(photoID, priority=1)               │
│       │                                                              │
│       ▼                                                              │
│  FaceQueue worker (single goroutine, persistent ONNX sessions):      │
│    1. Mark photos.recognition_status = 'pending'                     │
│    2. Decode image (JPEG or HEIC)                                    │
│    3. SCRFD: Detect(img) → []Detection{bbox, score}                  │
│    4. For each detection:                                            │
│       a. Crop face to 112×112                                        │
│       b. ArcFace: Embed(crop) → 512-dim float32 vector               │
│       c. INSERT INTO faces (photo_id, bbox_*, confidence,            │
│            embedding, source='auto', verified=0)                     │
│    5. Mark photos.recognition_status = 'done'                        │
│    6. Mark photos.recognition_version = CurrentRecognitionVersion    │
│                                                                      │
│  STAGE 2: SUGGEST + CLUSTER (after bulk copy or manual trigger)     │
│                                                                      │
│  runRecognitionPostScan():                                            │
│                                                                      │
│    2a. SUGGEST (per-person mean matching)                            │
│    ┌──────────────────────────────────────────────────┐             │
│    │  For each person with verified faces:            │             │
│    │    Compute mean of all their face embeddings     │             │
│    │  For each unidentified face:                     │             │
│    │    Find closest person mean by cosine similarity │             │
│    │    If similarity >= (1 - recognition_threshold): │             │
│    │      Set faces.person_id = that person           │             │
│    │      (unverified — user must confirm)            │             │
│    └──────────────────────────────────────────────────┘             │
│                                                                      │
│    2b. CLUSTER (union-find on unidentified faces)                    │
│    ┌──────────────────────────────────────────────────┐             │
│    │  For all remaining unidentified faces:           │             │
│    │    Union-find: merge if cosine_sim >= threshold  │             │
│    │    Discard clusters smaller than min_samples     │             │
│    │    Store in-memory (server restart clears)       │             │
│    └──────────────────────────────────────────────────┘             │
│                                                                      │
│  STAGE 3: USER REVIEW (Face Review UI at /faces/review)              │
│                                                                      │
│  Left panel: Clusters of unidentified faces                          │
│    • Each cluster shown as face crops                                │
│    • Dropdown to assign all faces in cluster to a person             │
│    • "Confirm" writes person_id + verified=1 for all faces           │
│                                                                      │
│  Right panel: Auto-suggestions                                       │
│    • Shows each suggested face with proposed person name             │
│    • ✓ Confirm: writes verified=1                                    │
│    • ✗ Reject:  clears person_id (unverified column left as-is)     │
└─────────────────────────────────────────────────────────────────────┘
```

---

## 3. The two models in detail

### 3.1 SCRFD — face detection

**Model**: `det_10g.onnx` (InsightFace SCRFD-10G)  
**Input**: 640×640×3 BGR image, normalised by `(pixel - 127.5) / 128.0`  
**Output**: 9 tensors — scores, bounding boxes, and keypoints at strides 8, 16, 32  

The model uses an anchor-based detection scheme across three feature map
strides. Raw outputs are decoded into `Detection` structs:

```go
type Detection struct {
    X1, Y1, X2, Y2 float32       // bounding box in 640×640 space
    Score          float32       // confidence in [0, 1]
    Kps            [5][2]float32 // facial keypoints (stored but unused)
}
```

A configurable **detection threshold** (`face_recognition.detection_threshold`,
default 0.5) filters low-confidence detections. Greedy NMS with IoU threshold
0.4 removes overlapping boxes.

Bounding boxes are scaled from 640×640 space back to the original image
dimensions and stored as normalised `[0,1]` coordinates in the `faces` table.

### 3.2 ArcFace — face embedding

**Model**: `w600k_r50.onnx` (InsightFace ArcFace ResNet-50)  
**Input**: 112×112×3 BGR face crop, same normalisation  
**Output**: Node `683` — 512-dim float32 vector  

ArcFace is trained with an additive angular margin loss that forces embeddings
of the same person to be close and different people to be far apart. The output
is already L2-normalised so **cosine similarity = dot product**.

The 512-dimensional embedding is stored as raw little-endian float32 bytes in
the `faces.embedding` BLOB column (512 × 4 = 2048 bytes per face).

---

## 4. Database schema

### 4.1 Tables

```sql
-- Identity registry. Each row is a named person.
CREATE TABLE people (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    name          TEXT    NOT NULL DEFAULT '',
    notes         TEXT,
    cover_face_id INTEGER REFERENCES faces(id),
    created_at    TEXT    NOT NULL DEFAULT (datetime('now'))
);

-- Face annotations. Each row is one detected or manually-tagged face.
CREATE TABLE faces (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    photo_id    INTEGER NOT NULL REFERENCES photos(id),
    person_id   INTEGER REFERENCES people(id),  -- NULL = unidentified
    bbox_x      REAL,     -- normalised 0.0–1.0
    bbox_y      REAL,
    bbox_w      REAL,
    bbox_h      REAL,
    source      TEXT NOT NULL DEFAULT 'manual',  -- 'manual' | 'auto'
    confidence  REAL,     -- detection score; NULL for manual
    embedding   BLOB,     -- 2048 bytes: 512 × float32 LE
    verified    INTEGER NOT NULL DEFAULT 0,      -- 1 = user confirmed
    created_at  TEXT NOT NULL DEFAULT (datetime('now'))
);

-- Per-photo recognition tracking (006_recognition_version.sql).
ALTER TABLE photos ADD COLUMN recognition_version INTEGER;
ALTER TABLE photos ADD COLUMN recognition_status TEXT;   -- 'pending'|'done'|'error'
ALTER TABLE photos ADD COLUMN recognition_error TEXT;
```

### 4.2 Key relationships

```
photos ──1:N──→ faces ──N:1──→ people
  │                │
  │                ├── source: 'auto' (pipeline) or 'manual' (user-tagged)
  │                ├── verified: 0 (pending review) or 1 (confirmed)
  │                └── embedding: BLOB (only present for auto-detected faces)
  │
  ├── recognition_version: NULL=never, or CurrentRecognitionVersion
  ├── recognition_status: 'pending' / 'done' / 'error'
  └── recognition_error: error message when status='error'
```

### 4.3 Face states

| person_id | verified | source | Meaning |
|-----------|----------|--------|---------|
| NULL | 0 | auto | Unidentified — appears in clustering panel |
| NOT NULL | 0 | auto | Suggested by pipeline — appears in suggestions panel |
| NOT NULL | 1 | auto | User confirmed — used as training data for future suggestions |
| NOT NULL | 1 | manual | User manually tagged — also used as training data |
| NOT NULL | 0 | manual | User tagged but hasn't confirmed (edge case, rarely used) |

---

## 5. Suggestion algorithm

The suggestion pipeline (`recognition.Suggest`) runs after every bulk copy.

### 5.1 Per-person mean embedding

For each person with one or more **verified** faces that have embeddings:

```
person_mean[p] = L2_normalize( sum of all verified face embeddings for person p )
```

### 5.2 Nearest-neighbor matching

For each **unidentified** face (`person_id IS NULL`, has embedding):

1. Compute cosine similarity against every person's mean embedding
2. If the best similarity ≥ `1 - recognition_threshold` (default threshold 0.35 → similarity ≥ 0.65):
   - Set `faces.person_id = best_person_id`
   - Keep `verified = 0` (user must confirm)
3. If no person meets the threshold, the face remains unidentified

The suggestion never overwrites a `verified = 1` face, and never assigns to a
person the user hasn't confirmed at least one face for.

---

## 6. Clustering algorithm

The clustering pipeline (`recognition.Cluster`) runs after suggestions, grouping
remaining unidentified faces.

### 6.1 Union-find (single-linkage)

```
For every pair of unidentified faces (i, j):
    If cosine_similarity(emb_i, emb_j) >= 1 - recognition_threshold:
        Union(i, j)
```

This is O(n²) in the number of unidentified faces. For a library of thousands
of faces this is acceptable because it runs only after bulk operations, not
per-photo.

### 6.2 Filter and store

- Clusters smaller than `cluster_min_samples` (default 2) are discarded
- Results stored in-memory via `recognition.SetClusters()`
- `GET /api/faces/unidentified` annotates each face with its `cluster_id`
- Server restart clears clusters; re-run clustering from the Review UI or via `POST /api/faces/cluster`

---

## 7. Configuration knobs

All tunables live under `face_recognition` in `config.json` and can be edited
in the Settings page. A server restart is required for changes to take effect.

| Knob | Default | Range | Effect |
|------|---------|-------|--------|
| `enabled` | `false` | bool | Master switch. When false, all recognition is skipped. |
| `detection_threshold` | `0.5` | 0.0–1.0 | Minimum confidence for SCRFD to report a face. Lower = more detections (more false positives). Higher = fewer (misses some faces). |
| `recognition_threshold` | `0.4` | 0.0–1.0 | Maximum cosine distance for two faces to be considered the same person. Used in both suggestions and clustering. Lower = stricter (fewer matches, more conservative). Higher = looser (more matches, risk of merging different people). |
| `cluster_min_samples` | `2` | ≥1 | Minimum faces in a cluster for it to appear in the Review UI. Set to 1 to see every unidentified face. |
| `onnxruntime_lib` | (required) | path | Path to `libonnxruntime.so` |
| `model_dir` | (required) | path | Directory containing `.onnx` model files |
| `detection_model` | `det_10g.onnx` | filename | SCRFD model within `model_dir` |
| `recognition_model` | `w600k_r50.onnx` | filename | ArcFace model within `model_dir` |

### Tuning guide

- **Too many false positive detections** (faces detected on walls, trees): raise `detection_threshold` to 0.6–0.7.
- **Missing real faces**: lower `detection_threshold` to 0.3–0.4.
- **Same person split across multiple people**: lower `recognition_threshold` to 0.3 (more permissive matching). Use the Merge action on the People page to fix existing splits.
- **Different people incorrectly merged**: raise `recognition_threshold` to 0.5 (stricter matching).
- **Dashboard cluttered with tiny clusters**: raise `cluster_min_samples` to 3–5.

---

## 8. Model versioning

A constant `CurrentRecognitionVersion` in `db/photos.go` tracks the active
model version. When models are upgraded (e.g., SCRFD v2 → v3, or ArcFace
R50 → R100):

1. Bump `CurrentRecognitionVersion` (e.g., 1 → 2)
2. All existing `recognition_version < 2` rows are now stale
3. Click "Reprocess All Photos" in Settings to batch-enqueue stale photos
4. The background worker reprocesses them in `captured_at` order

Old embeddings remain in the `faces` table until the worker re-runs detection
on that photo. The worker checks `HasAutoFacesForPhoto` first — if auto-faces
already exist, they are **not** removed before re-detection (the new run
creates additional rows). Manual face tags are never affected.

### TODO: re-detection cleanup

Currently, re-running detection on a photo that already has auto-faces creates
duplicate face rows. A future improvement: delete old `source='auto'` rows
before re-detecting, so the faces table doesn't accumulate stale embeddings.

---

## 9. API reference

### Detection triggers

| Method | Endpoint | Description |
|--------|----------|-------------|
| `POST` | `/api/photos/{sha256}/detect-faces` | Enqueue for detection (priority 0=manual). Returns `{queued, reason}`. |
| `POST` | `/api/recognition/reprocess-all` | Batch-enqueue up to 1000 eligible photos in `captured_at` order (priority 2). |

### Review UI data

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/faces/unidentified` | All faces with `person_id IS NULL`, annotated with `cluster_id`. |
| `GET` | `/api/faces/suggestions` | Faces with `person_id NOT NULL AND verified = 0`. |
| `POST` | `/api/faces/{id}/confirm` | Body: `{person_id}`. Sets `verified = 1`, `person_id`. |
| `POST` | `/api/faces/{id}/reject` | Clears `person_id`, sets `verified = 0`. |
| `POST` | `/api/faces/cluster` | Re-runs clustering on current unidentified faces. |

### Status

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/recognition/status` | Runtime status + queue depth. |
| `GET` | `/api/recognition/queue` | Queue stats: `{total_queued, total_done, total_errors, processing_now}`. |

### Manual face tagging (Phase A — always available)

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/library/copies/{id}/faces` | List faces for a library copy. |
| `POST` | `/api/library/copies/{id}/faces` | Body: `{person_id, bbox_*?}`. Manual tag. |
| `PATCH` | `/api/faces/{id}` | Update bounding box or person assignment. |
| `DELETE` | `/api/faces/{id}` | Remove a face tag. |

---

## 10. UI reference

### 10.1 Face Review (`/faces/review`)

Two-panel layout accessible from the nav bar (gated on internal library enabled).

**Left panel — Unidentified Faces:**
- Faces grouped by `cluster_id` (from in-memory clustering)
- Each cluster shows face thumbnails cropped to bounding boxes
- Dropdown to assign all faces in a cluster to a person
- "Confirm" button sets `person_id` + `verified = 1` for all faces in the cluster
- Unclustered faces shown as individual cards

**Right panel — Auto-Suggestions:**
- Each suggested face shows the proposed person name and confidence score
- ✓ Confirm: accepts the suggestion (`verified = 1`)
- ✗ Reject: clears the suggestion (`person_id = NULL`)

**Re-run clustering button** calls `POST /api/faces/cluster` and refreshes.

### 10.2 Library edit panel

The "People in this photo" section in the library photo editor:
- Shows existing face tags (manual and auto)
- "Auto-Detect Faces" button enqueues the photo for detection
- Manual tagging input with person autocomplete

### 10.3 Settings → Face Recognition

- Live status: enabled/disabled, execution provider (CUDA/CPU), queue depth
- All configuration knobs editable
- "Reprocess All Photos" button for model upgrades

### 10.4 People page (`/people`)

- List all people with photo counts
- Person detail: edit name/notes, view tagged photos, merge into another person
- Merge: reassigns all face tags from one person to another, deletes the source

---

## 11. Files reference

| File | Role |
|------|------|
| `internal/recognition/detect.go` | SCRFD detector: `Detect(img) → []Detection` |
| `internal/recognition/embed.go` | ArcFace embedder: `Embed(img, det) → []float32` |
| `internal/recognition/preprocess.go` | Image preprocessing: resize, BGR, normalise, crop, L2-norm |
| `internal/recognition/suggest.go` | Suggestion pipeline: per-person mean → nearest-neighbor |
| `internal/recognition/cluster.go` | Union-find clustering of unidentified faces |
| `internal/recognition/queue.go` | `FaceQueue` + background worker goroutine |
| `internal/recognition/recognition.go` | Package init, singleton management, `EnqueueFaceDetection` |
| `internal/api/scan.go` | `runRecognitionPostScan` (suggest + cluster trigger) |
| `internal/api/people.go` | Face CRUD, suggestion/cluster endpoints |
| `internal/api/settings.go` | Recognition status + queue endpoints |
| `internal/api/photos.go` | `POST /api/photos/{sha256}/detect-faces` |
| `internal/db/people.go` | Face + Person CRUD, `HasAutoFacesForPhoto` |
| `internal/db/photos.go` | Recognition version tracking helpers |
| `internal/db/migrations/004_people.sql` | People + extended faces schema |
| `internal/db/migrations/006_recognition_version.sql` | Version/status columns on photos |
| `web/js/people.js` | Face Review UI, people list/detail, merge |
| `web/js/library.js` | Library edit panel face-tagging + detect button |
| `web/js/photo.js` | Photo detail detect button |
| `web/js/settings.js` | Recognition config + queue status + reprocess button |
