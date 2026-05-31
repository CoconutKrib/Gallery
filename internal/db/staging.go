package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// StagingState represents the state of a staging queue entry.
type StagingState string

const (
	StagingStateStaged   StagingState = "staged"
	StagingStateApproved StagingState = "approved"
	StagingStateRejected StagingState = "rejected"
)

// StagingEntry represents a row in the staging_queue table.
type StagingEntry struct {
	ID              int64
	PhotoSHA256     string
	Title           *string
	Description     *string
	OverrideDate    *string // RFC3339 UTC
	OverrideLat     *float64
	OverrideLon     *float64
	EventID         *int64
	Tags            []string
	TrueDateUnknown bool
	State           StagingState
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// InsertStagingEntry adds a photo to the staging queue with state 'staged'.
// Returns sql.ErrNoRows if the photo does not exist; returns an error with
// "UNIQUE constraint" in the message if the photo is already staged.
func InsertStagingEntry(db *sql.DB, sha256 string) (*StagingEntry, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := db.Exec(`
		INSERT INTO staging_queue (photo_sha256, tags, state, created_at, updated_at)
		VALUES (?, '[]', 'staged', ?, ?)`,
		sha256, now, now)
	if err != nil {
		return nil, fmt.Errorf("inserting staging entry: %w", err)
	}
	id, _ := res.LastInsertId()
	return GetStagingEntryByID(db, id)
}

// GetStagingEntryByID returns a single staging entry by its ID.
func GetStagingEntryByID(db *sql.DB, id int64) (*StagingEntry, error) {
	row := db.QueryRow(`SELECT `+stagingCols+` FROM staging_queue WHERE id = ?`, id)
	return scanStagingEntry(row)
}

// GetStagingEntryBySHA256 returns a single staging entry by photo sha256.
func GetStagingEntryBySHA256(db *sql.DB, sha256 string) (*StagingEntry, error) {
	row := db.QueryRow(`SELECT `+stagingCols+` FROM staging_queue WHERE photo_sha256 = ?`, sha256)
	return scanStagingEntry(row)
}

// ListStagingEntries returns all staging entries, optionally filtered by state.
// Pass "" for state to return all.
func ListStagingEntries(db *sql.DB, state StagingState) ([]*StagingEntry, error) {
	var rows *sql.Rows
	var err error
	if state == "" {
		rows, err = db.Query(`SELECT ` + stagingCols + ` FROM staging_queue ORDER BY created_at DESC`)
	} else {
		rows, err = db.Query(`SELECT `+stagingCols+` FROM staging_queue WHERE state = ? ORDER BY created_at DESC`, string(state))
	}
	if err != nil {
		return nil, fmt.Errorf("listing staging entries: %w", err)
	}
	defer rows.Close()
	var entries []*StagingEntry
	for rows.Next() {
		e, err := scanStagingEntry(rows)
		if err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// UpdateStagingAnnotations updates the annotation fields on a staging entry.
type StagingAnnotationUpdate struct {
	Title           *string
	Description     *string
	OverrideDate    *string
	OverrideLat     *float64
	OverrideLon     *float64
	EventID         *int64
	Tags            *[]string
	TrueDateUnknown *bool
}

// UpdateStagingEntry applies annotation changes to an existing staging entry.
func UpdateStagingEntry(db *sql.DB, id int64, u StagingAnnotationUpdate) error {
	entry, err := GetStagingEntryByID(db, id)
	if err != nil {
		return err
	}
	// Apply updates.
	if u.Title != nil {
		entry.Title = u.Title
	}
	if u.Description != nil {
		entry.Description = u.Description
	}
	if u.OverrideDate != nil {
		entry.OverrideDate = u.OverrideDate
	}
	if u.OverrideLat != nil {
		entry.OverrideLat = u.OverrideLat
	}
	if u.OverrideLon != nil {
		entry.OverrideLon = u.OverrideLon
	}
	if u.EventID != nil {
		entry.EventID = u.EventID
	}
	if u.Tags != nil {
		entry.Tags = *u.Tags
	}
	if u.TrueDateUnknown != nil {
		entry.TrueDateUnknown = *u.TrueDateUnknown
	}

	tagsJSON, err := json.Marshal(entry.Tags)
	if err != nil {
		return fmt.Errorf("marshalling tags: %w", err)
	}
	trueDateUnknownInt := 0
	if entry.TrueDateUnknown {
		trueDateUnknownInt = 1
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = db.Exec(`
		UPDATE staging_queue
		SET title = ?, description = ?, override_date = ?, override_lat = ?, override_lon = ?,
		    event_id = ?, tags = ?, true_date_unknown = ?, updated_at = ?
		WHERE id = ?`,
		entry.Title, entry.Description, entry.OverrideDate, entry.OverrideLat, entry.OverrideLon,
		entry.EventID, string(tagsJSON), trueDateUnknownInt, now, id)
	return err
}

// SetStagingState transitions a staging entry to a new state.
func SetStagingState(db *sql.DB, id int64, state StagingState) error {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := db.Exec(`UPDATE staging_queue SET state = ?, updated_at = ? WHERE id = ?`, string(state), now, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// DeleteStagingEntry removes a staging entry by ID.
func DeleteStagingEntry(db *sql.DB, id int64) error {
	res, err := db.Exec(`DELETE FROM staging_queue WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// ListApprovedStagingEntries returns all entries with state 'approved'.
func ListApprovedStagingEntries(db *sql.DB) ([]*StagingEntry, error) {
	return ListStagingEntries(db, StagingStateApproved)
}

const stagingCols = `id, photo_sha256, title, description, override_date,
	override_lat, override_lon, event_id, tags, true_date_unknown,
	state, created_at, updated_at`

type stagingScanner interface {
	Scan(dest ...any) error
}

func scanStagingEntry(s stagingScanner) (*StagingEntry, error) {
	var e StagingEntry
	var tagsJSON string
	var trueDateUnknownInt int
	var stateStr string
	var createdAt, updatedAt string

	err := s.Scan(
		&e.ID, &e.PhotoSHA256, &e.Title, &e.Description, &e.OverrideDate,
		&e.OverrideLat, &e.OverrideLon, &e.EventID, &tagsJSON, &trueDateUnknownInt,
		&stateStr, &createdAt, &updatedAt,
	)
	if err != nil {
		return nil, err
	}
	e.Tags = []string{}
	if err := json.Unmarshal([]byte(tagsJSON), &e.Tags); err != nil {
		e.Tags = []string{}
	}
	e.TrueDateUnknown = trueDateUnknownInt != 0
	e.State = StagingState(stateStr)
	if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
		e.CreatedAt = t
	}
	if t, err := time.Parse(time.RFC3339, updatedAt); err == nil {
		e.UpdatedAt = t
	}
	return &e, nil
}
