// Package library handles the internal photo library: path construction and
// copying photos from source locations into the managed library hierarchy.
package library

import (
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/halleck/gallery/internal/db"
	"github.com/halleck/gallery/internal/recognition"
)

// CopyPhoto copies an approved staging entry's photo into the internal library.
// It returns the relative path within the library where the file was written,
// or an empty string and no error if the photo was already copied (idempotent).
func CopyPhoto(database *sql.DB, entry *db.StagingEntry, photo *db.Photo, libraryRoot string) (string, error) {
	// Idempotency: already copied?
	if exists, err := db.LibraryCopyExistsBySHA256(database, photo.SHA256); err != nil {
		return "", err
	} else if exists {
		return "", nil
	}

	// Determine effective date and event for path construction.
	var capturedAt *time.Time
	trueDateUnknown := entry.TrueDateUnknown
	if entry.OverrideDate != nil && *entry.OverrideDate != "" {
		if t, err := time.Parse(time.RFC3339, *entry.OverrideDate); err == nil {
			capturedAt = &t
		}
	}
	if capturedAt == nil {
		capturedAt = photo.CapturedAt
	}

	// Build the destination directory relative to libraryRoot.
	relDir := buildRelDir(capturedAt, trueDateUnknown, entry.EventID, database)

	// Compute destination filename with collision resolution.
	destDir := filepath.Join(libraryRoot, relDir)
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return "", fmt.Errorf("creating library dir %q: %w", destDir, err)
	}
	destFilename := resolveFilename(photo.Filename, photo.SHA256, destDir)
	relPath := relDir + "/" + destFilename
	absPath := filepath.Join(destDir, destFilename)

	// Copy the file.
	if err := copyFile(photo.Filepath, absPath); err != nil {
		return "", fmt.Errorf("copying %q → %q: %w", photo.Filepath, absPath, err)
	}
	slog.Info("library: copied photo", "sha256", photo.SHA256[:8], "dest", relPath)

	// Persist the library_copies record.
	tags := entry.Tags
	if tags == nil {
		tags = []string{}
	}
	// Override date to store: use what was used for path calculation.
	var storedOverrideDate *string
	if entry.OverrideDate != nil && *entry.OverrideDate != "" {
		storedOverrideDate = entry.OverrideDate
	}
	_, err := db.InsertLibraryCopy(database, &db.LibraryCopy{
		PhotoSHA256:     photo.SHA256,
		RelativePath:    relPath,
		AbsolutePath:    absPath,
		TrueDateUnknown: trueDateUnknown,
		Tags:            tags,
		Title:           entry.Title,
		Description:     entry.Description,
		OverrideDate:    storedOverrideDate,
		EventID:         entry.EventID,
	})
	if err != nil {
		return relPath, err
	}

	// Enqueue face detection on the newly copied library photo (async, priority 1 = copy-time).
	if recognition.IsAvailable() {
		recognition.EnqueueFaceDetection(photo.ID, 1)
	}

	return relPath, nil
}

// BuildRelDir is the exported form of buildRelDir: constructs the directory
// path segment (relative to libraryRoot) for a photo based on its date and
// optional event. Used by the copy service and the re-organisation handler.
func BuildRelDir(capturedAt *time.Time, trueDateUnknown bool, eventID *int64, database *sql.DB) string {
	return buildRelDir(capturedAt, trueDateUnknown, eventID, database)
}

// ResolveFilename is the exported form of resolveFilename.
func ResolveFilename(filename, sha256, destDir string) string {
	return resolveFilename(filename, sha256, destDir)
}

// MovePhoto moves a file already in the internal library to a new relative path
// derived from updated date/event information. It updates the library_copies
// record in the DB after a successful file rename. Empty parent directories
// (up to 3 levels) are pruned after the move.
//
// If the calculated new directory is the same as the current one, no move is
// performed and the existing relative_path is returned unchanged.
func MovePhoto(database *sql.DB, lc *db.LibraryCopy, photo *db.Photo, libraryRoot string) (string, error) {
	// Determine effective date.
	var capturedAt *time.Time
	if lc.OverrideDate != nil && *lc.OverrideDate != "" {
		if t, err := time.Parse(time.RFC3339, *lc.OverrideDate); err == nil {
			capturedAt = &t
		}
	}
	if capturedAt == nil {
		capturedAt = photo.CapturedAt
	}

	newRelDir := buildRelDir(capturedAt, lc.TrueDateUnknown, lc.EventID, database)

	// Extract the directory part of the current relative_path.
	filename := filepath.Base(lc.RelativePath)
	currentRelDir := ""
	if i := strings.LastIndex(lc.RelativePath, "/"); i >= 0 {
		currentRelDir = lc.RelativePath[:i]
	}

	// No move needed if directory is unchanged.
	if newRelDir == currentRelDir {
		return lc.RelativePath, nil
	}

	newDestDir := filepath.Join(libraryRoot, newRelDir)
	if err := os.MkdirAll(newDestDir, 0o755); err != nil {
		return "", fmt.Errorf("creating destination dir %q: %w", newDestDir, err)
	}

	newFilename := resolveFilename(filename, photo.SHA256, newDestDir)
	newRelPath := newRelDir + "/" + newFilename
	newAbsPath := filepath.Join(newDestDir, newFilename)

	if err := os.Rename(lc.AbsolutePath, newAbsPath); err != nil {
		return "", fmt.Errorf("moving %q → %q: %w", lc.AbsolutePath, newAbsPath, err)
	}
	slog.Info("library: moved photo", "sha256", photo.SHA256[:8], "from", lc.RelativePath, "to", newRelPath)

	if err := db.UpdateLibraryCopy(database, lc.ID, db.LibraryCopyUpdate{
		RelativePath: &newRelPath,
		AbsolutePath: &newAbsPath,
	}); err != nil {
		return newRelPath, fmt.Errorf("updating library copy paths: %w", err)
	}

	// Prune empty ancestor dirs (up to 3 levels, stop at library root).
	pruneEmptyDirs(filepath.Join(libraryRoot, currentRelDir), libraryRoot, 3)

	return newRelPath, nil
}

// pruneEmptyDirs removes dir if empty, then walks up to maxLevels parent
// directories doing the same, stopping if a dir equals or is outside root.
func pruneEmptyDirs(dir, root string, maxLevels int) {
	for i := 0; i < maxLevels; i++ {
		// Safety: never remove the library root itself.
		rel, err := filepath.Rel(root, dir)
		if err != nil || rel == "." || rel == "" {
			return
		}
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) > 0 {
			return
		}
		if err := os.Remove(dir); err != nil {
			return
		}
		dir = filepath.Dir(dir)
	}
}

// buildRelDir constructs the directory path segment (relative to libraryRoot)
// for a photo based on its date and optional event.
func buildRelDir(capturedAt *time.Time, trueDateUnknown bool, eventID *int64, database *sql.DB) string {
	var eventSlug string
	if eventID != nil && database != nil {
		if events, err := db.GetAllEvents(database); err == nil {
			for _, ev := range events {
				if ev.ID == *eventID && ev.Label != "" {
					eventSlug = slugify(ev.Label)
					break
				}
			}
		}
	}

	if trueDateUnknown || capturedAt == nil {
		if eventSlug != "" {
			return "_undated/" + eventSlug
		}
		return "_undated"
	}

	year := capturedAt.UTC().Format("2006")
	month := capturedAt.UTC().Format("01")

	if eventSlug != "" {
		return year + "/" + month + "/" + eventSlug
	}
	return year + "/" + month
}

// slugify converts a string to a URL/filesystem-safe slug using hyphens.
var nonAlnum = regexp.MustCompile(`[^a-zA-Z0-9]+`)

func slugify(s string) string {
	// Replace non-alphanumeric runs with a single hyphen.
	slug := nonAlnum.ReplaceAllString(s, "-")
	// Trim leading/trailing hyphens.
	slug = strings.Trim(slug, "-")
	// Preserve capitalisation for readability (spec says Wedding-Smith).
	if slug == "" {
		return ""
	}
	// Title-case each word for nicer paths.
	slug = titleCaseSlug(slug)
	return slug
}

func titleCaseSlug(slug string) string {
	parts := strings.Split(slug, "-")
	for i, p := range parts {
		if len(p) == 0 {
			continue
		}
		runes := []rune(p)
		runes[0] = unicode.ToUpper(runes[0])
		parts[i] = string(runes)
	}
	return strings.Join(parts, "-")
}

// resolveFilename returns a filename that does not collide in destDir.
// If the original filename is free, it is returned unchanged. Otherwise the
// first 8 hex chars of the SHA-256 are appended before the extension.
func resolveFilename(filename, sha256, destDir string) string {
	candidate := filepath.Join(destDir, filename)
	if _, err := os.Stat(candidate); os.IsNotExist(err) {
		return filename
	}
	// Collision: append hash suffix.
	ext := filepath.Ext(filename)
	base := strings.TrimSuffix(filename, ext)
	suffix := sha256
	if len(suffix) > 8 {
		suffix = suffix[:8]
	}
	return base + "_" + suffix + ext
}

// copyFile copies src to dst, creating dst if needed.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		os.Remove(dst) // clean up partial file
		return err
	}
	return out.Close()
}
