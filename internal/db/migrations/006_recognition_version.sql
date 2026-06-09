-- 006_recognition_version: Track which photos have had face recognition run,
-- and with which version of the detection models. Also tracks the current
-- processing status so we can resume after a restart.
ALTER TABLE photos ADD COLUMN recognition_version INTEGER;   -- NULL = never attempted
ALTER TABLE photos ADD COLUMN recognition_status TEXT DEFAULT NULL;  -- 'pending', 'done', 'error'
ALTER TABLE photos ADD COLUMN recognition_error TEXT DEFAULT NULL;
