package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/halleck/gallery/internal/db"
)

// stagingEnabled returns 409 with a clear message if internal library is not enabled.
func (h *Handlers) stagingEnabled(w http.ResponseWriter) bool {
	if !h.cfg.InternalLibrary.Enabled {
		writeError(w, http.StatusConflict, "internal library is not enabled")
		return false
	}
	return true
}

// GET /api/staging[?state=staged|approved|rejected]
func (h *Handlers) handleListStaging(w http.ResponseWriter, r *http.Request) {
	if !h.stagingEnabled(w) {
		return
	}
	stateParam := db.StagingState(r.URL.Query().Get("state"))
	entries, err := db.ListStagingEntries(h.db, stateParam)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	if entries == nil {
		entries = []*db.StagingEntry{}
	}
	out := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		out = append(out, stagingEntryJSON(e))
	}
	writeJSON(w, http.StatusOK, out)
}

// POST /api/staging  body: {"sha256":"..."}
func (h *Handlers) handleAddToStaging(w http.ResponseWriter, r *http.Request) {
	if !h.stagingEnabled(w) {
		return
	}
	var req struct {
		SHA256 string `json:"sha256"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.SHA256 == "" {
		writeError(w, http.StatusBadRequest, "sha256 required")
		return
	}
	// Validate the photo exists.
	if _, err := db.GetPhotoBySHA256(h.db, req.SHA256); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "photo not found")
		} else {
			writeError(w, http.StatusInternalServerError, "db error")
		}
		return
	}
	entry, err := db.InsertStagingEntry(h.db, req.SHA256)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint") {
			writeError(w, http.StatusConflict, "photo is already in staging queue")
			return
		}
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	writeJSON(w, http.StatusCreated, stagingEntryJSON(entry))
}

// GET /api/staging/{id}
func (h *Handlers) handleGetStagingEntry(w http.ResponseWriter, r *http.Request) {
	if !h.stagingEnabled(w) {
		return
	}
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
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
	writeJSON(w, http.StatusOK, stagingEntryJSON(entry))
}

// PATCH /api/staging/{id}
func (h *Handlers) handleUpdateStagingEntry(w http.ResponseWriter, r *http.Request) {
	if !h.stagingEnabled(w) {
		return
	}
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	var req struct {
		Title           *string   `json:"title"`
		Description     *string   `json:"description"`
		OverrideDate    *string   `json:"override_date"`
		OverrideLat     *float64  `json:"override_lat"`
		OverrideLon     *float64  `json:"override_lon"`
		EventID         *int64    `json:"event_id"`
		Tags            *[]string `json:"tags"`
		TrueDateUnknown *bool     `json:"true_date_unknown"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	u := db.StagingAnnotationUpdate{
		Title:           req.Title,
		Description:     req.Description,
		OverrideDate:    req.OverrideDate,
		OverrideLat:     req.OverrideLat,
		OverrideLon:     req.OverrideLon,
		EventID:         req.EventID,
		Tags:            req.Tags,
		TrueDateUnknown: req.TrueDateUnknown,
	}
	if err := db.UpdateStagingEntry(h.db, id, u); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "staging entry not found")
		} else {
			writeError(w, http.StatusInternalServerError, "db error")
		}
		return
	}
	entry, _ := db.GetStagingEntryByID(h.db, id)
	writeJSON(w, http.StatusOK, stagingEntryJSON(entry))
}

// POST /api/staging/{id}/approve
func (h *Handlers) handleApproveStaging(w http.ResponseWriter, r *http.Request) {
	h.transitionStagingState(w, r, db.StagingStateApproved)
}

// POST /api/staging/{id}/reject
func (h *Handlers) handleRejectStaging(w http.ResponseWriter, r *http.Request) {
	h.transitionStagingState(w, r, db.StagingStateRejected)
}

func (h *Handlers) transitionStagingState(w http.ResponseWriter, r *http.Request, state db.StagingState) {
	if !h.stagingEnabled(w) {
		return
	}
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := db.SetStagingState(h.db, id, state); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "staging entry not found")
		} else {
			writeError(w, http.StatusInternalServerError, "db error")
		}
		return
	}
	entry, _ := db.GetStagingEntryByID(h.db, id)
	writeJSON(w, http.StatusOK, stagingEntryJSON(entry))
}

// DELETE /api/staging/{id}
func (h *Handlers) handleDeleteStagingEntry(w http.ResponseWriter, r *http.Request) {
	if !h.stagingEnabled(w) {
		return
	}
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := db.DeleteStagingEntry(h.db, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "staging entry not found")
		} else {
			writeError(w, http.StatusInternalServerError, "db error")
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// stagingEntryJSON converts a StagingEntry to a map for JSON serialisation.
func stagingEntryJSON(e *db.StagingEntry) map[string]any {
	return map[string]any{
		"id":                e.ID,
		"photo_sha256":      e.PhotoSHA256,
		"title":             e.Title,
		"description":       e.Description,
		"override_date":     e.OverrideDate,
		"override_lat":      e.OverrideLat,
		"override_lon":      e.OverrideLon,
		"event_id":          e.EventID,
		"tags":              e.Tags,
		"true_date_unknown": e.TrueDateUnknown,
		"state":             string(e.State),
		"created_at":        e.CreatedAt,
		"updated_at":        e.UpdatedAt,
	}
}
