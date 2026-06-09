-- 005_heic: Add format column to photos table for HEIC/JPEG tracking.
ALTER TABLE photos ADD COLUMN format TEXT NOT NULL DEFAULT 'jpeg';
