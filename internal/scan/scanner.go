package scan

import (
	"database/sql"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/halleck/gallery/internal/config"
	gdb "github.com/halleck/gallery/internal/db"
)

// Stats tracks counters for a single scan run.
type Stats struct {
	Found     int
	Skipped   int
	Ingested  int
	Duplicate int
	Errors    int
}

// Scanner holds scan configuration and dependencies.
type Scanner struct {
	cfg           *config.Config
	db            *sql.DB
	libraryPathID int64
	whitelist     []WhitelistEntry
	includeRe     []*regexp.Regexp
	excludeRe     []*regexp.Regexp
	// OnProgress is called after each file decision with current cumulative stats.
	// Optional; safe to leave nil.
	OnProgress func(Stats)
}

// NewScanner creates a Scanner for a single library path.
func NewScanner(cfg *config.Config, db *sql.DB, libraryPathID int64) (*Scanner, error) {
	s := &Scanner{
		cfg:           cfg,
		db:            db,
		libraryPathID: libraryPathID,
	}

	// Compile whitelist.
	for _, e := range cfg.CameraWhitelist {
		s.whitelist = append(s.whitelist, WhitelistEntry{Make: e.Make, Model: e.Model})
	}

	// Compile filename regexes.
	for _, pat := range cfg.FilenameFilters.Include {
		re, err := regexp.Compile(pat)
		if err != nil {
			return nil, fmt.Errorf("invalid include pattern %q: %w", pat, err)
		}
		s.includeRe = append(s.includeRe, re)
	}
	for _, pat := range cfg.FilenameFilters.Exclude {
		re, err := regexp.Compile(pat)
		if err != nil {
			return nil, fmt.Errorf("invalid exclude pattern %q: %w", pat, err)
		}
		s.excludeRe = append(s.excludeRe, re)
	}

	return s, nil
}

// Run walks the directory tree at rootPath and ingests qualifying photos.
// It starts a scan_runs record, processes files, and finalises the record when done.
func (s *Scanner) Run(rootPath string) (Stats, error) {
	runID, err := gdb.StartScanRun(s.db, s.libraryPathID)
	if err != nil {
		return Stats{}, fmt.Errorf("starting scan run: %w", err)
	}

	// Thumbnail worker pool.
	thumbJobs := make(chan *ThumbJob, s.cfg.ScanWorkers*4)
	var thumbWg sync.WaitGroup
	for i := 0; i < s.cfg.ScanWorkers; i++ {
		thumbWg.Add(1)
		go func() {
			defer thumbWg.Done()
			for job := range thumbJobs {
				path, err := GenerateThumbnail(job.SourcePath, job.SHA256, s.cfg.CacheDir)
				job.ResultPath = path
				job.Err = err
				if err != nil {
					log.Printf("[thumbnail] error for %s: %v", job.SourcePath, err)
					continue
				}
				if updateErr := gdb.UpdateThumbnailPath(s.db, job.SHA256, path); updateErr != nil {
					log.Printf("[thumbnail] db update error for %s: %v", job.SHA256, updateErr)
				}
			}
		}()
	}

	var stats Stats
	walkErr := filepath.WalkDir(rootPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			log.Printf("[scan] walk error at %s: %v", path, err)
			stats.Errors++
			return nil // continue walking
		}
		if d.IsDir() {
			return nil
		}

		if !isSupportedExtension(d.Name()) {
			return nil
		}
		stats.Found++

		if !s.passesFilenameFilters(d.Name()) {
			stats.Skipped++
			s.progress(stats)
			return nil
		}

		exifData, err := ReadEXIF(path)
		if err != nil {
			log.Printf("[scan] exif error for %s: %v", path, err)
			stats.Errors++
			s.progress(stats)
			return nil
		}
		// No EXIF or camera not on whitelist — skip silently.
		if exifData == nil || (len(s.whitelist) > 0 && !exifData.MatchesWhitelist(s.whitelist)) {
			stats.Skipped++
			s.progress(stats)
			return nil
		}

		hash, err := HashFile(path)
		if err != nil {
			log.Printf("[scan] hash error for %s: %v", path, err)
			stats.Errors++
			s.progress(stats)
			return nil
		}

		exists, err := gdb.PhotoExistsByHash(s.db, hash)
		if err != nil {
			log.Printf("[scan] db lookup error for %s: %v", path, err)
			stats.Errors++
			s.progress(stats)
			return nil
		}

		if exists {
			// Check if this is the canonical path already stored in photos.
			// If so, it's simply a re-scan of an already-ingested file — skip silently.
			canonical, err := gdb.GetCanonicalFilepath(s.db, hash)
			if err == nil && canonical == path {
				stats.Skipped++
				return nil
			}

			// Different path, same hash — it's a duplicate location.
			// Attempt to record it (idempotent: INSERT OR IGNORE handles rescans).
			isNewDupe, insertErr := recordDuplicateIfNew(s.db, hash, path, s.libraryPathID)
			if insertErr != nil {
				log.Printf("[scan] duplicate record error for %s: %v", path, insertErr)
			}
			if isNewDupe {
				stats.Duplicate++
			} else {
				// Already known duplicate — don't count again.
				stats.Skipped++
			}
			s.progress(stats)
			return nil
		}

		// New photo — ingest.
		photo := buildPhotoRecord(exifData, hash, path, d.Name(), s.libraryPathID)
		if _, insertErr := gdb.InsertPhoto(s.db, photo); insertErr != nil {
			log.Printf("[scan] insert error for %s: %v", path, insertErr)
			stats.Errors++
			return nil
		}
		stats.Ingested++
		s.progress(stats)

		// Queue thumbnail generation.
		thumbJobs <- &ThumbJob{
			SourcePath: path,
			SHA256:     hash,
			CacheDir:   s.cfg.CacheDir,
		}

		return nil
	})

	close(thumbJobs)
	thumbWg.Wait()

	finishErr := gdb.FinishScanRun(s.db, runID,
		stats.Found, stats.Skipped, stats.Ingested, stats.Duplicate, stats.Errors)
	if finishErr != nil {
		log.Printf("[scan] failed to finalise scan run %d: %v", runID, finishErr)
	}

	if err := gdb.TouchLibraryPath(s.db, s.libraryPathID); err != nil {
		log.Printf("[scan] failed to touch library path %d: %v", s.libraryPathID, err)
	}

	if walkErr != nil {
		return stats, fmt.Errorf("walking directory: %w", walkErr)
	}
	return stats, nil
}

// isSupportedExtension returns true for JPEG files. HEIC deferred to a future phase.
func isSupportedExtension(name string) bool {
	lower := strings.ToLower(name)
	return strings.HasSuffix(lower, ".jpg") || strings.HasSuffix(lower, ".jpeg")
}

// passesFilenameFilters returns false if the filename is rejected by include/exclude rules.
func (s *Scanner) passesFilenameFilters(name string) bool {
	for _, re := range s.excludeRe {
		if re.MatchString(name) {
			return false
		}
	}
	if len(s.includeRe) == 0 {
		return true
	}
	for _, re := range s.includeRe {
		if re.MatchString(name) {
			return true
		}
	}
	return false
}

// recordDuplicateIfNew attempts to insert a duplicate_paths record.
// Returns true if this was a genuinely new duplicate location (not seen before).
func recordDuplicateIfNew(db *sql.DB, sha256, path string, libraryPathID int64) (bool, error) {
	exists, err := gdb.DuplicatePathExists(db, sha256, path)
	if err != nil {
		return false, err
	}
	if exists {
		return false, nil
	}
	return true, gdb.InsertDuplicatePath(db, sha256, path, libraryPathID)
}

// buildPhotoRecord maps EXIFData + file metadata to a db.Photo ready for insertion.
func buildPhotoRecord(e *EXIFData, hash, path, filename string, libID int64) *gdb.Photo {
	p := &gdb.Photo{
		SHA256:        hash,
		Filepath:      path,
		LibraryPathID: libID,
		Filename:      filename,
		CapturedAt:    e.CapturedAt,
		Latitude:      e.Latitude,
		Longitude:     e.Longitude,
		Altitude:      e.Altitude,
		CameraMake:    e.CameraMake,
		CameraModel:   e.CameraModel,
		Flags:         e.Flags(),
	}
	if e.CameraSerial != "" {
		p.CameraSerial = &e.CameraSerial
	}
	if e.LensModel != "" {
		p.LensModel = &e.LensModel
	}
	if e.ISO != nil {
		p.ISO = e.ISO
	}
	if e.Aperture != nil {
		p.Aperture = e.Aperture
	}
	if e.ShutterSpeed != "" {
		s := e.ShutterSpeed
		p.ShutterSpeed = &s
	}
	if e.FocalLength != nil {
		p.FocalLength = e.FocalLength
	}
	if e.Flash != nil {
		p.Flash = e.Flash
	}
	if e.Width != nil {
		p.Width = e.Width
	}
	if e.Height != nil {
		p.Height = e.Height
	}
	if e.Orientation != nil {
		p.Orientation = e.Orientation
	}
	return p
}

// progress calls OnProgress with the current stats if it is set.
func (s *Scanner) progress(stats Stats) {
	if s.OnProgress != nil {
		s.OnProgress(stats)
	}
}

// ensureCacheDir creates the cache directory if it doesn't exist.
func ensureCacheDir(cacheDir string) error {
	return os.MkdirAll(cacheDir, 0o755)
}
