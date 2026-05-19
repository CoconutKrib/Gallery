package db

import (
	"database/sql"
	"fmt"
	"time"
)

type LibraryPath struct {
	ID            int64
	Path          string
	Label         string
	LastScannedAt *time.Time
}

// UpsertLibraryPath inserts or updates a library path record (matched by path).
// Returns the row ID.
func UpsertLibraryPath(db *sql.DB, path, label string) (int64, error) {
	res, err := db.Exec(`
		INSERT INTO library_paths (path, label)
		VALUES (?, ?)
		ON CONFLICT(path) DO UPDATE SET label = excluded.label`,
		path, label,
	)
	if err != nil {
		return 0, fmt.Errorf("upserting library path: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		// LastInsertId is 0 on a no-op upsert; look it up.
		err = db.QueryRow(`SELECT id FROM library_paths WHERE path = ?`, path).Scan(&id)
	}
	return id, err
}

// GetLibraryPaths returns all library paths.
func GetLibraryPaths(db *sql.DB) ([]LibraryPath, error) {
	rows, err := db.Query(`SELECT id, path, label, last_scanned_at FROM library_paths ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LibraryPath
	for rows.Next() {
		var lp LibraryPath
		if err := rows.Scan(&lp.ID, &lp.Path, &lp.Label, &lp.LastScannedAt); err != nil {
			return nil, err
		}
		out = append(out, lp)
	}
	return out, rows.Err()
}

// TouchLibraryPath updates last_scanned_at to now for the given ID.
func TouchLibraryPath(db *sql.DB, id int64) error {
	_, err := db.Exec(`UPDATE library_paths SET last_scanned_at = datetime('now') WHERE id = ?`, id)
	return err
}

// GetLibraryPathByID returns a single library path by ID.
func GetLibraryPathByID(db *sql.DB, id int64) (*LibraryPath, error) {
	var lp LibraryPath
	err := db.QueryRow(`SELECT id, path, label, last_scanned_at FROM library_paths WHERE id = ?`, id).
		Scan(&lp.ID, &lp.Path, &lp.Label, &lp.LastScannedAt)
	if err != nil {
		return nil, err
	}
	return &lp, nil
}
