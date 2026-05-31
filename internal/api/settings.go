package api

import (
	"encoding/json"
	"net/http"

	"github.com/halleck/gallery/internal/auth"
	"github.com/halleck/gallery/internal/config"
)

func (h *Handlers) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	// Return config without sensitive fields.
	writeJSON(w, http.StatusOK, map[string]any{
		"library_paths":    h.cfg.LibraryPaths,
		"camera_whitelist": h.cfg.CameraWhitelist,
		"filename_filters": h.cfg.FilenameFilters,
		"auth": map[string]any{
			"enabled": h.cfg.Auth.Enabled,
		},
		"db_path":           h.cfg.DBPath,
		"cache_dir":         h.cfg.CacheDir,
		"scan_workers":      h.cfg.ScanWorkers,
		"event_gap_days":    h.cfg.EventGapDays,
		"event_geo_km":      h.cfg.EventGeoKm,
		"session_ttl_hours": h.cfg.SessionTTLHours,
		"internal_library":  h.cfg.InternalLibrary,
		"dropzone":          h.cfg.Dropzone,
	})
}

type settingsUpdateRequest struct {
	LibraryPaths    *[]config.LibraryPath         `json:"library_paths"`
	CameraWhitelist *[]config.CameraEntry         `json:"camera_whitelist"`
	FilenameFilters *config.FilenameFilters       `json:"filename_filters"`
	ScanWorkers     *int                          `json:"scan_workers"`
	EventGapDays    *int                          `json:"event_gap_days"`
	EventGeoKm      *float64                      `json:"event_geo_km"`
	InternalLibrary *config.InternalLibraryConfig `json:"internal_library"`
	Dropzone        *config.DropzoneConfig        `json:"dropzone"`
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

	if req.LibraryPaths != nil {
		h.cfg.LibraryPaths = *req.LibraryPaths
	}
	if req.CameraWhitelist != nil {
		h.cfg.CameraWhitelist = *req.CameraWhitelist
	}
	if req.FilenameFilters != nil {
		h.cfg.FilenameFilters = *req.FilenameFilters
	}
	if req.ScanWorkers != nil && *req.ScanWorkers > 0 {
		h.cfg.ScanWorkers = *req.ScanWorkers
	}
	if req.EventGapDays != nil && *req.EventGapDays > 0 {
		h.cfg.EventGapDays = *req.EventGapDays
	}
	if req.EventGeoKm != nil && *req.EventGeoKm > 0 {
		h.cfg.EventGeoKm = *req.EventGeoKm
	}
	if req.InternalLibrary != nil {
		h.cfg.InternalLibrary = *req.InternalLibrary
	}
	if req.Dropzone != nil {
		h.cfg.Dropzone = *req.Dropzone
	}
	if req.AuthEnabled != nil {
		h.cfg.Auth.Enabled = *req.AuthEnabled
	}
	if req.NewPassword != nil && *req.NewPassword != "" {
		hash, err := auth.HashPassword(*req.NewPassword)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "password hashing failed")
			return
		}
		h.cfg.Auth.PasswordHash = hash
	}

	if err := config.Save(h.cfgPath, h.cfg); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save config")
		return
	}
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
