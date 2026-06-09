# HEIC Support — Feature Specification

## Status: Proposed

---

## 1. Goals

Add transparent HEIC (High Efficiency Image Container) support throughout the
ingest pipeline. HEIC photos from iPhones and other modern cameras should be
scanned, hashed, thumbnailed, EXIF-tagged, face-detected, and browsable just
like JPEGs — with no extra runtime dependencies beyond what is already bundled
in the repo.

### Non-goals

- HEIC **encoding** (the app never writes HEIC; thumbnails are always JPEG)
- HEIF variants other than HEIC (AVIF, HIF, etc.) — these share the same
  container but use different codecs; limited to HEIC/HEVC for now
- Transcoding HEIC originals to JPEG on ingest (originals stay as-is)
- On-the-fly transcoding for browsers that don't render HEIC (serve the
  pre-generated JPEG thumbnail instead)

---

## 2. Design: bundled static linking via CGO shim

### 2.1 Philosophy

The project already vendors `libonnxruntime.so` in `onnx/` for face
recognition. HEIC support follows a similar pattern but goes one step further:
it bundles **static** libraries (`.a` files) so that HEIC decode capability is
compiled directly into the `gallery` binary. No shared library needs to be
present at runtime.

A thin CGO shim package (`internal/heif/`) wraps the bundled `libheif` +
`libde265` and exposes three functions used by the scan pipeline:

```go
// Decode reads a HEIC image from r and returns a Go image.Image.
func Decode(r io.Reader) (image.Image, error)

// DecodeConfig returns image dimensions without a full decode.
func DecodeConfig(r io.Reader) (image.Config, error)

// ExtractEXIF reads EXIF data from a HEIC file, if present.
// HEIC stores EXIF inside the ISOBMFF container, not in a JPEG APP1
// segment, so goexif cannot find it on its own.
func ExtractEXIF(r io.Reader) ([]byte, error)
```

### 2.2 Why static linking

| Approach | Pros | Cons |
|---|---|---|
| Pure-Go HEIC decoder | No CGO, no system deps | No mature library exists as of 2026 |
| Bundled `.so` + CGO (like onnxruntime) | Simple, matches existing pattern | Still needs `.so` files at runtime; `LD_LIBRARY_PATH` dance |
| **Static `.a` + CGO shim (chosen)** | HEIC compiled into binary; zero runtime deps; clone-and-build | More complex initial setup; `.a` files are larger |

### 2.3 Repository layout

```
gallery/
├── heif/
│   ├── README.md                 # Build instructions for the static libs
│   ├── build_libs.sh             # Script to compile libde265 + libheif as .a
│   └── lib/
│       └── linux-x64/
│           ├── libheif.a         # ~4 MB (committed to git)
│           ├── libde265.a        # ~2 MB (committed to git)
│           └── libheif.pc        # pkg-config file (reference)
│
├── internal/
│   └── heif/
│       ├── heif.go               # CGO shim: Decode, DecodeConfig, ExtractEXIF
│       ├── heif_stub.go          # Build-tag stub when CGO is disabled
│       └── heif_test.go          # Unit tests with a minimal HEIC fixture
```

### 2.4 CGO shim (`internal/heif/heif.go`)

```go
package heif

/*
#cgo LDFLAGS: -L${SRCDIR}/../../heif/lib/linux-x64 -lheif -lde265 -lstdc++ -static-libstdc++ -static-libgcc
#cgo CFLAGS: -I${SRCDIR}/../../heif/include
#include <stdlib.h>
#include <libheif/heif.h>
*/
import "C"

import (
    "bytes"
    "fmt"
    "image"
    "io"
    "unsafe"
)

// Decode reads a HEIC image into a Go image.Image (NRGBA).
func Decode(r io.Reader) (image.Image, error) {
    data, err := io.ReadAll(r)
    if err != nil { return nil, fmt.Errorf("heif: read: %w", err) }
    // ... CGO calls to heif_context_read_from_memory,
    //     heif_decode_image, copy pixels to Go []uint8 ...
    return img, nil
}
```

The shim is deliberately small — just enough CGO to bridge libheif's C API
into Go `image.Image`. The heavy lifting (HEVC bitstream decode, color
conversion) stays inside the bundled static libs.

### 2.5 Build-tag stub for CGO-disabled builds

A `heif_stub.go` with `//go:build !cgo` provides the same API but returns
`ErrNotAvailable` for all calls. This allows `go build` to succeed on
platforms without a C compiler, with HEIC support gracefully absent:

```go
//go:build !cgo

package heif

import (
    "errors"
    "image"
    "io"
)

var ErrNotAvailable = errors.New("heif: not available (CGO disabled)")

func Decode(r io.Reader) (image.Image, error)       { return nil, ErrNotAvailable }
func DecodeConfig(r io.Reader) (image.Config, error) { return image.Config{}, ErrNotAvailable }
func ExtractEXIF(r io.Reader) ([]byte, error)        { return nil, ErrNotAvailable }
```

---

## 3. Libraries to bundle

### 3.1 `libde265` (HEVC decoder)

- **Repo**: https://github.com/strukturag/libde265
- **License**: LGPL-3.0 (statically linked — see §8.1)
- **Role**: Decodes the HEVC (H.265) bitstream inside the HEIC container.
  libheif uses it as a plugin.
- **Build**: `cmake -DBUILD_SHARED_LIBS=OFF -DCMAKE_POSITION_INDEPENDENT_CODE=ON`

### 3.2 `libheif` (HEIF/HEIC container)

- **Repo**: https://github.com/strukturag/libheif
- **License**: LGPL-3.0 (statically linked — see §8.1)
- **Role**: Parses the ISOBMFF container, extracts EXIF/XMP metadata, delegates
  codec work to libde265, provides decoded pixel data.
- **Build**: `cmake -DBUILD_SHARED_LIBS=OFF -DCMAKE_POSITION_INDEPENDENT_CODE=ON -DWITH_LIBDE265=ON -DWITH_X265=OFF -DWITH_AOM=OFF -DWITH_EXAMPLES=OFF`

### 3.3 What we do NOT bundle

| Library | Reason |
|---|---|
| `libx265` | Encoding only — we never encode HEIC |
| `libaom` | AVIF codec — out of scope |
| `libjpeg-turbo` | Already handled by Go stdlib |
| `libpng` | Already handled by Go stdlib |

---

## 4. Changes to the scan pipeline

### 4.1 Extension filter (`internal/scan/scanner.go`)

`isSupportedExtension` gains `.heic` and `.heif` (case-insensitive):

```go
func isSupportedExtension(name string) bool {
    lower := strings.ToLower(name)
    return strings.HasSuffix(lower, ".jpg") ||
        strings.HasSuffix(lower, ".jpeg") ||
        strings.HasSuffix(lower, ".heic") ||
        strings.HasSuffix(lower, ".heif")
}
```

### 4.2 Image decoding (`internal/scan/thumbnail.go`)

`decodeImage` currently calls `image.Decode(f)`. It is refactored to try HEIC
first when the extension matches, then fall through to the standard library:

```go
func decodeImage(path string) (image.Image, error) {
    f, err := os.Open(path)
    if err != nil { return nil, err }
    defer f.Close()

    if isHEICExtension(path) {
        img, err := heif.Decode(f)
        if err == nil {
            return img, nil
        }
        // If the bundled HEIC decoder fails (corrupt file, unsupported
        // variant), log and fall through — the file will be skipped by
        // the caller as a decode error.
        return nil, fmt.Errorf("heic decode: %w", err)
    }
    img, _, err := image.Decode(f)
    return img, err
}
```

Thumbnail output is unchanged — always JPEG at 400px long edge.

### 4.3 EXIF extraction (`internal/scan/exif.go`)

`ReadEXIF` gains a HEIC path. HEIC files store EXIF inside an `Exif` item in
the ISOBMFF container. `goexif` cannot find it natively. The shim's
`ExtractEXIF` reads the raw EXIF bytes, then `goexif.Decode` parses them:

```go
func ReadEXIF(path string) (*EXIFData, error) {
    f, err := os.Open(path)
    if err != nil { return nil, fmt.Errorf("opening file for exif: %w", err) }
    defer f.Close()

    if isHEICExtension(path) {
        return readHEICEXIF(f)
    }
    return readJPEGEXIF(f)  // existing logic, extracted to a helper
}

func readHEICEXIF(f *os.File) (*EXIFData, error) {
    exifBytes, err := heif.ExtractEXIF(f)
    if err != nil || len(exifBytes) == 0 {
        return nil, nil // no EXIF in this HEIC
    }
    x, err := exif.Decode(bytes.NewReader(exifBytes))
    if err != nil {
        return nil, nil
    }
    return parseEXIFFields(x), nil  // existing field extraction, extracted
}
```

All existing EXIF fields (make, model, date, GPS, dimensions, etc.) are
extracted identically — only the source byte stream differs.

### 4.4 Format tracking in the database

A new migration `005_heic.sql` adds a `format` column to `photos`:

```sql
ALTER TABLE photos ADD COLUMN format TEXT NOT NULL DEFAULT 'jpeg';
```

The `Photo` struct, `photoSelectCols`, `InsertPhoto`, `scanPhoto`, and
`scanPhotoRows` in `internal/db/photos.go` are updated accordingly.

The format is determined at ingest time from the file extension:

| Extension | `format` value |
|-----------|---------------|
| `.jpg`, `.jpeg` | `jpeg` |
| `.heic`, `.heif` | `heic` |

### 4.5 Face detection (`internal/scan/scanner.go`)

`detectAndStoreFaces` decodes source images via the same `decodeImage` path
used for thumbnails. With HEIC decode in place, face detection works on HEIC
originals automatically — no additional changes needed.

### 4.6 Hash

SHA-256 hashing (`internal/scan/hash.go`) reads raw file bytes — no changes
needed. A JPEG and a HEIC of the same scene will have different hashes (as
expected — they are different files).

---

## 5. Serving HEIC originals

### 5.1 Problem

Chrome and Firefox do not render HEIC images natively (patent concerns). Only
Safari supports `<img src="photo.heic">`.

### 5.2 Solution: serve the JPEG thumbnail for HEIC originals

`handlePhotoImage` in `internal/api/photos.go` checks the `format` column:

- `format = "jpeg"` → serve the original file (current behavior)
- `format = "heic"` → serve the pre-generated JPEG thumbnail instead

A new query parameter `?original=1` on `/api/photos/{sha256}/image` forces
download of the raw HEIC file (with `Content-Disposition: attachment`), for
users who want the original.

The photo detail JSON already includes both `image_url` and `thumbnail_url`:

```json
{
  "image_url": "/api/photos/abc123/image",
  "thumbnail_url": "/api/photos/abc123/thumbnail",
  "format": "heic"
}
```

Frontend `photo.js` uses `image_url` for the main view and falls back to
`thumbnail_url` if the image fails to load. With the server-side redirect to
the JPEG thumbnail, HEIC photos render correctly in all browsers with zero
frontend changes.

### 5.3 Content-Type

When serving the original HEIC (via `?original=1`), set
`Content-Type: image/heic`. When serving the thumbnail stand-in, set
`Content-Type: image/jpeg` (already correct).

---

## 6. Configuration

### 6.1 No new config fields

HEIC support is always-on when the bundled static libraries are available at
build time. There is no runtime toggle — if `go build` links against the
bundled `.a` files, HEIC works. If not (CGO disabled, platform mismatch), HEIC
files are skipped with a warning.

This follows the same philosophy as the scanner's camera whitelist: the
decision to support a format is a build-time property, not a runtime setting.

### 6.2 Detection at startup

On server startup, `main.go` logs whether HEIC support is available:

```
INFO heif: available (static, libheif 1.18.0 + libde265 1.0.15)
```

or, when CGO is disabled:

```
WARN heif: not available (CGO disabled); HEIC files will be skipped
```

This mirrors the existing recognition status log line.

---

## 7. Implementation phases

### Phase A — Library bundling & shim

1. Create `heif/build_libs.sh`: script that clones libde265 and libheif at
   pinned versions, builds static `.a` files with `-fPIC`, and copies them
   into `heif/lib/linux-x64/`.
2. Run it once; commit the resulting `.a` files.
3. Create `internal/heif/heif.go` (CGO shim) and `heif_stub.go`.
4. Add `internal/heif/heif_test.go` with a minimal valid HEIC fixture (a
   single orange pixel, ~500 bytes).
5. Add `heif/include/` with the headers needed at compile time (extracted
   during the build script, committed to git so `go build` works without
   running the script).

### Phase B — Scan pipeline integration

1. Extend `isSupportedExtension` in `scanner.go`.
2. Refactor `decodeImage` in `thumbnail.go` to route HEIC through the shim.
3. Add `readHEICEXIF` path in `exif.go`.
4. Add `isHEICExtension` helper (shared by thumbnail.go and exif.go).

### Phase C — Database & serving

1. Migration `005_heic.sql`: add `format` column.
2. Update `Photo` struct, `InsertPhoto`, `scanPhoto`, `scanPhotoRows`.
3. Set `format` during ingest based on extension.
4. `handlePhotoImage`: serve thumbnail for HEIC; `?original=1` for raw.

### Phase D — Documentation & architecture

1. Update `architecture.md`: scan pipeline diagram, key dependencies table.
2. Update `CLAUDE.md`: gotchas, build instructions.
3. Add a note to `README.md` about HEIC support and the bundled libraries.

---

## 8. Licensing & compliance

### 8.1 LGPL-3.0 static linking

Both libde265 and libheif are LGPL-3.0 licensed. Static linking an LGPL
library into a binary triggers the "Combined Work" clause (§4). The
obligations are:

1. The end user must be able to relink the application with a modified version
   of the LGPL library. This is satisfied by providing the build script
   (`heif/build_libs.sh`) and the exact source URLs/versions used.
2. The LGPL library's copyright notices must be included. We add a
   `heif/LICENSE.md` with the relevant notices.
3. The application's license (MIT, Apache, etc.) remains unaffected — LGPL
   does not "infect" the calling code.

### 8.2 HEVC patent notice

HEVC (H.265) is covered by patents. The bundled libde265 is an open-source
decoder implementation. Users in jurisdictions with software patent
enforcement should be aware of potential licensing obligations for commercial
use. Add a note to the README.

---

## 9. Testing

### 9.1 Unit tests (`internal/heif/heif_test.go`)

- Decode a minimal valid HEIC (single pixel, committed as a test fixture)
- DecodeConfig returns correct dimensions
- ExtractEXIF from a HEIC with known EXIF data
- Decode of garbage bytes returns an error
- Stub returns `ErrNotAvailable` when CGO is disabled

### 9.2 Integration tests (`internal/integration/pipeline_test.go`)

Extend the existing pipeline test with a HEIC sample:
- Scan a directory containing one `.heic` file
- Assert `format = "heic"` in the DB
- Assert thumbnail was generated
- Assert EXIF fields are populated (use a HEIC with known metadata)

### 9.3 Scanner filter tests (`internal/scan/scanner_filter_test.go`)

- `.heic` and `.HEIC` extensions pass `isSupportedExtension`
- `.heif` extension passes

---

## 10. Future considerations

### 10.1 AVIF support

AVIF uses the same ISOBMFF container as HEIC but the AV1 codec instead of
HEVC. Supporting AVIF would require bundling `libaom` or `dav1d` alongside
libheif. The architecture is identical — the shim layer abstracts the codec
details. Deferred until there is user demand.

### 10.2 Multi-platform static libs

The initial implementation targets Linux x86-64 only (matching the existing
onnxruntime bundle). Adding `heif/lib/darwin-arm64/` etc. follows the same
pattern. The `heif/build_libs.sh` script documents the cross-compilation
process.

### 10.3 Pure-Go HEIC decoder

If a mature pure-Go HEIC decoder emerges (e.g., a port of libde265), the CGO
shim can be replaced with a pure-Go implementation transparently — the
`internal/heif` package API stays the same.

### 10.4 Transcoding on library copy

When a HEIC photo is approved from staging into the internal library, we could
optionally transcode it to JPEG for maximum browser compatibility. This is a
configuration option for a future phase, not required for initial HEIC
support.
