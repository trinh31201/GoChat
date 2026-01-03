package main

import (
	"flag"
	"os"

	"github.com/go-kratos/kratos/v2"
	"github.com/go-kratos/kratos/v2/log"
	"github.com/go-kratos/kratos/v2/middleware/recovery"
	"github.com/go-kratos/kratos/v2/transport/grpc"
	"github.com/joho/godotenv"

	chatV1 "github.com/yourusername/chat-app/api/chat/v1"
	"github.com/yourusername/chat-app/internal/biz"
	"github.com/yourusername/chat-app/internal/client"
	"github.com/yourusername/chat-app/internal/conf"
	"github.com/yourusername/chat-app/internal/data"
	"github.com/yourusername/chat-app/internal/server"
	"github.com/yourusername/chat-app/internal/service"
)

var (
	Name     = "chat-service"
	Version  = "v1.0.0"
	httpAddr = ":8000"
	grpcAddr = ":9000"
	id, _    = os.Hostname()
)

func main() {
	flag.Parse()
	_ = godotenv.Load()

	// Logger
	logger := log.With(log.NewStdLogger(os.Stdout),
		"service.name", Name,
		"service.version", Version,
	)
	logHelper := log.NewHelper(logger)

	// Load config from environment
	dataConf := loadDataConfig()
	serverConf := loadServerConfig()

	// ============ 1. CONNECT ============
	// Connect to Database & Redis
	dataData, cleanup, err := data.NewData(dataConf, logger)
	if err != nil {
		logHelper.Fatalf("failed to create data: %v", err)
	}
	defer cleanup()
	logHelper.Info("connected to database and redis")

	// Connect to MinIO for file storage
	minioStorage := data.NewMinioStorage(dataConf, logger)

	// Connect to User Service via gRPC
	userServiceAddr := os.Getenv("USER_SERVICE_ADDR")
	if userServiceAddr == "" {
		userServiceAddr = "localhost:9000" // Default for local dev
	}

	userClient, err := client.NewUserClient(userServiceAddr, logger)
	if err != nil {
		logHelper.Fatalf("failed to connect to User Service at %s: %v", userServiceAddr, err)
	}
	defer func() {
		if err := userClient.Close(); err != nil {
			logHelper.Errorf("failed to close user client: %v", err)
		}
	}()
	logHelper.Infof("connected to User Service at %s", userServiceAddr)

	// ============ 2. CREATE COMPONENTS ============
	// Data layer
	roomRepo := data.NewRoomRepo(dataData, logger)
	bizRoomRepo := data.NewRoomRepoAdapter(roomRepo, logger)
	messageRepo := data.NewMessageRepo(dataData, logger)
	chatRepo := data.NewChatRepoAdapter(messageRepo, logger)
	userRepo := data.NewUserRepo(dataData, logger)
	bizUserRepo := data.NewUserRepoAdapter(userRepo, logger)

	// Biz layer
	roomUseCase := biz.NewRoomUseCase(bizRoomRepo, bizUserRepo, logger)
	chatUseCase := biz.NewChatUseCase(chatRepo, bizRoomRepo, bizUserRepo, logger)

	// Service layer
	roomService := service.NewRoomService(roomUseCase, logger)
	chatService := service.NewChatService(chatUseCase, logger)

	// ============ 3. CREATE SERVERS ============
	// gRPC server
	grpcServer := grpc.NewServer(
		grpc.Address(grpcAddr),
		grpc.Middleware(recovery.Recovery()),
	)
	chatV1.RegisterRoomServiceServer(grpcServer, roomService)
	chatV1.RegisterChatServiceServer(grpcServer, chatService)

	// HTTP server with WebSocket and file upload
	redisClient := data.NewRedisClient(dataData)
	httpServer := server.NewHTTPServerWithUserClient(serverConf, roomService, chatService, redisClient, userClient, minioStorage, logger)

	// ============ 4. START ============
	app := kratos.New(
		kratos.ID(id),
		kratos.Name(Name),
		kratos.Version(Version),
		kratos.Logger(logger),
		kratos.Server(grpcServer, httpServer),
	)

	logHelper.Infof("Chat Service starting - HTTP %s, gRPC %s", httpAddr, grpcAddr)

	if err := app.Run(); err != nil {
		logHelper.Fatalf("failed to run app: %v", err)
	}
}

func loadDataConfig() *conf.Data {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://chatuser:chatpass@localhost:5433/chatdb?sslmode=disable"
	}

	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		redisURL = "localhost:6381"
	}

	// MinIO config
	minioEndpoint := os.Getenv("MINIO_ENDPOINT")
	if minioEndpoint == "" {
		minioEndpoint = "localhost:9100"
	}
	minioAccessKey := os.Getenv("MINIO_ACCESS_KEY")
	if minioAccessKey == "" {
		minioAccessKey = "minioadmin"
	}
	minioSecretKey := os.Getenv("MINIO_SECRET_KEY")
	if minioSecretKey == "" {
		minioSecretKey = "minioadmin"
	}
	minioBucket := os.Getenv("MINIO_BUCKET")
	if minioBucket == "" {
		minioBucket = "chat-images"
	}
	minioPublicURL := os.Getenv("MINIO_PUBLIC_URL")
	if minioPublicURL == "" {
		minioPublicURL = "http://localhost:9100"
	}

	return &conf.Data{
		Database: &conf.Data_Database{
			Driver: "postgres",
			Source: dbURL,
		},
		Redis: &conf.Data_Redis{
			Addr: redisURL,
		},
		Minio: &conf.Data_Minio{
			Endpoint:   minioEndpoint,
			AccessKey:  minioAccessKey,
			SecretKey:  minioSecretKey,
			BucketName: minioBucket,
			UseSsl:     false,
			PublicUrl:  minioPublicURL,
		},
	}
}

func loadServerConfig() *conf.Server {
	return &conf.Server{
		Http: &conf.Server_HTTP{
			Addr: httpAddr,
		},
		Grpc: &conf.Server_GRPC{
			Addr: grpcAddr,
		},
	}
}
