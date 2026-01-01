package service

import (
	"context"

	"github.com/go-kratos/kratos/v2/log"

	chatV1 "github.com/yourusername/chat-app/api/chat/v1"
	"github.com/yourusername/chat-app/internal/biz"
	"github.com/yourusername/chat-app/internal/middleware"
)

// ChatService implements the chat/messaging service
type ChatService struct {
	chatV1.UnimplementedChatServiceServer

	uc  *biz.ChatUseCase
	log *log.Helper
}

// NewChatService creates a new chat service
func NewChatService(uc *biz.ChatUseCase, logger log.Logger) *ChatService {
	return &ChatService{
		uc:  uc,
		log: log.NewHelper(log.With(logger, "module", "service/chat")),
	}
}

// SendMessage sends a message to a room
func (s *ChatService) SendMessage(ctx context.Context, req *chatV1.SendMessageRequest) (*chatV1.Message, error) {
	// Get user ID from context (set by authentication middleware)
	userID, err := s.getUserIDFromContext(ctx)
	if err != nil {
		return nil, err
	}

	message, err := s.uc.SendMessage(ctx, userID, req)
	if err != nil {
		return nil, err
	}

	return &chatV1.Message{
		Id:        message.ID,
		RoomId:    message.RoomID,
		UserId:    message.UserID,
		Username:  message.Username,
		Content:   message.Content,
		Type:      message.Type,
		IsEdited:  message.IsEdited,
		CreatedAt: message.CreatedAt.Unix(),
	}, nil
}

// GetMessages retrieves messages for a room with pagination
func (s *ChatService) GetMessages(ctx context.Context, req *chatV1.GetMessagesRequest) (*chatV1.GetMessagesResponse, error) {
	// Get user ID from context (set by authentication middleware)
	userID, err := s.getUserIDFromContext(ctx)
	if err != nil {
		return nil, err
	}

	// Set default limit if not provided
	limit := req.Limit
	if limit == 0 {
		limit = 50 // Default to 50 messages
	}
	if limit > 100 {
		limit = 100 // Max 100 messages per request
	}

	// Calculate offset based on before_id
	// For now, we'll use simple offset-based pagination
	// TODO: Implement cursor-based pagination with before_id
	offset := int32(0)

	// Get messages from business logic layer
	messages, total, err := s.uc.ListMessages(ctx, userID, req.RoomId, limit, offset)
	if err != nil {
		s.log.Errorf("Failed to get messages for room %d: %v", req.RoomId, err)
		return nil, err
	}

	// Convert biz messages to proto messages
	protoMessages := make([]*chatV1.Message, len(messages))
	for i, msg := range messages {
		protoMessages[i] = &chatV1.Message{
			Id:        msg.ID,
			RoomId:    msg.RoomID,
			UserId:    msg.UserID,
			Username:  msg.Username,
			Content:   msg.Content,
			Type:      msg.Type,
			IsEdited:  msg.IsEdited,
			CreatedAt: msg.CreatedAt.Unix(),
		}
		if msg.EditedAt != nil {
			protoMessages[i].EditedAt = msg.EditedAt.Unix()
		}
	}

	// Determine if there are more messages
	hasMore := total > limit

	return &chatV1.GetMessagesResponse{
		Messages: protoMessages,
		HasMore:  hasMore,
	}, nil
}

// MarkAsRead marks a message as read
func (s *ChatService) MarkAsRead(ctx context.Context, req *chatV1.MarkAsReadRequest) (*chatV1.MarkAsReadResponse, error) {
	// Get user ID from context
	userID, err := s.getUserIDFromContext(ctx)
	if err != nil {
		return nil, err
	}

	err = s.uc.MarkMessageAsRead(ctx, userID, req.MessageId)
	if err != nil {
		s.log.Errorf("Failed to mark message %d as read: %v", req.MessageId, err)
		return nil, err
	}

	return &chatV1.MarkAsReadResponse{
		Success: true,
	}, nil
}

// getUserIDFromContext extracts user ID from request context
// This would be set by an authentication middleware
func (s *ChatService) getUserIDFromContext(ctx context.Context) (int64, error) {
	userID, ok := ctx.Value(middleware.UserIDKey).(int64)
	if !ok {
		return 0, biz.ErrUserNotFound
	}

	if userID <= 0 {
		return 0, biz.ErrUserNotFound
	}

	return userID, nil
}
