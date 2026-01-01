package data

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	chatV1 "github.com/yourusername/chat-app/api/chat/v1"
)

// RoomRepo defines the interface for room data operations
type RoomRepo interface {
	CreateRoom(ctx context.Context, room *chatV1.Room) (*chatV1.Room, error)
	GetRoomByID(ctx context.Context, id int64) (*chatV1.Room, error)
	ListUserRooms(ctx context.Context, userID int64, limit, offset int32) ([]*chatV1.Room, int32, error)
	JoinRoom(ctx context.Context, roomID, userID int64, role string) error
	LeaveRoom(ctx context.Context, roomID, userID int64) error
	GetRoomMembers(ctx context.Context, roomID int64) ([]*chatV1.RoomMember, error)
	IsUserInRoom(ctx context.Context, roomID, userID int64) (bool, error)
}

type roomRepo struct {
	data *Data
	log  *log.Helper
}

// NewRoomRepo creates a new room repository
func NewRoomRepo(data *Data, logger log.Logger) RoomRepo {
	return &roomRepo{
		data: data,
		log:  log.NewHelper(log.With(logger, "module", "data/room")),
	}
}

func (r *roomRepo) CreateRoom(ctx context.Context, room *chatV1.Room) (*chatV1.Room, error) {
	query := `
		INSERT INTO rooms (name, description, type, created_by, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id`

	now := time.Now()
	room.CreatedAt = now.Unix()

	err := r.data.db.QueryRowContext(ctx, query,
		room.Name,
		room.Description,
		room.Type,
		room.CreatedBy,
		now,
		now,
	).Scan(&room.Id)

	if err != nil {
		return nil, fmt.Errorf("failed to create room: %w", err)
	}

	// Add creator as admin member
	if err := r.JoinRoom(ctx, room.Id, room.CreatedBy, "admin"); err != nil {
		return nil, fmt.Errorf("failed to add creator to room: %w", err)
	}

	r.log.Infof("created room: id=%d, name=%s", room.Id, room.Name)
	return room, nil
}

func (r *roomRepo) GetRoomByID(ctx context.Context, id int64) (*chatV1.Room, error) {
	room := &chatV1.Room{}
	var createdAt time.Time

	query := `
		SELECT id, name, description, type, created_by, created_at,
		       (SELECT COUNT(*) FROM room_members WHERE room_id = $1) as member_count
		FROM rooms
		WHERE id = $1`

	err := r.data.db.QueryRowContext(ctx, query, id).Scan(
		&room.Id,
		&room.Name,
		&room.Description,
		&room.Type,
		&room.CreatedBy,
		&createdAt,
		&room.MemberCount,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("room not found")
		}
		return nil, fmt.Errorf("failed to get room: %w", err)
	}

	room.CreatedAt = createdAt.Unix()

	// Get room members
	members, err := r.GetRoomMembers(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get room members: %w", err)
	}
	room.Members = members

	return room, nil
}

func (r *roomRepo) ListUserRooms(ctx context.Context, userID int64, limit, offset int32) ([]*chatV1.Room, int32, error) {
	query := `
		SELECT r.id, r.name, r.description, r.type, r.created_by, r.created_at,
		       (SELECT COUNT(*) FROM room_members WHERE room_id = r.id) as member_count
		FROM rooms r
		JOIN room_members rm ON r.id = rm.room_id
		WHERE rm.user_id = $1
		ORDER BY r.updated_at DESC
		LIMIT $2 OFFSET $3`

	rows, err := r.data.db.QueryContext(ctx, query, userID, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list user rooms: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var rooms []*chatV1.Room
	for rows.Next() {
		room := &chatV1.Room{}
		var createdAt time.Time

		err := rows.Scan(
			&room.Id,
			&room.Name,
			&room.Description,
			&room.Type,
			&room.CreatedBy,
			&createdAt,
			&room.MemberCount,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to scan room: %w", err)
		}

		room.CreatedAt = createdAt.Unix()
		rooms = append(rooms, room)
	}

	// Get total count
	var total int32
	countQuery := `
		SELECT COUNT(*)
		FROM rooms r
		JOIN room_members rm ON r.id = rm.room_id
		WHERE rm.user_id = $1`

	err = r.data.db.QueryRowContext(ctx, countQuery, userID).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to get total room count: %w", err)
	}

	return rooms, total, nil
}

func (r *roomRepo) JoinRoom(ctx context.Context, roomID, userID int64, role string) error {
	// Check if user is already in the room
	exists, err := r.IsUserInRoom(ctx, roomID, userID)
	if err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("user already in room")
	}

	query := `
		INSERT INTO room_members (room_id, user_id, role, joined_at)
		VALUES ($1, $2, $3, $4)`

	_, err = r.data.db.ExecContext(ctx, query, roomID, userID, role, time.Now())
	if err != nil {
		return fmt.Errorf("failed to join room: %w", err)
	}

	// Update Redis set of room members for fast access
	if r.data.redis != nil {
		key := fmt.Sprintf("room:%d:members", roomID)
		r.data.redis.SAdd(ctx, key, userID)
		r.data.redis.Expire(ctx, key, time.Hour) // Cache for 1 hour
	}

	r.log.Infof("user joined room: user_id=%d, room_id=%d, role=%s", userID, roomID, role)
	return nil
}

func (r *roomRepo) LeaveRoom(ctx context.Context, roomID, userID int64) error {
	query := `DELETE FROM room_members WHERE room_id = $1 AND user_id = $2`

	result, err := r.data.db.ExecContext(ctx, query, roomID, userID)
	if err != nil {
		return fmt.Errorf("failed to leave room: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return fmt.Errorf("user not in room")
	}

	// Remove from Redis set
	if r.data.redis != nil {
		key := fmt.Sprintf("room:%d:members", roomID)
		r.data.redis.SRem(ctx, key, userID)
	}

	r.log.Infof("user left room: user_id=%d, room_id=%d", userID, roomID)
	return nil
}

func (r *roomRepo) GetRoomMembers(ctx context.Context, roomID int64) ([]*chatV1.RoomMember, error) {
	query := `
		SELECT rm.user_id, u.username, rm.role, rm.joined_at
		FROM room_members rm
		JOIN users u ON rm.user_id = u.id
		WHERE rm.room_id = $1
		ORDER BY rm.joined_at`

	rows, err := r.data.db.QueryContext(ctx, query, roomID)
	if err != nil {
		return nil, fmt.Errorf("failed to get room members: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var members []*chatV1.RoomMember
	for rows.Next() {
		member := &chatV1.RoomMember{}
		var joinedAt time.Time

		err := rows.Scan(
			&member.UserId,
			&member.Username,
			&member.Role,
			&joinedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan room member: %w", err)
		}

		member.JoinedAt = joinedAt.Unix()
		members = append(members, member)
	}

	return members, nil
}

func (r *roomRepo) IsUserInRoom(ctx context.Context, roomID, userID int64) (bool, error) {
	// TODO: Add Redis caching later with proper synchronization
	// For now, always use database for accuracy

	var exists bool
	query := `SELECT EXISTS(SELECT 1 FROM room_members WHERE room_id = $1 AND user_id = $2)`

	err := r.data.db.QueryRowContext(ctx, query, roomID, userID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("failed to check room membership: %w", err)
	}
	r.log.Infof("IsUserInRoom: DB check - room_id=%d, user_id=%d, exists=%v", roomID, userID, exists)

	return exists, nil
}
