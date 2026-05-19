// Package cluster implements rule-based event clustering for photos.
// Rules (from requirements §8):
//  1. Sort all photos by captured_at (photos without a date are skipped).
//  2. Group consecutive photos where the gap between adjacent photos is ≤ event_gap_days.
//  3. If the gap is ≤ event_gap_days but the photo pair is geographically distant
//     (> event_geo_km, configurable), treat it as a new event boundary.
//  4. Each resulting group is stored as one event.
//  5. Event label is auto-generated from the date range: "14–18 Aug 2019".
package cluster

import (
	"database/sql"
	"fmt"
	"log"
	"math"
	"time"

	dbpkg "github.com/halleck/gallery/internal/db"
)

type photoPoint struct {
	id         int64
	capturedAt time.Time
	lat        *float64
	lon        *float64
}

// Run re-clusters all photos and repopulates the events / photo_events tables.
// It is called after every successful scan.
func Run(database *sql.DB, gapDays int, geoKm float64) error {
	if gapDays <= 0 {
		gapDays = 2
	}
	if geoKm <= 0 {
		geoKm = 500
	}

	// Fetch all photos that have a captured_at, ordered chronologically.
	rows, err := database.Query(
		`SELECT id, captured_at, latitude, longitude FROM photos
		 WHERE captured_at IS NOT NULL
		 ORDER BY captured_at ASC, id ASC`,
	)
	if err != nil {
		return fmt.Errorf("querying photos for clustering: %w", err)
	}
	defer rows.Close()

	var points []photoPoint
	for rows.Next() {
		var pp photoPoint
		if err := rows.Scan(&pp.id, &pp.capturedAt, &pp.lat, &pp.lon); err != nil {
			return fmt.Errorf("scanning photo row: %w", err)
		}
		points = append(points, pp)
	}
	rows.Close()

	if len(points) == 0 {
		log.Printf("[cluster] no datable photos found; skipping")
		return nil
	}

	// Wipe existing clusters.
	if err := dbpkg.ClearEvents(database); err != nil {
		return fmt.Errorf("clearing events: %w", err)
	}

	// Group into events.
	type group struct {
		photos []photoPoint
	}
	var groups []group
	current := group{photos: []photoPoint{points[0]}}

	gapDuration := time.Duration(gapDays) * 24 * time.Hour

	for i := 1; i < len(points); i++ {
		prev := points[i-1]
		curr := points[i]

		gap := curr.capturedAt.Sub(prev.capturedAt)
		newBoundary := gap > gapDuration

		if !newBoundary && prev.lat != nil && prev.lon != nil && curr.lat != nil && curr.lon != nil {
			dist := haversineKm(*prev.lat, *prev.lon, *curr.lat, *curr.lon)
			if dist > geoKm {
				newBoundary = true
			}
		}

		if newBoundary {
			groups = append(groups, current)
			current = group{photos: []photoPoint{curr}}
		} else {
			current.photos = append(current.photos, curr)
		}
	}
	groups = append(groups, current)

	log.Printf("[cluster] formed %d events from %d photos", len(groups), len(points))

	// Persist events.
	for _, g := range groups {
		first := g.photos[0]
		last := g.photos[len(g.photos)-1]

		label := formatEventLabel(first.capturedAt, last.capturedAt)

		var centLat, centLon *float64
		centLat, centLon = centroid(g.photos)

		ev := &dbpkg.Event{
			Label:       label,
			StartedAt:   &first.capturedAt,
			EndedAt:     &last.capturedAt,
			CentroidLat: centLat,
			CentroidLon: centLon,
			PhotoCount:  len(g.photos),
		}
		eventID, err := dbpkg.InsertEvent(database, ev)
		if err != nil {
			return fmt.Errorf("inserting event: %w", err)
		}
		for _, pp := range g.photos {
			if err := dbpkg.InsertPhotoEvent(database, pp.id, eventID); err != nil {
				return fmt.Errorf("inserting photo_event: %w", err)
			}
		}
	}

	return nil
}

// formatEventLabel produces a human-readable date range label.
func formatEventLabel(start, end time.Time) string {
	if start.Year() == end.Year() && start.Month() == end.Month() && start.Day() == end.Day() {
		return start.Format("2 Jan 2006")
	}
	if start.Year() == end.Year() && start.Month() == end.Month() {
		return fmt.Sprintf("%d–%d %s %d", start.Day(), end.Day(), start.Format("Jan"), start.Year())
	}
	if start.Year() == end.Year() {
		return fmt.Sprintf("%d %s – %d %s %d", start.Day(), start.Format("Jan"), end.Day(), end.Format("Jan"), start.Year())
	}
	return fmt.Sprintf("%s – %s", start.Format("2 Jan 2006"), end.Format("2 Jan 2006"))
}

// centroid computes the average lat/lon of photos that have GPS, or nil if none do.
func centroid(photos []photoPoint) (*float64, *float64) {
	var sumLat, sumLon float64
	count := 0
	for _, p := range photos {
		if p.lat != nil && p.lon != nil {
			sumLat += *p.lat
			sumLon += *p.lon
			count++
		}
	}
	if count == 0 {
		return nil, nil
	}
	lat := sumLat / float64(count)
	lon := sumLon / float64(count)
	return &lat, &lon
}

func haversineKm(lat1, lon1, lat2, lon2 float64) float64 {
	const R = 6371.0
	dLat := (lat2 - lat1) * math.Pi / 180
	dLon := (lon2 - lon1) * math.Pi / 180
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*math.Pi/180)*math.Cos(lat2*math.Pi/180)*
			math.Sin(dLon/2)*math.Sin(dLon/2)
	return R * 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}
