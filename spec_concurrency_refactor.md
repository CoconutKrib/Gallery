# Scan Pipeline Concurrency Refactor

## Status: Implemented ✅

---

## 1. Problem analysis (pre-refactor state)

### 1.0 Photo pipeline stages and where face detection fired

The app has a three-stage pipeline:

```
  SCAN                    STAGING                  COPY
  (discover)              (review)                 (ingest into library)
  ─────────────────────────────────────────────────────────────────────
  Walk source dirs   →    User reviews photos  →   File copied to
  Extract EXIF             Annotates metadata       managed hierarchy
  SHA-256 hash             Approves or rejects      library_copies row
  InsertPhoto row                                   inserted
  Generate thumbnail                                
  detectAndStoreFaces ←── RUNS HERE (on ALL discovered photos)
  runRecognitionPostScan
  (suggest + cluster)
```

**Face detection currently runs during SCAN**, before the user has decided
which photos to keep. Every discovered photo gets full SCRFD detection +
ArcFace embedding + `faces` table insert, regardless of whether it will ever
be approved for the library.

The people UI (`/people`, face tagging panel, `/faces/review`) only operates
on `library_copies`. Auto-faces on rejected photos are never surfaced to the
user but still consumed GPU time, DB writes, and storage.

### 1.1 Goroutine layout

```
┌──────────────────────────────────────────────────────┐
│  ScanManager.runScans (single goroutine)              │
│                                                       │
│  for each LibraryPath:                                │
│    Scanner.Run(rootPath)  ← caller's goroutine        │
│    ┌─────────────────────────────────────────────┐   │
│    │  filepath.WalkDir(rootPath, func(path, d) { │   │
│    │    ← sequential, single goroutine            │   │
│    │    ...                                        │   │
│    │    ReadEXIF(path)     ← CPU-bound             │   │
│    │    HashFile(path)      ← I/O-bound            │   │
│    │    InsertPhoto(db)     ← DB write             │   │
│    │    thumbJobs <- job    ← non-blocking send     │   │
│    │  })                                           │   │
│    │                                               │   │
│    │  close(thumbJobs)                             │   │
│    │  thumbWg.Wait()                               │   │
│    └─────────────────────────────────────────────┘   │
│                                                       │
│  Thumbnail worker pool (N goroutines, N=ScanWorkers)  │
│  ┌─────────────────┐  ┌─────────────────┐            │
│  │ worker 1         │  │ worker N         │  ...       │
│  │ GenerateThumbnail │  │ GenerateThumbnail │           │
│  │ UpdateThumbnailPath│  │ UpdateThumbnailPath│          │
│  │ detectAndStoreFaces│  │ detectAndStoreFaces│           │
│  │   Decode HEIC      │  │   Decode HEIC      │           │
│  │   SCRFD Detect     │  │   SCRFD Detect     │           │
│  │   ArcFace Embed    │  │   ArcFace Embed    │           │
│  │   InsertFace(db)   │  │   InsertFace(db)   │  ←  DB   │
│  └─────────────────┘  └─────────────────┘     writes   │
└──────────────────────────────────────────────────────┘
```

### 1.2 What's shared

| Resource | Shared by | Synchronization |
|----------|-----------|-----------------|
| `s.db` (`*sql.DB`) | Walk goroutine + N thumbnail workers | None — single connection pool |
| `stats` (`Stats`) | Walk goroutine only (thumbnail workers don't touch) | None needed (single writer) |
| `thumbJobs` (chan) | Walk goroutine (producer), workers (consumers) | Channel semantics ✅ |
| ONNX sessions | Each thumbnail worker creates its own | Per-goroutine isolation ✅ |

---

## 2. Problems identified

### 2.1 SQLITE_BUSY — DB write contention (critical)

The walk goroutine and thumbnail workers share one `*sql.DB`. SQLite WAL mode
allows concurrent reads but only one writer at a time. When the walk goroutine
executes `InsertPhoto` or `recordDuplicateIfNew` and a thumbnail worker
simultaneously tries `InsertFace`, one gets `SQLITE_BUSY`.

**Current mitigation**: retry-after-50ms in `detectAndStoreFaces`. This is a
band-aid — it doesn't prevent the contention, it just retries once. A second
concurrent writer in the retry window could still fail.

**Production risk**: in WAL mode, SQLite busy-handler timeout defaults to 0
(immediate failure). A large scan with many faces could lose detections.

### 2.2 Sequential file processing (performance)

`filepath.WalkDir` processes each file inline in the walk callback. For each
file, the walk goroutine does:

1. `ReadEXIF(path)` — file open + `goexif.Decode` (CPU)
2. `HashFile(path)` — full file read + SHA-256 (I/O)
3. `PhotoExistsByHash` — DB read
4. `InsertPhoto` — DB write

Step 2 reads the entire file, then the thumbnail worker reads it AGAIN for
thumbnail generation. This means every ingested file is read from disk twice.

The walk goroutine is blocked during all of these, preventing discovery of the
next file. For HEIC files with full-resolution decode, this is particularly
slow.

### 2.3 No multi-library parallelism (performance)

`runScans` iterates library paths sequentially:

```go
for _, lp := range paths {
    scanner.Run(lp.Path)  // blocks until complete
}
```

If a user has multiple scan paths on different physical drives, they could
scan in parallel for a 2× speedup.

### 2.4 ONNX session duplication — VRAM/memory waste (critical)

`Detect()` and `Embed()` both call `ort.NewAdvancedSession()` on every
invocation, run a single inference, then `Destroy()` the session. With N
concurrent thumbnail workers, there are **N × 2 ONNX sessions** alive
simultaneously — each holding a full copy of the model weights.

| Worker count | SCRFD sessions | ArcFace sessions | VRAM (approx) |
|-------------|---------------|-----------------|---------------|
| 1 | 1 | 1 | ~100 MB |
| 2 | 2 | 2 | ~200 MB |
| 4 | 4 | 4 | ~400 MB |
| 8 | 8 | 8 | ~800 MB |

Additionally, GPU context-switching between concurrent sessions causes
**throughput loss** — a single session processing images sequentially runs
faster per image than N sessions running concurrently, because the GPU
execution pipeline stays hot and avoids dispatch overhead.

**Root cause**: sessions are treated as transient request-scoped objects
instead of persistent singletons. The `Detector` and `Embedder` structs hold
`modelPath` and `sessionOpts` but not the session itself.

### 2.5 Thumbnail worker design coupling (design)

Thumbnail workers handle three unrelated tasks in sequence:

1. `GenerateThumbnail` — decode image, resize, encode JPEG
2. `UpdateThumbnailPath` — DB write
3. `detectAndStoreFaces` — decode image AGAIN, run SCRFD, run ArcFace, DB write

Tasks 2 and 3 don't need the thumbnail result (they use the source path). They're
bundled in the thumbnail worker purely because it's the only worker pool available.
A dedicated face-detection worker pool would be cleaner.

---

## 3. Implemented architecture

### 3.1 Design principle: defer expensive work to library copy

The scan pipeline has three kinds of work, with very different cost profiles:

| Work | Cost | Should run at | Why |
|------|------|--------------|-----|
| EXIF, hash, thumbnail | Low (CPU/I/O) | **SCAN** | Needed for duplicate detection, browse, search |
| File copy | Low (I/O) | **COPY** | Only for approved photos |
| Face detection + embedding | High (GPU + DB) | **COPY** | Only library photos need face data |

Face detection was moved from SCAN to COPY. When a photo is approved and
copied into the library, it is enqueued for async face detection in a
background worker (`recognition.FaceQueue`). The `runRecognitionPostScan`
(suggestions + clustering) fires after bulk copy completes.

### 3.2 Design principle: separate I/O from GPU work

| Type | Examples | Best parallelism model |
|------|----------|----------------------|
| **I/O + CPU** | File read, SHA-256, EXIF parse, JPEG resize | Pool of N workers, one per CPU core |
| **GPU inference** | SCRFD detection, ArcFace embedding | **Single worker, sequential queue** — keeps GPU hot, avoids session duplication |

Thumbnail workers no longer do face detection. A dedicated background goroutine
(`FaceQueue` worker) owns the persistent ONNX sessions for the server's lifetime.

### 3.3 Architecture diagram (as built)

```
                         ┌──────────────────┐
  WalkDir ──────────────→│  thumbJobs chan  │
  (1 goroutine)          └────────┬─────────┘
                                  │
               ┌──────────────────┼──────────────────┐
               ▼                  ▼                  ▼
        ┌────────────┐    ┌────────────┐    ┌────────────┐
        │ worker 1   │    │ worker 2   │... │ worker M   │  (M = ScanWorkers)
        │ Thumbnail  │    │ Thumbnail  │    │ Thumbnail  │
        │ DB update  │    │ DB update  │    │ DB update  │
        └────────────┘    └────────────┘    └────────────┘
        ↑ dbMu serialises DB writes between walk + workers

  ─ ─ ─ ─ ─ SCAN / COPY boundary ─ ─ ─ ─ ─

  CopyPhoto ────────────→ recognition.EnqueueFaceDetection(photoID, priority=1)
  Manual trigger ──────→ recognition.EnqueueFaceDetection(photoID, priority=0)
  Reprocess-all ───────→ recognition.EnqueueFaceDetection(photoID, priority=2)

                                  ▼
                         ┌──────────────────────────────┐
                         │  FaceQueue (priority queue)   │
                         │  0=manual, 1=copy, 2=batch    │
                         └────────────┬─────────────────┘
                                      │
                                      ▼
                         ┌──────────────────────────────┐
                         │  Face worker (1 goroutine)    │
                         │  runs for server lifetime     │
                         │                               │
                         │  ← single SCRFD session       │
                         │  ← single ArcFace session     │
                         │                               │
                         │  for job := range queue:      │
                         │    SetPhotoRecognitionPending │
                         │    Decode image (JPEG/HEIC)   │
                         │    detector.Detect(img)       │
                         │    embedder.Embed(img, det)   │
                         │    InsertFace(db)             │
                         │    SetPhotoRecognitionDone    │
                         └──────────────────────────────┘
```

**Key properties:**

1. **Thumbnail workers** (I/O + CPU) stay parallel — image resize, EXIF, hashing.
   No ONNX sessions. DB writes serialised via `Scanner.dbMu`.

2. **Face worker** is a single goroutine with persistent SCRFD + ArcFace sessions.
   Sessions are created once at server startup (`recognition.Init`) and destroyed
   at shutdown (`recognition.Cleanup`). Images are processed sequentially from a
   priority queue. This keeps the GPU pipeline hot and eliminates session
   duplication entirely.

3. **Face detection is asynchronous** from the copy operation. `CopyPhoto` returns
   immediately after enqueuing. The face worker processes jobs in the background.

### 3.4 DB version tracking

A new migration (`006_recognition_version.sql`) adds three columns to `photos`:

| Column | Type | Purpose |
|--------|------|---------|
| `recognition_version` | INTEGER | `NULL`=never attempted. Compared against `CurrentRecognitionVersion`. |
| `recognition_status` | TEXT | `pending` / `done` / `error`. Updated by the face worker. |
| `recognition_error` | TEXT | Error message when status is `error`. |

`CurrentRecognitionVersion = 1` is a constant in `db/photos.go`. When face
detection models are upgraded, bumping this constant invalidates all previous
results. The "Reprocess All" button in Settings enqueues all photos with stale
or missing versions.

On server restart, photos with `recognition_status = 'pending'` from a previous
run are automatically eligible for re-enqueuing via the batch reprocess endpoint.

### 3.5 Priority queue

Three priority levels in `FaceQueue`:

| Priority | Value | Source | Use case |
|----------|-------|--------|----------|
| Manual | 0 | UI button click | User wants faces NOW |
| Copy-time | 1 | `CopyPhoto` | Normal ingest flow |
| Background | 2 | "Reprocess All" | Catch-up for old/stale photos |

Lower value = higher urgency. The worker always picks the highest-priority job.
Re-enqueuing a photo at a higher priority bumps it (idempotent).

### 3.6 API changes

| Method | Endpoint | Description |
|--------|----------|-------------|
| `POST` | `/api/photos/{sha256}/detect-faces` | Enqueue for face detection (priority 0). Returns `{queued: true/false, reason}`. |
| `GET` | `/api/recognition/status` | Now includes `queue_queued`, `queue_done`, `queue_errors`, `queue_processing`. |
| `GET` | `/api/recognition/queue` | Dedicated queue status: `{total_queued, total_done, total_errors, processing_now}`. |
| `POST` | `/api/recognition/reprocess-all` | Batch-enqueues up to 1000 eligible photos in `captured_at` order (priority 2). Returns `{total_eligible, queued, note}`. Re-runnable for large libraries. |

### 3.7 `internal/recognition` package changes

**`Detector` / `Embedder`** — persistent sessions via `DynamicAdvancedSession`:

```go
type Detector struct {
    modelPath   string
    threshold   float32
    sessionOpts *ort.SessionOptions
    mu          sync.Mutex
    session     *ort.DynamicAdvancedSession
    opened      bool
}

func (d *Detector) Open() error   // create session once (idempotent)
func (d *Detector) Close() error  // destroy session (idempotent)
func (d *Detector) Detect(img)    // lazy-open if needed, reuse session under mutex
```

`Embedder` has the same pattern. Sessions are created by the `FaceQueue` worker
at startup and held for the server's lifetime.

**`FaceQueue`** (`recognition/queue.go` — new file):

```go
type FaceQueue struct { ... }
func NewFaceQueue(database *sql.DB) *FaceQueue  // starts background worker
func (q *FaceQueue) Enqueue(photoID int64, priority int) bool
func (q *FaceQueue) Status() (queued, done, errors int, processing *int64)
func (q *FaceQueue) Shutdown()
```

**`recognition.go`** — manages the FaceQueue singleton:

```go
func Init(cfg FaceRecognitionConfig, database *sql.DB) Status  // creates FaceQueue if available
func EnqueueFaceDetection(photoID int64, priority int) bool    // package-level convenience
func QueueStatus() (queued, done, errors int, processing *int64)
func Cleanup()  // shuts down FaceQueue, closes sessions, destroys ONNX env
```

### 3.8 UI changes

| Page | Change |
|------|--------|
| Photo detail (`photo.js`) | "Detect Faces" button with status feedback |
| Library edit panel (`library.js`) | "Auto-Detect Faces" button in the People/face-tagging section |
| Settings (`settings.js`) | Queue status (queued/done/errors + current processing). "Reprocess All Photos" button with batch-feedback. |

---

## 4. Benefits realised

| Aspect | Before | After |
|--------|--------|-------|
| When faces detected | During SCAN (all discovered photos) | During COPY (only library photos), async |
| ONNX sessions | N×2 per scan (transient) | 2 total (persistent, server lifetime) |
| VRAM usage | N × ~50 MB per model | ~50 MB per model (fixed) |
| GPU throughput | Context-switching between N sessions | Sequential, hot pipeline |
| DB contention (scan) | Walk vs thumbnail workers | Walk vs thumbnail workers (dbMu) |
| DB contention (faces) | N concurrent InsertFace | Single-threaded via face worker |
| `runRecognitionPostScan` | After scan | After bulk copy |
| Rejected photos | Faces stored but never used | No faces stored |
| Model upgrades | Stuck with old embeddings | Bump `CurrentRecognitionVersion`, click "Reprocess All" |
| Manual re-detect | Impossible | One click from photo detail or library panel |
| Queue visibility | None | Settings page shows queue depth + current processing |

---

## 5. Files changed

| File | Change |
|------|--------|
| `internal/db/migrations/006_recognition_version.sql` | **New** — schema columns |
| `internal/db/photos.go` | Photo struct + scan helpers + 6 new query functions |
| `internal/recognition/queue.go` | **New** — `FaceQueue` + `FaceWorker` |
| `internal/recognition/recognition.go` | `Init(cfg, db)`, `EnqueueFaceDetection`, `QueueStatus`, `Cleanup` extended |
| `internal/recognition/detect.go` | Persistent `DynamicAdvancedSession`, `Open()`/`Close()`, lazy-open `Detect()` |
| `internal/recognition/embed.go` | Same persistent session pattern |
| `internal/scan/scanner.go` | Removed `DetectAndStoreFaces` + `recognition` import; added `dbMu` |
| `internal/library/copy.go` | `CopyPhoto` enqueues via `recognition.EnqueueFaceDetection` instead of blocking |
| `internal/api/library.go` | Removed session Open/Close from `runBulkCopy` |
| `internal/api/scan.go` | Removed `runRecognitionPostScan` calls (moved to bulk copy) |
| `internal/api/photos.go` | `POST /api/photos/{sha256}/detect-faces` handler |
| `internal/api/settings.go` | `GET /api/recognition/queue`, `POST /api/recognition/reprocess-all`, extended status |
| `internal/api/router.go` | New route registrations |
| `main.go` | Pass `database` to `recognition.Init` |
| `internal/heif/face_detect_test.go` | Changed to external test package (`heif_test`) to break import cycle |
| `internal/integration/pipeline_test.go` | Updated `Init` call signature |
| `web/js/photo.js` | "Detect Faces" button |
| `web/js/library.js` | "Auto-Detect Faces" button in face-tagging panel |
| `web/js/settings.js` | Queue status display + "Reprocess All Photos" button |

---

## 6. Additional opportunities (future work)

### 6.1 Hash-then-read optimization

Currently `HashFile` reads the entire file, then `GenerateThumbnail` reads it
again. With a single `[]byte` buffer the hash can be computed and the image
decoded from the same read. Trade-off: memory for large files.

### 6.2 Thumbnail caching before DB insert

Reversing the order (write thumbnail, then DB insert) eliminates a race where
the API could serve a photo with no thumbnail yet.

### 6.3 Multi-library parallel scan

`runScans` iterates library paths sequentially. Multiple paths on different
physical drives could scan in parallel.

### 6.4 TODO: "Detect all" smart batching

The current "Reprocess All" endpoint enqueues batches of 1000. For very large
libraries (50k+ photos), a smarter scheduler could process in historic
`captured_at` order and show progress. The batch size could be configurable.

### 6.5 TODO: Dropzone watch mode

`inotify`/`fsnotify` to trigger scans automatically instead of manual trigger.
