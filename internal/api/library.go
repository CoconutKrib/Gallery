package api

import (
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/halleck/gallery/internal/cluster"
	"github.com/halleck/gallery/internal/db"
	"github.com/halleck/gallery/internal/library"
)

// copyJobStatus tracks the state of a bulk copy operation.
type copyJobStatus struct {
	mu        sync.Mutex
	running   bool
	startedAt *time.Time
	copied    int
	skipped   int
	errors    int
	lastError string
}

var globalCopyJob = &copyJobStatus{}

// POST /api/library/copy  — bulk copy all approved entries
func (h *Handlers) handleLibraryCopyAll(w http.ResponseWriter, r *http.Request) {
	if !h.stagingEnabled(w) {
		return
	}
	globalCopyJob.mu.Lock()
	if globalCopyJob.running {
		globalCopyJob.mu.Unlock()
		writeError(w, http.StatusConflict, "copy job already running")
		return
	}
	globalCopyJob.running = true
	globalCopyJob.copied = 0
	globalCopyJob.skipped = 0
	globalCopyJob.errors = 0
	globalCopyJob.lastError = ""
	now := time.Now()
	globalCopyJob.startedAt = &now
	globalCopyJob.mu.Unlock()

	go h.runBulkCopy()
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "copy started"})
}

func (h *Handlers) runBulkCopy() {
	defer func() {
		globalCopyJob.mu.Lock()
		globalCopyJob.running = false
		globalCopyJob.mu.Unlock()
	}()

	entries, err := db.ListApprovedStagingEntries(h.db)
	if err != nil {
		slog.Error("library: list approved entries failed", "err", err)
		globalCopyJob.mu.Lock()
		globalCopyJob.lastError = err.Error()
		globalCopyJob.mu.Unlock()
		return
	}

	for _, entry := range entries {
		if relPath, err := h.copyOneEntry(entry); err != nil {
			slog.Warn("library: copy failed", "sha256", entry.PhotoSHA256, "err", err)
			globalCopyJob.mu.Lock()
			globalCopyJob.errors++
			globalCopyJob.lastError = err.Error()
			globalCopyJob.mu.Unlock()
		} else if relPath == "" {
			globalCopyJob.mu.Lock()
			globalCopyJob.skipped++
			globalCopyJob.mu.Unlock()
		} else {
			globalCopyJob.mu.Lock()
			globalCopyJob.copied++
			globalCopyJob.mu.Unlock()
		}
	}

	// Trigger re-clustering after bulk copy.
	if err := cluster.Run(h.db, h.cfg.EventGapDays, h.cfg.EventGeoKm); err != nil {
		slog.Warn("library: cluster run after copy failed", "err", err)
	}
}

// POST /api/library/copy/{staging_id} — copy a single approved entry
func (h *Handlers) handleLibraryCopyOne(w http.ResponseWriter, r *http.Request) {
	if !h.stagingEnabled(w) {
		return
	}
	id, err := parseID(r.PathValue("staging_id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid staging_id")
		return
	}
	entry, err := db.GetStagingEntryByID(h.db, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "staging entry not found")
		} else {
			writeError(w, http.StatusInternalServerError, "db error")
		}
		return
	}
	if entry.State != db.StagingStateApproved {
		writeError(w, http.StatusConflict, "staging entry is not in approved state")
		return
	}
	relPath, err := h.copyOneEntry(entry)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"sha256":        entry.PhotoSHA256,
		"relative_path": relPath,
		"skipped":       relPath == "",
	})
}

func (h *Handlers) copyOneEntry(entry *db.StagingEntry) (string, error) {
	photo, err := db.GetPhotoBySHA256(h.db, entry.PhotoSHA256)
	if err != nil {
		return "", err
	}
	return library.CopyPhoto(h.db, entry, photo, h.cfg.InternalLibrary.Path)
}

// GET /api/library/status
func (h *Handlers) handleLibraryStatus(w http.ResponseWriter, r *http.Request) {
	if !h.stagingEnabled(w) {
		return
	}
	globalCopyJob.mu.Lock()
	resp := map[string]any{
		"running":    globalCopyJob.running,
		"started_at": globalCopyJob.startedAt,
		"copied":     globalCopyJob.copied,
		"skipped":    globalCopyJob.skipped,
		"errors":     globalCopyJob.errors,
		"last_error": globalCopyJob.lastError,
	}
	globalCopyJob.mu.Unlock()
	writeJSON(w, http.StatusOK, resp)
}

// GET /api/library/photos
func (h *Handlers) handleLibraryPhotos(w http.ResponseWriter, r *http.Request) {
	if !h.stagingEnabled(w) {
		return
	}
	copies, err := db.ListLibraryCopies(h.db)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	if copies == nil {
		copies = []*db.LibraryCopy{}
	}
	out := make([]map[string]any, 0, len(copies))
	for _, c := range copies {
		out = append(out, libraryCopyJSON(c))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"photos": out,
		"total":  len(out),
	})
}

// GET /api/library/tree
func (h *Handlers) handleLibraryTree(w http.ResponseWriter, r *http.Request) {
	if !h.stagingEnabled(w) {
		return
	}
	tree, err := db.GetLibraryTree(h.db)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	if tree == nil {
		tree = []db.LibraryTreeNode{}
	}
	writeJSON(w, http.StatusOK, tree)
}

func libraryCopyJSON(c *db.LibraryCopy) map[string]any {
	return map[string]any{
		"id":                c.ID,
		"photo_sha256":      c.PhotoSHA256,
		"relative_path":     c.RelativePath,
		"absolute_path":     c.AbsolutePath,
		"true_date_unknown": c.TrueDateUnknown,
		"tags":              c.Tags,
		"copied_at":         c.CopiedAt,
	}
}
