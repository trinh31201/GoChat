# Real-Time Chat Application

A scalable real-time chat application built with Go, featuring microservices architecture, WebSocket support, and horizontal scaling.

## Live Demo

**Try it now:** [http://54.79.163.178](http://54.79.163.178)

- Create an account and start chatting in real-time
- Create or join chat rooms
- Experience WebSocket-powered instant messaging

## Architecture

```
                         ┌─────────────────┐
                         │     Nginx       │
                         │  Load Balancer  │
                         └────────┬────────┘
                                  │
              ┌───────────────────┼───────────────────┐
              │                   │                   │
              ▼                   ▼                   ▼
       ┌─────────────┐    ┌─────────────┐    ┌─────────────┐
       │    User     │    │    Chat     │    │    Chat     │
       │   Service   │    │  Service 1  │    │  Service 2  │
       └──────┬──────┘    └──────┬──────┘    └──────┬──────┘
              │                  │                   │
              │                  └─────────┬─────────┘
              │                            │
              ▼                            ▼
       ┌─────────────┐              ┌─────────────┐
       │ PostgreSQL  │              │    Redis    │
       │  Database   │              │   Pub/Sub   │
       └─────────────┘              └─────────────┘
                                           │
                                           ▼
                                    ┌─────────────┐
                                    │ Prometheus  │
                                    │ + Grafana   │
                                    └─────────────┘
```

## Features

- **Real-time Messaging**: WebSocket-based instant message delivery
- **Microservices**: Separate User Service and Chat Service
- **Horizontal Scaling**: Multiple Chat Service instances behind Nginx
- **Cross-server Messaging**: Redis Pub/Sub for message sync across servers
- **Monitoring**: Prometheus metrics + Grafana dashboards
- **JWT Authentication**: Secure token-based auth
- **Room-based Chat**: Create and join chat rooms

## Tech Stack

| Component | Technology |
|-----------|------------|
| Backend | Go, Kratos Framework |
| API | gRPC + REST (gRPC-Gateway) |
| Real-time | WebSocket (Gorilla) |
| Database | PostgreSQL |
| Cache/PubSub | Redis |
| Load Balancer | Nginx |
| Monitoring | Prometheus, Grafana |
| Containerization | Docker, Docker Compose |

## Quick Start

```bash
# Clone the repository
git clone https://github.com/yourusername/chat-app.git
cd chat-app

# Start all services
docker-compose up -d

# View logs
docker-compose logs -f
```

### Access Points

| Service | URL |
|---------|-----|
| Chat App | http://localhost |
| Prometheus | http://localhost:9090 |
| Grafana | http://localhost:3000 (admin/admin) |

## Project Structure

```
chat-app/
├── api/                    # Protocol buffer definitions
│   ├── chat/v1/           # Chat & Room services
│   └── user/v1/           # User service
├── cmd/
│   ├── chat/              # Chat Service entry point
│   └── user/              # User Service entry point
├── internal/
│   ├── biz/               # Business logic
│   ├── data/              # Data access layer
│   ├── server/            # HTTP/gRPC/WebSocket servers
│   ├── service/           # gRPC service implementations
│   ├── middleware/        # JWT auth middleware
│   └── metrics/           # Prometheus metrics
├── nginx/                  # Nginx configuration
├── prometheus/             # Prometheus configuration
├── configs/                # Application configs
└── test/                   # Load tests (k6)
```

## API Overview

### REST Endpoints

```bash
# User Service
POST /api/v1/users/register    # Register new user
POST /api/v1/users/login       # Login

# Room Service
POST /api/v1/rooms             # Create room
GET  /api/v1/rooms/{id}        # Get room
POST /api/v1/rooms/{id}/join   # Join room

# Chat Service
GET  /api/v1/rooms/{id}/messages  # Get messages
```

### WebSocket Protocol

```javascript
// Connect
ws://localhost/ws

// Authenticate
{ "type": "auth", "token": "jwt_token" }

// Join Room
{ "type": "join_room", "room_id": 1 }

// Send Message
{ "type": "send_message", "content": "Hello!" }

// Leave Room
{ "type": "leave_room" }
```

## Scaling

The application supports horizontal scaling:

```yaml
# Add more Chat Service instances in docker-compose.yml
chat-service-3:
  build: ./cmd/chat
  environment:
    - SERVER_ID=chat-3
```

Messages sync across all instances via Redis Pub/Sub.

## Monitoring

### Metrics Available

- `websocket_connections` - Current active connections
- `messages_sent_total` - Total messages sent
- `auth_requests_total` - Authentication attempts
- `grpc_calls_total` - Service-to-service calls

### Grafana Dashboard

1. Open http://localhost:3000
2. Login: admin / admin
3. Add Prometheus data source: http://prometheus:9090
4. Create dashboard with metrics above

## Load Testing

```bash
# Run load test with 50 concurrent users
k6 run test/simple-load-test.js
```

## Development

```bash
# Generate protobuf code
make api

# Generate Wire dependencies
wire ./cmd/chat

# Run locally
go run ./cmd/chat -conf ./configs
```

## Key Design Decisions

| Decision | Reason |
|----------|--------|
| Microservices | Scale User and Chat independently |
| Redis Pub/Sub | Real-time cross-server message sync |
| Nginx ip_hash | Sticky sessions for WebSocket |
| Goroutines | Handle 10K+ concurrent connections |
| Structured Logging | Easy debugging and monitoring |

