package biz

import (
	"context"
	"errors"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	chatV1 "github.com/yourusername/chat-app/api/chat/v1"
)

var (
	ErrMessageNotFound   = errors.New("message not found")
	ErrInvalidMessage    = errors.New("invalid message")
	ErrCannotSendMessage = errors.New("cannot send message to this room")
)

// Message represents the message business entity
type Message struct {
	ID        int64
	RoomID    int64
	UserID    int64
	Username  string
	Content   string
	Type      string // text, image, file
	IsEdited  bool
	EditedAt  *time.Time
	CreatedAt time.Time
	// File attachment fields
	FileURL  string
	FileName string
	FileSize int64
	MimeType string
}

// MessageRead represents message read receipt
type MessageRead struct {
	ID        int64
	MessageID int64
	UserID    int64
	ReadAt    time.Time
}

// ChatRepo defines the interface for chat data access
type ChatRepo interface {
	SendMessage(ctx context.Context, message *Message) (*Message, error)
	GetMessage(ctx context.Context, messageID int64) (*Message, error)
	ListMessages(ctx context.Context, roomID int64, limit, offset int32) ([]*Message, int32, error)
	EditMessage(ctx context.Context, messageID int64, content string) error
	DeleteMessage(ctx context.Context, messageID int64) error
	MarkMessageAsRead(ctx context.Context, messageID, userID int64) error
	GetUnreadMessages(ctx context.Context, roomID, userID int64) ([]*Message, error)
}

// ChatUseCase contains chat business logic
type ChatUseCase struct {
	repo     ChatRepo
	roomRepo RoomRepo
	userRepo UserRepo
	log      *log.Helper
}

// NewChatUseCase creates a new chat use case
func NewChatUseCase(repo ChatRepo, roomRepo RoomRepo, userRepo UserRepo, logger log.Logger) *ChatUseCase {
	return &ChatUseCase{
		repo:     repo,
		roomRepo: roomRepo,
		userRepo: userRepo,
		log:      log.NewHelper(log.With(logger, "module", "biz/chat")),
	}
}

// SendMessage sends a message to a room
func (uc *ChatUseCase) SendMessage(ctx context.Context, userID int64, req *chatV1.SendMessageRequest) (*Message, error) {
	uc.log.Infof("Sending message from user %d to room %d", userID, req.RoomId)

	// Validate input
	if err := uc.validateSendMessageRequest(req); err != nil {
		return nil, err
	}

	// Check if user is a member of the room
	isMember, err := uc.roomRepo.IsUserInRoom(ctx, req.RoomId, userID)
	if err != nil {
		return nil, err
	}
	if !isMember {
		return nil, ErrCannotSendMessage
	}

	// Get user info for username
	user, err := uc.userRepo.GetUserByID(ctx, userID)
	if err != nil {
		return nil, ErrUserNotFound
	}

	// Create message
	message := &Message{
		RoomID:   req.RoomId,
		UserID:   userID,
		Username: user.Username,
		Content:  req.Content,
		Type:     req.Type,
		FileURL:  req.FileUrl,
		FileName: req.FileName,
		FileSize: req.FileSize,
		MimeType: req.MimeType,
	}

	sentMessage, err := uc.repo.SendMessage(ctx, message)
	if err != nil {
		uc.log.Errorf("Failed to send message: %v", err)
		return nil, err
	}

	uc.log.Infof("Message sent successfully: id=%d, room=%d, user=%d", sentMessage.ID, sentMessage.RoomID, sentMessage.UserID)
	return sentMessage, nil
}

// GetMessage retrieves a message if user has access
func (uc *ChatUseCase) GetMessage(ctx context.Context, userID, messageID int64) (*Message, error) {
	message, err := uc.repo.GetMessage(ctx, messageID)
	if err != nil {
		return nil, ErrMessageNotFound
	}

	// Check if user has access to the room
	isMember, err := uc.roomRepo.IsUserInRoom(ctx, message.RoomID, userID)
	if err != nil {
		return nil, err
	}
	if !isMember {
		return nil, ErrRoomAccessDenied
	}

	return message, nil
}

// ListMessages lists messages in a room
func (uc *ChatUseCase) ListMessages(ctx context.Context, userID, roomID int64, limit, offset int32) ([]*Message, int32, error) {
	// Check if user has access to the room
	isMember, err := uc.roomRepo.IsUserInRoom(ctx, roomID, userID)
	if err != nil {
		return nil, 0, err
	}
	if !isMember {
		return nil, 0, ErrRoomAccessDenied
	}

	return uc.repo.ListMessages(ctx, roomID, limit, offset)
}

// EditMessage edits a message (only by the author)
func (uc *ChatUseCase) EditMessage(ctx context.Context, userID, messageID int64, content string) error {
	// Get message
	message, err := uc.repo.GetMessage(ctx, messageID)
	if err != nil {
		return ErrMessageNotFound
	}

	// Check if user is the author
	if message.UserID != userID {
		return errors.New("can only edit your own messages")
	}

	// Validate content
	if content == "" {
		return ErrInvalidMessage
	}

	return uc.repo.EditMessage(ctx, messageID, content)
}

// DeleteMessage deletes a message (only by the author or room admin)
func (uc *ChatUseCase) DeleteMessage(ctx context.Context, userID, messageID int64) error {
	// Get message
	message, err := uc.repo.GetMessage(ctx, messageID)
	if err != nil {
		return ErrMessageNotFound
	}

	// Check if user is the author
	if message.UserID != userID {
		// TODO: Also allow room admins to delete messages
		return errors.New("can only delete your own messages")
	}

	return uc.repo.DeleteMessage(ctx, messageID)
}

// MarkMessageAsRead marks a message as read by a user
func (uc *ChatUseCase) MarkMessageAsRead(ctx context.Context, userID, messageID int64) error {
	// Get message to verify room access
	message, err := uc.repo.GetMessage(ctx, messageID)
	if err != nil {
		return ErrMessageNotFound
	}

	// Check if user has access to the room
	isMember, err := uc.roomRepo.IsUserInRoom(ctx, message.RoomID, userID)
	if err != nil {
		return err
	}
	if !isMember {
		return ErrRoomAccessDenied
	}

	return uc.repo.MarkMessageAsRead(ctx, messageID, userID)
}

// GetUnreadMessages gets unread messages for a user in a room
func (uc *ChatUseCase) GetUnreadMessages(ctx context.Context, userID, roomID int64) ([]*Message, error) {
	// Check if user has access to the room
	isMember, err := uc.roomRepo.IsUserInRoom(ctx, roomID, userID)
	if err != nil {
		return nil, err
	}
	if !isMember {
		return nil, ErrRoomAccessDenied
	}

	return uc.repo.GetUnreadMessages(ctx, roomID, userID)
}

// validateSendMessageRequest validates message sending input
func (uc *ChatUseCase) validateSendMessageRequest(req *chatV1.SendMessageRequest) error {
	if req.RoomId <= 0 {
		return errors.New("invalid room ID")
	}
	if req.Type == "" {
		req.Type = "text" // Default to text
	}
	validTypes := map[string]bool{
		"text":  true,
		"image": true,
		"file":  true,
	}
	if !validTypes[req.Type] {
		return errors.New("invalid message type")
	}

	// For file/image messages, file_url is required
	if req.Type == "image" || req.Type == "file" {
		if req.FileUrl == "" {
			return errors.New("file_url is required for image/file messages")
		}
		if req.FileName == "" {
			return errors.New("file_name is required for image/file messages")
		}
	} else {
		// For text messages, content is required
		if req.Content == "" {
			return errors.New("message content cannot be empty")
		}
	}

	if len(req.Content) > 4000 {
		return errors.New("message content too long")
	}
	return nil
}
