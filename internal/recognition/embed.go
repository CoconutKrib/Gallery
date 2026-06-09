package recognition

import (
	"fmt"
	"image"
	"sync"

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
	mu          sync.Mutex
	session     *ort.DynamicAdvancedSession
	opened      bool
}

func newEmbedder(modelPath string, opts *ort.SessionOptions) (*Embedder, error) {
	if err := validateEmbedModel(modelPath, opts); err != nil {
		return nil, fmt.Errorf("embedding model validation failed: %w", err)
	}
	return &Embedder{modelPath: modelPath, sessionOpts: opts}, nil
}

// Open creates the persistent ONNX session. Idempotent.
func (e *Embedder) Open() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.opened {
		return nil
	}
	session, err := ort.NewDynamicAdvancedSession(e.modelPath,
		[]string{"input.1"},
		[]string{"683"},
		e.sessionOpts,
	)
	if err != nil {
		return fmt.Errorf("opening embedding session: %w", err)
	}
	e.session = session
	e.opened = true
	return nil
}

// Close destroys the persistent ONNX session. Idempotent.
func (e *Embedder) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.opened {
		return nil
	}
	err := e.session.Destroy()
	e.session = nil
	e.opened = false
	return err
}

// Embed crops the face described by det (in 640×640 detection space) from img
// (at its original dimensions origW×origH), resizes to 112×112, and returns
// an L2-normalised 512-dim ArcFace embedding.
// Uses a persistent session created by Open(); creates one lazily if not opened.
func (e *Embedder) Embed(img image.Image, det Detection, origW, origH int) ([]float32, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Lazy-open if needed.
	if !e.opened {
		if err := e.openLocked(); err != nil {
			return nil, err
		}
	}

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

	if err := e.session.Run([]ort.Value{inputTensor}, []ort.Value{outputTensor}); err != nil {
		return nil, fmt.Errorf("running embedding: %w", err)
	}

	raw := outputTensor.GetData()
	emb := make([]float32, embDim)
	copy(emb, raw)
	l2NormalizeInPlace(emb)
	return emb, nil
}

// openLocked creates the persistent session. Caller must hold e.mu.
func (e *Embedder) openLocked() error {
	session, err := ort.NewDynamicAdvancedSession(e.modelPath,
		[]string{"input.1"},
		[]string{"683"},
		e.sessionOpts,
	)
	if err != nil {
		return fmt.Errorf("opening embedding session: %w", err)
	}
	e.session = session
	e.opened = true
	return nil
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
