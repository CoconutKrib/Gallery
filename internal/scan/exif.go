package scan

import (
	"fmt"
	"math"
	"os"
	"strings"
	"time"

	"github.com/rwcarlsen/goexif/exif"
)

// EXIFData holds the EXIF fields we care about.
type EXIFData struct {
	CameraMake   string
	CameraModel  string
	CameraSerial string

	CapturedAt *time.Time

	Latitude  *float64
	Longitude *float64
	Altitude  *float64

	LensModel    string
	ISO          *int
	Aperture     *float64
	ShutterSpeed string
	FocalLength  *float64
	Flash        *int
	Width        *int
	Height       *int
	Orientation  *int

	// TrueDateUnknown is set by the lenient (dropzone) scanner when captured_at
	// falls back to file mtime rather than real EXIF date.
	TrueDateUnknown bool
}

// Flags returned when EXIF is present but fields are missing.
const (
	FlagMissingDate = "missing_date"
	FlagMissingGPS  = "missing_gps"
)

// ReadEXIF reads EXIF metadata from the file at path.
// Returns (nil, nil) if the file has no EXIF at all (not an error — caller decides).
func ReadEXIF(path string) (*EXIFData, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening file for exif: %w", err)
	}
	defer f.Close()

	x, err := exif.Decode(f)
	if err != nil {
		// No EXIF data is not a hard error; signal with nil.
		return nil, nil //nolint:nilerr
	}

	data := &EXIFData{}

	data.CameraMake = stringTag(x, exif.Make)
	data.CameraModel = stringTag(x, exif.Model)
	// BodySerialNumber (0xA431) is not in goexif's named constants; attempt a raw lookup.
	data.CameraSerial = stringTag(x, "BodySerialNumber")
	data.LensModel = stringTag(x, exif.LensModel)

	if t, err := x.DateTime(); err == nil {
		data.CapturedAt = &t
	}

	if lat, lon, err := x.LatLong(); err == nil {
		data.Latitude = &lat
		data.Longitude = &lon
	}

	if alt := rationalTag(x, exif.GPSAltitude); alt != nil {
		data.Altitude = alt
	}

	if iso := intTag(x, exif.ISOSpeedRatings); iso != nil {
		data.ISO = iso
	}
	if ap := rationalTag(x, exif.FNumber); ap != nil {
		data.Aperture = ap
	}
	if ss := stringTag(x, exif.ExposureTime); ss != "" {
		data.ShutterSpeed = ss
	}
	if fl := rationalTag(x, exif.FocalLength); fl != nil {
		data.FocalLength = fl
	}
	if flash := intTag(x, exif.Flash); flash != nil {
		data.Flash = flash
	}
	if w := intTag(x, exif.PixelXDimension); w != nil {
		data.Width = w
	}
	if h := intTag(x, exif.PixelYDimension); h != nil {
		data.Height = h
	}
	if o := intTag(x, exif.Orientation); o != nil {
		data.Orientation = o
	}

	return data, nil
}

// Flags computes the data-deficiency flag list for an ingested EXIFData.
// Always returns a non-nil slice so it serialises as [] not null.
func (e *EXIFData) Flags() []string {
	flags := []string{}
	if e.CapturedAt == nil {
		flags = append(flags, FlagMissingDate)
	}
	if e.Latitude == nil || e.Longitude == nil {
		flags = append(flags, FlagMissingGPS)
	}
	return flags
}

// MatchesWhitelist returns true if the EXIF make+model matches any entry in the
// whitelist (case-insensitive). An empty model in the whitelist matches any model
// for that make.
func (e *EXIFData) MatchesWhitelist(whitelist []WhitelistEntry) bool {
	make_ := strings.ToLower(strings.TrimSpace(e.CameraMake))
	model := strings.ToLower(strings.TrimSpace(e.CameraModel))
	for _, w := range whitelist {
		wMake := strings.ToLower(strings.TrimSpace(w.Make))
		wModel := strings.ToLower(strings.TrimSpace(w.Model))
		if make_ == wMake && (wModel == "" || model == wModel) {
			return true
		}
	}
	return false
}

// WhitelistEntry mirrors config.CameraEntry but is package-local to avoid import cycles.
type WhitelistEntry struct {
	Make  string
	Model string
}

// helpers

func stringTag(x *exif.Exif, field exif.FieldName) string {
	tag, err := x.Get(field)
	if err != nil {
		return ""
	}
	s, err := tag.StringVal()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(s)
}

func intTag(x *exif.Exif, field exif.FieldName) *int {
	tag, err := x.Get(field)
	if err != nil {
		return nil
	}
	v, err := tag.Int(0)
	if err != nil {
		return nil
	}
	return &v
}

func rationalTag(x *exif.Exif, field exif.FieldName) *float64 {
	tag, err := x.Get(field)
	if err != nil {
		return nil
	}
	num, den, err := tag.Rat2(0)
	if err != nil || den == 0 {
		return nil
	}
	v := float64(num) / float64(den)
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return nil
	}
	return &v
}
