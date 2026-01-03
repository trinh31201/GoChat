-- Add file metadata columns to messages table
ALTER TABLE messages ADD COLUMN IF NOT EXISTS file_url VARCHAR(500);
ALTER TABLE messages ADD COLUMN IF NOT EXISTS file_name VARCHAR(255);
ALTER TABLE messages ADD COLUMN IF NOT EXISTS file_size BIGINT;
ALTER TABLE messages ADD COLUMN IF NOT EXISTS mime_type VARCHAR(100);

-- Create index for file messages
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_messages_type ON messages(type) WHERE type IN ('image', 'file');

COMMENT ON COLUMN messages.file_url IS 'URL to the file in MinIO storage';
COMMENT ON COLUMN messages.file_name IS 'Original filename uploaded by user';
COMMENT ON COLUMN messages.file_size IS 'File size in bytes';
COMMENT ON COLUMN messages.mime_type IS 'MIME type of the file (e.g., image/png, application/pdf)';
