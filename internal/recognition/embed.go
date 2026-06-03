package recognition

import (
	"fmt"
	"image"

	ort "github.com/yalue/onnxruntime_go"
)

const (
	arcFaceSize = 112
	embDim      = 512
)

// Embedder wraps an ArcFace recognition model.
type Embedder struct {
	modelPath   string
	sessionOpts *ort.SessionOptions // nil = CPU default
}

func newEmbedder(modelPath string, opts *ort.SessionOptions) (*Embedder, error) {
	if err := validateEmbedModel(modelPath, opts); err != nil {
		return nil, fmt.Errorf("embedding model validation failed: %w", err)
	}
	return &Embedder{modelPath: modelPath, sessionOpts: opts}, nil
}

// Embed crops the face described by det (in 640×640 detection space) from img
// (at its original dimensions origW×origH), resizes to 112×112, and returns
// an L2-normalised 512-dim ArcFace embedding.
func (e *Embedder) Embed(img image.Image, det Detection, origW, origH int) ([]float32, error) {
	// Scale detection box from 640×640 detection space back to original image.
	scaleX := float32(origW) / float32(DetInputW)
	scaleY := float32(origH) / float32(DetInputH)

	x1 := clampInt(int(det.X1*scaleX), 0, origW)
	y1 := clampInt(int(det.Y1*scaleY), 0, origH)
	x2 := clampInt(int(det.X2*scaleX), 0, origW)
	y2 := clampInt(int(det.Y2*scaleY), 0, origH)

	if x2 <= x1 || y2 <= y1 {
		return nil, fmt.Errorf("degenerate bounding box after scaling to original dimensions")
	}

	crop := cropImage(img, x1, y1, x2, y2)
	inputData := preprocessBGRFloat32(crop, arcFaceSize, arcFaceSize)

	inputShape := ort.NewShape(1, 3, arcFaceSize, arcFaceSize)
	inputTensor, err := ort.NewTensor(inputShape, inputData)
	if err != nil {
		return nil, fmt.Errorf("creating embedding input tensor: %w", err)
	}
	defer inputTensor.Destroy()

	outputTensor, err := ort.NewEmptyTensor[float32](ort.NewShape(1, embDim))
	if err != nil {
		return nil, fmt.Errorf("allocating embedding output tensor: %w", err)
	}
	defer outputTensor.Destroy()

	// Output node "683" is the L2-normalised embedding output for w600k_r50.onnx.
	session, err := ort.NewAdvancedSession(e.modelPath,
		[]string{"input.1"},
		[]string{"683"},
		[]ort.Value{inputTensor},
		[]ort.Value{outputTensor},
		e.sessionOpts,
	)
	if err != nil {
		return nil, fmt.Errorf("creating embedding session: %w", err)
	}
	defer session.Destroy()

	if err := session.Run(); err != nil {
		return nil, fmt.Errorf("running embedding: %w", err)
	}

	raw := outputTensor.GetData()
	emb := make([]float32, embDim)
	copy(emb, raw)
	l2NormalizeInPlace(emb) // model outputs are already normalised, but re-normalise for safety
	return emb, nil
}

// validateEmbedModel creates a minimal session to confirm the model is valid.
func validateEmbedModel(modelPath string, opts *ort.SessionOptions) error {
	dummyInput := make([]float32, 3*arcFaceSize*arcFaceSize)
	inputShape := ort.NewShape(1, 3, arcFaceSize, arcFaceSize)
	inputTensor, err := ort.NewTensor(inputShape, dummyInput)
	if err != nil {
		return err
	}
	defer inputTensor.Destroy()

	outputTensor, err := ort.NewEmptyTensor[float32](ort.NewShape(1, embDim))
	if err != nil {
		return err
	}
	defer outputTensor.Destroy()

	session, err := ort.NewAdvancedSession(modelPath,
		[]string{"input.1"},
		[]string{"683"},
		[]ort.Value{inputTensor},
		[]ort.Value{outputTensor},
		opts,
	)
	if err != nil {
		return err
	}
	return session.Destroy()
}
