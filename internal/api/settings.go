package api

import (
	"encoding/json"
	"net/http"

	"github.com/halleck/gallery/internal/auth"
	"github.com/halleck/gallery/internal/config"
	"github.com/halleck/gallery/internal/db"
	"github.com/halleck/gallery/internal/recognition"
)

func (h *Handlers) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	// Return config without sensitive fields (password_hash, session_secret omitted).
	writeJSON(w, http.StatusOK, map[string]any{
		"scan_paths":       h.cfg.LibraryPaths,
		"library_paths":    h.cfg.LibraryPaths,
		"camera_whitelist": h.cfg.CameraWhitelist,
		"filename_filters": h.cfg.FilenameFilters,
		"auth": map[string]any{
			"enabled": h.cfg.Auth.Enabled,
		},
		"db_path":           h.cfg.DBPath,
		"cache_dir":         h.cfg.CacheDir,
		"log_file":          h.cfg.LogFile,
		"log_level":         h.cfg.LogLevel,
		"scan_workers":      h.cfg.ScanWorkers,
		"event_gap_days":    h.cfg.EventGapDays,
		"event_geo_km":      h.cfg.EventGeoKm,
		"session_ttl_hours": h.cfg.SessionTTLHours,
		"internal_library":  h.cfg.InternalLibrary,
		"dropzone":          h.cfg.Dropzone,
		"face_recognition":  h.cfg.FaceRecognition,
	})
}

type settingsUpdateRequest struct {
	ScanPaths       *[]config.LibraryPath         `json:"scan_paths"`
	LibraryPaths    *[]config.LibraryPath         `json:"library_paths"`
	CameraWhitelist *[]config.CameraEntry         `json:"camera_whitelist"`
	FilenameFilters *config.FilenameFilters       `json:"filename_filters"`
	ScanWorkers     *int                          `json:"scan_workers"`
	EventGapDays    *int                          `json:"event_gap_days"`
	EventGeoKm      *float64                      `json:"event_geo_km"`
	SessionTTLHours *int                          `json:"session_ttl_hours"`
	LogFile         *string                       `json:"log_file"`
	LogLevel        *string                       `json:"log_level"`
	InternalLibrary *config.InternalLibraryConfig `json:"internal_library"`
	Dropzone        *config.DropzoneConfig        `json:"dropzone"`
	FaceRecognition *config.FaceRecognitionConfig `json:"face_recognition"`
	// Auth password change.
	NewPassword *string `json:"new_password"`
	AuthEnabled *bool   `json:"auth_enabled"`
}

func (h *Handlers) handlePostSettings(w http.ResponseWriter, r *http.Request) {
	var req settingsUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	newCfg := *h.cfg

	if req.ScanPaths != nil {
		newCfg.LibraryPaths = *req.ScanPaths
	} else if req.LibraryPaths != nil {
		newCfg.LibraryPaths = *req.LibraryPaths
	}
	if req.CameraWhitelist != nil {
		newCfg.CameraWhitelist = *req.CameraWhitelist
	}
	if req.FilenameFilters != nil {
		newCfg.FilenameFilters = *req.FilenameFilters
	}
	if req.ScanWorkers != nil && *req.ScanWorkers > 0 {
		newCfg.ScanWorkers = *req.ScanWorkers
	}
	if req.EventGapDays != nil && *req.EventGapDays > 0 {
		newCfg.EventGapDays = *req.EventGapDays
	}
	if req.EventGeoKm != nil && *req.EventGeoKm > 0 {
		newCfg.EventGeoKm = *req.EventGeoKm
	}
	if req.SessionTTLHours != nil && *req.SessionTTLHours > 0 {
		newCfg.SessionTTLHours = *req.SessionTTLHours
	}
	if req.LogFile != nil {
		newCfg.LogFile = *req.LogFile
	}
	if req.LogLevel != nil {
		// Validate log level against allowed values.
		validLevels := map[string]bool{"debug": true, "info": true, "warn": true, "error": true}
		if !validLevels[*req.LogLevel] {
			writeError(w, http.StatusBadRequest, "invalid log_level: must be one of debug, info, warn, error")
			return
		}
		newCfg.LogLevel = *req.LogLevel
	}
	if req.InternalLibrary != nil {
		newCfg.InternalLibrary = *req.InternalLibrary
	}
	if req.Dropzone != nil {
		newCfg.Dropzone = *req.Dropzone
	}
	if req.FaceRecognition != nil {
		newCfg.FaceRecognition = *req.FaceRecognition
	}
	if req.AuthEnabled != nil {
		newCfg.Auth.Enabled = *req.AuthEnabled
	}
	if req.NewPassword != nil && *req.NewPassword != "" {
		hash, err := auth.HashPassword(*req.NewPassword)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "password hashing failed")
			return
		}
		newCfg.Auth.PasswordHash = hash
	}

	if err := config.Validate(&newCfg); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if err := config.Save(h.cfgPath, &newCfg); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save config")
		return
	}
	*h.cfg = newCfg
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handlers) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !h.cfg.Auth.Enabled {
		writeError(w, http.StatusBadRequest, "auth not enabled")
		return
	}
	if h.sessions.Login(w, r, h.cfg.Auth.PasswordHash, req.Password) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	} else {
		writeError(w, http.StatusUnauthorized, "invalid password")
	}
}

func (h *Handlers) handleLogout(w http.ResponseWriter, r *http.Request) {
	h.sessions.Logout(w, r)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handlers) handleIssues(w http.ResponseWriter, r *http.Request) {
	page, perPage := 1, 50
	if v := r.URL.Query().Get("page"); v != "" {
		if n, err := parseInt(v); err == nil && n > 0 {
			page = n
		}
	}
	if v := r.URL.Query().Get("per_page"); v != "" {
		if n, err := parseInt(v); err == nil && n > 0 {
			perPage = n
		}
	}
	// Issues = photos with any flag other than just missing_gps treated as informational.
	// For now return all photos that have any flags.
	from := 0
	to := 0
	_ = from
	_ = to

	photos, err := h.db.Query(`
		SELECT COUNT(*) OVER() as cnt,
		       sha256, filename, filepath, captured_at, camera_make, camera_model, flags, ingested_at
		FROM photos WHERE flags != '[]' AND flags != 'null' AND flags IS NOT NULL AND flags != ''
		ORDER BY ingested_at DESC
		LIMIT ? OFFSET ?`, perPage, (page-1)*perPage)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	defer photos.Close()

	type issue struct {
		SHA256      string   `json:"sha256"`
		Filename    string   `json:"filename"`
		Filepath    string   `json:"filepath"`
		CapturedAt  any      `json:"captured_at"`
		CameraMake  string   `json:"camera_make"`
		CameraModel string   `json:"camera_model"`
		Flags       []string `json:"flags"`
		IngestedAt  string   `json:"ingested_at"`
	}
	var issues []issue
	var totalCount int
	for photos.Next() {
		var iss issue
		var flagsJSON string
		var capturedAt any
		if err := photos.Scan(&totalCount,
			&iss.SHA256, &iss.Filename, &iss.Filepath, &capturedAt,
			&iss.CameraMake, &iss.CameraModel, &flagsJSON, &iss.IngestedAt,
		); err != nil {
			continue
		}
		iss.CapturedAt = capturedAt
		json.Unmarshal([]byte(flagsJSON), &iss.Flags) //nolint:errcheck
		if iss.Flags == nil {
			iss.Flags = []string{}
		}
		issues = append(issues, iss)
	}
	if issues == nil {
		issues = []issue{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"total":    totalCount,
		"page":     page,
		"per_page": perPage,
		"items":    issues,
	})
}

// GET /api/recognition/status
// Always returns 200. Returns the runtime status of the face recognition pipeline.
func (h *Handlers) handleRecognitionStatus(w http.ResponseWriter, r *http.Request) {
	s := h.recognition
	var ep interface{} = nil
	if s.ExecutionProvider != "" {
		ep = s.ExecutionProvider
	}
	var reason interface{} = nil
	if s.Reason != "" {
		reason = s.Reason
	}

	// Include queue stats when available.
	queued, done, errors, processingNow := recognition.QueueStatus()

	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":            s.Enabled,
		"available":          s.Available,
		"execution_provider": ep,
		"reason":             reason,
		"queue_queued":       queued,
		"queue_done":         done,
		"queue_errors":       errors,
		"queue_processing":   processingNow,
	})
}

// GET /api/recognition/queue
// Returns the current face detection queue state.
func (h *Handlers) handleRecognitionQueue(w http.ResponseWriter, r *http.Request) {
	queued, done, errors, processingNow := recognition.QueueStatus()
	writeJSON(w, http.StatusOK, map[string]any{
		"total_queued":   queued,
		"total_done":     done,
		"total_errors":   errors,
		"processing_now": processingNow,
	})
}

// POST /api/recognition/reprocess-all
// Enqueues all photos that need face detection (stale version, errored, or never
// attempted) in captured_at order. Returns the count queued.
func (h *Handlers) handleReprocessAll(w http.ResponseWriter, r *http.Request) {
	if !h.stagingEnabled(w) {
		return
	}
	if !h.recognitionCheck(w) {
		return
	}

	// Count total eligible for reporting.
	total, err := db.CountPhotosNeedingRecognition(h.db)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}

	// Enqueue in batches to avoid a huge in-memory queue. Each batch of 500
	// is processed by the worker; as it drains we could enqueue more. For now,
	// enqueue the first 1000 at background priority.
	ids, err := db.ListPhotosNeedingRecognition(h.db, 1000)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}

	queued := 0
	for _, id := range ids {
		if recognition.EnqueueFaceDetection(id, 2) {
			queued++
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"total_eligible": total,
		"queued":         queued,
		"note":           "Photos are processed in captured_at order. Re-run this endpoint to enqueue the next batch.",
	})
}
