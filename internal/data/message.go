package data

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	chatV1 "github.com/yourusername/chat-app/api/chat/v1"
)

// MessageRepo defines the interface for message data operations
type MessageRepo interface {
	CreateMessage(ctx context.Context, message *chatV1.Message) (*chatV1.Message, error)
	GetMessages(ctx context.Context, roomID int64, limit int32, beforeID int64) ([]*chatV1.Message, bool, error)
	GetMessageByID(ctx context.Context, id int64) (*chatV1.Message, error)
	MarkAsRead(ctx context.Context, messageID, userID int64) error
	GetUnreadCount(ctx context.Context, userID, roomID int64) (int32, error)
}

type messageRepo struct {
	data *Data
	log  *log.Helper
}

// NewMessageRepo creates a new message repository
func NewMessageRepo(data *Data, logger log.Logger) MessageRepo {
	return &messageRepo{
		data: data,
		log:  log.NewHelper(log.With(logger, "module", "data/message")),
	}
}

func (r *messageRepo) CreateMessage(ctx context.Context, message *chatV1.Message) (*chatV1.Message, error) {
	// Get username for the message (denormalized for performance)
	var username string
	userQuery := `SELECT username FROM users WHERE id = $1`
	err := r.data.db.QueryRowContext(ctx, userQuery, message.UserId).Scan(&username)
	if err != nil {
		return nil, fmt.Errorf("failed to get username: %w", err)
	}

	// Insert message into database
	query := `
		INSERT INTO messages (room_id, user_id, content, type, file_url, file_name, file_size, mime_type, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id, created_at`

	now := time.Now()
	message.CreatedAt = now.Unix()
	message.Username = username

	var createdAt time.Time
	err = r.data.db.QueryRowContext(ctx, query,
		message.RoomId,
		message.UserId,
		message.Content,
		message.Type,
		nullString(message.FileUrl),
		nullString(message.FileName),
		nullInt64(message.FileSize),
		nullString(message.MimeType),
		now,
	).Scan(&message.Id, &createdAt)

	if err != nil {
		return nil, fmt.Errorf("failed to create message: %w", err)
	}

	message.CreatedAt = createdAt.Unix()

	// Cache recent messages in Redis for fast access
	if r.data.redis != nil {
		r.cacheMessage(ctx, message)
		r.updateUnreadCounts(ctx, message)
	}

	r.log.Infof("created message: id=%d, room_id=%d, user_id=%d", message.Id, message.RoomId, message.UserId)
	return message, nil
}

func (r *messageRepo) GetMessages(ctx context.Context, roomID int64, limit int32, beforeID int64) ([]*chatV1.Message, bool, error) {
	// Try Redis cache first for recent messages (if no beforeID specified)
	if beforeID == 0 && r.data.redis != nil {
		if messages, ok := r.getCachedMessages(ctx, roomID, limit); ok {
			return messages, len(messages) == int(limit), nil
		}
	}

	// Build query with optional beforeID filter
	var query string
	var args []interface{}

	if beforeID > 0 {
		query = `
			SELECT m.id, m.room_id, m.user_id, u.username, m.content, m.type,
				   m.is_edited, m.edited_at, m.created_at,
				   m.file_url, m.file_name, m.file_size, m.mime_type
			FROM messages m
			JOIN users u ON m.user_id = u.id
			WHERE m.room_id = $1 AND m.id < $2
			ORDER BY m.created_at DESC
			LIMIT $3`
		args = []interface{}{roomID, beforeID, limit}
	} else {
		query = `
			SELECT m.id, m.room_id, m.user_id, u.username, m.content, m.type,
				   m.is_edited, m.edited_at, m.created_at,
				   m.file_url, m.file_name, m.file_size, m.mime_type
			FROM messages m
			JOIN users u ON m.user_id = u.id
			WHERE m.room_id = $1
			ORDER BY m.created_at DESC
			LIMIT $2`
		args = []interface{}{roomID, limit}
	}

	rows, err := r.data.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, false, fmt.Errorf("failed to get messages: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var messages []*chatV1.Message
	for rows.Next() {
		message := &chatV1.Message{}
		var createdAt time.Time
		var editedAt sql.NullTime
		var fileURL, fileName, mimeType sql.NullString
		var fileSize sql.NullInt64

		err := rows.Scan(
			&message.Id,
			&message.RoomId,
			&message.UserId,
			&message.Username,
			&message.Content,
			&message.Type,
			&message.IsEdited,
			&editedAt,
			&createdAt,
			&fileURL,
			&fileName,
			&fileSize,
			&mimeType,
		)
		if err != nil {
			return nil, false, fmt.Errorf("failed to scan message: %w", err)
		}

		message.CreatedAt = createdAt.Unix()
		if editedAt.Valid {
			message.EditedAt = editedAt.Time.Unix()
		}
		if fileURL.Valid {
			message.FileUrl = fileURL.String
		}
		if fileName.Valid {
			message.FileName = fileName.String
		}
		if fileSize.Valid {
			message.FileSize = fileSize.Int64
		}
		if mimeType.Valid {
			message.MimeType = mimeType.String
		}

		messages = append(messages, message)
	}

	// Check if there are more messages
	hasMore := len(messages) == int(limit)
	if hasMore && len(messages) > 0 {
		// Check if there's actually a next message
		var count int
		nextQuery := `SELECT COUNT(*) FROM messages WHERE room_id = $1 AND id < $2`
		lastID := messages[len(messages)-1].Id
		if err := r.data.db.QueryRowContext(ctx, nextQuery, roomID, lastID).Scan(&count); err != nil {
			r.log.Warnf("failed to check for more messages: %v", err)
		}
		hasMore = count > 0
	}

	// Cache recent messages if this was a recent query
	if beforeID == 0 && r.data.redis != nil && len(messages) > 0 {
		r.cacheMessages(ctx, roomID, messages)
	}

	return messages, hasMore, nil
}

func (r *messageRepo) GetMessageByID(ctx context.Context, id int64) (*chatV1.Message, error) {
	message := &chatV1.Message{}
	var createdAt time.Time
	var editedAt sql.NullTime
	var fileURL, fileName, mimeType sql.NullString
	var fileSize sql.NullInt64

	query := `
		SELECT m.id, m.room_id, m.user_id, u.username, m.content, m.type,
		       m.is_edited, m.edited_at, m.created_at,
		       m.file_url, m.file_name, m.file_size, m.mime_type
		FROM messages m
		JOIN users u ON m.user_id = u.id
		WHERE m.id = $1`

	err := r.data.db.QueryRowContext(ctx, query, id).Scan(
		&message.Id,
		&message.RoomId,
		&message.UserId,
		&message.Username,
		&message.Content,
		&message.Type,
		&message.IsEdited,
		&editedAt,
		&createdAt,
		&fileURL,
		&fileName,
		&fileSize,
		&mimeType,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("message not found")
		}
		return nil, fmt.Errorf("failed to get message: %w", err)
	}

	message.CreatedAt = createdAt.Unix()
	if editedAt.Valid {
		message.EditedAt = editedAt.Time.Unix()
	}
	if fileURL.Valid {
		message.FileUrl = fileURL.String
	}
	if fileName.Valid {
		message.FileName = fileName.String
	}
	if fileSize.Valid {
		message.FileSize = fileSize.Int64
	}
	if mimeType.Valid {
		message.MimeType = mimeType.String
	}

	return message, nil
}

func (r *messageRepo) MarkAsRead(ctx context.Context, messageID, userID int64) error {
	// Insert or ignore if already exists
	query := `
		INSERT INTO message_reads (message_id, user_id, read_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (message_id, user_id) DO NOTHING`

	_, err := r.data.db.ExecContext(ctx, query, messageID, userID, time.Now())
	if err != nil {
		return fmt.Errorf("failed to mark message as read: %w", err)
	}

	return nil
}

func (r *messageRepo) GetUnreadCount(ctx context.Context, userID, roomID int64) (int32, error) {
	// Try Redis cache first
	if r.data.redis != nil {
		key := fmt.Sprintf("unread:%d:%d", userID, roomID)
		count, err := r.data.redis.Get(ctx, key).Int()
		if err == nil {
			return int32(count), nil
		}
	}

	// Fall back to database
	var count int32
	query := `
		SELECT COUNT(*)
		FROM messages m
		LEFT JOIN message_reads mr ON m.id = mr.message_id AND mr.user_id = $1
		WHERE m.room_id = $2 AND m.user_id != $1 AND mr.message_id IS NULL`

	err := r.data.db.QueryRowContext(ctx, query, userID, roomID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to get unread count: %w", err)
	}

	// Cache the result
	if r.data.redis != nil {
		key := fmt.Sprintf("unread:%d:%d", userID, roomID)
		r.data.redis.Set(ctx, key, count, 30*time.Minute)
	}

	return count, nil
}

// Helper functions for Redis caching
func (r *messageRepo) cacheMessage(ctx context.Context, message *chatV1.Message) {
	key := fmt.Sprintf("room:%d:messages", message.RoomId)

	// Add to list (most recent first)
	r.data.redis.LPush(ctx, key, r.serializeMessage(message))

	// Keep only last 100 messages
	r.data.redis.LTrim(ctx, key, 0, 99)

	// Set expiration
	r.data.redis.Expire(ctx, key, time.Hour)
}

func (r *messageRepo) cacheMessages(ctx context.Context, roomID int64, messages []*chatV1.Message) {
	if len(messages) == 0 {
		return
	}

	key := fmt.Sprintf("room:%d:messages", roomID)

	// Clear existing cache
	r.data.redis.Del(ctx, key)

	// Add all messages (reverse order since LPush adds to front)
	for i := len(messages) - 1; i >= 0; i-- {
		r.data.redis.LPush(ctx, key, r.serializeMessage(messages[i]))
	}

	r.data.redis.Expire(ctx, key, time.Hour)
}

func (r *messageRepo) getCachedMessages(ctx context.Context, roomID int64, limit int32) ([]*chatV1.Message, bool) {
	key := fmt.Sprintf("room:%d:messages", roomID)

	cached := r.data.redis.LRange(ctx, key, 0, int64(limit-1)).Val()
	if len(cached) == 0 {
		return nil, false
	}

	var messages []*chatV1.Message
	for _, data := range cached {
		if message := r.deserializeMessage(data); message != nil {
			messages = append(messages, message)
		}
	}

	return messages, true
}

func (r *messageRepo) updateUnreadCounts(ctx context.Context, message *chatV1.Message) {
	// Get room members from Redis
	membersKey := fmt.Sprintf("room:%d:members", message.RoomId)
	members := r.data.redis.SMembers(ctx, membersKey).Val()

	// Update unread count for each member (except sender)
	for _, memberIDStr := range members {
		if memberIDStr != fmt.Sprintf("%d", message.UserId) {
			unreadKey := fmt.Sprintf("unread:%s:%d", memberIDStr, message.RoomId)
			r.data.redis.Incr(ctx, unreadKey)
			r.data.redis.Expire(ctx, unreadKey, 24*time.Hour)
		}
	}
}

// Serialization helpers using JSON for proper handling of special characters
func (r *messageRepo) serializeMessage(message *chatV1.Message) string {
	data, err := json.Marshal(message)
	if err != nil {
		r.log.Warnf("failed to serialize message: %v", err)
		return ""
	}
	return string(data)
}

func (r *messageRepo) deserializeMessage(data string) *chatV1.Message {
	message := &chatV1.Message{}
	if err := json.Unmarshal([]byte(data), message); err != nil {
		r.log.Warnf("failed to deserialize message: %v", err)
		return nil
	}
	return message
}

// Helper functions for nullable database values
func nullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

func nullInt64(i int64) sql.NullInt64 {
	if i == 0 {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: i, Valid: true}
}
