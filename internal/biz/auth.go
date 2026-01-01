package biz

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v4"
	"github.com/yourusername/chat-app/internal/conf"
	"golang.org/x/crypto/bcrypt"
)

// JWTTokenManager implements TokenManager interface
type JWTTokenManager struct {
	secret []byte
	expire time.Duration
}

// Claims represents JWT token claims
type Claims struct {
	UserID   int64  `json:"user_id"`
	Username string `json:"username"`
	jwt.RegisteredClaims
}

// NewJWTTokenManager creates a new JWT token manager
func NewJWTTokenManager(secret string, expire time.Duration) *JWTTokenManager {
	return &JWTTokenManager{
		secret: []byte(secret),
		expire: expire,
	}
}

// NewJWTTokenManagerFromConfig creates a new JWT token manager from auth config
func NewJWTTokenManagerFromConfig(auth *conf.Auth) *JWTTokenManager {
	return &JWTTokenManager{
		secret: []byte(auth.JwtSecret),
		expire: auth.JwtExpire.AsDuration(),
	}
}

// GenerateToken generates a JWT token for a user
func (tm *JWTTokenManager) GenerateToken(userID int64, username string) (string, error) {
	now := time.Now()
	claims := Claims{
		UserID:   userID,
		Username: username,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(tm.expire)),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString(tm.secret)
	if err != nil {
		return "", fmt.Errorf("failed to sign token: %w", err)
	}

	return tokenString, nil
}

// ValidateToken validates a JWT token and returns user info
func (tm *JWTTokenManager) ValidateToken(tokenString string) (userID int64, username string, err error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return tm.secret, nil
	})

	if err != nil {
		return 0, "", fmt.Errorf("invalid token: %w", err)
	}

	if claims, ok := token.Claims.(*Claims); ok && token.Valid {
		return claims.UserID, claims.Username, nil
	}

	return 0, "", fmt.Errorf("invalid token claims")
}

// BcryptPasswordHasher implements PasswordHasher interface
type BcryptPasswordHasher struct {
	cost int
}

// NewBcryptPasswordHasher creates a new bcrypt password hasher
func NewBcryptPasswordHasher() *BcryptPasswordHasher {
	return &BcryptPasswordHasher{
		cost: bcrypt.DefaultCost,
	}
}

// HashPassword hashes a password using bcrypt
func (h *BcryptPasswordHasher) HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), h.cost)
	if err != nil {
		return "", fmt.Errorf("failed to hash password: %w", err)
	}
	return string(hash), nil
}

// CheckPassword verifies a password against its hash
func (h *BcryptPasswordHasher) CheckPassword(password, hash string) error {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	if err != nil {
		return fmt.Errorf("password verification failed: %w", err)
	}
	return nil
}
