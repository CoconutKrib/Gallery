package db

import (
	"database/sql"
	"fmt"
	"time"
)

type DuplicatePath struct {
	ID            int64
	SHA256        string
	Filepath      string
	LibraryPathID int64
	DiscoveredAt  time.Time
}

// InsertDuplicatePath records a duplicate filepath for a known hash.
// If the (sha256, filepath) pair already exists the insert is silently ignored.
func InsertDuplicatePath(db *sql.DB, sha256, filepath string, libraryPathID int64) error {
	_, err := db.Exec(`
		INSERT OR IGNORE INTO duplicate_paths (sha256, filepath, library_path_id)
		VALUES (?, ?, ?)`,
		sha256, filepath, libraryPathID,
	)
	if err != nil {
		return fmt.Errorf("inserting duplicate path: %w", err)
	}
	return nil
}

// DuplicatePathExists returns true if this exact (sha256, filepath) pair is already recorded.
func DuplicatePathExists(db *sql.DB, sha256, filepath string) (bool, error) {
	var count int
	err := db.QueryRow(
		`SELECT COUNT(*) FROM duplicate_paths WHERE sha256 = ? AND filepath = ?`,
		sha256, filepath,
	).Scan(&count)
	return count > 0, err
}

// GetDuplicatesBySHA256 returns all known duplicate locations for a hash.
func GetDuplicatesBySHA256(db *sql.DB, sha256 string) ([]DuplicatePath, error) {
	rows, err := db.Query(
		`SELECT id, sha256, filepath, library_path_id, discovered_at
		 FROM duplicate_paths WHERE sha256 = ?`, sha256,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DuplicatePath
	for rows.Next() {
		var d DuplicatePath
		if err := rows.Scan(&d.ID, &d.SHA256, &d.Filepath, &d.LibraryPathID, &d.DiscoveredAt); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
