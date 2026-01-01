package biz

import (
	"context"
	"errors"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	chatV1 "github.com/yourusername/chat-app/api/chat/v1"
)

var (
	ErrRoomNotFound          = errors.New("room not found")
	ErrRoomAccessDenied      = errors.New("access denied to room")
	ErrUserAlreadyInRoom     = errors.New("user already in room")
	ErrUserNotInRoom         = errors.New("user not in room")
	ErrCannotJoinPrivateRoom = errors.New("cannot join private room without invitation")
)

// Room represents the room business entity
type Room struct {
	ID          int64
	Name        string
	Description string
	Type        string // public, private, direct
	CreatedBy   int64
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// RoomMember represents room membership
type RoomMember struct {
	ID       int64
	RoomID   int64
	UserID   int64
	Role     string // admin, moderator, member
	JoinedAt time.Time
}

// RoomRepo defines the interface for room data access
type RoomRepo interface {
	CreateRoom(ctx context.Context, room *Room) (*Room, error)
	GetRoomByID(ctx context.Context, id int64) (*Room, error)
	ListUserRooms(ctx context.Context, userID int64, limit, offset int32) ([]*Room, int32, error)
	IsUserInRoom(ctx context.Context, roomID, userID int64) (bool, error)
	JoinRoom(ctx context.Context, roomID, userID int64, role string) error
	LeaveRoom(ctx context.Context, roomID, userID int64) error
	GetRoomMembers(ctx context.Context, roomID int64) ([]*RoomMember, error)
}

// RoomUseCase contains room business logic
type RoomUseCase struct {
	repo     RoomRepo
	userRepo UserRepo
	log      *log.Helper
}

// NewRoomUseCase creates a new room use case
func NewRoomUseCase(repo RoomRepo, userRepo UserRepo, logger log.Logger) *RoomUseCase {
	return &RoomUseCase{
		repo:     repo,
		userRepo: userRepo,
		log:      log.NewHelper(log.With(logger, "module", "biz/room")),
	}
}

// CreateRoom creates a new chat room
func (uc *RoomUseCase) CreateRoom(ctx context.Context, userID int64, req *chatV1.CreateRoomRequest) (*Room, error) {
	uc.log.Infof("Creating room: %s by user %d", req.Name, userID)

	// Validate input
	if err := uc.validateCreateRoomRequest(req); err != nil {
		return nil, err
	}

	// Verify user exists
	_, err := uc.userRepo.GetUserByID(ctx, userID)
	if err != nil {
		return nil, ErrUserNotFound
	}

	// Create room
	room := &Room{
		Name:        req.Name,
		Description: req.Description,
		Type:        req.Type,
		CreatedBy:   userID,
	}

	createdRoom, err := uc.repo.CreateRoom(ctx, room)
	if err != nil {
		uc.log.Errorf("Failed to create room: %v", err)
		return nil, err
	}

	// Note: Creator is automatically added as admin in repo.CreateRoom

	uc.log.Infof("Room created successfully: id=%d, name=%s", createdRoom.ID, createdRoom.Name)
	return createdRoom, nil
}

// GetRoom retrieves room information if user has access
func (uc *RoomUseCase) GetRoom(ctx context.Context, userID, roomID int64) (*Room, error) {
	// Check if user is a member of the room
	isMember, err := uc.repo.IsUserInRoom(ctx, roomID, userID)
	if err != nil {
		uc.log.Errorf("Failed to check room membership: %v", err)
		return nil, err
	}

	if !isMember {
		return nil, ErrRoomAccessDenied
	}

	// Get room
	room, err := uc.repo.GetRoomByID(ctx, roomID)
	if err != nil {
		return nil, ErrRoomNotFound
	}

	return room, nil
}

// ListUserRooms lists rooms for a user
func (uc *RoomUseCase) ListUserRooms(ctx context.Context, userID int64, limit, offset int32) ([]*Room, int32, error) {
	return uc.repo.ListUserRooms(ctx, userID, limit, offset)
}

// JoinRoom allows a user to join a room
func (uc *RoomUseCase) JoinRoom(ctx context.Context, userID, roomID int64) (*Room, error) {
	uc.log.Infof("User %d attempting to join room %d", userID, roomID)

	// Verify room exists
	room, err := uc.repo.GetRoomByID(ctx, roomID)
	if err != nil {
		return nil, ErrRoomNotFound
	}

	// Check if user is already in room
	isMember, err := uc.repo.IsUserInRoom(ctx, roomID, userID)
	if err != nil {
		return nil, err
	}
	if isMember {
		return nil, ErrUserAlreadyInRoom
	}

	// Check room access rules
	if room.Type == "private" {
		// For private rooms, only allow if user is invited
		// In a real app, you'd have an invitations system
		return nil, ErrCannotJoinPrivateRoom
	}

	// Verify user exists
	_, err = uc.userRepo.GetUserByID(ctx, userID)
	if err != nil {
		return nil, ErrUserNotFound
	}

	// Join room with "member" role by default
	if err := uc.repo.JoinRoom(ctx, roomID, userID, "member"); err != nil {
		uc.log.Errorf("Failed to join room: %v", err)
		return nil, err
	}

	uc.log.Infof("User %d joined room %d successfully", userID, roomID)
	return room, nil
}

// LeaveRoom allows a user to leave a room
func (uc *RoomUseCase) LeaveRoom(ctx context.Context, userID, roomID int64) error {
	// Check if user is in the room
	isMember, err := uc.repo.IsUserInRoom(ctx, roomID, userID)
	if err != nil {
		return err
	}
	if !isMember {
		return ErrUserNotInRoom
	}

	// Leave room
	if err := uc.repo.LeaveRoom(ctx, roomID, userID); err != nil {
		uc.log.Errorf("Failed to leave room: %v", err)
		return err
	}

	uc.log.Infof("User %d left room %d", userID, roomID)
	return nil
}

// GetRoomMembers retrieves all members of a room
func (uc *RoomUseCase) GetRoomMembers(ctx context.Context, userID, roomID int64) ([]*RoomMember, error) {
	// Check if user has access to the room
	isMember, err := uc.repo.IsUserInRoom(ctx, roomID, userID)
	if err != nil {
		return nil, err
	}
	if !isMember {
		return nil, ErrRoomAccessDenied
	}

	return uc.repo.GetRoomMembers(ctx, roomID)
}

// validateCreateRoomRequest validates room creation input
func (uc *RoomUseCase) validateCreateRoomRequest(req *chatV1.CreateRoomRequest) error {
	if req.Name == "" {
		return errors.New("room name is required")
	}
	if len(req.Name) > 100 {
		return errors.New("room name must be less than 100 characters")
	}
	if req.Type == "" {
		req.Type = "public" // Default to public
	}
	if req.Type != "public" && req.Type != "private" {
		return errors.New("room type must be 'public' or 'private'")
	}
	if len(req.Description) > 500 {
		return errors.New("room description must be less than 500 characters")
	}
	return nil
}