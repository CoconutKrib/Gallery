package recognition

import (
	"database/sql"
	"image"
	"log/slog"
	"os"
	"strings"
	"sync"

	"github.com/halleck/gallery/internal/db"
	"github.com/halleck/gallery/internal/heif"
)

// faceJob represents a single face-detection request in the queue.
type faceJob struct {
	PhotoID  int64
	Priority int // 0=manual (highest), 1=copy-time, 2=background catch-up
}

// FaceQueue is a priority queue for face detection jobs, processed by a single
// background goroutine that owns the persistent ONNX sessions.
type FaceQueue struct {
	mu   sync.Mutex
	jobs []faceJob
	cond *sync.Cond
	done chan struct{} // closed on Shutdown

	// Snapshot for status queries.
	processingNow *int64 // photo ID currently being processed, or nil
	totalQueued   int
	totalDone     int
	totalErrors   int
}

// NewFaceQueue creates a FaceQueue and starts its background worker goroutine.
func NewFaceQueue(database *sql.DB) *FaceQueue {
	q := &FaceQueue{
		jobs: make([]faceJob, 0),
		done: make(chan struct{}),
	}
	q.cond = sync.NewCond(&q.mu)
	go q.worker(database)
	return q
}

// Enqueue adds a photo to the face detection queue at the given priority.
// Lower priority value = higher urgency (0=manual, 1=copy, 2=background).
// Returns true if the job was queued, false if it was already pending/done.
func (q *FaceQueue) Enqueue(photoID int64, priority int) bool {
	q.mu.Lock()
	defer q.mu.Unlock()

	// Check if already queued — bump priority if higher urgency.
	for i, j := range q.jobs {
		if j.PhotoID == photoID {
			if priority < j.Priority {
				q.jobs[i].Priority = priority
			}
			return false
		}
	}
	// Don't enqueue if currently being processed.
	if q.processingNow != nil && *q.processingNow == photoID {
		return false
	}

	q.jobs = append(q.jobs, faceJob{PhotoID: photoID, Priority: priority})
	q.totalQueued++
	q.cond.Signal()
	return true
}

// popJob returns the highest-priority job from the queue, blocking if empty.
// Returns false if the queue has been shut down.
func (q *FaceQueue) popJob() (faceJob, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()

	for len(q.jobs) == 0 {
		select {
		case <-q.done:
			return faceJob{}, false
		default:
		}
		q.cond.Wait()
		// Re-check after wakeup.
		select {
		case <-q.done:
			return faceJob{}, false
		default:
		}
	}

	// Find highest priority (lowest value); stable order within same priority.
	bestIdx := 0
	for i := 1; i < len(q.jobs); i++ {
		if q.jobs[i].Priority < q.jobs[bestIdx].Priority {
			bestIdx = i
		}
	}
	job := q.jobs[bestIdx]
	q.jobs = append(q.jobs[:bestIdx], q.jobs[bestIdx+1:]...)
	return job, true
}

// Status returns current queue state for API consumption.
func (q *FaceQueue) Status() (totalQueued, totalDone, totalErrors int, processingNow *int64) {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.totalQueued, q.totalDone, q.totalErrors, q.processingNow
}

// Shutdown signals the worker to stop and waits for it to finish.
func (q *FaceQueue) Shutdown() {
	close(q.done)
	q.cond.Broadcast()
}

func (q *FaceQueue) worker(database *sql.DB) {
	detector := GetDetector()
	embedder := GetEmbedder()

	// Open persistent ONNX sessions for the lifetime of the worker.
	if detector != nil {
		if err := detector.Open(); err != nil {
			slog.Error("recognition: face worker failed to open detector", "err", err)
			return
		}
		defer detector.Close()
	}
	if embedder != nil {
		if err := embedder.Open(); err != nil {
			slog.Error("recognition: face worker failed to open embedder", "err", err)
			return
		}
		defer embedder.Close()
	}

	slog.Info("recognition: face worker started",
		"detector_ok", detector != nil,
		"embedder_ok", embedder != nil)

	for {
		job, ok := q.popJob()
		if !ok {
			slog.Info("recognition: face worker shutting down")
			return
		}

		q.mu.Lock()
		q.processingNow = &job.PhotoID
		q.mu.Unlock()

		q.processFaceJob(database, job.PhotoID)

		q.mu.Lock()
		q.processingNow = nil
		q.totalDone++
		q.mu.Unlock()
	}
}

// processFaceJob runs face detection + embedding on a single photo and persists
// the results. Updates recognition_status on the photo row.
func (q *FaceQueue) processFaceJob(database *sql.DB, photoID int64) {
	// Mark as pending (clears any previous error).
	if _, err := db.SetPhotoRecognitionPending(database, photoID); err != nil {
		slog.Warn("face queue: set pending failed", "photo_id", photoID, "err", err)
		// Not a fatal error — continue anyway.
	}

	filepath, err := db.GetPhotoFilepath(database, photoID)
	if err != nil {
		slog.Warn("face queue: get filepath failed", "photo_id", photoID, "err", err)
		_ = db.SetPhotoRecognitionError(database, photoID, "filepath lookup: "+err.Error())
		return
	}

	// Check idempotency: skip if auto-faces already exist.
	has, err := db.HasAutoFacesForPhoto(database, photoID)
	if err != nil {
		slog.Warn("face queue: auto-faces check failed", "photo_id", photoID, "err", err)
	}
	if has {
		slog.Debug("face queue: skipping, auto-faces already exist", "photo_id", photoID)
		_ = db.SetPhotoRecognitionDone(database, photoID)
		return
	}

	img, err := decodeImageFile(filepath)
	if err != nil {
		slog.Warn("face queue: decode failed", "photo_id", photoID, "path", filepath, "err", err)
		_ = db.SetPhotoRecognitionError(database, photoID, "decode: "+err.Error())
		return
	}

	detector := GetDetector()
	if detector == nil {
		_ = db.SetPhotoRecognitionError(database, photoID, "detector not available")
		return
	}

	dets, err := detector.Detect(img)
	if err != nil {
		slog.Warn("face queue: detection failed", "photo_id", photoID, "err", err)
		_ = db.SetPhotoRecognitionError(database, photoID, "detection: "+err.Error())
		return
	}

	if len(dets) == 0 {
		slog.Debug("face queue: no faces found", "photo_id", photoID)
		_ = db.SetPhotoRecognitionDone(database, photoID)
		return
	}

	slog.Info("face queue: found faces", "photo_id", photoID, "count", len(dets))

	origW := img.Bounds().Dx()
	origH := img.Bounds().Dy()
	embedder := GetEmbedder()

	for _, det := range dets {
		bboxX := float64(det.X1) / float64(DetInputW)
		bboxY := float64(det.Y1) / float64(DetInputH)
		bboxW := float64(det.X2-det.X1) / float64(DetInputW)
		bboxH := float64(det.Y2-det.Y1) / float64(DetInputH)
		conf := float64(det.Score)

		face := db.Face{
			PhotoID:    photoID,
			BboxX:      &bboxX,
			BboxY:      &bboxY,
			BboxW:      &bboxW,
			BboxH:      &bboxH,
			Source:     "auto",
			Confidence: &conf,
			Verified:   false,
		}

		if embedder != nil {
			emb, embErr := embedder.Embed(img, det, origW, origH)
			if embErr != nil {
				slog.Warn("face queue: embedding failed", "photo_id", photoID, "err", embErr)
			} else {
				face.Embedding = EmbeddingToBytes(emb)
			}
		}

		if _, insertErr := db.InsertFace(database, face); insertErr != nil {
			slog.Warn("face queue: insert face failed", "photo_id", photoID, "err", insertErr)
		}
	}

	if err := db.SetPhotoRecognitionDone(database, photoID); err != nil {
		slog.Warn("face queue: set done failed", "photo_id", photoID, "err", err)
	}
}

// decodeImageFile decodes a JPEG or HEIC file at the given path.
// Mirrors scan.decodeImage but lives here to avoid a circular import.
func decodeImageFile(path string) (image.Image, error) {
	if isHEICPath(path) {
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		return heif.Decode(f)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	return img, err
}

// isHEICPath returns true if the path has a HEIC/HEIF extension.
func isHEICPath(path string) bool {
	lower := strings.ToLower(path)
	return strings.HasSuffix(lower, ".heic") || strings.HasSuffix(lower, ".heif")
}
