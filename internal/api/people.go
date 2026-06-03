package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/halleck/gallery/internal/db"
)

// ---- helpers ----

func faceToJSON(f *db.Face) map[string]any {
	return map[string]any{
		"id":          f.ID,
		"photo_id":    f.PhotoID,
		"person_id":   f.PersonID,
		"person_name": f.PersonName,
		"bbox_x":      f.BboxX,
		"bbox_y":      f.BboxY,
		"bbox_w":      f.BboxW,
		"bbox_h":      f.BboxH,
		"source":      f.Source,
		"confidence":  f.Confidence,
		"verified":    f.Verified,
		"created_at":  f.CreatedAt,
	}
}

func personToJSON(p *db.Person) map[string]any {
	return map[string]any{
		"id":            p.ID,
		"name":          p.Name,
		"notes":         p.Notes,
		"cover_face_id": p.CoverFaceID,
		"photo_count":   p.PhotoCount,
		"created_at":    p.CreatedAt,
	}
}

// ---- People CRUD ----

// GET /api/people
func (h *Handlers) handleListPeople(w http.ResponseWriter, r *http.Request) {
	if !h.stagingEnabled(w) {
		return
	}
	people, err := db.ListPeople(h.db)
	if err != nil {
		slog.Error("list people", "err", err)
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	out := make([]map[string]any, 0, len(people))
	for _, p := range people {
		out = append(out, personToJSON(p))
	}
	writeJSON(w, http.StatusOK, out)
}

// POST /api/people
func (h *Handlers) handleCreatePerson(w http.ResponseWriter, r *http.Request) {
	if !h.stagingEnabled(w) {
		return
	}
	var req struct {
		Name  string  `json:"name"`
		Notes *string `json:"notes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	id, err := db.InsertPerson(h.db, req.Name, req.Notes)
	if err != nil {
		slog.Error("insert person", "err", err)
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	p, err := db.GetPersonByID(h.db, id)
	if err != nil {
		slog.Error("get person after insert", "id", id, "err", err)
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	writeJSON(w, http.StatusCreated, personToJSON(p))
}

// GET /api/people/{id}
func (h *Handlers) handleGetPerson(w http.ResponseWriter, r *http.Request) {
	if !h.stagingEnabled(w) {
		return
	}
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	p, err := db.GetPersonByID(h.db, id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		slog.Error("get person", "id", id, "err", err)
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	writeJSON(w, http.StatusOK, personToJSON(p))
}

// PATCH /api/people/{id}
func (h *Handlers) handlePatchPerson(w http.ResponseWriter, r *http.Request) {
	if !h.stagingEnabled(w) {
		return
	}
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	// Check the person exists.
	if _, err = db.GetPersonByID(h.db, id); errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not found")
		return
	} else if err != nil {
		slog.Error("get person for patch", "id", id, "err", err)
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}

	var req struct {
		Name        *string `json:"name"`
		Notes       *string `json:"notes"`         // null or absent = clear; present = set
		CoverFaceID *int64  `json:"cover_face_id"` // 0 or absent = clear
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Build PersonUpdate using the same double-pointer convention:
	// outer nil = skip, outer non-nil inner nil = set NULL.
	u := db.PersonUpdate{Name: req.Name}
	if req.Notes != nil {
		if *req.Notes == "" {
			var nilStr *string
			u.Notes = &nilStr
		} else {
			u.Notes = &req.Notes
		}
	}
	if req.CoverFaceID != nil {
		if *req.CoverFaceID == 0 {
			var nilInt *int64
			u.CoverFaceID = &nilInt
		} else {
			u.CoverFaceID = &req.CoverFaceID
		}
	}

	if err := db.UpdatePerson(h.db, id, u); err != nil {
		slog.Error("update person", "id", id, "err", err)
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	p, err := db.GetPersonByID(h.db, id)
	if err != nil {
		slog.Error("get person after patch", "id", id, "err", err)
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	writeJSON(w, http.StatusOK, personToJSON(p))
}

// DELETE /api/people/{id}
func (h *Handlers) handleDeletePerson(w http.ResponseWriter, r *http.Request) {
	if !h.stagingEnabled(w) {
		return
	}
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if _, err = db.GetPersonByID(h.db, id); errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not found")
		return
	} else if err != nil {
		slog.Error("get person for delete", "id", id, "err", err)
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	if err := db.DeletePerson(h.db, id); err != nil {
		slog.Error("delete person", "id", id, "err", err)
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GET /api/people/{id}/photos
func (h *Handlers) handlePersonPhotos(w http.ResponseWriter, r *http.Request) {
	if !h.stagingEnabled(w) {
		return
	}
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	page := 1
	perPage := 50
	if v := r.URL.Query().Get("page"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			page = n
		}
	}
	if v := r.URL.Query().Get("per_page"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			perPage = n
		}
	}

	copies, total, err := db.ListLibraryCopiesForPerson(h.db, id, page, perPage)
	if err != nil {
		slog.Error("list person photos", "person_id", id, "err", err)
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	out := make([]map[string]any, 0, len(copies))
	for _, lc := range copies {
		out = append(out, libraryCopyJSON(lc))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"photos": out,
		"total":  total,
		"page":   page,
	})
}

// ---- Face tag CRUD ----

// GET /api/library/copies/{id}/faces
func (h *Handlers) handleListFaces(w http.ResponseWriter, r *http.Request) {
	if !h.stagingEnabled(w) {
		return
	}
	copyID, err := parseID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	faces, err := db.ListFacesForLibraryCopy(h.db, copyID)
	if err != nil {
		slog.Error("list faces", "copy_id", copyID, "err", err)
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	out := make([]map[string]any, 0, len(faces))
	for _, f := range faces {
		out = append(out, faceToJSON(f))
	}
	writeJSON(w, http.StatusOK, out)
}

// POST /api/library/copies/{id}/faces
func (h *Handlers) handleCreateFace(w http.ResponseWriter, r *http.Request) {
	if !h.stagingEnabled(w) {
		return
	}
	copyID, err := parseID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	photoID, err := db.GetPhotoIDForLibraryCopy(h.db, copyID)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "library copy not found")
		return
	}
	if err != nil {
		slog.Error("get photo id for copy", "copy_id", copyID, "err", err)
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}

	var req struct {
		PersonID *int64   `json:"person_id"`
		BboxX    *float64 `json:"bbox_x"`
		BboxY    *float64 `json:"bbox_y"`
		BboxW    *float64 `json:"bbox_w"`
		BboxH    *float64 `json:"bbox_h"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	faceID, err := db.InsertFace(h.db, db.Face{
		PhotoID:  photoID,
		PersonID: req.PersonID,
		BboxX:    req.BboxX,
		BboxY:    req.BboxY,
		BboxW:    req.BboxW,
		BboxH:    req.BboxH,
		Source:   "manual",
		Verified: true,
	})
	if err != nil {
		slog.Error("insert face", "copy_id", copyID, "err", err)
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	f, err := db.GetFaceByID(h.db, faceID)
	if err != nil {
		slog.Error("get face after insert", "id", faceID, "err", err)
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	writeJSON(w, http.StatusCreated, faceToJSON(f))
}

// PATCH /api/faces/{id}
func (h *Handlers) handlePatchFace(w http.ResponseWriter, r *http.Request) {
	if !h.stagingEnabled(w) {
		return
	}
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if _, err = db.GetFaceByID(h.db, id); errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not found")
		return
	} else if err != nil {
		slog.Error("get face for patch", "id", id, "err", err)
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}

	var req struct {
		PersonID *int64   `json:"person_id"` // 0 = clear to NULL
		BboxX    *float64 `json:"bbox_x"`
		BboxY    *float64 `json:"bbox_y"`
		BboxW    *float64 `json:"bbox_w"`
		BboxH    *float64 `json:"bbox_h"`
		Verified *bool    `json:"verified"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	u := db.FaceUpdate{
		BboxX:    toDoublePtrFloat(req.BboxX),
		BboxY:    toDoublePtrFloat(req.BboxY),
		BboxW:    toDoublePtrFloat(req.BboxW),
		BboxH:    toDoublePtrFloat(req.BboxH),
		Verified: req.Verified,
	}
	if req.PersonID != nil {
		if *req.PersonID == 0 {
			var nilInt *int64
			u.PersonID = &nilInt
		} else {
			u.PersonID = &req.PersonID
		}
	}

	if err := db.UpdateFace(h.db, id, u); err != nil {
		slog.Error("update face", "id", id, "err", err)
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	f, err := db.GetFaceByID(h.db, id)
	if err != nil {
		slog.Error("get face after patch", "id", id, "err", err)
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	writeJSON(w, http.StatusOK, faceToJSON(f))
}

// DELETE /api/faces/{id}
func (h *Handlers) handleDeleteFace(w http.ResponseWriter, r *http.Request) {
	if !h.stagingEnabled(w) {
		return
	}
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if _, err = db.GetFaceByID(h.db, id); errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not found")
		return
	} else if err != nil {
		slog.Error("get face for delete", "id", id, "err", err)
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	if err := db.DeleteFace(h.db, id); err != nil {
		slog.Error("delete face", "id", id, "err", err)
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- helpers ----

// toDoublePtrFloat converts *float64 to **float64 using the skip/clear convention.
// nil = skip; non-nil = set (even to 0.0).
func toDoublePtrFloat(p *float64) **float64 {
	if p == nil {
		return nil
	}
	return &p
}

// ---- Recognition pipeline (Phase B/C) ----

// recognitionCheck writes the appropriate error response when the recognition
// pipeline is not available, and returns false to signal the handler should stop.
func (h *Handlers) recognitionCheck(w http.ResponseWriter) bool {
	if !h.recognition.Enabled {
		writeError(w, http.StatusNotImplemented, "face recognition not enabled in config")
		return false
	}
	if !h.recognition.Available {
		writeError(w, http.StatusServiceUnavailable, h.recognition.Reason)
		return false
	}
	return true
}

// GET /api/faces/unidentified
func (h *Handlers) handleUnidentifiedFaces(w http.ResponseWriter, r *http.Request) {
	if !h.stagingEnabled(w) {
		return
	}
	if !h.recognitionCheck(w) {
		return
	}
	page := 1
	perPage := 50
	if v := r.URL.Query().Get("page"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			page = n
		}
	}
	if v := r.URL.Query().Get("per_page"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			perPage = n
		}
	}

	faces, total, err := db.ListUnidentifiedFaces(h.db, page, perPage)
	if err != nil {
		slog.Error("list unidentified faces", "err", err)
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	out := make([]map[string]any, 0, len(faces))
	for _, f := range faces {
		out = append(out, faceToJSON(f))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"faces": out,
		"total": total,
		"page":  page,
	})
}

// GET /api/faces/suggestions
func (h *Handlers) handleSuggestions(w http.ResponseWriter, r *http.Request) {
	if !h.stagingEnabled(w) {
		return
	}
	if !h.recognitionCheck(w) {
		return
	}
	faces, err := db.ListUnverifiedSuggestions(h.db)
	if err != nil {
		slog.Error("list suggestions", "err", err)
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	out := make([]map[string]any, 0, len(faces))
	for _, f := range faces {
		out = append(out, faceToJSON(f))
	}
	writeJSON(w, http.StatusOK, out)
}

// POST /api/faces/{id}/confirm
// Sets verified=1 on the current person_id assignment.
func (h *Handlers) handleConfirmFace(w http.ResponseWriter, r *http.Request) {
	if !h.stagingEnabled(w) {
		return
	}
	if !h.recognitionCheck(w) {
		return
	}
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	face, err := db.GetFaceByID(h.db, id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		slog.Error("get face for confirm", "id", id, "err", err)
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	if face.PersonID == nil {
		writeError(w, http.StatusConflict, "face has no person assigned; assign a person before confirming")
		return
	}
	verified := true
	if err := db.UpdateFace(h.db, id, db.FaceUpdate{Verified: &verified}); err != nil {
		slog.Error("confirm face", "id", id, "err", err)
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	face, _ = db.GetFaceByID(h.db, id)
	writeJSON(w, http.StatusOK, faceToJSON(face))
}

// POST /api/faces/{id}/reject
// Clears person_id and resets verified=0, returning the face to the unidentified pool.
func (h *Handlers) handleRejectFace(w http.ResponseWriter, r *http.Request) {
	if !h.stagingEnabled(w) {
		return
	}
	if !h.recognitionCheck(w) {
		return
	}
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if _, err := db.GetFaceByID(h.db, id); errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not found")
		return
	} else if err != nil {
		slog.Error("get face for reject", "id", id, "err", err)
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	var nilPersonID *int64
	verified := false
	if err := db.UpdateFace(h.db, id, db.FaceUpdate{PersonID: &nilPersonID, Verified: &verified}); err != nil {
		slog.Error("reject face", "id", id, "err", err)
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// POST /api/faces/cluster
// Triggers a manual re-cluster of unidentified face embeddings (Phase C stub).
func (h *Handlers) handleTriggerCluster(w http.ResponseWriter, r *http.Request) {
	if !h.stagingEnabled(w) {
		return
	}
	if !h.recognitionCheck(w) {
		return
	}
	// Phase C: full clustering will be implemented here.
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "clusters": 0})
}
