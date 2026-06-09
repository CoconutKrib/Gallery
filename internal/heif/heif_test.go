package heif

import (
	"bytes"
	"image"
	"os"
	"testing"
)

// TestDecodeRealHEIC uses the real sample HEIC from the project's samples directory.
// This test is skipped in short mode because the sample file is large (~3 MB).
func TestDecodeRealHEIC(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping: real HEIC decode requires large sample file")
	}

	samplePath := "../../samples/image1.heic"
	f, err := os.Open(samplePath)
	if err != nil {
		t.Skipf("sample HEIC not found at %s: %v", samplePath, err)
	}
	defer f.Close()

	cfg, err := DecodeConfig(f)
	if err != nil {
		t.Fatalf("DecodeConfig: %v", err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		t.Fatalf("invalid dimensions: %dx%d", cfg.Width, cfg.Height)
	}
	t.Logf("Dimensions: %dx%d", cfg.Width, cfg.Height)

	// Re-open for decode.
	f2, err := os.Open(samplePath)
	if err != nil {
		t.Fatal(err)
	}
	defer f2.Close()

	img, err := Decode(f2)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	bounds := img.Bounds()
	if bounds.Dx() != cfg.Width || bounds.Dy() != cfg.Height {
		t.Fatalf("decode dimensions %dx%d don't match config %dx%d",
			bounds.Dx(), bounds.Dy(), cfg.Width, cfg.Height)
	}

	// Verify the image is NRGBA (our shim always returns NRGBA).
	_, ok := img.(*image.NRGBA)
	if !ok {
		t.Fatalf("expected *image.NRGBA, got %T", img)
	}

	t.Logf("Decoded: %dx%d NRGBA", bounds.Dx(), bounds.Dy())
}

// TestExtractEXIFRealHEIC tests EXIF extraction from the real sample.
func TestExtractEXIFRealHEIC(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping: real HEIC EXIF requires large sample file")
	}

	samplePath := "../../samples/image2.heic"
	f, err := os.Open(samplePath)
	if err != nil {
		t.Skipf("sample HEIC not found at %s: %v", samplePath, err)
	}
	defer f.Close()

	exifBytes, err := ExtractEXIF(f)
	if err != nil {
		t.Fatalf("ExtractEXIF error: %v", err)
	}

	if exifBytes == nil {
		t.Log("No EXIF data found (valid — not all HEICs have EXIF)")
		return
	}

	t.Logf("extracted EXIF: %d bytes, first 16: %x", len(exifBytes), exifBytes[:min(16, len(exifBytes))])

	// The returned bytes must begin with a valid TIFF byte-order header.
	if len(exifBytes) < 4 {
		t.Fatalf("EXIF data too short: %d bytes", len(exifBytes))
	}
	bigEndian := exifBytes[0] == 'M' && exifBytes[1] == 'M' && exifBytes[2] == 0x00 && exifBytes[3] == 0x2a
	littleEndian := exifBytes[0] == 'I' && exifBytes[1] == 'I' && exifBytes[2] == 0x2a && exifBytes[3] == 0x00
	if !bigEndian && !littleEndian {
		t.Fatalf("EXIF does not start with TIFF header: first 4 bytes %x", exifBytes[:4])
	}
	if bigEndian {
		t.Logf("EXIF: %d bytes, big-endian TIFF (MM)", len(exifBytes))
	} else {
		t.Logf("EXIF: %d bytes, little-endian TIFF (II)", len(exifBytes))
	}
}

// TestDecodeInvalidInput verifies error handling for non-HEIC data.
func TestDecodeInvalidInput(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"empty", []byte{}},
		{"not heic", []byte("this is not a heic file")},
		{"jpeg header", []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 0x4A, 0x46, 0x49, 0x46, 0x00, 0x01}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := DecodeConfig(bytes.NewReader(tt.data))
			if err == nil {
				t.Error("expected error for invalid input, got nil")
			}
			_, err = Decode(bytes.NewReader(tt.data))
			if err == nil {
				t.Error("expected error for invalid input, got nil")
			}
		})
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func TestStripHEICExifPrefix(t *testing.T) {
	tests := []struct {
		name   string
		input  []byte
		expect []byte // nil means expect nil result
	}{
		{
			name:   "too short",
			input:  []byte{1, 2, 3},
			expect: nil,
		},
		{
			// Layout A: raw starts directly with "Exif\0\0" (no 4-byte offset field).
			name:   "Exif header prefix, TIFF immediately after",
			input:  hexToBytes("4578696600004d4d002a000000080004"),
			expect: hexToBytes("4d4d002a000000080004"),
		},
		{
			// Layout B: real libheif output — 4-byte big-endian offset (= 6) followed
			// by "Exif\0\0", then TIFF data. TIFF must start at raw[4+6] = raw[10].
			name:  "libheif: 4-byte offset=6 + Exif\\0\\0 + TIFF",
			input: hexToBytes("000000064578696600004d4d002a000000080004"),
			// offset=6, so TIFF at raw[10]: 4d4d002a000000080004
			expect: hexToBytes("4d4d002a000000080004"),
		},
		{
			// Layout B with offset=0: TIFF immediately follows the 4-byte field.
			name:   "4-byte offset=0, TIFF immediately",
			input:  hexToBytes("000000004d4d002a000000080004"),
			expect: hexToBytes("4d4d002a000000080004"),
		},
		{
			// Fallback: raw is bare TIFF with no prefix at all.
			name:   "bare TIFF, no prefix",
			input:  hexToBytes("4d4d002a000000080004"),
			expect: hexToBytes("4d4d002a000000080004"),
		},
		{
			// Little-endian TIFF via fallback scan.
			name:   "little-endian TIFF via fallback",
			input:  hexToBytes("49492a00080000000004"),
			expect: hexToBytes("49492a00080000000004"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripHEICExifPrefix(tt.input)
			if tt.expect == nil {
				if got != nil {
					t.Fatalf("expected nil, got %d bytes: %x", len(got), got[:min(16, len(got))])
				}
				return
			}
			if got == nil {
				t.Fatalf("expected %d bytes, got nil", len(tt.expect))
			}
			if len(got) != len(tt.expect) {
				t.Fatalf("length mismatch: got %d, want %d\ngot:  %x\nwant: %x",
					len(got), len(tt.expect), got[:min(32, len(got))], tt.expect[:min(32, len(tt.expect))])
			}
			for i := range got {
				if got[i] != tt.expect[i] {
					t.Fatalf("mismatch at byte %d: got %x, want %x\ngot:  %x\nwant: %x",
						i, got[i], tt.expect[i], got[:min(32, len(got))], tt.expect[:min(32, len(tt.expect))])
				}
			}
		})
	}
}

func hexToBytes(s string) []byte {
	var b []byte
	for i := 0; i < len(s); i += 2 {
		var v byte
		for j := 0; j < 2; j++ {
			c := s[i+j]
			v <<= 4
			switch {
			case c >= '0' && c <= '9':
				v |= c - '0'
			case c >= 'a' && c <= 'f':
				v |= c - 'a' + 10
			case c >= 'A' && c <= 'F':
				v |= c - 'A' + 10
			}
		}
		b = append(b, v)
	}
	return b
}
