package data

import (
	"context"
	"fmt"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	chatV1 "github.com/yourusername/chat-app/api/chat/v1"
	userV1 "github.com/yourusername/chat-app/api/user/v1"
	"github.com/yourusername/chat-app/internal/biz"
	"golang.org/x/crypto/bcrypt"
)

// UserRepoAdapter adapts the data layer UserRepo to biz layer UserRepo interface
type UserRepoAdapter struct {
	repo UserRepo
	log  *log.Helper
}

// NewUserRepoAdapter creates a new user repository adapter
func NewUserRepoAdapter(repo UserRepo, logger log.Logger) biz.UserRepo {
	return &UserRepoAdapter{
		repo: repo,
		log:  log.NewHelper(log.With(logger, "module", "data/user_adapter")),
	}
}

// CreateUser creates a new user in the database
func (a *UserRepoAdapter) CreateUser(ctx context.Context, user *biz.User, passwordHash string) (*biz.User, error) {
	// Convert biz.User to userV1.User for data layer
	dataUser := &userV1.User{
		Username:  user.Username,
		Email:     user.Email,
		AvatarUrl: user.AvatarURL,
		Status:    user.Status,
	}

	// Create user using data layer (it will hash the password again, but we pass the raw password for now)
	// TODO: Fix this - we need to modify data layer to accept already hashed passwords
	createdUser, err := a.repo.CreateUser(ctx, dataUser, passwordHash)
	if err != nil {
		return nil, err
	}

	// Convert back to biz.User
	return &biz.User{
		ID:        createdUser.Id,
		Username:  createdUser.Username,
		Email:     createdUser.Email,
		AvatarURL: createdUser.AvatarUrl,
		Status:    createdUser.Status,
		LastSeen:  time.Unix(createdUser.LastSeen, 0),
		CreatedAt: time.Unix(createdUser.CreatedAt, 0),
		UpdatedAt: time.Now(),
	}, nil
}

// GetUserByID retrieves a user by ID
func (a *UserRepoAdapter) GetUserByID(ctx context.Context, id int64) (*biz.User, error) {
	user, err := a.repo.GetUserByID(ctx, id)
	if err != nil {
		return nil, err
	}

	return &biz.User{
		ID:        user.Id,
		Username:  user.Username,
		Email:     user.Email,
		AvatarURL: user.AvatarUrl,
		Status:    user.Status,
		LastSeen:  time.Unix(user.LastSeen, 0),
		CreatedAt: time.Unix(user.CreatedAt, 0),
		UpdatedAt: time.Now(),
	}, nil
}

// GetUserByEmail retrieves a user by email
func (a *UserRepoAdapter) GetUserByEmail(ctx context.Context, email string) (*biz.User, error) {
	user, _, err := a.repo.GetUserByEmail(ctx, email)
	if err != nil {
		return nil, err
	}

	return &biz.User{
		ID:        user.Id,
		Username:  user.Username,
		Email:     user.Email,
		AvatarURL: user.AvatarUrl,
		Status:    user.Status,
		LastSeen:  time.Unix(user.LastSeen, 0),
		CreatedAt: time.Unix(user.CreatedAt, 0),
		UpdatedAt: time.Now(),
	}, nil
}

// GetUserByUsername retrieves a user by username
func (a *UserRepoAdapter) GetUserByUsername(ctx context.Context, username string) (*biz.User, error) {
	// This method needs to be added to the data layer UserRepo interface
	// For now, return not found error
	return nil, biz.ErrUserNotFound
}

// UpdateUserStatus updates user's online status
func (a *UserRepoAdapter) UpdateUserStatus(ctx context.Context, id int64, status string) error {
	return a.repo.UpdateUserStatus(ctx, id, status)
}

// VerifyPassword verifies user password and returns user if valid
func (a *UserRepoAdapter) VerifyPassword(ctx context.Context, email, password string) (*biz.User, error) {
	user, passwordHash, err := a.repo.GetUserByEmail(ctx, email)
	if err != nil {
		return nil, err
	}

	// Verify password
	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(password)); err != nil {
		return nil, biz.ErrInvalidCredentials
	}

	return &biz.User{
		ID:        user.Id,
		Username:  user.Username,
		Email:     user.Email,
		AvatarURL: user.AvatarUrl,
		Status:    user.Status,
		LastSeen:  time.Unix(user.LastSeen, 0),
		CreatedAt: time.Unix(user.CreatedAt, 0),
		UpdatedAt: time.Now(),
	}, nil
}

// RoomRepoAdapter adapts the data layer RoomRepo to biz layer RoomRepo interface
type RoomRepoAdapter struct {
	repo RoomRepo
	log  *log.Helper
}

// NewRoomRepoAdapter creates a new room repository adapter
func NewRoomRepoAdapter(repo RoomRepo, logger log.Logger) biz.RoomRepo {
	return &RoomRepoAdapter{
		repo: repo,
		log:  log.NewHelper(log.With(logger, "module", "data/room_adapter")),
	}
}

// CreateRoom creates a new room
func (a *RoomRepoAdapter) CreateRoom(ctx context.Context, room *biz.Room) (*biz.Room, error) {
	// Convert biz.Room to chatV1.Room for data layer
	dataRoom := &chatV1.Room{
		Name:        room.Name,
		Description: room.Description,
		Type:        room.Type,
		CreatedBy:   room.CreatedBy,
	}

	createdRoom, err := a.repo.CreateRoom(ctx, dataRoom)
	if err != nil {
		return nil, err
	}

	return &biz.Room{
		ID:          createdRoom.Id,
		Name:        createdRoom.Name,
		Description: createdRoom.Description,
		Type:        createdRoom.Type,
		CreatedBy:   createdRoom.CreatedBy,
		CreatedAt:   time.Unix(createdRoom.CreatedAt, 0),
		UpdatedAt:   time.Now(),
	}, nil
}

// GetRoomByID retrieves a room by ID
func (a *RoomRepoAdapter) GetRoomByID(ctx context.Context, id int64) (*biz.Room, error) {
	room, err := a.repo.GetRoomByID(ctx, id)
	if err != nil {
		return nil, err
	}

	return &biz.Room{
		ID:          room.Id,
		Name:        room.Name,
		Description: room.Description,
		Type:        room.Type,
		CreatedBy:   room.CreatedBy,
		CreatedAt:   time.Unix(room.CreatedAt, 0),
		UpdatedAt:   time.Now(),
	}, nil
}

// ListUserRooms lists rooms for a user
func (a *RoomRepoAdapter) ListUserRooms(ctx context.Context, userID int64, limit, offset int32) ([]*biz.Room, int32, error) {
	rooms, total, err := a.repo.ListUserRooms(ctx, userID, limit, offset)
	if err != nil {
		return nil, 0, err
	}

	var bizRooms []*biz.Room
	for _, room := range rooms {
		bizRooms = append(bizRooms, &biz.Room{
			ID:          room.Id,
			Name:        room.Name,
			Description: room.Description,
			Type:        room.Type,
			CreatedBy:   room.CreatedBy,
			CreatedAt:   time.Unix(room.CreatedAt, 0),
			UpdatedAt:   time.Now(),
		})
	}

	return bizRooms, total, nil
}

// IsUserInRoom checks if user is a member of the room
func (a *RoomRepoAdapter) IsUserInRoom(ctx context.Context, roomID, userID int64) (bool, error) {
	return a.repo.IsUserInRoom(ctx, roomID, userID)
}

// JoinRoom adds a user to a room with specified role
func (a *RoomRepoAdapter) JoinRoom(ctx context.Context, roomID, userID int64, role string) error {
	return a.repo.JoinRoom(ctx, roomID, userID, role)
}

// LeaveRoom removes a user from a room
func (a *RoomRepoAdapter) LeaveRoom(ctx context.Context, roomID, userID int64) error {
	return a.repo.LeaveRoom(ctx, roomID, userID)
}

// GetRoomMembers retrieves all members of a room
func (a *RoomRepoAdapter) GetRoomMembers(ctx context.Context, roomID int64) ([]*biz.RoomMember, error) {
	// This method needs to be implemented in the data layer
	// For now, return empty slice
	return []*biz.RoomMember{}, nil
}

// ChatRepoAdapter adapts the data layer MessageRepo to biz layer ChatRepo interface
type ChatRepoAdapter struct {
	repo MessageRepo
	log  *log.Helper
}

// NewChatRepoAdapter creates a new chat repository adapter
func NewChatRepoAdapter(repo MessageRepo, logger log.Logger) biz.ChatRepo {
	return &ChatRepoAdapter{
		repo: repo,
		log:  log.NewHelper(log.With(logger, "module", "data/chat_adapter")),
	}
}

// SendMessage sends a message to the database
func (a *ChatRepoAdapter) SendMessage(ctx context.Context, message *biz.Message) (*biz.Message, error) {
	// Convert biz.Message to chatV1.Message for data layer
	dataMessage := &chatV1.Message{
		RoomId:   message.RoomID,
		UserId:   message.UserID,
		Username: message.Username,
		Content:  message.Content,
		Type:     message.Type,
	}

	sentMessage, err := a.repo.CreateMessage(ctx, dataMessage)
	if err != nil {
		return nil, err
	}

	// Convert back to biz.Message
	return &biz.Message{
		ID:        sentMessage.Id,
		RoomID:    sentMessage.RoomId,
		UserID:    sentMessage.UserId,
		Username:  sentMessage.Username,
		Content:   sentMessage.Content,
		Type:      sentMessage.Type,
		IsEdited:  sentMessage.IsEdited,
		CreatedAt: time.Unix(sentMessage.CreatedAt, 0),
	}, nil
}

// GetMessage retrieves a message by ID
func (a *ChatRepoAdapter) GetMessage(ctx context.Context, messageID int64) (*biz.Message, error) {
	message, err := a.repo.GetMessageByID(ctx, messageID)
	if err != nil {
		return nil, err
	}

	return &biz.Message{
		ID:        message.Id,
		RoomID:    message.RoomId,
		UserID:    message.UserId,
		Username:  message.Username,
		Content:   message.Content,
		Type:      message.Type,
		IsEdited:  message.IsEdited,
		CreatedAt: time.Unix(message.CreatedAt, 0),
	}, nil
}

// ListMessages lists messages in a room
func (a *ChatRepoAdapter) ListMessages(ctx context.Context, roomID int64, limit, offset int32) ([]*biz.Message, int32, error) {
	// Use GetMessages with beforeID = 0 to get latest messages
	messages, hasMore, err := a.repo.GetMessages(ctx, roomID, limit, 0)
	if err != nil {
		return nil, 0, err
	}

	var bizMessages []*biz.Message
	for _, msg := range messages {
		bizMessages = append(bizMessages, &biz.Message{
			ID:        msg.Id,
			RoomID:    msg.RoomId,
			UserID:    msg.UserId,
			Username:  msg.Username,
			Content:   msg.Content,
			Type:      msg.Type,
			IsEdited:  msg.IsEdited,
			CreatedAt: time.Unix(msg.CreatedAt, 0),
		})
	}

	// For now, return length as total (could be improved with separate count query)
	total := int32(len(bizMessages))
	if hasMore {
		total += 1 // Indicate there are more
	}

	return bizMessages, total, nil
}

// EditMessage edits a message content
func (a *ChatRepoAdapter) EditMessage(ctx context.Context, messageID int64, content string) error {
	// This method doesn't exist in MessageRepo, return not implemented
	return fmt.Errorf("edit message not implemented in data layer")
}

// DeleteMessage deletes a message
func (a *ChatRepoAdapter) DeleteMessage(ctx context.Context, messageID int64) error {
	// This method doesn't exist in MessageRepo, return not implemented
	return fmt.Errorf("delete message not implemented in data layer")
}

// MarkMessageAsRead marks a message as read by a user
func (a *ChatRepoAdapter) MarkMessageAsRead(ctx context.Context, messageID, userID int64) error {
	return a.repo.MarkAsRead(ctx, messageID, userID)
}

// GetUnreadMessages gets unread messages for a user in a room
func (a *ChatRepoAdapter) GetUnreadMessages(ctx context.Context, roomID, userID int64) ([]*biz.Message, error) {
	// The current MessageRepo doesn't have this method, let's use GetUnreadCount for now
	// and return empty slice - this needs to be implemented in the data layer
	_, err := a.repo.GetUnreadCount(ctx, userID, roomID)
	if err != nil {
		return nil, err
	}

	// Return empty slice for now - this method needs proper implementation
	return []*biz.Message{}, nil
}
