package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// LibraryCopy represents a row in the library_copies table.
type LibraryCopy struct {
	ID              int64
	PhotoSHA256     string
	RelativePath    string
	AbsolutePath    string
	TrueDateUnknown bool
	Tags            []string
	CopiedAt        time.Time
	// Rich annotations — added by migration 003
	Title        *string
	Description  *string
	OverrideDate *string // RFC3339 UTC
	EventID      *int64
}

// InsertLibraryCopy records that a photo has been copied to the internal library.
func InsertLibraryCopy(db *sql.DB, c *LibraryCopy) (int64, error) {
	tagsJSON, err := json.Marshal(c.Tags)
	if err != nil {
		return 0, fmt.Errorf("marshalling tags: %w", err)
	}
	trueDateUnknownInt := 0
	if c.TrueDateUnknown {
		trueDateUnknownInt = 1
	}
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := db.Exec(`
		INSERT INTO library_copies
		  (photo_sha256, relative_path, absolute_path, true_date_unknown, tags, copied_at,
		   title, description, override_date, event_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.PhotoSHA256, c.RelativePath, c.AbsolutePath, trueDateUnknownInt, string(tagsJSON), now,
		c.Title, c.Description, c.OverrideDate, c.EventID)
	if err != nil {
		return 0, fmt.Errorf("inserting library copy: %w", err)
	}
	return res.LastInsertId()
}

// GetLibraryCopyByID returns a library copy record by its row ID.
func GetLibraryCopyByID(db *sql.DB, id int64) (*LibraryCopy, error) {
	row := db.QueryRow(`SELECT `+libraryCopyCols+` FROM library_copies WHERE id = ?`, id)
	return scanLibraryCopy(row)
}

// LibraryCopyUpdate carries the fields that PATCH /api/library/copies/{id} may change.
// Pointer fields: nil = do not change; pointer-to-nil-pointer = clear the field.
type LibraryCopyUpdate struct {
	Title           **string // outer nil = skip; *string = new value (nil clears)
	Description     **string
	OverrideDate    **string
	EventID         **int64
	Tags            *[]string
	TrueDateUnknown *bool
	RelativePath    *string // set by re-org handler after file move
	AbsolutePath    *string
}

// UpdateLibraryCopy applies a partial update to a library_copies row.
func UpdateLibraryCopy(db *sql.DB, id int64, u LibraryCopyUpdate) error {
	var sets []string
	var args []any

	if u.Title != nil {
		sets = append(sets, "title = ?")
		args = append(args, *u.Title)
	}
	if u.Description != nil {
		sets = append(sets, "description = ?")
		args = append(args, *u.Description)
	}
	if u.OverrideDate != nil {
		sets = append(sets, "override_date = ?")
		args = append(args, *u.OverrideDate)
	}
	if u.EventID != nil {
		sets = append(sets, "event_id = ?")
		args = append(args, *u.EventID)
	}
	if u.Tags != nil {
		tj, err := json.Marshal(*u.Tags)
		if err != nil {
			return fmt.Errorf("marshalling tags: %w", err)
		}
		sets = append(sets, "tags = ?")
		args = append(args, string(tj))
	}
	if u.TrueDateUnknown != nil {
		val := 0
		if *u.TrueDateUnknown {
			val = 1
		}
		sets = append(sets, "true_date_unknown = ?")
		args = append(args, val)
	}
	if u.RelativePath != nil {
		sets = append(sets, "relative_path = ?")
		args = append(args, *u.RelativePath)
	}
	if u.AbsolutePath != nil {
		sets = append(sets, "absolute_path = ?")
		args = append(args, *u.AbsolutePath)
	}

	if len(sets) == 0 {
		return nil
	}

	query := "UPDATE library_copies SET " + strings.Join(sets, ", ") + " WHERE id = ?"
	args = append(args, id)
	_, err := db.Exec(query, args...)
	return err
}

// DeleteLibraryPhotoByID deletes a library copy and all associated DB records for
// the given library_copies.id. It does NOT delete the physical file — the caller
// must do that before or after calling this function.
//
// Cascade order (no ON DELETE CASCADE in schema):
//  1. photo_events rows for the photo
//  2. staging_queue row
//  3. duplicate_paths rows
//  4. library_copies row
//  5. photos row
func DeleteLibraryPhotoByID(db *sql.DB, id int64) (sha256 string, absPath string, err error) {
	// Resolve sha256 and absolute_path first.
	if err = db.QueryRow(`SELECT photo_sha256, absolute_path FROM library_copies WHERE id = ?`, id).
		Scan(&sha256, &absPath); err != nil {
		return "", "", fmt.Errorf("resolving library copy: %w", err)
	}

	tx, err := db.Begin()
	if err != nil {
		return "", "", fmt.Errorf("beginning transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	// 1. Remove photo_events.
	if _, err = tx.Exec(`DELETE FROM photo_events WHERE photo_id = (SELECT id FROM photos WHERE sha256 = ?)`, sha256); err != nil {
		return sha256, absPath, fmt.Errorf("deleting photo_events: %w", err)
	}
	// 2. Remove staging_queue.
	if _, err = tx.Exec(`DELETE FROM staging_queue WHERE photo_sha256 = ?`, sha256); err != nil {
		return sha256, absPath, fmt.Errorf("deleting staging_queue: %w", err)
	}
	// 3. Remove duplicate_paths.
	if _, err = tx.Exec(`DELETE FROM duplicate_paths WHERE sha256 = ?`, sha256); err != nil {
		return sha256, absPath, fmt.Errorf("deleting duplicate_paths: %w", err)
	}
	// 4. Remove library_copies.
	if _, err = tx.Exec(`DELETE FROM library_copies WHERE id = ?`, id); err != nil {
		return sha256, absPath, fmt.Errorf("deleting library_copies: %w", err)
	}
	// 5. Remove photos.
	if _, err = tx.Exec(`DELETE FROM photos WHERE sha256 = ?`, sha256); err != nil {
		return sha256, absPath, fmt.Errorf("deleting photos: %w", err)
	}

	err = tx.Commit()
	return sha256, absPath, err
}

// LibraryCopyFilter specifies optional constraints for listing library copies.
type LibraryCopyFilter struct {
	Source          string // "scan" or "dropzone"; filters via JOIN to photos
	HasDateOverride *bool  // true → override_date IS NOT NULL
	TrueDateUnknown *bool
	Tag             string // must appear in tags JSON array (case-insensitive LIKE)
	EventID         *int64
	Keyword         string // LIKE search over title, description, filename
	Page            int    // 1-based; 0 treated as 1
	PerPage         int    // 0 treated as 50
}

// ListLibraryCopiesFiltered returns a page of library copies matching the filter.
func ListLibraryCopiesFiltered(database *sql.DB, f LibraryCopyFilter) ([]*LibraryCopy, int, error) {
	if f.Page < 1 {
		f.Page = 1
	}
	if f.PerPage < 1 {
		f.PerPage = 50
	}
	if f.PerPage > 500 {
		f.PerPage = 500
	}

	// Base query joins photos so we can filter on source and filename.
	base := `FROM library_copies lc
		JOIN photos p ON p.sha256 = lc.photo_sha256`
	var clauses []string
	var args []any

	if f.Source != "" {
		clauses = append(clauses, "p.source = ?")
		args = append(args, f.Source)
	}
	if f.HasDateOverride != nil {
		if *f.HasDateOverride {
			clauses = append(clauses, "lc.override_date IS NOT NULL")
		} else {
			clauses = append(clauses, "lc.override_date IS NULL")
		}
	}
	if f.TrueDateUnknown != nil {
		val := 0
		if *f.TrueDateUnknown {
			val = 1
		}
		clauses = append(clauses, "lc.true_date_unknown = ?")
		args = append(args, val)
	}
	if f.Tag != "" {
		clauses = append(clauses, "LOWER(lc.tags) LIKE LOWER(?)")
		args = append(args, "%\""+f.Tag+"\"%")
	}
	if f.EventID != nil {
		clauses = append(clauses, "lc.event_id = ?")
		args = append(args, *f.EventID)
	}
	if f.Keyword != "" {
		like := "%" + f.Keyword + "%"
		clauses = append(clauses, "(LOWER(lc.title) LIKE LOWER(?) OR LOWER(lc.description) LIKE LOWER(?) OR LOWER(p.filename) LIKE LOWER(?))")
		args = append(args, like, like, like)
	}

	where := ""
	if len(clauses) > 0 {
		where = " WHERE " + strings.Join(clauses, " AND ")
	}

	var total int
	if err := database.QueryRow("SELECT COUNT(*) "+base+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("counting library copies: %w", err)
	}

	offset := (f.Page - 1) * f.PerPage
	query := `SELECT lc.` + strings.Join(strings.Split(libraryCopyCols, ", "), ", lc.") +
		" " + base + where + " ORDER BY lc.copied_at DESC LIMIT ? OFFSET ?"
	rows, err := database.Query(query, append(args, f.PerPage, offset)...)
	if err != nil {
		return nil, 0, fmt.Errorf("listing library copies: %w", err)
	}
	defer rows.Close()
	var copies []*LibraryCopy
	for rows.Next() {
		c, err := scanLibraryCopy(rows)
		if err != nil {
			return nil, 0, err
		}
		copies = append(copies, c)
	}
	return copies, total, rows.Err()
}

// LibraryCopyExistsBySHA256 returns true if the photo has already been copied.
func LibraryCopyExistsBySHA256(db *sql.DB, sha256 string) (bool, error) {
	var count int
	err := db.QueryRow(`SELECT COUNT(*) FROM library_copies WHERE photo_sha256 = ?`, sha256).Scan(&count)
	return count > 0, err
}

// GetLibraryCopyBySHA256 returns the library copy record for a photo.
func GetLibraryCopyBySHA256(db *sql.DB, sha256 string) (*LibraryCopy, error) {
	row := db.QueryRow(`SELECT `+libraryCopyCols+` FROM library_copies WHERE photo_sha256 = ?`, sha256)
	return scanLibraryCopy(row)
}

// ListLibraryCopies returns all library copy records ordered by copied_at descending.
func ListLibraryCopies(db *sql.DB) ([]*LibraryCopy, error) {
	rows, err := db.Query(`SELECT ` + libraryCopyCols + ` FROM library_copies ORDER BY copied_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("listing library copies: %w", err)
	}
	defer rows.Close()
	var copies []*LibraryCopy
	for rows.Next() {
		c, err := scanLibraryCopy(rows)
		if err != nil {
			return nil, err
		}
		copies = append(copies, c)
	}
	return copies, rows.Err()
}

// LibraryTreeNode represents a year/month/event folder with its photo count.
type LibraryTreeNode struct {
	Year    string             `json:"year"`
	Months  []LibraryTreeMonth `json:"months"`
	Undated *LibraryTreeMonth  `json:"undated,omitempty"`
}

type LibraryTreeMonth struct {
	Month  string             `json:"month"`
	Events []LibraryTreeEvent `json:"events"`
	Count  int                `json:"count"` // photos without an event
}

type LibraryTreeEvent struct {
	Slug  string `json:"slug"`
	Count int    `json:"count"`
}

// GetLibraryTree returns the year/month/event hierarchy from relative_path data.
func GetLibraryTree(db *sql.DB) ([]LibraryTreeNode, error) {
	rows, err := db.Query(`SELECT relative_path FROM library_copies ORDER BY relative_path`)
	if err != nil {
		return nil, fmt.Errorf("querying library tree: %w", err)
	}
	defer rows.Close()

	// Build a map: year -> month -> event_slug -> count
	type key struct{ year, month, slug string }
	counts := map[key]int{}
	for rows.Next() {
		var rp string
		if err := rows.Scan(&rp); err != nil {
			return nil, err
		}
		y, m, s := parseRelativePath(rp)
		counts[key{y, m, s}]++
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Aggregate into tree.
	yearMap := map[string]map[string]map[string]int{}
	for k, c := range counts {
		if yearMap[k.year] == nil {
			yearMap[k.year] = map[string]map[string]int{}
		}
		if yearMap[k.year][k.month] == nil {
			yearMap[k.year][k.month] = map[string]int{}
		}
		yearMap[k.year][k.month][k.slug] += c
	}

	var result []LibraryTreeNode
	for year, monthMap := range yearMap {
		node := LibraryTreeNode{Year: year}
		for month, slugMap := range monthMap {
			mnode := LibraryTreeMonth{Month: month}
			for slug, count := range slugMap {
				if slug == "" {
					mnode.Count += count
				} else {
					mnode.Events = append(mnode.Events, LibraryTreeEvent{Slug: slug, Count: count})
				}
			}
			node.Months = append(node.Months, mnode)
		}
		result = append(result, node)
	}
	return result, nil
}

// parseRelativePath extracts (year, month, eventSlug) from a relative path like
// "2024/06/Wedding-Smith/IMG_0001.jpg" or "_undated/foo/img.jpg".
func parseRelativePath(rp string) (year, month, slug string) {
	parts := splitPath(rp)
	if len(parts) == 0 {
		return "", "", ""
	}
	year = parts[0]
	if year == "_undated" {
		if len(parts) >= 3 {
			slug = parts[1]
		}
		return "_undated", "", slug
	}
	if len(parts) >= 2 {
		month = parts[1]
	}
	if len(parts) >= 4 {
		slug = parts[2]
	}
	return year, month, slug
}

func splitPath(p string) []string {
	var parts []string
	cur := ""
	for _, r := range p {
		if r == '/' || r == '\\' {
			if cur != "" {
				parts = append(parts, cur)
				cur = ""
			}
		} else {
			cur += string(r)
		}
	}
	if cur != "" {
		parts = append(parts, cur)
	}
	return parts
}

const libraryCopyCols = `id, photo_sha256, relative_path, absolute_path, true_date_unknown, tags, copied_at, title, description, override_date, event_id`

type libraryCopyScanner interface {
	Scan(dest ...any) error
}

func scanLibraryCopy(s libraryCopyScanner) (*LibraryCopy, error) {
	var c LibraryCopy
	var tagsJSON string
	var trueDateUnknownInt int
	var copiedAt string

	err := s.Scan(&c.ID, &c.PhotoSHA256, &c.RelativePath, &c.AbsolutePath,
		&trueDateUnknownInt, &tagsJSON, &copiedAt,
		&c.Title, &c.Description, &c.OverrideDate, &c.EventID)
	if err != nil {
		return nil, err
	}
	c.Tags = []string{}
	if err := json.Unmarshal([]byte(tagsJSON), &c.Tags); err != nil {
		c.Tags = []string{}
	}
	c.TrueDateUnknown = trueDateUnknownInt != 0
	if t, err := time.Parse(time.RFC3339, copiedAt); err == nil {
		c.CopiedAt = t
	}
	return &c, nil
}
