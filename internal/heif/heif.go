// Package heif provides HEIC image decoding by statically linking against
// bundled libheif and libde265. When CGO is enabled and the static libraries
// are present, Decode, DecodeConfig, and ExtractEXIF are fully functional.
//
// When CGO is disabled (build tag !cgo), all functions return ErrNotAvailable.
package heif

/*
#cgo LDFLAGS: -L${SRCDIR}/../../heif/lib/linux-x64 -l:libheif.a -l:libde265.a -lstdc++ -lm
#cgo CFLAGS: -I${SRCDIR}/../../heif/include

#include <stdlib.h>
#include <string.h>
#include "heif.h"

// decode_to_rgba decodes handle into interleaved RGBA pixel data.
// Caller must free *out_data with free() and release *out_img with heif_image_release().
// Returns width, height on success, or a negative error code.
static int decode_to_rgba(
    const struct heif_image_handle* handle,
    uint8_t** out_data,
    int* out_width,
    int* out_height,
    int* out_stride
) {
    struct heif_image* img = NULL;
    struct heif_error err = heif_decode_image(
        handle, &img,
        heif_colorspace_RGB,
        heif_chroma_interleaved_RGBA,
        NULL
    );
    if (err.code != heif_error_Ok) {
        return -1;
    }

    int w = heif_image_get_primary_width(img);
    int h = heif_image_get_primary_height(img);
    if (w <= 0 || h <= 0) {
        heif_image_release(img);
        return -2;
    }

    int stride = 0;
    const uint8_t* src = heif_image_get_plane_readonly(img, heif_channel_interleaved, &stride);
    if (!src) {
        heif_image_release(img);
        return -3;
    }

    size_t data_size = (size_t)stride * h;
    uint8_t* copy = (uint8_t*)malloc(data_size);
    if (!copy) {
        heif_image_release(img);
        return -4;
    }
    memcpy(copy, src, data_size);

    *out_data = copy;
    *out_width = w;
    *out_height = h;
    *out_stride = stride;
    heif_image_release(img);
    return 0;
}

// extract_exif_data retrieves the raw Exif metadata block from a handle.
// Returns the raw data as stored in the HEIC item (with 4-byte prefix).
// Caller must free *out_data with free().
// Returns size on success, or 0 if no Exif data is present.
static size_t extract_exif_data(
    const struct heif_image_handle* handle,
    uint8_t** out_data
) {
    int num = heif_image_handle_get_number_of_metadata_blocks(handle, "Exif");
    if (num <= 0) {
        return 0;
    }

    heif_item_id id;
    int n = heif_image_handle_get_list_of_metadata_block_IDs(handle, "Exif", &id, 1);
    if (n <= 0) {
        return 0;
    }

    size_t sz = heif_image_handle_get_metadata_size(handle, id);
    if (sz == 0) {
        return 0;
    }

    uint8_t* buf = (uint8_t*)malloc(sz);
    if (!buf) {
        return 0;
    }

    struct heif_error err = heif_image_handle_get_metadata(handle, id, buf);
    if (err.code != heif_error_Ok) {
        free(buf);
        return 0;
    }

    *out_data = buf;
    return sz;
}
*/
import "C"

import (
	"errors"
	"fmt"
	"image"
	"io"
	"unsafe"
)

// ErrNotAvailable is returned when CGO is disabled. It is also defined in
// heif_stub.go for the !cgo build.
var ErrNotAvailable = errors.New("heif: not available (CGO disabled)")

// Decode reads a HEIC image from r and returns it as a Go image.Image (NRGBA).
func Decode(r io.Reader) (image.Image, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("heif: read: %w", err)
	}
	if len(data) == 0 {
		return nil, errors.New("heif: empty input")
	}

	cData := C.CBytes(data)
	defer C.free(cData)

	ctx := C.heif_context_alloc()
	if ctx == nil {
		return nil, errors.New("heif: failed to allocate context")
	}
	defer C.heif_context_free(ctx)

	errCode := C.heif_context_read_from_memory(ctx, cData, C.size_t(len(data)), nil)
	if errCode.code != C.heif_error_Ok {
		return nil, fmt.Errorf("heif: read from memory: code=%d", int(errCode.code))
	}

	var handle *C.struct_heif_image_handle
	errCode = C.heif_context_get_primary_image_handle(ctx, &handle)
	if errCode.code != C.heif_error_Ok {
		return nil, fmt.Errorf("heif: get primary handle: code=%d", int(errCode.code))
	}
	defer C.heif_image_handle_release(handle)

	var (
		outData   *C.uint8_t
		outWidth  C.int
		outHeight C.int
		outStride C.int
	)
	ret := C.decode_to_rgba(handle, &outData, &outWidth, &outHeight, &outStride)
	if ret != 0 {
		return nil, fmt.Errorf("heif: decode failed: code=%d", int(ret))
	}
	defer C.free(unsafe.Pointer(outData))

	w := int(outWidth)
	h := int(outHeight)
	stride := int(outStride)

	// Copy from C memory (RGBA, stride may be > width*4) into Go image.NRGBA.
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	src := (*[1 << 30]uint8)(unsafe.Pointer(outData))[: stride*h : stride*h]
	for y := 0; y < h; y++ {
		copy(img.Pix[y*img.Stride:y*img.Stride+w*4], src[y*stride:y*stride+w*4])
	}
	return img, nil
}

// DecodeConfig returns the image dimensions and model without decoding pixel data.
func DecodeConfig(r io.Reader) (image.Config, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return image.Config{}, fmt.Errorf("heif: read: %w", err)
	}
	if len(data) == 0 {
		return image.Config{}, errors.New("heif: empty input")
	}

	cData := C.CBytes(data)
	defer C.free(cData)

	ctx := C.heif_context_alloc()
	if ctx == nil {
		return image.Config{}, errors.New("heif: failed to allocate context")
	}
	defer C.heif_context_free(ctx)

	errCode := C.heif_context_read_from_memory(ctx, cData, C.size_t(len(data)), nil)
	if errCode.code != C.heif_error_Ok {
		return image.Config{}, fmt.Errorf("heif: read from memory: code=%d", int(errCode.code))
	}

	var handle *C.struct_heif_image_handle
	errCode = C.heif_context_get_primary_image_handle(ctx, &handle)
	if errCode.code != C.heif_error_Ok {
		return image.Config{}, fmt.Errorf("heif: get primary handle: code=%d", int(errCode.code))
	}
	defer C.heif_image_handle_release(handle)

	w := int(C.heif_image_handle_get_width(handle))
	h := int(C.heif_image_handle_get_height(handle))
	if w <= 0 || h <= 0 {
		return image.Config{}, errors.New("heif: invalid dimensions")
	}

	return image.Config{Width: w, Height: h}, nil
}

// ExtractEXIF reads the raw Exif data from a HEIC file.
// The returned bytes are the TIFF/Exif data suitable for parsing by goexif.
// Returns (nil, nil) if the file has no Exif data.
func ExtractEXIF(r io.Reader) ([]byte, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("heif: read: %w", err)
	}
	if len(data) == 0 {
		return nil, nil
	}

	cData := C.CBytes(data)
	defer C.free(cData)

	ctx := C.heif_context_alloc()
	if ctx == nil {
		return nil, errors.New("heif: failed to allocate context")
	}
	defer C.heif_context_free(ctx)

	errCode := C.heif_context_read_from_memory(ctx, cData, C.size_t(len(data)), nil)
	if errCode.code != C.heif_error_Ok {
		return nil, nil
	}

	var handle *C.struct_heif_image_handle
	errCode = C.heif_context_get_primary_image_handle(ctx, &handle)
	if errCode.code != C.heif_error_Ok {
		return nil, nil
	}
	defer C.heif_image_handle_release(handle)

	var outData *C.uint8_t
	sz := C.extract_exif_data(handle, &outData)
	if sz == 0 || outData == nil {
		return nil, nil
	}
	defer C.free(unsafe.Pointer(outData))

	raw := make([]byte, int(sz))
	copy(raw, (*[1 << 30]uint8)(unsafe.Pointer(outData))[:sz:sz])

	result := stripHEICExifPrefix(raw)
	return result, nil
}

// stripHEICExifPrefix removes the wrapper bytes that libheif prepends to raw
// Exif metadata, returning a clean TIFF byte stream suitable for goexif.
//
// The HEIC/HEIF spec (ISO 23008-12) stores Exif metadata items with a 4-byte
// big-endian integer at the start that gives the number of bytes between the
// end of that 4-byte field and the start of the TIFF data. In practice this
// value is 6, covering the "Exif\0\0" box type string:
//
//	[00 00 00 06] [45 78 69 66 00 00] [MM/II ...]
//	 offset=6      "Exif\0\0"          TIFF data starts at raw[4+6]=raw[10]
//
// Some writers omit the 4-byte field and start directly with "Exif\0\0".
// The fallback scans for the TIFF byte-order marker ("II\x2a\x00" or "MM\x00\x2a").
func stripHEICExifPrefix(raw []byte) []byte {
	if len(raw) < 4 {
		return nil
	}

	// Layout A: raw starts directly with "Exif\0\0" box header.
	if len(raw) >= 6 &&
		raw[0] == 'E' && raw[1] == 'x' && raw[2] == 'i' && raw[3] == 'f' &&
		raw[4] == 0 && raw[5] == 0 {
		return raw[6:]
	}

	// Layout B: 4-byte big-endian offset field. The offset is the number of
	// bytes after the field that precede the TIFF data, so TIFF starts at
	// raw[4 + offset].
	offset := int(raw[0])<<24 | int(raw[1])<<16 | int(raw[2])<<8 | int(raw[3])
	start := 4 + offset
	if start >= 4 && start+4 <= len(raw) && isTIFFStart(raw[start:]) {
		return raw[start:]
	}

	// Fallback: scan for the TIFF byte-order marker within the first 256 bytes.
	limit := len(raw) - 3
	if limit > 256 {
		limit = 256
	}
	for i := 0; i < limit; i++ {
		if isTIFFStart(raw[i:]) {
			return raw[i:]
		}
	}

	return nil
}

// isTIFFStart returns true if b begins with a valid TIFF byte-order header:
// "MM\x00\x2a" (big-endian) or "II\x2a\x00" (little-endian).
func isTIFFStart(b []byte) bool {
	if len(b) < 4 {
		return false
	}
	return (b[0] == 'M' && b[1] == 'M' && b[2] == 0x00 && b[3] == 0x2a) ||
		(b[0] == 'I' && b[1] == 'I' && b[2] == 0x2a && b[3] == 0x00)
}
