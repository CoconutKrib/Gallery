package db

import (
	"database/sql"
	"math"
	"strings"
	"time"
)

// PhotoFilter specifies optional constraints for listing photos.
type PhotoFilter struct {
	LibraryPathID          *int64
	InLibrary              *bool
	From                   *time.Time
	To                     *time.Time
	CameraMake             string
	CameraModel            string
	Source                 string // "scan" | "dropzone"
	HasGPS                 *bool
	Flag                   string
	Keyword                string // matched against filepath and filename (case-insensitive LIKE)
	Tag                    string // must appear in library_copies.tags JSON array (case-insensitive LIKE)
	EventID                *int64
	HasDateOverride        *bool
	TrueDateUnknown        *bool
	PersonID               *int64
	HasPeople              *bool
	FaceVerified           *bool
	FaceSource             string // "manual" | "auto"
	IncludeUnverifiedFaces bool
	FilepathPrefix         string // return only photos whose filepath starts with this + "/"
	Page                   int    // 1-based; 0 treated as 1
	PerPage                int    // 0 treated as 50; max 500
	OrderBy                string // "captured_at_desc" (default), "captured_at_asc", "filename"
}

func buildPhotoFilterWhere(f PhotoFilter) (string, []any) {
	var clauses []string
	var args []any

	if f.LibraryPathID != nil {
		clauses = append(clauses, "library_path_id = ?")
		args = append(args, *f.LibraryPathID)
	}
	if f.InLibrary != nil {
		if *f.InLibrary {
			clauses = append(clauses, `EXISTS (SELECT 1 FROM library_copies lc WHERE lc.photo_sha256 = photos.sha256)`)
		} else {
			clauses = append(clauses, `NOT EXISTS (SELECT 1 FROM library_copies lc WHERE lc.photo_sha256 = photos.sha256)`)
		}
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
	if f.Source != "" {
		clauses = append(clauses, "source = ?")
		args = append(args, f.Source)
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
	if f.Keyword != "" {
		clauses = append(clauses, "(LOWER(filepath) LIKE LOWER(?) OR LOWER(filename) LIKE LOWER(?))")
		like := "%" + strings.ReplaceAll(f.Keyword, "%", "\\%") + "%"
		args = append(args, like, like)
	}

	if f.Tag != "" {
		clauses = append(clauses, `EXISTS (
			SELECT 1 FROM library_copies lc
			WHERE lc.photo_sha256 = photos.sha256
			  AND LOWER(lc.tags) LIKE LOWER(?)
		)`)
		args = append(args, "%\""+f.Tag+"\"%")
	}
	if f.EventID != nil {
		clauses = append(clauses, `EXISTS (
			SELECT 1 FROM library_copies lc
			WHERE lc.photo_sha256 = photos.sha256
			  AND lc.event_id = ?
		)`)
		args = append(args, *f.EventID)
	}
	if f.HasDateOverride != nil {
		if *f.HasDateOverride {
			clauses = append(clauses, `EXISTS (
				SELECT 1 FROM library_copies lc
				WHERE lc.photo_sha256 = photos.sha256
				  AND lc.override_date IS NOT NULL
			)`)
		} else {
			clauses = append(clauses, `EXISTS (
				SELECT 1 FROM library_copies lc
				WHERE lc.photo_sha256 = photos.sha256
				  AND lc.override_date IS NULL
			)`)
		}
	}
	if f.TrueDateUnknown != nil {
		v := 0
		if *f.TrueDateUnknown {
			v = 1
		}
		clauses = append(clauses, `EXISTS (
			SELECT 1 FROM library_copies lc
			WHERE lc.photo_sha256 = photos.sha256
			  AND lc.true_date_unknown = ?
		)`)
		args = append(args, v)
	}

	facePreds := []string{"f.photo_id = photos.id"}
	var faceArgs []any
	if f.FaceSource != "" {
		facePreds = append(facePreds, "f.source = ?")
		faceArgs = append(faceArgs, f.FaceSource)
	}
	if f.FaceVerified != nil {
		v := 0
		if *f.FaceVerified {
			v = 1
		}
		facePreds = append(facePreds, "f.verified = ?")
		faceArgs = append(faceArgs, v)
	}
	if f.PersonID != nil {
		facePreds = append(facePreds, "f.person_id = ?")
		faceArgs = append(faceArgs, *f.PersonID)
	}

	hasFaceConstraints := f.FaceSource != "" || f.FaceVerified != nil || f.PersonID != nil

	if f.HasPeople != nil {
		if f.PersonID != nil && !*f.HasPeople {
			clauses = append(clauses, "1 = 0")
		} else if *f.HasPeople {
			preds := append([]string{}, facePreds...)
			preds = append(preds, "f.person_id IS NOT NULL")
			clauses = append(clauses, "EXISTS (SELECT 1 FROM faces f WHERE "+strings.Join(preds, " AND ")+")")
			args = append(args, faceArgs...)
		} else {
			preds := []string{"f.photo_id = photos.id", "f.person_id IS NOT NULL"}
			var predArgs []any
			if f.FaceSource != "" {
				preds = append(preds, "f.source = ?")
				predArgs = append(predArgs, f.FaceSource)
			}
			if f.FaceVerified != nil {
				v := 0
				if *f.FaceVerified {
					v = 1
				}
				preds = append(preds, "f.verified = ?")
				predArgs = append(predArgs, v)
			}
			clauses = append(clauses, "NOT EXISTS (SELECT 1 FROM faces f WHERE "+strings.Join(preds, " AND ")+")")
			args = append(args, predArgs...)
		}
	} else if hasFaceConstraints {
		clauses = append(clauses, "EXISTS (SELECT 1 FROM faces f WHERE "+strings.Join(facePreds, " AND ")+")")
		args = append(args, faceArgs...)
	}

	where := ""
	if len(clauses) > 0 {
		where = "WHERE " + strings.Join(clauses, " AND ")
	}
	return where, args
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

	where, args := buildPhotoFilterWhere(f)

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

// ListPhotosFilteredAll returns all photos matching the filter (no pagination).
func ListPhotosFilteredAll(db *sql.DB, f PhotoFilter) ([]Photo, error) {
	where, args := buildPhotoFilterWhere(f)

	orderBy := "captured_at DESC, id DESC"
	switch f.OrderBy {
	case "captured_at_asc":
		orderBy = "captured_at ASC, id ASC"
	case "filename":
		orderBy = "filename ASC"
	}

	rows, err := db.Query(
		"SELECT "+photoSelectCols+" FROM photos "+where+" ORDER BY "+orderBy,
		args...,
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
	if photos == nil {
		photos = []Photo{}
	}
	return photos, rows.Err()
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

// haversineKm returns the great-circle distance in kilometres between two lat/lon points.
func haversineKm(lat1, lon1, lat2, lon2 float64) float64 {
	const R = 6371.0
	dLat := (lat2 - lat1) * math.Pi / 180
	dLon := (lon2 - lon1) * math.Pi / 180
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*math.Pi/180)*math.Cos(lat2*math.Pi/180)*
			math.Sin(dLon/2)*math.Sin(dLon/2)
	return R * 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}

// GetPhotosNearby returns all geotagged photos within radiusKm of (lat, lon).
// A bounding-box pre-filter is applied in SQL; precise Haversine filtering is done in Go.
func GetPhotosNearby(db *sql.DB, lat, lon, radiusKm float64) ([]Photo, error) {
	// 1 degree latitude ≈ 111 km; longitude degree shrinks with cos(lat).
	latDelta := radiusKm / 111.0
	lonDelta := radiusKm / (111.0 * math.Cos(lat*math.Pi/180))

	rows, err := db.Query(
		"SELECT "+photoSelectCols+" FROM photos WHERE latitude IS NOT NULL AND longitude IS NOT NULL"+
			" AND latitude BETWEEN ? AND ? AND longitude BETWEEN ? AND ?",
		lat-latDelta, lat+latDelta, lon-lonDelta, lon+lonDelta,
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
		if haversineKm(lat, lon, *p.Latitude, *p.Longitude) <= radiusKm {
			photos = append(photos, *p)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if photos == nil {
		photos = []Photo{}
	}
	return photos, nil
}
