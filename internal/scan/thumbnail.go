package scan

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/jpeg" // register JPEG decoder
	"os"
	"path/filepath"

	"github.com/halleck/gallery/internal/heif"
	"golang.org/x/image/draw"
)

const thumbnailLongEdge = 400

// ThumbJob is a request to generate a thumbnail.
type ThumbJob struct {
	SourcePath string
	SHA256     string
	CacheDir   string
	PhotoID    int64 // set on new ingests; used for face detection (Phase B)
	// FileData, when non-nil, contains the pre-read file bytes. The thumbnail
	// worker decodes from memory instead of re-reading the file from disk.
	FileData []byte
	// Result is written back here by the worker.
	ResultPath string
	Err        error
}

// GenerateThumbnail creates a JPEG thumbnail for the image at sourcePath.
// The thumbnail is stored under cacheDir/<first2ofHash>/<hash>.jpg.
// If the file already exists, the path is returned without regenerating.
// When fileData is non-nil the image is decoded from memory instead of
// re-reading the file from disk.
func GenerateThumbnail(sourcePath, sha256, cacheDir string, fileData []byte) (string, error) {
	thumbPath := thumbnailPath(sha256, cacheDir)
	if _, err := os.Stat(thumbPath); err == nil {
		return thumbPath, nil // already exists
	}

	if err := os.MkdirAll(filepath.Dir(thumbPath), 0o755); err != nil {
		return "", fmt.Errorf("creating thumbnail dir: %w", err)
	}

	src, err := decodeImage(sourcePath, fileData)
	if err != nil {
		return "", fmt.Errorf("decoding image %s: %w", sourcePath, err)
	}

	thumb := resizeToLongEdge(src, thumbnailLongEdge)

	out, err := os.Create(thumbPath)
	if err != nil {
		return "", fmt.Errorf("creating thumbnail file: %w", err)
	}
	defer out.Close()

	if err := jpeg.Encode(out, thumb, &jpeg.Options{Quality: 80}); err != nil {
		return "", fmt.Errorf("encoding thumbnail: %w", err)
	}
	return thumbPath, nil
}

func thumbnailPath(sha256, cacheDir string) string {
	return filepath.Join(cacheDir, sha256[:2], sha256+".jpg")
}

// decodeImage decodes a JPEG or HEIC image. When data is non-nil the image is
// decoded from memory; otherwise the file at path is opened and read.
func decodeImage(path string, data []byte) (image.Image, error) {
	if data != nil {
		if isHEICExtension(path) {
			return heif.Decode(bytes.NewReader(data))
		}
		img, _, err := image.Decode(bytes.NewReader(data))
		return img, err
	}
	if isHEICExtension(path) {
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		return heif.Decode(f)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	return img, err
}

func resizeToLongEdge(src image.Image, maxEdge int) image.Image {
	bounds := src.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w == 0 || h == 0 {
		return src
	}

	var newW, newH int
	if w >= h {
		newW = maxEdge
		newH = int(float64(h) * float64(maxEdge) / float64(w))
	} else {
		newH = maxEdge
		newW = int(float64(w) * float64(maxEdge) / float64(h))
	}
	if newW < 1 {
		newW = 1
	}
	if newH < 1 {
		newH = 1
	}

	dst := image.NewRGBA(image.Rect(0, 0, newW, newH))
	draw.BiLinear.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Over, nil)
	return dst
}
