// onnxruntime_spike: build familiarity with yalue/onnxruntime_go and the
// InsightFace buffalo_l models before integrating them into Gallery.
//
// This is a standalone module (go.mod: "gallery-onnx-spike") kept under
// _exploration/ so that Gallery's ./... builds never pick it up.
//
// ── Prerequisites ─────────────────────────────────────────────────────────────
//
//  1. ONNX Runtime shared library
//     Download the "Linux x64" release from:
//       https://github.com/microsoft/onnxruntime/releases
//     Extract and note the path to libonnxruntime.so.1.x.x.
//     Pass it via -lib=<path> (or the ONNXRUNTIME_LIB env var).
//
//  2. InsightFace buffalo_l model pack
//     Two ways to get it:
//       a) Install insightface Python package and call:
//            python3 -c "import insightface; insightface.app.FaceAnalysis(name='buffalo_l')"
//          Models are downloaded to ~/.insightface/models/buffalo_l/
//       b) Download directly from the InsightFace release:
//            https://github.com/deepinsight/insightface/releases
//          Look for buffalo_l.zip and extract it.
//
//     Required model files in the same directory:
//       det_10g.onnx    – SCRFD-10GF face detector  (Apache 2.0)
//       w600k_r50.onnx  – ArcFace ResNet-50 recognition (non-commercial research only)
//
//  3. A test image (any JPEG/PNG with one or more faces)
//     Pass it via -img=<path>.
//
// ── Building ──────────────────────────────────────────────────────────────────
//   cd _exploration/onnxruntime_spike
//   go mod tidy
//   go build .        # requires gcc; does NOT need onnxruntime headers/library
//
// ── Running ───────────────────────────────────────────────────────────────────
//   ./onnxruntime_spike \
//       -lib  /path/to/libonnxruntime.so.1.x.x \
//       -models /path/to/buffalo_l/ \
//       -img   /path/to/test.jpg
//
// ── License note ──────────────────────────────────────────────────────────────
//   The spike code itself is MIT-licensed (part of Gallery).
//   w600k_r50.onnx is distributed under InsightFace non-commercial research
//   licence. Ensure you have the right to use these models for your purpose
//   before integrating them into production code.

package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"log"
	"math"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/image/draw"

	ort "github.com/yalue/onnxruntime_go"
)

func main() {
	libPath := flag.String("lib", os.Getenv("ONNXRUNTIME_LIB"), "path to libonnxruntime.so")
	modelDir := flag.String("models", "", "directory containing det_10g.onnx and w600k_r50.onnx")
	imgPath := flag.String("img", "", "test image path (JPEG or PNG)")
	flag.Parse()

	if *libPath == "" {
		log.Fatal("error: -lib (or ONNXRUNTIME_LIB env var) is required")
	}
	if *modelDir == "" {
		log.Fatal("error: -models is required")
	}
	if *imgPath == "" {
		log.Fatal("error: -img is required")
	}

	detModel := filepath.Join(*modelDir, "det_10g.onnx")
	recModel := filepath.Join(*modelDir, "w600k_r50.onnx")

	// ── 1. Initialise the ONNX Runtime environment ─────────────────────────

	log.Printf("Loading ONNX Runtime from: %s", *libPath)
	ort.SetSharedLibraryPath(*libPath)
	if err := ort.InitializeEnvironment(); err != nil {
		log.Fatalf("InitializeEnvironment: %v", err)
	}
	defer func() {
		if err := ort.DestroyEnvironment(); err != nil {
			log.Printf("warning: DestroyEnvironment: %v", err)
		}
	}()
	log.Println("ONNX Runtime environment ready.")

	// ── 2. Load the test image ─────────────────────────────────────────────

	log.Printf("Loading image: %s", *imgPath)
	srcImg, err := loadImage(*imgPath)
	if err != nil {
		log.Fatalf("loadImage: %v", err)
	}
	log.Printf("Image size: %dx%d", srcImg.Bounds().Dx(), srcImg.Bounds().Dy())

	// ── 3. Face detection (SCRFD via det_10g.onnx) ────────────────────────
	//
	// The SCRFD model expects:
	//   Input  : "input.1"  shape (1, 3, H, W)  float32
	//             pixel normalisation: (value - 127.5) / 128.0
	//             H and W must be multiples of 32; 640×640 is the standard size.
	//   Outputs: 9 tensors (3 strides × score + bbox + kps) — we use the first
	//             two strides' scores to confirm detections here.
	//
	// NOTE: buffalo_l's det_10g.onnx uses dynamic input shapes, so we fix
	// them to 640×640 for simplicity.

	const detW, detH = 640, 640

	log.Printf("Running face detection (det_10g.onnx) at %dx%d…", detW, detH)

	detInput, err := preprocessSCRFD(srcImg, detW, detH)
	if err != nil {
		log.Fatalf("preprocessSCRFD: %v", err)
	}

	detBoxes, err := runDetection(detModel, detInput, detW, detH)
	if err != nil {
		log.Fatalf("runDetection: %v", err)
	}

	log.Printf("Detected %d face(s).", len(detBoxes))
	for i, b := range detBoxes {
		log.Printf("  Face %d: x1=%.1f y1=%.1f x2=%.1f y2=%.1f score=%.3f",
			i, b.X1, b.Y1, b.X2, b.Y2, b.Score)
	}

	if len(detBoxes) == 0 {
		log.Println("No faces detected; skipping recognition step.")
		return
	}

	// ── 4. Recognition embedding (ArcFace via w600k_r50.onnx) ─────────────
	//
	// ArcFace expects:
	//   Input  : "input.1"  shape (1, 3, 112, 112)  float32
	//             pixel normalisation: (value - 127.5) / 128.0
	//   Output : "683"      shape (1, 512)           float32  (L2-normalised)
	//
	// For the spike we use a simple crop-and-resize alignment (no landmark warp).
	// Phase B will use a proper similarity transform onto the 5 detected keypoints.

	log.Printf("Running recognition embedding (w600k_r50.onnx) for %d face(s)…", len(detBoxes))

	origW := float32(srcImg.Bounds().Dx())
	origH := float32(srcImg.Bounds().Dy())

	for i, b := range detBoxes {
		// Scale detection box back to original image coordinates.
		scaleX := origW / float32(detW)
		scaleY := origH / float32(detH)

		x1 := int(b.X1 * scaleX)
		y1 := int(b.Y1 * scaleY)
		x2 := int(b.X2 * scaleX)
		y2 := int(b.Y2 * scaleY)

		// Clamp to image bounds.
		if x1 < 0 {
			x1 = 0
		}
		if y1 < 0 {
			y1 = 0
		}
		if x2 > srcImg.Bounds().Dx() {
			x2 = srcImg.Bounds().Dx()
		}
		if y2 > srcImg.Bounds().Dy() {
			y2 = srcImg.Bounds().Dy()
		}

		if x2 <= x1 || y2 <= y1 {
			log.Printf("  Face %d: bounding box is degenerate after scaling, skipping.", i)
			continue
		}

		crop := cropImage(srcImg, x1, y1, x2, y2)
		recInput, err := preprocessArcFace(crop)
		if err != nil {
			log.Printf("  Face %d: preprocessArcFace error: %v", i, err)
			continue
		}

		t0 := time.Now()
		embedding, err := runRecognition(recModel, recInput)
		if err != nil {
			log.Printf("  Face %d: runRecognition error: %v", i, err)
			continue
		}
		elapsed := time.Since(t0)

		// Print a summary of the embedding (first 8 values + L2 norm).
		norm := l2Norm(embedding)
		log.Printf("  Face %d: embedding[0:8]=%v … norm=%.4f (took %s)",
			i, floatSlicePreview(embedding, 8), norm, elapsed)
	}

	// ── 5. Cosine similarity demo (two faces) ─────────────────────────────
	//
	// If we found at least two faces, compute the cosine similarity between
	// the first two embeddings as a sanity check. Identical faces → ~1.0,
	// different people → typically < 0.4 with the default threshold.

	if len(detBoxes) >= 2 {
		log.Println("\nBonus: computing cosine similarity between face 0 and face 1…")
		// Re-run both to collect embeddings.
		embs := make([][]float32, 2)
		for i := 0; i < 2; i++ {
			b := detBoxes[i]
			scaleX := origW / float32(detW)
			scaleY := origH / float32(detH)
			x1 := clampInt(int(b.X1*scaleX), 0, int(origW))
			y1 := clampInt(int(b.Y1*scaleY), 0, int(origH))
			x2 := clampInt(int(b.X2*scaleX), 0, int(origW))
			y2 := clampInt(int(b.Y2*scaleY), 0, int(origH))

			crop := cropImage(srcImg, x1, y1, x2, y2)
			recInput, err := preprocessArcFace(crop)
			if err != nil {
				continue
			}
			emb, err := runRecognition(recModel, recInput)
			if err != nil {
				continue
			}
			embs[i] = emb
		}
		if embs[0] != nil && embs[1] != nil {
			sim := cosineSimilarity(embs[0], embs[1])
			log.Printf("Cosine similarity (face 0 vs face 1): %.4f  (threshold ~0.40)", sim)
		}
	}

	log.Println("\nSpike complete. See README.md for next steps.")
}

// ── Model execution ──────────────────────────────────────────────────────────

// DetBox holds a single detection result.
type DetBox struct {
	X1, Y1, X2, Y2 float32
	Score          float32
}

// runDetection runs the SCRFD face detector and returns filtered bounding boxes.
// It uses a simplified post-processing approach: since SCRFD outputs stride-based
// feature maps, we demonstrate loading the session and running inference, then
// interpret the score tensors to filter detections above a threshold.
func runDetection(modelPath string, inputData []float32, imgW, imgH int) ([]DetBox, error) {
	inputShape := ort.NewShape(1, 3, int64(imgH), int64(imgW))
	inputTensor, err := ort.NewTensor(inputShape, inputData)
	if err != nil {
		return nil, fmt.Errorf("creating input tensor: %w", err)
	}
	defer inputTensor.Destroy()

	// SCRFD outputs 9 tensors for 3 strides (8, 16, 32):
	//   score_8, score_16, score_32   — confidence scores
	//   bbox_8,  bbox_16,  bbox_32    — bounding box offsets
	//   kps_8,   kps_16,   kps_32    — keypoint offsets
	//
	// Anchor counts per stride for a 640×640 input:
	//   stride 8  → (640/8)²  × 2 = 12800 anchors
	//   stride 16 → (640/16)² × 2 =  3200 anchors
	//   stride 32 → (640/32)² × 2 =   800 anchors
	//
	// For the spike we only decode strides 8 and 16 (covers most real faces).

	const (
		stride8Anchors  = 12800
		stride16Anchors = 3200
		stride32Anchors = 800
		numKps          = 5
	)

	// Pre-allocate output tensors.
	score8, err := ort.NewEmptyTensor[float32](ort.NewShape(stride8Anchors, 1))
	if err != nil {
		return nil, err
	}
	defer score8.Destroy()

	bbox8, err := ort.NewEmptyTensor[float32](ort.NewShape(stride8Anchors, 4))
	if err != nil {
		return nil, err
	}
	defer bbox8.Destroy()

	kps8, err := ort.NewEmptyTensor[float32](ort.NewShape(stride8Anchors, numKps*2))
	if err != nil {
		return nil, err
	}
	defer kps8.Destroy()

	score16, err := ort.NewEmptyTensor[float32](ort.NewShape(stride16Anchors, 1))
	if err != nil {
		return nil, err
	}
	defer score16.Destroy()

	bbox16, err := ort.NewEmptyTensor[float32](ort.NewShape(stride16Anchors, 4))
	if err != nil {
		return nil, err
	}
	defer bbox16.Destroy()

	kps16, err := ort.NewEmptyTensor[float32](ort.NewShape(stride16Anchors, numKps*2))
	if err != nil {
		return nil, err
	}
	defer kps16.Destroy()

	score32, err := ort.NewEmptyTensor[float32](ort.NewShape(stride32Anchors, 1))
	if err != nil {
		return nil, err
	}
	defer score32.Destroy()

	bbox32, err := ort.NewEmptyTensor[float32](ort.NewShape(stride32Anchors, 4))
	if err != nil {
		return nil, err
	}
	defer bbox32.Destroy()

	kps32, err := ort.NewEmptyTensor[float32](ort.NewShape(stride32Anchors, numKps*2))
	if err != nil {
		return nil, err
	}
	defer kps32.Destroy()

	// Input/output name conventions for det_10g.onnx from buffalo_l.
	// Names were obtained by inspecting the ONNX graph; they vary by model version.
	// buffalo_l v1 used symbolic names (score_8, bbox_8, …); later releases use
	// numeric node IDs.  Order is stride-8 (score, bbox, kps), stride-16, stride-32.
	inputNames := []string{"input.1"}
	// Model outputs are grouped by type, then by stride:
	//   score_8, score_16, score_32, bbox_8, bbox_16, bbox_32, kps_8, kps_16, kps_32
	// The tensor slice must follow the same order.
	outputNames := []string{
		"448", "471", "494", // scores: stride 8, 16, 32
		"451", "474", "497", // bboxes: stride 8, 16, 32
		"454", "477", "500", // kps:    stride 8, 16, 32
	}

	session, err := ort.NewAdvancedSession(modelPath,
		inputNames, outputNames,
		[]ort.Value{inputTensor},
		[]ort.Value{score8, score16, score32, bbox8, bbox16, bbox32, kps8, kps16, kps32},
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("creating detection session: %w\n\n"+
			"TIP: if this fails with 'invalid node output name', run\n"+
			"     python3 -c \"import onnx; m=onnx.load('det_10g.onnx'); print([n.name for n in m.graph.output])\"\n"+
			"to find the actual output tensor names and adjust outputNames above.", err)
	}
	defer session.Destroy()

	t0 := time.Now()
	if err := session.Run(); err != nil {
		return nil, fmt.Errorf("running detection: %w", err)
	}
	log.Printf("  Detection inference took %s", time.Since(t0))

	// Decode detections.
	const scoreThreshold = 0.5
	var boxes []DetBox
	boxes = append(boxes, decodeAnchors(score8.GetData(), bbox8.GetData(), 8, imgW, imgH, scoreThreshold)...)
	boxes = append(boxes, decodeAnchors(score16.GetData(), bbox16.GetData(), 16, imgW, imgH, scoreThreshold)...)
	boxes = append(boxes, decodeAnchors(score32.GetData(), bbox32.GetData(), 32, imgW, imgH, scoreThreshold)...)

	return nmsBoxes(boxes, 0.4), nil
}

// runRecognition runs the ArcFace model and returns the 512-dim embedding.
func runRecognition(modelPath string, inputData []float32) ([]float32, error) {
	inputShape := ort.NewShape(1, 3, 112, 112)
	inputTensor, err := ort.NewTensor(inputShape, inputData)
	if err != nil {
		return nil, fmt.Errorf("creating recognition input tensor: %w", err)
	}
	defer inputTensor.Destroy()

	outputTensor, err := ort.NewEmptyTensor[float32](ort.NewShape(1, 512))
	if err != nil {
		return nil, fmt.Errorf("creating recognition output tensor: %w", err)
	}
	defer outputTensor.Destroy()

	// The output node name for w600k_r50.onnx is typically "683".
	// Adjust if needed (see TIP in runDetection above).
	session, err := ort.NewAdvancedSession(modelPath,
		[]string{"input.1"},
		[]string{"683"},
		[]ort.Value{inputTensor},
		[]ort.Value{outputTensor},
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("creating recognition session: %w", err)
	}
	defer session.Destroy()

	if err := session.Run(); err != nil {
		return nil, fmt.Errorf("running recognition: %w", err)
	}

	// Copy and L2-normalise the 512-dim embedding.
	raw := outputTensor.GetData()
	emb := make([]float32, 512)
	copy(emb, raw)
	l2NormalizeInPlace(emb)
	return emb, nil
}

// ── Image preprocessing ──────────────────────────────────────────────────────

// preprocessSCRFD resizes the image to (w, h), converts to float32 (C,H,W),
// and applies mean/std normalisation: (pixel - 127.5) / 128.0.
func preprocessSCRFD(img image.Image, w, h int) ([]float32, error) {
	resized := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.BiLinear.Scale(resized, resized.Bounds(), img, img.Bounds(), draw.Over, nil)

	data := make([]float32, 3*h*w)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			r, g, b, _ := resized.At(x, y).RGBA()
			fr := float32(r >> 8)
			fg := float32(g >> 8)
			fb := float32(b >> 8)
			// BGR ordering (InsightFace convention).
			idx := y*w + x
			data[0*h*w+idx] = (fb - 127.5) / 128.0
			data[1*h*w+idx] = (fg - 127.5) / 128.0
			data[2*h*w+idx] = (fr - 127.5) / 128.0
		}
	}
	return data, nil
}

// preprocessArcFace resizes the crop to 112×112 and applies the same
// normalisation as preprocessSCRFD.
func preprocessArcFace(img image.Image) ([]float32, error) {
	const size = 112
	resized := image.NewRGBA(image.Rect(0, 0, size, size))
	draw.BiLinear.Scale(resized, resized.Bounds(), img, img.Bounds(), draw.Over, nil)

	data := make([]float32, 3*size*size)
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			r, g, b, _ := resized.At(x, y).RGBA()
			fr := float32(r >> 8)
			fg := float32(g >> 8)
			fb := float32(b >> 8)
			// BGR.
			idx := y*size + x
			data[0*size*size+idx] = (fb - 127.5) / 128.0
			data[1*size*size+idx] = (fg - 127.5) / 128.0
			data[2*size*size+idx] = (fr - 127.5) / 128.0
		}
	}
	return data, nil
}

// ── Anchor decoding ───────────────────────────────────────────────────────────

// decodeAnchors converts raw SCRFD output tensors into DetBox values.
// SCRFD generates two anchors per cell; scores are sigmoid-activated.
func decodeAnchors(scores, bboxes []float32, stride, imgW, imgH int, threshold float32) []DetBox {
	fmW := imgW / stride
	_ = imgH // imgH used only for anchor centre calculation
	var out []DetBox
	// buffalo_l det_10g.onnx has sigmoid baked into the score outputs,
	// so values are already probabilities in [0, 1]. Do NOT apply sigmoid again.
	for idx := 0; idx < len(scores); idx++ {
		score := scores[idx]
		if score < threshold {
			continue
		}
		// Anchor centre.
		row := idx / (fmW * 2)
		col := (idx % (fmW * 2)) / 2
		anchor := idx % 2

		cx := float32(col*stride + stride/2 + anchor*stride/2)
		cy := float32(row*stride + stride/2)

		// Decode bbox: (dx1, dy1, dx2, dy2) relative to anchor, scaled by stride.
		b := idx * 4
		if b+3 >= len(bboxes) {
			continue
		}
		out = append(out, DetBox{
			X1:    cx - bboxes[b+0]*float32(stride),
			Y1:    cy - bboxes[b+1]*float32(stride),
			X2:    cx + bboxes[b+2]*float32(stride),
			Y2:    cy + bboxes[b+3]*float32(stride),
			Score: score,
		})
	}
	return out
}

// ── Math helpers ─────────────────────────────────────────────────────────────

func sigmoid(x float32) float32 {
	return float32(1.0 / (1.0 + math.Exp(-float64(x))))
}

func l2Norm(v []float32) float32 {
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	return float32(math.Sqrt(sum))
}

func l2NormalizeInPlace(v []float32) {
	n := l2Norm(v)
	if n == 0 {
		return
	}
	for i := range v {
		v[i] /= n
	}
}

func cosineSimilarity(a, b []float32) float32 {
	// Assumes both vectors are already L2-normalised.
	var dot float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
	}
	return float32(dot)
}

// nmsBoxes performs greedy NMS and returns surviving boxes.
func nmsBoxes(boxes []DetBox, iouThreshold float32) []DetBox {
	// Sort by score descending.
	for i := 0; i < len(boxes); i++ {
		for j := i + 1; j < len(boxes); j++ {
			if boxes[j].Score > boxes[i].Score {
				boxes[i], boxes[j] = boxes[j], boxes[i]
			}
		}
	}
	var keep []DetBox
	suppressed := make([]bool, len(boxes))
	for i, b := range boxes {
		if suppressed[i] {
			continue
		}
		keep = append(keep, b)
		for j := i + 1; j < len(boxes); j++ {
			if !suppressed[j] && iou(b, boxes[j]) > iouThreshold {
				suppressed[j] = true
			}
		}
	}
	return keep
}

func iou(a, b DetBox) float32 {
	interX1 := max32(a.X1, b.X1)
	interY1 := max32(a.Y1, b.Y1)
	interX2 := min32(a.X2, b.X2)
	interY2 := min32(a.Y2, b.Y2)
	if interX2 < interX1 || interY2 < interY1 {
		return 0
	}
	inter := (interX2 - interX1) * (interY2 - interY1)
	areaA := (a.X2 - a.X1) * (a.Y2 - a.Y1)
	areaB := (b.X2 - b.X1) * (b.Y2 - b.Y1)
	return inter / (areaA + areaB - inter)
}

func max32(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}

func min32(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// ── Image helpers ─────────────────────────────────────────────────────────────

func loadImage(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	return img, err
}

func cropImage(img image.Image, x1, y1, x2, y2 int) image.Image {
	type subImager interface {
		SubImage(r image.Rectangle) image.Image
	}
	if si, ok := img.(subImager); ok {
		return si.SubImage(image.Rect(x1, y1, x2, y2))
	}
	// Fallback: copy pixels.
	w := x2 - x1
	h := y2 - y1
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			dst.Set(x, y, img.At(x1+x, y1+y))
		}
	}
	return dst
}

func floatSlicePreview(v []float32, n int) []float32 {
	if n > len(v) {
		n = len(v)
	}
	return v[:n]
}

// embeddingToBytes serialises a []float32 as little-endian bytes.
// This is the format to store in the database's embedding BLOB column.
func embeddingToBytes(emb []float32) []byte {
	b := make([]byte, len(emb)*4)
	for i, f := range emb {
		binary.LittleEndian.PutUint32(b[i*4:], math.Float32bits(f))
	}
	return b
}

// bytesToEmbedding deserialises a BLOB back to []float32.
func bytesToEmbedding(b []byte) []float32 {
	emb := make([]float32, len(b)/4)
	for i := range emb {
		bits := binary.LittleEndian.Uint32(b[i*4:])
		emb[i] = math.Float32frombits(bits)
	}
	return emb
}
