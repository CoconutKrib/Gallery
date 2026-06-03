# onnxruntime_spike

A self-contained Go spike to explore the [`yalue/onnxruntime_go`](https://github.com/yalue/onnxruntime_go) API and the InsightFace `buffalo_l` ONNX models before integrating face detection + recognition into Gallery.

This module is **intentionally kept out of the main Gallery module** (underscore prefix prevents `./...` from picking it up). It has its own `go.mod`.

---

## Prerequisites

### 1 — ONNX Runtime shared library

Download the **Linux x64** GPU or CPU release from:

```
https://github.com/microsoft/onnxruntime/releases
```

E.g. `onnxruntime-linux-x64-1.21.0.tgz` (CPU) or `onnxruntime-linux-x64-gpu-1.21.0.tgz` (CUDA).

Extract and note the full path to `libonnxruntime.so.1.x.x`.

```bash
tar xzf onnxruntime-linux-x64-1.21.0.tgz
export ONNXRUNTIME_LIB=$PWD/onnxruntime-linux-x64-1.21.0/lib/libonnxruntime.so.1.21.0
```

### 2 — InsightFace `buffalo_l` model pack

#### Option A — via Python insightface package

```bash
pip install insightface onnxruntime
python3 -c "
import insightface
app = insightface.app.FaceAnalysis(name='buffalo_l')
app.prepare(ctx_id=0)   # downloads models to ~/.insightface/models/buffalo_l/
"
export BUFFALO_L_DIR=~/.insightface/models/buffalo_l
```

#### Option B — direct download

```bash
# Find the buffalo_l.zip asset in the InsightFace GitHub releases:
#   https://github.com/deepinsight/insightface/releases
unzip buffalo_l.zip -d buffalo_l/
export BUFFALO_L_DIR=$PWD/buffalo_l
```

Required files in the directory:

| File             | Description                          | Licence                              |
|------------------|--------------------------------------|--------------------------------------|
| `det_10g.onnx`   | SCRFD-10GF face detector             | Apache 2.0                           |
| `w600k_r50.onnx` | ArcFace ResNet-50 recognition model  | Non-commercial research only ⚠️      |

> **Licence note**: `w600k_r50.onnx` is provided by InsightFace under their non-commercial research licence. Ensure you have the right to use this model for your intended purpose before deploying it.

### 3 — A test image

Any JPEG or PNG file containing at least one face. The Gallery sample photos do _not_ contain faces; use your own.

---

## Building

```bash
cd _exploration/onnxruntime_spike
go mod tidy          # fetches yalue/onnxruntime_go and golang.org/x/image
go build .           # requires gcc; does NOT need onnxruntime headers installed
```

---

## Running

```bash
./onnxruntime_spike \
    -lib    "$ONNXRUNTIME_LIB" \
    -models "$BUFFALO_L_DIR" \
    -img    /path/to/test.jpg
```

Or use the environment variable shortcut for the library path:

```bash
ONNXRUNTIME_LIB="$ONNXRUNTIME_LIB" \
./onnxruntime_spike -models "$BUFFALO_L_DIR" -img /path/to/test.jpg
```

### Expected output (two-face image)

```
Loading ONNX Runtime from: /…/libonnxruntime.so.1.21.0
ONNX Runtime environment ready.
Loading image: /path/to/test.jpg
Image size: 1280x960
Running face detection (det_10g.onnx) at 640x640…
  Detection inference took 423ms
Detected 2 face(s).
  Face 0: x1=312.3 y1=105.7 x2=445.8 y2=278.3 score=0.978
  Face 1: x1=720.1 y1=120.4 x2=851.2 y2=295.6 score=0.944
Running recognition embedding (w600k_r50.onnx) for 2 face(s)…
  Face 0: embedding[0:8]=[-0.0821 0.0413 … ] … norm=1.0000 (took 310ms)
  Face 1: embedding[0:8]=[-0.0311 0.0872 … ] … norm=1.0000 (took 295ms)

Bonus: computing cosine similarity between face 0 and face 1…
Cosine similarity (face 0 vs face 1): 0.2847  (threshold ~0.40)
```

---

## Troubleshooting

### `invalid node output name` or session creation failure

The output node names in the spike (`score_8`, `bbox_8`, etc. for the detector;
`683` for the recogniser) were obtained from the buffalo_l pack released by
InsightFace. Different versions may have different names.

To inspect your model:

```bash
pip install onnx
python3 - <<'EOF'
import onnx
m = onnx.load('det_10g.onnx')
print("INPUTS:", [n.name for n in m.graph.input])
print("OUTPUTS:", [n.name for n in m.graph.output])
EOF
```

Then update `outputNames` in `runDetection` / `runRecognition` accordingly.

### CUDA / GPU

If you have a CUDA-capable GPU and downloaded the GPU build of ONNX Runtime,
the runtime will automatically try CUDA first and fall back to CPU if CUDA is
unavailable. No code changes are needed.

---

## What this teaches us for Gallery Phase B/C

1. **Runtime loading** — `ort.SetSharedLibraryPath` + `ort.InitializeEnvironment` work without needing onnxruntime headers at build time; only a C compiler is required (for the CGO wrapper bundled in `yalue/onnxruntime_go`).

2. **Session lifecycle** — sessions can be created once at startup and reused across calls. `session.Destroy()` must be called when done.

3. **Tensor ownership** — `NewTensor` / `NewEmptyTensor` allocate C-side memory; always call `.Destroy()` (use `defer`).

4. **Preprocessing** — SCRFD and ArcFace both use `(pixel_bgr - 127.5) / 128.0` normalisation and CHW channel ordering. The spike uses simple bilinear resize; Phase B should add a proper similarity-transform alignment using the 5 keypoints from SCRFD to improve recognition accuracy.

5. **Embedding storage** — `embeddingToBytes` / `bytesToEmbedding` show how to round-trip a 512-dim float32 vector through the `embedding BLOB` column in the Gallery DB.

6. **Cosine similarity threshold** — `~0.40` is the InsightFace default for same-person detection with `w600k_r50`. This maps to the `recognition_threshold` field in `FaceRecognitionConfig`.
