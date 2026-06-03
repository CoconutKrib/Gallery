package db

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// Person represents a row in the people table.
type Person struct {
	ID          int64
	Name        string
	Notes       *string
	CoverFaceID *int64
	PhotoCount  int // populated by ListPeople
	CreatedAt   time.Time
}

// PersonUpdate carries fields that PATCH /api/people/{id} may change.
// nil = do not change. For nullable fields (Notes, CoverFaceID): the field is a
// *string/*int64 pointer — nil means "don't change", pointing to nil means "clear".
type PersonUpdate struct {
	Name        *string
	Notes       **string // outer nil = skip; outer non-nil inner nil = clear to NULL
	CoverFaceID **int64
}

// Face represents a row in the faces table.
type Face struct {
	ID          int64
	PhotoID     int64
	PersonID    *int64
	PersonName  *string // populated by join queries, not stored
	PhotoSHA256 string  // populated by join queries that include photos.sha256
	BboxX       *float64
	BboxY       *float64
	BboxW       *float64
	BboxH       *float64
	Source      string   // 'manual' | 'auto'
	Confidence  *float64 // nil for manual tags
	Embedding   []byte   // nil in Phase A
	Verified    bool
	CreatedAt   time.Time
}

// FaceUpdate carries fields that PATCH /api/faces/{id} may change.
type FaceUpdate struct {
	PersonID **int64
	BboxX    **float64
	BboxY    **float64
	BboxW    **float64
	BboxH    **float64
	Verified *bool
}

// ---- People CRUD ----

// InsertPerson creates a new person record and returns the new row ID.
func InsertPerson(database *sql.DB, name string, notes *string) (int64, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := database.Exec(
		`INSERT INTO people (name, notes, created_at) VALUES (?, ?, ?)`,
		name, notes, now,
	)
	if err != nil {
		return 0, fmt.Errorf("inserting person: %w", err)
	}
	return res.LastInsertId()
}

// GetPersonByID returns a single person or sql.ErrNoRows.
func GetPersonByID(database *sql.DB, id int64) (*Person, error) {
	p := &Person{}
	var notesNull sql.NullString
	var coverNull sql.NullInt64
	var createdStr string
	err := database.QueryRow(
		`SELECT id, name, notes, cover_face_id, created_at FROM people WHERE id = ?`, id,
	).Scan(&p.ID, &p.Name, &notesNull, &coverNull, &createdStr)
	if err != nil {
		return nil, err
	}
	if notesNull.Valid {
		p.Notes = &notesNull.String
	}
	if coverNull.Valid {
		p.CoverFaceID = &coverNull.Int64
	}
	p.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
	return p, nil
}

// ListPeople returns all people sorted by name, with photo_count populated via a join.
func ListPeople(database *sql.DB) ([]*Person, error) {
	rows, err := database.Query(`
		SELECT p.id, p.name, p.notes, p.cover_face_id, p.created_at,
		       COUNT(DISTINCT lc.id) AS photo_count
		FROM people p
		LEFT JOIN faces f         ON f.person_id   = p.id
		LEFT JOIN photos ph       ON ph.id          = f.photo_id
		LEFT JOIN library_copies lc ON lc.photo_sha256 = ph.sha256
		GROUP BY p.id
		ORDER BY p.name`)
	if err != nil {
		return nil, fmt.Errorf("listing people: %w", err)
	}
	defer rows.Close()

	var out []*Person
	for rows.Next() {
		p := &Person{}
		var notesNull sql.NullString
		var coverNull sql.NullInt64
		var createdStr string
		if err := rows.Scan(&p.ID, &p.Name, &notesNull, &coverNull, &createdStr, &p.PhotoCount); err != nil {
			return nil, fmt.Errorf("scanning person: %w", err)
		}
		if notesNull.Valid {
			p.Notes = &notesNull.String
		}
		if coverNull.Valid {
			p.CoverFaceID = &coverNull.Int64
		}
		p.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
		out = append(out, p)
	}
	return out, rows.Err()
}

// UpdatePerson applies a partial update to a people row.
func UpdatePerson(database *sql.DB, id int64, u PersonUpdate) error {
	var sets []string
	var args []any

	if u.Name != nil {
		sets = append(sets, "name = ?")
		args = append(args, *u.Name)
	}
	if u.Notes != nil {
		sets = append(sets, "notes = ?")
		args = append(args, *u.Notes) // *u.Notes may be nil, which sets NULL
	}
	if u.CoverFaceID != nil {
		sets = append(sets, "cover_face_id = ?")
		args = append(args, *u.CoverFaceID) // may be nil → NULL
	}

	if len(sets) == 0 {
		return nil
	}
	args = append(args, id)
	_, err := database.Exec(
		"UPDATE people SET "+strings.Join(sets, ", ")+" WHERE id = ?",
		args...,
	)
	return err
}

// DeletePerson removes a person and clears all face assignments.
// Runs in a transaction:
//  1. Clears cover_face_id to break the circular FK.
//  2. NULLs faces.person_id for all linked faces (does not delete face rows).
//  3. Deletes the person row.
func DeletePerson(database *sql.DB, id int64) error {
	tx, err := database.Begin()
	if err != nil {
		return fmt.Errorf("beginning tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	if _, err = tx.Exec(`UPDATE people SET cover_face_id = NULL WHERE id = ?`, id); err != nil {
		return fmt.Errorf("clearing cover_face_id: %w", err)
	}
	if _, err = tx.Exec(`UPDATE faces SET person_id = NULL WHERE person_id = ?`, id); err != nil {
		return fmt.Errorf("nulling face assignments: %w", err)
	}
	if _, err = tx.Exec(`DELETE FROM people WHERE id = ?`, id); err != nil {
		return fmt.Errorf("deleting person: %w", err)
	}

	return tx.Commit()
}

// MergePerson reassigns all faces from fromID to toID, then deletes fromID.
// The from person's cover_face_id is cleared first to break the circular FK.
func MergePerson(database *sql.DB, fromID, toID int64) error {
	tx, err := database.Begin()
	if err != nil {
		return fmt.Errorf("beginning tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	if _, err = tx.Exec(`UPDATE people SET cover_face_id = NULL WHERE id = ?`, fromID); err != nil {
		return fmt.Errorf("clearing cover_face_id: %w", err)
	}
	if _, err = tx.Exec(`UPDATE faces SET person_id = ? WHERE person_id = ?`, toID, fromID); err != nil {
		return fmt.Errorf("reassigning faces: %w", err)
	}
	if _, err = tx.Exec(`DELETE FROM people WHERE id = ?`, fromID); err != nil {
		return fmt.Errorf("deleting source person: %w", err)
	}

	return tx.Commit()
}

// ---- Face CRUD ----

// InsertFace inserts a new face row and returns the new ID.
func InsertFace(database *sql.DB, f Face) (int64, error) {
	verified := 0
	if f.Verified {
		verified = 1
	}
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := database.Exec(`
		INSERT INTO faces
		  (photo_id, person_id, bbox_x, bbox_y, bbox_w, bbox_h,
		   source, confidence, embedding, verified, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		f.PhotoID, f.PersonID, f.BboxX, f.BboxY, f.BboxW, f.BboxH,
		f.Source, f.Confidence, f.Embedding, verified, now,
	)
	if err != nil {
		return 0, fmt.Errorf("inserting face: %w", err)
	}
	return res.LastInsertId()
}

// GetFaceByID returns a single face row or sql.ErrNoRows.
func GetFaceByID(database *sql.DB, id int64) (*Face, error) {
	row := database.QueryRow(`
		SELECT f.id, f.photo_id, f.person_id, pe.name,
		       f.bbox_x, f.bbox_y, f.bbox_w, f.bbox_h,
		       f.source, f.confidence, f.embedding, f.verified, f.created_at
		FROM faces f
		LEFT JOIN people p ON p.id = f.person_id
		WHERE f.id = ?`, id)
	return scanFace(row)
}

// ListFacesForLibraryCopy returns all face tags for a given library copy (by its row ID).
func ListFacesForLibraryCopy(database *sql.DB, libraryCopyID int64) ([]*Face, error) {
	rows, err := database.Query(`
		SELECT f.id, f.photo_id, f.person_id, p.name,
		       f.bbox_x, f.bbox_y, f.bbox_w, f.bbox_h,
		       f.source, f.confidence, f.embedding, f.verified, f.created_at
		FROM faces f
		LEFT JOIN people p         ON p.id           = f.person_id
		JOIN  photos ph            ON ph.id           = f.photo_id
		JOIN  library_copies lc    ON lc.photo_sha256 = ph.sha256
		WHERE lc.id = ?
		ORDER BY f.created_at`, libraryCopyID)
	if err != nil {
		return nil, fmt.Errorf("listing faces: %w", err)
	}
	defer rows.Close()
	return scanFaces(rows)
}

// GetPhotoIDForLibraryCopy resolves the photos.id for a library copy.
// Returns sql.ErrNoRows if the copy does not exist.
func GetPhotoIDForLibraryCopy(database *sql.DB, libraryCopyID int64) (int64, error) {
	var photoID int64
	err := database.QueryRow(`
		SELECT ph.id FROM photos ph
		JOIN library_copies lc ON lc.photo_sha256 = ph.sha256
		WHERE lc.id = ?`, libraryCopyID).Scan(&photoID)
	return photoID, err
}

// DeleteFace removes a face row and clears any cover_face_id references to it.
func DeleteFace(database *sql.DB, id int64) error {
	tx, err := database.Begin()
	if err != nil {
		return fmt.Errorf("beginning tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	if _, err = tx.Exec(`UPDATE people SET cover_face_id = NULL WHERE cover_face_id = ?`, id); err != nil {
		return fmt.Errorf("clearing cover_face_id: %w", err)
	}
	if _, err = tx.Exec(`DELETE FROM faces WHERE id = ?`, id); err != nil {
		return fmt.Errorf("deleting face: %w", err)
	}
	return tx.Commit()
}

// UpdateFace applies a partial update to a face row.
func UpdateFace(database *sql.DB, id int64, u FaceUpdate) error {
	var sets []string
	var args []any

	if u.PersonID != nil {
		sets = append(sets, "person_id = ?")
		args = append(args, *u.PersonID)
	}
	if u.BboxX != nil {
		sets = append(sets, "bbox_x = ?")
		args = append(args, *u.BboxX)
	}
	if u.BboxY != nil {
		sets = append(sets, "bbox_y = ?")
		args = append(args, *u.BboxY)
	}
	if u.BboxW != nil {
		sets = append(sets, "bbox_w = ?")
		args = append(args, *u.BboxW)
	}
	if u.BboxH != nil {
		sets = append(sets, "bbox_h = ?")
		args = append(args, *u.BboxH)
	}
	if u.Verified != nil {
		v := 0
		if *u.Verified {
			v = 1
		}
		sets = append(sets, "verified = ?")
		args = append(args, v)
	}

	if len(sets) == 0 {
		return nil
	}
	args = append(args, id)
	_, err := database.Exec(
		"UPDATE faces SET "+strings.Join(sets, ", ")+" WHERE id = ?",
		args...,
	)
	return err
}

// ListLibraryCopiesForPerson returns a page of library copies containing a given person.
func ListLibraryCopiesForPerson(database *sql.DB, personID int64, page, perPage int) ([]*LibraryCopy, int, error) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 50
	}
	if perPage > 500 {
		perPage = 500
	}

	var total int
	err := database.QueryRow(`
		SELECT COUNT(DISTINCT lc.id)
		FROM library_copies lc
		JOIN photos ph ON ph.sha256 = lc.photo_sha256
		JOIN faces f   ON f.photo_id = ph.id
		WHERE f.person_id = ?`, personID).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("counting person photos: %w", err)
	}

	// Prefix each column with lc. to avoid ambiguity in the JOIN.
	lcCols := "lc." + strings.Join(strings.Split(libraryCopyCols, ", "), ", lc.")
	rows, err := database.Query(
		"SELECT DISTINCT "+lcCols+`
		 FROM library_copies lc
		 JOIN photos ph ON ph.sha256 = lc.photo_sha256
		 JOIN faces f   ON f.photo_id = ph.id
		 WHERE f.person_id = ?
		 ORDER BY lc.copied_at DESC
		 LIMIT ? OFFSET ?`,

		personID, perPage, (page-1)*perPage)
	if err != nil {
		return nil, 0, fmt.Errorf("listing person photos: %w", err)
	}
	defer rows.Close()

	var copies []*LibraryCopy
	for rows.Next() {
		lc, err := scanLibraryCopy(rows)
		if err != nil {
			return nil, 0, err
		}
		copies = append(copies, lc)
	}
	return copies, total, rows.Err()
}

// ---- scan helpers ----

func scanFace(row libraryCopyScanner) (*Face, error) {
	f := &Face{}
	var personIDNull sql.NullInt64
	var personNameNull sql.NullString
	var bboxX, bboxY, bboxW, bboxH sql.NullFloat64
	var confidence sql.NullFloat64
	var verified int
	var createdStr string

	if err := row.Scan(
		&f.ID, &f.PhotoID, &personIDNull, &personNameNull,
		&bboxX, &bboxY, &bboxW, &bboxH,
		&f.Source, &confidence, &f.Embedding, &verified, &createdStr,
	); err != nil {
		return nil, err
	}
	if personIDNull.Valid {
		f.PersonID = &personIDNull.Int64
	}
	if personNameNull.Valid {
		f.PersonName = &personNameNull.String
	}
	if bboxX.Valid {
		f.BboxX = &bboxX.Float64
	}
	if bboxY.Valid {
		f.BboxY = &bboxY.Float64
	}
	if bboxW.Valid {
		f.BboxW = &bboxW.Float64
	}
	if bboxH.Valid {
		f.BboxH = &bboxH.Float64
	}
	if confidence.Valid {
		f.Confidence = &confidence.Float64
	}
	f.Verified = verified != 0
	f.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
	return f, nil
}

// scanFaceWithSHA scans a Face row that includes photo.sha256 as the
// 14th column (appended after the standard 13 columns).
func scanFaceWithSHA(row libraryCopyScanner) (*Face, error) {
	f := &Face{}
	var personIDNull sql.NullInt64
	var personNameNull sql.NullString
	var bboxX, bboxY, bboxW, bboxH sql.NullFloat64
	var confidence sql.NullFloat64
	var verified int
	var createdStr string
	var sha256 sql.NullString

	if err := row.Scan(
		&f.ID, &f.PhotoID, &personIDNull, &personNameNull,
		&bboxX, &bboxY, &bboxW, &bboxH,
		&f.Source, &confidence, &f.Embedding, &verified, &createdStr,
		&sha256,
	); err != nil {
		return nil, err
	}
	if personIDNull.Valid {
		f.PersonID = &personIDNull.Int64
	}
	if personNameNull.Valid {
		f.PersonName = &personNameNull.String
	}
	if bboxX.Valid {
		f.BboxX = &bboxX.Float64
	}
	if bboxY.Valid {
		f.BboxY = &bboxY.Float64
	}
	if bboxW.Valid {
		f.BboxW = &bboxW.Float64
	}
	if bboxH.Valid {
		f.BboxH = &bboxH.Float64
	}
	if confidence.Valid {
		f.Confidence = &confidence.Float64
	}
	f.Verified = verified != 0
	f.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
	if sha256.Valid {
		f.PhotoSHA256 = sha256.String
	}
	return f, nil
}

func scanFacesWithSHA(rows *sql.Rows) ([]*Face, error) {
	var out []*Face
	for rows.Next() {
		f, err := scanFaceWithSHA(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning face with sha: %w", err)
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func scanFaces(rows *sql.Rows) ([]*Face, error) {
	var out []*Face
	for rows.Next() {
		f, err := scanFace(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning face: %w", err)
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// ---- Recognition helpers ----

// HasAutoFacesForPhoto returns true if any source='auto' faces already exist
// for the given photo_id. Used for scan idempotency.
func HasAutoFacesForPhoto(database *sql.DB, photoID int64) (bool, error) {
	var count int
	err := database.QueryRow(
		`SELECT COUNT(*) FROM faces WHERE photo_id = ? AND source = 'auto'`,
		photoID,
	).Scan(&count)
	return count > 0, err
}

// ListFacesByPerson returns all faces assigned to a given person (verified or unverified).
// Returns faces joined with photo sha256 so callers can display face crops.
func ListFacesByPerson(database *sql.DB, personID int64, page, perPage int) ([]*Face, int, error) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 50
	}
	if perPage > 500 {
		perPage = 500
	}
	var total int
	if err := database.QueryRow(
		`SELECT COUNT(*) FROM faces WHERE person_id = ?`, personID,
	).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("counting faces for person: %w", err)
	}
	offset := (page - 1) * perPage
	rows, err := database.Query(`
		SELECT f.id, f.photo_id, f.person_id, pe.name,
		       f.bbox_x, f.bbox_y, f.bbox_w, f.bbox_h,
		       f.source, f.confidence, f.embedding, f.verified, f.created_at,
		       ph.sha256
		FROM   faces f
		JOIN   photos ph ON ph.id = f.photo_id
		LEFT   JOIN people pe ON pe.id = f.person_id
		WHERE  f.person_id = ?
		ORDER  BY f.id DESC
		LIMIT  ? OFFSET ?`, personID, perPage, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("querying faces by person: %w", err)
	}
	defer rows.Close()
	faces, err := scanFacesWithSHA(rows)
	return faces, total, err
}

// ListUnidentifiedFaces returns a page of auto-detected faces with no person assigned.
func ListUnidentifiedFaces(database *sql.DB, page, perPage int) ([]*Face, int, error) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 50
	}
	if perPage > 500 {
		perPage = 500
	}

	var total int
	if err := database.QueryRow(
		`SELECT COUNT(*) FROM faces WHERE source = 'auto' AND person_id IS NULL`,
	).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("counting unidentified faces: %w", err)
	}

	rows, err := database.Query(`
		SELECT f.id, f.photo_id, f.person_id, NULL,
		       f.bbox_x, f.bbox_y, f.bbox_w, f.bbox_h,
		       f.source, f.confidence, f.embedding, f.verified, f.created_at
		FROM faces f
		WHERE f.source = 'auto' AND f.person_id IS NULL
		ORDER BY f.created_at DESC
		LIMIT ? OFFSET ?`,
		perPage, (page-1)*perPage,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("listing unidentified faces: %w", err)
	}
	defer rows.Close()

	faces, err := scanFaces(rows)
	return faces, total, err
}

// ListUnverifiedSuggestions returns auto-detected faces that have a candidate
// person assigned but have not yet been confirmed by the user (verified=0).
func ListUnverifiedSuggestions(database *sql.DB) ([]*Face, error) {
	rows, err := database.Query(`
		SELECT f.id, f.photo_id, f.person_id, p.name,
		       f.bbox_x, f.bbox_y, f.bbox_w, f.bbox_h,
		       f.source, f.confidence, f.embedding, f.verified, f.created_at
		FROM faces f
		LEFT JOIN people p ON p.id = f.person_id
		WHERE f.source = 'auto' AND f.person_id IS NOT NULL AND f.verified = 0
		ORDER BY f.created_at DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("listing unverified suggestions: %w", err)
	}
	defer rows.Close()
	return scanFaces(rows)
}

// GetVerifiedFacesWithEmbeddings returns all verified faces that have an
// embedding and a person_id — used by the suggestion pipeline.
func GetVerifiedFacesWithEmbeddings(database *sql.DB) ([]*Face, error) {
	rows, err := database.Query(`
			SELECT f.id, f.photo_id, f.person_id, NULL,
			       f.bbox_x, f.bbox_y, f.bbox_w, f.bbox_h,
			       f.source, f.confidence, f.embedding, f.verified, f.created_at
			FROM faces f
			WHERE f.verified = 1
			  AND f.person_id IS NOT NULL
			  AND f.embedding IS NOT NULL
			ORDER BY f.id`)
	if err != nil {
		return nil, fmt.Errorf("getting verified faces: %w", err)
	}
	defer rows.Close()
	return scanFaces(rows)
}

// GetUnidentifiedFacesWithEmbeddings returns all unidentified auto-detected
// faces that have an embedding — used by the clustering pipeline.
func GetUnidentifiedFacesWithEmbeddings(database *sql.DB) ([]*Face, error) {
	rows, err := database.Query(`
			SELECT f.id, f.photo_id, f.person_id, NULL,
			       f.bbox_x, f.bbox_y, f.bbox_w, f.bbox_h,
			       f.source, f.confidence, f.embedding, f.verified, f.created_at
			FROM faces f
			WHERE f.source = 'auto'
			  AND f.person_id IS NULL
			  AND f.embedding IS NOT NULL
			ORDER BY f.id`)
	if err != nil {
		return nil, fmt.Errorf("getting unidentified faces: %w", err)
	}
	defer rows.Close()
	return scanFaces(rows)
}

// SetFacePersonCandidate assigns a candidate person_id to an auto-detected face,
// leaving verified=0. Only updates faces that still have person_id IS NULL
// (prevents overwriting user decisions made after the scan started).
func SetFacePersonCandidate(database *sql.DB, faceID, personID int64) error {
	_, err := database.Exec(
		`UPDATE faces SET person_id = ? WHERE id = ? AND person_id IS NULL`,
		personID, faceID,
	)
	return err
}
