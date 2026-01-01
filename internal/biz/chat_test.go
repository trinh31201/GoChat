package biz

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	chatV1 "github.com/yourusername/chat-app/api/chat/v1"
)

// ==================== Mock Chat Repository ====================

type MockChatRepo struct {
	messages    map[int64]*Message
	readReceipts map[int64]map[int64]bool // messageID -> userID -> read
	nextID      int64
	sendErr     error
	editErr     error
	deleteErr   error
}

func NewMockChatRepo() *MockChatRepo {
	return &MockChatRepo{
		messages:     make(map[int64]*Message),
		readReceipts: make(map[int64]map[int64]bool),
		nextID:       1,
	}
}

func (m *MockChatRepo) SendMessage(ctx context.Context, message *Message) (*Message, error) {
	if m.sendErr != nil {
		return nil, m.sendErr
	}
	message.ID = m.nextID
	message.CreatedAt = time.Now()
	m.nextID++
	m.messages[message.ID] = message
	return message, nil
}

func (m *MockChatRepo) GetMessage(ctx context.Context, messageID int64) (*Message, error) {
	if msg, ok := m.messages[messageID]; ok {
		return msg, nil
	}
	return nil, ErrMessageNotFound
}

func (m *MockChatRepo) ListMessages(ctx context.Context, roomID int64, limit, offset int32) ([]*Message, int32, error) {
	var messages []*Message
	for _, msg := range m.messages {
		if msg.RoomID == roomID {
			messages = append(messages, msg)
		}
	}
	return messages, int32(len(messages)), nil
}

func (m *MockChatRepo) EditMessage(ctx context.Context, messageID int64, content string) error {
	if m.editErr != nil {
		return m.editErr
	}
	if msg, ok := m.messages[messageID]; ok {
		msg.Content = content
		msg.IsEdited = true
		now := time.Now()
		msg.EditedAt = &now
		return nil
	}
	return ErrMessageNotFound
}

func (m *MockChatRepo) DeleteMessage(ctx context.Context, messageID int64) error {
	if m.deleteErr != nil {
		return m.deleteErr
	}
	if _, ok := m.messages[messageID]; ok {
		delete(m.messages, messageID)
		return nil
	}
	return ErrMessageNotFound
}

func (m *MockChatRepo) MarkMessageAsRead(ctx context.Context, messageID, userID int64) error {
	if m.readReceipts[messageID] == nil {
		m.readReceipts[messageID] = make(map[int64]bool)
	}
	m.readReceipts[messageID][userID] = true
	return nil
}

func (m *MockChatRepo) GetUnreadMessages(ctx context.Context, roomID, userID int64) ([]*Message, error) {
	var unread []*Message
	for _, msg := range m.messages {
		if msg.RoomID == roomID {
			if m.readReceipts[msg.ID] == nil || !m.readReceipts[msg.ID][userID] {
				unread = append(unread, msg)
			}
		}
	}
	return unread, nil
}

// Helper to add message directly
func (m *MockChatRepo) AddMessage(msg *Message) {
	m.messages[msg.ID] = msg
}

// ==================== Helper ====================

func newTestChatUseCase(chatRepo *MockChatRepo, roomRepo *MockRoomRepo, userRepo *MockUserRepo) *ChatUseCase {
	logger := log.NewStdLogger(io.Discard)
	return NewChatUseCase(chatRepo, roomRepo, userRepo, logger)
}

// ==================== SendMessage Tests ====================

func TestSendMessage_Success(t *testing.T) {
	// Arrange
	chatRepo := NewMockChatRepo()
	roomRepo := NewMockRoomRepo()
	userRepo := NewMockUserRepo()

	roomRepo.AddRoom(&Room{ID: 1, Name: "Test Room"})
	roomRepo.AddMember(1, 100)
	userRepo.usersById[100] = &User{ID: 100, Username: "sender"}

	uc := newTestChatUseCase(chatRepo, roomRepo, userRepo)

	req := &chatV1.SendMessageRequest{
		RoomId:  1,
		Content: "Hello, World!",
		Type:    "text",
	}

	// Act
	msg, err := uc.SendMessage(context.Background(), 100, req)

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if msg == nil {
		t.Fatal("expected message to be returned")
	}
	if msg.Content != "Hello, World!" {
		t.Errorf("expected content 'Hello, World!', got %s", msg.Content)
	}
	if msg.Username != "sender" {
		t.Errorf("expected username 'sender', got %s", msg.Username)
	}
}

func TestSendMessage_NotInRoom(t *testing.T) {
	// Arrange
	chatRepo := NewMockChatRepo()
	roomRepo := NewMockRoomRepo()
	userRepo := NewMockUserRepo()

	roomRepo.AddRoom(&Room{ID: 1, Name: "Test Room"})
	// User 100 is NOT a member
	userRepo.usersById[100] = &User{ID: 100}

	uc := newTestChatUseCase(chatRepo, roomRepo, userRepo)

	req := &chatV1.SendMessageRequest{
		RoomId:  1,
		Content: "Hello",
		Type:    "text",
	}

	// Act
	_, err := uc.SendMessage(context.Background(), 100, req)

	// Assert
	if err != ErrCannotSendMessage {
		t.Fatalf("expected ErrCannotSendMessage, got %v", err)
	}
}

func TestSendMessage_UserNotFound(t *testing.T) {
	// Arrange
	chatRepo := NewMockChatRepo()
	roomRepo := NewMockRoomRepo()
	userRepo := NewMockUserRepo()

	roomRepo.AddRoom(&Room{ID: 1, Name: "Test Room"})
	roomRepo.AddMember(1, 100)
	// User 100 not in userRepo

	uc := newTestChatUseCase(chatRepo, roomRepo, userRepo)

	req := &chatV1.SendMessageRequest{
		RoomId:  1,
		Content: "Hello",
		Type:    "text",
	}

	// Act
	_, err := uc.SendMessage(context.Background(), 100, req)

	// Assert
	if err != ErrUserNotFound {
		t.Fatalf("expected ErrUserNotFound, got %v", err)
	}
}

func TestSendMessage_EmptyContent(t *testing.T) {
	// Arrange
	chatRepo := NewMockChatRepo()
	roomRepo := NewMockRoomRepo()
	userRepo := NewMockUserRepo()

	uc := newTestChatUseCase(chatRepo, roomRepo, userRepo)

	req := &chatV1.SendMessageRequest{
		RoomId:  1,
		Content: "", // Empty
		Type:    "text",
	}

	// Act
	_, err := uc.SendMessage(context.Background(), 100, req)

	// Assert
	if err == nil {
		t.Fatal("expected error for empty content")
	}
}

func TestSendMessage_InvalidRoomID(t *testing.T) {
	// Arrange
	chatRepo := NewMockChatRepo()
	roomRepo := NewMockRoomRepo()
	userRepo := NewMockUserRepo()

	uc := newTestChatUseCase(chatRepo, roomRepo, userRepo)

	req := &chatV1.SendMessageRequest{
		RoomId:  0, // Invalid
		Content: "Hello",
		Type:    "text",
	}

	// Act
	_, err := uc.SendMessage(context.Background(), 100, req)

	// Assert
	if err == nil {
		t.Fatal("expected error for invalid room ID")
	}
}

func TestSendMessage_InvalidType(t *testing.T) {
	// Arrange
	chatRepo := NewMockChatRepo()
	roomRepo := NewMockRoomRepo()
	userRepo := NewMockUserRepo()

	uc := newTestChatUseCase(chatRepo, roomRepo, userRepo)

	req := &chatV1.SendMessageRequest{
		RoomId:  1,
		Content: "Hello",
		Type:    "invalid-type",
	}

	// Act
	_, err := uc.SendMessage(context.Background(), 100, req)

	// Assert
	if err == nil {
		t.Fatal("expected error for invalid message type")
	}
}

func TestSendMessage_ContentTooLong(t *testing.T) {
	// Arrange
	chatRepo := NewMockChatRepo()
	roomRepo := NewMockRoomRepo()
	userRepo := NewMockUserRepo()

	uc := newTestChatUseCase(chatRepo, roomRepo, userRepo)

	longContent := ""
	for i := 0; i < 4001; i++ {
		longContent += "a"
	}

	req := &chatV1.SendMessageRequest{
		RoomId:  1,
		Content: longContent,
		Type:    "text",
	}

	// Act
	_, err := uc.SendMessage(context.Background(), 100, req)

	// Assert
	if err == nil {
		t.Fatal("expected error for content too long")
	}
}

// ==================== GetMessage Tests ====================

func TestGetMessage_Success(t *testing.T) {
	// Arrange
	chatRepo := NewMockChatRepo()
	roomRepo := NewMockRoomRepo()
	userRepo := NewMockUserRepo()

	chatRepo.AddMessage(&Message{ID: 1, RoomID: 10, Content: "Test message"})
	roomRepo.AddRoom(&Room{ID: 10})
	roomRepo.AddMember(10, 100)

	uc := newTestChatUseCase(chatRepo, roomRepo, userRepo)

	// Act
	msg, err := uc.GetMessage(context.Background(), 100, 1)

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if msg.Content != "Test message" {
		t.Errorf("expected 'Test message', got %s", msg.Content)
	}
}

func TestGetMessage_NotFound(t *testing.T) {
	// Arrange
	chatRepo := NewMockChatRepo()
	roomRepo := NewMockRoomRepo()
	userRepo := NewMockUserRepo()

	uc := newTestChatUseCase(chatRepo, roomRepo, userRepo)

	// Act
	_, err := uc.GetMessage(context.Background(), 100, 999)

	// Assert
	if err != ErrMessageNotFound {
		t.Fatalf("expected ErrMessageNotFound, got %v", err)
	}
}

func TestGetMessage_AccessDenied(t *testing.T) {
	// Arrange
	chatRepo := NewMockChatRepo()
	roomRepo := NewMockRoomRepo()
	userRepo := NewMockUserRepo()

	chatRepo.AddMessage(&Message{ID: 1, RoomID: 10, Content: "Test"})
	roomRepo.AddRoom(&Room{ID: 10})
	// User 100 is NOT a member

	uc := newTestChatUseCase(chatRepo, roomRepo, userRepo)

	// Act
	_, err := uc.GetMessage(context.Background(), 100, 1)

	// Assert
	if err != ErrRoomAccessDenied {
		t.Fatalf("expected ErrRoomAccessDenied, got %v", err)
	}
}

// ==================== ListMessages Tests ====================

func TestListMessages_Success(t *testing.T) {
	// Arrange
	chatRepo := NewMockChatRepo()
	roomRepo := NewMockRoomRepo()
	userRepo := NewMockUserRepo()

	chatRepo.AddMessage(&Message{ID: 1, RoomID: 10, Content: "Msg 1"})
	chatRepo.AddMessage(&Message{ID: 2, RoomID: 10, Content: "Msg 2"})
	chatRepo.AddMessage(&Message{ID: 3, RoomID: 20, Content: "Other room"})
	roomRepo.AddRoom(&Room{ID: 10})
	roomRepo.AddMember(10, 100)

	uc := newTestChatUseCase(chatRepo, roomRepo, userRepo)

	// Act
	messages, total, err := uc.ListMessages(context.Background(), 100, 10, 50, 0)

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(messages) != 2 {
		t.Errorf("expected 2 messages, got %d", len(messages))
	}
	if total != 2 {
		t.Errorf("expected total 2, got %d", total)
	}
}

func TestListMessages_AccessDenied(t *testing.T) {
	// Arrange
	chatRepo := NewMockChatRepo()
	roomRepo := NewMockRoomRepo()
	userRepo := NewMockUserRepo()

	roomRepo.AddRoom(&Room{ID: 10})
	// User 100 is NOT a member

	uc := newTestChatUseCase(chatRepo, roomRepo, userRepo)

	// Act
	_, _, err := uc.ListMessages(context.Background(), 100, 10, 50, 0)

	// Assert
	if err != ErrRoomAccessDenied {
		t.Fatalf("expected ErrRoomAccessDenied, got %v", err)
	}
}

// ==================== EditMessage Tests ====================

func TestEditMessage_Success(t *testing.T) {
	// Arrange
	chatRepo := NewMockChatRepo()
	roomRepo := NewMockRoomRepo()
	userRepo := NewMockUserRepo()

	chatRepo.AddMessage(&Message{ID: 1, RoomID: 10, UserID: 100, Content: "Original"})

	uc := newTestChatUseCase(chatRepo, roomRepo, userRepo)

	// Act
	err := uc.EditMessage(context.Background(), 100, 1, "Edited content")

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Verify content changed
	msg, _ := chatRepo.GetMessage(context.Background(), 1)
	if msg.Content != "Edited content" {
		t.Errorf("expected 'Edited content', got %s", msg.Content)
	}
	if !msg.IsEdited {
		t.Error("expected IsEdited to be true")
	}
}

func TestEditMessage_NotAuthor(t *testing.T) {
	// Arrange
	chatRepo := NewMockChatRepo()
	roomRepo := NewMockRoomRepo()
	userRepo := NewMockUserRepo()

	chatRepo.AddMessage(&Message{ID: 1, RoomID: 10, UserID: 200, Content: "Original"}) // UserID 200

	uc := newTestChatUseCase(chatRepo, roomRepo, userRepo)

	// Act - User 100 tries to edit message by user 200
	err := uc.EditMessage(context.Background(), 100, 1, "Edited")

	// Assert
	if err == nil {
		t.Fatal("expected error for non-author edit")
	}
}

func TestEditMessage_EmptyContent(t *testing.T) {
	// Arrange
	chatRepo := NewMockChatRepo()
	roomRepo := NewMockRoomRepo()
	userRepo := NewMockUserRepo()

	chatRepo.AddMessage(&Message{ID: 1, RoomID: 10, UserID: 100, Content: "Original"})

	uc := newTestChatUseCase(chatRepo, roomRepo, userRepo)

	// Act
	err := uc.EditMessage(context.Background(), 100, 1, "")

	// Assert
	if err != ErrInvalidMessage {
		t.Fatalf("expected ErrInvalidMessage, got %v", err)
	}
}

func TestEditMessage_NotFound(t *testing.T) {
	// Arrange
	chatRepo := NewMockChatRepo()
	roomRepo := NewMockRoomRepo()
	userRepo := NewMockUserRepo()

	uc := newTestChatUseCase(chatRepo, roomRepo, userRepo)

	// Act
	err := uc.EditMessage(context.Background(), 100, 999, "Edited")

	// Assert
	if err != ErrMessageNotFound {
		t.Fatalf("expected ErrMessageNotFound, got %v", err)
	}
}

// ==================== DeleteMessage Tests ====================

func TestDeleteMessage_Success(t *testing.T) {
	// Arrange
	chatRepo := NewMockChatRepo()
	roomRepo := NewMockRoomRepo()
	userRepo := NewMockUserRepo()

	chatRepo.AddMessage(&Message{ID: 1, RoomID: 10, UserID: 100})

	uc := newTestChatUseCase(chatRepo, roomRepo, userRepo)

	// Act
	err := uc.DeleteMessage(context.Background(), 100, 1)

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Verify deleted
	_, err = chatRepo.GetMessage(context.Background(), 1)
	if err != ErrMessageNotFound {
		t.Error("expected message to be deleted")
	}
}

func TestDeleteMessage_NotAuthor(t *testing.T) {
	// Arrange
	chatRepo := NewMockChatRepo()
	roomRepo := NewMockRoomRepo()
	userRepo := NewMockUserRepo()

	chatRepo.AddMessage(&Message{ID: 1, RoomID: 10, UserID: 200}) // UserID 200

	uc := newTestChatUseCase(chatRepo, roomRepo, userRepo)

	// Act - User 100 tries to delete message by user 200
	err := uc.DeleteMessage(context.Background(), 100, 1)

	// Assert
	if err == nil {
		t.Fatal("expected error for non-author delete")
	}
}

func TestDeleteMessage_NotFound(t *testing.T) {
	// Arrange
	chatRepo := NewMockChatRepo()
	roomRepo := NewMockRoomRepo()
	userRepo := NewMockUserRepo()

	uc := newTestChatUseCase(chatRepo, roomRepo, userRepo)

	// Act
	err := uc.DeleteMessage(context.Background(), 100, 999)

	// Assert
	if err != ErrMessageNotFound {
		t.Fatalf("expected ErrMessageNotFound, got %v", err)
	}
}

// ==================== MarkMessageAsRead Tests ====================

func TestMarkMessageAsRead_Success(t *testing.T) {
	// Arrange
	chatRepo := NewMockChatRepo()
	roomRepo := NewMockRoomRepo()
	userRepo := NewMockUserRepo()

	chatRepo.AddMessage(&Message{ID: 1, RoomID: 10})
	roomRepo.AddRoom(&Room{ID: 10})
	roomRepo.AddMember(10, 100)

	uc := newTestChatUseCase(chatRepo, roomRepo, userRepo)

	// Act
	err := uc.MarkMessageAsRead(context.Background(), 100, 1)

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestMarkMessageAsRead_NotFound(t *testing.T) {
	// Arrange
	chatRepo := NewMockChatRepo()
	roomRepo := NewMockRoomRepo()
	userRepo := NewMockUserRepo()

	uc := newTestChatUseCase(chatRepo, roomRepo, userRepo)

	// Act
	err := uc.MarkMessageAsRead(context.Background(), 100, 999)

	// Assert
	if err != ErrMessageNotFound {
		t.Fatalf("expected ErrMessageNotFound, got %v", err)
	}
}

func TestMarkMessageAsRead_AccessDenied(t *testing.T) {
	// Arrange
	chatRepo := NewMockChatRepo()
	roomRepo := NewMockRoomRepo()
	userRepo := NewMockUserRepo()

	chatRepo.AddMessage(&Message{ID: 1, RoomID: 10})
	roomRepo.AddRoom(&Room{ID: 10})
	// User 100 is NOT a member

	uc := newTestChatUseCase(chatRepo, roomRepo, userRepo)

	// Act
	err := uc.MarkMessageAsRead(context.Background(), 100, 1)

	// Assert
	if err != ErrRoomAccessDenied {
		t.Fatalf("expected ErrRoomAccessDenied, got %v", err)
	}
}

// ==================== GetUnreadMessages Tests ====================

func TestGetUnreadMessages_Success(t *testing.T) {
	// Arrange
	chatRepo := NewMockChatRepo()
	roomRepo := NewMockRoomRepo()
	userRepo := NewMockUserRepo()

	chatRepo.AddMessage(&Message{ID: 1, RoomID: 10, Content: "Unread 1"})
	chatRepo.AddMessage(&Message{ID: 2, RoomID: 10, Content: "Unread 2"})
	roomRepo.AddRoom(&Room{ID: 10})
	roomRepo.AddMember(10, 100)

	uc := newTestChatUseCase(chatRepo, roomRepo, userRepo)

	// Act
	messages, err := uc.GetUnreadMessages(context.Background(), 100, 10)

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(messages) != 2 {
		t.Errorf("expected 2 unread messages, got %d", len(messages))
	}
}

func TestGetUnreadMessages_AccessDenied(t *testing.T) {
	// Arrange
	chatRepo := NewMockChatRepo()
	roomRepo := NewMockRoomRepo()
	userRepo := NewMockUserRepo()

	roomRepo.AddRoom(&Room{ID: 10})
	// User 100 is NOT a member

	uc := newTestChatUseCase(chatRepo, roomRepo, userRepo)

	// Act
	_, err := uc.GetUnreadMessages(context.Background(), 100, 10)

	// Assert
	if err != ErrRoomAccessDenied {
		t.Fatalf("expected ErrRoomAccessDenied, got %v", err)
	}
}
