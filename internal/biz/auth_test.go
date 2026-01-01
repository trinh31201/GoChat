package biz

import (
	"testing"
	"time"
)

// ==================== JWT Token Manager Tests ====================

func TestGenerateToken_Success(t *testing.T) {
	// Arrange
	tm := NewJWTTokenManager("test-secret", time.Hour)
	userID := int64(123)
	username := "testuser"

	// Act
	token, err := tm.GenerateToken(userID, username)

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if token == "" {
		t.Fatal("expected token to be non-empty")
	}
}

func TestValidateToken_Success(t *testing.T) {
	// Arrange
	tm := NewJWTTokenManager("test-secret", time.Hour)
	userID := int64(456)
	username := "john_doe"

	token, _ := tm.GenerateToken(userID, username)

	// Act
	gotUserID, gotUsername, err := tm.ValidateToken(token)

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if gotUserID != userID {
		t.Errorf("expected userID %d, got %d", userID, gotUserID)
	}
	if gotUsername != username {
		t.Errorf("expected username %s, got %s", username, gotUsername)
	}
}

func TestValidateToken_InvalidToken(t *testing.T) {
	// Arrange
	tm := NewJWTTokenManager("test-secret", time.Hour)

	// Act
	_, _, err := tm.ValidateToken("invalid-token-string")

	// Assert
	if err == nil {
		t.Fatal("expected error for invalid token, got nil")
	}
}

func TestValidateToken_WrongSecret(t *testing.T) {
	// Arrange
	tm1 := NewJWTTokenManager("secret-one", time.Hour)
	tm2 := NewJWTTokenManager("secret-two", time.Hour)

	// Generate token with first secret
	token, _ := tm1.GenerateToken(123, "user")

	// Act - validate with different secret
	_, _, err := tm2.ValidateToken(token)

	// Assert
	if err == nil {
		t.Fatal("expected error when validating with wrong secret, got nil")
	}
}

func TestValidateToken_ExpiredToken(t *testing.T) {
	// Arrange - token expires in 1 millisecond
	tm := NewJWTTokenManager("test-secret", time.Millisecond)

	token, _ := tm.GenerateToken(123, "user")

	// Wait for token to expire
	time.Sleep(10 * time.Millisecond)

	// Act
	_, _, err := tm.ValidateToken(token)

	// Assert
	if err == nil {
		t.Fatal("expected error for expired token, got nil")
	}
}

func TestValidateToken_EmptyToken(t *testing.T) {
	// Arrange
	tm := NewJWTTokenManager("test-secret", time.Hour)

	// Act
	_, _, err := tm.ValidateToken("")

	// Assert
	if err == nil {
		t.Fatal("expected error for empty token, got nil")
	}
}

// ==================== Password Hasher Tests ====================

func TestHashPassword_Success(t *testing.T) {
	// Arrange
	hasher := NewBcryptPasswordHasher()
	password := "mySecurePassword123"

	// Act
	hash, err := hasher.HashPassword(password)

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if hash == "" {
		t.Fatal("expected hash to be non-empty")
	}
	if hash == password {
		t.Fatal("hash should not equal plain password")
	}
}

func TestHashPassword_DifferentHashesForSamePassword(t *testing.T) {
	// Arrange
	hasher := NewBcryptPasswordHasher()
	password := "samePassword"

	// Act
	hash1, _ := hasher.HashPassword(password)
	hash2, _ := hasher.HashPassword(password)

	// Assert - bcrypt generates different hashes each time (due to salt)
	if hash1 == hash2 {
		t.Fatal("expected different hashes for same password (bcrypt uses salt)")
	}
}

func TestCheckPassword_CorrectPassword(t *testing.T) {
	// Arrange
	hasher := NewBcryptPasswordHasher()
	password := "correctPassword"
	hash, _ := hasher.HashPassword(password)

	// Act
	err := hasher.CheckPassword(password, hash)

	// Assert
	if err != nil {
		t.Fatalf("expected no error for correct password, got %v", err)
	}
}

func TestCheckPassword_WrongPassword(t *testing.T) {
	// Arrange
	hasher := NewBcryptPasswordHasher()
	password := "correctPassword"
	hash, _ := hasher.HashPassword(password)

	// Act
	err := hasher.CheckPassword("wrongPassword", hash)

	// Assert
	if err == nil {
		t.Fatal("expected error for wrong password, got nil")
	}
}

func TestCheckPassword_EmptyPassword(t *testing.T) {
	// Arrange
	hasher := NewBcryptPasswordHasher()
	hash, _ := hasher.HashPassword("somePassword")

	// Act
	err := hasher.CheckPassword("", hash)

	// Assert
	if err == nil {
		t.Fatal("expected error for empty password, got nil")
	}
}

func TestCheckPassword_InvalidHash(t *testing.T) {
	// Arrange
	hasher := NewBcryptPasswordHasher()

	// Act
	err := hasher.CheckPassword("password", "not-a-valid-hash")

	// Assert
	if err == nil {
		t.Fatal("expected error for invalid hash, got nil")
	}
}
