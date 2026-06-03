package db

import (
	"database/sql"
	"errors"
	"path/filepath"
	"sort"
	"testing"
	"time"
)

func TestUpdateStagingEntry_PartialAndStableFields(t *testing.T) {
	database := openTestDB(t)
	libraryPathID := mustLibraryPathID(t, database, "/photos", "photos")
	mustInsertPhoto(t, database, photoFixture{
		sha256:        "sha-staging-1",
		filepath:      "/photos/staging.jpg",
		filename:      "staging.jpg",
		libraryPathID: libraryPathID,
		source:        "scan",
		capturedAt:    mustTime(t, "2024-01-01T00:00:00Z"),
	})

	entry, err := InsertStagingEntry(database, "sha-staging-1")
	if err != nil {
		t.Fatalf("insert staging entry: %v", err)
	}

	tags := []string{"family", "keepers"}
	trueDateUnknown := true
	if err := UpdateStagingEntry(database, entry.ID, StagingAnnotationUpdate{
		Tags:            &tags,
		TrueDateUnknown: &trueDateUnknown,
	}); err != nil {
		t.Fatalf("update staging tags/date flag: %v", err)
	}

	title := "Annotated Title"
	if err := UpdateStagingEntry(database, entry.ID, StagingAnnotationUpdate{Title: &title}); err != nil {
		t.Fatalf("update staging title: %v", err)
	}

	updated, err := GetStagingEntryByID(database, entry.ID)
	if err != nil {
		t.Fatalf("get staging entry: %v", err)
	}
	if updated.Title == nil || *updated.Title != title {
		t.Fatalf("title mismatch: got %+v want %q", updated.Title, title)
	}
	if !updated.TrueDateUnknown {
		t.Fatalf("true_date_unknown was not persisted")
	}
	if len(updated.Tags) != 2 || updated.Tags[0] != "family" || updated.Tags[1] != "keepers" {
		t.Fatalf("tags mismatch: got %#v", updated.Tags)
	}
	if updated.Description != nil || updated.OverrideDate != nil || updated.EventID != nil {
		t.Fatalf("unexpected fields changed: description=%v override_date=%v event_id=%v", updated.Description, updated.OverrideDate, updated.EventID)
	}
}

func TestUpdateLibraryCopy_DoublePointerSemantics(t *testing.T) {
	database := openTestDB(t)
	libraryPathID := mustLibraryPathID(t, database, "/photos", "photos")
	mustInsertPhoto(t, database, photoFixture{
		sha256:        "sha-libcopy-1",
		filepath:      "/photos/libcopy.jpg",
		filename:      "libcopy.jpg",
		libraryPathID: libraryPathID,
		source:        "scan",
		capturedAt:    mustTime(t, "2024-02-01T00:00:00Z"),
	})

	copyID, err := InsertLibraryCopy(database, &LibraryCopy{
		PhotoSHA256:  "sha-libcopy-1",
		RelativePath: "2024/02/libcopy.jpg",
		AbsolutePath: "/internal/2024/02/libcopy.jpg",
		Tags:         []string{},
	})
	if err != nil {
		t.Fatalf("insert library copy: %v", err)
	}

	if err := UpdateLibraryCopy(database, copyID, LibraryCopyUpdate{}); err != nil {
		t.Fatalf("no-op update failed: %v", err)
	}

	title := "Beach Day"
	description := "Family picnic"
	overrideDate := "2024-02-03T10:00:00Z"
	eventID := int64(42)
	if err := UpdateLibraryCopy(database, copyID, LibraryCopyUpdate{
		Title:        toDoublePtrString(title),
		Description:  toDoublePtrString(description),
		OverrideDate: toDoublePtrString(overrideDate),
		EventID:      toDoublePtrInt64(eventID),
	}); err != nil {
		t.Fatalf("set non-null fields: %v", err)
	}

	afterSet, err := GetLibraryCopyByID(database, copyID)
	if err != nil {
		t.Fatalf("get after set: %v", err)
	}
	if afterSet.Title == nil || *afterSet.Title != title {
		t.Fatalf("title mismatch: got %+v want %q", afterSet.Title, title)
	}
	if afterSet.Description == nil || *afterSet.Description != description {
		t.Fatalf("description mismatch: got %+v want %q", afterSet.Description, description)
	}
	if afterSet.OverrideDate == nil || *afterSet.OverrideDate != overrideDate {
		t.Fatalf("override_date mismatch: got %+v want %q", afterSet.OverrideDate, overrideDate)
	}
	if afterSet.EventID == nil || *afterSet.EventID != eventID {
		t.Fatalf("event_id mismatch: got %+v want %d", afterSet.EventID, eventID)
	}

	var nilString *string
	var nilInt64 *int64
	if err := UpdateLibraryCopy(database, copyID, LibraryCopyUpdate{
		Title:        &nilString,
		Description:  &nilString,
		OverrideDate: &nilString,
		EventID:      &nilInt64,
	}); err != nil {
		t.Fatalf("clear nullable fields: %v", err)
	}

	afterClear, err := GetLibraryCopyByID(database, copyID)
	if err != nil {
		t.Fatalf("get after clear: %v", err)
	}
	if afterClear.Title != nil || afterClear.Description != nil || afterClear.OverrideDate != nil || afterClear.EventID != nil {
		t.Fatalf("expected nullable fields cleared, got title=%v description=%v override_date=%v event_id=%v", afterClear.Title, afterClear.Description, afterClear.OverrideDate, afterClear.EventID)
	}
}

func TestDeleteLibraryPhotoByID_CascadesOnlyTargetRows(t *testing.T) {
	database := openTestDB(t)
	libraryPathID := mustLibraryPathID(t, database, "/photos", "photos")

	targetPhotoID := mustInsertPhoto(t, database, photoFixture{
		sha256:        "sha-delete-target",
		filepath:      "/photos/target.jpg",
		filename:      "target.jpg",
		libraryPathID: libraryPathID,
		source:        "scan",
		capturedAt:    mustTime(t, "2024-03-01T00:00:00Z"),
	})
	controlPhotoID := mustInsertPhoto(t, database, photoFixture{
		sha256:        "sha-delete-control",
		filepath:      "/photos/control.jpg",
		filename:      "control.jpg",
		libraryPathID: libraryPathID,
		source:        "scan",
		capturedAt:    mustTime(t, "2024-03-02T00:00:00Z"),
	})

	targetStaging, err := InsertStagingEntry(database, "sha-delete-target")
	if err != nil {
		t.Fatalf("insert target staging: %v", err)
	}
	if err := SetStagingState(database, targetStaging.ID, StagingStateApproved); err != nil {
		t.Fatalf("approve target staging: %v", err)
	}
	controlStaging, err := InsertStagingEntry(database, "sha-delete-control")
	if err != nil {
		t.Fatalf("insert control staging: %v", err)
	}

	if err := InsertDuplicatePath(database, "sha-delete-target", "/photos/dupe-target.jpg", libraryPathID); err != nil {
		t.Fatalf("insert target duplicate: %v", err)
	}
	if err := InsertDuplicatePath(database, "sha-delete-control", "/photos/dupe-control.jpg", libraryPathID); err != nil {
		t.Fatalf("insert control duplicate: %v", err)
	}

	targetCopyID, err := InsertLibraryCopy(database, &LibraryCopy{
		PhotoSHA256:  "sha-delete-target",
		RelativePath: "2024/03/target.jpg",
		AbsolutePath: "/internal/2024/03/target.jpg",
		Tags:         []string{},
	})
	if err != nil {
		t.Fatalf("insert target copy: %v", err)
	}
	if _, err := InsertLibraryCopy(database, &LibraryCopy{
		PhotoSHA256:  "sha-delete-control",
		RelativePath: "2024/03/control.jpg",
		AbsolutePath: "/internal/2024/03/control.jpg",
		Tags:         []string{},
	}); err != nil {
		t.Fatalf("insert control copy: %v", err)
	}

	eventID, err := InsertEvent(database, &Event{Label: "Test Event", PhotoCount: 2})
	if err != nil {
		t.Fatalf("insert event: %v", err)
	}
	if err := InsertPhotoEvent(database, targetPhotoID, eventID); err != nil {
		t.Fatalf("insert target photo_event: %v", err)
	}
	if err := InsertPhotoEvent(database, controlPhotoID, eventID); err != nil {
		t.Fatalf("insert control photo_event: %v", err)
	}

	sha, absPath, err := DeleteLibraryPhotoByID(database, targetCopyID)
	if err != nil {
		t.Fatalf("delete library photo: %v", err)
	}
	if sha != "sha-delete-target" {
		t.Fatalf("deleted sha mismatch: got %q want %q", sha, "sha-delete-target")
	}
	if absPath != "/internal/2024/03/target.jpg" {
		t.Fatalf("deleted absPath mismatch: got %q", absPath)
	}

	if _, err := GetPhotoBySHA256(database, "sha-delete-target"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("target photo should be deleted, got err=%v", err)
	}
	if _, err := GetStagingEntryBySHA256(database, "sha-delete-target"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("target staging should be deleted, got err=%v", err)
	}
	targetDupes, err := GetDuplicatesBySHA256(database, "sha-delete-target")
	if err != nil {
		t.Fatalf("get target duplicates: %v", err)
	}
	if len(targetDupes) != 0 {
		t.Fatalf("target duplicate rows should be deleted, got %d", len(targetDupes))
	}
	if _, err := GetLibraryCopyBySHA256(database, "sha-delete-target"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("target library copy should be deleted, got err=%v", err)
	}
	if got := mustCount(t, database, `SELECT COUNT(*) FROM photo_events WHERE photo_id = ?`, targetPhotoID); got != 0 {
		t.Fatalf("target photo_events should be deleted, got %d", got)
	}

	if _, err := GetPhotoBySHA256(database, "sha-delete-control"); err != nil {
		t.Fatalf("control photo missing: %v", err)
	}
	if _, err := GetStagingEntryByID(database, controlStaging.ID); err != nil {
		t.Fatalf("control staging missing: %v", err)
	}
	controlDupes, err := GetDuplicatesBySHA256(database, "sha-delete-control")
	if err != nil {
		t.Fatalf("get control duplicates: %v", err)
	}
	if len(controlDupes) != 1 {
		t.Fatalf("control duplicate rows mismatch: got %d want %d", len(controlDupes), 1)
	}
	if got := mustCount(t, database, `SELECT COUNT(*) FROM photo_events WHERE photo_id = ?`, controlPhotoID); got != 1 {
		t.Fatalf("control photo_events should remain, got %d", got)
	}
}

func TestListLibraryCopiesFiltered_Combinations(t *testing.T) {
	database := openTestDB(t)
	libraryPathID := mustLibraryPathID(t, database, "/photos", "photos")
	eventID1, err := InsertEvent(database, &Event{Label: "Event One", PhotoCount: 1})
	if err != nil {
		t.Fatalf("insert event1: %v", err)
	}
	eventID2, err := InsertEvent(database, &Event{Label: "Event Two", PhotoCount: 1})
	if err != nil {
		t.Fatalf("insert event2: %v", err)
	}

	mustInsertPhoto(t, database, photoFixture{
		sha256:        "sha-filter-a",
		filepath:      "/photos/2024/beach.jpg",
		filename:      "beach.jpg",
		libraryPathID: libraryPathID,
		source:        "scan",
		capturedAt:    mustTime(t, "2024-04-01T00:00:00Z"),
	})
	mustInsertPhoto(t, database, photoFixture{
		sha256:        "sha-filter-b",
		filepath:      "/photos/2024/portrait.jpg",
		filename:      "portrait.jpg",
		libraryPathID: libraryPathID,
		source:        "dropzone",
		capturedAt:    mustTime(t, "2024-04-02T00:00:00Z"),
	})
	mustInsertPhoto(t, database, photoFixture{
		sha256:        "sha-filter-c",
		filepath:      "/photos/2024/birthday.jpg",
		filename:      "birthday.jpg",
		libraryPathID: libraryPathID,
		source:        "scan",
		capturedAt:    mustTime(t, "2024-04-03T00:00:00Z"),
	})

	overrideDate := "2024-04-05T10:00:00Z"
	titleA := "Summer"
	descA := "Family trip"
	titleC := "Birthday"
	descC := "Cake night"
	if _, err := InsertLibraryCopy(database, &LibraryCopy{
		PhotoSHA256:     "sha-filter-a",
		RelativePath:    "2024/04/beach.jpg",
		AbsolutePath:    "/internal/2024/04/beach.jpg",
		TrueDateUnknown: false,
		Tags:            []string{"family"},
		Title:           &titleA,
		Description:     &descA,
		OverrideDate:    nil,
		EventID:         nil,
	}); err != nil {
		t.Fatalf("insert copy a: %v", err)
	}
	if _, err := InsertLibraryCopy(database, &LibraryCopy{
		PhotoSHA256:     "sha-filter-b",
		RelativePath:    "2024/04/portrait.jpg",
		AbsolutePath:    "/internal/2024/04/portrait.jpg",
		TrueDateUnknown: true,
		Tags:            []string{"people", "favorite"},
		OverrideDate:    &overrideDate,
		EventID:         &eventID1,
	}); err != nil {
		t.Fatalf("insert copy b: %v", err)
	}
	if _, err := InsertLibraryCopy(database, &LibraryCopy{
		PhotoSHA256:     "sha-filter-c",
		RelativePath:    "2024/04/birthday.jpg",
		AbsolutePath:    "/internal/2024/04/birthday.jpg",
		TrueDateUnknown: false,
		Tags:            []string{"favorite"},
		Title:           &titleC,
		Description:     &descC,
		EventID:         &eventID2,
	}); err != nil {
		t.Fatalf("insert copy c: %v", err)
	}

	trueVal := true
	falseVal := false
	assertLibraryFilter(t, database, LibraryCopyFilter{Source: "dropzone"}, []string{"sha-filter-b"})
	assertLibraryFilter(t, database, LibraryCopyFilter{HasDateOverride: &trueVal}, []string{"sha-filter-b"})
	assertLibraryFilter(t, database, LibraryCopyFilter{HasDateOverride: &falseVal}, []string{"sha-filter-a", "sha-filter-c"})
	assertLibraryFilter(t, database, LibraryCopyFilter{TrueDateUnknown: &trueVal}, []string{"sha-filter-b"})
	assertLibraryFilter(t, database, LibraryCopyFilter{Tag: "favorite"}, []string{"sha-filter-b", "sha-filter-c"})
	assertLibraryFilter(t, database, LibraryCopyFilter{EventID: &eventID2}, []string{"sha-filter-c"})
	assertLibraryFilter(t, database, LibraryCopyFilter{Keyword: "birth"}, []string{"sha-filter-c"})
	assertLibraryFilter(t, database, LibraryCopyFilter{Source: "scan", Tag: "favorite"}, []string{"sha-filter-c"})
}

func TestListPhotosFiltered_CombinationsPaginationOrdering(t *testing.T) {
	database := openTestDB(t)
	libraryPathID := mustLibraryPathID(t, database, "/photos", "photos")

	lat := 51.5
	lon := -0.13
	mustInsertPhoto(t, database, photoFixture{
		sha256:        "sha-photos-1",
		filepath:      "/photos/events/party-1.jpg",
		filename:      "party-1.jpg",
		libraryPathID: libraryPathID,
		cameraMake:    "Canon",
		cameraModel:   "EOS 700D",
		flags:         []string{"soft_focus"},
		latitude:      &lat,
		longitude:     &lon,
		capturedAt:    mustTime(t, "2024-05-01T10:00:00Z"),
	})
	mustInsertPhoto(t, database, photoFixture{
		sha256:        "sha-photos-2",
		filepath:      "/photos/travel/phone.jpg",
		filename:      "phone.jpg",
		libraryPathID: libraryPathID,
		cameraMake:    "Apple",
		cameraModel:   "iPhone SE",
		flags:         []string{"missing_gps"},
		capturedAt:    mustTime(t, "2024-05-02T10:00:00Z"),
	})
	mustInsertPhoto(t, database, photoFixture{
		sha256:        "sha-photos-3",
		filepath:      "/photos/events/party-2.jpg",
		filename:      "party-2.jpg",
		libraryPathID: libraryPathID,
		cameraMake:    "Canon",
		cameraModel:   "EOS R6",
		flags:         []string{},
		latitude:      &lat,
		longitude:     &lon,
		capturedAt:    mustTime(t, "2024-05-03T10:00:00Z"),
	})

	hasGPS := true
	noGPS := false
	assertPhotoFilter(t, database, PhotoFilter{CameraMake: "Canon"}, []string{"sha-photos-1", "sha-photos-3"})
	assertPhotoFilter(t, database, PhotoFilter{CameraModel: "iPhone"}, []string{"sha-photos-2"})
	assertPhotoFilter(t, database, PhotoFilter{HasGPS: &hasGPS}, []string{"sha-photos-1", "sha-photos-3"})
	assertPhotoFilter(t, database, PhotoFilter{HasGPS: &noGPS}, []string{"sha-photos-2"})
	assertPhotoFilter(t, database, PhotoFilter{Flag: "missing_gps"}, []string{"sha-photos-2"})
	assertPhotoFilter(t, database, PhotoFilter{Keyword: "party"}, []string{"sha-photos-1", "sha-photos-3"})

	photosDesc, totalDesc, err := ListPhotosFiltered(database, PhotoFilter{Page: 1, PerPage: 2, OrderBy: "captured_at_desc"})
	if err != nil {
		t.Fatalf("list photos desc: %v", err)
	}
	if totalDesc != 3 {
		t.Fatalf("total mismatch: got %d want %d", totalDesc, 3)
	}
	if len(photosDesc) != 2 {
		t.Fatalf("page length mismatch: got %d want %d", len(photosDesc), 2)
	}
	if photosDesc[0].SHA256 != "sha-photos-3" || photosDesc[1].SHA256 != "sha-photos-2" {
		t.Fatalf("captured_at_desc order mismatch: got [%s, %s]", photosDesc[0].SHA256, photosDesc[1].SHA256)
	}

	photosFilename, _, err := ListPhotosFiltered(database, PhotoFilter{Page: 1, PerPage: 3, OrderBy: "filename"})
	if err != nil {
		t.Fatalf("list photos filename order: %v", err)
	}
	if len(photosFilename) != 3 {
		t.Fatalf("filename order length mismatch: got %d want %d", len(photosFilename), 3)
	}
	if photosFilename[0].Filename != "party-1.jpg" || photosFilename[1].Filename != "party-2.jpg" || photosFilename[2].Filename != "phone.jpg" {
		t.Fatalf("filename order mismatch: got [%s, %s, %s]", photosFilename[0].Filename, photosFilename[1].Filename, photosFilename[2].Filename)
	}
}

func TestListPhotosFiltered_UnifiedSearchFilters(t *testing.T) {
	database := openTestDB(t)
	libraryPathID := mustLibraryPathID(t, database, "/photos", "photos")

	eventID1, err := InsertEvent(database, &Event{Label: "Trip", PhotoCount: 1})
	if err != nil {
		t.Fatalf("insert event1: %v", err)
	}
	eventID2, err := InsertEvent(database, &Event{Label: "Portrait", PhotoCount: 1})
	if err != nil {
		t.Fatalf("insert event2: %v", err)
	}

	photo1ID := mustInsertPhoto(t, database, photoFixture{
		sha256:        "sha-unified-1",
		filepath:      "/photos/trip/one.jpg",
		filename:      "one.jpg",
		libraryPathID: libraryPathID,
		capturedAt:    mustTime(t, "2024-06-01T10:00:00Z"),
		source:        "scan",
	})
	photo2ID := mustInsertPhoto(t, database, photoFixture{
		sha256:        "sha-unified-2",
		filepath:      "/photos/portrait/two.jpg",
		filename:      "two.jpg",
		libraryPathID: libraryPathID,
		capturedAt:    mustTime(t, "2024-06-02T10:00:00Z"),
		source:        "dropzone",
	})
	mustInsertPhoto(t, database, photoFixture{
		sha256:        "sha-unified-3",
		filepath:      "/photos/unfiled/three.jpg",
		filename:      "three.jpg",
		libraryPathID: libraryPathID,
		capturedAt:    mustTime(t, "2024-06-03T10:00:00Z"),
		source:        "scan",
	})
	mustInsertPhoto(t, database, photoFixture{
		sha256:        "sha-unified-4",
		filepath:      "/photos/family/four.jpg",
		filename:      "four.jpg",
		libraryPathID: libraryPathID,
		capturedAt:    mustTime(t, "2024-06-04T10:00:00Z"),
		source:        "scan",
	})

	overrideDate := "2024-06-02T12:00:00Z"
	if _, err := InsertLibraryCopy(database, &LibraryCopy{
		PhotoSHA256:     "sha-unified-1",
		RelativePath:    "2024/06/one.jpg",
		AbsolutePath:    "/internal/2024/06/one.jpg",
		TrueDateUnknown: false,
		Tags:            []string{"family"},
		EventID:         &eventID1,
	}); err != nil {
		t.Fatalf("insert copy1: %v", err)
	}
	if _, err := InsertLibraryCopy(database, &LibraryCopy{
		PhotoSHA256:     "sha-unified-2",
		RelativePath:    "2024/06/two.jpg",
		AbsolutePath:    "/internal/2024/06/two.jpg",
		TrueDateUnknown: true,
		Tags:            []string{"favorite", "people"},
		EventID:         &eventID2,
		OverrideDate:    &overrideDate,
	}); err != nil {
		t.Fatalf("insert copy2: %v", err)
	}
	if _, err := InsertLibraryCopy(database, &LibraryCopy{
		PhotoSHA256:     "sha-unified-4",
		RelativePath:    "2024/06/four.jpg",
		AbsolutePath:    "/internal/2024/06/four.jpg",
		TrueDateUnknown: false,
		Tags:            []string{"family", "keepers"},
	}); err != nil {
		t.Fatalf("insert copy4: %v", err)
	}

	aliceID, err := InsertPerson(database, "Alice", nil)
	if err != nil {
		t.Fatalf("insert person alice: %v", err)
	}
	bobID, err := InsertPerson(database, "Bob", nil)
	if err != nil {
		t.Fatalf("insert person bob: %v", err)
	}

	if _, err := InsertFace(database, Face{
		PhotoID:  photo1ID,
		PersonID: &aliceID,
		Source:   "manual",
		Verified: true,
	}); err != nil {
		t.Fatalf("insert face alice: %v", err)
	}
	if _, err := InsertFace(database, Face{
		PhotoID:  photo2ID,
		PersonID: &bobID,
		Source:   "auto",
		Verified: false,
	}); err != nil {
		t.Fatalf("insert face bob: %v", err)
	}

	inLibrary := true
	notInLibrary := false
	dateOverride := true
	dateOverrideNo := false
	dateUnknown := true
	verified := true
	unverified := false
	hasPeople := true

	assertPhotoFilter(t, database, PhotoFilter{InLibrary: &inLibrary}, []string{"sha-unified-1", "sha-unified-2", "sha-unified-4"})
	assertPhotoFilter(t, database, PhotoFilter{InLibrary: &notInLibrary}, []string{"sha-unified-3"})
	assertPhotoFilter(t, database, PhotoFilter{Tag: "favorite"}, []string{"sha-unified-2"})
	assertPhotoFilter(t, database, PhotoFilter{EventID: &eventID1}, []string{"sha-unified-1"})
	assertPhotoFilter(t, database, PhotoFilter{HasDateOverride: &dateOverride}, []string{"sha-unified-2"})
	assertPhotoFilter(t, database, PhotoFilter{HasDateOverride: &dateOverrideNo}, []string{"sha-unified-1", "sha-unified-4"})
	assertPhotoFilter(t, database, PhotoFilter{TrueDateUnknown: &dateUnknown}, []string{"sha-unified-2"})
	assertPhotoFilter(t, database, PhotoFilter{Source: "dropzone"}, []string{"sha-unified-2"})
	assertPhotoFilter(t, database, PhotoFilter{PersonID: &aliceID, FaceVerified: &verified}, []string{"sha-unified-1"})
	assertPhotoFilter(t, database, PhotoFilter{PersonID: &bobID, FaceVerified: &verified}, []string{})
	assertPhotoFilter(t, database, PhotoFilter{PersonID: &bobID, FaceVerified: &unverified}, []string{"sha-unified-2"})
	assertPhotoFilter(t, database, PhotoFilter{HasPeople: &hasPeople, FaceVerified: &verified}, []string{"sha-unified-1"})
	assertPhotoFilter(t, database, PhotoFilter{HasPeople: &hasPeople, FaceVerified: &unverified}, []string{"sha-unified-2"})
	assertPhotoFilter(t, database, PhotoFilter{FaceSource: "auto", FaceVerified: &unverified}, []string{"sha-unified-2"})
}

type photoFixture struct {
	sha256        string
	filepath      string
	filename      string
	libraryPathID int64
	capturedAt    time.Time
	cameraMake    string
	cameraModel   string
	latitude      *float64
	longitude     *float64
	flags         []string
	source        string
}

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	database, err := Open(path)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() {
		_ = database.Close()
	})
	return database
}

func mustLibraryPathID(t *testing.T, database *sql.DB, path, label string) int64 {
	t.Helper()
	id, err := UpsertLibraryPath(database, path, label)
	if err != nil {
		t.Fatalf("upsert library path: %v", err)
	}
	return id
}

func mustInsertPhoto(t *testing.T, database *sql.DB, fx photoFixture) int64 {
	t.Helper()
	if fx.source == "" {
		fx.source = "scan"
	}
	id, err := InsertPhoto(database, &Photo{
		SHA256:        fx.sha256,
		Filepath:      fx.filepath,
		LibraryPathID: fx.libraryPathID,
		Filename:      fx.filename,
		CapturedAt:    &fx.capturedAt,
		Latitude:      fx.latitude,
		Longitude:     fx.longitude,
		CameraMake:    fx.cameraMake,
		CameraModel:   fx.cameraModel,
		Flags:         fx.flags,
		Source:        fx.source,
	})
	if err != nil {
		t.Fatalf("insert photo %s: %v", fx.sha256, err)
	}
	return id
}

func mustTime(t *testing.T, value string) time.Time {
	t.Helper()
	ts, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatalf("parse time %q: %v", value, err)
	}
	return ts
}

func toDoublePtrString(v string) **string {
	vv := v
	p := &vv
	return &p
}

func toDoublePtrInt64(v int64) **int64 {
	vv := v
	p := &vv
	return &p
}

func mustCount(t *testing.T, database *sql.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := database.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatalf("count query failed: %v", err)
	}
	return n
}

func assertLibraryFilter(t *testing.T, database *sql.DB, f LibraryCopyFilter, wantSHA []string) {
	t.Helper()
	if f.Page == 0 {
		f.Page = 1
	}
	if f.PerPage == 0 {
		f.PerPage = 50
	}
	copies, total, err := ListLibraryCopiesFiltered(database, f)
	if err != nil {
		t.Fatalf("list library copies filtered: %v", err)
	}
	if total != len(wantSHA) {
		t.Fatalf("total mismatch for %+v: got %d want %d", f, total, len(wantSHA))
	}
	got := make([]string, 0, len(copies))
	for _, c := range copies {
		got = append(got, c.PhotoSHA256)
	}
	sort.Strings(got)
	want := append([]string(nil), wantSHA...)
	sort.Strings(want)
	if len(got) != len(want) {
		t.Fatalf("result length mismatch for %+v: got %d want %d", f, len(got), len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("result mismatch for %+v: got %v want %v", f, got, want)
		}
	}
}

func assertPhotoFilter(t *testing.T, database *sql.DB, f PhotoFilter, wantSHA []string) {
	t.Helper()
	if f.Page == 0 {
		f.Page = 1
	}
	if f.PerPage == 0 {
		f.PerPage = 50
	}
	photos, total, err := ListPhotosFiltered(database, f)
	if err != nil {
		t.Fatalf("list photos filtered: %v", err)
	}
	if total != len(wantSHA) {
		t.Fatalf("total mismatch for %+v: got %d want %d", f, total, len(wantSHA))
	}
	got := make([]string, 0, len(photos))
	for _, p := range photos {
		got = append(got, p.SHA256)
	}
	sort.Strings(got)
	want := append([]string(nil), wantSHA...)
	sort.Strings(want)
	if len(got) != len(want) {
		t.Fatalf("result length mismatch for %+v: got %d want %d", f, len(got), len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("result mismatch for %+v: got %v want %v", f, got, want)
		}
	}
}
