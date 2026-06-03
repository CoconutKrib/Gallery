package recognition

import (
	"fmt"
	"image"

	ort "github.com/yalue/onnxruntime_go"
)

const (
	DetInputW = 640
	DetInputH = 640

	stride8Anchors  = 12800
	stride16Anchors = 3200
	stride32Anchors = 800
	numKps          = 5
)

// Detection holds a single face detection result in the 640×640 input space.
// Callers must scale X1/Y1/X2/Y2 back to original image coordinates before
// storing bbox_* values in the database.
type Detection struct {
	X1, Y1, X2, Y2 float32       // bounding box (pixels, 640×640 space)
	Score          float32       // confidence in [0, 1]
	Kps            [5][2]float32 // facial keypoints (unused in Phase B)
}

// Detector wraps an SCRFD face-detection model.
type Detector struct {
	modelPath   string
	threshold   float32
	sessionOpts *ort.SessionOptions // nil = CPU default
}

func newDetector(modelPath string, threshold float32, opts *ort.SessionOptions) (*Detector, error) {
	// Validate the model by creating and immediately destroying a test session.
	if err := validateDetectionModel(modelPath, opts); err != nil {
		return nil, fmt.Errorf("detection model validation failed: %w", err)
	}
	return &Detector{modelPath: modelPath, threshold: threshold, sessionOpts: opts}, nil
}

// Detect runs SCRFD face detection on img and returns filtered detections.
// The returned bounding boxes are in the 640×640 detection input space.
func (d *Detector) Detect(img image.Image) ([]Detection, error) {
	inputData := preprocessBGRFloat32(img, DetInputW, DetInputH)

	inputShape := ort.NewShape(1, 3, DetInputH, DetInputW)
	inputTensor, err := ort.NewTensor(inputShape, inputData)
	if err != nil {
		return nil, fmt.Errorf("creating detection input tensor: %w", err)
	}
	defer inputTensor.Destroy()

	score8, err := ort.NewEmptyTensor[float32](ort.NewShape(stride8Anchors, 1))
	if err != nil {
		return nil, fmt.Errorf("allocating score8: %w", err)
	}
	defer score8.Destroy()

	score16, err := ort.NewEmptyTensor[float32](ort.NewShape(stride16Anchors, 1))
	if err != nil {
		return nil, fmt.Errorf("allocating score16: %w", err)
	}
	defer score16.Destroy()

	score32, err := ort.NewEmptyTensor[float32](ort.NewShape(stride32Anchors, 1))
	if err != nil {
		return nil, fmt.Errorf("allocating score32: %w", err)
	}
	defer score32.Destroy()

	bbox8, err := ort.NewEmptyTensor[float32](ort.NewShape(stride8Anchors, 4))
	if err != nil {
		return nil, fmt.Errorf("allocating bbox8: %w", err)
	}
	defer bbox8.Destroy()

	bbox16, err := ort.NewEmptyTensor[float32](ort.NewShape(stride16Anchors, 4))
	if err != nil {
		return nil, fmt.Errorf("allocating bbox16: %w", err)
	}
	defer bbox16.Destroy()

	bbox32, err := ort.NewEmptyTensor[float32](ort.NewShape(stride32Anchors, 4))
	if err != nil {
		return nil, fmt.Errorf("allocating bbox32: %w", err)
	}
	defer bbox32.Destroy()

	kps8, err := ort.NewEmptyTensor[float32](ort.NewShape(stride8Anchors, numKps*2))
	if err != nil {
		return nil, fmt.Errorf("allocating kps8: %w", err)
	}
	defer kps8.Destroy()

	kps16, err := ort.NewEmptyTensor[float32](ort.NewShape(stride16Anchors, numKps*2))
	if err != nil {
		return nil, fmt.Errorf("allocating kps16: %w", err)
	}
	defer kps16.Destroy()

	kps32, err := ort.NewEmptyTensor[float32](ort.NewShape(stride32Anchors, numKps*2))
	if err != nil {
		return nil, fmt.Errorf("allocating kps32: %w", err)
	}
	defer kps32.Destroy()

	// Node names for buffalo_l v2+ det_10g.onnx.
	// Grouped by type then by stride (score, bbox, kps) × (8, 16, 32).
	inputNames := []string{"input.1"}
	outputNames := []string{
		"448", "471", "494", // scores: stride 8, 16, 32
		"451", "474", "497", // bboxes: stride 8, 16, 32
		"454", "477", "500", // kps:    stride 8, 16, 32
	}

	session, err := ort.NewAdvancedSession(d.modelPath,
		inputNames, outputNames,
		[]ort.Value{inputTensor},
		[]ort.Value{score8, score16, score32, bbox8, bbox16, bbox32, kps8, kps16, kps32},
		d.sessionOpts,
	)
	if err != nil {
		return nil, fmt.Errorf("creating detection session: %w", err)
	}
	defer session.Destroy()

	if err := session.Run(); err != nil {
		return nil, fmt.Errorf("running detection: %w", err)
	}

	// Decode and NMS.
	// Scores from det_10g.onnx are already sigmoid-activated (probabilities in
	// [0,1]). Do NOT apply sigmoid again.
	var boxes []rawBox
	boxes = append(boxes, decodeAnchors(score8.GetData(), bbox8.GetData(), kps8.GetData(), 8, DetInputW, d.threshold)...)
	boxes = append(boxes, decodeAnchors(score16.GetData(), bbox16.GetData(), kps16.GetData(), 16, DetInputW, d.threshold)...)
	boxes = append(boxes, decodeAnchors(score32.GetData(), bbox32.GetData(), kps32.GetData(), 32, DetInputW, d.threshold)...)

	return nmsDetections(boxes, 0.4), nil
}

// validateDetectionModel creates a minimal session to confirm the model file
// is valid and the expected node names match.
func validateDetectionModel(modelPath string, opts *ort.SessionOptions) error {
	// Use dummy tensors sized for a 640×640 input.
	dummyInput := make([]float32, 3*DetInputH*DetInputW)
	inputShape := ort.NewShape(1, 3, DetInputH, DetInputW)
	inputTensor, err := ort.NewTensor(inputShape, dummyInput)
	if err != nil {
		return err
	}
	defer inputTensor.Destroy()

	score8, _ := ort.NewEmptyTensor[float32](ort.NewShape(stride8Anchors, 1))
	defer score8.Destroy()
	score16, _ := ort.NewEmptyTensor[float32](ort.NewShape(stride16Anchors, 1))
	defer score16.Destroy()
	score32, _ := ort.NewEmptyTensor[float32](ort.NewShape(stride32Anchors, 1))
	defer score32.Destroy()
	bbox8, _ := ort.NewEmptyTensor[float32](ort.NewShape(stride8Anchors, 4))
	defer bbox8.Destroy()
	bbox16, _ := ort.NewEmptyTensor[float32](ort.NewShape(stride16Anchors, 4))
	defer bbox16.Destroy()
	bbox32, _ := ort.NewEmptyTensor[float32](ort.NewShape(stride32Anchors, 4))
	defer bbox32.Destroy()
	kps8, _ := ort.NewEmptyTensor[float32](ort.NewShape(stride8Anchors, numKps*2))
	defer kps8.Destroy()
	kps16, _ := ort.NewEmptyTensor[float32](ort.NewShape(stride16Anchors, numKps*2))
	defer kps16.Destroy()
	kps32, _ := ort.NewEmptyTensor[float32](ort.NewShape(stride32Anchors, numKps*2))
	defer kps32.Destroy()

	session, err := ort.NewAdvancedSession(modelPath,
		[]string{"input.1"},
		[]string{"448", "471", "494", "451", "474", "497", "454", "477", "500"},
		[]ort.Value{inputTensor},
		[]ort.Value{score8, score16, score32, bbox8, bbox16, bbox32, kps8, kps16, kps32},
		opts,
	)
	if err != nil {
		return err
	}
	return session.Destroy()
}

// ── internal anchor decoding ─────────────────────────────────────────────────

type rawBox struct {
	X1, Y1, X2, Y2 float32
	Score          float32
	Kps            [5][2]float32
}

// decodeAnchors converts raw SCRFD output tensors into rawBox values.
// SCRFD places 2 anchors per feature-map cell.
func decodeAnchors(scores, bboxes, kpsData []float32, stride, imgW int, threshold float32) []rawBox {
	fmW := imgW / stride
	var out []rawBox
	for idx := 0; idx < len(scores); idx++ {
		score := scores[idx]
		if score < threshold {
			continue
		}
		row := idx / (fmW * 2)
		col := (idx % (fmW * 2)) / 2
		anchor := idx % 2

		cx := float32(col*stride + stride/2 + anchor*stride/2)
		cy := float32(row*stride + stride/2)

		b := idx * 4
		if b+3 >= len(bboxes) {
			continue
		}

		box := rawBox{
			X1:    cx - bboxes[b+0]*float32(stride),
			Y1:    cy - bboxes[b+1]*float32(stride),
			X2:    cx + bboxes[b+2]*float32(stride),
			Y2:    cy + bboxes[b+3]*float32(stride),
			Score: score,
		}

		// Decode keypoints if present.
		k := idx * numKps * 2
		if k+numKps*2-1 < len(kpsData) {
			for i := 0; i < numKps; i++ {
				box.Kps[i][0] = cx + kpsData[k+i*2]*float32(stride)
				box.Kps[i][1] = cy + kpsData[k+i*2+1]*float32(stride)
			}
		}

		out = append(out, box)
	}
	return out
}

// nmsDetections applies greedy NMS and converts surviving rawBoxes to Detections.
func nmsDetections(boxes []rawBox, iouThreshold float32) []Detection {
	// Sort descending by score.
	for i := 0; i < len(boxes); i++ {
		for j := i + 1; j < len(boxes); j++ {
			if boxes[j].Score > boxes[i].Score {
				boxes[i], boxes[j] = boxes[j], boxes[i]
			}
		}
	}

	suppressed := make([]bool, len(boxes))
	var out []Detection
	for i, b := range boxes {
		if suppressed[i] {
			continue
		}
		out = append(out, Detection{
			X1: b.X1, Y1: b.Y1, X2: b.X2, Y2: b.Y2,
			Score: b.Score,
			Kps:   b.Kps,
		})
		for j := i + 1; j < len(boxes); j++ {
			if !suppressed[j] && iou(b, boxes[j]) > iouThreshold {
				suppressed[j] = true
			}
		}
	}
	return out
}

func iou(a, b rawBox) float32 {
	x1 := max32(a.X1, b.X1)
	y1 := max32(a.Y1, b.Y1)
	x2 := min32(a.X2, b.X2)
	y2 := min32(a.Y2, b.Y2)
	if x2 < x1 || y2 < y1 {
		return 0
	}
	inter := (x2 - x1) * (y2 - y1)
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
