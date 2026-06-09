package integration

import (
	"database/sql"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/halleck/gallery/internal/config"
	"github.com/halleck/gallery/internal/db"
	"github.com/halleck/gallery/internal/library"
	"github.com/halleck/gallery/internal/recognition"
	"github.com/halleck/gallery/internal/scan"
)

// TestSamplePipeline_ScanStageCopyAndOptionalRecognition validates a realistic
// end-to-end flow using the checked-in sample images.
func TestSamplePipeline_ScanStageCopyAndOptionalRecognition(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test skipped in short mode")
	}

	repoRoot := projectRoot(t)
	samplesDir := filepath.Join(repoRoot, "samples")
	expectedFound := countSupportedImages(t, samplesDir)
	if expectedFound == 0 {
		t.Fatalf("no supported images found in samples dir: %s", samplesDir)
	}

	workDir := t.TempDir()
	cacheDir := filepath.Join(workDir, "cache")
	internalLibraryDir := filepath.Join(workDir, "internal-library")
	dbPath := filepath.Join(workDir, "gallery-test.db")

	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	defer database.Close()

	cfg := &config.Config{
		LibraryPaths:    []config.LibraryPath{{Path: samplesDir, Label: "samples"}},
		CameraWhitelist: []config.CameraEntry{},
		FilenameFilters: config.FilenameFilters{
			Include: []string{},
			Exclude: []string{},
		},
		CacheDir:        cacheDir,
		ScanWorkers:     2,
		EventGapDays:    2,
		EventGeoKm:      500,
		SessionTTLHours: 24,
		InternalLibrary: config.InternalLibraryConfig{
			Enabled: true,
			Path:    internalLibraryDir,
		},
	}

	if err := os.MkdirAll(internalLibraryDir, 0o755); err != nil {
		t.Fatalf("create internal library dir: %v", err)
	}

	status, cleanupRecognition := maybeInitRecognition(repoRoot)
	if cleanupRecognition != nil {
		defer cleanupRecognition()
	}
	t.Logf("recognition status: enabled=%v available=%v provider=%q reason=%q", status.Enabled, status.Available, status.ExecutionProvider, status.Reason)

	libraryPathID, err := db.UpsertLibraryPath(database, samplesDir, "samples")
	if err != nil {
		t.Fatalf("upsert library path: %v", err)
	}

	scanner, err := scan.NewScanner(cfg, database, libraryPathID)
	if err != nil {
		t.Fatalf("create scanner: %v", err)
	}

	stats, err := scanner.Run(samplesDir)
	if err != nil {
		t.Fatalf("run scan: %v", err)
	}

	if stats.Found != expectedFound {
		t.Fatalf("found mismatch: got %d want %d", stats.Found, expectedFound)
	}
	if stats.Ingested == 0 {
		t.Fatalf("expected at least one ingested photo, got %d", stats.Ingested)
	}

	photos, total, err := db.ListPhotosFiltered(database, db.PhotoFilter{Page: 1, PerPage: 1000})
	if err != nil {
		t.Fatalf("list photos: %v", err)
	}
	if total != len(photos) {
		t.Fatalf("photo count mismatch: total=%d len=%d", total, len(photos))
	}
	if total != stats.Ingested {
		t.Fatalf("ingested mismatch: stats.ingested=%d db.total=%d", stats.Ingested, total)
	}

	for _, p := range photos {
		entry, err := db.InsertStagingEntry(database, p.SHA256)
		if err != nil {
			t.Fatalf("insert staging entry for %s: %v", p.SHA256, err)
		}
		if err := db.SetStagingState(database, entry.ID, db.StagingStateApproved); err != nil {
			t.Fatalf("approve staging entry %d: %v", entry.ID, err)
		}

		relPath, err := library.CopyPhoto(database, entry, &p, internalLibraryDir)
		if err != nil {
			t.Fatalf("copy photo %s to internal library: %v", p.SHA256, err)
		}
		if relPath == "" {
			t.Fatalf("copy photo %s returned empty relative path", p.SHA256)
		}

		absPath := filepath.Join(internalLibraryDir, relPath)
		if _, err := os.Stat(absPath); err != nil {
			t.Fatalf("copied file missing at %s: %v", absPath, err)
		}
	}

	copies, err := db.ListLibraryCopies(database)
	if err != nil {
		t.Fatalf("list library copies: %v", err)
	}
	if len(copies) != len(photos) {
		t.Fatalf("library copy count mismatch: got %d want %d", len(copies), len(photos))
	}
	for _, c := range copies {
		if !strings.HasPrefix(c.AbsolutePath, internalLibraryDir+string(filepath.Separator)) {
			t.Fatalf("copy path outside internal library: %s", c.AbsolutePath)
		}
		if _, err := os.Stat(c.AbsolutePath); err != nil {
			t.Fatalf("library copy file missing at %s: %v", c.AbsolutePath, err)
		}
	}

	approvedEntries, err := db.ListApprovedStagingEntries(database)
	if err != nil {
		t.Fatalf("list approved staging entries: %v", err)
	}
	if len(approvedEntries) != len(photos) {
		t.Fatalf("approved staging count mismatch: got %d want %d", len(approvedEntries), len(photos))
	}

	assertFaceDetectionOutcome(t, database)
}

func assertFaceDetectionOutcome(t *testing.T, database *sql.DB) {
	t.Helper()

	if !recognition.IsAvailable() {
		t.Log("recognition unavailable; skipping auto-face assertions")
		return
	}

	var autoFaceCount int
	if err := database.QueryRow(`SELECT COUNT(*) FROM faces WHERE source = 'auto'`).Scan(&autoFaceCount); err != nil {
		t.Fatalf("count auto faces: %v", err)
	}

	if autoFaceCount == 0 {
		if os.Getenv("GALLERY_REQUIRE_FACE_DETECTION") == "1" {
			t.Fatalf("recognition is available but no auto faces were detected")
		}
		t.Log("recognition available but sample set produced zero auto detections; set GALLERY_REQUIRE_FACE_DETECTION=1 to enforce")
		return
	}

	t.Logf("auto faces detected: %d", autoFaceCount)
}

func maybeInitRecognition(repoRoot string) (recognition.Status, func()) {
	cfg := config.FaceRecognitionConfig{
		Enabled:              true,
		OnnxruntimeLib:       filepath.Join(repoRoot, "onnx", "onnxruntime-linux-x64-gpu-1.26.0", "lib", "libonnxruntime.so.1"),
		ModelDir:             filepath.Join(repoRoot, "insightface", "buffalo_l"),
		DetectionModel:       "det_10g.onnx",
		RecognitionModel:     "w600k_r50.onnx",
		DetectionThreshold:   0.5,
		RecognitionThreshold: 0.4,
		ClusterMinSamples:    2,
	}

	if !fileExists(cfg.OnnxruntimeLib) || !fileExists(filepath.Join(cfg.ModelDir, cfg.DetectionModel)) {
		return recognition.Status{Enabled: false, Available: false, Reason: "test assets missing; recognition init skipped"}, nil
	}

	status := recognition.Init(cfg, nil)
	if !status.Available {
		return status, nil
	}
	return status, recognition.Cleanup
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func countSupportedImages(t *testing.T, root string) int {
	t.Helper()

	count := 0
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(d.Name()))
		if ext == ".jpg" || ext == ".jpeg" || ext == ".heic" || ext == ".heif" {
			count++
		}
		return nil
	})
	if err != nil {
		t.Fatalf("counting supported images under %s: %v", root, err)
	}
	return count
}

func projectRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// internal/integration/pipeline_test.go -> project root is ../../
	return filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", ".."))
}
