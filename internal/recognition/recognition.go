// Package recognition provides optional ONNX-based face detection and embedding.
//
// The package is always compiled in (it uses CGO via yalue/onnxruntime_go, so a
// C compiler is required at build time). At runtime, Init() loads the shared
// libonnxruntime library and ONNX model files. If any step fails the server
// starts normally with recognition marked unavailable; Phase A manual tagging
// continues to work regardless.
package recognition

import (
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/halleck/gallery/internal/config"
	ort "github.com/yalue/onnxruntime_go"
)

// Status reports the current capability state of the recognition pipeline.
type Status struct {
	Enabled           bool
	Available         bool
	ExecutionProvider string // "CUDA", "CPU", or "" when unavailable
	Reason            string // non-empty when Available == false
}

var (
	mu           sync.RWMutex
	globalStatus Status
	det          *Detector
	emb          *Embedder
	initialised  bool

	// clusterMu guards the in-memory cluster state (recomputed after each scan).
	clusterMu    sync.RWMutex
	clusterStore []FaceCluster
	faceCluster  map[int64]int // faceID → clusterID
)

// Init loads the ONNX runtime and model files according to cfg.
// It must be called once at server startup before scan workers begin.
// If cfg.Enabled is false, Init is a no-op that returns a disabled status.
func Init(cfg config.FaceRecognitionConfig) Status {
	if !cfg.Enabled {
		s := Status{
			Enabled:   false,
			Available: false,
			Reason:    "face_recognition.enabled is false",
		}
		mu.Lock()
		globalStatus = s
		mu.Unlock()
		return s
	}

	ort.SetSharedLibraryPath(cfg.OnnxruntimeLib)
	if err := ort.InitializeEnvironment(); err != nil {
		s := Status{
			Enabled:   true,
			Available: false,
			Reason:    "onnxruntime init failed: " + err.Error(),
		}
		mu.Lock()
		globalStatus = s
		mu.Unlock()
		slog.Warn("recognition: onnxruntime init failed", "err", err)
		return s
	}

	// Determine execution provider.
	// Try CUDA first; fall back to CPU on any error.
	ep, sessOpts := detectExecutionProvider()
	slog.Info("recognition: execution provider", "ep", ep)

	// Resolve model paths.
	detModel := cfg.DetectionModel
	if detModel == "" {
		detModel = "det_10g.onnx"
	}
	detPath := filepath.Join(cfg.ModelDir, detModel)

	if _, statErr := os.Stat(detPath); statErr != nil {
		s := Status{
			Enabled:           true,
			Available:         false,
			ExecutionProvider: ep,
			Reason:            "detection model not found: " + detPath,
		}
		mu.Lock()
		globalStatus = s
		mu.Unlock()
		if sessOpts != nil {
			_ = sessOpts.Destroy()
		}
		slog.Warn("recognition: detection model not found", "path", detPath)
		return s
	}

	threshold := float32(cfg.DetectionThreshold)
	if threshold == 0 {
		threshold = 0.5
	}

	detector, detErr := newDetector(detPath, threshold, sessOpts)
	if detErr != nil {
		s := Status{
			Enabled:           true,
			Available:         false,
			ExecutionProvider: ep,
			Reason:            "detector init failed: " + detErr.Error(),
		}
		mu.Lock()
		globalStatus = s
		mu.Unlock()
		if sessOpts != nil {
			_ = sessOpts.Destroy()
		}
		slog.Warn("recognition: detector init failed", "err", detErr)
		return s
	}

	// Load the ArcFace recognition model if configured.
	// If missing, enter detection-only mode.
	var embedder *Embedder
	if cfg.RecognitionModel != "" {
		recPath := filepath.Join(cfg.ModelDir, cfg.RecognitionModel)
		if _, statErr := os.Stat(recPath); statErr == nil {
			var embErr error
			embedder, embErr = newEmbedder(recPath, sessOpts)
			if embErr != nil {
				slog.Warn("recognition: embedder init failed, detection-only mode", "err", embErr)
				embedder = nil
			} else {
				slog.Info("recognition: embedder loaded", "model", recPath)
			}
		} else {
			slog.Info("recognition: recognition model not found, detection-only mode", "path", recPath)
		}
	}

	slog.Info("recognition: ready", "ep", ep, "embedder_loaded", embedder != nil)

	s := Status{
		Enabled:           true,
		Available:         true,
		ExecutionProvider: ep,
	}

	mu.Lock()
	globalStatus = s
	det = detector
	emb = embedder
	initialised = true
	mu.Unlock()

	return s
}

// IsAvailable returns true when the runtime was successfully initialised.
func IsAvailable() bool {
	mu.RLock()
	defer mu.RUnlock()
	return globalStatus.Available
}

// GetStatus returns a snapshot of the current recognition status.
func GetStatus() Status {
	mu.RLock()
	defer mu.RUnlock()
	return globalStatus
}

// GetDetector returns the singleton Detector, or nil if unavailable.
func GetDetector() *Detector {
	mu.RLock()
	defer mu.RUnlock()
	return det
}

// GetEmbedder returns the singleton Embedder.
// Returns nil in detection-only mode (recognition model not loaded).
func GetEmbedder() *Embedder {
	mu.RLock()
	defer mu.RUnlock()
	return emb
}

// SetClusters stores the latest clustering result in memory.
// Called by the post-scan hook and by POST /api/faces/cluster.
func SetClusters(clusters []FaceCluster) {
	lookup := make(map[int64]int, len(clusters)*8)
	for _, c := range clusters {
		for _, faceID := range c.FaceIDs {
			lookup[faceID] = c.ClusterID
		}
	}
	clusterMu.Lock()
	clusterStore = clusters
	faceCluster = lookup
	clusterMu.Unlock()
}

// GetClusters returns a snapshot of the current cluster list.
func GetClusters() []FaceCluster {
	clusterMu.RLock()
	defer clusterMu.RUnlock()
	return clusterStore
}

// GetClusterIDForFace returns the cluster ID for a face, and ok=true if the
// face belongs to any cluster. Returns -1, false when not clustered.
func GetClusterIDForFace(faceID int64) (int, bool) {
	clusterMu.RLock()
	defer clusterMu.RUnlock()
	if faceCluster == nil {
		return -1, false
	}
	id, ok := faceCluster[faceID]
	return id, ok
}

// Cleanup releases the ONNX runtime environment. Call on server shutdown.
func Cleanup() {
	mu.Lock()
	defer mu.Unlock()
	if !initialised {
		return
	}
	if err := ort.DestroyEnvironment(); err != nil {
		slog.Warn("recognition: destroy environment error", "err", err)
	}
	initialised = false
}

// detectExecutionProvider attempts to use CUDA; falls back to CPU.
// Returns the provider name and a *SessionOptions (nil for CPU default).
func detectExecutionProvider() (string, *ort.SessionOptions) {
	cudaOpts, cudaErr := ort.NewCUDAProviderOptions()
	if cudaErr != nil {
		slog.Info("recognition: CUDA provider options unavailable, using CPU", "reason", cudaErr)
		return "CPU", nil
	}
	defer func() { _ = cudaOpts.Destroy() }()

	sessOpts, optErr := ort.NewSessionOptions()
	if optErr != nil {
		slog.Warn("recognition: failed to create session options, using CPU", "err", optErr)
		return "CPU", nil
	}

	if appErr := sessOpts.AppendExecutionProviderCUDA(cudaOpts); appErr != nil {
		_ = sessOpts.Destroy()
		slog.Info("recognition: CUDA append failed, using CPU", "err", appErr)
		return "CPU", nil
	}

	return "CUDA", sessOpts
}
