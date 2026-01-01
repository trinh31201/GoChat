package main

import (
	"flag"
	netHttp "net/http"
	"os"
	"time"

	"github.com/go-kratos/kratos/v2"
	"github.com/go-kratos/kratos/v2/log"
	"github.com/go-kratos/kratos/v2/middleware/recovery"
	"github.com/go-kratos/kratos/v2/transport/grpc"
	"github.com/go-kratos/kratos/v2/transport/http"
	"github.com/joho/godotenv"
	"google.golang.org/protobuf/types/known/durationpb"

	userV1 "github.com/yourusername/chat-app/api/user/v1"
	"github.com/yourusername/chat-app/internal/biz"
	"github.com/yourusername/chat-app/internal/conf"
	"github.com/yourusername/chat-app/internal/data"
	"github.com/yourusername/chat-app/internal/service"
)

var (
	Name     = "user-service"
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
	authConf := loadAuthConfig()

	// ============ 1. CONNECT ============
	dataData, cleanup, err := data.NewData(dataConf, logger)
	if err != nil {
		logHelper.Fatalf("failed to create data: %v", err)
	}
	defer cleanup()
	logHelper.Info("connected to database and redis")

	// ============ 2. CREATE COMPONENTS ============
	// Data layer
	userRepo := data.NewUserRepo(dataData, logger)
	bizUserRepo := data.NewUserRepoAdapter(userRepo, logger)

	// Biz layer
	jwtManager := biz.NewJWTTokenManagerFromConfig(authConf)
	passwordHasher := biz.NewBcryptPasswordHasher()
	userUseCase := biz.NewUserUseCase(bizUserRepo, jwtManager, passwordHasher, logger)

	// Service layer
	userService := service.NewUserService(userUseCase, logger)

	// ============ 3. CREATE SERVERS ============
	// gRPC server (for internal service-to-service calls)
	grpcServer := grpc.NewServer(
		grpc.Address(grpcAddr),
		grpc.Middleware(recovery.Recovery()),
	)
	userV1.RegisterUserServiceServer(grpcServer, userService)

	// HTTP server (for external REST API)
	httpServer := http.NewServer(
		http.Address(httpAddr),
		http.Middleware(recovery.Recovery()),
	)
	userV1.RegisterUserServiceHTTPServer(httpServer, userService)

	// Health endpoint for Docker health check
	httpServer.HandleFunc("/health", func(w netHttp.ResponseWriter, r *netHttp.Request) {
		w.WriteHeader(netHttp.StatusOK)
		w.Write([]byte("ok"))
	})

	// ============ 4. START ============
	app := kratos.New(
		kratos.ID(id),
		kratos.Name(Name),
		kratos.Version(Version),
		kratos.Logger(logger),
		kratos.Server(grpcServer, httpServer),
	)

	logHelper.Infof("User Service starting - HTTP %s, gRPC %s", httpAddr, grpcAddr)

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

	return &conf.Data{
		Database: &conf.Data_Database{
			Driver: "postgres",
			Source: dbURL,
		},
		Redis: &conf.Data_Redis{
			Addr: redisURL,
		},
	}
}

func loadAuthConfig() *conf.Auth {
	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		jwtSecret = "your-secret-key-change-in-production"
	}

	return &conf.Auth{
		JwtSecret: jwtSecret,
		JwtExpire: durationpb.New(24 * time.Hour), // 24 hours
	}
}
