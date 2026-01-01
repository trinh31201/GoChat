package client

import (
	"context"
	"errors"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	userV1 "github.com/yourusername/chat-app/api/user/v1"
)

var ErrInvalidToken = errors.New("invalid token")

// UserClient is a gRPC client to call User Service
type UserClient struct {
	client userV1.UserServiceClient
	conn   *grpc.ClientConn
	log    *log.Helper
}

// NewUserClient creates a new User Service gRPC client
func NewUserClient(addr string, logger log.Logger) (*UserClient, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := grpc.DialContext(ctx, addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		return nil, err
	}

	return &UserClient{
		client: userV1.NewUserServiceClient(conn),
		conn:   conn,
		log:    log.NewHelper(log.With(logger, "module", "client/user")),
	}, nil
}

// Close closes the gRPC connection
func (c *UserClient) Close() error {
	return c.conn.Close()
}

// ValidateToken validates JWT token via User Service
// Returns userID, username, error
func (c *UserClient) ValidateToken(ctx context.Context, token string) (int64, string, error) {
	resp, err := c.client.ValidateToken(ctx, &userV1.ValidateTokenRequest{
		Token: token,
	})
	if err != nil {
		return 0, "", err
	}

	if !resp.Valid {
		return 0, "", ErrInvalidToken
	}

	return resp.UserId, resp.Username, nil
}

// GetUser gets user by ID
func (c *UserClient) GetUser(ctx context.Context, userID int64) (*userV1.User, error) {
	return c.client.GetUser(ctx, &userV1.GetUserRequest{Id: userID})
}

// GetUsersByIds gets multiple users by IDs (batch)
func (c *UserClient) GetUsersByIds(ctx context.Context, ids []int64) ([]*userV1.User, error) {
	resp, err := c.client.GetUsersByIds(ctx, &userV1.GetUsersByIdsRequest{Ids: ids})
	if err != nil {
		return nil, err
	}
	return resp.Users, nil
}
