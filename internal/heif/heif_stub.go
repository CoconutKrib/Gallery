//go:build !cgo

package heif

import (
	"errors"
	"image"
	"io"
)

// ErrNotAvailable is returned by all functions when CGO is disabled.
var ErrNotAvailable = errors.New("heif: not available (CGO disabled)")

// Decode always returns ErrNotAvailable when CGO is disabled.
func Decode(r io.Reader) (image.Image, error) {
	return nil, ErrNotAvailable
}

// DecodeConfig always returns ErrNotAvailable when CGO is disabled.
func DecodeConfig(r io.Reader) (image.Config, error) {
	return image.Config{}, ErrNotAvailable
}

// ExtractEXIF always returns ErrNotAvailable when CGO is disabled.
func ExtractEXIF(r io.Reader) ([]byte, error) {
	return nil, ErrNotAvailable
}
