package api

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/halleck/gallery/internal/auth"
	"github.com/halleck/gallery/internal/config"
	"github.com/halleck/gallery/internal/recognition"
)

// Handlers holds shared dependencies for all API handlers.
type Handlers struct {
	db          *sql.DB
	cfg         *config.Config
	cfgPath     string
	scanner     *ScanManager
	sessions    *auth.Store
	recognition recognition.Status
}

// NewHandlers creates a Handlers with all dependencies wired up.
func NewHandlers(db *sql.DB, cfg *config.Config, cfgPath string, recogStatus recognition.Status) *Handlers {
	return &Handlers{
		db:          db,
		cfg:         cfg,
		cfgPath:     cfgPath,
		scanner:     newScanManager(db, cfg),
		sessions:    auth.NewStore(cfg.SessionTTLHours),
		recognition: recogStatus,
	}
}

// RegisterRoutes registers all HTTP routes on the given mux.
// staticFS should be an http.FileSystem serving the web/ directory.
func (h *Handlers) RegisterRoutes(mux *http.ServeMux, staticFS http.FileSystem) {
	// Auth routes (no middleware).
	mux.HandleFunc("POST /api/login", h.handleLogin)
	mux.HandleFunc("POST /api/logout", h.handleLogout)

	// API routes — wrapped with auth middleware.
	api := func(method, pattern string, fn http.HandlerFunc) {
		mux.HandleFunc(method+" "+pattern, h.authMiddleware(fn))
	}

	api("GET", "/api/libraries", h.handleLibraries)
	api("GET", "/api/photos", h.handlePhotos)
	api("GET", "/api/photos/{sha256}", h.handlePhotoDetail)
	api("GET", "/api/photos/{sha256}/image", h.handlePhotoImage)
	api("GET", "/api/photos/{sha256}/thumbnail", h.handlePhotoThumbnail)
	api("GET", "/api/browse/{library_id}", h.handleBrowse)
	api("GET", "/api/browse/{library_id}/{path...}", h.handleBrowse)
	api("GET", "/api/scan/status", h.scanner.handleStatus)
	api("POST", "/api/scan", h.scanner.handleTrigger)
	api("GET", "/api/settings", h.handleGetSettings)
	api("POST", "/api/settings", h.handlePostSettings)
	api("GET", "/api/issues", h.handleIssues)
	api("GET", "/api/timeline", h.handleTimeline)
	api("GET", "/api/map", h.handleMapPhotos)
	api("GET", "/api/map/nearby", h.handleMapNearby)
	api("GET", "/api/events", h.handleListEvents)
	api("GET", "/api/events/{id}", h.handleEventDetail)
	api("GET", "/api/dedup/report", h.handleDedupReport)
	api("GET", "/api/dedup/subtree", h.handleDedupSubtree)

	// Staging queue.
	api("GET", "/api/staging", h.handleListStaging)
	api("POST", "/api/staging", h.handleAddToStaging)
	api("GET", "/api/staging/{id}", h.handleGetStagingEntry)
	api("PATCH", "/api/staging/{id}", h.handleUpdateStagingEntry)
	api("POST", "/api/staging/{id}/approve", h.handleApproveStaging)
	api("POST", "/api/staging/{id}/reject", h.handleRejectStaging)
	api("DELETE", "/api/staging/{id}", h.handleDeleteStagingEntry)

	// Internal library.
	api("GET", "/api/library/photos", h.handleLibraryPhotos)
	api("GET", "/api/library/tree", h.handleLibraryTree)
	api("GET", "/api/library/status", h.handleLibraryStatus)
	api("POST", "/api/library/copy", h.handleLibraryCopyAll)
	api("POST", "/api/library/copy/{staging_id}", h.handleLibraryCopyOne)
	api("PATCH", "/api/library/copies/{id}", h.handleLibraryCopyPatch)
	api("DELETE", "/api/library/copies/{id}", h.handleLibraryCopyDelete)

	// People and face tagging.
	api("GET", "/api/people", h.handleListPeople)
	api("POST", "/api/people", h.handleCreatePerson)
	api("GET", "/api/people/{id}", h.handleGetPerson)
	api("PATCH", "/api/people/{id}", h.handlePatchPerson)
	api("DELETE", "/api/people/{id}", h.handleDeletePerson)
	api("GET", "/api/people/{id}/photos", h.handlePersonPhotos)
	api("POST", "/api/people/{id}/merge", h.handleMergePerson)
	api("GET", "/api/library/copies/{id}/faces", h.handleListFaces)
	api("POST", "/api/library/copies/{id}/faces", h.handleCreateFace)
	api("PATCH", "/api/faces/{id}", h.handlePatchFace)
	api("DELETE", "/api/faces/{id}", h.handleDeleteFace)

	// Recognition status (always available).
	api("GET", "/api/recognition/status", h.handleRecognitionStatus)

	// Recognition pipeline — Phase B/C (gated on recognition available).
	api("GET", "/api/faces/unidentified", h.handleUnidentifiedFaces)
	api("GET", "/api/faces/suggestions", h.handleSuggestions)
	api("POST", "/api/faces/{id}/confirm", h.handleConfirmFace)
	api("POST", "/api/faces/{id}/reject", h.handleRejectFace)
	api("POST", "/api/faces/cluster", h.handleTriggerCluster)

	// Static assets.
	static := http.FileServer(staticFS)
	mux.HandleFunc("GET /css/", h.authMiddleware(static.ServeHTTP))
	mux.HandleFunc("GET /js/", h.authMiddleware(static.ServeHTTP))
	mux.HandleFunc("GET /vendor/", h.authMiddleware(static.ServeHTTP))

	// SPA fallback: all other GET requests get index.html.
	mux.HandleFunc("GET /", h.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		// Serve index.html for all non-asset, non-API routes.
		f, err := staticFS.Open("index.html")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		f.Close()
		static.ServeHTTP(w, r)
	}))
}

// authMiddleware wraps a handler with authentication if auth is enabled.
func (h *Handlers) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !h.cfg.Auth.Enabled {
			next(w, r)
			return
		}
		if h.sessions.Valid(r) {
			next(w, r)
			return
		}
		// API requests get 401; page requests get redirected to login.
		if strings.HasPrefix(r.URL.Path, "/api/") {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		http.Redirect(w, r, "/login", http.StatusFound)
	}
}

// libraryRoots returns the absolute paths of all configured library paths.
func (h *Handlers) libraryRoots() []string {
	roots := make([]string, 0, len(h.cfg.LibraryPaths))
	for _, lp := range h.cfg.LibraryPaths {
		abs, err := filepath.Abs(lp.Path)
		if err == nil {
			roots = append(roots, abs)
		}
	}
	return roots
}

// pathIsWithinRoots returns true if path resolves to inside one of the roots.
func pathIsWithinRoots(path string, roots []string) bool {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	for _, root := range roots {
		if absPath == root || strings.HasPrefix(absPath, root+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// writeJSON writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		// Headers already sent; nothing to do.
		fmt.Fprintf(w, `{"error":"encode error"}`)
	}
}

// writeError writes a JSON error response.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// parseID parses a string to int64.
func parseID(s string) (int64, error) {
	return strconv.ParseInt(s, 10, 64)
}

// parseInt parses a string to int.
func parseInt(s string) (int, error) {
	n, err := strconv.ParseInt(s, 10, 64)
	return int(n), err
}
