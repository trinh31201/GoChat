package server

import (
	netHttp "net/http"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/go-kratos/kratos/v2/middleware/recovery"
	"github.com/go-kratos/kratos/v2/transport/grpc"
	"github.com/go-kratos/kratos/v2/transport/http"
	"github.com/google/wire"

	chatV1 "github.com/yourusername/chat-app/api/chat/v1"
	userV1 "github.com/yourusername/chat-app/api/user/v1"
	"github.com/yourusername/chat-app/internal/conf"
	"github.com/yourusername/chat-app/internal/middleware"
	"github.com/yourusername/chat-app/internal/service"
)

// ProviderSet is server providers.
var ProviderSet = wire.NewSet(NewGRPCServer, NewHTTPServer)

// NewGRPCServer new a gRPC server.
func NewGRPCServer(
	c *conf.Server,
	auth *conf.Auth,
	userService *service.UserService,
	roomService *service.RoomService,
	chatService *service.ChatService,
	logger log.Logger,
) *grpc.Server {
	var opts = []grpc.ServerOption{
		grpc.Middleware(
			recovery.Recovery(),
			middleware.JWTAuth(auth),
		),
	}

	if c.Grpc.Network != "" {
		opts = append(opts, grpc.Network(c.Grpc.Network))
	}
	if c.Grpc.Addr != "" {
		opts = append(opts, grpc.Address(c.Grpc.Addr))
	}
	if c.Grpc.Timeout != nil {
		opts = append(opts, grpc.Timeout(c.Grpc.Timeout.AsDuration()))
	}

	srv := grpc.NewServer(opts...)

	// Register services
	userV1.RegisterUserServiceServer(srv, userService)
	chatV1.RegisterRoomServiceServer(srv, roomService)
	chatV1.RegisterChatServiceServer(srv, chatService)

	return srv
}

// NewHTTPServer new an HTTP server.
func NewHTTPServer(
	c *conf.Server,
	auth *conf.Auth,
	userService *service.UserService,
	roomService *service.RoomService,
	chatService *service.ChatService,
	logger log.Logger,
) *http.Server {
	var opts = []http.ServerOption{
		http.Middleware(
			recovery.Recovery(),
			middleware.JWTAuth(auth),
		),
	}

	if c.Http.Network != "" {
		opts = append(opts, http.Network(c.Http.Network))
	}
	if c.Http.Addr != "" {
		opts = append(opts, http.Address(c.Http.Addr))
	}
	if c.Http.Timeout != nil {
		opts = append(opts, http.Timeout(c.Http.Timeout.AsDuration()))
	}

	srv := http.NewServer(opts...)

	// Create and start WebSocket hub
	hub := NewHub(chatService, roomService, logger)
	go hub.Run()

	// Register HTTP handlers
	userV1.RegisterUserServiceHTTPServer(srv, userService)
	chatV1.RegisterRoomServiceHTTPServer(srv, roomService)
	chatV1.RegisterChatServiceHTTPServer(srv, chatService)

	// Register WebSocket endpoint
	srv.HandleFunc("/ws", HandleWebSocket(hub, auth.JwtSecret))

	// Serve static files for testing UI
	webDir := netHttp.Dir("./web")
	srv.HandlePrefix("/web/", netHttp.StripPrefix("/web/", netHttp.FileServer(webDir)))

	// Serve OpenAPI spec for Swagger UI
	srv.HandleFunc("/openapi.yaml", func(w netHttp.ResponseWriter, r *netHttp.Request) {
		w.Header().Set("Content-Type", "text/yaml")
		netHttp.ServeFile(w, r, "./openapi.yaml")
	})

	// Redirect root to login page
	srv.HandleFunc("/", func(w netHttp.ResponseWriter, r *netHttp.Request) {
		if r.URL.Path == "/" {
			netHttp.Redirect(w, r, "/web/login.html", netHttp.StatusTemporaryRedirect)
			return
		}
		netHttp.NotFound(w, r)
	})

	return srv
}