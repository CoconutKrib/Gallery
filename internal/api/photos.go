package api

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"image/jpeg"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/halleck/gallery/internal/db"
	"github.com/halleck/gallery/internal/heif"
)

func (h *Handlers) handlePhotos(w http.ResponseWriter, r *http.Request) {
	includeMatchMeta := false
	if v := r.URL.Query().Get("include_match_meta"); v != "" {
		parsed, err := parseBoolParam(v)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid include_match_meta")
			return
		}
		includeMatchMeta = parsed
	}

	f, err := parsePhotoFilterFromQuery(r.URL.Query())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	photos, total, err := db.ListPhotosFiltered(h.db, f)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}

	libraryMetaBySHA, peopleBySHA, err := getSearchMetadata(h.db, photos, f.IncludeUnverifiedFaces)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}

	var items []map[string]any
	for _, p := range photos {
		item := photoSummaryWithMeta(p, libraryMetaBySHA[p.SHA256], peopleBySHA[p.SHA256])
		if includeMatchMeta {
			item["match_reasons"] = buildMatchReasons(f, p, libraryMetaBySHA[p.SHA256], peopleBySHA[p.SHA256])
		}
		items = append(items, item)
	}
	if items == nil {
		items = []map[string]any{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"total":    total,
		"page":     f.Page,
		"per_page": f.PerPage,
		"items":    items,
	})
}

// handleSearchFacets returns global facet counts used to power search controls.
// For now this returns global counts (not query-contextual), with optional in_library tri-state scope.
func (h *Handlers) handleSearchFacets(w http.ResponseWriter, r *http.Request) {
	f, err := parsePhotoFilterFromQuery(r.URL.Query())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	res, err := h.searchFacetsForFilter(f)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (h *Handlers) searchFacetsForFilter(base db.PhotoFilter) (map[string]any, error) {
	// Facet counts are query-contextual, but each facet excludes its own dimension.
	sourceFilter := base
	sourceFilter.Source = ""
	sources, err := facetSourceCounts(h.db, sourceFilter)
	if err != nil {
		return nil, err
	}

	makeFilter := base
	makeFilter.CameraMake = ""
	cameraMake, err := facetCameraMakeCounts(h.db, makeFilter)
	if err != nil {
		return nil, err
	}

	modelFilter := base
	modelFilter.CameraModel = ""
	cameraModel, err := facetCameraModelCounts(h.db, modelFilter)
	if err != nil {
		return nil, err
	}

	tagFilter := base
	tagFilter.Tag = ""
	tags, err := facetTagCounts(h.db, tagFilter)
	if err != nil {
		return nil, err
	}

	personFilter := base
	personFilter.PersonID = nil
	people, err := facetPeopleCounts(h.db, personFilter)
	if err != nil {
		return nil, err
	}

	flagFilter := base
	flagFilter.Flag = ""
	flags, err := facetFlagCounts(h.db, flagFilter)
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"scope": map[string]any{
			"in_library": base.InLibrary,
		},
		"sources":      sources,
		"camera_make":  cameraMake,
		"camera_model": cameraModel,
		"tags":         tags,
		"people":       people,
		"flags":        flags,
	}, nil
}

func parsePhotoFilterFromQuery(q url.Values) (db.PhotoFilter, error) {
	f := db.PhotoFilter{
		CameraMake:  q.Get("make"),
		CameraModel: q.Get("model"),
		Flag:        q.Get("flag"),
		OrderBy:     q.Get("order"),
		Keyword:     q.Get("q"),
		Tag:         q.Get("tag"),
	}

	if v := q.Get("source"); v != "" && v != "any" {
		if v != "scan" && v != "dropzone" {
			return f, fmt.Errorf("invalid source")
		}
		f.Source = v
	}
	if v := q.Get("face_source"); v != "" && v != "any" {
		if v != "manual" && v != "auto" {
			return f, fmt.Errorf("invalid face_source")
		}
		f.FaceSource = v
	}
	if v := q.Get("in_library"); v != "" {
		parsed, err := parseTriStateParam(v)
		if err != nil {
			return f, fmt.Errorf("invalid in_library")
		}
		f.InLibrary = parsed
	}
	if v := q.Get("has_date_override"); v != "" {
		parsed, err := parseTriStateParam(v)
		if err != nil {
			return f, fmt.Errorf("invalid has_date_override")
		}
		f.HasDateOverride = parsed
	}
	if v := q.Get("true_date_unknown"); v != "" {
		parsed, err := parseTriStateParam(v)
		if err != nil {
			return f, fmt.Errorf("invalid true_date_unknown")
		}
		f.TrueDateUnknown = parsed
	}
	if v := q.Get("has_people"); v != "" {
		parsed, err := parseTriStateParam(v)
		if err != nil {
			return f, fmt.Errorf("invalid has_people")
		}
		f.HasPeople = parsed
	}
	if v := q.Get("face_verified"); v != "" {
		parsed, err := parseTriStateParam(v)
		if err != nil {
			return f, fmt.Errorf("invalid face_verified")
		}
		f.FaceVerified = parsed
	}
	if v := q.Get("include_unverified_faces"); v != "" {
		parsed, err := parseBoolParam(v)
		if err != nil {
			return f, fmt.Errorf("invalid include_unverified_faces")
		}
		f.IncludeUnverifiedFaces = parsed
	}

	if v := q.Get("library_id"); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err == nil {
			f.LibraryPathID = &id
		}
	}
	if v := q.Get("from"); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			f.From = &t
		}
	}
	if v := q.Get("to"); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			end := t.Add(24*time.Hour - time.Nanosecond)
			f.To = &end
		}
	}
	if v := q.Get("has_gps"); v == "true" {
		t := true
		f.HasGPS = &t
	} else if v == "false" {
		t := false
		f.HasGPS = &t
	}
	if v := q.Get("page"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			f.Page = n
		}
	}
	if v := q.Get("per_page"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			f.PerPage = n
		}
	}
	if v := q.Get("event_id"); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return f, fmt.Errorf("invalid event_id")
		}
		f.EventID = &id
	}
	if v := q.Get("person_id"); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return f, fmt.Errorf("invalid person_id")
		}
		f.PersonID = &id
	}

	hasFaceFilter := f.PersonID != nil || f.HasPeople != nil || f.FaceSource != ""
	if hasFaceFilter && f.FaceVerified == nil && !f.IncludeUnverifiedFaces {
		t := true
		f.FaceVerified = &t
	}

	return f, nil
}

func listAllPhotosFiltered(database *sql.DB, f db.PhotoFilter) ([]db.Photo, error) {
	all := []db.Photo{}
	if f.PerPage <= 0 {
		f.PerPage = 500
	}
	if f.Page <= 0 {
		f.Page = 1
	}
	for {
		items, total, err := db.ListPhotosFiltered(database, f)
		if err != nil {
			return nil, err
		}
		all = append(all, items...)
		if len(all) >= total || len(items) == 0 {
			break
		}
		f.Page++
	}
	return all, nil
}

func countRows(database *sql.DB, query string, args ...any) ([]map[string]any, error) {
	rows, err := database.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var value string
		var count int
		if err := rows.Scan(&value, &count); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"value": value, "count": count})
	}
	return out, rows.Err()
}

func facetSourceCounts(database *sql.DB, f db.PhotoFilter) ([]map[string]any, error) {
	photos, err := listAllPhotosFiltered(database, f)
	if err != nil {
		return nil, err
	}
	counts := map[string]int{}
	for _, p := range photos {
		if p.Source == "" {
			continue
		}
		counts[p.Source]++
	}
	out := make([]map[string]any, 0, len(counts))
	for k, v := range counts {
		out = append(out, map[string]any{"value": k, "count": v})
	}
	return out, nil
}

func facetCameraMakeCounts(database *sql.DB, f db.PhotoFilter) ([]map[string]any, error) {
	photos, err := listAllPhotosFiltered(database, f)
	if err != nil {
		return nil, err
	}
	counts := map[string]int{}
	for _, p := range photos {
		if p.CameraMake == "" {
			continue
		}
		counts[p.CameraMake]++
	}
	out := make([]map[string]any, 0, len(counts))
	for k, v := range counts {
		out = append(out, map[string]any{"value": k, "count": v})
	}
	return out, nil
}

func facetCameraModelCounts(database *sql.DB, f db.PhotoFilter) ([]map[string]any, error) {
	photos, err := listAllPhotosFiltered(database, f)
	if err != nil {
		return nil, err
	}
	counts := map[string]int{}
	for _, p := range photos {
		if p.CameraModel == "" {
			continue
		}
		counts[p.CameraModel]++
	}
	out := make([]map[string]any, 0, len(counts))
	for k, v := range counts {
		out = append(out, map[string]any{"value": k, "count": v})
	}
	return out, nil
}

func facetTagCounts(database *sql.DB, f db.PhotoFilter) ([]map[string]any, error) {
	photos, err := listAllPhotosFiltered(database, f)
	if err != nil {
		return nil, err
	}
	meta, _, err := getSearchMetadata(database, photos, true)
	if err != nil {
		return nil, err
	}
	counts := map[string]int{}
	for _, p := range photos {
		m := meta[p.SHA256]
		if m == nil {
			continue
		}
		for _, tag := range m.Tags {
			counts[tag]++
		}
	}
	out := make([]map[string]any, 0, len(counts))
	for k, v := range counts {
		out = append(out, map[string]any{"value": k, "count": v})
	}
	return out, nil
}

func facetPeopleCounts(database *sql.DB, f db.PhotoFilter) ([]map[string]any, error) {
	photos, err := listAllPhotosFiltered(database, f)
	if err != nil {
		return nil, err
	}
	_, people, err := getSearchMetadata(database, photos, f.IncludeUnverifiedFaces)
	if err != nil {
		return nil, err
	}
	type personFacet struct {
		id    int64
		name  string
		count int
	}
	counts := map[int64]*personFacet{}
	for _, list := range people {
		for _, pe := range list {
			pf := counts[pe.PersonID]
			if pf == nil {
				pf = &personFacet{id: pe.PersonID, name: pe.PersonName}
				counts[pe.PersonID] = pf
			}
			pf.count += pe.FaceCount
		}
	}
	out := make([]map[string]any, 0, len(counts))
	for _, v := range counts {
		out = append(out, map[string]any{"id": v.id, "name": v.name, "count": v.count})
	}
	return out, nil
}

func facetFlagCounts(database *sql.DB, f db.PhotoFilter) ([]map[string]any, error) {
	photos, err := listAllPhotosFiltered(database, f)
	if err != nil {
		return nil, err
	}
	counts := map[string]int{}
	for _, p := range photos {
		for _, fl := range p.Flags {
			counts[fl]++
		}
	}
	out := make([]map[string]any, 0, len(counts))
	for k, v := range counts {
		out = append(out, map[string]any{"value": k, "count": v})
	}
	return out, nil
}

func tagFacetRows(database *sql.DB, photoWhere string) ([]map[string]any, error) {
	query := `SELECT lc.tags
		FROM library_copies lc
		JOIN photos p ON p.sha256 = lc.photo_sha256` + photoWhere
	rows, err := database.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	counts := map[string]int{}
	for rows.Next() {
		var tagsJSON string
		if err := rows.Scan(&tagsJSON); err != nil {
			return nil, err
		}
		if tagsJSON == "" || tagsJSON == "null" {
			continue
		}
		var tags []string
		if err := json.Unmarshal([]byte(tagsJSON), &tags); err != nil {
			continue
		}
		for _, t := range tags {
			counts[t]++
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(counts))
	for k, v := range counts {
		out = append(out, map[string]any{"value": k, "count": v})
	}
	return out, nil
}

func peopleFacetRows(database *sql.DB, photoWhere string) ([]map[string]any, error) {
	query := `SELECT pe.id, pe.name, COUNT(*)
		FROM faces f
		JOIN photos p ON p.id = f.photo_id
		JOIN people pe ON pe.id = f.person_id` + photoWhere + `
		GROUP BY pe.id, pe.name
		ORDER BY COUNT(*) DESC`
	rows, err := database.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id int64
		var name string
		var count int
		if err := rows.Scan(&id, &name, &count); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"id": id, "name": name, "count": count})
	}
	return out, rows.Err()
}

func flagsFacetRows(database *sql.DB, photoWhere string) ([]map[string]any, error) {
	rows, err := database.Query("SELECT p.flags FROM photos p" + photoWhere)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	counts := map[string]int{}
	for rows.Next() {
		var flagsJSON string
		if err := rows.Scan(&flagsJSON); err != nil {
			return nil, err
		}
		if flagsJSON == "" || flagsJSON == "null" || flagsJSON == "[]" {
			continue
		}
		var flags []string
		if err := json.Unmarshal([]byte(flagsJSON), &flags); err != nil {
			continue
		}
		for _, fl := range flags {
			counts[fl]++
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(counts))
	for k, v := range counts {
		out = append(out, map[string]any{"value": k, "count": v})
	}
	return out, nil
}

func (h *Handlers) handlePhotoDetail(w http.ResponseWriter, r *http.Request) {
	sha256 := r.PathValue("sha256")
	if !isValidSHA256(sha256) {
		writeError(w, http.StatusBadRequest, "invalid sha256")
		return
	}
	photo, err := db.GetPhotoBySHA256(h.db, sha256)
	if err == sql.ErrNoRows {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}

	dupes, _ := db.GetDuplicatesBySHA256(h.db, sha256)
	var dupeList []map[string]any
	for _, d := range dupes {
		dupeList = append(dupeList, map[string]any{
			"filepath":        d.Filepath,
			"library_path_id": d.LibraryPathID,
			"discovered_at":   d.DiscoveredAt,
		})
	}
	if dupeList == nil {
		dupeList = []map[string]any{}
	}

	eventID, _ := db.GetEventForPhoto(h.db, photo.ID)

	out := photoDetail(*photo)
	out["duplicates"] = dupeList
	out["event_id"] = eventID
	writeJSON(w, http.StatusOK, out)
}

func (h *Handlers) handlePhotoImage(w http.ResponseWriter, r *http.Request) {
	sha256 := r.PathValue("sha256")
	if !isValidSHA256(sha256) {
		writeError(w, http.StatusBadRequest, "invalid sha256")
		return
	}
	photo, err := db.GetPhotoBySHA256(h.db, sha256)
	if err == sql.ErrNoRows {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}

	// Security: ensure the filepath is within a configured library root.
	if !pathIsWithinRoots(photo.Filepath, h.libraryRoots()) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}

	wantOriginal := r.URL.Query().Get("original") == "1"
	wantJPEG := r.URL.Query().Get("format") == "jpeg"

	// On-the-fly HEIC→JPEG transcode for browsers that can't render HEIC.
	// Reuses the same heif.Decode → jpeg.Encode pipeline used for thumbnails.
	if photo.Format == "heic" && wantJPEG {
		h.serveTranscodedJPEG(w, r, photo.Filepath, sha256)
		return
	}

	// For HEIC originals, browsers (Chrome, Firefox) cannot render them
	// natively. Serve the pre-generated JPEG thumbnail instead, unless the
	// caller explicitly requests the original with ?original=1.
	if photo.Format == "heic" && !wantOriginal {
		if len(sha256) < 2 {
			writeError(w, http.StatusBadRequest, "invalid sha256")
			return
		}
		thumbPath := filepath.Join(h.cfg.CacheDir, sha256[:2], sha256+".jpg")
		absCache, _ := filepath.Abs(h.cfg.CacheDir)
		absThumb, _ := filepath.Abs(thumbPath)
		if !strings.HasPrefix(absThumb, absCache+string(filepath.Separator)) {
			writeError(w, http.StatusForbidden, "forbidden")
			return
		}
		http.ServeFile(w, r, thumbPath)
		return
	}

	http.ServeFile(w, r, photo.Filepath)
}

// serveTranscodedJPEG decodes a HEIC file and re-encodes it as JPEG on the fly.
func (h *Handlers) serveTranscodedJPEG(w http.ResponseWriter, r *http.Request, sourcePath, sha256 string) {
	f, err := os.Open(sourcePath)
	if err != nil {
		slog.Warn("transcode: open failed", "path", sourcePath, "err", err)
		writeError(w, http.StatusInternalServerError, "transcode error")
		return
	}
	defer f.Close()

	img, err := heif.Decode(f)
	if err != nil {
		slog.Warn("transcode: heif decode failed", "sha256", sha256, "err", err)
		writeError(w, http.StatusInternalServerError, "transcode error")
		return
	}

	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	if err := jpeg.Encode(w, img, &jpeg.Options{Quality: 85}); err != nil {
		slog.Warn("transcode: jpeg encode failed", "sha256", sha256, "err", err)
	}
}

func (h *Handlers) handlePhotoThumbnail(w http.ResponseWriter, r *http.Request) {
	sha256 := r.PathValue("sha256")
	if !isValidSHA256(sha256) {
		writeError(w, http.StatusBadRequest, "invalid sha256")
		return
	}
	if len(sha256) < 2 {
		writeError(w, http.StatusBadRequest, "invalid sha256")
		return
	}

	// Reconstruct thumbnail path from hash + configured cache dir.
	thumbPath := filepath.Join(h.cfg.CacheDir, sha256[:2], sha256+".jpg")
	absCache, _ := filepath.Abs(h.cfg.CacheDir)
	absThumb, _ := filepath.Abs(thumbPath)

	// Security: ensure thumbnail path is within cache dir.
	if !strings.HasPrefix(absThumb, absCache+string(filepath.Separator)) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	http.ServeFile(w, r, thumbPath)
}

type searchLibraryMeta struct {
	LibraryCopyID    int64
	Tags             []string
	EventID          *int64
	Title            *string
	Description      *string
	OverrideDate     *string
	TrueDateUnknown  bool
	HasTrueDateValue bool
}

type searchPersonSummary struct {
	PersonID   int64  `json:"person_id"`
	PersonName string `json:"person_name"`
	Verified   bool   `json:"verified"`
	Source     string `json:"source"`
	FaceCount  int    `json:"face_count"`
}

func getSearchMetadata(database *sql.DB, photos []db.Photo, includeUnverified bool) (map[string]*searchLibraryMeta, map[string][]searchPersonSummary, error) {
	if len(photos) == 0 {
		return map[string]*searchLibraryMeta{}, map[string][]searchPersonSummary{}, nil
	}
	shas := make([]string, 0, len(photos))
	for _, p := range photos {
		shas = append(shas, p.SHA256)
	}

	libraryMeta, err := getSearchLibraryMeta(database, shas)
	if err != nil {
		return nil, nil, err
	}
	people, err := getSearchPeopleSummary(database, shas, includeUnverified)
	if err != nil {
		return nil, nil, err
	}
	return libraryMeta, people, nil
}

func getSearchLibraryMeta(database *sql.DB, shas []string) (map[string]*searchLibraryMeta, error) {
	query, args := inQuery(
		`SELECT photo_sha256, id, tags, event_id, title, description, override_date, true_date_unknown
		 FROM library_copies WHERE photo_sha256 IN (%s)`,
		shas,
	)
	rows, err := database.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]*searchLibraryMeta{}
	for rows.Next() {
		var sha string
		var id int64
		var tagsJSON string
		var eventID sql.NullInt64
		var title sql.NullString
		var description sql.NullString
		var overrideDate sql.NullString
		var trueDateUnknown int

		if err := rows.Scan(&sha, &id, &tagsJSON, &eventID, &title, &description, &overrideDate, &trueDateUnknown); err != nil {
			return nil, err
		}

		tags := []string{}
		if tagsJSON != "" && tagsJSON != "null" {
			_ = json.Unmarshal([]byte(tagsJSON), &tags)
			if tags == nil {
				tags = []string{}
			}
		}

		m := &searchLibraryMeta{
			LibraryCopyID:    id,
			Tags:             tags,
			TrueDateUnknown:  trueDateUnknown != 0,
			HasTrueDateValue: true,
		}
		if eventID.Valid {
			m.EventID = &eventID.Int64
		}
		if title.Valid {
			m.Title = &title.String
		}
		if description.Valid {
			m.Description = &description.String
		}
		if overrideDate.Valid {
			m.OverrideDate = &overrideDate.String
		}
		out[sha] = m
	}
	return out, rows.Err()
}

func getSearchPeopleSummary(database *sql.DB, shas []string, includeUnverified bool) (map[string][]searchPersonSummary, error) {
	base := `SELECT ph.sha256, f.person_id, COALESCE(pe.name, ''), f.verified, f.source, COUNT(*)
		FROM faces f
		JOIN photos ph ON ph.id = f.photo_id
		LEFT JOIN people pe ON pe.id = f.person_id
		WHERE ph.sha256 IN (%s) AND f.person_id IS NOT NULL`
	query, args := inQuery(base, shas)
	if !includeUnverified {
		query += " AND f.verified = 1"
	}
	query += ` GROUP BY ph.sha256, f.person_id, pe.name, f.verified, f.source
		ORDER BY ph.sha256, COUNT(*) DESC`

	rows, err := database.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string][]searchPersonSummary{}
	for rows.Next() {
		var sha string
		var personID int64
		var personName string
		var verified int
		var source string
		var count int

		if err := rows.Scan(&sha, &personID, &personName, &verified, &source, &count); err != nil {
			return nil, err
		}
		if len(out[sha]) >= 5 {
			continue
		}
		out[sha] = append(out[sha], searchPersonSummary{
			PersonID:   personID,
			PersonName: personName,
			Verified:   verified != 0,
			Source:     source,
			FaceCount:  count,
		})
	}
	return out, rows.Err()
}

func inQuery(base string, values []string) (string, []any) {
	args := make([]any, 0, len(values))
	for _, v := range values {
		args = append(args, v)
	}
	placeholders := strings.TrimRight(strings.Repeat("?,", len(values)), ",")
	return fmt.Sprintf(base, placeholders), args
}

func parseBoolParam(v string) (bool, error) {
	switch v {
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, fmt.Errorf("invalid bool")
	}
}

func parseTriStateParam(v string) (*bool, error) {
	switch v {
	case "", "any":
		return nil, nil
	case "true":
		t := true
		return &t, nil
	case "false":
		f := false
		return &f, nil
	default:
		return nil, fmt.Errorf("invalid tri-state")
	}
}

func buildMatchReasons(f db.PhotoFilter, p db.Photo, libraryMeta *searchLibraryMeta, people []searchPersonSummary) []string {
	reasons := []string{}
	if f.Keyword != "" {
		reasons = append(reasons, "keyword:"+f.Keyword)
	}
	if f.CameraMake != "" {
		reasons = append(reasons, "camera_make:"+f.CameraMake)
	}
	if f.CameraModel != "" {
		reasons = append(reasons, "camera_model:"+f.CameraModel)
	}
	if f.Source != "" {
		reasons = append(reasons, "source:"+f.Source)
	}
	if f.Flag != "" {
		reasons = append(reasons, "flag:"+f.Flag)
	}
	if f.Tag != "" {
		reasons = append(reasons, "tag:"+f.Tag)
	}
	if f.EventID != nil {
		reasons = append(reasons, "event:"+strconv.FormatInt(*f.EventID, 10))
	}
	if f.PersonID != nil {
		reasons = append(reasons, "person:"+strconv.FormatInt(*f.PersonID, 10))
	}
	if f.HasDateOverride != nil && *f.HasDateOverride {
		reasons = append(reasons, "has_date_override:true")
	}
	if f.TrueDateUnknown != nil && *f.TrueDateUnknown {
		reasons = append(reasons, "true_date_unknown:true")
	}

	if len(reasons) == 0 {
		reasons = append(reasons, "default")
	}

	if libraryMeta != nil && len(libraryMeta.Tags) > 0 && f.Tag == "" {
		reasons = append(reasons, "tags_present")
	}
	if len(people) > 0 && f.PersonID == nil {
		reasons = append(reasons, "people_present")
	}
	_ = p
	return reasons
}

// photoSummary returns a condensed map suitable for grid/list views.
func photoSummary(p db.Photo) map[string]any {
	return photoSummaryWithMeta(p, nil, nil)
}

func photoSummaryWithMeta(p db.Photo, libraryMeta *searchLibraryMeta, people []searchPersonSummary) map[string]any {
	out := map[string]any{
		"sha256":            p.SHA256,
		"filename":          p.Filename,
		"captured_at":       p.CapturedAt,
		"camera_make":       p.CameraMake,
		"camera_model":      p.CameraModel,
		"width":             p.Width,
		"height":            p.Height,
		"flags":             p.Flags,
		"source":            p.Source,
		"in_library":        false,
		"library_copy_id":   nil,
		"tags":              []string{},
		"event_id":          nil,
		"title":             nil,
		"description":       nil,
		"override_date":     nil,
		"true_date_unknown": nil,
		"people":            people,
		"thumbnail_url":     "/api/photos/" + p.SHA256 + "/thumbnail",
		"image_url":         "/api/photos/" + p.SHA256 + "/image",
		"format":            p.Format,
	}
	if out["people"] == nil {
		out["people"] = []searchPersonSummary{}
	}
	if libraryMeta != nil {
		out["in_library"] = true
		out["library_copy_id"] = libraryMeta.LibraryCopyID
		out["tags"] = libraryMeta.Tags
		out["event_id"] = libraryMeta.EventID
		out["title"] = libraryMeta.Title
		out["description"] = libraryMeta.Description
		out["override_date"] = libraryMeta.OverrideDate
		out["true_date_unknown"] = libraryMeta.TrueDateUnknown
	}
	return out
}

// photoDetail returns a full map for the photo detail view.
func photoDetail(p db.Photo) map[string]any {
	return map[string]any{
		"sha256":          p.SHA256,
		"filename":        p.Filename,
		"filepath":        p.Filepath,
		"library_path_id": p.LibraryPathID,
		"captured_at":     p.CapturedAt,
		"latitude":        p.Latitude,
		"longitude":       p.Longitude,
		"altitude":        p.Altitude,
		"camera_make":     p.CameraMake,
		"camera_model":    p.CameraModel,
		"camera_serial":   p.CameraSerial,
		"lens_model":      p.LensModel,
		"iso":             p.ISO,
		"aperture":        p.Aperture,
		"shutter_speed":   p.ShutterSpeed,
		"focal_length":    p.FocalLength,
		"flash":           p.Flash,
		"width":           p.Width,
		"height":          p.Height,
		"orientation":     p.Orientation,
		"flags":           p.Flags,
		"ingested_at":     p.IngestedAt,
		"thumbnail_url":   "/api/photos/" + p.SHA256 + "/thumbnail",
		"image_url":       "/api/photos/" + p.SHA256 + "/image",
		"format":          p.Format,
	}
}

// isValidSHA256 checks that s looks like a hex sha256 (64 chars, 0-9a-f).
func isValidSHA256(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}
