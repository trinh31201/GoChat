-- Rollback: Drop all performance indexes

DROP INDEX IF EXISTS idx_messages_room_created;
DROP INDEX IF EXISTS idx_messages_user_id;
DROP INDEX IF EXISTS idx_room_members_user_id;
DROP INDEX IF EXISTS idx_room_members_room_id;
DROP INDEX IF EXISTS idx_users_email;
DROP INDEX IF EXISTS idx_users_username;
DROP INDEX IF EXISTS idx_message_reads_user_id;
DROP INDEX IF EXISTS idx_message_reads_message_id;
