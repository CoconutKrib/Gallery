package db

import (
	"database/sql"
	"strings"
	"time"
)

// PhotoFilter specifies optional constraints for listing photos.
type PhotoFilter struct {
	LibraryPathID  *int64
	From           *time.Time
	To             *time.Time
	CameraMake     string
	CameraModel    string
	HasGPS         *bool
	Flag           string
	FilepathPrefix string // return only photos whose filepath starts with this + "/"
	Page           int    // 1-based; 0 treated as 1
	PerPage        int    // 0 treated as 50; max 500
	OrderBy        string // "captured_at_desc" (default), "captured_at_asc", "filename"
}

// ListPhotosFiltered returns a page of photos matching the filter, plus total count.
func ListPhotosFiltered(db *sql.DB, f PhotoFilter) ([]Photo, int, error) {
	if f.Page < 1 {
		f.Page = 1
	}
	if f.PerPage < 1 {
		f.PerPage = 50
	}
	if f.PerPage > 500 {
		f.PerPage = 500
	}

	var clauses []string
	var args []any

	if f.LibraryPathID != nil {
		clauses = append(clauses, "library_path_id = ?")
		args = append(args, *f.LibraryPathID)
	}
	if f.From != nil {
		clauses = append(clauses, "captured_at >= ?")
		args = append(args, f.From)
	}
	if f.To != nil {
		clauses = append(clauses, "captured_at <= ?")
		args = append(args, f.To)
	}
	if f.CameraMake != "" {
		clauses = append(clauses, "LOWER(camera_make) LIKE LOWER(?)")
		args = append(args, "%"+f.CameraMake+"%")
	}
	if f.CameraModel != "" {
		clauses = append(clauses, "LOWER(camera_model) LIKE LOWER(?)")
		args = append(args, "%"+f.CameraModel+"%")
	}
	if f.HasGPS != nil {
		if *f.HasGPS {
			clauses = append(clauses, "latitude IS NOT NULL AND longitude IS NOT NULL")
		} else {
			clauses = append(clauses, "(latitude IS NULL OR longitude IS NULL)")
		}
	}
	if f.FilepathPrefix != "" {
		clauses = append(clauses, "filepath LIKE ?")
		args = append(args, f.FilepathPrefix+"/%")
	}
	if f.Flag != "" {
		clauses = append(clauses, `flags LIKE ?`)
		args = append(args, `%"`+f.Flag+`"%`)
	}

	where := ""
	if len(clauses) > 0 {
		where = "WHERE " + strings.Join(clauses, " AND ")
	}

	orderBy := "captured_at DESC, id DESC"
	switch f.OrderBy {
	case "captured_at_asc":
		orderBy = "captured_at ASC, id ASC"
	case "filename":
		orderBy = "filename ASC"
	}

	var total int
	if err := db.QueryRow("SELECT COUNT(*) FROM photos "+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	offset := (f.Page - 1) * f.PerPage
	listArgs := append(args, f.PerPage, offset) //nolint:gocritic
	rows, err := db.Query(
		"SELECT "+photoSelectCols+" FROM photos "+where+
			" ORDER BY "+orderBy+" LIMIT ? OFFSET ?",
		listArgs...,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var photos []Photo
	for rows.Next() {
		p, err := scanPhotoRows(rows)
		if err != nil {
			return nil, 0, err
		}
		photos = append(photos, *p)
	}
	if photos == nil {
		photos = []Photo{}
	}
	return photos, total, rows.Err()
}

// GetPhotosByFilepathPrefix returns all photos (no pagination) whose filepath starts
// with prefix+"/". Used internally by the browse handler to list a directory.
func GetPhotosByFilepathPrefix(db *sql.DB, prefix string) ([]Photo, error) {
	rows, err := db.Query(
		"SELECT "+photoSelectCols+" FROM photos WHERE filepath LIKE ? ORDER BY captured_at ASC, filename ASC",
		prefix+"/%",
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var photos []Photo
	for rows.Next() {
		p, err := scanPhotoRows(rows)
		if err != nil {
			return nil, err
		}
		photos = append(photos, *p)
	}
	return photos, rows.Err()
}

// GetPhotosWithFlags returns photos that have one or more data-deficiency flags set.
func GetPhotosWithFlags(db *sql.DB, page, perPage int) ([]Photo, int, error) {
	hasGPS := false
	return ListPhotosFiltered(db, PhotoFilter{
		HasGPS:  &hasGPS,
		Page:    page,
		PerPage: perPage,
	})
}

// GetGeotaggedPhotos returns all photos with GPS coordinates.
func GetGeotaggedPhotos(db *sql.DB) ([]Photo, error) {
	rows, err := db.Query(
		"SELECT " + photoSelectCols + " FROM photos WHERE latitude IS NOT NULL AND longitude IS NOT NULL",
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var photos []Photo
	for rows.Next() {
		p, err := scanPhotoRows(rows)
		if err != nil {
			return nil, err
		}
		photos = append(photos, *p)
	}
	return photos, rows.Err()
}
