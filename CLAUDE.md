# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

A real-time chat application built with Go, using the Kratos framework (a microservices framework). The application provides gRPC and HTTP/REST APIs (via gRPC-Gateway), WebSocket support for real-time messaging, JWT authentication, PostgreSQL storage, and Redis caching.

## Architecture

The project follows **Clean Architecture** principles with clear layer separation:

- **api/**: Protocol Buffer definitions (`.proto` files) for two main services:
  - `chat/v1`: Chat and room management (ChatService, RoomService)
  - `user/v1`: User authentication and management (UserService)

- **cmd/chat**: Application entry point
  - Uses Wire for dependency injection (see `wire.go` and generated `wire_gen.go`)
  - Configuration loaded from YAML via Kratos config system

- **internal/**: Core application code following Clean Architecture layers:
  - `biz/`: Business logic layer (use cases) - independent of frameworks
  - `data/`: Data access layer (repositories) - database operations
  - `service/`: gRPC service implementations - translates between transport and business logic
  - `server/`: Transport layer - HTTP and gRPC server setup, WebSocket hub
  - `conf/`: Configuration structures (generated from proto)
  - `middleware/`: Authentication middleware (JWT validation)

**Layer dependencies flow**: service → biz → data (outer layers depend on inner)

## Key Components

### WebSocket Hub (`internal/server/websocket.go`)
- Manages real-time connections via a central Hub
- Hub maintains `rooms` map: `map[int64]map[*Client]bool`
- Clients authenticate with JWT, then join rooms by room ID
- Messages are broadcast directly to all clients in a room
- Message types: `auth`, `join_room`, `send_message`, `leave_room`, `ping`
- **Important**: User must join room via REST/gRPC API first to become a member before WebSocket join

### Wire Dependency Injection
- Wire configuration in `cmd/chat/wire.go` defines the dependency graph
- Generate with: `wire ./cmd/chat` or `wire gen ./cmd/chat`
- ProviderSets defined in: `data/data.go`, `biz/biz.go`, `service/service.go`, `server/server.go`
- Auth components: `JWTTokenManager` and `BcryptPasswordHasher` bound to interfaces

### Database Schema
- Located in `schema.sql` at project root
- Tables: `users`, `rooms`, `room_members`, `messages`, `message_reads`
- Auto-initialized in Docker via docker-entrypoint-initdb.d volume mount

## Development Commands

### Docker Compose (Recommended for local development)
```bash
# Start all services (PostgreSQL on 5433, Redis on 6381, app on 8001/9001)
docker-compose up -d

# View logs
docker-compose logs -f chat-app

# Stop all services
docker-compose down

# Rebuild after code changes
docker-compose up -d --build
```

### Local Development (without Docker)
```bash
# Install dependencies
go mod download

# Generate Wire dependency injection code (run after changing wire.go)
wire ./cmd/chat

# Run the application (expects PostgreSQL on 5432 and Redis on 6379)
go run ./cmd/chat -conf ./configs

# Build binary
go build -o chat-app ./cmd/chat
```

### Testing

#### Web-based Testing Tools (`web/` directory)
- **`web/index.html`**: Swagger UI for interactive REST API documentation
  - Loads `openapi.yaml` to display all endpoints
  - Test API calls directly in the browser
  - Access at `http://localhost:8001/` when app is running

- **`web/chat-test.html`**: Full-featured WebSocket test client
  - Connect to WebSocket server at `ws://localhost:8001/ws`
  - Authenticate with JWT tokens
  - Join/leave rooms and send real-time messages
  - View connection status and message history
  - Useful for testing the complete chat flow without building a client

#### Command-line Testing
```bash
# Run the included API test script (tests all endpoints)
./test_api.sh

# Manual testing with gRPCurl
grpcurl -plaintext localhost:9001 list
grpcurl -plaintext -d '{"username":"user1","email":"user@test.com","password":"pass123"}' \
  localhost:9001 api.user.v1.UserService/Register

# Manual testing with cURL (REST endpoints)
curl -X POST http://localhost:8001/api/v1/users/register \
  -H "Content-Type: application/json" \
  -d '{"username":"user1","email":"user@test.com","password":"pass123"}'
```

## Configuration

Configuration is loaded via Kratos config system from YAML files. The path is specified via `-conf` flag (defaults to `../../configs` from cmd/chat).

Key configuration sections:
- `server`: HTTP and gRPC server ports
- `data`: Database and Redis connection settings
- `auth`: JWT secret and token expiration

Configuration structure defined in `internal/conf/conf.proto` (generated to conf.pb.go).

## Code Generation

### Regenerate protobuf code (after editing .proto files)
The project currently doesn't have a Makefile. To regenerate protobuf code, you'll need:
```bash
# Install protoc compiler and plugins
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
go install github.com/go-kratos/kratos/cmd/protoc-gen-go-http/v2@latest

# Generate code for each service
protoc --proto_path=. \
       --proto_path=./third_party \
       --go_out=paths=source_relative:. \
       --go-grpc_out=paths=source_relative:. \
       --go-http_out=paths=source_relative:. \
       api/chat/v1/*.proto

protoc --proto_path=. \
       --proto_path=./third_party \
       --go_out=paths=source_relative:. \
       --go-grpc_out=paths=source_relative:. \
       --go-http_out=paths=source_relative:. \
       api/user/v1/*.proto

```

## Important Notes

- **Port mapping**: Docker Compose maps host ports differently (8001→8000, 9001→9000, 5433→5432, 6381→6379)
- **JWT Authentication**: Most endpoints require JWT token in Authorization header: `Authorization: Bearer <token>`
- **Context values**: User ID is injected into context by auth middleware as `"user_id"`
- **No Makefile**: The project references a Makefile in README but it doesn't exist yet
- **Module path**: Import path is `github.com/yourusername/chat-app` (placeholder - update if needed)
