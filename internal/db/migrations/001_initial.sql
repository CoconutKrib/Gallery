CREATE TABLE IF NOT EXISTS schema_migrations (
    version     INTEGER PRIMARY KEY,
    applied_at  DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS library_paths (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    path            TEXT    NOT NULL UNIQUE,
    label           TEXT    NOT NULL DEFAULT '',
    last_scanned_at DATETIME
);

CREATE TABLE IF NOT EXISTS photos (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    sha256          TEXT    NOT NULL UNIQUE,
    filepath        TEXT    NOT NULL,
    library_path_id INTEGER NOT NULL REFERENCES library_paths(id),
    filename        TEXT    NOT NULL,
    captured_at     DATETIME,
    latitude        REAL,
    longitude       REAL,
    altitude        REAL,
    camera_make     TEXT    NOT NULL DEFAULT '',
    camera_model    TEXT    NOT NULL DEFAULT '',
    camera_serial   TEXT,
    lens_model      TEXT,
    iso             INTEGER,
    aperture        REAL,
    shutter_speed   TEXT,
    focal_length    REAL,
    flash           INTEGER,
    width           INTEGER,
    height          INTEGER,
    orientation     INTEGER,
    thumbnail_path  TEXT,
    flags           TEXT    NOT NULL DEFAULT '[]',
    ingested_at     DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_photos_captured_at     ON photos(captured_at);
CREATE INDEX IF NOT EXISTS idx_photos_library_path_id ON photos(library_path_id);
CREATE INDEX IF NOT EXISTS idx_photos_latitude        ON photos(latitude);
CREATE INDEX IF NOT EXISTS idx_photos_longitude       ON photos(longitude);

CREATE TABLE IF NOT EXISTS duplicate_paths (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    sha256          TEXT    NOT NULL REFERENCES photos(sha256),
    filepath        TEXT    NOT NULL,
    library_path_id INTEGER NOT NULL REFERENCES library_paths(id),
    discovered_at   DATETIME NOT NULL DEFAULT (datetime('now')),
    UNIQUE(sha256, filepath)
);

CREATE TABLE IF NOT EXISTS scan_runs (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    library_path_id   INTEGER NOT NULL REFERENCES library_paths(id),
    started_at        DATETIME NOT NULL DEFAULT (datetime('now')),
    finished_at       DATETIME,
    files_found       INTEGER NOT NULL DEFAULT 0,
    files_skipped     INTEGER NOT NULL DEFAULT 0,
    files_ingested    INTEGER NOT NULL DEFAULT 0,
    files_duplicate   INTEGER NOT NULL DEFAULT 0,
    files_error       INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS events (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    label        TEXT    NOT NULL DEFAULT '',
    started_at   DATETIME,
    ended_at     DATETIME,
    centroid_lat REAL,
    centroid_lon REAL,
    photo_count  INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS photo_events (
    photo_id INTEGER NOT NULL REFERENCES photos(id),
    event_id INTEGER NOT NULL REFERENCES events(id),
    PRIMARY KEY (photo_id, event_id)
);

CREATE TABLE IF NOT EXISTS faces (
    id       INTEGER PRIMARY KEY AUTOINCREMENT,
    photo_id INTEGER NOT NULL REFERENCES photos(id)
    -- Reserved for future face detection features.
    -- Additional columns (embedding, bounding box, person_id, etc.) added in a future migration.
);
