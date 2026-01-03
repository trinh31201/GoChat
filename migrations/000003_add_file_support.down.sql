-- Remove file metadata columns from messages table
DROP INDEX IF EXISTS idx_messages_type;

ALTER TABLE messages DROP COLUMN IF EXISTS mime_type;
ALTER TABLE messages DROP COLUMN IF EXISTS file_size;
ALTER TABLE messages DROP COLUMN IF EXISTS file_name;
ALTER TABLE messages DROP COLUMN IF EXISTS file_url;
