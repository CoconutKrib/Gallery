package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"strconv"
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

	// Face recognition post-copy: suggest identities + cluster unidentified faces.
	runRecognitionPostScan(h.db, h.cfg)
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

	f := db.LibraryCopyFilter{
		Source:  r.URL.Query().Get("source"),
		Tag:     r.URL.Query().Get("tag"),
		Keyword: r.URL.Query().Get("q"),
	}
	if v := r.URL.Query().Get("has_date_override"); v == "true" {
		t := true
		f.HasDateOverride = &t
	} else if v == "false" {
		t := false
		f.HasDateOverride = &t
	}
	if v := r.URL.Query().Get("true_date_unknown"); v == "true" {
		t := true
		f.TrueDateUnknown = &t
	} else if v == "false" {
		t := false
		f.TrueDateUnknown = &t
	}
	if v := r.URL.Query().Get("event_id"); v != "" {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil {
			f.EventID = &id
		}
	}
	if v := r.URL.Query().Get("page"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			f.Page = n
		}
	}
	if v := r.URL.Query().Get("per_page"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			f.PerPage = n
		}
	}

	copies, total, err := db.ListLibraryCopiesFiltered(h.db, f)
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
		"total":  total,
	})
}

// PATCH /api/library/copies/{id}
func (h *Handlers) handleLibraryCopyPatch(w http.ResponseWriter, r *http.Request) {
	if !h.stagingEnabled(w) {
		return
	}
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	lc, err := db.GetLibraryCopyByID(h.db, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "library copy not found")
		} else {
			writeError(w, http.StatusInternalServerError, "db error")
		}
		return
	}

	// Decode partial update from JSON body.
	var req struct {
		Title           *string   `json:"title"`
		Description     *string   `json:"description"`
		Tags            *[]string `json:"tags"`
		EventID         *int64    `json:"event_id"`
		OverrideDate    *string   `json:"override_date"`
		TrueDateUnknown *bool     `json:"true_date_unknown"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err.Error() != "EOF" {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Remember whether date-related fields changed so we can trigger re-org.
	oldOverrideDate := lc.OverrideDate
	oldTrueDateUnknown := lc.TrueDateUnknown

	// Build the DB update.
	update := db.LibraryCopyUpdate{
		Title:       toDoublePtr(req.Title),
		Description: toDoublePtr(req.Description),
		Tags:        req.Tags,
		EventID:     toDoublePtrInt(req.EventID),
	}
	if req.OverrideDate != nil {
		var od *string
		if *req.OverrideDate != "" {
			if _, err := time.Parse(time.RFC3339, *req.OverrideDate); err != nil {
				writeError(w, http.StatusBadRequest, "invalid override_date: must be RFC3339")
				return
			}
			od = req.OverrideDate
		}
		update.OverrideDate = &od
		lc.OverrideDate = od
	}
	if req.TrueDateUnknown != nil {
		update.TrueDateUnknown = req.TrueDateUnknown
		lc.TrueDateUnknown = *req.TrueDateUnknown
	}

	if err := db.UpdateLibraryCopy(h.db, id, update); err != nil {
		writeError(w, http.StatusInternalServerError, "db update failed")
		return
	}

	// Re-organisation: if date-related fields changed, potentially move the file.
	dateMayHaveChanged := ptrStringChanged(oldOverrideDate, lc.OverrideDate) ||
		(req.TrueDateUnknown != nil && lc.TrueDateUnknown != oldTrueDateUnknown)
	if dateMayHaveChanged {
		// Re-fetch the updated copy (so EventID etc. are current).
		if updated, err := db.GetLibraryCopyByID(h.db, id); err == nil {
			lc = updated
		}
		photo, err := db.GetPhotoBySHA256(h.db, lc.PhotoSHA256)
		if err == nil {
			if _, moveErr := library.MovePhoto(h.db, lc, photo, h.cfg.InternalLibrary.Path); moveErr != nil {
				slog.Warn("library: re-org move failed", "id", id, "err", moveErr)
				writeError(w, http.StatusInternalServerError, "file move failed: "+moveErr.Error())
				return
			}
		}
	}

	// Return updated record.
	final, err := db.GetLibraryCopyByID(h.db, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	writeJSON(w, http.StatusOK, libraryCopyJSON(final))
}

// DELETE /api/library/copies/{id}
func (h *Handlers) handleLibraryCopyDelete(w http.ResponseWriter, r *http.Request) {
	if !h.stagingEnabled(w) {
		return
	}
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	sha256, absPath, err := db.DeleteLibraryPhotoByID(h.db, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "library copy not found")
		} else {
			slog.Error("library: delete failed", "id", id, "err", err)
			writeError(w, http.StatusInternalServerError, "db error")
		}
		return
	}

	// Delete the physical file. If already absent, log a warning but don't fail.
	if removeErr := removeFile(absPath, h.cfg.InternalLibrary.Path); removeErr != nil {
		slog.Warn("library: physical file delete failed", "sha256", sha256[:8], "path", absPath, "err", removeErr)
		// DB records are already gone — log and continue.
	}

	slog.Info("library: removed photo", "sha256", sha256[:8], "path", absPath)
	w.WriteHeader(http.StatusNoContent)
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
		"title":             c.Title,
		"description":       c.Description,
		"override_date":     c.OverrideDate,
		"event_id":          c.EventID,
	}
}

// removeFile deletes absPath if it exists and is within libraryRoot.
func removeFile(absPath, libraryRoot string) error {
	if !pathIsWithinRoots(absPath, []string{libraryRoot}) {
		return nil // safety: refuse to delete outside library root
	}
	if err := os.Remove(absPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// toDoublePtr converts *string to **string for LibraryCopyUpdate.
func toDoublePtr(p *string) **string {
	if p == nil {
		return nil
	}
	return &p
}

// toDoublePtrInt converts *int64 to **int64 for LibraryCopyUpdate.
func toDoublePtrInt(p *int64) **int64 {
	if p == nil {
		return nil
	}
	return &p
}

// ptrStringChanged returns true if two *string pointers hold different values.
func ptrStringChanged(a, b *string) bool {
	if a == nil && b == nil {
		return false
	}
	if a == nil || b == nil {
		return true
	}
	return *a != *b
}
