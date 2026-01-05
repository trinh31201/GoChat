package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/golang-jwt/jwt/v4"
	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"

	chatV1 "github.com/yourusername/chat-app/api/chat/v1"
	"github.com/yourusername/chat-app/internal/client"
	"github.com/yourusername/chat-app/internal/metrics"
	"github.com/yourusername/chat-app/internal/middleware"
	"github.com/yourusername/chat-app/internal/service"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		// Allow all origins for development
		// In production, check the origin properly
		return true
	},
}

// Client represents a connected WebSocket client
type Client struct {
	ID          int64
	Username    string
	Conn        *websocket.Conn
	Send        chan []byte
	Hub         *Hub
	RoomID      int64
	ConnectedAt time.Time // Track connection time
	IP          string    // Client IP address
}

// RedisMessage represents a message published to Redis Pub/Sub
type RedisMessage struct {
	RoomID    int64  `json:"room_id"`
	MessageID int64  `json:"message_id,omitempty"`
	UserID    int64  `json:"user_id"`
	Username  string `json:"username"`
	Content   string `json:"content"`
	CreatedAt int64  `json:"created_at,omitempty"`
	// File attachment fields
	Type     string `json:"type,omitempty"`      // text, image, file
	FileURL  string `json:"file_url,omitempty"`
	FileName string `json:"file_name,omitempty"`
	FileSize int64  `json:"file_size,omitempty"`
	MimeType string `json:"mime_type,omitempty"`
}

// safeSend safely sends a message to a client's channel with panic recovery
func (c *Client) safeSend(message []byte) bool {
	defer func() {
		if r := recover(); r != nil {
			c.Hub.log.Warnf("Recovered from panic sending to client %s (user_id=%d): %v", c.Username, c.ID, r)
		}
	}()

	select {
	case c.Send <- message:
		return true
	default:
		// Channel is full - track dropped message
		c.Hub.droppedMessages.Add(1)
		metrics.RecordDroppedMessage()
		return false
	}
}

// Hub maintains active WebSocket connections
type Hub struct {
	// Registered clients by room
	rooms map[int64]map[*Client]bool

	// Register requests from clients
	register chan *Client

	// Unregister requests from clients
	unregister chan *Client

	// Mutex for concurrent access
	mu sync.RWMutex

	// Services
	chatService *service.ChatService
	roomService *service.RoomService

	// Logger
	log *log.Helper

	// Redis Pub/Sub
	redisClient *redis.Client

	// User Client for microservices mode (calls User Service for auth)
	userClient *client.UserClient

	// Performance monitoring
	droppedMessages  atomic.Int64 // Messages dropped due to full buffer
	activeBroadcasts atomic.Int64 // Currently running broadcast goroutines
}

// WebSocketMessage represents messages between client and server
type WebSocketMessage struct {
	Type    string          `json:"type"`
	RoomID  int64           `json:"room_id,omitempty"`
	Content string          `json:"content,omitempty"`
	Token   string          `json:"token,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
	// File attachment fields (for send_message with type=image/file)
	MessageType string `json:"message_type,omitempty"` // text, image, file
	FileURL     string `json:"file_url,omitempty"`
	FileName    string `json:"file_name,omitempty"`
	FileSize    int64  `json:"file_size,omitempty"`
	MimeType    string `json:"mime_type,omitempty"`
}

// NewHub creates a new WebSocket hub (monolith mode)
func NewHub(chatService *service.ChatService, roomService *service.RoomService, redisClient *redis.Client, logger log.Logger) *Hub {
	hub := &Hub{
		rooms:       make(map[int64]map[*Client]bool),
		register:    make(chan *Client, 100),
		unregister:  make(chan *Client, 100),
		chatService: chatService,
		roomService: roomService,
		redisClient: redisClient,
		log:         log.NewHelper(logger),
	}

	// Start Redis subscriber
	go hub.subscribeToRedis()

	// Start performance monitor
	go hub.monitorPerformance()

	return hub
}

// NewHubWithUserClient creates a new WebSocket hub (microservices mode)
// Uses userClient to call User Service for authentication
func NewHubWithUserClient(chatService *service.ChatService, roomService *service.RoomService, redisClient *redis.Client, userClient *client.UserClient, logger log.Logger) *Hub {
	hub := &Hub{
		rooms:       make(map[int64]map[*Client]bool),
		register:    make(chan *Client, 100),
		unregister:  make(chan *Client, 100),
		chatService: chatService,
		roomService: roomService,
		redisClient: redisClient,
		userClient:  userClient,
		log:         log.NewHelper(logger),
	}

	// Start Redis subscriber
	go hub.subscribeToRedis()

	// Start performance monitor
	go hub.monitorPerformance()

	return hub
}

// monitorPerformance logs performance stats every 5 seconds
func (h *Hub) monitorPerformance() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		// Count total clients
		h.mu.RLock()
		totalClients := 0
		totalRooms := len(h.rooms)
		for _, clients := range h.rooms {
			totalClients += len(clients)
		}
		h.mu.RUnlock()

		// Get stats
		goroutines := runtime.NumGoroutine()
		dropped := h.droppedMessages.Load()
		broadcasts := h.activeBroadcasts.Load()
		regLen := len(h.register)
		unregLen := len(h.unregister)

		// Update Prometheus metrics
		metrics.UpdateGoroutinesCount()
		metrics.SetActiveRooms(totalRooms)

		h.log.Infof("[PERF] goroutines=%d clients=%d rooms=%d dropped=%d activeBroadcasts=%d registerQueue=%d/%d unregisterQueue=%d/%d",
			goroutines, totalClients, totalRooms, dropped, broadcasts, regLen, 100, unregLen, 100)
	}
}

// Run starts the hub's event loop
func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.log.Infof("Hub received register request: user_id=%d, username=%s, room_id=%d", client.ID, client.Username, client.RoomID)
			h.mu.Lock()
			if h.rooms[client.RoomID] == nil {
				h.rooms[client.RoomID] = make(map[*Client]bool)
				h.log.Infof("Created new room %d in hub", client.RoomID)
			}
			h.rooms[client.RoomID][client] = true
			clientCount := len(h.rooms[client.RoomID])
			roomCount := len(h.rooms)
			metrics.IncWebSocketConnection()
			metrics.RecordRoomJoin()
			metrics.SetActiveRooms(roomCount)
			metrics.RecordUsersPerRoom(clientCount)
			h.mu.Unlock()

			h.log.Infof("Client %s joined room %d (total clients in room: %d)", client.Username, client.RoomID, clientCount)

			// Send join notification to room (direct broadcast)
			joinMsg := map[string]interface{}{
				"type":     "user_joined",
				"username": client.Username,
				"user_id":  client.ID,
				"room_id":  client.RoomID,
			}
			msgBytes, _ := json.Marshal(joinMsg)

			// Broadcast directly to all clients in room
			go func(roomClients map[*Client]bool, message []byte) {
				h.log.Infof("Broadcasting user_joined to %d clients", len(roomClients))
				for roomClient := range roomClients {
					go func(cl *Client) {
						if cl.safeSend(message) {
							h.log.Infof("Join notification sent to %s", cl.Username)
						} else {
							h.log.Warnf("Failed to send join notification to %s", cl.Username)
						}
					}(roomClient)
				}
			}(h.rooms[client.RoomID], msgBytes)

		case client := <-h.unregister:
			h.mu.Lock()
			var remainingClients map[*Client]bool
			if clients, ok := h.rooms[client.RoomID]; ok {
				if _, ok := clients[client]; ok {
					delete(clients, client)
					close(client.Send)
					metrics.DecWebSocketConnection()
					metrics.RecordRoomLeave()
					if len(clients) == 0 {
						delete(h.rooms, client.RoomID)
					} else {
						// Copy remaining clients for broadcasting
						remainingClients = make(map[*Client]bool)
						for cl := range clients {
							remainingClients[cl] = true
						}
						metrics.RecordUsersPerRoom(len(clients))
					}
					metrics.SetActiveRooms(len(h.rooms))
				}
			}
			h.mu.Unlock()

			h.log.Infof("Client %s left room %d", client.Username, client.RoomID)

			// Send leave notification to remaining clients (direct broadcast)
			if len(remainingClients) > 0 {
				leaveMsg := map[string]interface{}{
					"type":     "user_left",
					"username": client.Username,
					"user_id":  client.ID,
					"room_id":  client.RoomID,
				}
				msgBytes, _ := json.Marshal(leaveMsg)

				// Broadcast directly to remaining clients
				go func(clients map[*Client]bool, message []byte) {
					h.log.Infof("Broadcasting user_left to %d remaining clients", len(clients))
					for remainingClient := range clients {
						go func(cl *Client) {
							if cl.safeSend(message) {
								h.log.Infof("Leave notification sent to %s", cl.Username)
							} else {
								h.log.Warnf("Failed to send leave notification to %s", cl.Username)
							}
						}(remainingClient)
					}
				}(remainingClients, msgBytes)
			}
		}
	}
}

// HandleWebSocket handles WebSocket connections (monolith mode - local JWT validation)
func HandleWebSocket(hub *Hub, jwtSecret string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Upgrade HTTP connection to WebSocket
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			hub.log.Errorf("WebSocket upgrade failed: %v", err)
			return
		}

		// Create client
		client := &Client{
			Conn: conn,
			Send: make(chan []byte, 256),
			Hub:  hub,
		}

		// Start client goroutines
		go client.writePump()
		go client.readPump(jwtSecret)
	}
}

// HandleWebSocketWithUserClient handles WebSocket connections (microservices mode)
// Uses userClient to call User Service for authentication
func HandleWebSocketWithUserClient(hub *Hub, userClient *client.UserClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Get client IP
		clientIP := r.Header.Get("X-Real-IP")
		if clientIP == "" {
			clientIP = r.Header.Get("X-Forwarded-For")
		}
		if clientIP == "" {
			clientIP = r.RemoteAddr
		}

		hub.log.Infow("WebSocket connection attempt", "ip", clientIP)

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			hub.log.Errorw("WebSocket upgrade failed", "ip", clientIP, "error", err)
			return
		}

		client := &Client{
			Conn:        conn,
			Send:        make(chan []byte, 256),
			Hub:         hub,
			ConnectedAt: time.Now(),
			IP:          clientIP,
		}

		hub.log.Infow("WebSocket connected", "ip", clientIP)

		go client.writePump()
		go client.readPumpWithUserClient(userClient)
	}
}

// readPumpWithUserClient handles incoming messages using User Service for auth
func (c *Client) readPumpWithUserClient(userClient *client.UserClient) {
	defer func() {
		// Calculate session duration and record metric
		metrics.RecordConnectionDuration(c.ConnectedAt)
		c.Hub.log.Infow("WebSocket disconnected",
			"user_id", c.ID,
			"username", c.Username,
			"room_id", c.RoomID,
			"ip", c.IP,
			"duration_seconds", time.Since(c.ConnectedAt).Seconds(),
		)

		if c.ID != 0 && c.RoomID != 0 {
			c.Hub.unregister <- c
		}
		_ = c.Conn.Close()
	}()

	_ = c.Conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.Conn.SetPongHandler(func(string) error {
		_ = c.Conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		var msg WebSocketMessage
		err := c.Conn.ReadJSON(&msg)
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				c.Hub.log.Errorf("WebSocket error: %v", err)
			}
			break
		}

		switch msg.Type {
		case "auth":
			// Authenticate via User Service (gRPC call)
			if err := c.authenticateWithUserClient(msg.Token, userClient); err != nil {
				c.sendError("Authentication failed")
				return
			}
			c.sendSuccess("Authenticated successfully")

		case "join_room":
			if c.ID == 0 {
				c.sendError("Please authenticate first")
				continue
			}
			if err := c.joinRoom(msg.RoomID); err != nil {
				c.sendError(fmt.Sprintf("Failed to join room: %v", err))
				continue
			}

		case "send_message":
			if c.ID == 0 || c.RoomID == 0 {
				c.sendError("Please authenticate and join a room first")
				continue
			}
			if err := c.sendMessage(&msg); err != nil {
				c.sendError(fmt.Sprintf("Failed to send message: %v", err))
				continue
			}

		case "leave_room":
			if c.RoomID != 0 {
				c.Hub.unregister <- c
				c.RoomID = 0
				c.sendSuccess("Left room")
			}

		case "ping":
			c.safeSend([]byte(`{"type":"pong"}`))
		}
	}
}

// authenticateWithUserClient validates token via User Service (gRPC call)
func (c *Client) authenticateWithUserClient(tokenString string, userClient *client.UserClient) error {
	if tokenString == "" {
		return fmt.Errorf("missing token")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Call User Service to validate token
	userID, username, err := userClient.ValidateToken(ctx, tokenString)
	if err != nil {
		return fmt.Errorf("token validation failed: %v", err)
	}

	if userID == 0 {
		return fmt.Errorf("invalid token")
	}

	c.ID = userID
	c.Username = username

	c.Hub.log.Infow("User authenticated",
		"user_id", c.ID,
		"username", c.Username,
		"ip", c.IP,
		"method", "user_service",
	)
	return nil
}

// readPump handles incoming messages from the client
func (c *Client) readPump(jwtSecret string) {
	defer func() {
		if c.ID != 0 && c.RoomID != 0 {
			c.Hub.unregister <- c
		}
		_ = c.Conn.Close()
	}()

	_ = c.Conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.Conn.SetPongHandler(func(string) error {
		_ = c.Conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		var msg WebSocketMessage
		err := c.Conn.ReadJSON(&msg)
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				c.Hub.log.Errorf("WebSocket error: %v", err)
			}
			break
		}

		switch msg.Type {
		case "auth":
			// Authenticate the client
			if err := c.authenticate(msg.Token, jwtSecret); err != nil {
				c.sendError("Authentication failed")
				return
			}
			c.sendSuccess("Authenticated successfully")

		case "join_room":
			// Join a room
			if c.ID == 0 {
				c.sendError("Please authenticate first")
				continue
			}

			if err := c.joinRoom(msg.RoomID); err != nil {
				c.sendError(fmt.Sprintf("Failed to join room: %v", err))
				continue
			}

		case "send_message":
			// Send a message to the room (text, image, or file)
			if c.ID == 0 || c.RoomID == 0 {
				c.sendError("Please authenticate and join a room first")
				continue
			}

			if err := c.sendMessage(&msg); err != nil {
				c.sendError(fmt.Sprintf("Failed to send message: %v", err))
				continue
			}

		case "leave_room":
			// Leave the current room
			if c.RoomID != 0 {
				c.Hub.unregister <- c
				c.RoomID = 0
				c.sendSuccess("Left room")
			}

		case "ping":
			// Respond to ping
			c.safeSend([]byte(`{"type":"pong"}`))
		}
	}
}

// writePump handles outgoing messages to the client
func (c *Client) writePump() {
	ticker := time.NewTicker(54 * time.Second)
	defer func() {
		ticker.Stop()
		_ = c.Conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.Send:
			_ = c.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				_ = c.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			_ = c.Conn.WriteMessage(websocket.TextMessage, message)

		case <-ticker.C:
			_ = c.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// authenticate validates the JWT token and sets client info
func (c *Client) authenticate(tokenString string, jwtSecret string) error {
	if tokenString == "" {
		return fmt.Errorf("missing token")
	}

	// Parse JWT token
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method")
		}
		return []byte(jwtSecret), nil
	})

	if err != nil || !token.Valid {
		return fmt.Errorf("invalid token")
	}

	// Extract user info from token
	if claims, ok := token.Claims.(jwt.MapClaims); ok {
		if userID, ok := claims["user_id"].(float64); ok {
			c.ID = int64(userID)
		}
		if username, ok := claims["username"].(string); ok {
			c.Username = username
		}
	}

	if c.ID == 0 || c.Username == "" {
		return fmt.Errorf("invalid token claims")
	}

	return nil
}

// joinRoom adds the client to a room
func (c *Client) joinRoom(roomID int64) error {
	c.Hub.log.Infof("ENTER joinRoom: user_id=%d, username=%s, room_id=%d", c.ID, c.Username, roomID)

	if roomID <= 0 {
		c.Hub.log.Errorf("joinRoom FAILED: invalid room ID %d for user_id=%d", roomID, c.ID)
		return fmt.Errorf("invalid room ID")
	}

	// Debug: Log the user ID being used
	c.Hub.log.Infof("WebSocket joinRoom: user_id=%d trying to join room_id=%d", c.ID, roomID)

	// Check if user is a member of the room using the service
	ctx := context.Background()
	ctx = context.WithValue(ctx, middleware.UserIDKey, c.ID)

	// Verify room access (the service will check membership)
	room, err := c.Hub.roomService.GetRoom(ctx, &chatV1.GetRoomRequest{
		Id: roomID,
	})
	if err != nil {
		c.Hub.log.Errorf("WebSocket joinRoom failed: user_id=%d, room_id=%d, error=%v", c.ID, roomID, err)
		return fmt.Errorf("cannot access room: %v", err)
	}

	// Leave current room if in one
	if c.RoomID != 0 {
		c.Hub.unregister <- c
	}

	// Join new room
	c.RoomID = roomID
	c.Hub.log.Infof("Sending client to register channel: user_id=%d, room_id=%d", c.ID, roomID)
	c.Hub.register <- c

	// Send room info to client
	roomInfo := map[string]interface{}{
		"type":    "room_joined",
		"room_id": room.Id,
		"room":    room,
	}
	msgBytes, _ := json.Marshal(roomInfo)
	c.safeSend(msgBytes)

	c.Hub.log.Infof("EXIT joinRoom: user_id=%d successfully joined room_id=%d", c.ID, roomID)
	return nil
}

// GetRoomClients returns a copy of all clients in a room (thread-safe)
func (h *Hub) GetRoomClients(roomID int64) map[*Client]bool {
	h.mu.RLock()
	defer h.mu.RUnlock()

	// Return copy to avoid race conditions
	clients := make(map[*Client]bool)
	if roomClients, exists := h.rooms[roomID]; exists {
		for client := range roomClients {
			clients[client] = true
		}
	}

	h.log.Infof("GetRoomClients: room %d has %d clients", roomID, len(clients))
	return clients
}

// sendMessage sends a message to the room (supports text, image, file)
func (c *Client) sendMessage(wsMsg *WebSocketMessage) error {
	startTime := time.Now()

	// Determine message type
	msgType := wsMsg.MessageType
	if msgType == "" {
		msgType = "text"
	}

	// Validate based on type
	if msgType == "text" && wsMsg.Content == "" {
		return fmt.Errorf("empty message")
	}
	if (msgType == "image" || msgType == "file") && wsMsg.FileURL == "" {
		return fmt.Errorf("file_url required for image/file messages")
	}

	// Use the chat service to send the message
	ctx := context.Background()
	ctx = context.WithValue(ctx, middleware.UserIDKey, c.ID)
	ctx = context.WithValue(ctx, middleware.UsernameKey, c.Username)

	msg, err := c.Hub.chatService.SendMessage(ctx, &chatV1.SendMessageRequest{
		RoomId:   c.RoomID,
		Content:  wsMsg.Content,
		Type:     msgType,
		FileUrl:  wsMsg.FileURL,
		FileName: wsMsg.FileName,
		FileSize: wsMsg.FileSize,
		MimeType: wsMsg.MimeType,
	})
	if err != nil {
		c.Hub.log.Errorw("Failed to send message",
			"user_id", c.ID,
			"room_id", c.RoomID,
			"type", msgType,
			"error", err,
			"duration_ms", time.Since(startTime).Milliseconds(),
		)
		return err
	}

	c.Hub.log.Infow("Message sent",
		"message_id", msg.Id,
		"user_id", c.ID,
		"username", c.Username,
		"room_id", c.RoomID,
		"type", msgType,
		"content_length", len(wsMsg.Content),
		"duration_ms", time.Since(startTime).Milliseconds(),
	)

	// Publish to Redis instead of local broadcast
	redisMsg := RedisMessage{
		RoomID:    msg.RoomId,
		MessageID: msg.Id,
		UserID:    msg.UserId,
		Username:  msg.Username,
		Content:   msg.Content,
		CreatedAt: msg.CreatedAt,
		Type:      msg.Type,
		FileURL:   msg.FileUrl,
		FileName:  msg.FileName,
		FileSize:  msg.FileSize,
		MimeType:  msg.MimeType,
	}

	msgBytes, _ := json.Marshal(redisMsg)
	channel := fmt.Sprintf("room:%d", c.RoomID)

	if err := c.Hub.redisClient.Publish(ctx, channel, msgBytes).Err(); err != nil {
		c.Hub.log.Errorf("Redis publish failed: %v", err)
		return err
	}

	c.Hub.log.Infof("Published to Redis channel: %s", channel)
	metrics.RecordMessageSent("public") // Track message sent
	metrics.RecordMessageLatency(startTime)
	return nil
}

// sendError sends an error message to the client
func (c *Client) sendError(message string) {
	errMsg := map[string]interface{}{
		"type":    "error",
		"message": message,
	}
	msgBytes, _ := json.Marshal(errMsg)
	c.safeSend(msgBytes)
}

// sendSuccess sends a success message to the client
func (c *Client) sendSuccess(message string) {
	successMsg := map[string]interface{}{
		"type":    "success",
		"message": message,
	}
	msgBytes, _ := json.Marshal(successMsg)
	c.safeSend(msgBytes)
}

// subscribeToRedis listens for messages from Redis Pub/Sub
func (h *Hub) subscribeToRedis() {
	ctx := context.Background()
	pubsub := h.redisClient.PSubscribe(ctx, "room:*")
	defer func() { _ = pubsub.Close() }()

	h.log.Info("Redis Pub/Sub subscriber started - listening to room:*")

	for msg := range pubsub.Channel() {
		var redisMsg RedisMessage
		if err := json.Unmarshal([]byte(msg.Payload), &redisMsg); err != nil {
			h.log.Errorf("Failed to unmarshal Redis message: %v", err)
			continue
		}

		h.log.Infof("Received from Redis: channel=%s, room=%d, user=%s, content=%s",
			msg.Channel, redisMsg.RoomID, redisMsg.Username, redisMsg.Content)

		// Broadcast to local WebSocket clients in this room
		h.mu.RLock()
		clients := h.rooms[redisMsg.RoomID]
		h.mu.RUnlock()

		if len(clients) == 0 {
			h.log.Infof("No local clients in room %d, skipping broadcast", redisMsg.RoomID)
			continue
		}

		// Build WebSocket message
		msgData := map[string]interface{}{
			"type":       "new_message",
			"message_id": redisMsg.MessageID,
			"room_id":    redisMsg.RoomID,
			"user_id":    redisMsg.UserID,
			"username":   redisMsg.Username,
			"content":    redisMsg.Content,
			"created_at": redisMsg.CreatedAt,
		}

		// Add file fields if present
		if redisMsg.FileURL != "" {
			msgData["message_type"] = redisMsg.Type
			msgData["file_url"] = redisMsg.FileURL
			msgData["file_name"] = redisMsg.FileName
			msgData["file_size"] = redisMsg.FileSize
			msgData["mime_type"] = redisMsg.MimeType
		}

		msgBytes, _ := json.Marshal(msgData)

		h.log.Infof("Broadcasting to %d local clients in room %d", len(clients), redisMsg.RoomID)

		// Send to all local WebSocket connections
		broadcastStart := time.Now()
		for client := range clients {
			go client.safeSend(msgBytes)
		}
		metrics.RecordBroadcastDuration(broadcastStart)
	}
}
