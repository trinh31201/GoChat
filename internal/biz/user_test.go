package biz

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/go-kratos/kratos/v2/log"
	userV1 "github.com/yourusername/chat-app/api/user/v1"
)

// ==================== Mock Implementations ====================

// MockUserRepo is a mock implementation of UserRepo
type MockUserRepo struct {
	users         map[string]*User // email -> user
	usersByName   map[string]*User // username -> user
	usersById     map[int64]*User  // id -> user
	passwords     map[string]string // email -> password hash
	nextID        int64
	createError   error
	verifyError   error
}

func NewMockUserRepo() *MockUserRepo {
	return &MockUserRepo{
		users:       make(map[string]*User),
		usersByName: make(map[string]*User),
		usersById:   make(map[int64]*User),
		passwords:   make(map[string]string),
		nextID:      1,
	}
}

func (m *MockUserRepo) CreateUser(ctx context.Context, user *User, passwordHash string) (*User, error) {
	if m.createError != nil {
		return nil, m.createError
	}
	user.ID = m.nextID
	m.nextID++
	m.users[user.Email] = user
	m.usersByName[user.Username] = user
	m.usersById[user.ID] = user
	m.passwords[user.Email] = passwordHash
	return user, nil
}

func (m *MockUserRepo) GetUserByID(ctx context.Context, id int64) (*User, error) {
	if user, ok := m.usersById[id]; ok {
		return user, nil
	}
	return nil, ErrUserNotFound
}

func (m *MockUserRepo) GetUserByEmail(ctx context.Context, email string) (*User, error) {
	if user, ok := m.users[email]; ok {
		return user, nil
	}
	return nil, ErrUserNotFound
}

func (m *MockUserRepo) GetUserByUsername(ctx context.Context, username string) (*User, error) {
	if user, ok := m.usersByName[username]; ok {
		return user, nil
	}
	return nil, ErrUserNotFound
}

func (m *MockUserRepo) UpdateUserStatus(ctx context.Context, id int64, status string) error {
	if user, ok := m.usersById[id]; ok {
		user.Status = status
		return nil
	}
	return ErrUserNotFound
}

func (m *MockUserRepo) VerifyPassword(ctx context.Context, email, password string) (*User, error) {
	if m.verifyError != nil {
		return nil, m.verifyError
	}
	user, ok := m.users[email]
	if !ok {
		return nil, ErrUserNotFound
	}
	// In mock, we just check if password matches stored "hash" (which is plain text for simplicity)
	if m.passwords[email] != password {
		return nil, ErrInvalidCredentials
	}
	return user, nil
}

// MockTokenManager is a mock implementation of TokenManager
type MockTokenManager struct {
	generateError error
	validateError error
}

func (m *MockTokenManager) GenerateToken(userID int64, username string) (string, error) {
	if m.generateError != nil {
		return "", m.generateError
	}
	return "mock-token-12345", nil
}

func (m *MockTokenManager) ValidateToken(token string) (int64, string, error) {
	if m.validateError != nil {
		return 0, "", m.validateError
	}
	return 123, "testuser", nil
}

// MockPasswordHasher is a mock implementation of PasswordHasher
type MockPasswordHasher struct {
	hashError  error
	checkError error
}

func (m *MockPasswordHasher) HashPassword(password string) (string, error) {
	if m.hashError != nil {
		return "", m.hashError
	}
	// Return password as "hash" for simplicity in tests
	return password, nil
}

func (m *MockPasswordHasher) CheckPassword(password, hash string) error {
	if m.checkError != nil {
		return m.checkError
	}
	if password == hash {
		return nil
	}
	return errors.New("password mismatch")
}

// Helper to create UserUseCase with mocks
func newTestUserUseCase(repo *MockUserRepo, tokenMgr *MockTokenManager, hasher *MockPasswordHasher) *UserUseCase {
	logger := log.NewStdLogger(io.Discard) // Discard logs in tests
	return NewUserUseCase(repo, tokenMgr, hasher, logger)
}

// ==================== Register Tests ====================

func TestRegister_Success(t *testing.T) {
	// Arrange
	repo := NewMockUserRepo()
	tokenMgr := &MockTokenManager{}
	hasher := &MockPasswordHasher{}
	uc := newTestUserUseCase(repo, tokenMgr, hasher)

	req := &userV1.RegisterRequest{
		Username: "newuser",
		Email:    "new@example.com",
		Password: "password123",
	}

	// Act
	user, token, err := uc.Register(context.Background(), req)

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if user == nil {
		t.Fatal("expected user to be returned")
	}
	if user.Username != "newuser" {
		t.Errorf("expected username 'newuser', got %s", user.Username)
	}
	if token == "" {
		t.Fatal("expected token to be returned")
	}
}

func TestRegister_EmailAlreadyExists(t *testing.T) {
	// Arrange
	repo := NewMockUserRepo()
	// Pre-add existing user
	repo.users["existing@example.com"] = &User{ID: 1, Email: "existing@example.com"}

	tokenMgr := &MockTokenManager{}
	hasher := &MockPasswordHasher{}
	uc := newTestUserUseCase(repo, tokenMgr, hasher)

	req := &userV1.RegisterRequest{
		Username: "newuser",
		Email:    "existing@example.com", // Already exists
		Password: "password123",
	}

	// Act
	_, _, err := uc.Register(context.Background(), req)

	// Assert
	if err != ErrUserAlreadyExists {
		t.Fatalf("expected ErrUserAlreadyExists, got %v", err)
	}
}

func TestRegister_UsernameAlreadyExists(t *testing.T) {
	// Arrange
	repo := NewMockUserRepo()
	repo.usersByName["existinguser"] = &User{ID: 1, Username: "existinguser"}

	tokenMgr := &MockTokenManager{}
	hasher := &MockPasswordHasher{}
	uc := newTestUserUseCase(repo, tokenMgr, hasher)

	req := &userV1.RegisterRequest{
		Username: "existinguser", // Already exists
		Email:    "new@example.com",
		Password: "password123",
	}

	// Act
	_, _, err := uc.Register(context.Background(), req)

	// Assert
	if err != ErrUserAlreadyExists {
		t.Fatalf("expected ErrUserAlreadyExists, got %v", err)
	}
}

func TestRegister_UsernameTooShort(t *testing.T) {
	// Arrange
	repo := NewMockUserRepo()
	tokenMgr := &MockTokenManager{}
	hasher := &MockPasswordHasher{}
	uc := newTestUserUseCase(repo, tokenMgr, hasher)

	req := &userV1.RegisterRequest{
		Username: "ab", // Too short (< 3)
		Email:    "test@example.com",
		Password: "password123",
	}

	// Act
	_, _, err := uc.Register(context.Background(), req)

	// Assert
	if err == nil {
		t.Fatal("expected validation error for short username")
	}
}

func TestRegister_PasswordTooShort(t *testing.T) {
	// Arrange
	repo := NewMockUserRepo()
	tokenMgr := &MockTokenManager{}
	hasher := &MockPasswordHasher{}
	uc := newTestUserUseCase(repo, tokenMgr, hasher)

	req := &userV1.RegisterRequest{
		Username: "validuser",
		Email:    "test@example.com",
		Password: "12345", // Too short (< 6)
	}

	// Act
	_, _, err := uc.Register(context.Background(), req)

	// Assert
	if err == nil {
		t.Fatal("expected validation error for short password")
	}
}

func TestRegister_EmptyEmail(t *testing.T) {
	// Arrange
	repo := NewMockUserRepo()
	tokenMgr := &MockTokenManager{}
	hasher := &MockPasswordHasher{}
	uc := newTestUserUseCase(repo, tokenMgr, hasher)

	req := &userV1.RegisterRequest{
		Username: "validuser",
		Email:    "", // Empty
		Password: "password123",
	}

	// Act
	_, _, err := uc.Register(context.Background(), req)

	// Assert
	if err == nil {
		t.Fatal("expected validation error for empty email")
	}
}

// ==================== Login Tests ====================

func TestLogin_Success(t *testing.T) {
	// Arrange
	repo := NewMockUserRepo()
	// Create user first
	existingUser := &User{ID: 1, Username: "testuser", Email: "test@example.com"}
	repo.users["test@example.com"] = existingUser
	repo.usersById[1] = existingUser
	repo.passwords["test@example.com"] = "correctpassword"

	tokenMgr := &MockTokenManager{}
	hasher := &MockPasswordHasher{}
	uc := newTestUserUseCase(repo, tokenMgr, hasher)

	req := &userV1.LoginRequest{
		Email:    "test@example.com",
		Password: "correctpassword",
	}

	// Act
	user, token, err := uc.Login(context.Background(), req)

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if user == nil {
		t.Fatal("expected user to be returned")
	}
	if token == "" {
		t.Fatal("expected token to be returned")
	}
}

func TestLogin_WrongPassword(t *testing.T) {
	// Arrange
	repo := NewMockUserRepo()
	existingUser := &User{ID: 1, Username: "testuser", Email: "test@example.com"}
	repo.users["test@example.com"] = existingUser
	repo.passwords["test@example.com"] = "correctpassword"

	tokenMgr := &MockTokenManager{}
	hasher := &MockPasswordHasher{}
	uc := newTestUserUseCase(repo, tokenMgr, hasher)

	req := &userV1.LoginRequest{
		Email:    "test@example.com",
		Password: "wrongpassword",
	}

	// Act
	_, _, err := uc.Login(context.Background(), req)

	// Assert
	if err != ErrInvalidCredentials {
		t.Fatalf("expected ErrInvalidCredentials, got %v", err)
	}
}

func TestLogin_UserNotFound(t *testing.T) {
	// Arrange
	repo := NewMockUserRepo()
	repo.verifyError = ErrUserNotFound

	tokenMgr := &MockTokenManager{}
	hasher := &MockPasswordHasher{}
	uc := newTestUserUseCase(repo, tokenMgr, hasher)

	req := &userV1.LoginRequest{
		Email:    "nonexistent@example.com",
		Password: "password123",
	}

	// Act
	_, _, err := uc.Login(context.Background(), req)

	// Assert
	if err != ErrInvalidCredentials {
		t.Fatalf("expected ErrInvalidCredentials, got %v", err)
	}
}

func TestLogin_EmptyEmail(t *testing.T) {
	// Arrange
	repo := NewMockUserRepo()
	tokenMgr := &MockTokenManager{}
	hasher := &MockPasswordHasher{}
	uc := newTestUserUseCase(repo, tokenMgr, hasher)

	req := &userV1.LoginRequest{
		Email:    "",
		Password: "password123",
	}

	// Act
	_, _, err := uc.Login(context.Background(), req)

	// Assert
	if err != ErrInvalidCredentials {
		t.Fatalf("expected ErrInvalidCredentials, got %v", err)
	}
}

func TestLogin_EmptyPassword(t *testing.T) {
	// Arrange
	repo := NewMockUserRepo()
	tokenMgr := &MockTokenManager{}
	hasher := &MockPasswordHasher{}
	uc := newTestUserUseCase(repo, tokenMgr, hasher)

	req := &userV1.LoginRequest{
		Email:    "test@example.com",
		Password: "",
	}

	// Act
	_, _, err := uc.Login(context.Background(), req)

	// Assert
	if err != ErrInvalidCredentials {
		t.Fatalf("expected ErrInvalidCredentials, got %v", err)
	}
}

// ==================== UpdateStatus Tests ====================

func TestUpdateStatus_ValidStatus(t *testing.T) {
	// Arrange
	repo := NewMockUserRepo()
	repo.usersById[1] = &User{ID: 1, Username: "testuser", Status: "offline"}

	tokenMgr := &MockTokenManager{}
	hasher := &MockPasswordHasher{}
	uc := newTestUserUseCase(repo, tokenMgr, hasher)

	// Act & Assert - test all valid statuses
	validStatuses := []string{"online", "offline", "away"}
	for _, status := range validStatuses {
		err := uc.UpdateStatus(context.Background(), 1, status)
		if err != nil {
			t.Errorf("expected no error for status '%s', got %v", status, err)
		}
	}
}

func TestUpdateStatus_InvalidStatus(t *testing.T) {
	// Arrange
	repo := NewMockUserRepo()
	repo.usersById[1] = &User{ID: 1, Username: "testuser"}

	tokenMgr := &MockTokenManager{}
	hasher := &MockPasswordHasher{}
	uc := newTestUserUseCase(repo, tokenMgr, hasher)

	// Act
	err := uc.UpdateStatus(context.Background(), 1, "invalid-status")

	// Assert
	if err == nil {
		t.Fatal("expected error for invalid status")
	}
}

// ==================== GetUser Tests ====================

func TestGetUser_Success(t *testing.T) {
	// Arrange
	repo := NewMockUserRepo()
	expectedUser := &User{ID: 1, Username: "testuser", Email: "test@example.com"}
	repo.usersById[1] = expectedUser

	tokenMgr := &MockTokenManager{}
	hasher := &MockPasswordHasher{}
	uc := newTestUserUseCase(repo, tokenMgr, hasher)

	// Act
	user, err := uc.GetUser(context.Background(), 1)

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if user.ID != expectedUser.ID {
		t.Errorf("expected user ID %d, got %d", expectedUser.ID, user.ID)
	}
}

func TestGetUser_NotFound(t *testing.T) {
	// Arrange
	repo := NewMockUserRepo()
	tokenMgr := &MockTokenManager{}
	hasher := &MockPasswordHasher{}
	uc := newTestUserUseCase(repo, tokenMgr, hasher)

	// Act
	_, err := uc.GetUser(context.Background(), 999)

	// Assert
	if err != ErrUserNotFound {
		t.Fatalf("expected ErrUserNotFound, got %v", err)
	}
}
