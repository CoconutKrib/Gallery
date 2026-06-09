package api

import (
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/halleck/gallery/internal/cluster"
	"github.com/halleck/gallery/internal/config"
	"github.com/halleck/gallery/internal/db"
	"github.com/halleck/gallery/internal/recognition"
	"github.com/halleck/gallery/internal/scan"
)

// ScanManager coordinates scan runs and exposes status for the API.
type ScanManager struct {
	mu        sync.Mutex
	running   bool
	liveStats scan.Stats
	startedAt *time.Time
	label     string
	database  *sql.DB
	cfg       *config.Config
}

func newScanManager(database *sql.DB, cfg *config.Config) *ScanManager {
	return &ScanManager{database: database, cfg: cfg}
}

type scanStatusResponse struct {
	Running      bool            `json:"running"`
	CurrentLabel string          `json:"current_label,omitempty"`
	StartedAt    *time.Time      `json:"started_at,omitempty"`
	LiveStats    *scan.Stats     `json:"live_stats,omitempty"`
	LastRuns     []scanRunDetail `json:"last_runs"`
}

type scanRunDetail struct {
	ID             int64      `json:"id"`
	LibraryPathID  int64      `json:"library_path_id"`
	LibraryLabel   string     `json:"library_label"`
	StartedAt      time.Time  `json:"started_at"`
	FinishedAt     *time.Time `json:"finished_at"`
	FilesFound     int        `json:"files_found"`
	FilesSkipped   int        `json:"files_skipped"`
	FilesIngested  int        `json:"files_ingested"`
	FilesDuplicate int        `json:"files_duplicate"`
	FilesError     int        `json:"files_error"`
}

func (sm *ScanManager) handleStatus(w http.ResponseWriter, r *http.Request) {
	sm.mu.Lock()
	resp := scanStatusResponse{
		Running:      sm.running,
		CurrentLabel: sm.label,
		StartedAt:    sm.startedAt,
	}
	if sm.running {
		stats := sm.liveStats
		resp.LiveStats = &stats
	}
	sm.mu.Unlock()

	// Fetch latest scan run per library path from DB.
	runs, err := db.GetAllLatestScanRuns(sm.database)
	if err != nil {
		slog.Warn("scan: fetch latest runs failed", "err", err)
	}
	resp.LastRuns = []scanRunDetail{}
	for _, run := range runs {
		lp, _ := db.GetLibraryPathByID(sm.database, run.LibraryPathID)
		label := ""
		if lp != nil {
			label = lp.Label
		}
		resp.LastRuns = append(resp.LastRuns, scanRunDetail{
			ID:             run.ID,
			LibraryPathID:  run.LibraryPathID,
			LibraryLabel:   label,
			StartedAt:      run.StartedAt,
			FinishedAt:     run.FinishedAt,
			FilesFound:     run.FilesFound,
			FilesSkipped:   run.FilesSkipped,
			FilesIngested:  run.FilesIngested,
			FilesDuplicate: run.FilesDuplicate,
			FilesError:     run.FilesError,
		})
	}

	writeJSON(w, http.StatusOK, resp)
}

type scanTriggerRequest struct {
	LibraryPathID *int64  `json:"library_path_id"` // nil = scan all library paths
	Source        *string `json:"source"`          // "dropzone" to scan the dropzone instead
}

func (sm *ScanManager) handleTrigger(w http.ResponseWriter, r *http.Request) {
	var req scanTriggerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err.Error() != "EOF" {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Dropzone scan — handled separately.
	if req.Source != nil && *req.Source == "dropzone" {
		if !sm.cfg.Dropzone.Enabled || sm.cfg.Dropzone.Path == "" {
			writeError(w, http.StatusConflict, "dropzone not enabled")
			return
		}
		sm.mu.Lock()
		if sm.running {
			sm.mu.Unlock()
			writeError(w, http.StatusConflict, "scan already running")
			return
		}
		sm.running = true
		sm.liveStats = scan.Stats{}
		now := time.Now()
		sm.startedAt = &now
		sm.mu.Unlock()

		go sm.runDropzoneScan()
		writeJSON(w, http.StatusAccepted, map[string]string{"status": "dropzone scan started"})
		return
	}

	sm.mu.Lock()
	if sm.running {
		sm.mu.Unlock()
		writeError(w, http.StatusConflict, "scan already running")
		return
	}
	sm.running = true
	sm.liveStats = scan.Stats{}
	now := time.Now()
	sm.startedAt = &now
	sm.mu.Unlock()

	// Determine which library paths to scan.
	var paths []config.LibraryPath
	if req.LibraryPathID != nil {
		for _, lp := range sm.cfg.LibraryPaths {
			lpID, err := db.UpsertLibraryPath(sm.database, lp.Path, lp.Label)
			if err == nil && lpID == *req.LibraryPathID {
				paths = append(paths, lp)
				break
			}
		}
		if len(paths) == 0 {
			sm.mu.Lock()
			sm.running = false
			sm.mu.Unlock()
			writeError(w, http.StatusNotFound, "library path not found")
			return
		}
	} else {
		paths = sm.cfg.LibraryPaths
	}

	go sm.runScans(paths)
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "scan started"})
}

func (sm *ScanManager) runDropzoneScan() {
	defer func() {
		sm.mu.Lock()
		sm.running = false
		sm.label = ""
		sm.startedAt = nil
		sm.mu.Unlock()
	}()

	dzPath := sm.cfg.Dropzone.Path
	lpID, err := db.UpsertLibraryPath(sm.database, dzPath, "Dropzone")
	if err != nil {
		slog.Error("dropzone: upsert library path failed", "path", dzPath, "err", err)
		return
	}

	sm.mu.Lock()
	sm.label = "Dropzone"
	sm.liveStats = scan.Stats{}
	sm.mu.Unlock()

	scanner, err := scan.NewDropzoneScanner(sm.cfg, sm.database, lpID)
	if err != nil {
		slog.Error("dropzone: creating scanner failed", "err", err)
		return
	}
	scanner.OnProgress = func(stats scan.Stats) {
		sm.mu.Lock()
		sm.liveStats = stats
		sm.mu.Unlock()
	}

	slog.Info("dropzone: scan starting", "path", dzPath)
	stats, err := scanner.Run(dzPath)
	if err != nil {
		slog.Error("dropzone: scan failed", "path", dzPath, "err", err)
	}
	slog.Info("dropzone: scan done",
		"path", dzPath,
		"found", stats.Found, "skipped", stats.Skipped,
		"ingested", stats.Ingested, "duplicate", stats.Duplicate,
		"auto_staged", stats.AutoStaged, "errors", stats.Errors)

	// Re-cluster so newly ingested photos are grouped.
	if stats.Ingested > 0 {
		gapDays := sm.cfg.EventGapDays
		geoKm := sm.cfg.EventGeoKm
		slog.Info("cluster: running after dropzone scan", "gap_days", gapDays, "geo_km", geoKm)
		if err := cluster.Run(sm.database, gapDays, geoKm); err != nil {
			slog.Error("cluster: failed", "err", err)
		} else {
			slog.Info("cluster: done")
		}
	}
}

func (sm *ScanManager) runScans(paths []config.LibraryPath) {
	defer func() {
		sm.mu.Lock()
		sm.running = false
		sm.label = ""
		sm.startedAt = nil
		sm.mu.Unlock()
	}()

	for _, lp := range paths {
		lpID, err := db.UpsertLibraryPath(sm.database, lp.Path, lp.Label)
		if err != nil {
			slog.Error("scan: upsert library path failed", "path", lp.Path, "err", err)
			continue
		}

		sm.mu.Lock()
		sm.label = lp.Label
		sm.liveStats = scan.Stats{}
		sm.mu.Unlock()

		scanner, err := scan.NewScanner(sm.cfg, sm.database, lpID)
		if err != nil {
			slog.Error("scan: creating scanner failed", "path", lp.Path, "err", err)
			continue
		}
		scanner.OnProgress = func(stats scan.Stats) {
			sm.mu.Lock()
			sm.liveStats = stats
			sm.mu.Unlock()
		}

		slog.Info("scan: starting", "path", lp.Path, "label", lp.Label)
		stats, err := scanner.Run(lp.Path)
		if err != nil {
			slog.Error("scan: failed", "path", lp.Path, "err", err)
		}
		slog.Info("scan: done",
			"path", lp.Path,
			"found", stats.Found, "skipped", stats.Skipped,
			"ingested", stats.Ingested, "duplicate", stats.Duplicate,
			"errors", stats.Errors)
	}

	// Re-cluster after all scans complete.
	gapDays := sm.cfg.EventGapDays
	geoKm := sm.cfg.EventGeoKm
	slog.Info("cluster: running", "gap_days", gapDays, "geo_km", geoKm)
	if err := cluster.Run(sm.database, gapDays, geoKm); err != nil {
		slog.Error("cluster: failed", "err", err)
	} else {
		slog.Info("cluster: done")
	}
}

// runRecognitionPostScan runs the suggestion and clustering pipelines after a
// scan completes, if the recognition runtime is available.
// Step 1 — suggest: for unidentified faces, find the closest known person and
// set person_id (unverified candidate).
// Step 2 — cluster: group remaining unidentified faces by embedding similarity
// and store the result in memory for the review UI.
func runRecognitionPostScan(database *sql.DB, cfg *config.Config) {
	if !recognition.IsAvailable() {
		return
	}

	threshold := cfg.FaceRecognition.RecognitionThreshold
	if threshold == 0 {
		threshold = 0.35
	}

	// Step 1: suggestion pipeline.
	verified, err := db.GetVerifiedFacesWithEmbeddings(database)
	if err != nil {
		slog.Error("recognition: loading verified faces", "err", err)
	} else if len(verified) > 0 {
		unid, err := db.GetUnidentifiedFacesWithEmbeddings(database)
		if err != nil {
			slog.Error("recognition: loading unidentified faces for suggest", "err", err)
		} else if len(unid) > 0 {
			verEmbs := facesToEmbeddings(verified)
			unidEmbs := facesToEmbeddings(unid)
			suggestions := recognition.Suggest(unidEmbs, verEmbs, threshold)
			for _, s := range suggestions {
				if err := db.SetFacePersonCandidate(database, s.FaceID, s.PersonID); err != nil {
					slog.Warn("recognition: set person candidate", "face_id", s.FaceID, "err", err)
				}
			}
			slog.Info("recognition: suggestions applied", "count", len(suggestions))
		}
	}

	// Step 2: cluster remaining unidentified faces.
	unid, err := db.GetUnidentifiedFacesWithEmbeddings(database)
	if err != nil {
		slog.Error("recognition: loading unidentified faces for cluster", "err", err)
		return
	}
	minSamples := cfg.FaceRecognition.ClusterMinSamples
	if minSamples < 2 {
		minSamples = 2
	}
	embs := facesToEmbeddings(unid)
	clusters := recognition.Cluster(embs, threshold, minSamples)
	recognition.SetClusters(clusters)
	slog.Info("recognition: clustering done", "unidentified", len(unid), "clusters", len(clusters))
}

// facesToEmbeddings converts db.Face slices into recognition.FaceEmbedding slices,
// decoding the embedding bytes.
func facesToEmbeddings(faces []*db.Face) []recognition.FaceEmbedding {
	out := make([]recognition.FaceEmbedding, 0, len(faces))
	for _, f := range faces {
		if len(f.Embedding) == 0 {
			continue
		}
		out = append(out, recognition.FaceEmbedding{
			FaceID:    f.ID,
			PersonID:  f.PersonID,
			Embedding: recognition.BytesToEmbedding(f.Embedding),
		})
	}
	return out
}
