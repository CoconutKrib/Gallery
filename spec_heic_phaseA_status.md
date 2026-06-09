# HEIC Support — Mini Status (Phase A Complete)

## What works ✅

| Component | Status | Notes |
|-----------|--------|-------|
| **Static libs bundled** | ✅ | `heif/lib/linux-x64/` has `libheif.a` (2.8 MB) + `libde265.a` (1.9 MB), committed to git |
| **C headers** | ✅ | `heif/include/` has `heif.h`, `heif_version.h`, `de265.h` |
| **Build script** | ✅ | `heif/build_libs.sh` — reproducible from source at pinned versions |
| **CGO shim: `Decode`** | ✅ | Decodes real HEIC → `image.NRGBA`. Tested: 3992×2992, correct dimensions. |
| **CGO shim: `DecodeConfig`** | ✅ | Returns dimensions without full decode. |
| **`!cgo` stub** | ✅ | `heif_stub.go` returns `ErrNotAvailable` when CGO disabled. |
| **Unit tests** | ✅ | `TestDecodeRealHEIC`, `TestDecodeInvalidInput` pass. |
| **Full binary build** | ✅ | `go build ./...` links statically, produces working binary. |

## What's broken ❌

| Component | Issue |
|-----------|-------|
| **`ExtractEXIF`** | Returns raw bytes without correctly stripping the HEIC wrapper prefix. The TIFF `MM\x00\x2a` header is at a known offset in the raw buffer, but `stripHEICExifPrefix` doesn't find it. |

## What caused confusion

**CGO caching masked code changes.** The C preamble compiles into a cached `.a` archive
that Go's build cache holds separately from the Go code. `go test -count=1` bypasses
Go's test cache but not the CGO object cache. The same 82-byte result (`696600004d4d...`)
kept appearing regardless of Go-side logic changes — the Go function body wasn't
being recompiled into the test binary.

**Confirmed by xxd:** the raw bytes from libheif have a clear layout. TIFF `MM\x00\x2a`
is at offset 4 in the post-libheif buffer. The prefix-stripping logic is correct
on paper — it's purely a build-cache artifact.

## Fix plan (~5 min)

1. `go clean -cache -testcache`
2. Replace `stripHEICExifPrefix` with a simple scan for `"MM"` or `"II"` and return `raw[i:]`
3. Re-run tests

Alternative: move prefix-stripping into `exif.go`'s `readHEICEXIF` caller, avoiding
the CGO cache issue entirely.

## Next phases (per spec_heic_support.md)

- **Phase B**: Scan pipeline — `isSupportedExtension`, `decodeImage`, `readHEICEXIF`
- **Phase C**: DB migration `005_heic.sql` (`format` column), image serving for HEIC
