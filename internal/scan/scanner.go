package scan

import (
	"database/sql"
	"fmt"
	"io/fs"
	"log/slog"
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
	Found      int
	Skipped    int
	Ingested   int
	Duplicate  int
	Errors     int
	AutoStaged int // dropzone-only: photos automatically added to staging queue
}

// ScanMode controls how strictly the scanner filters incoming files.
type ScanMode string

const (
	// ScanModeStrict applies camera whitelist and filename filters (normal library scan).
	ScanModeStrict ScanMode = "strict"
	// ScanModeLenient skips whitelist and filename filters; allows missing captured_at
	// (falls back to file mtime and marks the photo as true_date_unknown).
	ScanModeLenient ScanMode = "lenient"
)

// Scanner holds scan configuration and dependencies.
type Scanner struct {
	cfg           *config.Config
	db            *sql.DB
	libraryPathID int64
	whitelist     []WhitelistEntry
	includeRe     []*regexp.Regexp
	excludeRe     []*regexp.Regexp
	mode          ScanMode // strict (default) or lenient (dropzone)
	source        string   // 'scan' or 'dropzone'
	// OnProgress is called after each file decision with current cumulative stats.
	// Optional; safe to leave nil.
	OnProgress func(Stats)
}

// NewScanner creates a strict Scanner for a single library path.
func NewScanner(cfg *config.Config, db *sql.DB, libraryPathID int64) (*Scanner, error) {
	return newScannerWithMode(cfg, db, libraryPathID, ScanModeStrict, "scan")
}

// NewDropzoneScanner creates a lenient Scanner for the dropzone path.
// It skips the camera whitelist and filename filters, allows photos with no
// EXIF date (falling back to file mtime), and sets source = 'dropzone' on
// each newly inserted photo, then auto-stages it in the staging queue.
func NewDropzoneScanner(cfg *config.Config, db *sql.DB, libraryPathID int64) (*Scanner, error) {
	return newScannerWithMode(cfg, db, libraryPathID, ScanModeLenient, "dropzone")
}

func newScannerWithMode(cfg *config.Config, db *sql.DB, libraryPathID int64, mode ScanMode, source string) (*Scanner, error) {
	s := &Scanner{
		cfg:           cfg,
		db:            db,
		libraryPathID: libraryPathID,
		mode:          mode,
		source:        source,
	}

	// Only compile whitelist and filename regexes for strict scans.
	if mode == ScanModeStrict {
		// Compile whitelist.
		for _, e := range cfg.CameraWhitelist {
			s.whitelist = append(s.whitelist, WhitelistEntry{Make: e.Make, Model: e.Model})
		}

		// Compile filename regexes. Patterns are matched case-insensitively; prepend
		// (?i) unless the pattern already opens with it.
		for _, pat := range cfg.FilenameFilters.Include {
			re, err := regexp.Compile(caseInsensitivePattern(pat))
			if err != nil {
				return nil, fmt.Errorf("invalid include pattern %q: %w", pat, err)
			}
			s.includeRe = append(s.includeRe, re)
		}
		for _, pat := range cfg.FilenameFilters.Exclude {
			re, err := regexp.Compile(caseInsensitivePattern(pat))
			if err != nil {
				return nil, fmt.Errorf("invalid exclude pattern %q: %w", pat, err)
			}
			s.excludeRe = append(s.excludeRe, re)
		}
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
					slog.Warn("thumbnail error", "path", job.SourcePath, "err", err)
					continue
				}
				if updateErr := gdb.UpdateThumbnailPath(s.db, job.SHA256, path); updateErr != nil {
					slog.Warn("thumbnail db update error", "sha256", job.SHA256, "err", updateErr)
				}
			}
		}()
	}

	var stats Stats
	walkErr := filepath.WalkDir(rootPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			slog.Warn("scan walk error", "path", path, "err", err)
			stats.Errors++
			return nil // continue walking
		}
		// Skip any directory that is the internal library path or a child of it.
		if d.IsDir() && s.isInternalLibraryPath(path) {
			slog.Debug("scan: skipping internal library dir", "path", path)
			return fs.SkipDir
		}
		if d.IsDir() {
			return nil
		}

		if !isSupportedExtension(d.Name()) {
			return nil
		}
		stats.Found++

		if s.mode == ScanModeStrict && !s.passesFilenameFilters(d.Name()) {
			stats.Skipped++
			s.progress(stats)
			return nil
		}

		exifData, err := ReadEXIF(path)
		if err != nil {
			slog.Warn("scan exif error", "path", path, "err", err)
			stats.Errors++
			s.progress(stats)
			return nil
		}

		if s.mode == ScanModeStrict {
			// No EXIF or camera not on whitelist — skip silently.
			if exifData == nil || (len(s.whitelist) > 0 && !exifData.MatchesWhitelist(s.whitelist)) {
				stats.Skipped++
				s.progress(stats)
				return nil
			}
		} else {
			// Lenient mode: synthesise an empty EXIFData if the file has no EXIF.
			if exifData == nil {
				exifData = &EXIFData{}
			}
			// No EXIF date: fall back to file mtime and flag true_date_unknown.
			if exifData.CapturedAt == nil {
				if fi, err2 := d.Info(); err2 == nil {
					mtime := fi.ModTime().UTC()
					exifData.CapturedAt = &mtime
				}
				exifData.TrueDateUnknown = true
			}
		}

		hash, err := HashFile(path)
		if err != nil {
			slog.Warn("scan hash error", "path", path, "err", err)
			stats.Errors++
			s.progress(stats)
			return nil
		}

		exists, err := gdb.PhotoExistsByHash(s.db, hash)
		if err != nil {
			slog.Warn("scan db lookup error", "path", path, "err", err)
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
				slog.Warn("scan duplicate record error", "path", path, "err", insertErr)
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
		photo := buildPhotoRecord(exifData, hash, path, d.Name(), s.libraryPathID, s.source)
		if _, insertErr := gdb.InsertPhoto(s.db, photo); insertErr != nil {
			slog.Error("scan insert error", "path", path, "err", insertErr)
			stats.Errors++
			return nil
		}
		stats.Ingested++

		// Dropzone: auto-stage the newly inserted photo.
		if s.source == "dropzone" {
			entry, stageErr := gdb.InsertStagingEntry(s.db, hash)
			if stageErr != nil {
				// UNIQUE constraint means already in staging queue — that's fine.
				if !strings.Contains(stageErr.Error(), "UNIQUE") {
					slog.Warn("dropzone: auto-stage failed", "sha256", hash[:8], "err", stageErr)
				}
				entry = nil
			}
			if entry != nil && exifData.TrueDateUnknown {
				// Mark true_date_unknown on the staging entry.
				trueDateUnknown := true
				if updateErr := gdb.UpdateStagingEntry(s.db, entry.ID, gdb.StagingAnnotationUpdate{
					TrueDateUnknown: &trueDateUnknown,
				}); updateErr != nil {
					slog.Warn("dropzone: set true_date_unknown failed", "sha256", hash[:8], "err", updateErr)
				}
			}
			if entry != nil {
				stats.AutoStaged++
			}
		}

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
		slog.Error("scan failed to finalise scan run", "run_id", runID, "err", finishErr)
	}

	if err := gdb.TouchLibraryPath(s.db, s.libraryPathID); err != nil {
		slog.Warn("scan failed to touch library path", "library_path_id", s.libraryPathID, "err", err)
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

// isInternalLibraryPath returns true if path equals or is a subdirectory of the
// configured internal library path. Used to skip the managed copy tree during scans.
func (s *Scanner) isInternalLibraryPath(path string) bool {
	if !s.cfg.InternalLibrary.Enabled || s.cfg.InternalLibrary.Path == "" {
		return false
	}
	ilAbs, err := filepath.Abs(s.cfg.InternalLibrary.Path)
	if err != nil {
		return false
	}
	pathAbs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	ilAbs = filepath.Clean(ilAbs) + string(filepath.Separator)
	pathAbs = filepath.Clean(pathAbs) + string(filepath.Separator)
	return strings.HasPrefix(pathAbs, ilAbs)
}

// passesFilenameFilters returns false if the filename is rejected by include/exclude rules.
// Logic: include (file must match at least one pattern if any are defined), then
// exclude overrides — even if a file matched an include, an exclude match rejects it.
func (s *Scanner) passesFilenameFilters(name string) bool {
	// 1. Must satisfy include list (if one is configured).
	if len(s.includeRe) > 0 {
		matched := false
		for _, re := range s.includeRe {
			if re.MatchString(name) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	// 2. Exclude beats include — if any exclude pattern matches, reject.
	for _, re := range s.excludeRe {
		if re.MatchString(name) {
			return false
		}
	}
	return true
}

// caseInsensitivePattern wraps a regex pattern with (?i) unless it already has it.
func caseInsensitivePattern(pat string) string {
	if strings.HasPrefix(pat, "(?i)") || strings.HasPrefix(pat, "(?-i)") {
		return pat
	}
	return "(?i)" + pat
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
func buildPhotoRecord(e *EXIFData, hash, path, filename string, libID int64, source string) *gdb.Photo {
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
		Source:        source,
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
