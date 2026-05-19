package db

import (
	"database/sql"
	"fmt"
	"time"
)

type ScanRun struct {
	ID             int64
	LibraryPathID  int64
	StartedAt      time.Time
	FinishedAt     *time.Time
	FilesFound     int
	FilesSkipped   int
	FilesIngested  int
	FilesDuplicate int
	FilesError     int
}

// StartScanRun inserts a new scan_run record and returns its ID.
func StartScanRun(db *sql.DB, libraryPathID int64) (int64, error) {
	res, err := db.Exec(
		`INSERT INTO scan_runs (library_path_id) VALUES (?)`, libraryPathID,
	)
	if err != nil {
		return 0, fmt.Errorf("starting scan run: %w", err)
	}
	return res.LastInsertId()
}

// FinishScanRun updates the scan_run with final counters and finished_at.
func FinishScanRun(db *sql.DB, id int64, found, skipped, ingested, duplicate, errCount int) error {
	_, err := db.Exec(`
		UPDATE scan_runs SET
			finished_at     = datetime('now'),
			files_found     = ?,
			files_skipped   = ?,
			files_ingested  = ?,
			files_duplicate = ?,
			files_error     = ?
		WHERE id = ?`,
		found, skipped, ingested, duplicate, errCount, id,
	)
	return err
}

// GetLatestScanRun returns the most recent scan_run for a library path.
func GetLatestScanRun(db *sql.DB, libraryPathID int64) (*ScanRun, error) {
	row := db.QueryRow(`
		SELECT id, library_path_id, started_at, finished_at,
		       files_found, files_skipped, files_ingested, files_duplicate, files_error
		FROM   scan_runs
		WHERE  library_path_id = ?
		ORDER BY started_at DESC
		LIMIT 1`, libraryPathID)
	var s ScanRun
	if err := row.Scan(
		&s.ID, &s.LibraryPathID, &s.StartedAt, &s.FinishedAt,
		&s.FilesFound, &s.FilesSkipped, &s.FilesIngested, &s.FilesDuplicate, &s.FilesError,
	); err != nil {
		return nil, err
	}
	return &s, nil
}

// GetAllLatestScanRuns returns the most recent scan_run for each library path.
func GetAllLatestScanRuns(db *sql.DB) ([]ScanRun, error) {
	rows, err := db.Query(`
		SELECT s.id, s.library_path_id, s.started_at, s.finished_at,
		       s.files_found, s.files_skipped, s.files_ingested, s.files_duplicate, s.files_error
		FROM   scan_runs s
		INNER JOIN (
			SELECT library_path_id, MAX(started_at) AS max_started
			FROM   scan_runs
			GROUP  BY library_path_id
		) latest ON s.library_path_id = latest.library_path_id
		         AND s.started_at = latest.max_started
		ORDER BY s.library_path_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ScanRun
	for rows.Next() {
		var s ScanRun
		if err := rows.Scan(
			&s.ID, &s.LibraryPathID, &s.StartedAt, &s.FinishedAt,
			&s.FilesFound, &s.FilesSkipped, &s.FilesIngested, &s.FilesDuplicate, &s.FilesError,
		); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
