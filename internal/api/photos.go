package api

import (
	"database/sql"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/halleck/gallery/internal/db"
)

func (h *Handlers) handlePhotos(w http.ResponseWriter, r *http.Request) {
	f := db.PhotoFilter{
		CameraMake:  r.URL.Query().Get("make"),
		CameraModel: r.URL.Query().Get("model"),
		Flag:        r.URL.Query().Get("flag"),
		OrderBy:     r.URL.Query().Get("order"),
	}

	if v := r.URL.Query().Get("library_id"); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err == nil {
			f.LibraryPathID = &id
		}
	}
	if v := r.URL.Query().Get("has_gps"); v == "true" {
		t := true
		f.HasGPS = &t
	} else if v == "false" {
		t := false
		f.HasGPS = &t
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

	photos, total, err := db.ListPhotosFiltered(h.db, f)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}

	var items []map[string]any
	for _, p := range photos {
		items = append(items, photoSummary(p))
	}
	if items == nil {
		items = []map[string]any{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"total":    total,
		"page":     f.Page,
		"per_page": f.PerPage,
		"items":    items,
	})
}

func (h *Handlers) handlePhotoDetail(w http.ResponseWriter, r *http.Request) {
	sha256 := r.PathValue("sha256")
	if !isValidSHA256(sha256) {
		writeError(w, http.StatusBadRequest, "invalid sha256")
		return
	}
	photo, err := db.GetPhotoBySHA256(h.db, sha256)
	if err == sql.ErrNoRows {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}

	dupes, _ := db.GetDuplicatesBySHA256(h.db, sha256)
	var dupeList []map[string]any
	for _, d := range dupes {
		dupeList = append(dupeList, map[string]any{
			"filepath":        d.Filepath,
			"library_path_id": d.LibraryPathID,
			"discovered_at":   d.DiscoveredAt,
		})
	}
	if dupeList == nil {
		dupeList = []map[string]any{}
	}

	out := photoDetail(*photo)
	out["duplicates"] = dupeList
	writeJSON(w, http.StatusOK, out)
}

func (h *Handlers) handlePhotoImage(w http.ResponseWriter, r *http.Request) {
	sha256 := r.PathValue("sha256")
	if !isValidSHA256(sha256) {
		writeError(w, http.StatusBadRequest, "invalid sha256")
		return
	}
	photo, err := db.GetPhotoBySHA256(h.db, sha256)
	if err == sql.ErrNoRows {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}

	// Security: ensure the filepath is within a configured library root.
	if !pathIsWithinRoots(photo.Filepath, h.libraryRoots()) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	http.ServeFile(w, r, photo.Filepath)
}

func (h *Handlers) handlePhotoThumbnail(w http.ResponseWriter, r *http.Request) {
	sha256 := r.PathValue("sha256")
	if !isValidSHA256(sha256) {
		writeError(w, http.StatusBadRequest, "invalid sha256")
		return
	}
	if len(sha256) < 2 {
		writeError(w, http.StatusBadRequest, "invalid sha256")
		return
	}

	// Reconstruct thumbnail path from hash + configured cache dir.
	thumbPath := filepath.Join(h.cfg.CacheDir, sha256[:2], sha256+".jpg")
	absCache, _ := filepath.Abs(h.cfg.CacheDir)
	absThumb, _ := filepath.Abs(thumbPath)

	// Security: ensure thumbnail path is within cache dir.
	if !strings.HasPrefix(absThumb, absCache+string(filepath.Separator)) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	http.ServeFile(w, r, thumbPath)
}

// photoSummary returns a condensed map suitable for grid/list views.
func photoSummary(p db.Photo) map[string]any {
	return map[string]any{
		"sha256":        p.SHA256,
		"filename":      p.Filename,
		"captured_at":   p.CapturedAt,
		"camera_make":   p.CameraMake,
		"camera_model":  p.CameraModel,
		"width":         p.Width,
		"height":        p.Height,
		"flags":         p.Flags,
		"thumbnail_url": "/api/photos/" + p.SHA256 + "/thumbnail",
		"image_url":     "/api/photos/" + p.SHA256 + "/image",
	}
}

// photoDetail returns a full map for the photo detail view.
func photoDetail(p db.Photo) map[string]any {
	return map[string]any{
		"sha256":          p.SHA256,
		"filename":        p.Filename,
		"filepath":        p.Filepath,
		"library_path_id": p.LibraryPathID,
		"captured_at":     p.CapturedAt,
		"latitude":        p.Latitude,
		"longitude":       p.Longitude,
		"altitude":        p.Altitude,
		"camera_make":     p.CameraMake,
		"camera_model":    p.CameraModel,
		"camera_serial":   p.CameraSerial,
		"lens_model":      p.LensModel,
		"iso":             p.ISO,
		"aperture":        p.Aperture,
		"shutter_speed":   p.ShutterSpeed,
		"focal_length":    p.FocalLength,
		"flash":           p.Flash,
		"width":           p.Width,
		"height":          p.Height,
		"orientation":     p.Orientation,
		"flags":           p.Flags,
		"ingested_at":     p.IngestedAt,
		"thumbnail_url":   "/api/photos/" + p.SHA256 + "/thumbnail",
		"image_url":       "/api/photos/" + p.SHA256 + "/image",
	}
}

// isValidSHA256 checks that s looks like a hex sha256 (64 chars, 0-9a-f).
func isValidSHA256(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}
