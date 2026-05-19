package api

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/halleck/gallery/internal/cluster"
	"github.com/halleck/gallery/internal/config"
	"github.com/halleck/gallery/internal/db"
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
		log.Printf("[api/scan] fetching latest runs: %v", err)
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
	LibraryPathID *int64 `json:"library_path_id"` // nil = scan all
}

func (sm *ScanManager) handleTrigger(w http.ResponseWriter, r *http.Request) {
	var req scanTriggerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err.Error() != "EOF" {
		writeError(w, http.StatusBadRequest, "invalid request body")
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
			log.Printf("[scan] upsert library path %q: %v", lp.Path, err)
			continue
		}

		sm.mu.Lock()
		sm.label = lp.Label
		sm.liveStats = scan.Stats{}
		sm.mu.Unlock()

		scanner, err := scan.NewScanner(sm.cfg, sm.database, lpID)
		if err != nil {
			log.Printf("[scan] creating scanner for %q: %v", lp.Path, err)
			continue
		}
		scanner.OnProgress = func(stats scan.Stats) {
			sm.mu.Lock()
			sm.liveStats = stats
			sm.mu.Unlock()
		}

		log.Printf("[scan] starting %q (%s)", lp.Path, lp.Label)
		stats, err := scanner.Run(lp.Path)
		if err != nil {
			log.Printf("[scan] %q failed: %v", lp.Path, err)
		}
		log.Printf("[scan] %q done — found:%d skipped:%d ingested:%d duplicate:%d errors:%d",
			lp.Path, stats.Found, stats.Skipped, stats.Ingested, stats.Duplicate, stats.Errors)
	}

	// Re-cluster after all scans complete.
	gapDays := sm.cfg.EventGapDays
	geoKm := sm.cfg.EventGeoKm
	log.Printf("[cluster] running event clustering (gap=%dd, geo=%.0fkm)", gapDays, geoKm)
	if err := cluster.Run(sm.database, gapDays, geoKm); err != nil {
		log.Printf("[cluster] error: %v", err)
	} else {
		log.Printf("[cluster] done")
	}
}
