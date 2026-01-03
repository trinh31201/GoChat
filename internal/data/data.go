package data

import (
	"context"
	"database/sql"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/google/wire"
	_ "github.com/lib/pq" // PostgreSQL driver
	"github.com/redis/go-redis/v9"
	"github.com/yourusername/chat-app/internal/conf"
	"github.com/yourusername/chat-app/internal/storage"
)

// ProviderSet is data providers.
var ProviderSet = wire.NewSet(
	NewData,
	NewRedisClient,
	NewMinioStorage,
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

// NewMinioStorage creates a new MinIO storage client
func NewMinioStorage(c *conf.Data, logger log.Logger) *storage.MinioStorage {
	helper := log.NewHelper(log.With(logger, "module", "data/minio"))

	if c.Minio == nil {
		helper.Warn("MinIO configuration not found, file uploads disabled")
		return nil
	}

	cfg := &storage.MinioConfig{
		Endpoint:   c.Minio.Endpoint,
		AccessKey:  c.Minio.AccessKey,
		SecretKey:  c.Minio.SecretKey,
		BucketName: c.Minio.BucketName,
		UseSSL:     c.Minio.UseSsl,
		PublicURL:  c.Minio.PublicUrl,
	}

	store, err := storage.NewMinioStorage(cfg, logger)
	if err != nil {
		helper.Warnf("Failed to initialize MinIO: %v, file uploads disabled", err)
		return nil
	}

	helper.Info("MinIO storage connected")
	return store
}
