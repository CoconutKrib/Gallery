package db

import (
	"database/sql"
)

// LibraryDedupSummary holds per-library deduplication statistics.
type LibraryDedupSummary struct {
	LibraryPathID int64  `json:"library_path_id"`
	Label         string `json:"label"`
	TotalPhotos   int    `json:"total_photos"`
	// UniquePhotos: photos whose hash is not found in any other library path.
	UniquePhotos int `json:"unique_photos"`
	// DuplicatePhotos: photos whose hash also appears in duplicate_paths from another library.
	DuplicatePhotos int `json:"duplicate_photos"`
}

// CrossPathOverlap holds the number of shared hashes between two library paths.
type CrossPathOverlap struct {
	LibraryPathIDA   int64  `json:"library_path_id_a"`
	LabelA           string `json:"label_a"`
	LibraryPathIDB   int64  `json:"library_path_id_b"`
	LabelB           string `json:"label_b"`
	SharedPhotoCount int    `json:"shared_photo_count"`
}

// SubtreeEntry describes a photo in a subtree dedup analysis.
type SubtreeEntry struct {
	SHA256         string   `json:"sha256"`
	Filepath       string   `json:"filepath"`
	ThumbnailPath  *string  `json:"thumbnail_path"`
	DuplicateCount int      `json:"duplicate_count"` // 0 = unique
	OtherPaths     []string `json:"other_paths"`
}

// GetLibraryDedupSummaries returns per-library deduplication stats.
func GetLibraryDedupSummaries(db *sql.DB) ([]LibraryDedupSummary, error) {
	// Fetch all library paths.
	rows, err := db.Query(`SELECT id, label FROM library_paths ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type libRow struct {
		id    int64
		label string
	}
	var libs []libRow
	for rows.Next() {
		var l libRow
		if err := rows.Scan(&l.id, &l.label); err != nil {
			return nil, err
		}
		libs = append(libs, l)
	}
	rows.Close()

	var summaries []LibraryDedupSummary
	for _, l := range libs {
		var s LibraryDedupSummary
		s.LibraryPathID = l.id
		s.Label = l.label

		if err := db.QueryRow(
			`SELECT COUNT(*) FROM photos WHERE library_path_id = ?`, l.id,
		).Scan(&s.TotalPhotos); err != nil {
			return nil, err
		}

		// Photos whose sha256 appears in duplicate_paths (from any library).
		if err := db.QueryRow(`
			SELECT COUNT(*) FROM photos p
			WHERE p.library_path_id = ?
			  AND EXISTS (SELECT 1 FROM duplicate_paths d WHERE d.sha256 = p.sha256)`,
			l.id,
		).Scan(&s.DuplicatePhotos); err != nil {
			return nil, err
		}

		s.UniquePhotos = s.TotalPhotos - s.DuplicatePhotos
		summaries = append(summaries, s)
	}
	if summaries == nil {
		summaries = []LibraryDedupSummary{}
	}
	return summaries, nil
}

// GetCrossPathOverlap returns, for every pair of library paths, how many photos overlap.
// A photo "overlaps" when its canonical library_path_id is A, and it also has a
// duplicate_paths entry whose library_path_id is B (or vice versa).
func GetCrossPathOverlap(db *sql.DB) ([]CrossPathOverlap, error) {
	rows, err := db.Query(`
		SELECT
			p.library_path_id          AS lib_a,
			la.label                   AS label_a,
			d.library_path_id          AS lib_b,
			lb.label                   AS label_b,
			COUNT(DISTINCT p.sha256)   AS shared
		FROM photos p
		INNER JOIN duplicate_paths d  ON d.sha256 = p.sha256
		INNER JOIN library_paths la   ON la.id = p.library_path_id
		INNER JOIN library_paths lb   ON lb.id = d.library_path_id
		WHERE p.library_path_id <> d.library_path_id
		GROUP BY p.library_path_id, d.library_path_id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []CrossPathOverlap
	for rows.Next() {
		var o CrossPathOverlap
		if err := rows.Scan(&o.LibraryPathIDA, &o.LabelA, &o.LibraryPathIDB, &o.LabelB, &o.SharedPhotoCount); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	if out == nil {
		out = []CrossPathOverlap{}
	}
	return out, rows.Err()
}

// GetSubtreeDedupEntries returns dedup details for all photos under a given filepath prefix.
func GetSubtreeDedupEntries(db *sql.DB, prefix string) ([]SubtreeEntry, error) {
	rows, err := db.Query(
		`SELECT sha256, filepath, thumbnail_path FROM photos WHERE filepath LIKE ? ORDER BY filepath ASC`,
		prefix+"/%",
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []SubtreeEntry
	for rows.Next() {
		var e SubtreeEntry
		if err := rows.Scan(&e.SHA256, &e.Filepath, &e.ThumbnailPath); err != nil {
			return nil, err
		}
		// Fetch duplicate paths for this hash.
		dpRows, err := db.Query(
			`SELECT filepath FROM duplicate_paths WHERE sha256 = ?`, e.SHA256,
		)
		if err != nil {
			return nil, err
		}
		for dpRows.Next() {
			var fp string
			if err := dpRows.Scan(&fp); err != nil {
				dpRows.Close()
				return nil, err
			}
			e.OtherPaths = append(e.OtherPaths, fp)
		}
		dpRows.Close()
		e.DuplicateCount = len(e.OtherPaths)
		if e.OtherPaths == nil {
			e.OtherPaths = []string{}
		}
		entries = append(entries, e)
	}
	if entries == nil {
		entries = []SubtreeEntry{}
	}
	return entries, rows.Err()
}
