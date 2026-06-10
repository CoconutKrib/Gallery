#!/usr/bin/env bash
# package.sh — Build the gallery binary and create a distributable .tar.gz
#
# The tarball includes:
#   gallery              — the Go binary (statically linked, web assets embedded)
#   lib/                 — ONNX runtime shared libraries (face recognition)
#   models/              — InsightFace ONNX model files
#   config.json.example  — default config template with relative paths
#
# Usage:
#   ./package.sh                 # CPU build (default, smaller ~15 MB lib)
#   ./package.sh --gpu           # GPU build (includes CUDA/TensorRT, ~440 MB lib)
#   ./package.sh --version X     # override version (default: git describe or date)
#
# ONNX runtime variants:
#   By default this script packages the CPU-only build of libonnxruntime.so
#   (~15 MB). Use --gpu to package the GPU build (~440 MB) which enables CUDA
#   and TensorRT acceleration for face recognition.
#
#   To use the CPU build, download the CPU package into onnx/:
#     wget https://github.com/microsoft/onnxruntime/releases/download/v1.26.0/\
# onnxruntime-linux-x64-1.26.0.tgz
#     tar -xzf onnxruntime-linux-x64-1.26.0.tgz -C onnx/
#
#   To use the GPU build (CUDA 12, TensorRT), download:
#     wget https://github.com/microsoft/onnxruntime/releases/download/v1.26.0/\
# onnxruntime-linux-x64-gpu-1.26.0.tgz
#     tar -xzf onnxruntime-linux-x64-gpu-1.26.0.tgz -C onnx/
#
# The HEIC decode libraries (libheif, libde265) are statically linked into the
# binary, so no .so files are needed for HEIC support. SQLite is pure Go
# (modernc.org/sqlite), so no libsqlite3 needed either.

set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
cd "$SCRIPT_DIR"

# --- Defaults ---
USE_GPU=false
VERSION="${VERSION:-}"

# --- Parse args ---
while [ $# -gt 0 ]; do
    case "$1" in
        --gpu)    USE_GPU=true; shift ;;
        --version) VERSION="$2"; shift 2 ;;
        *) echo "Unknown option: $1"; exit 1 ;;
    esac
done

if [ -z "$VERSION" ]; then
    VERSION=$(git describe --tags --always --dirty 2>/dev/null || date +%Y%m%d-%H%M%S)
fi

PKG_NAME="gallery-${VERSION}"
BUILD_DIR="build/${PKG_NAME}"
ONNX_VARIANT="cpu"

echo "=== Packaging gallery ${VERSION} ==="

# --- Clean previous build dir ---
rm -rf "build/${PKG_NAME}" "build/${PKG_NAME}.tar.gz"

# --- Step 1: Build the binary ---
echo "[1/4] Building gallery binary..."
go build -ldflags="-s -w" -o "${BUILD_DIR}/gallery" .
echo "       Binary: ${BUILD_DIR}/gallery ($(du -h "${BUILD_DIR}/gallery" | cut -f1))"

# --- Step 2: Locate and copy ONNX runtime shared libraries ---
echo "[2/4] Copying ONNX runtime libraries..."

# Find the best-matching ONNX runtime directory.
# Prefer the highest version that matches the requested variant (cpu or gpu).
find_onnx_dir() {
    local variant="$1"  # "cpu" or "gpu"
    local pattern
    if [ "$variant" = "gpu" ]; then
        pattern="onnxruntime-linux-x64-gpu-*"
    else
        # CPU: match "onnxruntime-linux-x64-*" but NOT the GPU ones
        pattern="onnxruntime-linux-x64-[0-9]*"
    fi
    # List matching dirs, sort by version descending, take the first.
    local dir
    dir=$(find onnx/ -maxdepth 1 -type d -name "$pattern" 2>/dev/null | sort -Vr | head -1)
    echo "$dir"
}

ONNX_DIR=""
if $USE_GPU; then
    ONNX_VARIANT="gpu"
    ONNX_DIR=$(find_onnx_dir "gpu")
    if [ -z "$ONNX_DIR" ]; then
        echo "       ERROR: No GPU ONNX runtime found in onnx/"
        echo "       Download it from:"
        echo "         https://github.com/microsoft/onnxruntime/releases"
        echo "       Look for: onnxruntime-linux-x64-gpu-<version>.tgz"
        echo "       Extract with: tar -xzf onnxruntime-linux-x64-gpu-*.tgz -C onnx/"
        exit 1
    fi
else
    ONNX_DIR=$(find_onnx_dir "cpu")
    if [ -z "$ONNX_DIR" ]; then
        # Fall back to GPU if no CPU build is present.
        ONNX_DIR=$(find_onnx_dir "gpu")
        if [ -n "$ONNX_DIR" ]; then
            echo "       NOTE: No CPU ONNX runtime found, falling back to GPU build."
            echo "       For a smaller package (~15 MB lib vs ~440 MB), download the CPU build:"
            echo "         wget https://github.com/microsoft/onnxruntime/releases/download/v1.26.0/onnxruntime-linux-x64-1.26.0.tgz"
            echo "         tar -xzf onnxruntime-linux-x64-1.26.0.tgz -C onnx/"
            echo "       Then re-run without --gpu."
            ONNX_VARIANT="gpu (fallback)"
        else
            echo "       ERROR: No ONNX runtime found in onnx/"
            echo "       Download the CPU build (~15 MB):"
            echo "         wget https://github.com/microsoft/onnxruntime/releases/download/v1.26.0/onnxruntime-linux-x64-1.26.0.tgz"
            echo "         tar -xzf onnxruntime-linux-x64-1.26.0.tgz -C onnx/"
            echo "       Or the GPU build (~440 MB, for CUDA acceleration):"
            echo "         wget https://github.com/microsoft/onnxruntime/releases/download/v1.26.0/onnxruntime-linux-x64-gpu-1.26.0.tgz"
            echo "         tar -xzf onnxruntime-linux-x64-gpu-1.26.0.tgz -C onnx/"
            exit 1
        fi
    fi
fi

ONNX_LIB_DIR="${ONNX_DIR}/lib"
echo "       Using: ${ONNX_DIR} (${ONNX_VARIANT})"
mkdir -p "${BUILD_DIR}/lib"

# Always copy the main library + symlinks.
cp -a "${ONNX_LIB_DIR}"/libonnxruntime.so* "${BUILD_DIR}/lib/"

# Only include GPU provider libs when --gpu is explicitly requested.
if $USE_GPU; then
    cp -a "${ONNX_LIB_DIR}"/libonnxruntime_providers_*.so "${BUILD_DIR}/lib/" 2>/dev/null || true
    echo "       Copied $(ls "${BUILD_DIR}/lib/" | wc -l) shared libraries (GPU) to lib/"
else
    echo "       Copied $(ls "${BUILD_DIR}/lib/" | wc -l) shared libraries (CPU) to lib/"
fi

# --- Step 3: Copy InsightFace model files ---
# Only the two models actually used by the recognition pipeline:
#   det_10g.onnx    — SCRFD face detection (17 MB)
#   w600k_r50.onnx  — ArcFace face embedding (167 MB)
# The other buffalo_l models (1k3d68, 2d106det, genderage) are unused by gallery
# and excluded to keep the package smaller (~143 MB saved).
echo "[3/4] Copying InsightFace models..."
MODEL_DIR="insightface/buffalo_l"
REQUIRED_MODELS=("det_10g.onnx" "w600k_r50.onnx")
if [ -d "$MODEL_DIR" ]; then
    mkdir -p "${BUILD_DIR}/models"
    copied=0
    for model in "${REQUIRED_MODELS[@]}"; do
        if [ -f "${MODEL_DIR}/${model}" ]; then
            cp "${MODEL_DIR}/${model}" "${BUILD_DIR}/models/"
            copied=$((copied + 1))
        else
            echo "       WARNING: Required model ${model} not found in ${MODEL_DIR}"
        fi
    done
    echo "       Copied ${copied}/${#REQUIRED_MODELS[@]} model files to models/ (skipping unused: 1k3d68, 2d106det, genderage)"
else
    echo "       WARNING: Models not found at ${MODEL_DIR} — skipping"
    mkdir -p "${BUILD_DIR}/models"
fi

# --- Step 4: Create example config with relative paths ---
echo "[4/4] Creating config template..."
cat > "${BUILD_DIR}/config.json.example" <<'CONFEOF'
{
  "scan_paths": [],
  "camera_whitelist": [],
  "filename_filters": {
    "include": [],
    "exclude": [
      "^\\.",
      "^thumb_",
      "_copy\\.jpe?g$",
      "_n\\.jpe?g$"
    ]
  },
  "auth": {
    "enabled": false,
    "password_hash": "",
    "session_secret": ""
  },
  "db_path": "./gallery.db",
  "cache_dir": "./.cache",
  "log_file": "",
  "log_level": "info",
  "scan_workers": 4,
  "event_gap_days": 2,
  "event_geo_km": 500,
  "session_ttl_hours": 24,
  "internal_library": {
    "path": "",
    "enabled": false
  },
  "dropzone": {
    "path": "",
    "enabled": false
  },
  "face_recognition": {
    "enabled": true,
    "onnxruntime_lib": "./lib/libonnxruntime.so",
    "model_dir": "./models",
    "detection_model": "det_10g.onnx",
    "recognition_model": "w600k_r50.onnx",
    "detection_threshold": 0.8,
    "recognition_threshold": 0.8,
    "cluster_min_samples": 5
  }
}
CONFEOF
echo "       Created config.json.example (relative paths)"

# --- Create tarball ---
echo ""
echo "=== Creating tarball ==="
mkdir -p build
tar -czf "build/${PKG_NAME}.tar.gz" -C build "${PKG_NAME}"
echo "       build/${PKG_NAME}.tar.gz ($(du -h "build/${PKG_NAME}.tar.gz" | cut -f1))"

# --- Print summary ---
echo ""
echo "=== Package contents ==="
tar -tzf "build/${PKG_NAME}.tar.gz" | head -30
echo "       ... ($(tar -tzf "build/${PKG_NAME}.tar.gz" | wc -l) entries total)"
echo ""
echo "Done! To deploy:"
echo "  tar -xzf build/${PKG_NAME}.tar.gz"
echo "  cd ${PKG_NAME}"
echo "  cp config.json.example config.json   # edit paths as needed"
echo "  mkdir -p .cache"
echo "  ./gallery --config config.json"
