package biz

import (
	"context"
	"errors"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	userV1 "github.com/yourusername/chat-app/api/user/v1"
)

var (
	ErrUserNotFound      = errors.New("user not found")
	ErrUserAlreadyExists = errors.New("user already exists")
	ErrInvalidCredentials = errors.New("invalid credentials")
)

// User represents the user business entity
type User struct {
	ID        int64
	Username  string
	Email     string
	AvatarURL string
	Status    string
	LastSeen  time.Time
	CreatedAt time.Time
	UpdatedAt time.Time
}

// UserRepo defines the interface for user data access
type UserRepo interface {
	CreateUser(ctx context.Context, user *User, passwordHash string) (*User, error)
	GetUserByID(ctx context.Context, id int64) (*User, error)
	GetUserByEmail(ctx context.Context, email string) (*User, error)
	GetUserByUsername(ctx context.Context, username string) (*User, error)
	UpdateUserStatus(ctx context.Context, id int64, status string) error
	VerifyPassword(ctx context.Context, email, password string) (*User, error)
}

// TokenManager defines the interface for JWT token operations
type TokenManager interface {
	GenerateToken(userID int64, username string) (string, error)
	ValidateToken(token string) (userID int64, username string, err error)
}

// PasswordHasher defines the interface for password hashing
type PasswordHasher interface {
	HashPassword(password string) (string, error)
	CheckPassword(password, hash string) error
}

// UserUseCase contains user business logic
type UserUseCase struct {
	repo         UserRepo
	tokenManager TokenManager
	hasher       PasswordHasher
	log          *log.Helper
}

// NewUserUseCase creates a new user use case
func NewUserUseCase(repo UserRepo, tokenManager TokenManager, hasher PasswordHasher, logger log.Logger) *UserUseCase {
	return &UserUseCase{
		repo:         repo,
		tokenManager: tokenManager,
		hasher:       hasher,
		log:          log.NewHelper(log.With(logger, "module", "biz/user")),
	}
}

// Register creates a new user account
func (uc *UserUseCase) Register(ctx context.Context, req *userV1.RegisterRequest) (*User, string, error) {
	uc.log.Infof("Registering user: %s", req.Username)

	// Validate input
	if err := uc.validateRegisterRequest(req); err != nil {
		return nil, "", err
	}

	// Check if user already exists
	if _, err := uc.repo.GetUserByEmail(ctx, req.Email); err == nil {
		return nil, "", ErrUserAlreadyExists
	}
	if _, err := uc.repo.GetUserByUsername(ctx, req.Username); err == nil {
		return nil, "", ErrUserAlreadyExists
	}

	// Hash password
	passwordHash, err := uc.hasher.HashPassword(req.Password)
	if err != nil {
		uc.log.Errorf("Failed to hash password: %v", err)
		return nil, "", err
	}

	// Create user
	user := &User{
		Username: req.Username,
		Email:    req.Email,
		Status:   "offline",
	}

	createdUser, err := uc.repo.CreateUser(ctx, user, passwordHash)
	if err != nil {
		uc.log.Errorf("Failed to create user: %v", err)
		return nil, "", err
	}

	// Generate token
	token, err := uc.tokenManager.GenerateToken(createdUser.ID, createdUser.Username)
	if err != nil {
		uc.log.Errorf("Failed to generate token: %v", err)
		return nil, "", err
	}

	uc.log.Infof("User registered successfully: id=%d, username=%s", createdUser.ID, createdUser.Username)
	return createdUser, token, nil
}

// Login authenticates a user
func (uc *UserUseCase) Login(ctx context.Context, req *userV1.LoginRequest) (*User, string, error) {
	uc.log.Infof("Login attempt for email: %s", req.Email)

	// Validate input
	if req.Email == "" || req.Password == "" {
		return nil, "", ErrInvalidCredentials
	}

	// Verify credentials
	user, err := uc.repo.VerifyPassword(ctx, req.Email, req.Password)
	if err != nil {
		uc.log.Warnf("Login failed for email %s: %v", req.Email, err)
		return nil, "", ErrInvalidCredentials
	}

	// Update user status to online
	if err := uc.repo.UpdateUserStatus(ctx, user.ID, "online"); err != nil {
		uc.log.Warnf("Failed to update user status: %v", err)
	}

	// Generate token
	token, err := uc.tokenManager.GenerateToken(user.ID, user.Username)
	if err != nil {
		uc.log.Errorf("Failed to generate token: %v", err)
		return nil, "", err
	}

	uc.log.Infof("User logged in successfully: id=%d, username=%s", user.ID, user.Username)
	return user, token, nil
}

// GetUser retrieves user information
func (uc *UserUseCase) GetUser(ctx context.Context, userID int64) (*User, error) {
	user, err := uc.repo.GetUserByID(ctx, userID)
	if err != nil {
		return nil, ErrUserNotFound
	}
	return user, nil
}

// UpdateStatus updates user online status
func (uc *UserUseCase) UpdateStatus(ctx context.Context, userID int64, status string) error {
	// Validate status
	validStatuses := map[string]bool{
		"online":  true,
		"offline": true,
		"away":    true,
	}

	if !validStatuses[status] {
		return errors.New("invalid status")
	}

	return uc.repo.UpdateUserStatus(ctx, userID, status)
}

// ValidateToken validates JWT token and returns user info
func (uc *UserUseCase) ValidateToken(ctx context.Context, token string) (int64, string, error) {
	return uc.tokenManager.ValidateToken(token)
}

// GetUsersByIds returns multiple users by their IDs
func (uc *UserUseCase) GetUsersByIds(ctx context.Context, ids []int64) ([]*User, error) {
	var users []*User
	for _, id := range ids {
		user, err := uc.repo.GetUserByID(ctx, id)
		if err != nil {
			continue // Skip users not found
		}
		users = append(users, user)
	}
	return users, nil
}

// validateRegisterRequest validates registration input
func (uc *UserUseCase) validateRegisterRequest(req *userV1.RegisterRequest) error {
	if req.Username == "" || len(req.Username) < 3 || len(req.Username) > 50 {
		return errors.New("username must be between 3 and 50 characters")
	}
	
	if req.Email == "" || len(req.Email) > 100 {
		return errors.New("invalid email")
	}
	
	if req.Password == "" || len(req.Password) < 6 {
		return errors.New("password must be at least 6 characters")
	}
	
	return nil
}