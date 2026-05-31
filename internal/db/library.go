package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
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
		INSERT INTO library_copies (photo_sha256, relative_path, absolute_path, true_date_unknown, tags, copied_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		c.PhotoSHA256, c.RelativePath, c.AbsolutePath, trueDateUnknownInt, string(tagsJSON), now)
	if err != nil {
		return 0, fmt.Errorf("inserting library copy: %w", err)
	}
	return res.LastInsertId()
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

const libraryCopyCols = `id, photo_sha256, relative_path, absolute_path, true_date_unknown, tags, copied_at`

type libraryCopyScanner interface {
	Scan(dest ...any) error
}

func scanLibraryCopy(s libraryCopyScanner) (*LibraryCopy, error) {
	var c LibraryCopy
	var tagsJSON string
	var trueDateUnknownInt int
	var copiedAt string

	err := s.Scan(&c.ID, &c.PhotoSHA256, &c.RelativePath, &c.AbsolutePath,
		&trueDateUnknownInt, &tagsJSON, &copiedAt)
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
