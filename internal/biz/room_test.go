package biz

import (
	"context"
	"io"
	"testing"

	"github.com/go-kratos/kratos/v2/log"
	chatV1 "github.com/yourusername/chat-app/api/chat/v1"
)

// ==================== Mock Room Repository ====================

type MockRoomRepo struct {
	rooms      map[int64]*Room
	members    map[int64]map[int64]bool // roomID -> userID -> isMember
	nextID     int64
	createErr  error
	joinErr    error
	leaveErr   error
}

func NewMockRoomRepo() *MockRoomRepo {
	return &MockRoomRepo{
		rooms:   make(map[int64]*Room),
		members: make(map[int64]map[int64]bool),
		nextID:  1,
	}
}

func (m *MockRoomRepo) CreateRoom(ctx context.Context, room *Room) (*Room, error) {
	if m.createErr != nil {
		return nil, m.createErr
	}
	room.ID = m.nextID
	m.nextID++
	m.rooms[room.ID] = room

	// Auto-add creator as member
	if m.members[room.ID] == nil {
		m.members[room.ID] = make(map[int64]bool)
	}
	m.members[room.ID][room.CreatedBy] = true

	return room, nil
}

func (m *MockRoomRepo) GetRoomByID(ctx context.Context, id int64) (*Room, error) {
	if room, ok := m.rooms[id]; ok {
		return room, nil
	}
	return nil, ErrRoomNotFound
}

func (m *MockRoomRepo) ListUserRooms(ctx context.Context, userID int64, limit, offset int32) ([]*Room, int32, error) {
	var rooms []*Room
	for roomID, members := range m.members {
		if members[userID] {
			rooms = append(rooms, m.rooms[roomID])
		}
	}
	return rooms, int32(len(rooms)), nil
}

func (m *MockRoomRepo) IsUserInRoom(ctx context.Context, roomID, userID int64) (bool, error) {
	if members, ok := m.members[roomID]; ok {
		return members[userID], nil
	}
	return false, nil
}

func (m *MockRoomRepo) JoinRoom(ctx context.Context, roomID, userID int64, role string) error {
	if m.joinErr != nil {
		return m.joinErr
	}
	if m.members[roomID] == nil {
		m.members[roomID] = make(map[int64]bool)
	}
	m.members[roomID][userID] = true
	return nil
}

func (m *MockRoomRepo) LeaveRoom(ctx context.Context, roomID, userID int64) error {
	if m.leaveErr != nil {
		return m.leaveErr
	}
	if members, ok := m.members[roomID]; ok {
		delete(members, userID)
	}
	return nil
}

func (m *MockRoomRepo) GetRoomMembers(ctx context.Context, roomID int64) ([]*RoomMember, error) {
	var members []*RoomMember
	if roomMembers, ok := m.members[roomID]; ok {
		for userID := range roomMembers {
			members = append(members, &RoomMember{
				RoomID: roomID,
				UserID: userID,
				Role:   "member",
			})
		}
	}
	return members, nil
}

// Helper to add a room directly for testing
func (m *MockRoomRepo) AddRoom(room *Room) {
	m.rooms[room.ID] = room
	if m.members[room.ID] == nil {
		m.members[room.ID] = make(map[int64]bool)
	}
}

// Helper to add member directly for testing
func (m *MockRoomRepo) AddMember(roomID, userID int64) {
	if m.members[roomID] == nil {
		m.members[roomID] = make(map[int64]bool)
	}
	m.members[roomID][userID] = true
}

// ==================== Helper ====================

func newTestRoomUseCase(roomRepo *MockRoomRepo, userRepo *MockUserRepo) *RoomUseCase {
	logger := log.NewStdLogger(io.Discard)
	return NewRoomUseCase(roomRepo, userRepo, logger)
}

// ==================== CreateRoom Tests ====================

func TestCreateRoom_Success(t *testing.T) {
	// Arrange
	roomRepo := NewMockRoomRepo()
	userRepo := NewMockUserRepo()
	userRepo.usersById[1] = &User{ID: 1, Username: "creator"}

	uc := newTestRoomUseCase(roomRepo, userRepo)

	req := &chatV1.CreateRoomRequest{
		Name:        "Test Room",
		Description: "A test room",
		Type:        "public",
	}

	// Act
	room, err := uc.CreateRoom(context.Background(), 1, req)

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if room == nil {
		t.Fatal("expected room to be returned")
	}
	if room.Name != "Test Room" {
		t.Errorf("expected room name 'Test Room', got %s", room.Name)
	}
	if room.CreatedBy != 1 {
		t.Errorf("expected created_by 1, got %d", room.CreatedBy)
	}
}

func TestCreateRoom_EmptyName(t *testing.T) {
	// Arrange
	roomRepo := NewMockRoomRepo()
	userRepo := NewMockUserRepo()
	userRepo.usersById[1] = &User{ID: 1}

	uc := newTestRoomUseCase(roomRepo, userRepo)

	req := &chatV1.CreateRoomRequest{
		Name: "", // Empty
		Type: "public",
	}

	// Act
	_, err := uc.CreateRoom(context.Background(), 1, req)

	// Assert
	if err == nil {
		t.Fatal("expected error for empty room name")
	}
}

func TestCreateRoom_NameTooLong(t *testing.T) {
	// Arrange
	roomRepo := NewMockRoomRepo()
	userRepo := NewMockUserRepo()
	userRepo.usersById[1] = &User{ID: 1}

	uc := newTestRoomUseCase(roomRepo, userRepo)

	longName := ""
	for i := 0; i < 101; i++ {
		longName += "a"
	}

	req := &chatV1.CreateRoomRequest{
		Name: longName,
		Type: "public",
	}

	// Act
	_, err := uc.CreateRoom(context.Background(), 1, req)

	// Assert
	if err == nil {
		t.Fatal("expected error for room name too long")
	}
}

func TestCreateRoom_InvalidType(t *testing.T) {
	// Arrange
	roomRepo := NewMockRoomRepo()
	userRepo := NewMockUserRepo()
	userRepo.usersById[1] = &User{ID: 1}

	uc := newTestRoomUseCase(roomRepo, userRepo)

	req := &chatV1.CreateRoomRequest{
		Name: "Test Room",
		Type: "invalid-type",
	}

	// Act
	_, err := uc.CreateRoom(context.Background(), 1, req)

	// Assert
	if err == nil {
		t.Fatal("expected error for invalid room type")
	}
}

func TestCreateRoom_UserNotFound(t *testing.T) {
	// Arrange
	roomRepo := NewMockRoomRepo()
	userRepo := NewMockUserRepo() // No users

	uc := newTestRoomUseCase(roomRepo, userRepo)

	req := &chatV1.CreateRoomRequest{
		Name: "Test Room",
		Type: "public",
	}

	// Act
	_, err := uc.CreateRoom(context.Background(), 999, req)

	// Assert
	if err != ErrUserNotFound {
		t.Fatalf("expected ErrUserNotFound, got %v", err)
	}
}

// ==================== GetRoom Tests ====================

func TestGetRoom_Success(t *testing.T) {
	// Arrange
	roomRepo := NewMockRoomRepo()
	userRepo := NewMockUserRepo()

	room := &Room{ID: 1, Name: "Test Room", Type: "public"}
	roomRepo.AddRoom(room)
	roomRepo.AddMember(1, 100) // User 100 is member

	uc := newTestRoomUseCase(roomRepo, userRepo)

	// Act
	result, err := uc.GetRoom(context.Background(), 100, 1)

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result.ID != 1 {
		t.Errorf("expected room ID 1, got %d", result.ID)
	}
}

func TestGetRoom_AccessDenied(t *testing.T) {
	// Arrange
	roomRepo := NewMockRoomRepo()
	userRepo := NewMockUserRepo()

	room := &Room{ID: 1, Name: "Test Room", Type: "public"}
	roomRepo.AddRoom(room)
	// User 100 is NOT a member

	uc := newTestRoomUseCase(roomRepo, userRepo)

	// Act
	_, err := uc.GetRoom(context.Background(), 100, 1)

	// Assert
	if err != ErrRoomAccessDenied {
		t.Fatalf("expected ErrRoomAccessDenied, got %v", err)
	}
}

func TestGetRoom_NotFound(t *testing.T) {
	// Arrange
	roomRepo := NewMockRoomRepo()
	userRepo := NewMockUserRepo()
	roomRepo.AddMember(999, 100) // User is "member" but room doesn't exist

	uc := newTestRoomUseCase(roomRepo, userRepo)

	// Act
	_, err := uc.GetRoom(context.Background(), 100, 999)

	// Assert
	if err != ErrRoomNotFound {
		t.Fatalf("expected ErrRoomNotFound, got %v", err)
	}
}

// ==================== JoinRoom Tests ====================

func TestJoinRoom_Success(t *testing.T) {
	// Arrange
	roomRepo := NewMockRoomRepo()
	userRepo := NewMockUserRepo()

	room := &Room{ID: 1, Name: "Public Room", Type: "public"}
	roomRepo.AddRoom(room)
	userRepo.usersById[100] = &User{ID: 100, Username: "joiner"}

	uc := newTestRoomUseCase(roomRepo, userRepo)

	// Act
	result, err := uc.JoinRoom(context.Background(), 100, 1)

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result.ID != 1 {
		t.Errorf("expected room ID 1, got %d", result.ID)
	}

	// Verify user is now member
	isMember, _ := roomRepo.IsUserInRoom(context.Background(), 1, 100)
	if !isMember {
		t.Error("expected user to be member after joining")
	}
}

func TestJoinRoom_AlreadyMember(t *testing.T) {
	// Arrange
	roomRepo := NewMockRoomRepo()
	userRepo := NewMockUserRepo()

	room := &Room{ID: 1, Name: "Test Room", Type: "public"}
	roomRepo.AddRoom(room)
	roomRepo.AddMember(1, 100) // Already a member
	userRepo.usersById[100] = &User{ID: 100}

	uc := newTestRoomUseCase(roomRepo, userRepo)

	// Act
	_, err := uc.JoinRoom(context.Background(), 100, 1)

	// Assert
	if err != ErrUserAlreadyInRoom {
		t.Fatalf("expected ErrUserAlreadyInRoom, got %v", err)
	}
}

func TestJoinRoom_PrivateRoom(t *testing.T) {
	// Arrange
	roomRepo := NewMockRoomRepo()
	userRepo := NewMockUserRepo()

	room := &Room{ID: 1, Name: "Private Room", Type: "private"}
	roomRepo.AddRoom(room)
	userRepo.usersById[100] = &User{ID: 100}

	uc := newTestRoomUseCase(roomRepo, userRepo)

	// Act
	_, err := uc.JoinRoom(context.Background(), 100, 1)

	// Assert
	if err != ErrCannotJoinPrivateRoom {
		t.Fatalf("expected ErrCannotJoinPrivateRoom, got %v", err)
	}
}

func TestJoinRoom_RoomNotFound(t *testing.T) {
	// Arrange
	roomRepo := NewMockRoomRepo()
	userRepo := NewMockUserRepo()
	userRepo.usersById[100] = &User{ID: 100}

	uc := newTestRoomUseCase(roomRepo, userRepo)

	// Act
	_, err := uc.JoinRoom(context.Background(), 100, 999) // Room doesn't exist

	// Assert
	if err != ErrRoomNotFound {
		t.Fatalf("expected ErrRoomNotFound, got %v", err)
	}
}

func TestJoinRoom_UserNotFound(t *testing.T) {
	// Arrange
	roomRepo := NewMockRoomRepo()
	userRepo := NewMockUserRepo() // No users

	room := &Room{ID: 1, Name: "Test Room", Type: "public"}
	roomRepo.AddRoom(room)

	uc := newTestRoomUseCase(roomRepo, userRepo)

	// Act
	_, err := uc.JoinRoom(context.Background(), 999, 1) // User doesn't exist

	// Assert
	if err != ErrUserNotFound {
		t.Fatalf("expected ErrUserNotFound, got %v", err)
	}
}

// ==================== LeaveRoom Tests ====================

func TestLeaveRoom_Success(t *testing.T) {
	// Arrange
	roomRepo := NewMockRoomRepo()
	userRepo := NewMockUserRepo()

	room := &Room{ID: 1, Name: "Test Room"}
	roomRepo.AddRoom(room)
	roomRepo.AddMember(1, 100) // User is member

	uc := newTestRoomUseCase(roomRepo, userRepo)

	// Act
	err := uc.LeaveRoom(context.Background(), 100, 1)

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Verify user is no longer member
	isMember, _ := roomRepo.IsUserInRoom(context.Background(), 1, 100)
	if isMember {
		t.Error("expected user to not be member after leaving")
	}
}

func TestLeaveRoom_NotInRoom(t *testing.T) {
	// Arrange
	roomRepo := NewMockRoomRepo()
	userRepo := NewMockUserRepo()

	room := &Room{ID: 1, Name: "Test Room"}
	roomRepo.AddRoom(room)
	// User 100 is NOT a member

	uc := newTestRoomUseCase(roomRepo, userRepo)

	// Act
	err := uc.LeaveRoom(context.Background(), 100, 1)

	// Assert
	if err != ErrUserNotInRoom {
		t.Fatalf("expected ErrUserNotInRoom, got %v", err)
	}
}

// ==================== GetRoomMembers Tests ====================

func TestGetRoomMembers_Success(t *testing.T) {
	// Arrange
	roomRepo := NewMockRoomRepo()
	userRepo := NewMockUserRepo()

	room := &Room{ID: 1, Name: "Test Room"}
	roomRepo.AddRoom(room)
	roomRepo.AddMember(1, 100)
	roomRepo.AddMember(1, 101)
	roomRepo.AddMember(1, 102)

	uc := newTestRoomUseCase(roomRepo, userRepo)

	// Act
	members, err := uc.GetRoomMembers(context.Background(), 100, 1)

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(members) != 3 {
		t.Errorf("expected 3 members, got %d", len(members))
	}
}

func TestGetRoomMembers_AccessDenied(t *testing.T) {
	// Arrange
	roomRepo := NewMockRoomRepo()
	userRepo := NewMockUserRepo()

	room := &Room{ID: 1, Name: "Test Room"}
	roomRepo.AddRoom(room)
	roomRepo.AddMember(1, 101) // Only user 101 is member

	uc := newTestRoomUseCase(roomRepo, userRepo)

	// Act - user 100 is NOT a member
	_, err := uc.GetRoomMembers(context.Background(), 100, 1)

	// Assert
	if err != ErrRoomAccessDenied {
		t.Fatalf("expected ErrRoomAccessDenied, got %v", err)
	}
}

// ==================== ListUserRooms Tests ====================

func TestListUserRooms_Success(t *testing.T) {
	// Arrange
	roomRepo := NewMockRoomRepo()
	userRepo := NewMockUserRepo()

	room1 := &Room{ID: 1, Name: "Room 1"}
	room2 := &Room{ID: 2, Name: "Room 2"}
	roomRepo.AddRoom(room1)
	roomRepo.AddRoom(room2)
	roomRepo.AddMember(1, 100)
	roomRepo.AddMember(2, 100)

	uc := newTestRoomUseCase(roomRepo, userRepo)

	// Act
	rooms, total, err := uc.ListUserRooms(context.Background(), 100, 10, 0)

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(rooms) != 2 {
		t.Errorf("expected 2 rooms, got %d", len(rooms))
	}
	if total != 2 {
		t.Errorf("expected total 2, got %d", total)
	}
}

func TestListUserRooms_Empty(t *testing.T) {
	// Arrange
	roomRepo := NewMockRoomRepo()
	userRepo := NewMockUserRepo()

	uc := newTestRoomUseCase(roomRepo, userRepo)

	// Act - user 100 has no rooms
	rooms, total, err := uc.ListUserRooms(context.Background(), 100, 10, 0)

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(rooms) != 0 {
		t.Errorf("expected 0 rooms, got %d", len(rooms))
	}
	if total != 0 {
		t.Errorf("expected total 0, got %d", total)
	}
}
