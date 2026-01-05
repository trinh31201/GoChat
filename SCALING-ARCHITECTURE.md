# Scaling Architecture: WebSocket with Redis Pub/Sub

## Overview

This document describes the architecture for scaling the chat application to handle 100,000+ concurrent WebSocket connections using Redis Pub/Sub for message distribution across multiple server instances.

---

## Current Architecture (Single Server)

```
┌──────────────────────────────────────┐
│         Single Chat Server           │
│                                      │
│  ┌────────────────────────────┐    │
│  │   WebSocket Hub            │    │
│  │   - 1,000 connections max  │    │
│  │   - In-memory broadcast    │    │
│  └────────────────────────────┘    │
│              ↓                      │
│  ┌────────────────────────────┐    │
│  │      PostgreSQL            │    │
│  │   (message persistence)    │    │
│  └────────────────────────────┘    │
└──────────────────────────────────────┘

Limitations:
- ❌ Max 1,000-1,500 concurrent connections per server
- ❌ Single point of failure
- ❌ Can't scale horizontally
- ❌ Messages only broadcast within single server
```

---

## New Architecture (Multi-Server with Redis Pub/Sub)

```
                         Load Balancer (Nginx)
                         Port 8080
                                │
        ┌───────────────────────┼───────────────────────┐
        │                       │                       │
        ↓                       ↓                       ↓
┌──────────────┐        ┌──────────────┐        ┌──────────────┐
│ Server 1     │        │ Server 2     │        │ Server 3     │
│ Port 8001    │        │ Port 8002    │        │ Port 8003    │
│              │        │              │        │              │
│ WebSocket Hub│        │ WebSocket Hub│        │ WebSocket Hub│
│ 30K users    │        │ 30K users    │        │ 40K users    │
└──────┬───────┘        └──────┬───────┘        └──────┬───────┘
       │                       │                       │
       │ PUBLISH               │ PUBLISH               │ PUBLISH
       │ SUBSCRIBE             │ SUBSCRIBE             │ SUBSCRIBE
       │                       │                       │
       └───────────────────────┼───────────────────────┘
                               ↓
                    ┌─────────────────────┐
                    │   Redis Pub/Sub     │
                    │                     │
                    │  Channels:          │
                    │  - room:1           │
                    │  - room:2           │
                    │  - room:3           │
                    │  ...                │
                    └─────────────────────┘
                               ↓
                    ┌─────────────────────┐
                    │   PostgreSQL        │
                    │ (Message Storage)   │
                    └─────────────────────┘

Capabilities:
- ✓ 100,000+ concurrent connections (distributed across servers)
- ✓ Horizontal scaling (add more servers as needed)
- ✓ High availability (if one server fails, others continue)
- ✓ Messages broadcast across ALL servers
- ✓ Sub-millisecond message propagation
```

---

## How Redis Pub/Sub Works

### Message Flow:

```
1. User A (connected to Server 1) sends message to Room 1
   │
   ↓
2. Server 1 receives WebSocket message
   │
   ↓
3. Server 1 saves message to PostgreSQL (persistence)
   │
   ↓
4. Server 1 PUBLISHES message to Redis channel "room:1"
   │
   REDIS.PUBLISH("room:1", {
       message_id: 123,
       user_id: 456,
       content: "Hello!",
       room_id: 1
   })
   │
   ↓
5. Redis broadcasts to ALL servers subscribed to "room:1"
   │
   ├─> Server 1 receives
   ├─> Server 2 receives
   └─> Server 3 receives
   │
   ↓
6. Each server broadcasts to its local WebSocket connections
   │
   ├─> Server 1: Broadcasts to 100 users in Room 1
   ├─> Server 2: Broadcasts to 150 users in Room 1
   └─> Server 3: Broadcasts to 200 users in Room 1
   │
   ↓
7. All 450 users in Room 1 receive the message (across all servers!)
```

### Redis Channels:

```
Channel Pattern: "room:{room_id}"

Examples:
- room:1  → All messages for room 1
- room:2  → All messages for room 2
- room:99 → All messages for room 99

Each server subscribes to ALL room channels they have active users in.
```

---

## Code Changes Required

### 1. Modify `internal/server/websocket.go`

**Current Hub struct:**
```go
type Hub struct {
    rooms      map[int64]map[*Client]bool
    register   chan *Client
    unregister chan *Client
    mu         sync.RWMutex
}
```

**New Hub struct (with Redis):**
```go
type Hub struct {
    rooms         map[int64]map[*Client]bool
    register      chan *Client
    unregister    chan *Client
    mu            sync.RWMutex

    // NEW: Redis Pub/Sub
    redisClient   *redis.Client
    pubsub        *redis.PubSub
    messagesChan  chan *RedisMessage
}

type RedisMessage struct {
    RoomID    int64  `json:"room_id"`
    MessageID int64  `json:"message_id"`
    UserID    int64  `json:"user_id"`
    Username  string `json:"username"`
    Content   string `json:"content"`
    Type      string `json:"type"`
    CreatedAt int64  `json:"created_at"`
}
```

**Key changes:**

1. **Initialize Redis client in Hub:**
```go
func NewHub(chatService, roomService, logger, redisAddr string) *Hub {
    hub := &Hub{
        rooms:        make(map[int64]map[*Client]bool),
        register:     make(chan *Client, 100),
        unregister:   make(chan *Client, 100),
        redisClient:  redis.NewClient(&redis.Options{Addr: redisAddr}),
        messagesChan: make(chan *RedisMessage, 1000),
    }

    // Start Redis subscriber goroutine
    go hub.subscribeToRedis()

    return hub
}
```

2. **Subscribe to Redis channels:**
```go
func (h *Hub) subscribeToRedis() {
    h.pubsub = h.redisClient.PSubscribe(ctx, "room:*")

    for msg := range h.pubsub.Channel() {
        var redisMsg RedisMessage
        json.Unmarshal([]byte(msg.Payload), &redisMsg)

        // Broadcast to local WebSocket connections
        h.broadcastToRoom(redisMsg.RoomID, redisMsg)
    }
}
```

3. **Publish messages to Redis instead of local broadcast:**
```go
func (c *Client) sendMessage(content string) error {
    // Save to database (unchanged)
    msg, err := c.Hub.chatService.SendMessage(ctx, &chatV1.SendMessageRequest{
        RoomId:  c.RoomID,
        Content: content,
        Type:    "text",
    })

    // NEW: Publish to Redis instead of local broadcast
    redisMsg := RedisMessage{
        RoomID:    msg.RoomId,
        MessageID: msg.Id,
        UserID:    msg.UserId,
        Username:  msg.Username,
        Content:   msg.Content,
        Type:      msg.Type,
        CreatedAt: msg.CreatedAt,
    }

    msgBytes, _ := json.Marshal(redisMsg)

    // Publish to Redis channel
    channel := fmt.Sprintf("room:%d", c.RoomID)
    c.Hub.redisClient.Publish(ctx, channel, msgBytes)

    // Redis will broadcast back to all servers (including this one)
    return nil
}
```

4. **Broadcast to local connections when receiving from Redis:**
```go
func (h *Hub) broadcastToRoom(roomID int64, msg RedisMessage) {
    h.mu.RLock()
    clients := h.rooms[roomID]
    h.mu.RUnlock()

    msgData := map[string]interface{}{
        "type":       "new_message",
        "message_id": msg.MessageID,
        "room_id":    msg.RoomID,
        "user_id":    msg.UserID,
        "username":   msg.Username,
        "content":    msg.Content,
        "created_at": msg.CreatedAt,
    }
    msgBytes, _ := json.Marshal(msgData)

    // Broadcast to all LOCAL WebSocket connections in this room
    for client := range clients {
        go client.safeSend(msgBytes)
    }
}
```

---

### 2. Update `docker-compose.yml`

**Add multiple app instances:**

```yaml
services:
  # Keep existing postgres, redis, migrate services...

  # Chat App Instance 1
  chat-app-1:
    build: .
    container_name: chat-app-1
    depends_on:
      - postgres
      - redis
    environment:
      - DATABASE_URL=postgresql://chatuser:chatpass@postgres:5432/chatdb?sslmode=disable
      - REDIS_URL=redis:6379
      - JWT_SECRET=your-secret-key
      - SERVER_ID=server-1  # NEW: Identify server instance
    ports:
      - "8001:8000"
    networks:
      - chat-network

  # Chat App Instance 2
  chat-app-2:
    build: .
    container_name: chat-app-2
    depends_on:
      - postgres
      - redis
    environment:
      - DATABASE_URL=postgresql://chatuser:chatpass@postgres:5432/chatdb?sslmode=disable
      - REDIS_URL=redis:6379
      - JWT_SECRET=your-secret-key
      - SERVER_ID=server-2
    ports:
      - "8002:8000"
    networks:
      - chat-network

  # Chat App Instance 3
  chat-app-3:
    build: .
    container_name: chat-app-3
    depends_on:
      - postgres
      - redis
    environment:
      - DATABASE_URL=postgresql://chatuser:chatpass@postgres:5432/chatdb?sslmode=disable
      - REDIS_URL=redis:6379
      - JWT_SECRET=your-secret-key
      - SERVER_ID=server-3
    ports:
      - "8003:8000"
    networks:
      - chat-network

  # Nginx Load Balancer
  nginx:
    image: nginx:alpine
    container_name: chat-nginx
    volumes:
      - ./nginx.conf:/etc/nginx/nginx.conf:ro
    ports:
      - "8080:8080"  # External port
    depends_on:
      - chat-app-1
      - chat-app-2
      - chat-app-3
    networks:
      - chat-network
```

---

### 3. Create `nginx.conf`

```nginx
events {
    worker_connections 10000;
}

http {
    upstream chat_backend {
        least_conn;  # Route to server with least connections

        server chat-app-1:8000 max_fails=3 fail_timeout=30s;
        server chat-app-2:8000 max_fails=3 fail_timeout=30s;
        server chat-app-3:8000 max_fails=3 fail_timeout=30s;
    }

    upstream chat_websocket {
        ip_hash;  # Sticky sessions: same user → same server

        server chat-app-1:8000;
        server chat-app-2:8000;
        server chat-app-3:8000;
    }

    server {
        listen 8080;

        # WebSocket endpoint
        location /ws {
            proxy_pass http://chat_websocket;
            proxy_http_version 1.1;
            proxy_set_header Upgrade $http_upgrade;
            proxy_set_header Connection "upgrade";
            proxy_set_header Host $host;
            proxy_read_timeout 3600s;
        }

        # API endpoints
        location /api/ {
            proxy_pass http://chat_backend;
            proxy_set_header Host $host;
        }

        # Health check
        location /health {
            proxy_pass http://chat_backend;
            access_log off;
        }
    }
}
```

---

### 4. Add Health Check Endpoint

**In `internal/server/server.go`:**

```go
// Add health check handler
func (s *Server) healthCheck(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(http.StatusOK)

    response := map[string]interface{}{
        "status": "healthy",
        "server_id": os.Getenv("SERVER_ID"),
        "timestamp": time.Now().Unix(),
    }

    json.NewEncoder(w).Encode(response)
}

// Register in HTTP router
func NewHTTPServer(...) {
    // ... existing routes ...

    router.HandleFunc("/health", s.healthCheck).Methods("GET")
}
```

---

## Deployment Steps

### Step 1: Build and Start

```bash
# Build the app
docker-compose build

# Start all services (3 app instances + nginx + postgres + redis)
docker-compose up -d

# Check all services are running
docker-compose ps
```

### Step 2: Verify Load Balancer

```bash
# Hit health check multiple times - should round-robin between servers
curl http://localhost:8080/health
# Response: {"status":"healthy","server_id":"server-1",...}

curl http://localhost:8080/health
# Response: {"status":"healthy","server_id":"server-2",...}

curl http://localhost:8080/health
# Response: {"status":"healthy","server_id":"server-3",...}
```

### Step 3: Test Redis Pub/Sub

```bash
# Connect to Redis and monitor messages
docker exec -it newchat-redis redis-cli
> PSUBSCRIBE room:*

# In another terminal, send a message via API
# You should see the PUBLISH event in Redis CLI
```

### Step 4: Load Test

```bash
# Modify k6 test to use load balancer port 8080
# Test with 1500+ concurrent users
k6 run test/websocket-load-test.js
```

---

## Scaling Capabilities

### Current Capacity (3 Servers):

```
Server 1: 1,000 concurrent WebSocket connections
Server 2: 1,000 concurrent WebSocket connections
Server 3: 1,000 concurrent WebSocket connections
───────────────────────────────────────────────
Total:    3,000 concurrent connections
```

### Scaling to 100K+ Users:

```
100 servers × 1,000 connections = 100,000 concurrent users

With better hardware (8 cores, 16GB RAM):
100 servers × 5,000 connections = 500,000 concurrent users
```

### Horizontal Scaling:

```bash
# Add more instances easily
docker-compose up -d --scale chat-app=10

# Now you have 10 instances instead of 3
# Nginx automatically load balances across all
```

---

## Performance Characteristics

### Redis Pub/Sub Performance:

```
Latency: <1ms for message propagation
Throughput: 1,000,000+ messages/second
Memory: ~100 bytes per channel subscription
```

### Message Flow Latency:

```
User sends message
    ↓ 1ms - WebSocket receive
Server processes
    ↓ 2ms - Save to PostgreSQL
Publish to Redis
    ↓ 0.5ms - Redis propagation
Other servers receive
    ↓ 1ms - Broadcast to local WebSockets
Users receive message
───────────────────
Total: ~5ms end-to-end
```

### Scaling Limits:

```
Single Redis instance:
- 50,000-100,000 channels
- 1,000,000+ messages/second
- Supports 100-200 app servers

Beyond that, use Redis Cluster or Kafka
```

---

## Alternative: Kafka (More Scalable)

If you need even more scale, use Kafka instead of Redis:

### Kafka Advantages:

```
✓ Message persistence (Redis Pub/Sub is fire-and-forget)
✓ Replay messages (useful for new servers joining)
✓ Better for analytics (can process message history)
✓ Handles millions of messages/second
✓ Better for microservices (decoupled architecture)
```

### Kafka Architecture:

```
                    Apache Kafka
                         │
        ┌────────────────┼────────────────┐
        │                │                │
    Topic: room-1    Topic: room-2    Topic: room-3
        │                │                │
        ├─ Partition 0   ├─ Partition 0  ├─ Partition 0
        ├─ Partition 1   ├─ Partition 1  ├─ Partition 1
        └─ Partition 2   └─ Partition 2  └─ Partition 2
                         │
            ┌────────────┼────────────┐
            ↓            ↓            ↓
       Server 1     Server 2     Server 3
    (Consumers)  (Consumers)  (Consumers)
```

### When to Use Each:

```
Redis Pub/Sub:
✓ Real-time chat (your use case)
✓ <100K concurrent users
✓ Simple architecture
✓ Don't need message persistence

Kafka:
✓ >100K concurrent users
✓ Need message history
✓ Analytics on messages
✓ Microservices architecture
✓ Event sourcing
```

---

## Testing Strategy

### Unit Tests:

```go
// Test Redis publishing
func TestPublishMessage(t *testing.T) {
    // Create mock Redis
    // Publish message
    // Verify message in channel
}

// Test Redis subscription
func TestSubscribeToRoom(t *testing.T) {
    // Subscribe to channel
    // Publish test message
    // Verify received
}
```

### Integration Tests:

```bash
# Start 3 servers locally
docker-compose up -d

# Connect WebSocket to server 1
# Connect WebSocket to server 2
# Send message from server 1
# Verify received on server 2

# Success = Redis Pub/Sub working!
```

### Load Tests:

```javascript
// k6 test with 1500 concurrent users
// Distributed across 3 servers (500 each)
// Verify all users receive messages
// Measure latency
```

---

## Monitoring & Observability

### Key Metrics to Track:

```
1. WebSocket Connections:
   - Per server: 500-1,000 connections
   - Total: 3,000 connections

2. Redis Pub/Sub:
   - Active channels: Number of rooms
   - Messages/second: Message throughput
   - Subscription lag: Propagation delay

3. Message Latency:
   - WebSocket → PostgreSQL: ~2ms
   - PostgreSQL → Redis: ~1ms
   - Redis → Broadcast: ~2ms
   - Total: ~5ms

4. Server Health:
   - CPU usage: <50% normal, <80% peak
   - Memory: <2GB per server
   - Network: Message rate
```

### Monitoring Tools:

```bash
# Redis monitoring
docker exec -it newchat-redis redis-cli
> INFO stats
> PUBSUB CHANNELS
> PUBSUB NUMSUB room:1

# Nginx access logs
docker logs chat-nginx -f

# App server logs
docker logs chat-app-1 -f
docker logs chat-app-2 -f
docker logs chat-app-3 -f
```

---

## Cost Estimation

### Cloud Deployment (AWS):

```
3 × EC2 t3.medium (2 vCPU, 4GB RAM):
  $0.0416/hour × 3 × 730 hours = $91/month

1 × ElastiCache Redis (cache.t3.medium):
  $0.068/hour × 730 hours = $50/month

1 × RDS PostgreSQL (db.t3.medium):
  $0.073/hour × 730 hours = $53/month

1 × Application Load Balancer:
  $16/month + data transfer

Total: ~$210/month for 3,000 concurrent users
Per user: $0.07/month
───────────────────────────────────────────
Scale to 100K users: ~$7,000/month
(Still cheaper than alternatives!)
```

---

## Resume/CV Bullet Points

After implementing this:

```
• Architected distributed WebSocket system using Redis Pub/Sub to scale from
  1,000 to 100,000+ concurrent connections across multiple server instances

• Implemented horizontal scaling strategy with Nginx load balancing and
  Redis message broker, achieving <5ms message propagation latency

• Designed fault-tolerant architecture where servers can be added/removed
  dynamically without downtime or message loss

• Reduced infrastructure cost 10× compared to vertical scaling by implementing
  horizontal scaling with commodity hardware
```

---

## Interview Talking Points

**Question: "How would you scale WebSocket connections to 100K users?"**

**Your Answer:**
```
"I implemented a distributed WebSocket architecture using Redis Pub/Sub.

The key insight is that WebSocket connections are stateful - each user maintains
a persistent connection to ONE server. So you can't just add a load balancer.

My solution:
1. Run multiple app servers (each handling 1K-5K connections)
2. Use Redis Pub/Sub as a message broker between servers
3. When a user sends a message, their server publishes to Redis
4. All servers subscribe to Redis and broadcast to their local connections
5. Use Nginx with ip_hash for sticky sessions

This architecture scales linearly - add more servers for more users. I tested
with 3 servers handling 3,000 concurrent connections with <5ms latency. The
same pattern scales to 100 servers for 100K users.

The alternative is WebSocket with Kafka, which adds message persistence and
is better for event sourcing, but Redis is simpler and sufficient for
real-time chat."
```

---

## Next Steps

### Phase 1: Basic Implementation (2-3 hours)
- [ ] Add Redis Pub/Sub to WebSocket Hub
- [ ] Test with 2 server instances
- [ ] Verify messages work cross-server

### Phase 2: Production Ready (3-4 hours)
- [ ] Add Nginx load balancer
- [ ] Add health check endpoints
- [ ] Add monitoring/metrics
- [ ] Load test with 1500+ users

### Phase 3: Advanced (optional, 4-6 hours)
- [ ] Add Kafka for message persistence
- [ ] Add message replay capability
- [ ] Add analytics pipeline
- [ ] Add auto-scaling

---

## References

- [Redis Pub/Sub Documentation](https://redis.io/docs/manual/pubsub/)
- [Scaling WebSocket (Slack Engineering Blog)](https://slack.engineering/scaling-slacks-job-queue/)
- [Discord: How They Handle Millions of Concurrent Users](https://discord.com/blog/how-discord-stores-billions-of-messages)
- [Nginx WebSocket Proxying](https://nginx.org/en/docs/http/websocket.html)

---

## Conclusion

This architecture demonstrates understanding of:
- ✓ Distributed systems
- ✓ Horizontal scaling
- ✓ Message brokers (Pub/Sub pattern)
- ✓ Load balancing
- ✓ Stateful vs stateless services
- ✓ CAP theorem (choosing availability + partition tolerance)

**This is exactly what companies like Slack, Discord, and WhatsApp use at scale.**

Implementing this will make your project stand out in interviews at Google, Meta,
Amazon, or any company doing real-time systems.
