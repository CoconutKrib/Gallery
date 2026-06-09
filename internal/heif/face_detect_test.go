package heif

import (
	"image"
	"os"
	"path/filepath"
	"testing"

	"github.com/halleck/gallery/internal/config"
	"github.com/halleck/gallery/internal/recognition"
)

// TestDecodeHEICForFaceDetection is a targeted test that decodes a HEIC file
// containing a face and verifies that:
//  1. heif.Decode produces a valid *image.NRGBA (correct dimensions, stride, non-zero pixels)
//  2. The SCRFD detector finds the expected face in those pixels
//
// This test does NOT require a database, scan pipeline, or library — just
// the HEIC shim and the ONNX runtime with models present.
func TestDecodeHEICForFaceDetection(t *testing.T) {
	samplePath := "../../samples/heic_face.heic"
	if _, err := os.Stat(samplePath); os.IsNotExist(err) {
		t.Skipf("sample not found: %s", samplePath)
	}

	// Try to init recognition. Skip gracefully if the ONNX runtime or models
	// aren't available — this test only asserts when they are.
	initRecognitionForTest(t)

	if !recognition.IsAvailable() {
		t.Skip("face recognition not available (ONNX runtime or models missing)")
	}

	// --- Decode the HEIC ---

	f, err := os.Open(samplePath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	img, err := Decode(f)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	nrgba, ok := img.(*image.NRGBA)
	if !ok {
		t.Fatalf("expected *image.NRGBA, got %T", img)
	}
	bounds := nrgba.Bounds()
	t.Logf("decoded: %dx%d NRGBA, stride=%d", bounds.Dx(), bounds.Dy(), nrgba.Stride)

	if bounds.Dx() == 0 || bounds.Dy() == 0 {
		t.Fatal("zero dimensions")
	}
	if len(nrgba.Pix) == 0 {
		t.Fatal("empty pixel buffer")
	}

	// Sanity: pixels should not be all zeros.
	nonZero := 0
	for i := 0; i < min(1000, len(nrgba.Pix)); i++ {
		if nrgba.Pix[i] != 0 {
			nonZero++
		}
	}
	if nonZero == 0 {
		t.Error("all sampled pixels are zero — image may be blank")
	}

	// --- Run face detection ---

	detector := recognition.GetDetector()
	if detector == nil {
		t.Fatal("detector is nil despite IsAvailable=true")
	}

	dets, err := detector.Detect(nrgba)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}

	t.Logf("faces detected: %d", len(dets))
	for i, d := range dets {
		t.Logf("  face %d: score=%.3f bbox=[%.0f,%.0f,%.0f,%.0f]",
			i, d.Score, d.X1, d.Y1, d.X2, d.Y2)
	}

	if len(dets) == 0 {
		t.Error("expected at least one face in heic_face.heic, got none")
	}
}

// initRecognitionForTest tries to load the ONNX runtime and models so that
// face detection works in tests. If anything is missing the test skips
// gracefully — recognition is purely additive.
func initRecognitionForTest(t *testing.T) {
	t.Helper()

	if recognition.IsAvailable() {
		return // already initialised
	}

	// Find the repo root relative to this test file (internal/heif/).
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Skipf("cannot resolve repo root: %v", err)
	}

	onnxLib := filepath.Join(repoRoot, "onnx", "onnxruntime-linux-x64-gpu-1.26.0", "lib", "libonnxruntime.so.1")
	modelDir := filepath.Join(repoRoot, "insightface", "buffalo_l")

	if _, err := os.Stat(onnxLib); err != nil {
		t.Skipf("onnxruntime lib not found at %s", onnxLib)
	}
	if _, err := os.Stat(filepath.Join(modelDir, "det_10g.onnx")); err != nil {
		t.Skipf("detection model not found in %s", modelDir)
	}

	cfg := config.FaceRecognitionConfig{
		Enabled:              true,
		OnnxruntimeLib:       onnxLib,
		ModelDir:             modelDir,
		DetectionModel:       "det_10g.onnx",
		RecognitionModel:     "w600k_r50.onnx",
		DetectionThreshold:   0.5,
		RecognitionThreshold: 0.4,
		ClusterMinSamples:    2,
	}

	status := recognition.Init(cfg)
	if !status.Available {
		t.Skipf("recognition init failed: %s", status.Reason)
	}
	t.Cleanup(recognition.Cleanup)
}
