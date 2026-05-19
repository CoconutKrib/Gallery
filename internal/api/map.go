package api

import (
	"net/http"
	"strconv"

	"github.com/halleck/gallery/internal/db"
)

// mapPin is the lightweight payload sent to the map frontend.
type mapPin struct {
	SHA256     string  `json:"sha256"`
	Lat        float64 `json:"lat"`
	Lon        float64 `json:"lon"`
	Filename   string  `json:"filename"`
	CapturedAt string  `json:"captured_at,omitempty"`
}

func photosToMapPins(photos []db.Photo) []mapPin {
	pins := make([]mapPin, 0, len(photos))
	for _, p := range photos {
		if p.Latitude == nil || p.Longitude == nil {
			continue
		}
		pin := mapPin{
			SHA256:   p.SHA256,
			Lat:      *p.Latitude,
			Lon:      *p.Longitude,
			Filename: p.Filename,
		}
		if p.CapturedAt != nil {
			pin.CapturedAt = p.CapturedAt.UTC().Format("2006-01-02T15:04:05Z")
		}
		pins = append(pins, pin)
	}
	return pins
}

// handleMapPhotos returns all geotagged photos as map pins.
// GET /api/map
func (h *Handlers) handleMapPhotos(w http.ResponseWriter, r *http.Request) {
	photos, err := db.GetGeotaggedPhotos(h.db)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	writeJSON(w, http.StatusOK, photosToMapPins(photos))
}

// handleMapNearby returns geotagged photos within a radius of a point.
// GET /api/map/nearby?lat=<float>&lon=<float>&radius_km=<float>
func (h *Handlers) handleMapNearby(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	lat, err := strconv.ParseFloat(q.Get("lat"), 64)
	if err != nil || lat < -90 || lat > 90 {
		writeError(w, http.StatusBadRequest, "invalid lat")
		return
	}
	lon, err := strconv.ParseFloat(q.Get("lon"), 64)
	if err != nil || lon < -180 || lon > 180 {
		writeError(w, http.StatusBadRequest, "invalid lon")
		return
	}
	radiusKm := 10.0
	if s := q.Get("radius_km"); s != "" {
		radiusKm, err = strconv.ParseFloat(s, 64)
		if err != nil || radiusKm <= 0 || radiusKm > 20000 {
			writeError(w, http.StatusBadRequest, "invalid radius_km")
			return
		}
	}

	photos, err := db.GetPhotosNearby(h.db, lat, lon, radiusKm)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	writeJSON(w, http.StatusOK, photosToMapPins(photos))
}
