package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

const photoSelectCols = `id, sha256, filepath, library_path_id, filename,
	captured_at, latitude, longitude, altitude,
	camera_make, camera_model, camera_serial, lens_model,
	iso, aperture, shutter_speed, focal_length, flash,
	width, height, orientation, thumbnail_path, flags, ingested_at, source`

// Photo represents a row in the photos table.
type Photo struct {
	ID            int64
	SHA256        string
	Filepath      string
	LibraryPathID int64
	Filename      string
	CapturedAt    *time.Time
	Latitude      *float64
	Longitude     *float64
	Altitude      *float64
	CameraMake    string
	CameraModel   string
	CameraSerial  *string
	LensModel     *string
	ISO           *int
	Aperture      *float64
	ShutterSpeed  *string
	FocalLength   *float64
	Flash         *int
	Width         *int
	Height        *int
	Orientation   *int
	ThumbnailPath *string
	Flags         []string
	IngestedAt    time.Time
	Source        string // 'scan' or 'dropzone'
}

// InsertPhoto inserts a new photo record. Returns the new row ID.
func InsertPhoto(db *sql.DB, p *Photo) (int64, error) {
	flagsJSON, err := json.Marshal(p.Flags)
	if err != nil {
		return 0, fmt.Errorf("marshalling flags: %w", err)
	}
	// Store captured_at as UTC RFC3339 so SQLite's strftime() can parse it.
	var capturedAt any
	if p.CapturedAt != nil {
		capturedAt = p.CapturedAt.UTC().Format(time.RFC3339)
	}
	res, err := db.Exec(`
		INSERT INTO photos (
			sha256, filepath, library_path_id, filename,
			captured_at, latitude, longitude, altitude,
			camera_make, camera_model, camera_serial, lens_model,
			iso, aperture, shutter_speed, focal_length, flash,
			width, height, orientation, thumbnail_path, flags, source
		) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		p.SHA256, p.Filepath, p.LibraryPathID, p.Filename,
		capturedAt, p.Latitude, p.Longitude, p.Altitude,
		p.CameraMake, p.CameraModel, p.CameraSerial, p.LensModel,
		p.ISO, p.Aperture, p.ShutterSpeed, p.FocalLength, p.Flash,
		p.Width, p.Height, p.Orientation, p.ThumbnailPath, string(flagsJSON), sourceVal(p.Source),
	)
	if err != nil {
		return 0, fmt.Errorf("inserting photo: %w", err)
	}
	return res.LastInsertId()
}

// sourceVal returns 'scan' as a default when source is empty.
func sourceVal(s string) string {
	if s == "" {
		return "scan"
	}
	return s
}

// PhotoExistsByHash returns true if a photo with the given SHA256 exists.
func PhotoExistsByHash(db *sql.DB, sha256 string) (bool, error) {
	var count int
	err := db.QueryRow(`SELECT COUNT(*) FROM photos WHERE sha256 = ?`, sha256).Scan(&count)
	return count > 0, err
}

// GetCanonicalFilepath returns the canonical filepath stored in photos for the given hash.
func GetCanonicalFilepath(db *sql.DB, sha256 string) (string, error) {
	var fp string
	err := db.QueryRow(`SELECT filepath FROM photos WHERE sha256 = ?`, sha256).Scan(&fp)
	return fp, err
}

// UpdateThumbnailPath sets the thumbnail_path for a photo by sha256.
func UpdateThumbnailPath(db *sql.DB, sha256, thumbPath string) error {
	_, err := db.Exec(`UPDATE photos SET thumbnail_path = ? WHERE sha256 = ?`, thumbPath, sha256)
	return err
}

// GetPhotoBySHA256 returns a single photo by its hash.
func GetPhotoBySHA256(db *sql.DB, sha256 string) (*Photo, error) {
	row := db.QueryRow(`SELECT `+photoSelectCols+` FROM photos WHERE sha256 = ?`, sha256)
	return scanPhoto(row)
}

func scanPhoto(row *sql.Row) (*Photo, error) {
	var p Photo
	var flagsJSON string
	if err := row.Scan(
		&p.ID, &p.SHA256, &p.Filepath, &p.LibraryPathID, &p.Filename,
		&p.CapturedAt, &p.Latitude, &p.Longitude, &p.Altitude,
		&p.CameraMake, &p.CameraModel, &p.CameraSerial, &p.LensModel,
		&p.ISO, &p.Aperture, &p.ShutterSpeed, &p.FocalLength, &p.Flash,
		&p.Width, &p.Height, &p.Orientation, &p.ThumbnailPath, &flagsJSON, &p.IngestedAt, &p.Source,
	); err != nil {
		return nil, err
	}
	normalizeFlags(&p, flagsJSON)
	return &p, nil
}

func scanPhotoRows(rows *sql.Rows) (*Photo, error) {
	var p Photo
	var flagsJSON string
	if err := rows.Scan(
		&p.ID, &p.SHA256, &p.Filepath, &p.LibraryPathID, &p.Filename,
		&p.CapturedAt, &p.Latitude, &p.Longitude, &p.Altitude,
		&p.CameraMake, &p.CameraModel, &p.CameraSerial, &p.LensModel,
		&p.ISO, &p.Aperture, &p.ShutterSpeed, &p.FocalLength, &p.Flash,
		&p.Width, &p.Height, &p.Orientation, &p.ThumbnailPath, &flagsJSON, &p.IngestedAt, &p.Source,
	); err != nil {
		return nil, err
	}
	normalizeFlags(&p, flagsJSON)
	return &p, nil
}

func normalizeFlags(p *Photo, flagsJSON string) {
	if flagsJSON == "" || flagsJSON == "null" {
		p.Flags = []string{}
		return
	}
	if err := json.Unmarshal([]byte(flagsJSON), &p.Flags); err != nil || p.Flags == nil {
		p.Flags = []string{}
	}
}
