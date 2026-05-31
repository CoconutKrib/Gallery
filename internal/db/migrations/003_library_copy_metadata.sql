-- Rich annotations on library copies: title, description, override_date, event_id.
-- These are carried from staging_queue at copy time and can be updated post-copy.
ALTER TABLE library_copies ADD COLUMN title        TEXT;
ALTER TABLE library_copies ADD COLUMN description  TEXT;
ALTER TABLE library_copies ADD COLUMN override_date TEXT;  -- RFC3339 UTC; when set, drives path placement
ALTER TABLE library_copies ADD COLUMN event_id     INTEGER REFERENCES events(id);

CREATE INDEX IF NOT EXISTS idx_library_copies_event_id ON library_copies(event_id);
