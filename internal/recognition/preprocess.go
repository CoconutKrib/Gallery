package recognition

import (
	"encoding/binary"
	"image"
	"math"

	"golang.org/x/image/draw"
)

// preprocessBGRFloat32 resizes img to (w, h) using bilinear interpolation,
// then returns a flat float32 slice in CHW layout with BGR channel ordering
// and (pixel - 127.5) / 128.0 normalisation. This is the preprocessing
// convention expected by InsightFace SCRFD and ArcFace models.
func preprocessBGRFloat32(img image.Image, w, h int) []float32 {
	resized := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.BiLinear.Scale(resized, resized.Bounds(), img, img.Bounds(), draw.Over, nil)

	data := make([]float32, 3*h*w)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			r, g, b, _ := resized.At(x, y).RGBA()
			fr := float32(r >> 8)
			fg := float32(g >> 8)
			fb := float32(b >> 8)
			// BGR ordering (InsightFace convention — NOT RGB).
			idx := y*w + x
			data[0*h*w+idx] = (fb - 127.5) / 128.0
			data[1*h*w+idx] = (fg - 127.5) / 128.0
			data[2*h*w+idx] = (fr - 127.5) / 128.0
		}
	}
	return data
}

// cropImage extracts the sub-image at [x1,y1,x2,y2) from img.
// Uses SubImage when available; falls back to pixel-by-pixel copy.
func cropImage(img image.Image, x1, y1, x2, y2 int) image.Image {
	type subImager interface {
		SubImage(r image.Rectangle) image.Image
	}
	if si, ok := img.(subImager); ok {
		return si.SubImage(image.Rect(x1, y1, x2, y2))
	}
	w := x2 - x1
	h := y2 - y1
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			dst.Set(x, y, img.At(x1+x, y1+y))
		}
	}
	return dst
}

// clampInt clamps v to [lo, hi].
func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// l2NormalizeInPlace L2-normalises a float32 slice in place.
func l2NormalizeInPlace(v []float32) {
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	n := float32(math.Sqrt(sum))
	if n == 0 {
		return
	}
	for i := range v {
		v[i] /= n
	}
}

// EmbeddingToBytes serialises a []float32 embedding as little-endian bytes
// suitable for storage in the faces.embedding BLOB column.
func EmbeddingToBytes(emb []float32) []byte {
	b := make([]byte, len(emb)*4)
	for i, f := range emb {
		binary.LittleEndian.PutUint32(b[i*4:], math.Float32bits(f))
	}
	return b
}

// BytesToEmbedding deserialises a BLOB back to []float32.
func BytesToEmbedding(b []byte) []float32 {
	emb := make([]float32, len(b)/4)
	for i := range emb {
		bits := binary.LittleEndian.Uint32(b[i*4:])
		emb[i] = math.Float32frombits(bits)
	}
	return emb
}
