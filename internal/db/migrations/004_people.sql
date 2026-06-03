-- 004_people.sql
-- People registry and face-tagging support (Phase A: manual tagging).
-- Phase B/C (auto-detection + recognition) use the same tables but populate the
-- embedding, confidence, source='auto', and verified=0 columns.

-- Identity registry.
-- Implicitly scoped to the internal library (single internal library per config).
CREATE TABLE IF NOT EXISTS people (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    name          TEXT    NOT NULL DEFAULT '',
    notes         TEXT,
    created_at    TEXT    NOT NULL DEFAULT (datetime('now'))
);

-- Extend the existing faces table (created as an empty stub in 001_initial.sql).
--   person_id   → which person this face belongs to (NULL = unidentified)
--   bbox_*      → normalised 0.0–1.0 bounding box; all NULL = presence-only tag
--   source      → 'manual' (user-entered) or 'auto' (face-detection pipeline)
--   confidence  → detection confidence from the model; NULL for manual tags
--   embedding   → raw little-endian float32 bytes (512-dim ArcFace); NULL in Phase A
--   verified    → 1 = user confirmed; 0 = auto-suggested but not yet confirmed
ALTER TABLE faces ADD COLUMN person_id   INTEGER REFERENCES people(id);
ALTER TABLE faces ADD COLUMN bbox_x      REAL;
ALTER TABLE faces ADD COLUMN bbox_y      REAL;
ALTER TABLE faces ADD COLUMN bbox_w      REAL;
ALTER TABLE faces ADD COLUMN bbox_h      REAL;
ALTER TABLE faces ADD COLUMN source      TEXT NOT NULL DEFAULT 'manual';
ALTER TABLE faces ADD COLUMN confidence  REAL;
ALTER TABLE faces ADD COLUMN embedding   BLOB;
ALTER TABLE faces ADD COLUMN verified    INTEGER NOT NULL DEFAULT 0;
ALTER TABLE faces ADD COLUMN created_at  TEXT    NOT NULL DEFAULT (datetime('now'));

-- cover_face_id added after faces is extended to avoid a forward-reference problem.
ALTER TABLE people ADD COLUMN cover_face_id INTEGER REFERENCES faces(id);

CREATE INDEX IF NOT EXISTS idx_faces_photo_id  ON faces(photo_id);
CREATE INDEX IF NOT EXISTS idx_faces_person_id ON faces(person_id);
CREATE INDEX IF NOT EXISTS idx_faces_source    ON faces(source);
CREATE INDEX IF NOT EXISTS idx_faces_verified  ON faces(verified);
