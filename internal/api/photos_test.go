package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/halleck/gallery/internal/config"
	"github.com/halleck/gallery/internal/db"
)

func TestHandlePhotos_ValidationAndDefaults(t *testing.T) {
	database := openAPITestDB(t)
	libraryPathID := mustAPIUpsertLibraryPath(t, database, "/photos", "photos")

	captured1 := mustAPITime(t, "2024-07-01T10:00:00Z")
	captured2 := mustAPITime(t, "2024-07-02T10:00:00Z")
	photo1ID := mustAPIInsertPhoto(t, database, db.Photo{
		SHA256:        "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Filepath:      "/photos/one.jpg",
		Filename:      "one.jpg",
		LibraryPathID: libraryPathID,
		CapturedAt:    &captured1,
		Source:        "scan",
		Flags:         []string{},
	})
	photo2ID := mustAPIInsertPhoto(t, database, db.Photo{
		SHA256:        "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		Filepath:      "/photos/two.jpg",
		Filename:      "two.jpg",
		LibraryPathID: libraryPathID,
		CapturedAt:    &captured2,
		Source:        "dropzone",
		Flags:         []string{},
	})

	eventID, err := db.InsertEvent(database, &db.Event{Label: "Summer", PhotoCount: 1})
	if err != nil {
		t.Fatalf("insert event: %v", err)
	}
	if _, err := db.InsertLibraryCopy(database, &db.LibraryCopy{
		PhotoSHA256:     "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		RelativePath:    "2024/07/one.jpg",
		AbsolutePath:    "/internal/2024/07/one.jpg",
		Tags:            []string{"favorite"},
		TrueDateUnknown: false,
		EventID:         &eventID,
	}); err != nil {
		t.Fatalf("insert copy one: %v", err)
	}
	if _, err := db.InsertLibraryCopy(database, &db.LibraryCopy{
		PhotoSHA256:     "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		RelativePath:    "2024/07/two.jpg",
		AbsolutePath:    "/internal/2024/07/two.jpg",
		Tags:            []string{"people"},
		TrueDateUnknown: true,
	}); err != nil {
		t.Fatalf("insert copy two: %v", err)
	}

	aliceID, err := db.InsertPerson(database, "Alice", nil)
	if err != nil {
		t.Fatalf("insert person alice: %v", err)
	}
	bobID, err := db.InsertPerson(database, "Bob", nil)
	if err != nil {
		t.Fatalf("insert person bob: %v", err)
	}
	if _, err := db.InsertFace(database, db.Face{PhotoID: photo1ID, PersonID: &aliceID, Source: "manual", Verified: true}); err != nil {
		t.Fatalf("insert face alice: %v", err)
	}
	if _, err := db.InsertFace(database, db.Face{PhotoID: photo2ID, PersonID: &bobID, Source: "auto", Verified: false}); err != nil {
		t.Fatalf("insert face bob: %v", err)
	}

	h := &Handlers{db: database, cfg: &config.Config{CacheDir: ".cache"}}

	t.Run("invalid enum returns 400", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/api/photos?source=bad", nil)
		w := httptest.NewRecorder()

		h.handlePhotos(w, r)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("status mismatch: got %d want %d", w.Code, http.StatusBadRequest)
		}
	})

	t.Run("person filter defaults to verified-only", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/api/photos?person_id="+itoa64(bobID), nil)
		w := httptest.NewRecorder()

		h.handlePhotos(w, r)

		if w.Code != http.StatusOK {
			t.Fatalf("status mismatch: got %d want %d", w.Code, http.StatusOK)
		}
		var payload struct {
			Total int `json:"total"`
		}
		if err := json.NewDecoder(w.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		if payload.Total != 0 {
			t.Fatalf("total mismatch: got %d want %d", payload.Total, 0)
		}
	})

	t.Run("include_unverified_faces allows unverified person matches", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/api/photos?person_id="+itoa64(bobID)+"&include_unverified_faces=true", nil)
		w := httptest.NewRecorder()

		h.handlePhotos(w, r)

		if w.Code != http.StatusOK {
			t.Fatalf("status mismatch: got %d want %d", w.Code, http.StatusOK)
		}
		var payload struct {
			Total int `json:"total"`
		}
		if err := json.NewDecoder(w.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		if payload.Total != 1 {
			t.Fatalf("total mismatch: got %d want %d", payload.Total, 1)
		}
	})

	t.Run("response includes unified metadata fields", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/api/photos?person_id="+itoa64(aliceID), nil)
		w := httptest.NewRecorder()

		h.handlePhotos(w, r)

		if w.Code != http.StatusOK {
			t.Fatalf("status mismatch: got %d want %d", w.Code, http.StatusOK)
		}
		var payload struct {
			Items []map[string]any `json:"items"`
		}
		if err := json.NewDecoder(w.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		if len(payload.Items) != 1 {
			t.Fatalf("items length mismatch: got %d want %d", len(payload.Items), 1)
		}
		item := payload.Items[0]
		for _, key := range []string{"source", "in_library", "library_copy_id", "tags", "event_id", "title", "description", "override_date", "true_date_unknown", "people"} {
			if _, ok := item[key]; !ok {
				t.Fatalf("missing response key: %s", key)
			}
		}
	})

	t.Run("include_match_meta returns match_reasons", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/api/photos?tag=favorite&include_match_meta=true", nil)
		w := httptest.NewRecorder()

		h.handlePhotos(w, r)

		if w.Code != http.StatusOK {
			t.Fatalf("status mismatch: got %d want %d", w.Code, http.StatusOK)
		}
		var payload struct {
			Items []map[string]any `json:"items"`
		}
		if err := json.NewDecoder(w.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		if len(payload.Items) == 0 {
			t.Fatalf("expected at least one item")
		}
		if _, ok := payload.Items[0]["match_reasons"]; !ok {
			t.Fatalf("missing match_reasons in response")
		}
	})

	t.Run("search facets endpoint returns facet groups", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/api/search/facets?in_library=true&source=scan", nil)
		w := httptest.NewRecorder()

		h.handleSearchFacets(w, r)

		if w.Code != http.StatusOK {
			t.Fatalf("status mismatch: got %d want %d", w.Code, http.StatusOK)
		}
		var payload map[string]any
		if err := json.NewDecoder(w.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		for _, key := range []string{"scope", "sources", "camera_make", "camera_model", "tags", "people", "flags"} {
			if _, ok := payload[key]; !ok {
				t.Fatalf("missing facet key: %s", key)
			}
		}

		sourcesRaw, ok := payload["sources"].([]any)
		if !ok {
			t.Fatalf("sources type mismatch")
		}
		seen := map[string]bool{}
		for _, row := range sourcesRaw {
			m, ok := row.(map[string]any)
			if !ok {
				continue
			}
			if v, ok := m["value"].(string); ok {
				seen[v] = true
			}
		}
		// Source facet excludes its own source filter, so both sources remain visible.
		if !seen["scan"] || !seen["dropzone"] {
			t.Fatalf("expected both scan and dropzone in source facet, got %#v", seen)
		}
	})
}

func openAPITestDB(t *testing.T) *sql.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "api_test.db")
	database, err := db.Open(path)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database
}

func mustAPIUpsertLibraryPath(t *testing.T, database *sql.DB, path, label string) int64 {
	t.Helper()
	id, err := db.UpsertLibraryPath(database, path, label)
	if err != nil {
		t.Fatalf("upsert library path: %v", err)
	}
	return id
}

func mustAPIInsertPhoto(t *testing.T, database *sql.DB, p db.Photo) int64 {
	t.Helper()
	id, err := db.InsertPhoto(database, &p)
	if err != nil {
		t.Fatalf("insert photo: %v", err)
	}
	return id
}

func mustAPITime(t *testing.T, value string) time.Time {
	t.Helper()
	ts, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatalf("parse time %q: %v", value, err)
	}
	return ts
}

func itoa64(v int64) string {
	return strconv.FormatInt(v, 10)
}
