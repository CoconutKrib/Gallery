package api

import (
	"database/sql"
	"net/http"
	"strconv"

	"github.com/halleck/gallery/internal/db"
)

func (h *Handlers) handleListEvents(w http.ResponseWriter, r *http.Request) {
	events, err := db.GetAllEvents(h.db)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}

	items := make([]map[string]any, 0, len(events))
	for _, e := range events {
		items = append(items, eventSummary(e))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"total": len(events),
		"items": items,
	})
}

func (h *Handlers) handleEventDetail(w http.ResponseWriter, r *http.Request) {
	rawID := r.PathValue("id")
	id, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	event, err := db.GetEventByID(h.db, id)
	if err == sql.ErrNoRows {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}

	photos, err := db.GetPhotosForEvent(h.db, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}

	items := make([]map[string]any, 0, len(photos))
	for _, p := range photos {
		items = append(items, photoSummary(p))
	}

	out := eventSummary(*event)
	out["photos"] = items
	writeJSON(w, http.StatusOK, out)
}

func eventSummary(e db.Event) map[string]any {
	return map[string]any{
		"id":           e.ID,
		"label":        e.Label,
		"started_at":   e.StartedAt,
		"ended_at":     e.EndedAt,
		"centroid_lat": e.CentroidLat,
		"centroid_lon": e.CentroidLon,
		"photo_count":  e.PhotoCount,
	}
}
