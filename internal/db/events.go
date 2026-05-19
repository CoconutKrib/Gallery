package db

import (
	"database/sql"
	"fmt"
	"time"
)

// Event represents a row in the events table.
type Event struct {
	ID          int64
	Label       string
	StartedAt   *time.Time
	EndedAt     *time.Time
	CentroidLat *float64
	CentroidLon *float64
	PhotoCount  int
}

// ClearEvents deletes all events and photo_events rows (used before re-clustering).
func ClearEvents(db *sql.DB) error {
	_, err := db.Exec(`DELETE FROM photo_events`)
	if err != nil {
		return fmt.Errorf("clearing photo_events: %w", err)
	}
	_, err = db.Exec(`DELETE FROM events`)
	if err != nil {
		return fmt.Errorf("clearing events: %w", err)
	}
	return nil
}

// InsertEvent inserts a new event and returns its ID.
func InsertEvent(db *sql.DB, e *Event) (int64, error) {
	var startedAt, endedAt any
	if e.StartedAt != nil {
		startedAt = e.StartedAt.UTC().Format(time.RFC3339)
	}
	if e.EndedAt != nil {
		endedAt = e.EndedAt.UTC().Format(time.RFC3339)
	}
	res, err := db.Exec(
		`INSERT INTO events (label, started_at, ended_at, centroid_lat, centroid_lon, photo_count)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		e.Label, startedAt, endedAt, e.CentroidLat, e.CentroidLon, e.PhotoCount,
	)
	if err != nil {
		return 0, fmt.Errorf("inserting event: %w", err)
	}
	return res.LastInsertId()
}

// InsertPhotoEvent links a photo to an event in the junction table.
func InsertPhotoEvent(db *sql.DB, photoID, eventID int64) error {
	_, err := db.Exec(
		`INSERT OR IGNORE INTO photo_events (photo_id, event_id) VALUES (?, ?)`,
		photoID, eventID,
	)
	return err
}

// GetAllEvents returns all events ordered by started_at.
func GetAllEvents(db *sql.DB) ([]Event, error) {
	rows, err := db.Query(
		`SELECT id, label, started_at, ended_at, centroid_lat, centroid_lon, photo_count
		 FROM events ORDER BY started_at ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEventRows(rows)
}

// GetEventByID returns a single event by ID.
func GetEventByID(db *sql.DB, id int64) (*Event, error) {
	row := db.QueryRow(
		`SELECT id, label, started_at, ended_at, centroid_lat, centroid_lon, photo_count
		 FROM events WHERE id = ?`, id,
	)
	var e Event
	if err := row.Scan(&e.ID, &e.Label, &e.StartedAt, &e.EndedAt, &e.CentroidLat, &e.CentroidLon, &e.PhotoCount); err != nil {
		return nil, err
	}
	return &e, nil
}

// GetPhotoIDsForEvent returns all photo IDs linked to an event.
func GetPhotoIDsForEvent(db *sql.DB, eventID int64) ([]int64, error) {
	rows, err := db.Query(
		`SELECT photo_id FROM photo_events WHERE event_id = ? ORDER BY photo_id ASC`, eventID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// GetPhotosForEvent returns all photos linked to an event, ordered by captured_at.
func GetPhotosForEvent(db *sql.DB, eventID int64) ([]Photo, error) {
	rows, err := db.Query(
		`SELECT `+photoSelectCols+`
		 FROM photos
		 INNER JOIN photo_events ON photos.id = photo_events.photo_id
		 WHERE photo_events.event_id = ?
		 ORDER BY captured_at ASC, id ASC`, eventID,
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

// GetEventForPhoto returns the event ID (if any) for a given photo ID.
func GetEventForPhoto(db *sql.DB, photoID int64) (*int64, error) {
	var eventID int64
	err := db.QueryRow(
		`SELECT event_id FROM photo_events WHERE photo_id = ? LIMIT 1`, photoID,
	).Scan(&eventID)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &eventID, nil
}

func scanEventRows(rows *sql.Rows) ([]Event, error) {
	var events []Event
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.Label, &e.StartedAt, &e.EndedAt, &e.CentroidLat, &e.CentroidLon, &e.PhotoCount); err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	if events == nil {
		events = []Event{}
	}
	return events, rows.Err()
}
