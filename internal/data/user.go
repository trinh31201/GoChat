package data

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	userV1 "github.com/yourusername/chat-app/api/user/v1"
)

// UserRepo defines the interface for user data operations
type UserRepo interface {
	CreateUser(ctx context.Context, user *userV1.User, password string) (*userV1.User, error)
	GetUserByEmail(ctx context.Context, email string) (*userV1.User, string, error)       // returns user and password hash
	GetUserByUsername(ctx context.Context, username string) (*userV1.User, string, error) // returns user and password hash
	GetUserByID(ctx context.Context, id int64) (*userV1.User, error)
	UpdateUserStatus(ctx context.Context, userID int64, status string) error
	UpdateLastSeen(ctx context.Context, userID int64) error
}

type userRepo struct {
	data *Data
	log  *log.Helper
}

// NewUserRepo creates a new user repository
func NewUserRepo(data *Data, logger log.Logger) UserRepo {
	return &userRepo{
		data: data,
		log:  log.NewHelper(log.With(logger, "module", "data/user")),
	}
}

func (r *userRepo) CreateUser(ctx context.Context, user *userV1.User, password string) (*userV1.User, error) {
	// Password is already hashed by the biz layer
	hashedPassword := []byte(password)

	// Insert user into database
	query := `
		INSERT INTO users (username, email, password_hash, avatar_url, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, created_at`

	now := time.Now()
	user.Status = "offline"
	user.CreatedAt = now.Unix()

	var createdAt time.Time
	err := r.data.db.QueryRowContext(ctx, query,
		user.Username,
		user.Email,
		string(hashedPassword),
		user.AvatarUrl,
		user.Status,
		now,
		now,
	).Scan(&user.Id, &createdAt)

	if err == nil {
		user.CreatedAt = createdAt.Unix()
		user.LastSeen = createdAt.Unix() // Set last_seen to created_at initially
	}

	if err != nil {
		if err.Error() == "pq: duplicate key value violates unique constraint" {
			return nil, fmt.Errorf("user with this email or username already exists")
		}
		return nil, fmt.Errorf("failed to create user: %w", err)
	}

	r.log.Infof("created user: id=%d, username=%s", user.Id, user.Username)
	return user, nil
}

func (r *userRepo) GetUserByEmail(ctx context.Context, email string) (*userV1.User, string, error) {
	user := &userV1.User{}
	var passwordHash string
	var lastSeen, createdAt time.Time

	query := `
		SELECT id, username, email, password_hash, avatar_url, status, last_seen, created_at
		FROM users
		WHERE email = $1`

	err := r.data.db.QueryRowContext(ctx, query, email).Scan(
		&user.Id,
		&user.Username,
		&user.Email,
		&passwordHash,
		&user.AvatarUrl,
		&user.Status,
		&lastSeen,
		&createdAt,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, "", fmt.Errorf("user not found")
		}
		return nil, "", fmt.Errorf("failed to get user: %w", err)
	}

	user.LastSeen = lastSeen.Unix()
	user.CreatedAt = createdAt.Unix()

	return user, passwordHash, nil
}

func (r *userRepo) GetUserByUsername(ctx context.Context, username string) (*userV1.User, string, error) {
	user := &userV1.User{}
	var passwordHash string
	var lastSeen, createdAt time.Time

	query := `
		SELECT id, username, email, password_hash, avatar_url, status, last_seen, created_at
		FROM users
		WHERE username = $1`

	err := r.data.db.QueryRowContext(ctx, query, username).Scan(
		&user.Id,
		&user.Username,
		&user.Email,
		&passwordHash,
		&user.AvatarUrl,
		&user.Status,
		&lastSeen,
		&createdAt,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, "", fmt.Errorf("user not found")
		}
		return nil, "", fmt.Errorf("failed to get user: %w", err)
	}

	user.LastSeen = lastSeen.Unix()
	user.CreatedAt = createdAt.Unix()

	return user, passwordHash, nil
}

func (r *userRepo) GetUserByID(ctx context.Context, id int64) (*userV1.User, error) {
	user := &userV1.User{}
	var lastSeen, createdAt time.Time

	query := `
		SELECT id, username, email, avatar_url, status, last_seen, created_at
		FROM users
		WHERE id = $1`

	err := r.data.db.QueryRowContext(ctx, query, id).Scan(
		&user.Id,
		&user.Username,
		&user.Email,
		&user.AvatarUrl,
		&user.Status,
		&lastSeen,
		&createdAt,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("user not found")
		}
		return nil, fmt.Errorf("failed to get user: %w", err)
	}

	user.LastSeen = lastSeen.Unix()
	user.CreatedAt = createdAt.Unix()

	return user, nil
}

func (r *userRepo) UpdateUserStatus(ctx context.Context, userID int64, status string) error {
	query := `UPDATE users SET status = $1, updated_at = $2 WHERE id = $3`

	result, err := r.data.db.ExecContext(ctx, query, status, time.Now(), userID)
	if err != nil {
		return fmt.Errorf("failed to update user status: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return fmt.Errorf("user not found")
	}

	// Update status in Redis cache if available
	if r.data.redis != nil {
		key := fmt.Sprintf("user:status:%d", userID)
		r.data.redis.Set(ctx, key, status, 5*time.Minute)
	}

	r.log.Infof("updated user status: id=%d, status=%s", userID, status)
	return nil
}

func (r *userRepo) UpdateLastSeen(ctx context.Context, userID int64) error {
	query := `UPDATE users SET last_seen = $1 WHERE id = $2`

	_, err := r.data.db.ExecContext(ctx, query, time.Now(), userID)
	if err != nil {
		return fmt.Errorf("failed to update last seen: %w", err)
	}

	return nil
}
