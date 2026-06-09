package scan

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
)

// maxReadAndHashBytes is the maximum file size for which ReadFileAndHash will
// return the raw bytes alongside the hash. Larger files get a nil byte slice
// (caller falls back to a separate disk read for thumbnail generation).
const maxReadAndHashBytes = 50 << 20 // 50 MB

// HashFile computes the SHA-256 hex digest of the file at path.
func HashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("opening file for hash: %w", err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("hashing file: %w", err)
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

// ReadFileAndHash reads the file at path into memory and computes its SHA-256
// hex digest. If the file is larger than maxReadAndHashBytes the returned data
// slice is nil — the caller should fall back to a separate disk read for any
// subsequent processing (thumbnail generation, face detection).
func ReadFileAndHash(path string) (hash string, data []byte, err error) {
	fi, err := os.Stat(path)
	if err != nil {
		return "", nil, fmt.Errorf("stat file: %w", err)
	}
	if fi.Size() > maxReadAndHashBytes {
		// Large file: hash only, don't buffer.
		h, hashErr := HashFile(path)
		return h, nil, hashErr
	}

	b, err := os.ReadFile(path)
	if err != nil {
		return "", nil, fmt.Errorf("reading file: %w", err)
	}
	h := sha256.Sum256(b)
	return fmt.Sprintf("%x", h[:]), b, nil
}
