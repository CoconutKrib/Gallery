package api

import (
	"net/http"
	"path/filepath"
	"sort"
	"strings"

	"github.com/halleck/gallery/internal/db"
)

type browseResponse struct {
	LibraryID    int64            `json:"library_id"`
	LibraryLabel string           `json:"library_label"`
	RelativePath string           `json:"relative_path"`
	Subdirs      []subdirEntry    `json:"subdirs"`
	Photos       []map[string]any `json:"photos"`
}

type subdirEntry struct {
	Name string `json:"name"`
	Path string `json:"path"` // relative path from library root
}

func (h *Handlers) handleBrowse(w http.ResponseWriter, r *http.Request) {
	// Path values: library_id and optional path remainder.
	libraryIDStr := r.PathValue("library_id")
	relPath := r.PathValue("path")
	// Clean the relative path to prevent traversal.
	relPath = filepath.ToSlash(filepath.Clean("/" + relPath))
	relPath = strings.TrimPrefix(relPath, "/")

	libraryID, err := parseID(libraryIDStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid library_id")
		return
	}

	lp, err := db.GetLibraryPathByID(h.db, libraryID)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	// Build the absolute directory path we are listing.
	var dirPath string
	if relPath == "" || relPath == "." {
		dirPath = lp.Path
	} else {
		dirPath = filepath.Join(lp.Path, relPath)
	}

	// Security: ensure dirPath is within the library root.
	absRoot, _ := filepath.Abs(lp.Path)
	absDir, _ := filepath.Abs(dirPath)
	if absDir != absRoot && !strings.HasPrefix(absDir, absRoot+string(filepath.Separator)) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}

	// Get all photos under this directory (recursive DB query, then filter in Go).
	allPhotos, err := db.GetPhotosByFilepathPrefix(h.db, absDir)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}

	// Partition into direct children and subdirectory names.
	var directPhotos []map[string]any
	subdirSet := map[string]struct{}{}

	for _, p := range allPhotos {
		// Strip the dirPath prefix to get the relative portion.
		rel, err := filepath.Rel(absDir, p.Filepath)
		if err != nil {
			continue
		}
		rel = filepath.ToSlash(rel)
		parts := strings.SplitN(rel, "/", 2)
		if len(parts) == 1 {
			// Direct child.
			directPhotos = append(directPhotos, photoSummary(p))
		} else {
			// In a subdirectory — record the first component.
			subdirSet[parts[0]] = struct{}{}
		}
	}

	// Sort subdirs alphabetically.
	subdirs := make([]subdirEntry, 0, len(subdirSet))
	for name := range subdirSet {
		var subRelPath string
		if relPath == "" || relPath == "." {
			subRelPath = name
		} else {
			subRelPath = relPath + "/" + name
		}
		subdirs = append(subdirs, subdirEntry{Name: name, Path: subRelPath})
	}
	sort.Slice(subdirs, func(i, j int) bool { return subdirs[i].Name < subdirs[j].Name })

	if directPhotos == nil {
		directPhotos = []map[string]any{}
	}

	writeJSON(w, http.StatusOK, browseResponse{
		LibraryID:    libraryID,
		LibraryLabel: lp.Label,
		RelativePath: relPath,
		Subdirs:      subdirs,
		Photos:       directPhotos,
	})
}

func (h *Handlers) handleLibraries(w http.ResponseWriter, r *http.Request) {
	lps, err := db.GetLibraryPaths(h.db)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	type lpResp struct {
		ID            int64  `json:"id"`
		Path          string `json:"path"`
		Label         string `json:"label"`
		LastScannedAt any    `json:"last_scanned_at"`
	}
	out := make([]lpResp, 0, len(lps))
	for _, lp := range lps {
		out = append(out, lpResp{
			ID:            lp.ID,
			Path:          lp.Path,
			Label:         lp.Label,
			LastScannedAt: lp.LastScannedAt,
		})
	}
	writeJSON(w, http.StatusOK, out)
}
