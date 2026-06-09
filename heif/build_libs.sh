#!/usr/bin/env bash
# Build libde265 and libheif as static libraries for bundling into the gallery binary.
#
# Usage:   ./heif/build_libs.sh
# Output:  heif/lib/linux-x64/libde265.a, libheif.a
#          heif/include/ (updated headers)
#
# Requires: cmake, g++, make, git
#
# Pinned versions:
#   libde265 v1.0.15  (HEVC decoder)
#   libheif  v1.18.2  (HEIF container + EXIF extraction)

set -euo pipefail
cd "$(dirname "$0")"

LIBDE265_VERSION="1.0.15"
LIBHEIF_VERSION="1.18.2"
BUILD_DIR="$(pwd)/_build"
OUT_DIR="$(pwd)/lib/linux-x64"
INC_DIR="$(pwd)/include"

rm -rf "$BUILD_DIR"
mkdir -p "$BUILD_DIR" "$OUT_DIR" "$INC_DIR"

echo "=== Building libde265 v${LIBDE265_VERSION} ==="
cd "$BUILD_DIR"
git clone --depth 1 --branch "v${LIBDE265_VERSION}" https://github.com/strukturag/libde265.git
cd libde265
mkdir build && cd build
cmake .. \
    -DCMAKE_POSITION_INDEPENDENT_CODE=ON \
    -DBUILD_SHARED_LIBS=OFF \
    -DDISABLE_SSE=OFF \
    -DCMAKE_BUILD_TYPE=Release
make -j"$(nproc)"
cp libde265/libde265.a "$OUT_DIR/"
cp ../libde265/de265.h ../libde265/de265-version.h "$INC_DIR/"
echo "   libde265.a built ($(stat -c%s "$OUT_DIR/libde265.a") bytes)"

echo "=== Building libheif v${LIBHEIF_VERSION} ==="
cd "$BUILD_DIR"
git clone --depth 1 --branch "v${LIBHEIF_VERSION}" https://github.com/strukturag/libheif.git
cd libheif
mkdir build && cd build
cmake .. \
    -DCMAKE_POSITION_INDEPENDENT_CODE=ON \
    -DBUILD_SHARED_LIBS=OFF \
    -DCMAKE_BUILD_TYPE=Release \
    -DWITH_LIBDE265=ON \
    -DWITH_X265=OFF \
    -DWITH_AOM_DECODER=OFF \
    -DWITH_AOM_ENCODER=OFF \
    -DWITH_DAV1D=OFF \
    -DWITH_RAV1E=OFF \
    -DWITH_SvtEnc=OFF \
    -DWITH_EXAMPLES=OFF \
    -DWITH_GDK_PIXBUF=OFF \
    -DENABLE_PLUGIN_LOADING=OFF \
    -DLIBDE265_INCLUDE_DIR="$BUILD_DIR/libde265" \
    -DLIBDE265_LIBRARY="$OUT_DIR/libde265.a"
make -j"$(nproc)"
cp libheif/libheif.a "$OUT_DIR/"
cp ../libheif/heif.h ../libheif/heif_version.h "$INC_DIR/"
echo "   libheif.a built ($(stat -c%s "$OUT_DIR/libheif.a") bytes)"

echo ""
echo "=== Done ==="
echo "Static libraries:"
ls -lh "$OUT_DIR/"
echo ""
echo "Headers:"
ls -lh "$INC_DIR/"
echo ""
echo "You can now build the gallery binary with: go build ./..."
