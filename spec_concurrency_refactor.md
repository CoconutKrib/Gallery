# Scan Pipeline Concurrency Review

## Status: Analysis complete, refactoring proposed

---

## 1. Current architecture

### 1.0 Photo pipeline stages and where face detection fires

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

## 3. Proposed refactoring

### 3.1 Design principle: defer expensive work to library copy

The scan pipeline has three kinds of work, with very different cost profiles
and different appropriate stages:

| Work | Cost | Should run at | Why |
|------|------|--------------|-----|
| EXIF, hash, thumbnail | Low (CPU/I/O) | **SCAN** | Needed for duplicate detection, browse, search |
| File copy | Low (I/O) | **COPY** | Only for approved photos |
| Face detection + embedding | High (GPU + DB) | **COPY** | Only library photos need face data |

**Current problem**: face detection runs at SCAN time on all discovered photos.
A large source directory with thousands of photos will run face detection on
every one, even if the user only keeps 10%. This wastes GPU time, DB writes
(one `faces` row per detected face), and DB storage (512-dim embeddings).

**Proposed change**: move face detection from SCAN to COPY. When a photo is
approved and copied into the library (`library.CopyPhoto`), trigger face
detection on the library copy. This ensures face data only exists for photos
the user has chosen to keep.

The `runRecognitionPostScan` (suggestions + clustering) would also move to
after the bulk copy completes, rather than after scan.

### 3.2 Design principle: separate I/O work from GPU work

The scan pipeline has two very different kinds of work:

| Type | Examples | Best parallelism model |
|------|----------|----------------------|
| **I/O + CPU** | File read, SHA-256, EXIF parse, JPEG resize | Pool of N workers, one per CPU core |
| **GPU inference** | SCRFD detection, ArcFace embedding | **Single worker, sequential queue** — keeps GPU hot, avoids session duplication |

The current design conflates these: thumbnail workers do both I/O-bound work
(resize, file write) and GPU inference (detection, embedding) in the same
goroutine. This forces every worker to hold its own ONNX sessions.

### 3.2 Recommended architecture: thumbnail pool + dedicated face worker

```
                         ┌──────────────────┐
  WalkDir ──────────────→│  fileJob channel │
  (1 goroutine, fast)    └────────┬─────────┘
                                  │
               ┌──────────────────┼──────────────────┐
               ▼                  ▼                  ▼
        ┌────────────┐    ┌────────────┐    ┌────────────┐
        │ worker 1   │    │ worker 2   │... │ worker M   │  (M = ScanWorkers)
        │ EXIF       │    │ EXIF       │    │ EXIF       │
        │ Hash       │    │ Hash       │    │ Hash       │
        │ Thumbnail  │    │ Thumbnail  │    │ Thumbnail  │
        │            │    │            │    │            │
        │── faceJob ─│──  │── faceJob ─│──  │── faceJob ─│──
        └────────────┘    └────────────┘    └────────────┘
               │                  │                  │
               └──────────────────┼──────────────────┘
                                  ▼
                         ┌──────────────────┐
                         │  faceJob channel │
                         └────────┬─────────┘
                                  │
                                  ▼
                         ┌──────────────────────────┐
                         │  Face worker (1 goroutine)│
                         │                           │
                         │  ← single SCRFD session   │
                         │  ← single ArcFace session │
                         │                           │
                         │  for job := range queue:  │
                         │    Decode image           │
                         │    detector.Detect(img)   │
                         │    embedder.Embed(img)     │
                         │    DB insert face         │
                         └──────────────────────────┘
```

**Key properties:**

1. **Thumbnail workers** (I/O + CPU) stay parallel — image resize, file write,
   EXIF, hashing. No ONNX sessions needed here.

2. **Face worker** is a single goroutine with a single SCRFD session and a
   single ArcFace session. The sessions are created once at scan start and
   destroyed at scan end. Images are processed sequentially from a buffered
   channel. This keeps the GPU pipeline hot and eliminates session duplication
   entirely.

3. **DB writes** are serialized either through the face worker (for face inserts)
   or through a mutex shared with the walk goroutine (for photo inserts +
   thumbnail path updates).

### 3.3 API changes to `internal/recognition`

The `Detector` and `Embedder` types need to hold a persistent session:

```go
type Detector struct {
    modelPath   string
    threshold   float32
    sessionOpts *ort.SessionOptions
    mu          sync.Mutex        // serialize access to the session
    session     *ort.AdvancedSession
}

func (d *Detector) Detect(img image.Image) ([]Detection, error) {
    d.mu.Lock()
    defer d.mu.Unlock()
    // ... reuse d.session, update input tensor data, call session.Run()
}
```

The session is created once via a new `Open()` method and destroyed via `Close()`.
The mutex ensures only one goroutine uses the session at a time — exactly what
we want for a single-consumer queue.

The face worker goroutine would:

```go
func (s *Scanner) faceWorker(jobs <-chan *FaceJob) {
    detector := recognition.GetDetector()
    embedder := recognition.GetEmbedder()
    for job := range jobs {
        img := decodeImage(job.SourcePath)
        detections, _ := detector.Detect(img)  // mutex-protected, sequential
        for _, det := range detections {
            emb, _ := embedder.Embed(img, det, ...)  // mutex-protected, sequential
            // ... build Face, send to DB writer
        }
    }
}
```

### 3.4 Benefits

| Aspect | Before | After |
|--------|--------|-------|
| ONNX sessions | N×2 per scan | 2 total (shared) |
| VRAM usage | N × ~50 MB per model | ~50 MB per model (fixed) |
| GPU throughput | Context-switching between N sessions | Sequential, hot pipeline |
| DB writes | Contention, retry | Serialized (mutex or dedicated writer) |
| Code clarity | Face detection embedded in thumbnail worker | Clean separation: thumbnails vs faces |

---

## 4. Additional opportunities

### 4.1 Hash-then-read optimization

Currently `HashFile` reads the entire file, then `GenerateThumbnail` (or
`decodeImage` for face detection) reads it again. With a single file read
into a `[]byte` buffer, the hash can be computed and the image decoded from
the same buffer:

```go
data, _ := os.ReadFile(path)
hash := sha256.Sum256(data)
img, _, _ := image.Decode(bytes.NewReader(data))
```

**Trade-off**: memory for large RAW/TIFF files. For JPEG/HEIC phone photos
(typically 2–12 MB), this is fine. Can add a size threshold.

### 4.2 Thumbnail caching before DB insert

Thumbnails are written to disk and their path is DB-updated. If the DB insert
happens first (current order), a race exists where the API could serve a photo
with no thumbnail yet. Reversing the order (write thumbnail, then DB insert)
eliminates this.

### 4.3 Face detection batch processing

Rather than running face detection inline in thumbnail workers, batch face
detection jobs and process them after all thumbnails are done. This lets the
thumbnail pool finish faster and avoids competing for CPU between image resize
and ONNX inference.

---

## 5. Recommendation

**Move face detection from SCAN to COPY**, and **use a single dedicated face
worker with persistent ONNX sessions**. This combination:

1. **Eliminates wasted work** — only library photos get face detection
2. **Eliminates ONNX session duplication** — single SCRFD + ArcFace session
3. **Eliminates SQLITE_BUSY** — face worker is single-threaded, no DB contention with thumbnail workers
4. **Keeps thumbnail generation parallel** — CPU-bound image resize still benefits from worker pool

The implementation has two independent axes that can be done separately:

| Axis | What changes | Solves |
|------|-------------|--------|
| **Pipeline stage** | Move `detectAndStoreFaces` call from `scanner.go` thumbnail workers to `library/copy.go` CopyPhoto | Wasted GPU/DB on rejected photos |
| **Concurrency model** | Single face worker goroutine + persistent ONNX sessions + DB write mutex | Session duplication, SQLITE_BUSY |

Both axes are compatible — the face worker can be started during the COPY
phase (in `handleLibraryCopyAll` or `CopyPhoto`) rather than during SCAN.

---

## 6. Implementation plan (step by step)

### Axis A: Move face detection from SCAN to COPY

**File**: `internal/scan/scanner.go`

- Remove the `detectAndStoreFaces` call from the thumbnail worker (line ~134):
  ```go
  // REMOVE:
  if recognition.IsAvailable() && job.PhotoID != 0 {
      s.detectAndStoreFaces(job.SourcePath, job.PhotoID)
  }
  ```

**File**: `internal/library/copy.go`, `CopyPhoto` function

- After the file is copied and `library_copies` record inserted, trigger face
  detection on the library copy if recognition is available:
  ```go
  if recognition.IsAvailable() {
      s.detectAndStoreFaces(absPath, photo.ID)
  }
  ```
  (The Scanner or a face-detection service needs to be accessible here.
  Alternatively, queue a face job on a shared channel.)

**File**: `internal/api/library.go`, `runBulkCopy`

- Move `runRecognitionPostScan` call to after bulk copy completes (it
  currently runs after scan in `internal/api/scan.go`).

**File**: `internal/api/scan.go`, `runScans` and `runDropzoneScan`

- Remove `runRecognitionPostScan` call from the scan completion path.

### Axis B: Single face worker + persistent ONNX sessions

**Files**: `internal/recognition/detect.go`, `internal/recognition/embed.go`

- Add `session *ort.AdvancedSession` and `mu sync.Mutex` to `Detector` struct
- Add `session *ort.AdvancedSession` and `mu sync.Mutex` to `Embedder` struct
- Add `Open() error` method that creates the session once
- Add `Close() error` method that destroys it
- Change `Detect()` and `Embed()` to lock `mu`, reuse `session`, unlock

**File**: `internal/scan/scanner.go` (or new `internal/scan/face_worker.go`)

- Extract face detection logic into a standalone face worker that can be
  started from either the scan path or the copy path:
  ```go
  func StartFaceWorker(jobs <-chan *FaceJob, db *sql.DB) {
      detector := recognition.GetDetector()
      embedder := recognition.GetEmbedder()
      detector.Open()
      embedder.Open()
      defer detector.Close()
      defer embedder.Close()
      for job := range jobs {
          detectAndStoreFaces(job.SourcePath, job.PhotoID, db)
      }
  }
  ```

**File**: `internal/scan/scanner.go`

- Add `dbMu sync.Mutex` to `Scanner` struct for DB write serialization
- Lock around `InsertPhoto`, `UpdateThumbnailPath`, `recordDuplicateIfNew`
- Remove the 50ms SQLITE_BUSY retry in `detectAndStoreFaces`

### Files touched

| Step | File | Lines changed |
|------|------|--------------|
| A.1 | `internal/scan/scanner.go` | −4 |
| A.2 | `internal/library/copy.go` | +8 |
| A.3 | `internal/api/library.go` | +3, −0 |
| A.4 | `internal/api/scan.go` | −3 |
| B.1 | `internal/recognition/detect.go` | ~30 |
| B.2 | `internal/recognition/embed.go` | ~30 |
| B.3 | `internal/scan/scanner.go` (face worker) | ~20 |
| B.4 | `internal/scan/scanner.go` (dbMu) | ~15 |
| **Total** | | **~110** |

### Behavior changes

| Aspect | Before | After |
|--------|--------|-------|
| When faces detected | During SCAN (all discovered photos) | During COPY (only library photos) |
| ONNX sessions | N×2 per scan (transient) | 2 total (persistent, shared) |
| DB contention | Walk vs thumbnail workers | Walk vs thumbnail workers (face worker is single-threaded) |
| `runRecognitionPostScan` | After scan | After bulk copy |
| Rejected photos | Faces stored but never used | No faces stored |

Estimated effort: ~150 lines net change in `internal/scan/scanner.go` (add
faceJob channel + face worker goroutine), ~30 lines in `internal/recognition`
(add session persistence + mutex to Detector/Embedder). All existing tests
should pass without modification. New tests for the face worker channel and
session reuse.
