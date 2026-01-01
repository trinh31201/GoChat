package middleware

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-kratos/kratos/v2/middleware"
	"github.com/go-kratos/kratos/v2/transport"
	"github.com/golang-jwt/jwt/v4"
	"github.com/yourusername/chat-app/internal/client"
	"github.com/yourusername/chat-app/internal/conf"
)

// Context key types to avoid collisions
type contextKey string

const (
	UserIDKey   contextKey = "user_id"
	UsernameKey contextKey = "username"
	EmailKey    contextKey = "email"
)

// JWT claims structure
type JWTClaims struct {
	UserID   int64  `json:"user_id"`
	Username string `json:"username"`
	Email    string `json:"email"`
	jwt.RegisteredClaims
}

// JWTAuth creates JWT authentication middleware
func JWTAuth(c *conf.Auth) middleware.Middleware {
	return func(handler middleware.Handler) middleware.Handler {
		return func(ctx context.Context, req interface{}) (interface{}, error) {
			// Skip authentication for certain endpoints
			if tr, ok := transport.FromServerContext(ctx); ok {
				operation := tr.Operation()

				// Public endpoints that don't require authentication
				publicEndpoints := []string{
					"/api.user.v1.UserService/Register",
					"/api.user.v1.UserService/Login",
				}

				for _, endpoint := range publicEndpoints {
					if strings.Contains(operation, endpoint) {
						return handler(ctx, req)
					}
				}

				// Extract token from Authorization header
				var token string
				if header := tr.RequestHeader(); header != nil {
					auth := header.Get("Authorization")
					if auth != "" && strings.HasPrefix(auth, "Bearer ") {
						token = auth[7:] // Remove "Bearer " prefix
					}
				}

				if token == "" {
					return nil, fmt.Errorf("authentication required: missing token")
				}

				// Validate JWT token
				claims := &JWTClaims{}
				jwtToken, err := jwt.ParseWithClaims(token, claims, func(token *jwt.Token) (interface{}, error) {
					if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
						return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
					}
					return []byte(c.JwtSecret), nil
				})

				if err != nil {
					return nil, fmt.Errorf("authentication failed: %v", err)
				}

				if !jwtToken.Valid {
					return nil, fmt.Errorf("authentication failed: invalid token")
				}

				// Add user ID to context
				ctx = context.WithValue(ctx, UserIDKey, claims.UserID)
				ctx = context.WithValue(ctx, UsernameKey, claims.Username)
				ctx = context.WithValue(ctx, EmailKey, claims.Email)
			}

			return handler(ctx, req)
		}
	}
}

// JWTAuthWithUserClient creates JWT authentication middleware using User Service
// This is used by Chat Service to validate tokens via gRPC call to User Service
func JWTAuthWithUserClient(userClient *client.UserClient) middleware.Middleware {
	return func(handler middleware.Handler) middleware.Handler {
		return func(ctx context.Context, req interface{}) (interface{}, error) {
			if tr, ok := transport.FromServerContext(ctx); ok {
				// Extract token from Authorization header
				var token string
				if header := tr.RequestHeader(); header != nil {
					auth := header.Get("Authorization")
					if auth != "" && strings.HasPrefix(auth, "Bearer ") {
						token = auth[7:] // Remove "Bearer " prefix
					}
				}

				if token == "" {
					return nil, fmt.Errorf("authentication required: missing token")
				}

				// Validate token via User Service gRPC call
				userID, username, err := userClient.ValidateToken(ctx, token)
				if err != nil {
					return nil, fmt.Errorf("authentication failed: %v", err)
				}

				// Add user info to context
				ctx = context.WithValue(ctx, UserIDKey, userID)
				ctx = context.WithValue(ctx, UsernameKey, username)
			}

			return handler(ctx, req)
		}
	}
}
