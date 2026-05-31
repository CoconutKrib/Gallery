-- Source column on photos; 'scan' for normal library paths, 'dropzone' for dropzone ingest
ALTER TABLE photos ADD COLUMN source TEXT NOT NULL DEFAULT 'scan';

-- Staging queue: holding area before photos are copied to the internal library
CREATE TABLE IF NOT EXISTS staging_queue (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    photo_sha256      TEXT    NOT NULL UNIQUE REFERENCES photos(sha256),
    title             TEXT,
    description       TEXT,
    override_date     TEXT,               -- RFC3339 UTC, nullable
    override_lat      REAL,
    override_lon      REAL,
    event_id          INTEGER REFERENCES events(id),
    tags              TEXT    NOT NULL DEFAULT '[]',  -- JSON array
    true_date_unknown INTEGER NOT NULL DEFAULT 0,
    state             TEXT    NOT NULL DEFAULT 'staged',  -- staged | approved | rejected
    created_at        TEXT    NOT NULL,
    updated_at        TEXT    NOT NULL
);

-- Internal library copies: permanent record of photos copied to the internal library
CREATE TABLE IF NOT EXISTS library_copies (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    photo_sha256      TEXT    NOT NULL UNIQUE REFERENCES photos(sha256),
    relative_path     TEXT    NOT NULL,  -- e.g. 2024/06/Wedding-Smith/IMG_0001.jpg
    absolute_path     TEXT    NOT NULL,
    true_date_unknown INTEGER NOT NULL DEFAULT 0,
    tags              TEXT    NOT NULL DEFAULT '[]',  -- JSON array; carried over from staging_queue at copy time
    copied_at         TEXT    NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_staging_state ON staging_queue(state);
CREATE INDEX IF NOT EXISTS idx_library_copies_sha ON library_copies(photo_sha256);
