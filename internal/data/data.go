package data

import (
	"context"
	"database/sql"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/google/wire"
	"github.com/redis/go-redis/v9"
	_ "github.com/lib/pq" // PostgreSQL driver
	"github.com/yourusername/chat-app/internal/conf"
)

// ProviderSet is data providers.
var ProviderSet = wire.NewSet(
	NewData,
	NewRedisClient,
	NewUserRepo,
	NewRoomRepo,
	NewMessageRepo,
	// Biz adapters
	NewUserRepoAdapter,
	NewRoomRepoAdapter,
	NewChatRepoAdapter,
)

// Data contains all data sources
type Data struct {
	db    *sql.DB
	redis *redis.Client
	log   *log.Helper
}

// NewData creates a new Data instance with database and Redis connections
func NewData(c *conf.Data, logger log.Logger) (*Data, func(), error) {
	helper := log.NewHelper(log.With(logger, "module", "data"))

	// Initialize database connection
	db, err := sql.Open(c.Database.Driver, c.Database.Source)
	if err != nil {
		return nil, nil, err
	}

	// Configure connection pool
	db.SetMaxIdleConns(int(c.Database.MaxIdleConns))
	db.SetMaxOpenConns(int(c.Database.MaxOpenConns))
	db.SetConnMaxLifetime(c.Database.ConnMaxLifetime.AsDuration())

	// Test database connection
	if err := db.Ping(); err != nil {
		return nil, nil, err
	}
	helper.Info("database connected")

	// Initialize Redis connection
	rdb := redis.NewClient(&redis.Options{
		Addr:         c.Redis.Addr,
		Password:     c.Redis.Password,
		DB:           int(c.Redis.Db),
		ReadTimeout:  c.Redis.ReadTimeout.AsDuration(),
		WriteTimeout: c.Redis.WriteTimeout.AsDuration(),
	})

	// Test Redis connection
	ctx := context.Background()
	if err := rdb.Ping(ctx).Err(); err != nil {
		helper.Warnf("redis connection failed: %v, continuing without cache", err)
		// Don't fail if Redis is not available, just warn
		rdb = nil
	} else {
		helper.Info("redis connected")
	}

	// Cleanup function
	cleanup := func() {
		if err := db.Close(); err != nil {
			helper.Error("failed to close database connection:", err)
		}
		if rdb != nil {
			if err := rdb.Close(); err != nil {
				helper.Error("failed to close redis connection:", err)
			}
		}
		helper.Info("data connections closed")
	}

	return &Data{
		db:    db,
		redis: rdb,
		log:   helper,
	}, cleanup, nil
}

// NewRedisClient provides redis client from Data
func NewRedisClient(d *Data) *redis.Client {
	return d.redis
}