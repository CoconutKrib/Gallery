package api

import (
	"net/http"

	"github.com/halleck/gallery/internal/db"
)

func (h *Handlers) handleDedupReport(w http.ResponseWriter, r *http.Request) {
	summaries, err := db.GetLibraryDedupSummaries(h.db)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}

	overlaps, err := db.GetCrossPathOverlap(h.db)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"libraries": summaries,
		"overlaps":  overlaps,
	})
}

func (h *Handlers) handleDedupSubtree(w http.ResponseWriter, r *http.Request) {
	prefix := r.URL.Query().Get("prefix")
	if prefix == "" {
		writeError(w, http.StatusBadRequest, "prefix query param required")
		return
	}

	entries, err := db.GetSubtreeDedupEntries(h.db, prefix)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}

	dupeCount := 0
	for _, e := range entries {
		if e.DuplicateCount > 0 {
			dupeCount++
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"prefix":     prefix,
		"total":      len(entries),
		"with_dupes": dupeCount,
		"unique":     len(entries) - dupeCount,
		"entries":    entries,
	})
}
